package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type regionState struct {
	r      *region
	dir    string
	bm     *blockMapper
	rx, rz int
	cdata  [1024]chunkDatum
	cadj   [16]*[1024]chunkDatum

	nbs [6]uint16
	nls [6]byte
	nsl [6]byte
}

func (rs *regionState) get(x, y, z int) (uint16, uint8, byte, byte) {
	if y < 0 {
		return 7, 0, 0xf, 0 // bedrock
	}
	var chunk *chunkDatum
	if (x|z)&512 != 0 {
		key := (uint(x>>9)&3)<<2 | uint(z>>9)&3
		if rs.cadj[key] == nil {
			ox := rs.rx
			oz := rs.rz
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
			ap := path.Join(rs.dir, fmt.Sprintf("r.%d.%d.mca", ox, oz))
			r, err := makeRegion(ap, rs.bm)
			if err != nil {
				rs.cadj[key] = &[1024]chunkDatum{}
				return 0, 0, 0xf, 0
			}
			chunks, err := r.readChunks(wanted)
			if err != nil {
				rs.cadj[key] = &[1024]chunkDatum{}
				return 0, 0, 0xf, 0
			}
			rs.cadj[key] = &chunks
		}
		chunk = &rs.cadj[key][((x&511)>>4)+((z&511)>>4)*32]
	} else {
		chunk = &rs.cdata[(x>>4)+(z>>4)*32]
	}
	ys := y >> 4
	if ys >= len(chunk.blocks) {
		return 0, 0, 0, 0xf
	}
	o := x&15 + (z&15)*16 + (y&15)*256
	s := (x & 1) << 2
	b := chunk.blocks[ys][o]
	bs := chunk.blockState[ys][o]
	if ys >= len(chunk.lights) {
		if ys >= len(chunk.lightsSky) {
			return b, bs, 0xf, 0xf
		}
		return b, bs, 0xf, (chunk.lightsSky[ys][o/2] >> s) & 0xf
	} else if ys >= len(chunk.lightsSky) {
		return b, bs, (chunk.lights[ys][o/2] >> s) & 0xf, 0xf
	}
	return b, bs, (chunk.lights[ys][o/2] >> s) & 0xf, (chunk.lightsSky[ys][o/2] >> s) & 0xf
}

func (rs *regionState) getLight(x, y, z int) byte {
	chunk := &rs.cdata[(x>>4)+(z>>4)*32]
	ys := y >> 4
	if ys >= len(chunk.lights) || ys >= len(chunk.lightsSky) {
		return 15
	}
	o := ((x & 15) + (z&15)*16 + (y&15)*256) / 2
	s := (x & 1) << 2
	return chunk.lights[ys][o]>>s + chunk.lightsSky[ys][o]>>s
}

func (rs *regionState) neighs(x, y, z int) ([]uint16, []byte, []byte) {
	// NOTE: the order of this return value is critical
	// for the vertex shader to reject hidden faces
	rs.nbs[0], _, rs.nls[0], rs.nsl[0] = rs.get(x-1, y, z)
	rs.nbs[1], _, rs.nls[1], rs.nsl[1] = rs.get(x+1, y, z)
	rs.nbs[2], _, rs.nls[2], rs.nsl[2] = rs.get(x, y, z+1)
	rs.nbs[3], _, rs.nls[3], rs.nsl[3] = rs.get(x, y, z-1)
	rs.nbs[4], _, rs.nls[4], rs.nsl[4] = rs.get(x, y+1, z)
	rs.nbs[5], _, rs.nls[5], rs.nsl[5] = rs.get(x, y-1, z)
	return rs.nbs[:], rs.nls[:], rs.nsl[:]
}

type scanRegionConfig struct {
	dir, outdir string
	file        os.FileInfo
	bm          *blockMapper

	pruneCaves bool
}

func scanRegion(conf *scanRegionConfig) error {
	if !strings.HasSuffix(conf.file.Name(), ".mca") {
		return errors.New("file has wrong suffix (not .mca): " + conf.file.Name())
	}

	bm := conf.bm
	r, err := makeRegion(path.Join(conf.dir, conf.file.Name()), bm)
	if err != nil {
		return err
	}

	var rx, rz int
	regionMatch := regexp.MustCompile(`r\.(-?\d+)\.(-?\d+)`).FindStringSubmatch(conf.file.Name())
	if regionMatch != nil {
		rx, _ = strconv.Atoi(regionMatch[1])
		rz, _ = strconv.Atoi(regionMatch[2])
	}

	cdata, err := r.readChunks(nil)
	if err != nil {
		return err
	}

	rs := regionState{
		dir:   conf.dir,
		bm:    bm,
		rx:    rx,
		rz:    rz,
		cdata: cdata,
	}

	var chunkVis *chunkVis
	var lit []bool
	const lr = 4

	if conf.pruneCaves {
		// does each 4*4*4 region have at least one lit block?
		lit = make([]bool, (256*512*512)/(lr*lr*lr))
		for y := 0; y < 64; y++ {
			for z := 0; z < 512; z++ {
				for x := 0; x < 512; x++ {
					if rs.getLight(x, y, z) > 0 {
						lit[(y/lr)*512*512/16+z/lr*512/lr+x/lr] = true
					}
				}
			}
		}
		chunkVis = makeChunkvis(cdata, bm)
	}

	var bufs [4][numRenderLayers]bytes.Buffer

	buf := make([]byte, 12)
	// TODO: emulate minecraft renderpasses -- solid, cutout (i.e. sprite), translucent (liquid)

	const subScale = 16

	// iterate bottom-to-top so that transparency (i.e. ocean water)
	// has a chance to render the bottom THEN the surface

	waterID := bm.nameToNid["minecraft:water"]

	blockCounts := make([]int, len(bm.tmpl))

	for y := 0; y < 256; y++ {
		for z := 0; z < 512; z++ {
			for x := 0; x < 512; x++ {
				if conf.pruneCaves {
					chunkletVis := &chunkVis[(x>>4)+(z>>4)*32+(y>>4)*1024]
					if chunkletVis.dirReachable == 0 && (y < 40 || !lit[(y/lr)*512*512/16+z/lr*512/lr+x/lr]) {
						// cavey-elimination
						continue
					}
				}

				chunk := &cdata[(x>>4)+(z>>4)*32]
				if len(chunk.blocks) < y>>4 {
					continue
				}

				b, bs, bl, bsl := rs.get(x, y, z)

				if b == 0 {
					continue
				}

				ns, nl, nsl := rs.neighs(x, y, z)

				sideVis := uint32(0)
				sideLight := uint32(0)
				for i, nb := range ns {
					if b == waterID {
						if nb == 0 || (nb != waterID && !bm.isSolid(nb)) {
							sideVis |= 1 << i
						}
					} else if !bm.isSolid(nb) {
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
					blockCounts[b]++

					// extra rendering flags
					// 0: use sprite+256 for sides
					// 1: tint according to biome colors
					// fmt.Println(x, y, z, b, bm.nidToName[b], bs)
					var tmpl []uint32
					var layer uint8
					if int(bs) < len(bm.tmpl[b]) {
						tmpl = bm.tmpl[b][bs]
						layer = bm.layer[b][bs]
					} else {
						tmpl = bm.tmpl[b][0]
						layer = bm.layer[b][0]
					}
					// x: 8b z: 8b y: 8b   8+8+8=24b
					binary.LittleEndian.PutUint32(buf, tmpl[0]|uint32((x&255)<<16|(z&255)<<8|y))
					binary.LittleEndian.PutUint32(buf[4:], tmpl[1]|sideLight<<6|sideVis)
					bs := &bufs[x>>8+2*(z>>8)]
					bs[layer].Write(buf[:8])
				}
			}
		}
	}

	nameBase := path.Join(conf.outdir, strings.TrimSuffix(path.Base(conf.file.Name()), ".mca"))
	outLen := 0
	for bi := range bufs {
		bs := &bufs[bi]
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

	fmt.Println(conf.dir, conf.file.Name(), outLen/1024, "KiB")

	presentBlocks := []uint16{}
	for bid, count := range blockCounts {
		if count > 0 {
			presentBlocks = append(presentBlocks, uint16(bid))
		}
	}
	sort.Slice(presentBlocks, func(i, j int) bool {
		return blockCounts[presentBlocks[i]] < blockCounts[presentBlocks[j]]
	})

	for _, bid := range presentBlocks {
		if blockCounts[bid] < 100 {
			continue
		}
		if bm.layer[bid][0] == 2 {
			fmt.Println(bm.nidToName[bid], blockCounts[bid])
		}
	}

	return err
}
