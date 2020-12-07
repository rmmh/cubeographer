package main

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

type nbtType int

const (
	tagEnd = iota
	tagByte
	tagShort
	tagInt
	tagLong
	tagFloat
	tagDouble
	tagByteArray
	tagString
	tagList
	tagCompound
	tagIntArray
	tagLongArray
)

type nbtList struct {
	depth  int
	ty     nbtType
	length int
	idx    int
}

// a stream-oriented zero-copy nbt parser
func nbtWalk(buf []byte, cb func(path []string, idxes []int, ty nbtType, value []byte)) error {
	path := []string{}
	idxes := []int{}
	listStack := []nbtList{}
	depth := 0
	var ty nbtType
	for o := 0; o < len(buf); {
		if len(listStack) > 0 && listStack[len(listStack)-1].depth == depth {
			lt := &listStack[len(listStack)-1]
			lt.idx++
			if (lt.ty == tagCompound && lt.idx == lt.length) || lt.idx > lt.length {
				listStack = listStack[:len(listStack)-1]
				if lt.ty == tagList {
					depth--
				}
				idxes = idxes[:len(listStack)]
				continue
			} else {
				ty = lt.ty
				path = append(path[:depth], strconv.Itoa(lt.idx-1))
				idxes = append(idxes[:len(listStack)-1], lt.idx-1)
			}
		} else {
			ty = nbtType(buf[o])
			if ty == tagEnd {
				o++
				depth--
				if depth < 0 {
					return fmt.Errorf("unexpected end tag %v, %v", ty, buf[o-1:o+100])
				}
				continue
			}
			tagLen := int(binary.BigEndian.Uint16(buf[o+1:]))
			tag := buf[o+3 : o+3+tagLen]
			path = append(path[:depth], string(tag))
			o += 3 + tagLen
		}
		jpath := strings.Join(path[1:], ".")
		// fmt.Println(jpath, ty, listStack, idxes)
		switch ty {
		case tagCompound:
			cb(path[1:], idxes, ty, nil)
			depth++
		case tagByte:
			cb(path[1:], idxes, ty, buf[o:o+1])
			o++
		case tagShort:
			cb(path[1:], idxes, ty, buf[o:o+2])
			o += 2
		case tagInt:
			cb(path[1:], idxes, ty, buf[o:o+4])
			o += 4
		case tagLong:
			cb(path[1:], idxes, ty, buf[o:o+8])
			o += 8
		case tagFloat:
			cb(path[1:], idxes, ty, buf[o:o+4])
			o += 4
		case tagDouble:
			cb(path[1:], idxes, ty, buf[o:o+8])
			o += 8
		case tagByteArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(path[1:], idxes, ty, buf[o+4:o+4+int(len)])
			o += 4 + int(len)
		case tagString:
			len := binary.BigEndian.Uint16(buf[o:])
			cb(path[1:], idxes, ty, buf[o+2:o+2+int(len)])
			o += 2 + int(len)
		case tagIntArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(path[1:], idxes, ty, buf[o+4:o+4+int(len)*4])
			o += 4 + int(len)*4
		case tagLongArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(path[1:], idxes, ty, buf[o+4:o+4+int(len)*8])
			o += 4 + int(len)*8
		case tagList:
			lty := nbtType(buf[o])
			len := int(binary.BigEndian.Uint32(buf[o+1:]))
			o += 5
			if lty >= tagByte && lty <= tagDouble {
				ltyLen := int(lty)
				if lty > 2 {
					ltyLen = 4 + 4*int((lty-3)%2)
				}
				cb(path[1:], idxes, -lty, buf[o:o+len*ltyLen])
				o += len * ltyLen
			} else if lty == tagCompound {
				if len > 0 {
					depth++
					listStack = append(listStack, nbtList{depth: depth, ty: tagCompound, length: len, idx: 0})
				}
			} else if lty == tagList {
				if len > 0 {
					depth++
					listStack = append(listStack, nbtList{depth: depth, ty: tagList, length: len, idx: 0})
				}
			} else if lty == tagString {
				// e.g. Level.TileEntities.Items.tag.pages
				start := o
				for i := 0; i < len; i++ {
					o += int(binary.BigEndian.Uint16(buf[o:])) + 2
				}
				cb(path[1:], idxes, -lty, buf[start:o])
			} else if len > 0 {
				// TileEntities is length=0 and type=0 when empty?
				return fmt.Errorf("unhandled TAG_List type: %d at %s (len %d)", lty, jpath, len)
			}
		default:
			return fmt.Errorf("unhandled nbt tag type: %d at %s", ty, jpath)
		}
	}
	return nil
}
