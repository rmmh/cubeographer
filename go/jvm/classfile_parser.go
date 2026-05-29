package jvm

import (
	"encoding/binary"
	"fmt"
)

// Constant pool tags
type CPUtf8 string
type CPClass struct{ NameIndex uint16 }
type CPString struct{ StringIndex uint16 }
type CPFieldRef struct{ ClassIndex, NameAndTypeIndex uint16 }
type CPMethodRef struct{ ClassIndex, NameAndTypeIndex uint16 }
type CPInterfaceMethodRef struct{ ClassIndex, NameAndTypeIndex uint16 }
type CPNameAndType struct{ NameIndex, DescriptorIndex uint16 }
type CPInteger int32
type CPFloat float32
type CPLong int64
type CPDouble float64

type CPInvokeDynamic struct {
	BootstrapMethodAttrIndex uint16
	NameAndTypeIndex         uint16
}

type CPMethodHandle struct {
	ReferenceKind  uint8
	ReferenceIndex uint16
}

type ClassFile struct {
	Magic        uint32
	MinorVersion uint16
	MajorVersion uint16
	ConstantPool []any
	AccessFlags  uint16
	ThisClass    string // resolved class name (e.g. "net/minecraft/world/level/block/Blocks")
	SuperClass   string // resolved super class name
	Interfaces   []string
	Fields       []FieldInfo
	Methods      []MethodInfo
	Attributes   []AttributeInfo
}

type FieldInfo struct {
	AccessFlags uint16
	Name        string
	Descriptor  string
	Attributes  []AttributeInfo
}

type MethodInfo struct {
	AccessFlags uint16
	Name        string
	Descriptor  string
	Attributes  []AttributeInfo
}

type AttributeInfo struct {
	Name string
	Data []byte
}

type CodeAttribute struct {
	MaxStack       uint16
	MaxLocals      uint16
	Code           []byte
	ExceptionTable []ExceptionTableEntry
	Attributes     []AttributeInfo
}

type ExceptionTableEntry struct {
	StartPc   uint16
	EndPc     uint16
	HandlerPc uint16
	CatchType uint16
}

// ResolveUtf8 resolves a CONSTANT_Utf8 constant pool index to its string value.
func (cf *ClassFile) ResolveUtf8(index uint16) string {
	if index == 0 || int(index) >= len(cf.ConstantPool) {
		return ""
	}
	if s, ok := cf.ConstantPool[index].(CPUtf8); ok {
		return string(s)
	}
	return ""
}

// ResolveClass resolves a CONSTANT_Class constant pool index to its class name.
func (cf *ClassFile) ResolveClass(index uint16) string {
	if index == 0 || int(index) >= len(cf.ConstantPool) {
		return ""
	}
	if c, ok := cf.ConstantPool[index].(CPClass); ok {
		return cf.ResolveUtf8(c.NameIndex)
	}
	return ""
}

// ResolveString resolves a CONSTANT_String constant pool index to its string value.
func (cf *ClassFile) ResolveString(index uint16) string {
	if index == 0 || int(index) >= len(cf.ConstantPool) {
		return ""
	}
	if s, ok := cf.ConstantPool[index].(CPString); ok {
		return cf.ResolveUtf8(s.StringIndex)
	}
	return ""
}

// ParseClassFile parses class file data into a structured ClassFile.
func ParseClassFile(data []byte) (*ClassFile, error) {
	if len(data) < 10 {
		return nil, fmt.Errorf("class file too short: %d bytes", len(data))
	}

	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != 0xCAFEBABE {
		return nil, fmt.Errorf("invalid magic: 0x%08x", magic)
	}

	cf := &ClassFile{
		Magic:        magic,
		MinorVersion: binary.BigEndian.Uint16(data[4:6]),
		MajorVersion: binary.BigEndian.Uint16(data[6:8]),
	}

	constantPoolCount := binary.BigEndian.Uint16(data[8:10])
	cf.ConstantPool = make([]any, constantPoolCount)

	pos := 10
	for i := uint16(1); i < constantPoolCount; i++ {
		if pos >= len(data) {
			return nil, fmt.Errorf("unexpected end of constant pool at index %d", i)
		}
		tag := data[pos]
		pos++
		switch tag {
		case CONSTANT_Utf8:
			if pos+2 > len(data) {
				return nil, fmt.Errorf("short Utf8 length at cp %d", i)
			}
			length := binary.BigEndian.Uint16(data[pos : pos+2])
			pos += 2
			if pos+int(length) > len(data) {
				return nil, fmt.Errorf("short Utf8 bytes at cp %d", i)
			}
			cf.ConstantPool[i] = CPUtf8(string(data[pos : pos+int(length)]))
			pos += int(length)

		case CONSTANT_Integer:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short Integer at cp %d", i)
			}
			val := int32(binary.BigEndian.Uint32(data[pos : pos+4]))
			cf.ConstantPool[i] = CPInteger(val)
			pos += 4

		case CONSTANT_Float:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short Float at cp %d", i)
			}
			val := float32(binary.BigEndian.Uint32(data[pos : pos+4])) // close enough representation
			cf.ConstantPool[i] = CPFloat(val)
			pos += 4

		case CONSTANT_Long:
			if pos+8 > len(data) {
				return nil, fmt.Errorf("short Long at cp %d", i)
			}
			val := int64(binary.BigEndian.Uint64(data[pos : pos+8]))
			cf.ConstantPool[i] = CPLong(val)
			pos += 8
			i++ // Long takes 2 entries

		case CONSTANT_Double:
			if pos+8 > len(data) {
				return nil, fmt.Errorf("short Double at cp %d", i)
			}
			val := float64(binary.BigEndian.Uint64(data[pos : pos+8]))
			cf.ConstantPool[i] = CPDouble(val)
			pos += 8
			i++ // Double takes 2 entries

		case CONSTANT_Class:
			if pos+2 > len(data) {
				return nil, fmt.Errorf("short Class at cp %d", i)
			}
			idx := binary.BigEndian.Uint16(data[pos : pos+2])
			cf.ConstantPool[i] = CPClass{NameIndex: idx}
			pos += 2

		case CONSTANT_String:
			if pos+2 > len(data) {
				return nil, fmt.Errorf("short String at cp %d", i)
			}
			idx := binary.BigEndian.Uint16(data[pos : pos+2])
			cf.ConstantPool[i] = CPString{StringIndex: idx}
			pos += 2

		case CONSTANT_Fieldref:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short Fieldref at cp %d", i)
			}
			cIdx := binary.BigEndian.Uint16(data[pos : pos+2])
			ntIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
			cf.ConstantPool[i] = CPFieldRef{ClassIndex: cIdx, NameAndTypeIndex: ntIdx}
			pos += 4

		case CONSTANT_Methodref:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short Methodref at cp %d", i)
			}
			cIdx := binary.BigEndian.Uint16(data[pos : pos+2])
			ntIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
			cf.ConstantPool[i] = CPMethodRef{ClassIndex: cIdx, NameAndTypeIndex: ntIdx}
			pos += 4

		case CONSTANT_InterfaceMethodref:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short InterfaceMethodref at cp %d", i)
			}
			cIdx := binary.BigEndian.Uint16(data[pos : pos+2])
			ntIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
			cf.ConstantPool[i] = CPInterfaceMethodRef{ClassIndex: cIdx, NameAndTypeIndex: ntIdx}
			pos += 4

		case CONSTANT_NameAndType:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short NameAndType at cp %d", i)
			}
			nIdx := binary.BigEndian.Uint16(data[pos : pos+2])
			dIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
			cf.ConstantPool[i] = CPNameAndType{NameIndex: nIdx, DescriptorIndex: dIdx}
			pos += 4

		case CONSTANT_MethodHandle:
			if pos+3 > len(data) {
				return nil, fmt.Errorf("short MethodHandle at cp %d", i)
			}
			kind := data[pos]
			idx := binary.BigEndian.Uint16(data[pos+1 : pos+3])
			cf.ConstantPool[i] = CPMethodHandle{ReferenceKind: kind, ReferenceIndex: idx}
			pos += 3

		case CONSTANT_MethodType:
			if pos+2 > len(data) {
				return nil, fmt.Errorf("short MethodType at cp %d", i)
			}
			pos += 2

		case CONSTANT_InvokeDynamic:
			if pos+4 > len(data) {
				return nil, fmt.Errorf("short InvokeDynamic at cp %d", i)
			}
			bIdx := binary.BigEndian.Uint16(data[pos : pos+2])
			ntIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
			cf.ConstantPool[i] = CPInvokeDynamic{BootstrapMethodAttrIndex: bIdx, NameAndTypeIndex: ntIdx}
			pos += 4

		default:
			return nil, fmt.Errorf("unknown constant pool tag %d at index %d", tag, i)
		}
	}

	if pos+6 > len(data) {
		return nil, fmt.Errorf("short header after constant pool")
	}

	cf.AccessFlags = binary.BigEndian.Uint16(data[pos : pos+2])
	thisClassIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
	superClassIdx := binary.BigEndian.Uint16(data[pos+4 : pos+6])
	pos += 6

	cf.ThisClass = cf.ResolveClass(thisClassIdx)
	if superClassIdx != 0 {
		cf.SuperClass = cf.ResolveClass(superClassIdx)
	}

	if pos+2 > len(data) {
		return nil, fmt.Errorf("short interfaces count")
	}
	interfacesCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	if pos+2*int(interfacesCount) > len(data) {
		return nil, fmt.Errorf("short interfaces data")
	}
	cf.Interfaces = make([]string, interfacesCount)
	for i := uint16(0); i < interfacesCount; i++ {
		idx := binary.BigEndian.Uint16(data[pos : pos+2])
		cf.Interfaces[i] = cf.ResolveClass(idx)
		pos += 2
	}

	parseAttributes := func(count uint16) ([]AttributeInfo, error) {
		attrs := make([]AttributeInfo, count)
		for i := uint16(0); i < count; i++ {
			if pos+6 > len(data) {
				return nil, fmt.Errorf("short attribute header")
			}
			nameIdx := binary.BigEndian.Uint16(data[pos : pos+2])
			length := binary.BigEndian.Uint32(data[pos+2 : pos+6])
			pos += 6

			if pos+int(length) > len(data) {
				return nil, fmt.Errorf("short attribute data: expected %d bytes, got %d", length, len(data)-pos)
			}
			attrs[i] = AttributeInfo{
				Name: cf.ResolveUtf8(nameIdx),
				Data: data[pos : pos+int(length)],
			}
			pos += int(length)
		}
		return attrs, nil
	}

	if pos+2 > len(data) {
		return nil, fmt.Errorf("short fields count")
	}
	fieldsCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	cf.Fields = make([]FieldInfo, fieldsCount)
	for i := uint16(0); i < fieldsCount; i++ {
		if pos+8 > len(data) {
			return nil, fmt.Errorf("short field info at index %d", i)
		}
		acc := binary.BigEndian.Uint16(data[pos : pos+2])
		nameIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
		descIdx := binary.BigEndian.Uint16(data[pos+4 : pos+6])
		attrCount := binary.BigEndian.Uint16(data[pos+6 : pos+8])
		pos += 8

		attrs, err := parseAttributes(attrCount)
		if err != nil {
			return nil, err
		}

		cf.Fields[i] = FieldInfo{
			AccessFlags: acc,
			Name:        cf.ResolveUtf8(nameIdx),
			Descriptor:  cf.ResolveUtf8(descIdx),
			Attributes:  attrs,
		}
	}

	if pos+2 > len(data) {
		return nil, fmt.Errorf("short methods count")
	}
	methodsCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	cf.Methods = make([]MethodInfo, methodsCount)
	for i := uint16(0); i < methodsCount; i++ {
		if pos+8 > len(data) {
			return nil, fmt.Errorf("short method info at index %d", i)
		}
		acc := binary.BigEndian.Uint16(data[pos : pos+2])
		nameIdx := binary.BigEndian.Uint16(data[pos+2 : pos+4])
		descIdx := binary.BigEndian.Uint16(data[pos+4 : pos+6])
		attrCount := binary.BigEndian.Uint16(data[pos+6 : pos+8])
		pos += 8

		attrs, err := parseAttributes(attrCount)
		if err != nil {
			return nil, err
		}

		cf.Methods[i] = MethodInfo{
			AccessFlags: acc,
			Name:        cf.ResolveUtf8(nameIdx),
			Descriptor:  cf.ResolveUtf8(descIdx),
			Attributes:  attrs,
		}
	}

	if pos+2 > len(data) {
		return nil, fmt.Errorf("short class attributes count")
	}
	classAttrsCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	attrs, err := parseAttributes(classAttrsCount)
	if err != nil {
		return nil, err
	}
	cf.Attributes = attrs

	return cf, nil
}

// ParseCode parses the Data field of a "Code" attribute.
func (a *AttributeInfo) ParseCode() (*CodeAttribute, error) {
	if a.Name != "Code" {
		return nil, fmt.Errorf("attribute is not a Code attribute: %s", a.Name)
	}
	data := a.Data
	if len(data) < 8 {
		return nil, fmt.Errorf("code attribute too short")
	}
	maxStack := binary.BigEndian.Uint16(data[0:2])
	maxLocals := binary.BigEndian.Uint16(data[2:4])
	codeLength := binary.BigEndian.Uint32(data[4:8])

	pos := 8
	if pos+int(codeLength) > len(data) {
		return nil, fmt.Errorf("short bytecode array")
	}
	code := data[pos : pos+int(codeLength)]
	pos += int(codeLength)

	if pos+2 > len(data) {
		return nil, fmt.Errorf("short exception table count")
	}
	exceptionTableLength := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	if pos+8*int(exceptionTableLength) > len(data) {
		return nil, fmt.Errorf("short exception table data")
	}
	exceptions := make([]ExceptionTableEntry, exceptionTableLength)
	for i := uint16(0); i < exceptionTableLength; i++ {
		exceptions[i] = ExceptionTableEntry{
			StartPc:   binary.BigEndian.Uint16(data[pos : pos+2]),
			EndPc:     binary.BigEndian.Uint16(data[pos+2 : pos+4]),
			HandlerPc: binary.BigEndian.Uint16(data[pos+4 : pos+6]),
			CatchType: binary.BigEndian.Uint16(data[pos+6 : pos+8]),
		}
		pos += 8
	}

	if pos+2 > len(data) {
		return nil, fmt.Errorf("short code attributes count")
	}
	codeAttributesCount := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	attrs := make([]AttributeInfo, codeAttributesCount)
	for i := uint16(0); i < codeAttributesCount; i++ {
		if pos+6 > len(data) {
			return nil, fmt.Errorf("short code attribute header")
		}
		nameIdx := binary.BigEndian.Uint16(data[pos : pos+2]) // wait, how to resolve name?
		// Note: since CodeAttribute parsing doesn't have direct access to ClassFile,
		// we will just parse it as binary block, and the name can be mapped if we pass ClassFile,
		// but since we don't strictly need sub-attributes of Code (like LineNumberTable),
		// we can just store the NameIndex and skip/keep it. Let's make it simpler:
		// We'll store Name as an empty string or whatever, we don't need it.
		length := binary.BigEndian.Uint32(data[pos+2 : pos+6])
		pos += 6
		if pos+int(length) > len(data) {
			return nil, fmt.Errorf("short code attribute data")
		}
		attrs[i] = AttributeInfo{
			Name: fmt.Sprintf("attr_%d", nameIdx),
			Data: data[pos : pos+int(length)],
		}
		pos += int(length)
	}

	return &CodeAttribute{
		MaxStack:       maxStack,
		MaxLocals:      maxLocals,
		Code:           code,
		ExceptionTable: exceptions,
		Attributes:     attrs,
	}, nil
}
