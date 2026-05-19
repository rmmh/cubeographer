package region

import (
	"fmt"

	"github.com/rmmh/cubeographer/go/render"
)

func FakeReadRegion(path string, bm *BlockMapper, wanted []int) ([]ChunkDatum, error) {
	cdata := make([]ChunkDatum, 1024)

	if path != "r.0.0.mca" {
		return cdata, nil
	}

	for cn := 0; cn < 1024; cn++ {
		nblocks := [][]uint16{}
		nstates := [][]render.Stateval{}
		nsky := [][]byte{}

		for layer := 0; layer < 1; layer++ {
			nb := make([]uint16, 4096)
			ns := make([]render.Stateval, 4096)
			for j := 0; j < 256; j++ {
				nb[j+256] = bm.NameToNid["minecraft:grass_block"]
			}
			nblocks = append(nblocks, nb)
			nstates = append(nstates, ns)
		}

		cdata[cn] = ChunkDatum{
			Blocks:     nblocks,
			BlockState: nstates,
			LightsSky:  nsky,
			Lights:     nsky,
		}
	}

	set := func(x, y, z int, b uint16, s render.Stateval) {
		if x < 0 || x >= 512 || z < 0 || z >= 512 {
			panic(fmt.Sprintf("coord out of bounds (%d,%d)", x, z))
		}
		chunk := &cdata[x>>4+(z>>4)*32]
		o := (x % 16) + (z%16)*16 + (y%16)*256
		chunk.Blocks[y/16][o] = b
		chunk.BlockState[y/16][o] = s
	}

	bx := 32
	bz := 32

	for layer := 0; layer < int(render.NumRenderLayers); layer++ {
		qualName := []string{
			"gold_block", "diamond_block", "emerald_block", "dirt", "copper_block", "iron_block", "stone"}[layer]
		qualBlock := bm.NameToNid["minecraft:"+qualName]

		for b := 1; b < len(bm.NidToName); b++ {
			layerIdx := bm.Layer[b][0]
			if int(layerIdx) != layer {
				continue
			}

			ns := int(bm.nidToSmap[b].Max())
			nl := ns/6 + 1
			if bx+nl >= 220 {
				bx = 32
				bz += 8
			}
			if bz == 32 {
				fmt.Printf("FakeReadRegion (bz=32): block=%s layerIdx=%d layerName=%s qualBlockID=%d (%s) bx=%d nl=%d ns=%d\n",
					bm.NidToName[b], layerIdx, render.LayerNames[layerIdx], qualBlock, qualName, bx, nl, ns)
			}
			for i := 0; i <= ns; i++ {
				set(bx+i%nl, 3+(i%nl+i/nl)%2, bz+i/nl, uint16(b), render.Stateval(i))
				set(bx+i%nl, 1, bz+i/nl, qualBlock, 0)
			}
			bx += nl
			bx += 2
		}
	}

	return cdata, nil
}
