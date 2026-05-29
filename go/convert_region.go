package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/bits"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/klauspost/compress/gzip"

	"github.com/rmmh/cubeographer/go/region"
	"github.com/rmmh/cubeographer/go/render"
	rp "github.com/rmmh/cubeographer/go/resourcepack"
	"github.com/rmmh/cubeographer/go/zvcr"
)

type regionState struct {
	openRegion  region.ReadRegionFunc
	dir         string
	ext         string
	bm          *region.BlockMapper
	rx, rz      int
	cdata       []region.ChunkDatum
	cadj        [16][]region.ChunkDatum
	minSectionY int // section Y of chunk.Blocks[0] (= *region.MinWorldY >> 4)

	nbs [6]uint16
	nls [6]byte
	nsl [6]byte
}

func (rs *regionState) get(x, y, z int) (uint16, render.Stateval, byte, byte) {
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
			ap := path.Join(rs.dir, fmt.Sprintf("r.%d.%d%s", ox, oz, rs.ext))
			chunks, err := rs.openRegion(ap, rs.bm, wanted)
			if err != nil {
				rs.cadj[key] = make([]region.ChunkDatum, 1024)
				return 0, 0, 0xf, 0
			}
			rs.cadj[key] = chunks
		}
		chunk = &rs.cadj[key][((x&511)>>4)+((z&511)>>4)*32]
	} else {
		chunk = &rs.cdata[(x>>4)+(z>>4)*32]
	}
	ys := (y >> 4) - rs.minSectionY
	if ys < 0 || ys >= len(chunk.Blocks) {
		return 0, 0, 0xf, 0xf
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
	ys := (y >> 4) - rs.minSectionY
	if ys < 0 || ys >= len(chunk.Lights) || ys >= len(chunk.LightsSky) {
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
	readRegion  region.ReadRegionFunc

	prune bool
	debug string
	mode  string
}

func scanRegion(conf *scanRegionConfig) error {
	if conf.dir == "test" {
		conf.readRegion = region.FakeReadRegion
	}
	ext := path.Ext(conf.file)
	readRegion := conf.readRegion
	if readRegion == nil {
		if ext == ".zvcr3" {
			readRegion = zvcr.ReadZVCR
		} else if ext == ".mca" {
			readRegion = region.ReadRegion
		} else {
			return errors.New("file has wrong suffix (not .mca or .zvcr3): " + conf.file)
		}
	}

	bm := conf.bm
	regionPath := path.Join(conf.dir, conf.file)
	cdata, err := readRegion(regionPath, bm, nil)
	if err != nil {
		return err
	}
	var regionSize int64
	if st, err := region.Stat(regionPath); err == nil {
		regionSize = st.Size()
	} else if conf.readRegion == nil {
		return err
	}

	rx, rz, err := region.ParseRegionPath(conf.file)
	if err != nil {
		return err
	}

	rs := regionState{
		dir:         conf.dir,
		ext:         ext,
		bm:          bm,
		rx:          rx,
		rz:          rz,
		cdata:       cdata,
		openRegion:  readRegion,
		minSectionY: *region.MinWorldY >> 4,
	}

	mode := conf.mode
	if mode == "" {
		mode = "full"
	}

	if mode == "full" {
		var chunkVis *blockVis

		if conf.prune {
			chunkVis = makeBlockvis(cdata, bm, visTriakisOctahedral)
		}

		const sliceHeight = 256
		minWorldY := *region.MinWorldY
		numSlices := (320 - minWorldY + sliceHeight - 1) / sliceHeight

		// Index: xzQuad (0-3) + ySlice*4
		bufs := make([][render.NumRenderLayers]bytes.Buffer, 4*numSlices)
		faceCounts := make([][render.NumRenderLayers]int, 4*numSlices)

		buf := make([]byte, 64)
		// TODO: emulate minecraft renderpasses -- solid, cutout (i.e. sprite), translucent (liquid)

		// iterate bottom-to-top so that transparency (i.e. ocean water)
		// has a chance to render the bottom THEN the surface

		waterID := bm.NameToNid["minecraft:water"]

		blockCounts := make([]int, len(bm.Tmpl))

		for y := minWorldY; y < 320; y++ {
			ySlice := (y - minWorldY) / sliceHeight
			localY := y - (minWorldY + ySlice*sliceHeight)
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
						continue
					}

					chunk := &cdata[(x>>4)+(z>>4)*32]
					if len(chunk.Blocks) <= (y>>4)-rs.minSectionY {
						continue
					}

					if conf.prune {
						if !chunkVis.isVisible(x, y, z) {
							continue
						}
					}

					b, bs, bl, bsl := rs.get(x, y, z)

					if b == 0 {
						continue
					}

					// waterlogged blocks render as *two* blocks on top of each other,
					// so we have to be able to process multiple blocks per coordinate!
					blocksToProcess := [2]uint16{b, 0}
					statesToProcess := [2]render.Stateval{bs, 0}
					numBlocks := 1

					if b != waterID && bm.IsWaterloggable(b) && bm.IsWaterlogged(b, bs) {
						blocksToProcess[1] = waterID
						statesToProcess[1] = 0 // default state for water
						numBlocks = 2
					}

					ns, nl, nsl := rs.neighs(x, y, z)

					for idx := 0; idx < numBlocks; idx++ {
						b = blocksToProcess[idx]
						bs = statesToProcess[idx]

						sideVis := uint32(0)
						sideLight := uint32(0)
						for i, nb := range ns {
							if b == waterID {
								if nb == 0 || (nb != waterID && !bm.IsSolid(nb)) {
									isWaterlogged := false
									if nb != 0 {
										nx, ny, nz := x, y, z
										switch i {
										case 0:
											nx--
										case 1:
											nx++
										case 2:
											nz++
										case 3:
											nz--
										case 4:
											ny++
										case 5:
											ny--
										}
										_, nbState, _, _ := rs.get(nx, ny, nz)
										if bm.IsWaterlogged(nb, nbState) {
											isWaterlogged = true
										}
										if conf.debug != "" && strings.Contains(conf.debug, "waterlogged") {
											fmt.Printf("[debug waterlogged] Water neighbor is %s (stateval %d), IsWaterlogged=%t\n",
												bm.NidToName[nb], nbState, isWaterlogged)
										}
									}
									if isWaterlogged {
										// Neighbor is waterlogged, cull this water face
									} else {
										sideVis |= 1 << i
									}
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
							var tmpls [][]uint32
							var layers []uint8
							var noshades []bool
							stateIdx := int(bs)
							if stateIdx >= len(bm.Tmpl[b]) {
								stateIdx = 0
							}
							tmpls = bm.Tmpl[b][stateIdx]
							layers = bm.Layer[b][stateIdx]
							if stateIdx < len(bm.NoShade[b]) {
								noshades = bm.NoShade[b][stateIdx]
							}

							pos := uint32(x&255)<<16 | uint32(z&255)<<8 | uint32(localY)

							// uniform light for no-shade elements: max(blocklight, skylight) broadcast to all faces
							uniformLight := uint32(max(bl, bsl)) * 0x111111 // replicate 4-bit value across all 6 faces (24 bits)

							for eIdx, tmpl := range tmpls {
								layer := layers[eIdx]
								noShade := eIdx < len(noshades) && noshades[eIdx]
								eSideLight := sideLight
								if noShade {
									eSideLight = uniformLight
								}
								blen := 0

								for i := 0; i < len(tmpl); i += 2 {
									// x: 8b z: 8b y: 8b   8+8+8=24b
									var visibleFaces uint32
									if render.LayerNumber(layer) == render.LayerCuboid {
										cullableMask := (tmpl[i+1] >> 18) & 0b111111
										visibleFaces = tmpl[i+1] & (sideVis | ^cullableMask) & 0b111111
									} else {
										visibleFaces = sideVis & tmpl[i+1]
									}

									if visibleFaces != 0 {
										binary.LittleEndian.PutUint32(buf[blen:], tmpl[i]|pos)
										var yVal uint32
										if render.LayerNumber(layer) == render.LayerCuboid {
											cuboidSideLight := ((eSideLight >> 1) & 7) |
												((eSideLight >> 2) & 0x38) |
												((eSideLight >> 3) & 0x1C0) |
												((eSideLight >> 4) & 0xE00) |
												((eSideLight >> 5) & 0x7000) |
												((eSideLight >> 6) & 0x38000)
											// Cuboid template attr.y:
											// - Bits 24-31: top 8 bits of 16-bit cuboid ID.
											// - Bits 0-5: face presence mask.
											// We clear bottom 6 bits of the template, OR in cuboidSideLight (18 bits) at bits 6-23,
											// and OR in the visibility mask (visibleFaces).
											yVal = (tmpl[i+1] & ^uint32(0b111111)) | (cuboidSideLight << 6) | visibleFaces
										} else {
											yVal = (tmpl[i+1] & ^uint32(0b111111)) | (eSideLight << 6) | visibleFaces
										}
										binary.LittleEndian.PutUint32(buf[blen+4:], yVal)
										blen += 8
										faceCounts[x>>8+2*(z>>8)+ySlice*4][layer] += bits.OnesCount32(visibleFaces)
									}
								}
								if blen > 0 {
									bufs[x>>8+2*(z>>8)+ySlice*4][layer].Write(buf[:blen])
								}
							}
						}
					}
				}
			}
		}

		if _, err := os.Stat(conf.outdir); os.IsNotExist(err) {
			os.MkdirAll(conf.outdir, 0755)
		}

		baseName := path.Base(conf.file)
		baseName = strings.TrimSuffix(baseName, ext)
		nameBase := path.Join(conf.outdir, baseName)
		outLen := 0
		outLenComp := int64(0)
		// note: the gzip.BestCompression level is 4x slower and <1% smaller for our files
		outComp := gzip.NewWriter(nil)

		type layerHeader struct {
			Length int    `json:"length"`
			Faces  int    `json:"faces"`
			Name   string `json:"name"`
		}
		type regionletHeader struct {
			YOffset int                                 `json:"y_offset"`
			Layers  [render.NumRenderLayers]layerHeader `json:"layers"`
		}
		type cmtHeader struct {
			Regionlets []regionletHeader `json:"regionlets"`
		}

		for bi := 0; bi < 4; bi++ {
			out, err := os.Create(fmt.Sprintf("%s.%d.cmt", nameBase, bi))
			if err != nil {
				log.Println("unable to open dest file")
				return err
			}

			outComp.Reset(out)
			outComp.Write([]byte("COMTE00\n"))

			header := cmtHeader{}
			for ys := 0; ys < numSlices; ys++ {
				bufIdx := bi + ys*4
				total := 0
				for i := range bufs[bufIdx] {
					total += bufs[bufIdx][i].Len()
				}
				if total == 0 {
					continue
				}
				rlet := regionletHeader{YOffset: minWorldY + ys*sliceHeight}
				for i := range rlet.Layers {
					rlet.Layers[i].Length = bufs[bufIdx][i].Len()
					rlet.Layers[i].Faces = faceCounts[bufIdx][i]
					rlet.Layers[i].Name = render.LayerNames[i]
				}
				header.Regionlets = append(header.Regionlets, rlet)
			}
			headerJSON, err := json.Marshal(header)
			if err != nil {
				log.Fatal(err)
			}
			binary.LittleEndian.PutUint32(buf, uint32(len(headerJSON)))
			outComp.Write(buf[:4])
			outComp.Write(headerJSON)

			for ys := 0; ys < numSlices; ys++ {
				bufIdx := bi + ys*4
				total := 0
				for i := range bufs[bufIdx] {
					total += bufs[bufIdx][i].Len()
				}
				if total == 0 {
					continue
				}
				for _, obuf := range bufs[bufIdx] {
					outLen += obuf.Len()
					outComp.Write(obuf.Bytes())
				}
			}
			outComp.Flush()
			outComp.Close()
			bufOutLen, _ := out.Seek(0, io.SeekEnd)
			outLenComp += bufOutLen
			out.Close()
		}

		hasDebug := func(opt string) bool {
			if conf.debug == "" {
				return false
			}
			for _, o := range strings.Split(conf.debug, ",") {
				if o == opt {
					return true
				}
			}
			return false
		}

		if hasDebug("blockcount") {
			presentBlocks := []uint16{}
			for bid, count := range blockCounts {
				if count > 0 {
					presentBlocks = append(presentBlocks, uint16(bid))
				}
			}
			sort.Slice(presentBlocks, func(i, j int) bool {
				return blockCounts[presentBlocks[i]] > blockCounts[presentBlocks[j]]
			})
			fmt.Printf("Block counts for %s:\n", conf.file)
			for _, bid := range presentBlocks {
				fmt.Printf("  %s: %d\n", rp.RemoveDefaultPrefix(bm.NidToName[bid]), blockCounts[bid])
			}
		}
	}

	lodErr := computeRegionLODs(&rs, conf)
	if lodErr != nil {
		log.Printf("error computing region LODs for %s: %v", conf.file, lodErr)
	}

	// Mode-aware summary print
	{
		baseName := path.Base(conf.file)
		baseName = strings.TrimSuffix(baseName, ext)

		var parts []string
		parts = append(parts, fmt.Sprintf("%s %s %d KiB region", conf.dir, conf.file, regionSize/1024))

		if mode == "full" {
			var cmtSize int64
			for bi := 0; bi < 4; bi++ {
				cmtPath := path.Join(conf.outdir, fmt.Sprintf("%s.%d.cmt", baseName, bi))
				if st, err := os.Stat(cmtPath); err == nil {
					cmtSize += st.Size()
				}
			}
			parts = append(parts, fmt.Sprintf("cmt %d KiB", cmtSize/1024))
		}

		if mode == "full" || mode == "lod" {
			binPath := path.Join(conf.outdir, "lods", fmt.Sprintf("%s.bin", baseName))
			if st, err := os.Stat(binPath); err == nil {
				parts = append(parts, fmt.Sprintf("bin %d KiB", st.Size()/1024))
			}
		}

		pngPath := path.Join(conf.outdir, "tiles", fmt.Sprintf("%s.png", baseName))
		if st, err := os.Stat(pngPath); err == nil {
			parts = append(parts, fmt.Sprintf("png %d KiB", st.Size()/1024))
		}

		fmt.Println(strings.Join(parts, ", "))
	}

	return err
}
