package jvm

import (
	"archive/zip"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

const debug = false

type JVMValue any

type JVMInt int32
type JVMLong int64
type JVMFloat float32
type JVMDouble float64
type JVMString string
type JVMNull struct{}

type JVMObject struct {
	ClassName string
	Fields    map[string]JVMValue
}

func (o *JVMObject) String() string {
	return fmt.Sprintf("JVMObject<%s>", o.ClassName)
}

type ClassProvider interface {
	GetClass(className string) (*ClassFile, error)
}

type ZipClassProvider struct {
	Zip   *zip.ReadCloser
	files map[string]*zip.File
	cache map[string]*ClassFile
}

func NewZipClassProvider(z *zip.ReadCloser) *ZipClassProvider {
	files := make(map[string]*zip.File)
	for _, f := range z.File {
		files[f.Name] = f
	}
	return &ZipClassProvider{Zip: z, files: files, cache: make(map[string]*ClassFile)}
}

func (p *ZipClassProvider) GetClass(className string) (*ClassFile, error) {
	if cached, ok := p.cache[className]; ok {
		return cached, nil
	}
	path := className + ".class"
	f, ok := p.files[path]
	if !ok {
		return nil, fmt.Errorf("class not found: %s", className)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	cf, err := ParseClassFile(data)
	if err != nil {
		return nil, err
	}
	p.cache[className] = cf
	return cf, nil
}

type Interpreter struct {
	Provider     ClassProvider
	StaticFields map[string]JVMValue
	Blocks       map[string]*JVMObject // maps block ID (e.g. "minecraft:big_dripleaf") to its JVMObject block representation

	// Dynamic configuration for obfuscated environments
	BlocksClassName    string
	BlockClassName     string
	RegisterMethodName string
}

func NewInterpreter(provider ClassProvider) *Interpreter {
	return &Interpreter{
		Provider:           provider,
		StaticFields:       make(map[string]JVMValue),
		Blocks:             make(map[string]*JVMObject),
		BlocksClassName:    "net/minecraft/world/level/block/Blocks",
		BlockClassName:     "net/minecraft/world/level/block/Block",
		RegisterMethodName: "register",
	}
}

func (interp *Interpreter) IsSubclassOf(className, superClassName string) bool {
	if className == superClassName {
		return true
	}
	curr := className
	for curr != "" && curr != "java/lang/Object" {
		if curr == superClassName {
			return true
		}
		cf, err := interp.Provider.GetClass(curr)
		if err != nil {
			break
		}
		curr = cf.SuperClass
	}
	// Fallback for tests/unresolvable types
	return strings.HasSuffix(className, "Block")
}

// ParseMethodDescriptor parses a Java method descriptor and returns the number of parameter slots/arguments
// and whether it has a return value.
func ParseMethodDescriptor(desc string) (int, bool) {
	if !strings.HasPrefix(desc, "(") {
		return 0, false
	}
	end := strings.Index(desc, ")")
	if end == -1 {
		return 0, false
	}
	paramsPart := desc[1:end]
	retPart := desc[end+1:]

	argCount := 0
	inClass := false
	for i := 0; i < len(paramsPart); i++ {
		c := paramsPart[i]
		if inClass {
			if c == ';' {
				inClass = false
			}
			continue
		}
		if c == 'L' {
			inClass = true
			argCount++
		} else if c == '[' {
			// Array type prefix, does not count as a separate argument
			continue
		} else {
			// Primitive types: B, C, D, F, I, J, S, Z
			argCount++
			if c == 'D' || c == 'J' {
				// Double and Long occupy two slots in local variables, but we count them as 1 argument value here
			}
		}
	}
	hasReturn := retPart != "V"
	return argCount, hasReturn
}

func (interp *Interpreter) InterpretClassClinit(className string) error {
	methodName, err := interp.FindRegistryMethod(className)
	if err != nil {
		methodName = "<clinit>"
	}
	return interp.InterpretClassMethod(className, methodName)
}

func (interp *Interpreter) InterpretClassMethod(className string, methodName string) error {
	cf, err := interp.Provider.GetClass(className)
	if err != nil {
		return err
	}

	var method *MethodInfo
	// First look for a static void no-argument method ()V
	for _, m := range cf.Methods {
		if m.Name == methodName && m.Descriptor == "()V" {
			method = &m
			break
		}
	}
	// Fallback to name-only match
	if method == nil {
		for _, m := range cf.Methods {
			if m.Name == methodName {
				method = &m
				break
			}
		}
	}

	if method == nil {
		return fmt.Errorf("no method %s found in class %s", methodName, className)
	}

	var codeAttr *CodeAttribute
	for _, attr := range method.Attributes {
		if attr.Name == "Code" {
			parsed, err := attr.ParseCode()
			if err != nil {
				return fmt.Errorf("failed to parse Code attribute: %v", err)
			}
			codeAttr = parsed
			break
		}
	}

	if codeAttr == nil {
		return fmt.Errorf("no Code attribute in %s", methodName)
	}

	return interp.Execute(cf, codeAttr.Code)
}

func (interp *Interpreter) Execute(cf *ClassFile, code []byte) error {
	_, err := interp.ExecuteWithLocals(cf, code, nil)
	return err
}

func (interp *Interpreter) ExecuteWithLocals(cf *ClassFile, code []byte, initialLocals []JVMValue) (JVMValue, error) {
	stack := []JVMValue{}
	locals := make([]JVMValue, 256)
	copy(locals, initialLocals)

	push := func(v JVMValue) {
		stack = append(stack, v)
	}

	pop := func() JVMValue {
		if len(stack) == 0 {
			return JVMNull{}
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}

	peek := func() JVMValue {
		if len(stack) == 0 {
			return JVMNull{}
		}
		return stack[len(stack)-1]
	}

	pc := 0
	instructionCount := 0
	for pc < len(code) {
		instructionCount++
		if instructionCount > 100000 {
			return JVMNull{}, fmt.Errorf("interpreter executed too many instructions (> 100,000), aborting to prevent infinite loop")
		}
		opcode := code[pc]
		if debug {
			fmt.Printf("DEBUG pc %04x: %s | Stack: %+v | Locals: %+v\n", pc, JavaOpcodes[opcode].Name, stack, locals[:4])
		}

		switch opcode {
		case op_nop:
			pc++

		case op_aconst_null:
			push(JVMNull{})
			pc++

		case op_iconst_m1, op_iconst_0, op_iconst_1, op_iconst_2, op_iconst_3, op_iconst_4, op_iconst_5:
			push(JVMInt(int32(opcode) - 3))
			pc++

		case op_lconst_0, op_lconst_1:
			push(JVMLong(int64(opcode) - 9))
			pc++

		case op_fconst_0, op_fconst_1, op_fconst_2:
			push(JVMFloat(float32(opcode) - 11))
			pc++

		case op_dconst_0, op_dconst_1:
			push(JVMDouble(float64(opcode) - 14))
			pc++

		case op_bipush:
			val := int8(code[pc+1])
			push(JVMInt(int32(val)))
			pc += 2

		case op_sipush:
			val := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			push(JVMInt(int32(val)))
			pc += 3

		case op_ldc:
			idx := code[pc+1]
			val := cf.ConstantPool[idx]
			if cstr, ok := val.(CPString); ok {
				push(JVMString(cf.ResolveUtf8(cstr.StringIndex)))
			} else if cint, ok := val.(CPInteger); ok {
				push(JVMInt(cint))
			} else if cfloat, ok := val.(CPFloat); ok {
				push(JVMFloat(cfloat))
			} else if cclass, ok := val.(CPClass); ok {
				push(JVMString(cf.ResolveUtf8(cclass.NameIndex)))
			} else {
				push(JVMNull{})
			}
			pc += 2

		case op_ldc_w, op_ldc2_w:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			val := cf.ConstantPool[idx]
			if cstr, ok := val.(CPString); ok {
				push(JVMString(cf.ResolveUtf8(cstr.StringIndex)))
			} else if cint, ok := val.(CPInteger); ok {
				push(JVMInt(cint))
			} else if cfloat, ok := val.(CPFloat); ok {
				push(JVMFloat(cfloat))
			} else if clong, ok := val.(CPLong); ok {
				push(JVMLong(clong))
			} else if cdouble, ok := val.(CPDouble); ok {
				push(JVMDouble(cdouble))
			} else if cclass, ok := val.(CPClass); ok {
				push(JVMString(cf.ResolveUtf8(cclass.NameIndex)))
			} else {
				push(JVMNull{})
			}
			pc += 3

		case op_iload, op_lload, op_fload, op_dload, op_aload:
			idx := code[pc+1]
			push(locals[idx])
			pc += 2

		case op_iload_0, op_iload_1, op_iload_2, op_iload_3:
			push(locals[opcode-op_iload_0])
			pc++

		case op_lload_0, op_lload_1, op_lload_2, op_lload_3:
			push(locals[opcode-op_lload_0])
			pc++

		case op_fload_0, op_fload_1, op_fload_2, op_fload_3:
			push(locals[opcode-op_fload_0])
			pc++

		case op_dload_0, op_dload_1, op_dload_2, op_dload_3:
			push(locals[opcode-op_dload_0])
			pc++

		case op_aload_0, op_aload_1, op_aload_2, op_aload_3:
			push(locals[opcode-op_aload_0])
			pc++

		case op_aaload:
			pop()        // index
			arr := pop() // arrayref
			if obj, ok := arr.(*JVMObject); ok && obj.ClassName == "array" {
				// Return a mock element or null
				push(JVMNull{})
			} else {
				push(JVMNull{})
			}
			pc++

		case op_istore, op_lstore, op_fstore, op_dstore, op_astore:
			idx := code[pc+1]
			locals[idx] = pop()
			pc += 2

		case op_istore_0, op_istore_1, op_istore_2, op_istore_3:
			locals[opcode-op_istore_0] = pop()
			pc++

		case op_lstore_0, op_lstore_1, op_lstore_2, op_lstore_3:
			locals[opcode-op_lstore_0] = pop()
			pc++

		case op_fstore_0, op_fstore_1, op_fstore_2, op_fstore_3:
			locals[opcode-op_fstore_0] = pop()
			pc++

		case op_dstore_0, op_dstore_1, op_dstore_2, op_dstore_3:
			locals[opcode-op_dstore_0] = pop()
			pc++

		case op_astore_0, op_astore_1, op_astore_2, op_astore_3:
			locals[opcode-op_astore_0] = pop()
			pc++

		case op_aastore:
			pop() // value
			pop() // index
			pop() // arrayref
			pc++

		case op_pop:
			pop()
			pc++

		case op_pop2:
			pop()
			pc++

		case op_dup:
			v := peek()
			push(v)
			pc++

		case op_dup_x1:
			v1 := pop()
			v2 := pop()
			push(v1)
			push(v2)
			push(v1)
			pc++

		case op_dup_x2:
			v1 := pop()
			v2 := pop()
			v3 := pop()
			push(v1)
			push(v3)
			push(v2)
			push(v1)
			pc++

		case op_dup2:
			v1 := pop()
			v2 := pop()
			push(v2)
			push(v1)
			push(v2)
			push(v1)
			pc++

		case op_swap:
			v1 := pop()
			v2 := pop()
			push(v1)
			push(v2)
			pc++

		case op_iadd, op_ladd, op_fadd, op_dadd, op_isub, op_lsub, op_fsub, op_dsub, op_imul, op_lmul, op_fmul, op_dmul, op_idiv, op_ldiv, op_fdiv, op_ddiv:
			// Arithmetic: pop 2, perform simple mock push
			pop()
			v1 := pop()
			push(v1)
			pc++

		case op_iinc:
			pc += 3

		case op_i2l, op_i2f, op_i2d, op_l2i, op_l2f, op_l2d, op_f2i, op_f2l, op_f2d, op_d2i, op_d2l, op_d2f, op_i2b, op_i2c, op_i2s:
			// Type conversions: pop and keep top
			v := pop()
			push(v)
			pc++

		case op_ifeq, op_ifne, op_iflt, op_ifge, op_ifgt, op_ifle:
			offset := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			val := pop()
			v := getInt(val)
			jump := false
			switch opcode {
			case op_ifeq:
				jump = v == 0
			case op_ifne:
				jump = v != 0
			case op_iflt:
				jump = v < 0
			case op_ifge:
				jump = v >= 0
			case op_ifgt:
				jump = v > 0
			case op_ifle:
				jump = v <= 0
			}
			if jump {
				pc += int(offset)
			} else {
				pc += 3
			}

		case op_if_icmpeq, op_if_icmpne, op_if_icmplt, op_if_icmpge, op_if_icmpgt, op_if_icmple, op_if_acmpeq, op_if_acmpne:
			offset := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			v2 := pop()
			v1 := pop()
			jump := false
			switch opcode {
			case op_if_icmpeq:
				jump = getInt(v1) == getInt(v2)
			case op_if_icmpne:
				jump = getInt(v1) != getInt(v2)
			case op_if_icmplt:
				jump = getInt(v1) < getInt(v2)
			case op_if_icmpge:
				jump = getInt(v1) >= getInt(v2)
			case op_if_icmpgt:
				jump = getInt(v1) > getInt(v2)
			case op_if_icmple:
				jump = getInt(v1) <= getInt(v2)
			case op_if_acmpeq:
				jump = v1 == v2
			case op_if_acmpne:
				jump = v1 != v2
			}
			if jump {
				pc += int(offset)
			} else {
				pc += 3
			}

		case op_goto:
			offset := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			pc += int(offset)

		case op_tableswitch:
			startPc := pc
			pc++
			for pc%4 != 0 {
				pc++
			}
			defOffset := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
			pc += 4
			low := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
			pc += 4
			high := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
			pc += 4

			val := pop()
			var targetPc int
			if v, ok := val.(JVMInt); ok && int32(v) >= low && int32(v) <= high {
				idx := int32(v) - low
				targetOffset := int32(binary.BigEndian.Uint32(code[pc+int(idx)*4 : pc+int(idx)*4+4]))
				targetPc = startPc + int(targetOffset)
			} else {
				targetPc = startPc + int(defOffset)
			}
			pc = targetPc

		case op_lookupswitch:
			startPc := pc
			pc++
			for pc%4 != 0 {
				pc++
			}
			defOffset := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
			pc += 4
			npairs := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
			pc += 4

			val := pop()
			targetPc := startPc + int(defOffset)
			if v, ok := val.(JVMInt); ok {
				for i := int32(0); i < npairs; i++ {
					match := int32(binary.BigEndian.Uint32(code[pc+int(i)*8 : pc+int(i)*8+4]))
					offset := int32(binary.BigEndian.Uint32(code[pc+int(i)*8+4 : pc+int(i)*8+8]))
					if match == int32(v) {
						targetPc = startPc + int(offset)
						break
					}
				}
			}
			pc = targetPc

		case op_ireturn, op_lreturn, op_freturn, op_dreturn, op_areturn:
			return pop(), nil

		case op_return:
			return JVMNull{}, nil

		case op_getstatic:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			val := cf.ConstantPool[idx]
			if fr, ok := val.(CPFieldRef); ok {
				className := cf.ResolveClass(fr.ClassIndex)
				nt := cf.ConstantPool[fr.NameAndTypeIndex].(CPNameAndType)
				fieldName := cf.ResolveUtf8(nt.NameIndex)
				fieldDesc := cf.ResolveUtf8(nt.DescriptorIndex)

				key := className + "." + fieldName
				if cached, ok := interp.StaticFields[key]; ok {
					push(cached)
				} else {
					// Push a mock static value
					if strings.HasPrefix(fieldDesc, "L") {
						push(&JVMObject{
							ClassName: strings.TrimSuffix(strings.TrimPrefix(fieldDesc, "L"), ";"),
							Fields:    make(map[string]JVMValue),
						})
					} else {
						push(JVMInt(0))
					}
				}
			} else {
				push(JVMNull{})
			}
			pc += 3

		case op_putstatic:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			val := cf.ConstantPool[idx]
			if fr, ok := val.(CPFieldRef); ok {
				className := cf.ResolveClass(fr.ClassIndex)
				nt := cf.ConstantPool[fr.NameAndTypeIndex].(CPNameAndType)
				fieldName := cf.ResolveUtf8(nt.NameIndex)

				key := className + "." + fieldName
				interp.StaticFields[key] = pop()
			} else {
				pop()
			}
			pc += 3

		case op_getfield:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			val := cf.ConstantPool[idx]
			objVal := pop()
			if fr, ok := val.(CPFieldRef); ok {
				nt := cf.ConstantPool[fr.NameAndTypeIndex].(CPNameAndType)
				fieldName := cf.ResolveUtf8(nt.NameIndex)
				fieldDesc := cf.ResolveUtf8(nt.DescriptorIndex)

				if obj, ok := objVal.(*JVMObject); ok && obj.Fields != nil {
					if fVal, ok := obj.Fields[fieldName]; ok {
						push(fVal)
					} else {
						// Mock value
						if strings.HasPrefix(fieldDesc, "L") {
							push(&JVMObject{
								ClassName: strings.TrimSuffix(strings.TrimPrefix(fieldDesc, "L"), ";"),
								Fields:    make(map[string]JVMValue),
							})
						} else {
							push(JVMInt(0))
						}
					}
				} else {
					push(JVMNull{})
				}
			} else {
				push(JVMNull{})
			}
			pc += 3

		case op_putfield:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			val := cf.ConstantPool[idx]
			fVal := pop()
			objVal := pop()
			if fr, ok := val.(CPFieldRef); ok {
				nt := cf.ConstantPool[fr.NameAndTypeIndex].(CPNameAndType)
				fieldName := cf.ResolveUtf8(nt.NameIndex)

				if obj, ok := objVal.(*JVMObject); ok && obj.Fields != nil {
					obj.Fields[fieldName] = fVal
				}
			}
			pc += 3

		case op_invokevirtual, op_invokespecial, op_invokestatic, op_invokeinterface:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			isInterface := opcode == op_invokeinterface
			isStatic := opcode == op_invokestatic

			var methodClass, methodName, methodDesc string
			val := cf.ConstantPool[idx]

			if mr, ok := val.(CPMethodRef); ok {
				methodClass = cf.ResolveClass(mr.ClassIndex)
				nt := cf.ConstantPool[mr.NameAndTypeIndex].(CPNameAndType)
				methodName = cf.ResolveUtf8(nt.NameIndex)
				methodDesc = cf.ResolveUtf8(nt.DescriptorIndex)
			} else if imr, ok := val.(CPInterfaceMethodRef); ok {
				methodClass = cf.ResolveClass(imr.ClassIndex)
				nt := cf.ConstantPool[imr.NameAndTypeIndex].(CPNameAndType)
				methodName = cf.ResolveUtf8(nt.NameIndex)
				methodDesc = cf.ResolveUtf8(nt.DescriptorIndex)
			}

			argCount, hasReturn := ParseMethodDescriptor(methodDesc)
			args := make([]JVMValue, argCount)
			for i := argCount - 1; i >= 0; i-- {
				args[i] = pop()
			}

			var thisObj *JVMObject
			if !isStatic {
				thisVal := pop()
				if obj, ok := thisVal.(*JVMObject); ok {
					thisObj = obj
				}
			}

			if debug && isStatic && len(initialLocals) > 0 {
				fmt.Printf("DEBUG nested invokestatic: class=%q name=%q desc=%q args=%+v\n", methodClass, methodName, methodDesc, args)
			}

			// Perform mock execution logic
			handled := false

			// Custom Constructor Logic
			if methodName == "<init>" && thisObj != nil {
				if strings.Contains(methodClass, "ResourceLocation") {
					if len(args) == 1 {
						thisObj.Fields["path"] = args[0]
					} else if len(args) == 2 {
						thisObj.Fields["path"] = args[1]
					}
				}
				// Store all constructor arguments generically too
				for i, arg := range args {
					thisObj.Fields[fmt.Sprintf("arg_%d", i)] = arg
				}
				handled = true
			}

			// Properties builder logic (e.g. BlockBehaviour$Properties or MapColor etc.)
			if !handled && (strings.Contains(methodClass, "Properties") || strings.Contains(methodClass, "Properties$")) {
				// Builder methods typically return the builder object itself (thisObj)
				if thisObj != nil {
					// Record properties
					if len(args) > 0 {
						thisObj.Fields[methodName] = args[0]
					} else {
						thisObj.Fields[methodName] = true
					}
					push(thisObj)
					handled = true
				}
			}

			// Intercept ResourceLocation static creation:
			if !handled && strings.Contains(methodClass, "ResourceLocation") {
				if (methodName == "parse" || methodName == "of" || methodName == "fromNamespaceAndPath" || strings.Contains(methodName, "read")) && len(args) > 0 {
					var path JVMValue
					if len(args) == 1 {
						path = args[0]
					} else if len(args) == 2 {
						path = args[1] // namespace, path
					}
					push(&JVMObject{
						ClassName: "net/minecraft/resources/ResourceLocation",
						Fields:    map[string]JVMValue{"path": path},
					})
					handled = true
				}
			}

			// Intercept ResourceKey creation:
			if strings.Contains(methodClass, "ResourceKey") {
				if methodName == "create" && len(args) >= 2 {
					var path JVMValue
					if loc, ok := args[1].(*JVMObject); ok {
						path = loc.Fields["path"]
					}
					push(&JVMObject{
						ClassName: "net/minecraft/resources/ResourceKey",
						Fields:    map[string]JVMValue{"path": path},
					})
					handled = true
				}
			}

			// Determine if this is a block registration call or a helper method call
			isBlockRegistration := false
			if isStatic && (methodClass == interp.BlocksClassName || strings.HasSuffix(methodClass, "block/Blocks")) && methodName == interp.RegisterMethodName {
				// Let's check if it's actually a helper method because it passes an already registered block
				isHelper := false
				for _, arg := range args {
					if obj, ok := arg.(*JVMObject); ok {
						if interp.IsSubclassOf(obj.ClassName, interp.BlockClassName) && !strings.Contains(obj.ClassName, "Properties") {
							for _, registered := range interp.Blocks {
								if registered == obj {
									isHelper = true
									break
								}
							}
						}
					}
				}
				if !isHelper {
					isBlockRegistration = true
				}
			}

			// If it's a static helper method in the same class (like registerLegacyStair), execute it recursively!
			if !isBlockRegistration && isStatic && methodClass == cf.ThisClass {
				var targetMethod *MethodInfo
				for _, m := range cf.Methods {
					if m.Name == methodName && m.Descriptor == methodDesc {
						targetMethod = &m
						break
					}
				}
				if targetMethod != nil {
					var codeAttr *CodeAttribute
					for _, attr := range targetMethod.Attributes {
						if attr.Name == "Code" {
							parsed, err := attr.ParseCode()
							if err == nil {
								codeAttr = parsed
							}
							break
						}
					}
					if codeAttr != nil {
						retVal, err := interp.ExecuteWithLocals(cf, codeAttr.Code, args)
						if err == nil {
							if hasReturn {
								push(retVal)
							}
							handled = true
						}
					}
				}
			}

			// Block registration detection!
			if isBlockRegistration {
				var blockID string
				for _, arg := range args {
					if s, ok := arg.(JVMString); ok {
						blockID = string(s)
						break
					}
					if obj, ok := arg.(*JVMObject); ok {
						if pathVal, ok := obj.Fields["path"]; ok {
							if s, ok := pathVal.(JVMString); ok {
								blockID = string(s)
								break
							}
						}
					}
				}

				// Find custom block class or default to Block
				var blockObj *JVMObject
				for _, arg := range args {
					if obj, ok := arg.(*JVMObject); ok {
						if interp.IsSubclassOf(obj.ClassName, interp.BlockClassName) && !strings.Contains(obj.ClassName, "Properties") {
							blockObj = obj
							break
						}
						// Also check if it's a lambda Function wrapping a custom block constructor
						if obj.ClassName == "java/util/function/Function" {
							if fClassVal, ok := obj.Fields["factory_class"]; ok {
								if s, ok := fClassVal.(JVMString); ok && string(s) != "" {
									blockObj = &JVMObject{
										ClassName: string(s),
										Fields:    make(map[string]JVMValue),
									}
									break
								}
							}
						}
					}
				}

				if blockObj == nil {
					blockObj = &JVMObject{
						ClassName: interp.BlockClassName,
						Fields:    make(map[string]JVMValue),
					}
				}

				if blockID != "" {
					interp.Blocks[blockID] = blockObj
				}
				push(blockObj)
				handled = true
			}

			if !handled {
				// Fallback: balance the stack
				if hasReturn {
					// Parse return type from signature
					retPart := methodDesc[strings.Index(methodDesc, ")")+1:]
					if strings.HasPrefix(retPart, "L") {
						className := strings.TrimSuffix(strings.TrimPrefix(retPart, "L"), ";")
						push(&JVMObject{
							ClassName: className,
							Fields:    make(map[string]JVMValue),
						})
					} else if retPart == "D" || retPart == "J" || retPart == "I" || retPart == "Z" || retPart == "F" {
						push(JVMInt(0))
					} else {
						push(JVMNull{})
					}
				}
			}

			if isInterface {
				pc += 5
			} else {
				pc += 3
			}

		case op_invokedynamic:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			val := cf.ConstantPool[idx]

			factoryClassName := ""
			if indy, ok := val.(CPInvokeDynamic); ok {
				// invokedynamic pops its captured arguments from the stack.
				// The descriptor in NameAndType specifies the parameters (captured args).
				if int(indy.NameAndTypeIndex) < len(cf.ConstantPool) {
					if nt, ok := cf.ConstantPool[indy.NameAndTypeIndex].(CPNameAndType); ok {
						desc := cf.ResolveUtf8(nt.DescriptorIndex)
						argCount, _ := ParseMethodDescriptor(desc)
						for i := 0; i < argCount; i++ {
							pop()
						}
					}
				}

				// Find "BootstrapMethods" attribute in cf.Attributes
				var bsAttr *AttributeInfo
				for _, attr := range cf.Attributes {
					if attr.Name == "BootstrapMethods" {
						bsAttr = &attr
						break
					}
				}
				if bsAttr != nil {
					methods, err := ParseBootstrapMethods(bsAttr.Data)
					if err == nil && int(indy.BootstrapMethodAttrIndex) < len(methods) {
						method := methods[indy.BootstrapMethodAttrIndex]
						// Scan Arguments for a MethodHandle constant
						for _, argIdx := range method.Arguments {
							if int(argIdx) < len(cf.ConstantPool) {
								if mh, ok := cf.ConstantPool[argIdx].(CPMethodHandle); ok {
									refIdx := mh.ReferenceIndex
									if int(refIdx) < len(cf.ConstantPool) {
										var helperMethodName, helperMethodDesc string
										if mr, ok := cf.ConstantPool[refIdx].(CPMethodRef); ok {
											factoryClassName = cf.ResolveClass(mr.ClassIndex)
											nt := cf.ConstantPool[mr.NameAndTypeIndex].(CPNameAndType)
											helperMethodName = cf.ResolveUtf8(nt.NameIndex)
											helperMethodDesc = cf.ResolveUtf8(nt.DescriptorIndex)
										} else if imr, ok := cf.ConstantPool[refIdx].(CPInterfaceMethodRef); ok {
											factoryClassName = cf.ResolveClass(imr.ClassIndex)
											nt := cf.ConstantPool[imr.NameAndTypeIndex].(CPNameAndType)
											helperMethodName = cf.ResolveUtf8(nt.NameIndex)
											helperMethodDesc = cf.ResolveUtf8(nt.DescriptorIndex)
										}

										if (factoryClassName == interp.BlocksClassName || strings.HasSuffix(factoryClassName, "block/Blocks")) && helperMethodName != "" {
											resolved := interp.resolveBlockClassFromMethod(cf, helperMethodName, helperMethodDesc)
											if debug {
												fmt.Printf("DEBUG indy blocks match: helper=%q resolved=%q\n", helperMethodName, resolved)
											}
											if resolved != "" {
												factoryClassName = resolved
											}
										}
									}
								}
							}
						}
					}
				}

			}

			push(&JVMObject{
				ClassName: "java/util/function/Function",
				Fields:    map[string]JVMValue{"factory_class": JVMString(factoryClassName)},
			})
			pc += 5

		case op_new:
			idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
			className := cf.ResolveClass(idx)
			push(&JVMObject{
				ClassName: className,
				Fields:    make(map[string]JVMValue),
			})
			pc += 3

		case op_newarray:
			pop() // size
			push(&JVMObject{
				ClassName: "array",
				Fields:    make(map[string]JVMValue),
			})
			pc += 2

		case op_anewarray:
			pop() // size
			push(&JVMObject{
				ClassName: "array",
				Fields:    make(map[string]JVMValue),
			})
			pc += 3

		case op_arraylength:
			pop() // arrayref
			push(JVMInt(0))
			pc++

		case op_checkcast:
			pc += 3

		case op_instanceof:
			pop()           // objectref
			push(JVMInt(1)) // assume true
			pc += 3

		case op_wide:
			nextOpcode := code[pc+1]
			if nextOpcode == op_iinc { // iinc
				pc += 6
			} else {
				// load/store wide
				pc += 4
			}

		case op_multianewarray:
			dims := code[pc+3]
			for i := 0; i < int(dims); i++ {
				pop()
			}
			push(&JVMObject{
				ClassName: "array",
				Fields:    make(map[string]JVMValue),
			})
			pc += 4

		case op_ifnull:
			offset := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			val := pop()
			if _, ok := val.(JVMNull); ok {
				pc += int(offset)
			} else {
				pc += 3
			}

		case op_ifnonnull:
			offset := int16(binary.BigEndian.Uint16(code[pc+1 : pc+3]))
			val := pop()
			if _, ok := val.(JVMNull); !ok {
				pc += int(offset)
			} else {
				pc += 3
			}

		default:
			// Unhandled opcode. To be safe, skip using JavaOpcodes length if known
			instr := JavaOpcodes[opcode]
			if instr.Length > 0 {
				pc += instr.Length
			} else {
				return JVMNull{}, fmt.Errorf("interpreter encountered unhandled variable-length opcode: 0x%02x (%s) at pc %d", opcode, instr.Name, pc)
			}
		}
	}

	return JVMNull{}, nil
}

func getInt(v JVMValue) int32 {
	if i, ok := v.(JVMInt); ok {
		return int32(i)
	}
	if l, ok := v.(JVMLong); ok {
		return int32(l)
	}
	return 0
}

type BootstrapMethod struct {
	MethodRef uint16
	Arguments []uint16
}

func ParseBootstrapMethods(data []byte) ([]BootstrapMethod, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("bootstrap methods attribute too short")
	}
	count := binary.BigEndian.Uint16(data[0:2])
	methods := make([]BootstrapMethod, count)
	pos := 2
	for i := uint16(0); i < count; i++ {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("short bootstrap method header")
		}
		ref := binary.BigEndian.Uint16(data[pos : pos+2])
		argCount := binary.BigEndian.Uint16(data[pos+2 : pos+4])
		pos += 4
		if pos+2*int(argCount) > len(data) {
			return nil, fmt.Errorf("short bootstrap method args")
		}
		args := make([]uint16, argCount)
		for j := uint16(0); j < argCount; j++ {
			args[j] = binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
		}
		methods[i] = BootstrapMethod{MethodRef: ref, Arguments: args}
	}
	return methods, nil
}

func (interp *Interpreter) resolveBlockClassFromMethod(cf *ClassFile, methodName string, methodDesc string) string {
	for _, m := range cf.Methods {
		if m.Name == methodName && (methodDesc == "" || m.Descriptor == methodDesc) {
			for _, attr := range m.Attributes {
				if attr.Name == "Code" {
					codeAttr, err := attr.ParseCode()
					if err == nil {
						code := codeAttr.Code
						for i := 0; i < len(code)-2; i++ {
							if code[i] == op_new {
								idx := binary.BigEndian.Uint16(code[i+1 : i+3])
								if int(idx) < len(cf.ConstantPool) {
									className := cf.ResolveClass(idx)
									if interp.IsSubclassOf(className, interp.BlockClassName) {
										return className
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return ""
}

func (interp *Interpreter) FindRegistryMethod(className string) (string, error) {
	cf, err := interp.Provider.GetClass(className)
	if err != nil {
		return "", err
	}

	bestMethod := ""
	maxScore := 0

	for _, m := range cf.Methods {
		// Method must be static and take no arguments and return void: ()V
		if (m.AccessFlags&0x0008) == 0 || m.Descriptor != "()V" {
			continue
		}

		var codeAttr *CodeAttribute
		for _, attr := range m.Attributes {
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

		score := 0
		code := codeAttr.Code
		pc := 0
		for pc < len(code) {
			opcode := code[pc]
			if opcode == 0xb8 { // op_invokestatic
				idx := binary.BigEndian.Uint16(code[pc+1 : pc+3])
				if int(idx) < len(cf.ConstantPool) {
					val := cf.ConstantPool[idx]
					if mr, ok := val.(CPMethodRef); ok {
						nt := cf.ConstantPool[mr.NameAndTypeIndex].(CPNameAndType)
						mDesc := cf.ResolveUtf8(nt.DescriptorIndex)

						// Check if it's a register-shaped invocation (has at least 2 arguments)
						argsCount, _ := ParseMethodDescriptor(mDesc)
						if argsCount >= 2 {
							score++
						}
					}
				}
			}

			instr := JavaOpcodes[opcode]
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

		if score > maxScore {
			maxScore = score
			bestMethod = m.Name
		}
	}

	if maxScore > 20 {
		return bestMethod, nil
	}

	return "", fmt.Errorf("no registry-shaped method found in class %s", className)
}
