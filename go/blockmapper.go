package main

import (
	"encoding/json"
)

type blockMapper struct {
	meta            blockentryMetadata
	solid           []uint64
	blockstateToNid [4096]uint16
	nameToNid       map[string]uint16
	tmpl            [][]uint32
	layer           []uint8
}

func loadBlockMapper(buf []byte) (*blockMapper, error) {
	bm := &blockMapper{
		nameToNid: map[string]uint16{},
		solid:     []uint64{},
		tmpl:      [][]uint32{nil},
		layer:     []uint8{0},
	}

	err := json.Unmarshal(buf, &bm.meta)
	if err != nil {
		return nil, err
	}

	numBlocks := 0
	count := 1
	for layer, bs := range bm.meta.Layers {
		numBlocks += len(bs)
		for _, b := range bs {
			n := uint16(count)
			if b.Name == "air" || b.Name == "cave_air" || b.Name == "void_air" {
				n = 0
			} else {
				count++
			}
			bm.nameToNid["minecraft:"+b.Name] = n
			if n > 0 {
				if int(n>>6) >= len(bm.solid) {
					bm.solid = append(bm.solid, 0)
				}
				if b.Solid {
					bm.solid[n>>6] |= 1 << (n & 63)
				}
				bm.tmpl = append(bm.tmpl, b.Template)
				bm.layer = append(bm.layer, uint8(layer))
			}
		}
	}

	for blockstate, data := range blockstateMap {
		bm.blockstateToNid[blockstate] = bm.nameToNid["minecraft:"+data.name]
	}

	return bm, nil
}

func (bm *blockMapper) isSolid(b uint16) bool {
	// instead of trying to track every transparent block, keep a list of *known* solid blocks
	return bm.solid[b>>6]&(1<<(b&63)) != 0
}
