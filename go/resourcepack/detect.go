package resourcepack

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/rmmh/cubeographer/go/jvm"
)

func DetectRegistryMeta(jar *zip.ReadCloser) (blocksClass, blockClass, registerMethod, initMethod string, err error) {
	var candidateBlocks []*jvm.ClassFile
	for _, f := range jar.File {
		if strings.HasSuffix(f.Name, ".class") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}

			cf, err := jvm.ParseClassFile(data)
			if err != nil {
				continue
			}

			// Scan constant pool for block IDs: "command_block", "stone", "lava"
			hasCommandBlock := false
			hasStone := false
			hasLava := false
			for i := 1; i < len(cf.ConstantPool); i++ {
				if s, ok := cf.ConstantPool[i].(jvm.CPString); ok {
					strVal := cf.ResolveUtf8(s.StringIndex)
					if strVal == "command_block" {
						hasCommandBlock = true
					} else if strVal == "stone" {
						hasStone = true
					} else if strVal == "lava" {
						hasLava = true
					}
				}
			}

			if hasCommandBlock && hasStone && hasLava {
				candidateBlocks = append(candidateBlocks, cf)
			}
		}
	}

	var bestCF *jvm.ClassFile
	bestBlockDesc := ""
	maxFieldCount := 0

	for _, cf := range candidateBlocks {
		fieldTypes := make(map[string]int)
		for _, field := range cf.Fields {
			if strings.HasPrefix(field.Descriptor, "L") && strings.HasSuffix(field.Descriptor, ";") {
				fieldTypes[field.Descriptor]++
			}
		}
		for desc, count := range fieldTypes {
			if count > maxFieldCount {
				maxFieldCount = count
				bestBlockDesc = desc
				bestCF = cf
			}
		}
	}

	if bestCF == nil {
		return "", "", "", "", fmt.Errorf("failed to find Blocks registry class")
	}

	blocksClass = bestCF.ThisClass
	blockClass = strings.TrimSuffix(strings.TrimPrefix(bestBlockDesc, "L"), ";")

	// Now find the initialization method M_init and registration helper M_reg in either class.
	type MethodCallCandidate struct {
		Class          string
		InitMethod     string
		RegisterMethod string
		Count          int
	}

	var candidates []MethodCallCandidate
	provider := jvm.NewZipClassProvider(jar)

	classesToScan := []string{blocksClass, blockClass}
	for _, clsName := range classesToScan {
		cf, err := provider.GetClass(clsName)
		if err != nil {
			continue
		}

		for _, method := range cf.Methods {
			var codeAttr *jvm.CodeAttribute
			for _, attr := range method.Attributes {
				if attr.Name == "Code" {
					parsed, err := attr.ParseCode()
					if err == nil {
						codeAttr = parsed
					}
					break
				}
			}
			if codeAttr == nil {
				continue
			}

			methodCalls := make(map[string]int)
			code := codeAttr.Code
			pc := 0
			for pc < len(code) {
				opcode := code[pc]
				if opcode == 0xb8 { // op_invokestatic
					idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
					if int(idx) < len(cf.ConstantPool) {
						val := cf.ConstantPool[idx]
						if mr, ok := val.(jvm.CPMethodRef); ok {
							mClass := cf.ResolveClass(mr.ClassIndex)
							nt := cf.ConstantPool[mr.NameAndTypeIndex].(jvm.CPNameAndType)
							mName := cf.ResolveUtf8(nt.NameIndex)
							mDesc := cf.ResolveUtf8(nt.DescriptorIndex)

							if strings.Contains(mDesc, bestBlockDesc) {
								argsCount, _ := jvm.ParseMethodDescriptor(mDesc)
								if argsCount >= 2 {
									key := mClass + "." + mName
									methodCalls[key]++
								}
							}
						}
					}
				}

				instr := jvm.JavaOpcodes[opcode]
				if instr.Length > 0 {
					pc += instr.Length
				} else {
					pc++
					switch opcode {
					case 0xaa: // tableswitch
						for pc%4 != 0 {
							pc++
						}
						pc += 4
						low := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
						pc += 4
						high := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
						pc += 4
						pc += 4 * int(high-low+1)
					case 0xab: // lookupswitch
						for pc%4 != 0 {
							pc++
						}
						pc += 4
						npairs := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
						pc += 4
						pc += 8 * int(npairs)
					case 0xc4: // wide
						nextOpcode := code[pc]
						pc++
						if nextOpcode == 0x84 {
							pc += 4
						} else {
							pc += 2
						}
					default:
						pc++
					}
				}
			}

			for key, count := range methodCalls {
				if count > 100 {
					dotIdx := strings.LastIndex(key, ".")
					if dotIdx != -1 {
						candidates = append(candidates, MethodCallCandidate{
							Class:          clsName,
							InitMethod:     method.Name,
							RegisterMethod: key[dotIdx+1:],
							Count:          count,
						})
					}
				}
			}
		}
	}

	var bestCandidate *MethodCallCandidate
	for i := range candidates {
		c := &candidates[i]
		regCF, err := provider.GetClass(c.Class)
		if err != nil {
			continue
		}
		var mDesc string
		for _, m := range regCF.Methods {
			if m.Name == c.RegisterMethod {
				mDesc = m.Descriptor
				break
			}
		}
		if mDesc != "" {
			argsCount, _ := jvm.ParseMethodDescriptor(mDesc)
			if argsCount >= 2 {
				bestCandidate = c
				break
			}
		}
	}

	if bestCandidate == nil && len(candidates) > 0 {
		bestCandidate = &candidates[0]
	}

	if bestCandidate == nil {
		return "", "", "", "", fmt.Errorf("failed to detect register method")
	}

	blocksClass = bestCandidate.Class
	registerMethod = bestCandidate.RegisterMethod
	initMethod = bestCandidate.InitMethod

	return blocksClass, blockClass, registerMethod, initMethod, nil
}

type WaterloggedBlocks struct {
	Waterlogged   []string
	Waterloggable []string
}

func FindWaterloggableBlocks(provider jvm.ClassProvider, blocks map[string]*jvm.JVMObject) (WaterloggedBlocks, error) {
	knownWaterloggableIDs := []string{
		"oak_slab",
		"oak_stairs",
		"oak_fence",
		"chest",
		"oak_trapdoor",
	}

	// Helper to collect all interfaces implemented by a class and its hierarchy
	ifaceCache := make(map[string]map[string]bool)
	var collectAllInterfaces func(className string) (map[string]bool, error)
	collectAllInterfaces = func(className string) (map[string]bool, error) {
		if cached, ok := ifaceCache[className]; ok {
			return cached, nil
		}
		interfaces := make(map[string]bool)
		curr := className
		for curr != "" && curr != "java/lang/Object" {
			cf, err := provider.GetClass(curr)
			if err != nil {
				break
			}
			for _, iface := range cf.Interfaces {
				interfaces[iface] = true
				subIfaces, err := collectAllInterfaces(iface)
				if err == nil {
					for sub := range subIfaces {
						interfaces[sub] = true
					}
				}
			}
			curr = cf.SuperClass
		}
		ifaceCache[className] = interfaces
		return interfaces, nil
	}
	var intersection map[string]bool
	for _, id := range knownWaterloggableIDs {
		obj, ok := blocks[id]
		if !ok {
			continue
		}
		ifaces, err := collectAllInterfaces(obj.ClassName)
		if err != nil {
			continue
		}
		if intersection == nil {
			intersection = make(map[string]bool)
			for k := range ifaces {
				intersection[k] = true
			}
		} else {
			for k := range intersection {
				if !ifaces[k] {
					delete(intersection, k)
				}
			}
		}
	}

	// Subtract interfaces implemented by blocks that are NEVER waterloggable (like stone, dirt, oak_planks)
	neverWaterloggableIDs := []string{
		"stone",
		"dirt",
		"oak_planks",
		"lava",
	}
	neverInterfaces := make(map[string]bool)
	for _, id := range neverWaterloggableIDs {
		if obj, ok := blocks[id]; ok {
			ifaces, err := collectAllInterfaces(obj.ClassName)
			if err == nil {
				for k := range ifaces {
					neverInterfaces[k] = true
				}
			}
		}
	}

	for k := range neverInterfaces {
		delete(intersection, k)
	}

	var waterloggedInterface string
	var waterloggableInterface string
	if len(intersection) == 0 {
		return WaterloggedBlocks{}, fmt.Errorf("could not find a common interface amongst known waterloggable blocks")
	} else if len(intersection) == 1 {
		for k := range intersection {
			waterloggedInterface = k
			waterloggableInterface = k
		}
	} else {
		// Prefer the most general (root-most) interface (i.e. one that does not extend any other interface in the set)
		for k := range intersection {
			extendsOther := false
			kIfaces, err := collectAllInterfaces(k)
			if err == nil {
				for other := range intersection {
					if k == other {
						continue
					}
					if kIfaces[other] {
						extendsOther = true
						break
					}
				}
			}
			if !extendsOther {
				waterloggedInterface = k
				break
			}
		}
		if waterloggedInterface == "" {
			for k := range intersection {
				if strings.Contains(strings.ToLower(k), "waterlog") {
					waterloggedInterface = k
					break
				}
			}
		}
		if waterloggedInterface == "" {
			for k := range intersection {
				waterloggedInterface = k
				break
			}
		}

		// Prefer the most specific (leaf-most) interface (i.e. one that is not extended by another interface in the set)
		for k := range intersection {
			isExtended := false
			for other := range intersection {
				if k == other {
					continue
				}
				otherIfaces, err := collectAllInterfaces(other)
				if err == nil && otherIfaces[k] {
					isExtended = true
					break
				}
			}
			if !isExtended {
				waterloggableInterface = k
				break
			}
		}
		if waterloggableInterface == "" {
			for k := range intersection {
				if strings.Contains(strings.ToLower(k), "waterlog") {
					waterloggableInterface = k
					break
				}
			}
		}
		if waterloggableInterface == "" {
			for k := range intersection {
				waterloggableInterface = k
				break
			}
		}
	}

	// fmt.Printf("Detected waterloggedInterface: %s, waterloggableInterface: %s\n", waterloggedInterface, waterloggableInterface)

	var waterloggableBlocks []string
	var waterloggedBlocks []string
	for blockID, obj := range blocks {
		ifaces, err := collectAllInterfaces(obj.ClassName)
		if err != nil {
			continue
		}
		if ifaces[waterloggableInterface] {
			waterloggableBlocks = append(waterloggableBlocks, blockID)
		} else if ifaces[waterloggedInterface] {
			waterloggedBlocks = append(waterloggedBlocks, blockID)
		}
	}

	sort.Strings(waterloggableBlocks)
	sort.Strings(waterloggedBlocks)
	return WaterloggedBlocks{
		Waterlogged:   waterloggedBlocks,
		Waterloggable: waterloggableBlocks,
	}, nil
}
