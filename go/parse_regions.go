package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path"
	"sort"
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

type chunkDatum struct {
	blocks, lights, lightsSky [][]byte
}

// a stream-oriented zero-copy nbt parser
func nbtWalk(buf []byte, cb func(path string, ty nbtType, value []byte)) error {
	path := []string{}
	listStack := []nbtList{}
	depth := 0
	var ty nbtType
	for o := 0; o < len(buf); {
		if len(listStack) > 0 && listStack[len(listStack)-1].depth == depth {
			lt := &listStack[len(listStack)-1]
			lt.idx++
			if lt.idx == lt.length {
				listStack = listStack[:len(listStack)-1]
				continue
			} else {
				ty = lt.ty
				path = append(path[:depth], strconv.Itoa(lt.idx-1))
			}
		} else {
			ty = nbtType(buf[o])
			if ty == tagEnd {
				o++
				depth--
				if depth < 0 {
					return errors.New("unexpected end tag")
				}
				continue
			}
			tagLen := int(binary.BigEndian.Uint16(buf[o+1:]))
			tag := buf[o+3 : o+3+tagLen]
			path = append(path[:depth], string(tag))
			o += 3 + tagLen
		}
		jpath := strings.Join(path[1:], ".")
		switch ty {
		case tagCompound:
			cb(jpath, ty, nil)
			depth++
		case tagByte:
			cb(jpath, ty, buf[o:o+1])
			o++
		case tagShort:
			cb(jpath, ty, buf[o:o+2])
			o += 2
		case tagInt:
			cb(jpath, ty, buf[o:o+4])
			o += 4
		case tagLong:
			cb(jpath, ty, buf[o:o+8])
			o += 8
		case tagFloat:
			cb(jpath, ty, buf[o:o+4])
			o += 4
		case tagDouble:
			cb(jpath, ty, buf[o:o+8])
			o += 8
		case tagByteArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(jpath, ty, buf[o+4:o+4+int(len)])
			o += 4 + int(len)
		case tagString:
			len := binary.BigEndian.Uint16(buf[o:])
			cb(jpath, ty, buf[o+2:o+2+int(len)])
			o += 2 + int(len)
		case tagIntArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(jpath, ty, buf[o+4:o+4+int(len)*4])
			o += 4 + int(len)*4
		case tagLongArray:
			len := binary.BigEndian.Uint32(buf[o:])
			cb(jpath, ty, buf[o+4:o+4+int(len)*8])
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
				cb(jpath, -lty, buf[o:o+len*ltyLen])
				o += len * ltyLen
			} else if lty == tagCompound {
				depth++
				listStack = append(listStack, nbtList{depth: depth, ty: tagCompound, length: len, idx: 0})
			} else if lty == tagString {
				// e.g. Level.TileEntities.Items.tag.pages
				start := o
				for i := 0; i < len; i++ {
					o += int(binary.BigEndian.Uint16(buf[o:])) + 2
				}
				cb(jpath, -lty, buf[start:o])
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

func scanRegion(dir string, outdir string, file os.FileInfo) error {
	f, err := os.Open(path.Join(dir, file.Name()))
	if err != nil {
		return err
	}
	var buf [4096]uint8
	var offsets [1024]uint32
	var timestamps [4096]uint8
	_, err = f.Read(buf[:])
	if err != nil {
		return err
	}

	maxSectors := 0 // size of largest chunk in region
	for i := 0; i < 1024; i++ {
		offsets[i] = binary.BigEndian.Uint32(buf[i*4:])
		if int(offsets[i]&255) > maxSectors {
			maxSectors = int(offsets[i] & 255)
		}
	}

	_, err = f.Read(timestamps[:])
	if err != nil {
		return err
	}

	// read the region file in sequential order, by first sorting
	// a list of chunks indexes according to their offset in the region file
	seqChunks := make([]uint16, 0, 1024)
	for i := 0; i < 1024; i++ {
		if offsets[i] != 0 {
			seqChunks = append(seqChunks, uint16(i))
		}
	}
	sort.Slice(seqChunks[:], func(i, j int) bool {
		return offsets[seqChunks[i]] < offsets[seqChunks[j]]
	})

	fmt.Printf("%s %s ", dir, file.Name())

	chunkBuf := make([]byte, 4096*maxSectors)

	var cdata [1024]chunkDatum

	for _, chunkNum := range seqChunks {
		f.Seek(int64(offsets[chunkNum]>>8)*4096, os.SEEK_SET)
		paddedLen := 4096 * int(offsets[chunkNum]&0xff)
		_, err = f.Read(chunkBuf[:paddedLen])
		if err != nil {
			return err
		}
		chunkLen := int(binary.BigEndian.Uint32(chunkBuf))
		if chunkLen > paddedLen {
			// TODO: header indicates chunk is too long
			log.Println("chunkLen too long??")
			continue
		}
		if chunkBuf[4] != 2 {
			// TODO: ERROR unhandled compression
			log.Println("unhandled compression type", chunkBuf[4])
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader(chunkBuf[5 : chunkLen+4]))
		if err != nil {
			return err
		}
		chunk, err := ioutil.ReadAll(zr)
		if err != nil {
			return err
		}
		blocks := [][]byte{}
		lights := [][]byte{}
		lightsSky := [][]byte{}
		ys := []byte{}
		xPos, zPos := int(chunkNum&31), int(chunkNum>>5)
		chunkZPos := math.MaxInt64
		chunkXPos := math.MaxInt64
		err = nbtWalk(chunk, func(path string, ty nbtType, value []byte) {
			// fmt.Println(path, ty, len(value))
			if ty == tagByte {
				if path == "Level.xPos" {
					chunkXPos = int(value[0])
				} else if path == "level.zPos" {
					chunkZPos = int(value[0])
				}
			}
			if strings.HasPrefix(path, "Level.Sections.") {
				if ty == tagByteArray {
					if strings.HasSuffix(path, "Blocks") {
						blocks = append(blocks, value)
					} else if strings.HasSuffix(path, "BlockLight") {
						lights = append(lights, value)
					} else if strings.HasSuffix(path, "SkyLight") {
						lightsSky = append(lightsSky, value)
					}
				} else if ty == tagByte && strings.HasSuffix(path, "Y") {
					ys = append(ys, value[0])
				}
			}
		})
		if (chunkXPos != math.MaxInt64 && chunkXPos != xPos) || (chunkZPos != math.MaxInt64 && chunkZPos != zPos) {
			log.Printf("chunk misplaced (corrupt region file?)-- expected %d,%d got %d,%d\n", xPos, zPos, chunkXPos, chunkZPos)
			continue
		}
		if !sort.SliceIsSorted(ys, func(i, j int) bool { return ys[i] < ys[j] }) {
			log.Println("sections out of order somehow?", ys)
			continue
		}
		cdata[chunkNum] = chunkDatum{blocks, lights, lightsSky}
		if err != nil {
			return err
		}
	}

	get := func(x, y, z int) byte {
		if y < 0 {
			return 7 // bedrock
		}
		if (x|y|z)&512 != 0 {
			return 0
		}
		chunk := cdata[(x>>4)+(z>>4)*32]
		ys := y >> 4
		if ys >= len(chunk.blocks) {
			return 0
		}
		return chunk.blocks[ys][x&15+(z&15)*16+(y&15)*256]
	}

	neighs := func(x, y, z int) []byte {
		// NOTE: the order of this return value is critical
		// for the vertex shader to reject hidden faces
		return []byte{
			get(x-1, y, z), get(x+1, y, z),
			get(x, y, z+1), get(x, y, z-1),
			get(x, y+1, z), get(x, y-1, z)}
	}

	out, err := os.Create(path.Join(outdir, strings.Replace(path.Base(file.Name()), ".mca", ".cmt", 1)))
	if err != nil {
		log.Println("unable to open dest file")
		return err
	}
	defer out.Close()
	outBuf := bufio.NewWriterSize(out, 1<<20)
	defer outBuf.Flush()

	isSolid := func(b byte) bool {
		// instead of trying to track every transparent block, keep a list of *known* solid blocks
		// this data was stolen from Overviewer
		return [...]uint64{
			2811352310602264766, 9079609371303151104, 11389998976731682, 11529215046034675456,
		}[b>>6]&(1<<(b&63)) != 0
	}

	attrBuf := make([]byte, 8)

	// iterate bottom-to-top so that transparecy (i.e. ocean water)
	// has a chance to render the bottom THEN the surface
	for y := 0; y <= 256; y++ {
		for x := 0; x < 512; x++ {
			for z := 0; z < 512; z++ {
				chunk := cdata[(x>>4)+(z>>4)*32]
				if len(chunk.blocks) < y>>4 {
					continue
				}

				b := get(x, y, z)

				if b == 0 {
					continue
				}

				ns := neighs(x, y, z)

				sideVis := 0
				for i, nb := range ns {
					if b == 8 || b == 9 {
						if nb == 0 || (nb != 8 && nb != 9 && !isSolid(nb)) {
							sideVis |= 1 << i
						}
					} else if !isSolid(nb) {
						sideVis |= 1 << i
					}
				}

				if sideVis != 0 {
					color := uint32(0xfff)
					//TODO: make colors biome-based
					if b == 8 || b == 9 { // water
						color = 0x33f
					} else if b == 2 || b == 18 || b == 161 { // grass (top) / leaves
						color = 0x374
					}
					binary.LittleEndian.PutUint32(attrBuf, uint32(x<<20|y<<10|z))
					binary.LittleEndian.PutUint32(attrBuf[4:], uint32(b)<<24|(color<<6)|uint32(sideVis))
					outBuf.Write(attrBuf)
					// fmt.Println(x, y, z, b)
				}
			}
		}
	}
	offset, err := out.Seek(0, io.SeekEnd)
	fmt.Println(offset/1024, "KiB")

	return err
}

type coord struct {
	x, z int
}

func main() {
	regionDir := os.Args[1]
	filters := []string{}
	if len(os.Args) >= 4 {
		filters = os.Args[3:]
	}
	files, err := ioutil.ReadDir(regionDir)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	for _, file := range files {
		if len(filters) > 0 {
			good := false
			for _, filter := range filters {
				if strings.Contains(file.Name(), filter) {
					good = true
					break
				}
			}
			if !good {
				continue
			}
		}
		err = scanRegion(regionDir, os.Args[2], file)
		if err != nil {
			log.Fatal(err)
		}
	}
}
