package jvm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type MockClassProvider struct {
	Classes map[string]*ClassFile
}

func (p *MockClassProvider) GetClass(className string) (*ClassFile, error) {
	cf, ok := p.Classes[className]
	if !ok {
		return &ClassFile{
			ThisClass:    className,
			ConstantPool: []any{nil},
		}, nil
	}
	return cf, nil
}

func TestParseMethodDescriptor(t *testing.T) {
	args, ret := ParseMethodDescriptor("(Ljava/lang/String;Lnet/minecraft/world/level/block/Block;)Lnet/minecraft/world/level/block/Block;")
	assert.Equal(t, 2, args)
	assert.True(t, ret)

	args, ret = ParseMethodDescriptor("()V")
	assert.Equal(t, 0, args)
	assert.False(t, ret)

	args, ret = ParseMethodDescriptor("(IDLjava/lang/String;)V")
	assert.Equal(t, 3, args)
	assert.False(t, ret)
}

func TestInterpreterBasic(t *testing.T) {
	provider := &MockClassProvider{Classes: make(map[string]*ClassFile)}
	interp := NewInterpreter(provider)

	// Build a mock ClassFile
	cf := &ClassFile{
		ThisClass: "net/minecraft/world/level/block/Blocks",
		ConstantPool: []any{
			nil,                      // 0
			CPString{StringIndex: 2}, // 1
			CPUtf8("big_dripleaf"),   // 2
			CPClass{NameIndex: 4},    // 3
			CPUtf8("net/minecraft/world/level/block/BigDripleafBlock"), // 4
			CPMethodRef{ClassIndex: 6, NameAndTypeIndex: 7},            // 5 (register)
			CPClass{NameIndex: 8},                                      // 6 (Blocks class)
			CPNameAndType{NameIndex: 9, DescriptorIndex: 10},           // 7 (register name & type)
			CPUtf8("net/minecraft/world/level/block/Blocks"),           // 8
			CPUtf8("register"),                                         // 9
			CPUtf8("(Ljava/lang/String;Lnet/minecraft/world/level/block/Block;)Lnet/minecraft/world/level/block/Block;"), // 10
			CPMethodRef{ClassIndex: 3, NameAndTypeIndex: 12},                                                             // 11 (constructor of BigDripleafBlock)
			CPNameAndType{NameIndex: 13, DescriptorIndex: 14},                                                            // 12 (<init> name & type)
			CPUtf8("<init>"), // 13
			CPUtf8("()V"),    // 14
		},
	}

	// Bytecode sequence:
	// 0x12 0x01: ldc #1 (pushes string "big_dripleaf")
	// 0xbb 0x00 0x03: new #3 (pushes new BigDripleafBlock JVMObject)
	// 0x59: dup
	// 0xb7 0x00 0x0b: invokespecial #11 (calls <init> on BigDripleafBlock)
	// 0xb8 0x00 0x05: invokestatic #5 (calls register)
	// 0x57: pop
	// 0xb1: return
	code := []byte{
		0x12, 0x01,
		0xbb, 0x00, 0x03,
		0x59,
		0xb7, 0x00, 0x0b, // invokespecial #11
		0xb8, 0x00, 0x05, // invokestatic #5
		0x57,
		0xb1,
	}

	err := interp.Execute(cf, code)
	assert.NoError(t, err)

	assert.Len(t, interp.Blocks, 1)
	assert.Contains(t, interp.Blocks, "big_dripleaf")
	blockObj := interp.Blocks["big_dripleaf"]
	assert.Equal(t, "net/minecraft/world/level/block/BigDripleafBlock", blockObj.ClassName)
}
