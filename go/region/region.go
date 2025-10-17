package region

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"

	"github.com/rmmh/cubeographer/go/render"
)

type RegionOpener func(path string, bm *BlockMapper) (Regioner, error)
type Regioner interface {
	ReadChunks(wanted []int) ([1024]ChunkDatum, error)
	Rx() int
	Rz() int
}

type paletteEntry struct {
	name  string
	props []string
}

var regionMatchRE = regexp.MustCompile(`r\.(-?\d+)\.(-?\d+)`)

type Region struct {
	path       string
	rx, rz     int
	bm         *BlockMapper
	offsets    [1024]uint32
	timestamps [1024]uint32
}

func (r *Region) Rx() int { return r.rx }
func (r *Region) Rz() int { return r.rz }

func MakeRegion(path string, bm *BlockMapper) (Regioner, error) {
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

	r := &Region{path: path, bm: bm, rx: rx, rz: rz}

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

type ChunkDatum struct {
	Blocks            [][]uint16
	BlockState        [][]render.Stateval
	Lights, LightsSky [][]byte
}

func (r *Region) ReadChunks(wanted []int) ([1024]ChunkDatum, error) {
	var cdata [1024]ChunkDatum

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

	// allocating these once per region saves memory
	chunkBuf := make([]byte, 4096*maxSectors)
	chunkDecompressed := bytes.NewBuffer(make([]byte, 0, 4*(1<<20)))
	palNids := make([]uint16, 0, 64)
	palStates := make([]render.Stateval, 0, 64)
	var zr io.ReadCloser
	var zrr zlib.Resetter

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

		chunkReader := bytes.NewReader(chunkBuf[5 : chunkLen+4])
		if zr == nil {
			zr, err = zlib.NewReader(chunkReader)
			var ok bool
			zrr, ok = zr.(zlib.Resetter)
			if !ok {
				panic("zlib.NewReader MUST be resettable")
			}
		} else {
			err = zrr.Reset(chunkReader, nil)
		}
		if err != nil {
			return cdata, err
		}
		chunkDecompressed.Reset()
		_, err = chunkDecompressed.ReadFrom(zr)
		if err != nil {
			return cdata, err
		}
		chunkDecompressedBytes := chunkDecompressed.Bytes()

		dataVersion := 0
		blocks := [][]byte{}
		blockData := [][]byte{}
		blockStates := make([][]byte, 25) // -64 to 320
		palettes := make([][]paletteEntry, 25)
		lights := [][]byte{}
		lightsSky := [][]byte{}
		ys := []int8{}
		xPos, zPos := int(chunkNum&31)|r.rx<<5, int(chunkNum>>5)|r.rz<<5
		chunkZPos := math.MaxInt64
		chunkXPos := math.MaxInt64
		chunkStatus := ""

		err = NbtWalk(chunkDecompressedBytes, func(path []string, idxes []int, ty NbtType, value []byte) {
			if len(path) == 0 {
				return
			}
			if len(path) <= 2 && ty == TagInt {
				switch path[len(path)-1] {
				case "xPos":
					chunkXPos = int(int32(binary.BigEndian.Uint32(value)))
				case "zPos":
					chunkZPos = int(int32(binary.BigEndian.Uint32(value)))
				}
			}
			last := path[len(path)-1]
			if last == "DataVersion" {
				dataVersion = int(binary.BigEndian.Uint32(value))
			} else if last == "Status" {
				chunkStatus = string(value)
			} else if path[0] == "sections" || len(path) > 1 && path[1] == "Sections" {
				penult := path[len(path)-2]
				if len(idxes) == 2 && len(path) > 4 && (path[3] == "Palette" || path[3] == "palette") {
					cpal := &palettes[idxes[0]]
					if idxes[1] >= len(*cpal) {
						*cpal = append(*cpal, paletteEntry{})
					}
					entry := &(*cpal)[idxes[1]]
					if last == "Name" {
						entry.name = string(value)
					} else if len(path) == 7 && path[5] == "Properties" {
						entry.props = append(entry.props, last+"="+string(value))
					}
				} else if ty == TagByteArray {
					if last == "Blocks" {
						blocks = append(blocks, value)
					} else if last == "Data" {
						blockData = append(blockData, value)
					} else if last == "BlockLight" {
						// lights and lightsky are the only values that escape this
						// function-- don't reference the reused chunk data buffer!
						light := make([]byte, len(value))
						copy(light, value)
						lights = append(lights, light)
					} else if last == "SkyLight" {
						light := make([]byte, len(value))
						copy(light, value)
						lightsSky = append(lightsSky, light)
					}
				} else if ty == TagLongArray {
					if last == "BlockStates" || last == "data" && penult == "block_states" {
						blockStates[idxes[0]] = value
					}
				} else if ty == TagByte && last == "Y" {
					ys = append(ys, int8(value[0]))
				}
			}
		})
		if (chunkXPos != math.MaxInt64 && chunkXPos != xPos) || (chunkZPos != math.MaxInt64 && chunkZPos != zPos) {
			log.Printf("chunk misplaced (corrupt region file?)-- expected %d,%d got %d,%d\n", xPos, zPos, chunkXPos, chunkZPos)
			continue
		}
		if chunkStatus != "" && chunkStatus != "minecraft:full" {
			continue // skip proto-chunks
		}
		for len(ys) > 1 && ys[0] < 0 {
			// TODO: actually render negative Y
			ys = ys[1:]
			palettes = palettes[1:]
			blockStates = blockStates[1:]
		}
		// omit the all-air sections on top
		for i := len(blockStates) - 1; i >= 0; i-- {
			if len(blockStates[i]) == 0 && (len(palettes[i]) == 0 || len(palettes[i]) == 1 && palettes[i][0].name == "minecraft:air") {
				blockStates = blockStates[:i]
			} else {
				break
			}
		}
		if !sort.SliceIsSorted(ys, func(i, j int) bool { return ys[i] < ys[j] }) {
			log.Println("sections out of order somehow? Ys:", ys, "Blockstates:", len(blockStates), blockStates)
			continue
		}
		if len(ys) > 1 && len(palettes[0]) > 1 {
			// for pasting into https://www.brandonfowler.me/nbtreader/
			// fmt.Println(base64.StdEncoding.EncodeToString(chunkDecompressedBytes))
		}
		r.bm.migrate(dataVersion, palettes)
		nblocks := [][]uint16{}
		nstates := [][]render.Stateval{}
		if len(blocks) > 0 {
			if len(blocks) != len(blockData) {
				panic("blocks/blockData length mismatch in" + r.path)
			}
			for bi, bs := range blocks {
				nb := make([]uint16, 4096)
				ns := make([]render.Stateval, 4096)
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
				palNids = palNids[:0]
				palStates = palStates[:0]
				for i := range palettes[bi] {
					palName := palettes[bi][i].name
					nid, ok := r.bm.NameToNid[palName]
					if !ok {
						return cdata, fmt.Errorf("unable to map palette name %s to an id", palName)
					}
					palNids = append(palNids, nid)
					palStates = append(palStates, r.bm.nidToSmap[nid].GetList(palettes[bi][i].props))
				}
				if len(bs) == 0 {
					// empty segment, fill as appropriate
					vals := make([]uint16, 16*16*16)
					states := make([]render.Stateval, 16*16*16)
					if len(palettes[bi]) > 0 && palettes[bi][0].name != "minecraft:air" {
						// only have to fill if it's not air (usually is water)
						p := palettes[bi][0]
						v := r.bm.NameToNid[p.name]
						for i := range 4096 {
							vals[i] = v
						}
						if s := r.bm.nidToSmap[v].GetList(p.props); s != 0 {
							for i := range 4096 {
								states[i] = s
							}
						}
					}
					nblocks = append(nblocks, vals)
					nstates = append(nstates, states)
					continue
				}
				if bi >= len(palettes) {
					fmt.Println("wtf, a blockstate without an associated palette?")
					break
				}
				var vals []uint16
				states := make([]render.Stateval, 16*16*16)
				if dataVersion < 2529 {
					// before 1.16 snapshot 20w17a
					vals = blockstatesToShortsPacked(bs)
				} else {
					vals = blockstatesToShorts116(bs)
				}
				for i, v := range vals {
					vals[i] = palNids[v]
					states[i] = palStates[v]
				}
				nblocks = append(nblocks, vals)
				nstates = append(nstates, states)
			}
		}
		cdata[chunkNum] = ChunkDatum{nblocks, nstates, lights, lightsSky}
		if err != nil {
			return cdata, err
		}
	}

	return cdata, nil
}

// 1.16 64-bit BlockState long array to uint16 array
func blockstatesToShorts116(value []byte) []uint16 {
	bpb := (64 * (len(value) / 8)) / 4096
	if bpb < 4 {
		bpb = 4
	}

	ret := make([]uint16, 4096)
	if bpb == 4 {
		// fast case: a nibble
		for i := 0; i < 4096; i += 2 {
			b := uint16(value[(i/2)&^7+7-(i/2)&7])
			ret[i] = b & 0xf
			ret[i+1] = b >> 4
		}
		return ret
	}

	larr := make([]uint64, len(value)/8)
	for i, v := range value {
		larr[i>>3] |= uint64(v) << ((7 - (i & 7)) * 8)
	}
	bmask := uint64(1<<bpb) - 1
	bpe := 64 / bpb
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
