package region

import (
	"encoding/json"

	"github.com/rmmh/cubeographer/go/render"
	"github.com/rmmh/cubeographer/go/resourcepack"
)

// TODO: this should probably go back to AOS instead of this SOA form
type BlockMapper struct {
	meta               render.BlockEntryMetadata
	solid              []uint64
	blockstateToNid    [4096]uint16
	blockstateToNstate [4096]render.Stateval
	NameToNid          map[string]uint16
	NidToName          []string
	nidToSmap          []render.Statemap
	Tmpl               [][][]uint32
	Layer              [][]uint8
}

func LoadBlockMapper(buf []byte) (*BlockMapper, error) {
	bm := &BlockMapper{
		NameToNid: map[string]uint16{},
		NidToName: []string{""},
		nidToSmap: []render.Statemap{nil},
		solid:     []uint64{},
		Tmpl:      [][][]uint32{nil},
		Layer:     [][]uint8{nil},
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
		bm.NameToNid["minecraft:"+b.Name] = n
		smap := render.BuildStateMap(b.States)
		if int(n) >= len(bm.NidToName) {
			bm.NidToName = append(bm.NidToName, b.Name)
			bm.nidToSmap = append(bm.nidToSmap, smap)
		} else {
			bm.NidToName[n] = b.Name
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
			bm.Tmpl = append(bm.Tmpl, tmpls)
			bm.Layer = append(bm.Layer, layers)
		}
	}

	for blockstate, data := range resourcepack.BlockstateMap {
		nid := bm.NameToNid["minecraft:"+data.Name]
		bm.blockstateToNid[blockstate] = nid
		bm.blockstateToNstate[blockstate] = bm.nidToSmap[nid].Get(data.Properties)
	}

	return bm, nil
}

func (bm *BlockMapper) IsSolid(b uint16) bool {
	// instead of trying to track every transparent block, keep a list of *known* solid blocks
	return bm.solid[b>>6]&(1<<(b&63)) != 0
}
