package region

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"

	"github.com/rmmh/cubeographer/go/render"
)

func FakeReadRegion(path string, bm *BlockMapper, wanted []int) ([]ChunkDatum, error) {
	cdata := make([]ChunkDatum, 1024)

	if path != "r.0.0.mca" {
		return cdata, nil
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

	for cn := 0; cn < 1024; cn++ {
		nblocks := [][]uint16{}
		nstates := [][]render.Stateval{}
		nsky := [][]byte{}

		for layer := 0; layer < 1; layer++ {
			nb := make([]uint16, 4096)
			ns := make([]render.Stateval, 4096)
			for i := range 4096 {
				nb[i] = bm.NameToNid["minecraft:air"]
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

	grass := bm.NameToNid["minecraft:grass_block"]

	for x := range 256 {
		for z := range 256 {
			set(x, 1, z, grass, 0)
		}
	}

	bx := 32
	bz := 32

	layerMap := map[int][]int{}

	layerBlocks := [][]string{{"MISSING"}}

	for b := 1; b < len(bm.NidToName); b++ {
		layerMask := 0
		for _, ls := range bm.Layer[b] {
			for _, l := range ls {
				layerMask |= 1 << l
			}
		}
		if layerMask == 0 {
			// fmt.Println("layerMask == 0", bm.NidToName[b], bm.Layer[b])
			layerBlocks[0] = append(layerBlocks[0], bm.NidToName[b])
			continue
		}
		layerMap[layerMask] = append(layerMap[layerMask], b)
	}

	layerMasks := []int{}
	for layerMask := range 1 << render.NumRenderLayers {
		if len(layerMap[layerMask]) == 0 {
			continue
		}
		layerMasks = append(layerMasks, layerMask)
	}
	sort.Slice(layerMasks, func(i, j int) bool {
		// sort order: single layer masks (other than FALLBACK), then multi-layer masks without FALLBACK,
		// then multi-layer masks with FALLBACK, then FALLBACK

		// Rule 3: Pure fallback always goes to the very end
		fallbackMask := 1 << render.LayerCubeFallback
		iIsPureFallback := layerMasks[i] == fallbackMask
		jIsPureFallback := layerMasks[j] == fallbackMask
		if iIsPureFallback != jIsPureFallback {
			return jIsPureFallback // The one that is NOT pure fallback comes first
		}

		// Rule 1: Single layers come first
		iIsSingleLayer := (layerMasks[i] & (layerMasks[i] - 1)) == 0
		jIsSingleLayer := (layerMasks[j] & (layerMasks[j] - 1)) == 0
		if iIsSingleLayer != jIsSingleLayer {
			return iIsSingleLayer
		}

		// Rule 2: Multi-layers without fallback come before multi-layers with fallback
		iHasFallback := (layerMasks[i] & fallbackMask) != 0
		jHasFallback := (layerMasks[j] & fallbackMask) != 0
		if iHasFallback != jHasFallback {
			return jHasFallback // The one WITHOUT fallback comes first
		}

		return layerMasks[i] < layerMasks[j]
	})

	for _, layerMask := range layerMasks {
		layer := bits.TrailingZeros(uint(layerMask))

		// by default, we use a minecraft block that is two mixed metals:
		qualName := "weathered_cut_copper"
		if layerMask&(layerMask-1) == 0 { // power of 2 == block represented by single layer
			qualName = []string{
				"gold_block", "diamond_block", "emerald_block", "dirt", "copper_block", "andesite"}[layer]
		} else if (layerMask & (1 << render.LayerCubeFallback)) != 0 {
			qualName = "cobblestone"
		}

		layers := []string{}
		for i := range render.LayerNames {
			if (layerMask & (1 << i)) != 0 {
				layers = append(layers, render.LayerNames[i])
			}
		}
		layerBlocks = append(layerBlocks, []string{strings.Join(layers, "_")})

		qualBlock := bm.NameToNid["minecraft:"+qualName]

		for _, b := range layerMap[layerMask] {
			ns := []int{}
			seen := map[string]bool{}
			maxState := int(bm.nidToSmap[b].Max())
			for i := 0; i <= maxState; i++ {
				tmplIdx := i
				if len(bm.Tmpl[b]) == 1 {
					tmplIdx = 0
				}
				key := fmt.Sprintf("%v", bm.Tmpl[b][tmplIdx])
				if seen[key] || key == "[]" {
					continue
				}
				seen[key] = true
				ns = append(ns, i)
			}
			layerBlocks[len(layerBlocks)-1] = append(layerBlocks[len(layerBlocks)-1], bm.NidToName[b])
			nl := len(ns)/6 + 1
			if bx+nl >= 220 {
				bx = 32
				bz += 8
			}
			for i, s := range ns {
				set(bx+i%nl, 3+(i%nl+i/nl)%2, bz+i/nl, uint16(b), render.Stateval(s))
				set(bx+i%nl, 1, bz+i/nl, qualBlock, 0)
			}
			bx += nl
			bx += 2
		}
	}

	for _, lbs := range layerBlocks {
		if len(lbs) > 3 {
			fmt.Printf("%s (%d): %s ... %s\n", lbs[0], len(lbs)-1, lbs[1], lbs[len(lbs)-1])
		} else {
			fmt.Printf("%s: %s\n", lbs[0], strings.Join(lbs[1:], " "))
		}
	}

	return cdata, nil
}
