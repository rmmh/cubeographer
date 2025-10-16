package resourcepack

import (
	"encoding/binary"
	"log"
)

const (
	CONSTANT_Utf8               = 1
	CONSTANT_Integer            = 3
	CONSTANT_Float              = 4
	CONSTANT_Long               = 5
	CONSTANT_Double             = 6
	CONSTANT_Class              = 7
	CONSTANT_String             = 8
	CONSTANT_Fieldref           = 9
	CONSTANT_Methodref          = 10
	CONSTANT_InterfaceMethodref = 11
	CONSTANT_NameAndType        = 12
	CONSTANT_MethodHandle       = 15
	CONSTANT_MethodType         = 16
	CONSTANT_InvokeDynamic      = 18
)

var javaOpcodes = []struct {
	Name   string
	Desc   string
	Length int
}{
	0x00: {"nop", "Do nothing", 1},
	0x01: {"aconst_null", "Push null", 1},
	0x02: {"iconst_m1", "Push int constant -1", 1},
	0x03: {"iconst_0", "Push int constant 0", 1},
	0x04: {"iconst_1", "Push int constant 1", 1},
	0x05: {"iconst_2", "Push int constant 2", 1},
	0x06: {"iconst_3", "Push int constant 3", 1},
	0x07: {"iconst_4", "Push int constant 4", 1},
	0x08: {"iconst_5", "Push int constant 5", 1},
	0x09: {"lconst_0", "Push long constant 0", 1},
	0x0a: {"lconst_1", "Push long constant 1", 1},
	0x0b: {"fconst_0", "Push float constant 0.0", 1},
	0x0c: {"fconst_1", "Push float constant 1.0", 1},
	0x0d: {"fconst_2", "Push float constant 2.0", 1},
	0x0e: {"dconst_0", "Push double constant 0.0", 1},
	0x0f: {"dconst_1", "Push double constant 1.0", 1},
	0x10: {"bipush", "Push byte", 2},
	0x11: {"sipush", "Push short", 3},
	0x12: {"ldc", "Push item from run-time constant pool", 2},
	0x13: {"ldc_w", "Push item from run-time constant pool (wide index)", 3},
	0x14: {"ldc2_w", "Push long or double from run-time constant pool (wide index)", 3},
	0x15: {"iload", "Load int from local variable", 2},
	0x16: {"lload", "Load long from local variable", 2},
	0x17: {"fload", "Load float from local variable", 2},
	0x18: {"dload", "Load double from local variable", 2},
	0x19: {"aload", "Load reference from local variable", 2},
	0x1a: {"iload_0", "Load int from local variable", 1},
	0x1b: {"iload_1", "Load int from local variable", 1},
	0x1c: {"iload_2", "Load int from local variable", 1},
	0x1d: {"iload_3", "Load int from local variable", 1},
	0x1e: {"lload_0", "Load long from local variable", 1},
	0x1f: {"lload_1", "Load long from local variable", 1},
	0x20: {"lload_2", "Load long from local variable", 1},
	0x21: {"lload_3", "Load long from local variable", 1},
	0x22: {"fload_0", "Load float from local variable", 1},
	0x23: {"fload_1", "Load float from local variable", 1},
	0x24: {"fload_2", "Load float from local variable", 1},
	0x25: {"fload_3", "Load float from local variable", 1},
	0x26: {"dload_0", "Load double from local variable", 1},
	0x27: {"dload_1", "Load double from local variable", 1},
	0x28: {"dload_2", "Load double from local variable", 1},
	0x29: {"dload_3", "Load double from local variable", 1},
	0x2a: {"aload_0", "Load reference from local variable", 1},
	0x2b: {"aload_1", "Load reference from local variable", 1},
	0x2c: {"aload_2", "Load reference from local variable", 1},
	0x2d: {"aload_3", "Load reference from local variable", 1},
	0x2e: {"iaload", "Load int from array", 1},
	0x2f: {"laload", "Load long from array", 1},
	0x30: {"faload", "Load float from array", 1},
	0x31: {"daload", "Load double from array", 1},
	0x32: {"aaload", "Load reference from array", 1},
	0x33: {"baload", "Load byte or boolean from array", 1},
	0x34: {"caload", "Load char from array", 1},
	0x35: {"saload", "Load short from array", 1},
	0x36: {"istore", "Store int into local variable", 2},
	0x37: {"lstore", "Store long into local variable", 2},
	0x38: {"fstore", "Store float into local variable", 2},
	0x39: {"dstore", "Store double into local variable", 2},
	0x3a: {"astore", "Store reference into local variable", 2},
	0x3b: {"istore_0", "Store int into local variable", 1},
	0x3c: {"istore_1", "Store int into local variable", 1},
	0x3d: {"istore_2", "Store int into local variable", 1},
	0x3e: {"istore_3", "Store int into local variable", 1},
	0x3f: {"lstore_0", "Store long into local variable", 1},
	0x40: {"lstore_1", "Store long into local variable", 1},
	0x41: {"lstore_2", "Store long into local variable", 1},
	0x42: {"lstore_3", "Store long into local variable", 1},
	0x43: {"fstore_0", "Store float into local variable", 1},
	0x44: {"fstore_1", "Store float into local variable", 1},
	0x45: {"fstore_2", "Store float into local variable", 1},
	0x46: {"fstore_3", "Store float into local variable", 1},
	0x47: {"dstore_0", "Store double into local variable", 1},
	0x48: {"dstore_1", "Store double into local variable", 1},
	0x49: {"dstore_2", "Store double into local variable", 1},
	0x4a: {"dstore_3", "Store double into local variable", 1},
	0x4b: {"astore_0", "Store reference into local variable", 1},
	0x4c: {"astore_1", "Store reference into local variable", 1},
	0x4d: {"astore_2", "Store reference into local variable", 1},
	0x4e: {"astore_3", "Store reference into local variable", 1},
	0x4f: {"iastore", "Store into int array", 1},
	0x50: {"lastore", "Store into long array", 1},
	0x51: {"fastore", "Store into float array", 1},
	0x52: {"dastore", "Store into double array", 1},
	0x53: {"aastore", "Store into reference array", 1},
	0x54: {"bastore", "Store into byte or boolean array", 1},
	0x55: {"castore", "Store into char array", 1},
	0x56: {"sastore", "Store into short array", 1},
	0x57: {"pop", "Pop the top operand stack value", 1},
	0x58: {"pop2", "Pop the top one or two operand stack values", 1},
	0x59: {"dup", "Duplicate the top operand stack value", 1},
	0x5a: {"dup_x1", "Duplicate the top operand stack value and insert two values down", 1},
	0x5b: {"dup_x2", "Duplicate the top operand stack value and insert two or three values down", 1},
	0x5c: {"dup2", "Duplicate the top one or two operand stack values", 1},
	0x5d: {"dup2_x1", "Duplicate the top one or two operand stack values and insert two or three values down", 1},
	0x5e: {"dup2_x2", "Duplicate the top one or two operand stack values and insert two, three, or four values down", 1},
	0x5f: {"swap", "Swap the top two operand stack values", 1},
	0x60: {"iadd", "Add int", 1},
	0x61: {"ladd", "Add long", 1},
	0x62: {"fadd", "Add float", 1},
	0x63: {"dadd", "Add double", 1},
	0x64: {"isub", "Subtract int", 1},
	0x65: {"lsub", "Subtract long", 1},
	0x66: {"fsub", "Subtract float", 1},
	0x67: {"dsub", "Subtract double", 1},
	0x68: {"imul", "Multiply int", 1},
	0x69: {"lmul", "Multiply long", 1},
	0x6a: {"fmul", "Multiply float", 1},
	0x6b: {"dmul", "Multiply double", 1},
	0x6c: {"idiv", "Divide int", 1},
	0x6d: {"ldiv", "Divide long", 1},
	0x6e: {"fdiv", "Divide float", 1},
	0x6f: {"ddiv", "Divide double", 1},
	0x70: {"irem", "Remainder int", 1},
	0x71: {"lrem", "Remainder long", 1},
	0x72: {"frem", "Remainder float", 1},
	0x73: {"drem", "Remainder double", 1},
	0x74: {"ineg", "Negate int", 1},
	0x75: {"lneg", "Negate long", 1},
	0x76: {"fneg", "Negate float", 1},
	0x77: {"dneg", "Negate double", 1},
	0x78: {"ishl", "Shift left int", 1},
	0x79: {"lshl", "Shift left long", 1},
	0x7a: {"ishr", "Arithmetic shift right int", 1},
	0x7b: {"lshr", "Arithmetic shift right long", 1},
	0x7c: {"iushr", "Logical shift right int", 1},
	0x7d: {"lushr", "Logical shift right long", 1},
	0x7e: {"iand", "Boolean AND int", 1},
	0x7f: {"land", "Boolean AND long", 1},
	0x80: {"ior", "Boolean OR int", 1},
	0x81: {"lor", "Boolean OR long", 1},
	0x82: {"ixor", "Boolean XOR int", 1},
	0x83: {"lxor", "Boolean XOR long", 1},
	0x84: {"iinc", "Increment local variable by constant", 3},
	0x85: {"i2l", "Convert int to long", 1},
	0x86: {"i2f", "Convert int to float", 1},
	0x87: {"i2d", "Convert int to double", 1},
	0x88: {"l2i", "Convert long to int", 1},
	0x89: {"l2f", "Convert long to float", 1},
	0x8a: {"l2d", "Convert long to double", 1},
	0x8b: {"f2i", "Convert float to int", 1},
	0x8c: {"f2l", "Convert float to long", 1},
	0x8d: {"f2d", "Convert float to double", 1},
	0x8e: {"d2i", "Convert double to int", 1},
	0x8f: {"d2l", "Convert double to long", 1},
	0x90: {"d2f", "Convert double to float", 1},
	0x91: {"i2b", "Convert int to byte", 1},
	0x92: {"i2c", "Convert int to char", 1},
	0x93: {"i2s", "Convert int to short", 1},
	0x94: {"lcmp", "Compare long", 1},
	0x95: {"fcmpl", "Compare float", 1},
	0x96: {"fcmpg", "Compare float", 1},
	0x97: {"dcmpl", "Compare double", 1},
	0x98: {"dcmpg", "Compare double", 1},
	0x99: {"ifeq", "Branch if int comparison with zero succeeds", 3},
	0x9a: {"ifne", "Branch if int comparison with zero succeeds", 3},
	0x9b: {"iflt", "Branch if int comparison with zero succeeds", 3},
	0x9c: {"ifge", "Branch if int comparison with zero succeeds", 3},
	0x9d: {"ifgt", "Branch if int comparison with zero succeeds", 3},
	0x9e: {"ifle", "Branch if int comparison with zero succeeds", 3},
	0x9f: {"if_icmpeq", "Branch if int comparison succeeds", 3},
	0xa0: {"if_icmpne", "Branch if int comparison succeeds", 3},
	0xa1: {"if_icmplt", "Branch if int comparison succeeds", 3},
	0xa2: {"if_icmpge", "Branch if int comparison succeeds", 3},
	0xa3: {"if_icmpgt", "Branch if int comparison succeeds", 3},
	0xa4: {"if_icmple", "Branch if int comparison succeeds", 3},
	0xa5: {"if_acmpeq", "Branch if reference comparison succeeds", 3},
	0xa6: {"if_acmpne", "Branch if reference comparison succeeds", 3},
	0xa7: {"goto", "Branch always", 3},
	0xa8: {"jsr", "Jump subroutine", 3},
	0xa9: {"ret", "Return from subroutine", 2},
	0xaa: {"tableswitch", "Access jump table by index and jump", 0},
	0xab: {"lookupswitch", "Access jump table by key match and jump", 0},
	0xac: {"ireturn", "Return int from method", 1},
	0xad: {"lreturn", "Return long from method", 1},
	0xae: {"freturn", "Return float from method", 1},
	0xaf: {"dreturn", "Return double from method", 1},
	0xb0: {"areturn", "Return reference from method", 1},
	0xb1: {"return", "Return void from method", 1},
	0xb2: {"getstatic", "Get static field from class", 3},
	0xb3: {"putstatic", "Set static field in class", 3},
	0xb4: {"getfield", "Fetch field from object", 3},
	0xb5: {"putfield", "Set field in object", 3},
	0xb6: {"invokevirtual", "Invoke instance method; dispatch based on class", 3},
	0xb7: {"invokespecial", "Invoke instance method; direct invocation", 3},
	0xb8: {"invokestatic", "Invoke a class (static) method", 3},
	0xb9: {"invokeinterface", "Invoke interface method", 5},
	0xba: {"invokedynamic", "Invoke a dynamically-computed call site", 5},
	0xbb: {"new", "Create new object", 3},
	0xbc: {"newarray", "Create new array", 2},
	0xbd: {"anewarray", "Create new array of reference", 3},
	0xbe: {"arraylength", "Get length of array", 1},
	0xbf: {"athrow", "Throw exception or error", 1},
	0xc0: {"checkcast", "Check whether object is of given type", 3},
	0xc1: {"instanceof", "Determine if object is of given type", 3},
	0xc2: {"monitorenter", "Enter monitor for object", 1},
	0xc3: {"monitorexit", "Exit monitor for object", 1},
	0xc4: {"wide", "Extend local variable index by additional bytes", 0}, // Special handling
	0xc5: {"multianewarray", "Create new multidimensional array", 4},
	0xc6: {"ifnull", "Branch if reference is null", 3},
	0xc7: {"ifnonnull", "Branch if reference not null", 3},
	0xc8: {"goto_w", "Branch always (wide index)", 5},
	0xc9: {"jsr_w", "Jump subroutine (wide index)", 5},
	0xca: {"breakpoint", "Reserved for debuggers", 1},
	0xfe: {"impdep1", "Reserved for implementation-dependent operations", 1},
	0xff: {"impdep2", "Reserved for implementation-dependent operations", 1},
}

// Walk a Java class file, and increment stringCounts every time a given string constant
// is referenced in the bytecode.
func walkClassForCounts(data []byte, stringCounts map[string]int) {
	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != 0xCAFEBABE {
		log.Fatalf("Not a class file")
	}
	// minorVersion := binary.BigEndian.Uint16(data[4:6])
	// majorVersion := binary.BigEndian.Uint16(data[6:8])
	constantPoolCount := binary.BigEndian.Uint16(data[8:10])

	constantPool := make([]interface{}, constantPoolCount)

	pos := 10
	for i := uint16(1); i < constantPoolCount; i++ {
		tag := data[pos]
		pos++
		switch tag {
		case CONSTANT_Utf8:
			length := binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
			bytes := data[pos : pos+int(length)]
			pos += int(length)
			constantPool[i] = string(bytes)
		case CONSTANT_String:
			index := binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
			constantPool[i] = index
		case CONSTANT_Integer, CONSTANT_Float, CONSTANT_Fieldref, CONSTANT_Methodref, CONSTANT_InterfaceMethodref, CONSTANT_NameAndType, CONSTANT_InvokeDynamic:
			pos += 4
		case CONSTANT_Long, CONSTANT_Double:
			pos += 8
			i++ // Long and Double take two constant pool entries.
		case CONSTANT_Class, CONSTANT_MethodType:
			pos += 2
		case CONSTANT_MethodHandle:
			pos += 3
		default:
			log.Fatalf("Unknown tag: %d", tag)
		}
	}

	// accessFlags := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	// thisClass := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	// superClass := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	interfacesCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	pos += 2 * int(interfacesCount)

	fieldsCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	for i := uint16(0); i < fieldsCount; i++ {
		// accessFlags := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		// nameIndex := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		// descriptorIndex := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		attributesCount := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		for j := uint16(0); j < attributesCount; j++ {
			// attributeNameIndex := binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
			attributeLength := binary.BigEndian.Uint32(data[pos : pos+4])
			pos += 4
			pos += int(attributeLength)
		}
	}

	methodsCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	for i := uint16(0); i < methodsCount; i++ {
		// accessFlags := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		// nameIndex := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		// descriptorIndex := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		attributesCount := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2
		for j := uint16(0); j < attributesCount; j++ {
			attributeNameIndex := binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
			attributeLength := binary.BigEndian.Uint32(data[pos : pos+4])
			pos += 4
			attributeStartPos := pos
			if constantPool[attributeNameIndex] == "Code" {
				// maxStack := binary.BigEndian.Uint16(data[pos : pos+2])
				pos += 2
				// maxLocals := binary.BigEndian.Uint16(data[pos : pos+2])
				pos += 2
				codeLength := binary.BigEndian.Uint32(data[pos : pos+4])
				pos += 4
				code := data[pos : pos+int(codeLength)]

				for pc := 0; pc < len(code); {
					opcode := code[pc]

					if int(opcode) >= len(javaOpcodes) || javaOpcodes[opcode].Name == "" {
						log.Fatalf("Unknown opcode: 0x%x at pc %d", opcode, pc)
					}

					instr := javaOpcodes[opcode]
					// fmt.Printf("PC: %04x | Opcode: 0x%x (%s)\n", pc, opcode, instr.Name)

					switch opcode {
					case 0x12: // ldc
						index := uint16(code[pc+1])
						if s, ok := constantPool[index].(uint16); ok {
							if str, ok := constantPool[s].(string); ok {
								stringCounts[str]++
							}
						}
					case 0x13: // ldc_w
						index := binary.BigEndian.Uint16(code[pc+1 : pc+3])
						if s, ok := constantPool[index].(uint16); ok {
							if str, ok := constantPool[s].(string); ok {
								stringCounts[str]++
							}
						}
					}

					if instr.Length > 0 {
						pc += instr.Length
					} else {
						pc++ // Move past the opcode byte itself.
						switch opcode {
						case 0xaa: // tableswitch
							// Skip padding bytes to align to a 4-byte boundary.
							for pc%4 != 0 {
								pc++
							}
							pc += 4 // default
							low := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
							pc += 4
							high := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
							pc += 4
							pc += 4 * int(high-low+1)
						case 0xab: // lookupswitch
							// Skip padding bytes to align to a 4-byte boundary.
							for pc%4 != 0 {
								pc++
							}
							pc += 4 // default
							npairs := int32(binary.BigEndian.Uint32(code[pc : pc+4]))
							pc += 4
							pc += 8 * int(npairs)
						case 0xc4: // wide
							nextOpcode := code[pc]
							pc++
							if nextOpcode == 0x84 { // iinc
								pc += 4
							} else {
								pc += 2
							}
						default:
							log.Fatalf("Unhandled variable-length opcode %s (0x%x)", instr.Name, opcode)
						}
					}
				}
				pos += int(codeLength)

				exceptionTableLength := binary.BigEndian.Uint16(data[pos : pos+2])
				pos += 2
				pos += 8 * int(exceptionTableLength)
				codeAttributesCount := binary.BigEndian.Uint16(data[pos : pos+2])
				pos += 2
				for k := uint16(0); k < codeAttributesCount; k++ {
					// attributeNameIndex := binary.BigEndian.Uint16(data[pos : pos+2])
					pos += 2
					codeAttributeLength := binary.BigEndian.Uint32(data[pos : pos+4])
					pos += 4
					pos += int(codeAttributeLength)
				}
			} else {
				pos = attributeStartPos + int(attributeLength)
			}
		}
	}
}
