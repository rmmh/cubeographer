package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
)

type region struct {
	path       string
	rx, rz     int
	bm         *blockMapper
	offsets    [1024]uint32
	timestamps [1024]uint32
}

type paletteEntry struct {
	name  string
	props []string
}

var regionMatchRE = regexp.MustCompile(`r\.(-?\d+)\.(-?\d+)`)

func makeRegion(path string, bm *blockMapper) (*region, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rx, rz int
	m := regionMatchRE.FindStringSubmatch(path)
	if m != nil {
		rx, _ = strconv.Atoi(m[1])
		rz, _ = strconv.Atoi(m[2])
	} else {
		fmt.Println("WARN: region file doesn't match expected r.XX.ZZ format")
	}

	r := &region{path: path, bm: bm, rx: rx, rz: rz}

	var buf [4096]uint8
	_, err = f.Read(buf[:])
	if err != nil {
		return nil, err
	}
	for i := 0; i < 1024; i++ {
		r.offsets[i] = binary.BigEndian.Uint32(buf[i*4:])
	}

	_, err = f.Read(buf[:])
	if err != nil {
		return nil, err
	}
	for i := 0; i < 1024; i++ {
		r.timestamps[i] = binary.BigEndian.Uint32(buf[i*4:])
	}

	return r, nil
}

type chunkDatum struct {
	blocks            [][]uint16
	blockState        [][]uint8
	lights, lightsSky [][]byte
}

func (r *region) readChunks(wanted []int) ([1024]chunkDatum, error) {
	var cdata [1024]chunkDatum

	f, err := os.Open(r.path)
	if err != nil {
		return cdata, err
	}
	defer f.Close()

	maxSectors := 0 // size of largest chunk in region
	for _, offset := range r.offsets {
		if int(offset&255) > maxSectors {
			maxSectors = int(offset & 255)
		}
	}

	// read the region file in sequential order, by first sorting
	// a list of chunks indexes according to their offset in the region file
	seqChunks := make([]uint16, 0, 1024)
	if len(wanted) > 0 {
		for _, i := range wanted {
			if r.offsets[i] != 0 {
				seqChunks = append(seqChunks, uint16(i))
			}
		}
	} else {
		for i := 0; i < 1024; i++ {
			if r.offsets[i] != 0 {
				seqChunks = append(seqChunks, uint16(i))
			}
		}
	}
	sort.Slice(seqChunks[:], func(i, j int) bool {
		return r.offsets[seqChunks[i]] < r.offsets[seqChunks[j]]
	})

	chunkBuf := make([]byte, 4096*maxSectors)

	for _, chunkNum := range seqChunks {
		f.Seek(int64(r.offsets[chunkNum]>>8)*4096, os.SEEK_SET)
		paddedLen := 4096 * int(r.offsets[chunkNum]&0xff)
		_, err = f.Read(chunkBuf[:paddedLen])
		if err != nil {
			return cdata, err
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
			return cdata, err
		}
		chunk, err := ioutil.ReadAll(zr)
		if err != nil {
			return cdata, err
		}
		dataVersion := 0
		blocks := [][]byte{}
		blockData := [][]byte{}
		blockStates := make([][]byte, 17)
		palettes := make([][]paletteEntry, 17)
		lights := [][]byte{}
		lightsSky := [][]byte{}
		ys := []byte{}
		xPos, zPos := int(chunkNum&31)|r.rx<<5, int(chunkNum>>5)|r.rz<<5
		chunkZPos := math.MaxInt64
		chunkXPos := math.MaxInt64
		err = nbtWalk(chunk, func(path []string, idxes []int, ty nbtType, value []byte) {
			//fmt.Println(path, ty, len(value), value)
			if len(path) == 2 && ty == tagInt {
				if path[1] == "xPos" {
					chunkXPos = int(int32(binary.BigEndian.Uint32(value)))
				} else if path[1] == "zPos" {
					chunkZPos = int(int32(binary.BigEndian.Uint32(value)))
				}
			}
			if len(path) == 1 && path[0] == "DataVersion" {
				dataVersion = int(binary.BigEndian.Uint32(value))
			} else if len(path) > 1 && path[1] == "Sections" {
				last := path[len(path)-1]
				if len(idxes) == 2 && len(path) > 4 && path[3] == "Palette" {
					cpal := &palettes[idxes[0]]
					if idxes[1] >= len(*cpal) {
						*cpal = append(*cpal, paletteEntry{})
					}
					entry := &(*cpal)[idxes[1]]
					if last == "Name" {
						entry.name = string(value)
						// fmt.Println("PALNAME", path, idxes, string(value))
					} else if len(path) == 7 && path[5] == "Properties" {
						entry.props = append(entry.props, last+"="+string(value))
						// fmt.Println("PALPROP", path, last, idxes, string(value))
					}
				} else if ty == tagByteArray {
					if last == "Blocks" {
						blocks = append(blocks, value)
					} else if last == "Data" {
						blockData = append(blockData, value)
					} else if last == "BlockLight" {
						lights = append(lights, value)
					} else if last == "SkyLight" {
						lightsSky = append(lightsSky, value)
					}
				} else if ty == tagLongArray {
					if last == "BlockStates" {
						blockStates[idxes[0]] = value
						// fmt.Println("BLOCKSTATES", path, last, idxes, len(value), 8*len(value)/4096)
					}
				} else if ty == tagByte && last == "Y" {
					ys = append(ys, value[0])
				}
			}
		})
		if (chunkXPos != math.MaxInt64 && chunkXPos != xPos) || (chunkZPos != math.MaxInt64 && chunkZPos != zPos) {
			log.Printf("chunk misplaced (corrupt region file?)-- expected %d,%d got %d,%d\n", xPos, zPos, chunkXPos, chunkZPos)
			continue
		}
		if len(ys) > 1 && ys[0] == 255 {
			ys = ys[1:]
			palettes = palettes[1:]
			blockStates = blockStates[1:]
		}
		for len(blockStates) > 0 && len(blockStates[len(blockStates)-1]) == 0 {
			blockStates = blockStates[:len(blockStates)-1]
		}
		if !sort.SliceIsSorted(ys, func(i, j int) bool { return ys[i] < ys[j] }) {
			log.Println("sections out of order somehow? Ys:", ys, "Blockstates:", len(blockStates), blockStates)
			continue
		}
		nblocks := [][]uint16{}
		nstates := [][]uint8{}
		if len(blocks) > 0 {
			if len(blocks) != len(blockData) {
				panic("blocks/blockData length mismatch in" + r.path)
			}
			for bi, bs := range blocks {
				nb := make([]uint16, 4096)
				ns := make([]uint8, 4096)
				for i, ob := range bs {
					o := uint16(ob)<<4 | uint16((blockData[bi][i>>1]>>((i&1)<<2))&0xf)
					nb[i] = r.bm.blockstateToNid[o]
					ns[i] = r.bm.blockstateToNstate[o]
					if nb[i] == 0 {
						nb[i] = r.bm.blockstateToNid[o&^0xf]
						ns[i] = r.bm.blockstateToNstate[o&^0xf]
					}
				}
				nblocks = append(nblocks, nb)
				nstates = append(nstates, ns)
			}
		} else if len(blockStates) > 0 {
			for bi, bs := range blockStates {
				if len(bs) == 0 {
					// empty segment
					nblocks = append(nblocks, make([]uint16, 16*16*16))
					nstates = append(nstates, make([]uint8, 16*16*16))
					continue
				}
				if bi >= len(palettes) {
					fmt.Println("wtf, a blockstate without an associated palette?")
					break
				}
				var vals []uint16
				states := make([]uint8, 16*16*16)
				if dataVersion < 2529 {
					// before 1.16 snapshot 20w17a
					vals = blockstatesToShortsPacked(bs)
				} else {
					vals = blockstatesToShorts116(bs)
				}
				for i, v := range vals {
					vals[i] = r.bm.nameToNid[palettes[bi][v].name]
					states[i] = r.bm.nidToSmap[vals[i]].getStateList(palettes[bi][v].props)
				}
				nblocks = append(nblocks, vals)
				nstates = append(nstates, states)
			}
		}
		cdata[chunkNum] = chunkDatum{nblocks, nstates, lights, lightsSky}
		if err != nil {
			return cdata, err
		}
	}

	return cdata, nil
}

// 1.16 64-bit BlockState long array to uint16 array
func blockstatesToShorts116(value []byte) []uint16 {
	larr := make([]uint64, len(value)/8)
	for i, v := range value {
		larr[i>>3] |= uint64(v) << ((7 - (i & 7)) * 8)
	}
	bpb := (64 * len(larr)) / 4096
	if bpb < 4 {
		bpb = 4
	}
	bmask := uint64(1<<bpb) - 1
	bpe := 64 / bpb
	ret := make([]uint16, 4096)
	for bsi, v := range larr {
		for i := 0; i < bpe; i++ {
			index := (v >> (i * bpb)) & bmask
			nido := bsi*bpe + i
			if nido >= 4096 {
				break
			}
			ret[nido] = uint16(index)
		}
	}
	return ret
}

// pre-1.16, blockstates are packed to use every bit possible
func blockstatesToShortsPacked(value []byte) []uint16 {
	bpb := (64 * (len(value) / 8)) / 4096
	if 64%bpb == 0 {
		// simple case: the state bits fit into longs with no slop
		return blockstatesToShorts116(value)
	}

	bmask := uint32(1<<bpb) - 1
	ret := make([]uint16, 4096)
	var bitbuf uint32
	bits := 0
	vptr := 0
	for i := 0; i < 4096; i++ {
		for bits < bpb {
			// n.b.: value is a representation of *big endian* longs
			// this bit twiddling reads it in the right order
			bitbuf |= (uint32(value[vptr&^7+(7-vptr&7)]) << bits)
			bits += 8
			vptr++
		}
		ret[i] = uint16(bitbuf & bmask)
		bitbuf >>= bpb
		bits -= bpb
	}

	return ret
}
