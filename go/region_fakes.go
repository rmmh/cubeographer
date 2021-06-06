package main

import (
	"errors"
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

	if rx != 0 || rz != 0 {
		return nil, errors.New("out of bounds")
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
			for j := 0; j < 256; j++ {
				nb[j+256] = r.bm.nameToNid["minecraft:grass_block"]
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

	set := func(x, y, z int, b uint16, s uint8) {
		if x < 0 || x >= 512 || z < 0 || z >= 512 {
			panic(fmt.Sprintf("coord out of bounds (%d,%d)", x, z))
		}
		chunk := &cdata[x>>4+(z>>4)*32]
		o := (x % 16) + (z%16)*16 + (y%16)*256
		chunk.blocks[y/16][o] = b
		chunk.blockState[y/16][o] = s
	}

	bx := 32
	bz := 32

	for b := 1; b < len(r.bm.nidToName); b++ {
		ns := int(r.bm.nidToSmap[b].max())
		nl := ns/6 + 1
		if bx+nl >= 220 {
			bx = 32
			bz += 8
		}
		qualBlock := r.bm.nameToNid["minecraft:"+[]string{
			"gold_block", "diamond_block", "emerald_block", "dirt", "iron_block"}[r.bm.layer[b][0]]]
		for i := 0; i <= ns; i++ {
			set(bx+i%nl, 3+(i%nl+i/nl)%2, bz+i/nl, uint16(b), uint8(i))
			set(bx+i%nl, 1, bz+i/nl, qualBlock, 0)
		}
		bx += nl
		bx += 2
	}

	return cdata, nil
}

func (r *fakeRegion) Rx() int { return r.rx }
func (r *fakeRegion) Rz() int { return r.rz }
