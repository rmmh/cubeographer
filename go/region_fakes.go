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

		for layer := 0; layer < 1; layer++ {
			nb := make([]uint16, 4096)
			ns := make([]uint8, 4096)

			for j := 0; j < 256; j++ {
				nb[j] = r.bm.nameToNid["minecraft:grass_block"]
			}

			if r.rx >= 0 {
				for j := 0; j < 256; j++ {
					x := j%16 + (cn%16)*16 + r.rx*512
					z := j/16 + (cn/16)*16
					if x < 0 || x >= len(r.bm.nidToName) || (z > 0 && z > int(r.bm.nidToSmap[x].max())) {
						continue
					}
					nb[256+j] = uint16(x)
					ns[256+j] = uint8(z)
				}
			}

			nblocks = append(nblocks, nb)
			nstates = append(nstates, ns)
		}

		cdata[cn] = chunkDatum{
			blocks:     nblocks,
			blockState: nstates,
		}
	}

	return cdata, nil
}

func (r *fakeRegion) Rx() int { return r.rx }
func (r *fakeRegion) Rz() int { return r.rz }