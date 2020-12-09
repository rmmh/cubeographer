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
	"sort"
)

type region struct {
	path       string
	bm         *blockMapper
	offsets    [1024]uint32
	timestamps [1024]uint32
}

type paletteEntry struct {
	name  string
	props []string
}

func makeRegion(path string, bm *blockMapper) (*region, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := &region{path: path, bm: bm}

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
		blocks := [][]byte{}
		blockData := [][]byte{}
		blockStates := [][]byte{}
		palettes := make([][]paletteEntry, 17)
		lights := [][]byte{}
		lightsSky := [][]byte{}
		ys := []byte{}
		xPos, zPos := int(chunkNum&31), int(chunkNum>>5)
		chunkZPos := math.MaxInt64
		chunkXPos := math.MaxInt64
		err = nbtWalk(chunk, func(path []string, idxes []int, ty nbtType, value []byte) {
			// fmt.Println(path, ty, len(value))
			if ty == tagByte {
				if path[1] == "xPos" {
					chunkXPos = int(value[0])
				} else if path[1] == "zPos" {
					chunkZPos = int(value[0])
				}
			}
			if len(path) > 1 && path[1] == "Sections" {
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
						blockStates = append(blockStates, value)
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
				if bi >= len(palettes) {
					fmt.Println("wtf, a blockstate without an associated palette?")
					break
				}
				// TODO: handle 1.13-1.15 packing
				vals := blockstatesToShorts116(bs)
				for i, v := range vals {
					vals[i] = r.bm.nameToNid[palettes[bi][v].name]
				}
				nblocks = append(nblocks, vals)
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
