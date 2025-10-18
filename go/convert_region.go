package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/klauspost/compress/gzip"

	"github.com/rmmh/cubeographer/go/region"
	"github.com/rmmh/cubeographer/go/render"
)

type regionState struct {
	openRegion region.RegionOpener
	dir        string
	bm         *region.BlockMapper
	rx, rz     int
	cdata      [1024]region.ChunkDatum
	cadj       [16]*[1024]region.ChunkDatum

	nbs [6]uint16
	nls [6]byte
	nsl [6]byte
}

func (rs *regionState) get(x, y, z int) (uint16, render.Stateval, byte, byte) {
	if y < 0 {
		return 7, 0, 0xf, 0 // bedrock
	}
	var chunk *region.ChunkDatum
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
			r, err := rs.openRegion(ap, rs.bm)
			if err != nil {
				rs.cadj[key] = &[1024]region.ChunkDatum{}
				return 0, 0, 0xf, 0
			}
			chunks, err := r.ReadChunks(wanted)
			if err != nil {
				rs.cadj[key] = &[1024]region.ChunkDatum{}
				return 0, 0, 0xf, 0
			}
			rs.cadj[key] = &chunks
		}
		chunk = &rs.cadj[key][((x&511)>>4)+((z&511)>>4)*32]
	} else {
		chunk = &rs.cdata[(x>>4)+(z>>4)*32]
	}
	ys := y >> 4
	if ys >= len(chunk.Blocks) {
		return 0, 0, 0, 0xf
	}
	o := x&15 + (z&15)*16 + (y&15)*256
	s := (x & 1) << 2
	b := chunk.Blocks[ys][o]
	bs := chunk.BlockState[ys][o]
	if ys >= len(chunk.Lights) {
		if ys >= len(chunk.LightsSky) {
			return b, bs, 0xf, 0xf
		}
		return b, bs, 0xf, (chunk.LightsSky[ys][o/2] >> s) & 0xf
	} else if ys >= len(chunk.LightsSky) {
		return b, bs, (chunk.Lights[ys][o/2] >> s) & 0xf, 0xf
	}
	return b, bs, (chunk.Lights[ys][o/2] >> s) & 0xf, (chunk.LightsSky[ys][o/2] >> s) & 0xf
}

func (rs *regionState) getLight(x, y, z int) byte {
	chunk := &rs.cdata[(x>>4)+(z>>4)*32]
	ys := y >> 4
	if ys >= len(chunk.Lights) || ys >= len(chunk.LightsSky) {
		return 15
	}
	o := ((x & 15) + (z&15)*16 + (y&15)*256) / 2
	s := (x & 1) << 2
	return chunk.Lights[ys][o]>>s + chunk.LightsSky[ys][o]>>s
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
	file        string
	bm          *region.BlockMapper
	openRegion  region.RegionOpener

	pruneCaves bool
}

func scanRegion(conf *scanRegionConfig) error {
	if !strings.HasSuffix(conf.file, ".mca") {
		return errors.New("file has wrong suffix (not .mca): " + conf.file)
	}

	openRegion := region.MakeRegion
	if conf.openRegion != nil {
		openRegion = conf.openRegion
	}

	bm := conf.bm
	r, err := openRegion(path.Join(conf.dir, conf.file), bm)
	if err != nil {
		return err
	}
	cdata, err := r.ReadChunks(nil)
	if err != nil {
		return err
	}

	rs := regionState{
		dir:        conf.dir,
		bm:         bm,
		rx:         r.Rx(),
		rz:         r.Rz(),
		cdata:      cdata,
		openRegion: openRegion,
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

	var bufs [4][render.NumRenderLayers]bytes.Buffer

	buf := make([]byte, 64)
	// TODO: emulate minecraft renderpasses -- solid, cutout (i.e. sprite), translucent (liquid)

	const subScale = 16

	// iterate bottom-to-top so that transparency (i.e. ocean water)
	// has a chance to render the bottom THEN the surface

	waterID := bm.NameToNid["minecraft:water"]

	blockCounts := make([]int, len(bm.Tmpl))

	for y := 0; y <= 320; y++ {
		for z := 0; z < 512; z++ {
			// skipping empty rows is a significant speedup for empty regions
			minX := 0
			for minX < 512 && cdata[(minX>>4)+(z>>4)*32].Blocks == nil {
				minX += 16
			}
			if minX == 512 {
				z += 15
				continue
			}
			for x := minX; x < 512; x++ {
				if cdata[(x>>4)+(z>>4)*32].Blocks == nil {
					x += 15
				}

				if conf.pruneCaves {
					chunkletVis := &chunkVis[(x>>4)+(z>>4)*32+(y>>4)*1024]
					if chunkletVis.dirReachable == 0 && (y < 64) { // || !lit[(y/lr)*512*512/16+z/lr*512/lr+x/lr]) {
						// cavey-elimination
						continue
					}
				}

				chunk := &cdata[(x>>4)+(z>>4)*32]
				if len(chunk.Blocks) < y>>4 {
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
						if nb == 0 || (nb != waterID && !bm.IsSolid(nb)) {
							sideVis |= 1 << i
						}
					} else if !bm.IsSolid(nb) {
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
					if int(bs) < len(bm.Tmpl[b]) {
						tmpl = bm.Tmpl[b][bs]
						layer = bm.Layer[b][bs]
					} else {
						tmpl = bm.Tmpl[b][0]
						layer = bm.Layer[b][0]
					}

					pos := uint32((x&255)<<16 | (z&255)<<8 | y)
					blen := 0

					for i := 0; i < len(tmpl); i += 2 {
						// x: 8b z: 8b y: 8b   8+8+8=24b
						if sideVis&tmpl[i+1] != 0 {
							binary.LittleEndian.PutUint32(buf[blen:], tmpl[i]|pos)
							binary.LittleEndian.PutUint32(buf[blen+4:], tmpl[i+1]&^0b111111|sideLight<<6|(sideVis&tmpl[i+1]))
							blen += 8
						}
					}
					if blen > 0 {
						bufs[x>>8+2*(z>>8)][layer].Write(buf[:blen])
					}
				}
			}
		}
	}

	if _, err := os.Stat(conf.outdir); os.IsNotExist(err) {
		os.MkdirAll(conf.outdir, 0755)
	}

	nameBase := path.Join(conf.outdir, strings.TrimSuffix(path.Base(conf.file), ".mca"))
	outLen := 0
	outLenComp := int64(0)
	// note: the gzip.BestCompression level is 4x slower and <1% smaller for our files
	outComp := gzip.NewWriter(nil)
	for bi := range bufs {
		bs := &bufs[bi]
		out, err := os.Create(fmt.Sprintf("%s.%d.cmt", nameBase, bi))
		if err != nil {
			log.Println("unable to open dest file")
			return err
		}

		outComp.Reset(out)
		outComp.Write([]byte("COMTE00\n"))

		var header (struct {
			Layers [render.NumRenderLayers]struct {
				Length int    `json:"length"`
				Name   string `json:"name"`
			} `json:"layers"`
		})

		for i, obuf := range bs {
			header.Layers[i].Length = obuf.Len()
			header.Layers[i].Name = render.LayerNames[i]
		}
		headerJSON, err := json.Marshal(header)
		if err != nil {
			log.Fatal(err)
		}
		binary.LittleEndian.PutUint32(buf, uint32(len(headerJSON)))
		outComp.Write(buf[:4])
		outComp.Write(headerJSON)

		for _, obuf := range bs {
			outLen += obuf.Len()
			outComp.Write(obuf.Bytes())
		}
		outComp.Flush()
		outComp.Close()
		bufOutLen, _ := out.Seek(0, io.SeekEnd)
		outLenComp += bufOutLen
		out.Close()
	}

	fmt.Println(conf.dir, conf.file, outLen/1024, "KiB =>", outLenComp/1024, "KiB")

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
		if render.LayerNumber(bm.Layer[bid][0]) == render.LayerCubeFallback {
			fmt.Println(bm.NidToName[bid], blockCounts[bid])
		}
	}

	return err
}
