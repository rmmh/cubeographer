package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/bits"
	"os"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
				if len > 0 {
					depth++
					listStack = append(listStack, nbtList{depth: depth, ty: tagCompound, length: len, idx: 0})
				}
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

type region struct {
	path       string
	offsets    [1024]uint32
	timestamps [1024]uint32
}

func makeRegion(path string) (*region, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := &region{path: path}

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
			return cdata, err
		}
	}

	return cdata, nil
}

type chunkVis [32 * 32 * (256 / 16)]struct {
	dirReachable int   // 0: +y, 1: -y, 2: +x, 3: -x, 4: +z, 5: -z
	dirVisited   int   // which dirReachable states has this been visited with?
	connected    int64 // 0-5 +y can reach (+y,-y,+x,...), 6-11 -y can reach (+y, -y ...)
}

type tinybitset struct {
	// TODO: accelerate by tracking nonzero vals ?
	vals [4096 / 64]uint64
}

func (t *tinybitset) set(x int) {
	t.vals[x>>6] |= 1 << (x & 63)
}

func (t *tinybitset) clear(x int) {
	t.vals[x>>6] &^= 1 << (x & 63)
}

func (t *tinybitset) has(x int) bool {
	return t.vals[x>>6]&(1<<(x&63)) != 0
}

func (t *tinybitset) pop() int {
	for o, v := range t.vals {
		if v != 0 {
			off := 63 - bits.LeadingZeros64(v)
			t.vals[o] ^= 1 << off
			return o*64 + off
		}
	}
	return -1
}

func isSolid(b byte) bool {
	// instead of trying to track every transparent block, keep a list of *known* solid blocks
	// this data was stolen from Overviewer
	return [...]uint64{
		2811352310602264766 | (1 << 11 /* still lava */), 9079609371303151104, 11389998976731682, 11529215046034675456,
	}[b>>6]&(1<<(b&63)) != 0
}

func computeConnected(chunklet []byte) int64 {
	var passable tinybitset
	for i, b := range chunklet {
		if !isSolid(b) {
			passable.set(i)
		}
	}
	var conn int64
	for cur := passable.pop(); cur != -1; cur = passable.pop() {
		// layout: x+z*16+y*256
		faces := 0
		var todo tinybitset
		todo.set(cur)
		for cur = todo.pop(); cur != -1; cur = todo.pop() {
			passable.clear(cur)
			// fmt.Println(cur, faces, todo.has(cur), passable.has(cur))
			if cur < 256 { // -y, i.e. an exit to the negative y face (down)
				faces |= 1 << 1
				if passable.has(cur + 256) {
					todo.set(cur + 256)
				}
			} else if cur >= 15*256 { // +y
				faces |= 1 << 0
				if passable.has(cur - 256) {
					todo.set(cur - 256)
				}
			} else {
				if passable.has(cur - 256) {
					todo.set(cur - 256)
				}
				if passable.has(cur + 256) {
					todo.set(cur + 256)
				}
			}
			if (cur & 0xFF) < 16 { // -z
				faces |= 1 << 5
				if passable.has(cur + 16) {
					todo.set(cur + 16)
				}
			} else if (cur & 0xFF) >= 15*16 { // +z
				faces |= 1 << 4
				if passable.has(cur - 16) {
					todo.set(cur - 16)
				}
			} else {
				if passable.has(cur - 16) {
					todo.set(cur - 16)
				}
				if passable.has(cur + 16) {
					todo.set(cur + 16)
				}
			}
			if (cur & 0xF) == 0 { // -x
				faces |= 1 << 3
				if passable.has(cur + 1) {
					todo.set(cur + 1)
				}
			} else if (cur & 0xF) >= 15 { // +x
				faces |= 1 << 2
				if passable.has(cur - 1) {
					todo.set(cur - 1)
				}
			} else {
				if passable.has(cur - 1) {
					todo.set(cur - 1)
				}
				if passable.has(cur + 1) {
					todo.set(cur + 1)
				}
			}
		}
		for i := 0; i < 6; i++ {
			if faces&(1<<i) != 0 {
				conn |= int64(faces) << (6 * i)
			}
		}
	}
	return conn
}

func makeChunkvis(chunks [1024]chunkDatum) *chunkVis {
	var cv chunkVis

	for cx := 0; cx < 32; cx++ {
		for cz := 0; cz < 32; cz++ {
			for ys, chunklet := range chunks[cx+cz*32].blocks {
				cv[cx+cz*32+ys*1024].connected = computeConnected(chunklet)
			}
		}
	}

	// 0: +y, 1: -y, 2: +x, 3: -x, 4: +z, 5: -z
	for cx := 0; cx < 32; cx++ {
		for cz := 0; cz < 32; cz++ {
			chunk := chunks[cx+cz*32]
			if len(chunk.blocks) == 0 {
				continue
			}
			mask := 0b111101 // top chunklet reachable every dir but below
			cv[cx+cz*32+(len(chunk.blocks)-1)*1024].dirReachable |= mask
			if cx == 0 {
				mask &^= 1 << 2
			} else if cx == 31 {
				mask &^= 1 << 3
			}
			if cz == 0 {
				mask &^= 1 << 4
			} else if cz == 31 {
				mask &^= 1 << 5
			}
			if mask == 0b111101 { // i.e., not on edge
				continue
			}
			for ys := 0; ys < len(chunk.blocks); ys++ {
				cv[cx+cz*32+ys*1024].dirReachable |= mask // side chunklet reachable every dir
			}
		}
	}

	// this algorithm is vaguely based on https://tomcc.github.io/2014/08/31/visibility-2.html
	// ...or it would be, but we end up mostly just following straight down, oh well
	for cx := 0; cx < 32; cx++ {
		for cz := 0; cz < 32; cz++ {
			for ys := len(chunks[cx+cz*32].blocks) - 1; ys >= 0; ys-- {
				ccv := &cv[cx+cz*32+ys*1024]
				ccv.dirReachable |= 1 << 1
				if ys > 3 {
					if ccv.connected&0b000010_000010_000010_000010_000000_000010 == 0 {
						break
					}
				} else if ccv.connected&0b000010 == 0 {
					break
				}
			}
		}
	}

	return &cv
}

func scanRegion(dir string, outdir string, file os.FileInfo) error {
	if !strings.HasSuffix(file.Name(), ".mca") {
		return errors.New("file has wrong suffix (not .mca): " + file.Name())
	}

	r, err := makeRegion(path.Join(dir, file.Name()))
	if err != nil {
		return err
	}

	var rx, rz int
	regionMatch := regexp.MustCompile(`r\.(-?\d+)\.(-?\d+)`).FindStringSubmatch(file.Name())
	if regionMatch != nil {
		rx, _ = strconv.Atoi(regionMatch[1])
		rz, _ = strconv.Atoi(regionMatch[2])
	}

	cdata, err := r.readChunks(nil)
	if err != nil {
		return err
	}

	cadj := map[uint][1024]chunkDatum{}

	get := func(x, y, z int) (byte, byte, byte) {
		if y < 0 {
			return 7, 0xf, 0 // bedrock
		}
		var chunk chunkDatum
		if (x|z)&512 != 0 {
			key := (uint(x>>9)&3)<<2 | uint(z>>9)&3
			if _, ok := cadj[key]; !ok {
				ox := rx
				oz := rz
				wanted := make([]int, 0, 32)
				if x < 0 {
					ox--
					for i := 0; i < 32; i++ {
						wanted = append(wanted, 31+i*32)
					}
				} else if x >= 512 {
					ox++
					for i := 0; i < 32; i++ {
						wanted = append(wanted, i*32)
					}
				} else if z < 0 {
					oz--
					for i := 0; i < 32; i++ {
						wanted = append(wanted, i+31*32)
					}
				} else if z >= 512 {
					oz++
					for i := 0; i < 32; i++ {
						wanted = append(wanted, i)
					}
				}
				ap := path.Join(dir, fmt.Sprintf("r.%d.%d.mca", ox, oz))
				r, err := makeRegion(ap)
				if err != nil {
					cadj[key] = [1024]chunkDatum{}
					return 0, 0xf, 0
				}
				cadj[key], err = r.readChunks(wanted)
				if err != nil {
					cadj[key] = [1024]chunkDatum{}
					return 0, 0xf, 0
				}
			}
			chunk = cadj[key][((x&511)>>4)+((z&511)>>4)*32]
		} else {
			chunk = cdata[(x>>4)+(z>>4)*32]
		}
		ys := y >> 4
		if ys >= len(chunk.blocks) {
			return 0, 0, 0xf
		}
		o := x&15 + (z&15)*16 + (y&15)*256
		s := (x & 1) << 2
		return chunk.blocks[ys][o], (chunk.lights[ys][o/2] >> s) & 0xf, (chunk.lightsSky[ys][o/2] >> s) & 0xf
	}

	getLight := func(x, y, z int) byte {
		chunk := cdata[(x>>4)+(z>>4)*32]
		ys := y >> 4
		if ys >= len(chunk.lights) || ys >= len(chunk.lightsSky) {
			return 15
		}
		o := ((x & 15) + (z&15)*16 + (y&15)*256) / 2
		s := (x & 1) << 2
		return chunk.lights[ys][o]>>s + chunk.lightsSky[ys][o]>>s
	}

	neighs := func(x, y, z int) ([]byte, []byte, []byte) {
		// NOTE: the order of this return value is critical
		// for the vertex shader to reject hidden faces
		bs := make([]byte, 6)
		ls := make([]byte, 6)
		sl := make([]byte, 6)
		bs[0], ls[0], sl[0] = get(x-1, y, z)
		bs[1], ls[1], sl[1] = get(x+1, y, z)
		bs[2], ls[2], sl[2] = get(x, y, z+1)
		bs[3], ls[3], sl[3] = get(x, y, z-1)
		bs[4], ls[4], sl[4] = get(x, y+1, z)
		bs[5], ls[5], sl[5] = get(x, y-1, z)
		return bs, ls, sl
	}

	getLight(0, 0, 0)
	// does this 4*4*4 regions have at least one lit block?
	const lr = 4
	lit := make([]bool, (256*512*512)/(lr*lr*lr))
	for y := 0; y < 63; y++ {
		for x := 0; x < 512; x++ {
			for z := 0; z < 512; z++ {
				if getLight(x, y, z) > 0 {
					lit[(y/lr)*512*512/16+z/lr*512/lr+x/lr] = true
				}
			}
		}
	}

	chunkVis := makeChunkvis(cdata)

	var bufs [4][2]bytes.Buffer

	buf := make([]byte, 12)
	// TODO: emulate minecraft renderpasses -- solid, cutout (i.e. sprite), translucent (liquid)

	const subScale = 16
	lowRez := make([]uint64, 512*512*256/(subScale*subScale*subScale))

	// iterate bottom-to-top so that transparency (i.e. ocean water)
	// has a chance to render the bottom THEN the surface
	for y := 0; y < 256; y++ {
		for x := 0; x < 512; x++ {
			for z := 0; z < 512; z++ {
				chunkletVis := chunkVis[(x>>4)+(z>>4)*32+(y>>4)*1024]

				if chunkletVis.dirReachable == 0 && (y < 40 || !lit[(y/lr)*512*512/16+z/lr*512/lr+x/lr]) {
					// cavey-elimination
					// continue
				}

				chunk := cdata[(x>>4)+(z>>4)*32]
				if len(chunk.blocks) < y>>4 {
					continue
				}

				b, bl, bsl := get(x, y, z)

				if b == 0 {
					continue
				}

				ns, nl, nsl := neighs(x, y, z)

				sideVis := uint32(0)
				sideLight := uint32(0)
				for i, nb := range ns {
					if b == 8 || b == 9 {
						if nb == 0 || (nb != 8 && nb != 9 && !isSolid(nb)) {
							sideVis |= 1 << i
						}
					} else if !isSolid(nb) {
						sideVis |= 1 << i
					}
					l := nsl[i]
					if nl[i] > l {
						l = nl[i]
					} else if bl > l {
						l = bl
					} else if bsl > l {
						l = bsl
					}
					sideLight |= uint32(l) << (4 * i)
				}

				if sideVis != 0 {
					// extra rendering flags
					// 0: use sprite+256 for sides
					// 1: tint according to biome colors
					meta := uint32(0)
					if b == 2 || b == 17 || b == 46 || b == 47 || b == 24 {
						meta = 1 // special side
					}
					//TODO: make colors biome-based
					if b == 8 || b == 9 { // water
						meta |= 2 // color = 0x36e
					} else if b == 2 || b == 18 || b == 161 { // grass (top) / leaves
						meta |= 2 // color = 0x6C4
					} else if b == 106 { // vines
						meta |= 2 // color = 0x5b6
					}
					// x: 8b z: 8b y: 8b   8+8+8=26b
					binary.LittleEndian.PutUint32(buf, uint32(b)<<24|uint32((x&255)<<16|(z&255)<<8|y))
					binary.LittleEndian.PutUint32(buf[4:], meta<<30|sideLight<<6|sideVis)
					bs := &bufs[x>>8+2*(z>>8)]
					if b == 8 || b == 9 || b == 79 || b == 174 /* ice */ || !isSolid(b) {
						bs[1].Write(buf[:8])
					} else {
						bs[0].Write(buf[:8])
					}

					if isSolid(b) || b == 8 || b == 9 {
						lro := (x / subScale) + (z/subScale)*(512/subScale) + (y/subScale)*(512*512/subScale/subScale)
						if byte(lowRez[lro]>>24) == b {
							// lowRez[lro]++
						} else if lowRez[lro]&0xffffff == 0 {
							// 4b y (0-255), 9b x (0-4096), 9b z (0-4096)
							lowRez[lro] = uint64(uint32(b)<<24+meta<<22+uint32(y/subScale)+uint32((rx<<9+x)&4095)<<4+uint32((rz<<9+z)&4095)<<13)<<32 + 1
						} else {
							lowRez[lro]--
						}
					}
				}
			}
		}
	}

	nameBase := path.Join(outdir, strings.TrimSuffix(path.Base(file.Name()), ".mca"))
	outLen := 0
	for bi, bs := range bufs {
		out, err := os.Create(fmt.Sprintf("%s.%d.cmt", nameBase, bi))
		outBuf := bufio.NewWriterSize(out, 1<<20)
		if err != nil {
			log.Println("unable to open dest file")
			return err
		}

		outBuf.Write([]byte("COMTE00\n"))
		for _, obuf := range bs {
			binary.LittleEndian.PutUint32(buf, uint32(obuf.Len()))
			outLen += obuf.Len()
			outBuf.Write(buf[:4])
		}
		for _, obuf := range bs {
			outBuf.Write(obuf.Bytes())
		}
		outBuf.Flush()
		out.Close()
	}

	lout, err := os.Create(path.Join(outdir, strings.Replace(path.Base(file.Name()), ".mca", ".cml", 1)))
	if err != nil {
		log.Println("unable to open dest lowrez file")
		return err
	}
	defer lout.Close()

	var lowrezBuf bytes.Buffer
	for _, v := range lowRez {
		if v == 0 {
			continue
		}
		binary.LittleEndian.PutUint32(buf, uint32(v>>32))
		lowrezBuf.Write(buf[:4])
	}

	lrsize := lowrezBuf.Len()
	binary.LittleEndian.PutUint32(buf, uint32(rx))
	binary.LittleEndian.PutUint32(buf[4:], uint32(rz))
	lout.Write(buf[:8])
	binary.LittleEndian.PutUint32(buf, uint32(lowrezBuf.Len()))
	lout.Write(buf[:4])
	lout.Write(lowrezBuf.Bytes())

	fmt.Println(dir, file.Name(), outLen/1024, "KiB", lrsize, "B shrunk")

	return err
}

type coord struct {
	x, z int
}

func main() {
	regionDir := os.Args[1]
	outDir := os.Args[2]
	filters := []string{}
	if len(os.Args) >= 4 {
		filters = os.Args[3:]
	}

	files, err := ioutil.ReadDir(regionDir)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	work := make(chan os.FileInfo)
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for file := range work {
				err = scanRegion(regionDir, outDir, file)
				if err != nil {
					log.Fatal(err)
				}
				wg.Done()
			}
		}()
	}

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
		wg.Add(1)
		work <- file
	}
	wg.Wait()
}
