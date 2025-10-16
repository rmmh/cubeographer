package main

import (
	"encoding/json"

	"github.com/rmmh/cubeographer/go/resourcepack"
)

// TODO: this should probably go back to AOS instead of this SOA form
type blockMapper struct {
	meta               blockentryMetadata
	solid              []uint64
	blockstateToNid    [4096]uint16
	blockstateToNstate [4096]stateval
	nameToNid          map[string]uint16
	nidToName          []string
	nidToSmap          []statemap
	tmpl               [][][]uint32
	layer              [][]uint8
}

func loadBlockMapper(buf []byte) (*blockMapper, error) {
	bm := &blockMapper{
		nameToNid: map[string]uint16{},
		nidToName: []string{""},
		nidToSmap: []statemap{nil},
		solid:     []uint64{},
		tmpl:      [][][]uint32{nil},
		layer:     [][]uint8{nil},
	}

	err := json.Unmarshal(buf, &bm.meta)
	if err != nil {
		return nil, err
	}

	count := 1
	for _, b := range bm.meta.Blocks {
		n := uint16(count)
		if b.Name == "air" || b.Name == "cave_air" || b.Name == "void_air" {
			n = 0
		} else {
			count++
		}
		bm.nameToNid["minecraft:"+b.Name] = n
		smap := buildStateMap(b.States)
		if int(n) >= len(bm.nidToName) {
			bm.nidToName = append(bm.nidToName, b.Name)
			bm.nidToSmap = append(bm.nidToSmap, smap)
		} else {
			bm.nidToName[n] = b.Name
		}
		if n > 0 {
			if int(n>>6) >= len(bm.solid) {
				bm.solid = append(bm.solid, 0)
			}
			if b.Solid {
				bm.solid[n>>6] |= 1 << (n & 63)
			}
			tmpls := [][]uint32{}
			layers := []uint8{}
			for _, model := range b.Templates {
				tmpls = append(tmpls, model.Template)
				layers = append(layers, uint8(model.Layer))
			}
			bm.tmpl = append(bm.tmpl, tmpls)
			bm.layer = append(bm.layer, layers)
		}
	}

	for blockstate, data := range resourcepack.BlockstateMap {
		nid := bm.nameToNid["minecraft:"+data.Name]
		bm.blockstateToNid[blockstate] = nid
		bm.blockstateToNstate[blockstate] = bm.nidToSmap[nid].getState(data.Properties)
	}

	return bm, nil
}

func (bm *blockMapper) isSolid(b uint16) bool {
	// instead of trying to track every transparent block, keep a list of *known* solid blocks
	return bm.solid[b>>6]&(1<<(b&63)) != 0
}
