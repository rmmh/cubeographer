package region

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

type NbtType int

const (
	TagEnd = iota
	TagByte
	TagShort
	TagInt
	TagLong
	TagFloat
	TagDouble
	TagByteArray
	TagString
	TagList
	TagCompound
	TagIntArray
	TagLongArray
)

type nbtList struct {
	depth  int
	ty     NbtType
	length int
	idx    int
}

// a stream-oriented zero-copy nbt parser
func NbtWalk(buf []byte, cb func(path []string, idxes []int, ty NbtType, value []byte)) error {
	path := []string{}
	idxes := []int{}
	listStack := []nbtList{}
	depth := 0
	var ty NbtType
	for o := 0; o < len(buf); {
		if len(listStack) > 0 && listStack[len(listStack)-1].depth == depth {
			lt := &listStack[len(listStack)-1]
			lt.idx++
			if lt.idx > lt.length {
				listStack = listStack[:len(listStack)-1]
				depth--
				idxes = idxes[:len(listStack)]
				continue
			} else {
				ty = lt.ty
				path = append(path[:depth], strconv.Itoa(lt.idx-1))
				idxes = append(idxes[:len(listStack)-1], lt.idx-1)
			}
		} else {
			ty = NbtType(buf[o])
			if ty == TagEnd {
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
		// fmt.Println(jpath, ty, listStack, depth, idxes)
		switch ty {
		case TagCompound:
			cb(path[1:], idxes, ty, nil)
			depth++
		case TagByte:
			cb(path[1:], idxes, ty, buf[o:o+1])
			o++
		case TagShort:
			cb(path[1:], idxes, ty, buf[o:o+2])
			o += 2
		case TagInt:
			cb(path[1:], idxes, ty, buf[o:o+4])
			o += 4
		case TagLong:
			cb(path[1:], idxes, ty, buf[o:o+8])
			o += 8
		case TagFloat:
			cb(path[1:], idxes, ty, buf[o:o+4])
			o += 4
		case TagDouble:
			cb(path[1:], idxes, ty, buf[o:o+8])
			o += 8
		case TagByteArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(path[1:], idxes, ty, buf[o+4:o+4+int(len)])
			o += 4 + int(len)
		case TagString:
			len := binary.BigEndian.Uint16(buf[o:])
			cb(path[1:], idxes, ty, buf[o+2:o+2+int(len)])
			o += 2 + int(len)
		case TagIntArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(path[1:], idxes, ty, buf[o+4:o+4+int(len)*4])
			o += 4 + int(len)*4
		case TagLongArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(path[1:], idxes, ty, buf[o+4:o+4+int(len)*8])
			o += 4 + int(len)*8
		case TagList:
			lty := NbtType(buf[o])
			len := int(binary.BigEndian.Uint32(buf[o+1:]))
			o += 5
			if lty >= TagByte && lty <= TagDouble {
				ltyLen := int(lty)
				if lty > 2 {
					ltyLen = 4 + 4*int((lty-3)%2)
				}
				cb(path[1:], idxes, -lty, buf[o:o+len*ltyLen])
				o += len * ltyLen
			} else if lty == TagCompound {
				if len > 0 {
					depth++
					listStack = append(listStack, nbtList{depth: depth, ty: TagCompound, length: len, idx: 0})
				}
			} else if lty == TagList {
				if len > 0 {
					depth++
					listStack = append(listStack, nbtList{depth: depth, ty: TagList, length: len, idx: 0})
				}
			} else if lty == TagIntArray {
				if len > 0 {
					depth++
					listStack = append(listStack, nbtList{depth: depth, ty: TagIntArray, length: len, idx: 0})
				}
			} else if lty == TagString {
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
