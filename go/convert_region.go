package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"path"
	"os"
	"regexp"
	"strconv"
	"strings"
)

func isSolid(b byte) bool {
	// instead of trying to track every transparent block, keep a list of *known* solid blocks
	// this data was stolen from Overviewer
	return [...]uint64{
		2811352310602264766 | (1 << 11 /* still lava */), 9079609371303151104, 11389998976731682, 11529215046034675456,
	}[b>>6]&(1<<(b&63)) != 0
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
