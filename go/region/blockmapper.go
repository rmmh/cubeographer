package region

import (
	"encoding/json"
	"image/color"
	"strconv"
	"strings"

	"github.com/rmmh/cubeographer/go/render"
	"github.com/rmmh/cubeographer/go/resourcepack"
)

// TODO: this should probably go back to AOS instead of this SOA form
type BlockMapper struct {
	meta             render.BlockEntryMetadata
	migrateBlockMaps []migrateVersionedBlockMap

	solid              []uint64
	blockstateToNid    [4096]uint16
	blockstateToNstate [4096]render.Stateval
	NameToNid          map[string]uint16
	NidToName          []string
	nidToSmap          []render.Statemap
	Tmpl               [][][][]uint32
	Layer              [][][]uint8
	NoShade            [][][]bool
	Colors             [][][]color.RGBA
}

func LoadBlockMapper(buf []byte) (*BlockMapper, error) {
	bm := &BlockMapper{
		NameToNid: map[string]uint16{},
		NidToName: []string{""},
		nidToSmap: []render.Statemap{nil},
		solid:     []uint64{},
		Tmpl:      [][][][]uint32{nil},
		Layer:     [][][]uint8{nil},
		NoShade:   [][][]bool{nil},
		Colors:    [][][]color.RGBA{nil},
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
		bm.NameToNid[b.Name] = n
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
			tmpls := [][][]uint32{}
			layers := [][]uint8{}
			noshades := [][]bool{}
			colors := [][]color.RGBA{}
			for i, variant := range b.Templates {
				vtmpls := [][]uint32{}
				vlayers := []uint8{}
				vnoshades := []bool{}
				for _, model := range variant {
					vtmpls = append(vtmpls, model.Template)
					vlayers = append(vlayers, uint8(model.Layer))
					vnoshades = append(vnoshades, model.NoShade)
				}
				tmpls = append(tmpls, vtmpls)
				layers = append(layers, vlayers)
				noshades = append(noshades, vnoshades)
				colors = append(colors, convertColors(b.Colors[i]))
			}
			bm.Tmpl = append(bm.Tmpl, tmpls)
			bm.Layer = append(bm.Layer, layers)
			bm.NoShade = append(bm.NoShade, noshades)
			bm.Colors = append(bm.Colors, colors)
		}
	}

	for blockstate, data := range resourcepack.BlockstateMap {
		nid := bm.NameToNid["minecraft:"+data.Name]
		bm.blockstateToNid[blockstate] = nid
		bm.blockstateToNstate[blockstate] = bm.nidToSmap[nid].Get(data.Properties)
	}

	bm.precalculateMigrations()

	return bm, nil
}

func convertColor(s string) color.RGBA {
	c := color.RGBA{}
	if s == "" {
		return c
	}
	// convert from hex like ffff00 to RGBA
	r, err := strconv.ParseUint(s[0:2], 16, 8)
	if err != nil {
		panic(err)
	}
	g, err := strconv.ParseUint(s[2:4], 16, 8)
	if err != nil {
		panic(err)
	}
	b, err := strconv.ParseUint(s[4:6], 16, 8)
	if err != nil {
		panic(err)
	}
	c.R = uint8(r)
	c.G = uint8(g)
	c.B = uint8(b)
	c.A = 255
	return c
}

func convertColors(s string) []color.RGBA {
	if s == "" {
		return []color.RGBA{{0, 0, 0, 0}}
	}
	parts := strings.Split(s, ",")
	res := make([]color.RGBA, len(parts))
	for i, part := range parts {
		res[i] = convertColor(part)
	}
	return res
}

func (bm *BlockMapper) IsSolid(b uint16) bool {
	// instead of trying to track every transparent block, keep a list of *known* solid blocks
	return bm.solid[b>>6]&(1<<(b&63)) != 0
}

func (bm *BlockMapper) GetStateval(nid uint16, props []string) render.Stateval {
	if int(nid) < len(bm.nidToSmap) && bm.nidToSmap[nid] != nil {
		return bm.nidToSmap[nid].GetList(props)
	}
	return 0
}
