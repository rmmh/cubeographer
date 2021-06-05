package main

import (
	"fmt"
	"strconv"
)

type fakeRegion struct {
	path   string
	rx, rz int
	bm     *blockMapper
}

func testWorldOpener(path string, bm *blockMapper) (Region, error) {
	m := regionMatchRE.FindStringSubmatch(path)
	var rx, rz int
	if m != nil {
		rx, _ = strconv.Atoi(m[1])
		rz, _ = strconv.Atoi(m[2])
	} else {
		fmt.Println("WARN: region file doesn't match expected r.XX.ZZ format")
	}

	r := &fakeRegion{path, rx, rz, bm}
	return r, nil
}

func (r *fakeRegion) ReadChunks(wanted []int) ([1024]chunkDatum, error) {
	var cdata [1024]chunkDatum

	for cn := 0; cn < 1024; cn++ {
		nblocks := [][]uint16{}
		nstates := [][]uint8{}
		nsky := [][]byte{}

		for layer := 0; layer < 1; layer++ {
			nb := make([]uint16, 4096)
			ns := make([]uint8, 4096)
			nl := make([]byte, 4096)

			for j := 0; j < 256; j++ {
				nb[j] = r.bm.nameToNid["minecraft:grass_block"]
			}

			if r.rx >= 0 {
				for j := 0; j < 256; j++ {
					x := j%16 + (cn%32)*16 + r.rx*512
					z := j/16 + (cn/32)*16
					if x < 0 || x >= len(r.bm.nidToName) || (z > 0 && z > int(r.bm.nidToSmap[x].max())) {
						if z == 0 && x < len(r.bm.nidToName) {
							fmt.Println("???", x, r.bm.nidToName[x])
						}
						continue
					}
					if x > 0 && r.bm.layer[x][0] != uint8(layerCubeFallback) {
						nb[x%16+0x400] = r.bm.nameToNid["minecraft:gold_block"]
					} else {
						nb[x%16+0x400] = r.bm.nameToNid["minecraft:iron_block"]
					}
					nb[256+j] = uint16(x)
					ns[256+j] = uint8(z)
					nl[256+j] = 255
				}
			}

			nblocks = append(nblocks, nb)
			nstates = append(nstates, ns)
		}

		cdata[cn] = chunkDatum{
			blocks:     nblocks,
			blockState: nstates,
			lightsSky:  nsky,
			lights:     nsky,
		}
	}

	return cdata, nil
}

func (r *fakeRegion) Rx() int { return r.rx }
func (r *fakeRegion) Rz() int { return r.rz }
