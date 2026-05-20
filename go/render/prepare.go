package render

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"sort"

	rp "github.com/rmmh/cubeographer/go/resourcepack"
	"github.com/samber/lo"
)

type TextureType int

const (
	TexUnknown TextureType = iota
	TexOpaque
	TexCutout
	TexTranslucent
)

/*
Render Layers and Packing Formats:

Each block model is classified into one of the following render layers.
During conversion, instances are packed into a 64-bit attribute structure:
  - `attr.x` (32-bit uint): Packed position and texture/block ID.
  - `attr.y` (32-bit uint): Packed lighting, visibility, flags, and tint.

Positional Packing in `attr.x`:
  - Bits  0 -  7: Y Coordinate (8 bits, 0-255 relative to regionlet)
  - Bits  8 - 15: Z Coordinate (8 bits, 0-255 relative to regionlet)
  - Bits 16 - 23: X Coordinate (8 bits, 0-255 relative to regionlet)
  - Bits 24 - 31: Texture ID (8 bits, base index in the layer's texture atlas)

1. CUBE (LayerCube)
  - Represents solid/opaque full-size blocks with 1 to 4 textures.
  - Utilizes `sideSpecial` face-mapping to render multiple textures (sides, top, bottom) on a single cube.
  - `attr.y` packing:
  - Bits  0 -  5: Active visible faces mask (6 bits; bit set if face is visible)
  - Bits  6 - 29: Per-face lighting values (4 bits per face for 6 faces; order: West, East, South, North, Up, Down)
  - Bit      30: `sideSpecial` flag (if set, uses secondary/tertiary textures for top/bottom faces)
  - Bit      31: Tint flag (1 to apply biome coloring, 0 otherwise)

2. VOXEL (LayerVoxel)
  - Represents complex or transparent multi-textured voxel blocks (where each face has independent textures).
  - `attr.y` packing:
  - Bits  0 -  5: Active visible faces mask
  - Bits  6 - 29: Per-face lighting values (4 bits per face for 6 faces)
  - Bits 30 - 31: Unused / High-bits of Texture ID (if texture ID > 255) / Tint flag

3. CROSS (LayerCross)
  - Represents diagonal intersecting 2D plane sprites (e.g. flowers, saplings, tall grass, webs).
  - `attr.y` packing:
  - Bits  0 -  3: Sprite light level (4 bits, 0-15)
  - Bits  4 - 30: Unused
  - Bit      31: Tint flag

4. CROP (LayerCrop)
  - Represents agricultural crop-style overlapping vertical parallel planes (e.g. wheat, carrots).
  - `attr.y` packing:
  - Bits  0 -  3: Sprite light level (4 bits, 0-15)
  - Bits  4 - 30: Unused
  - Bit      31: Tint flag

5. CUBOID (LayerCuboid)
  - Represents rectangular prism-shaped blocks with 6 independent textures.
  - "Texture id" becomes "cuboid ID", which is an index into the cuboid UBO.
  - `attr.y` packing:
  - Bits  0 -  5: Active visible faces mask (6 bits; bit set if face is visible)
  - Bits  6 - 29: Per-face lighting values (4 bits per face for 6 faces; order: West, East, South, North, Up, Down)
  - Bit      30: Unused.
  - Bit      31: Tint flag (1 to apply biome coloring, 0 otherwise)

6. CUBE_FALLBACK (LayerCubeFallback)
  - A general fallback layer for complex models that are otherwise unsupported.
  - Renders as a standard solid cube using only the first resolved texture.
  - `attr.y` packing: Same as CUBE.
*/
var LayerNames = []string{
	"CUBE",
	"VOXEL",
	"CROSS",
	"CROP",
	"CUBOID",
	"CUBE_FALLBACK",
}

type LayerNumber int

const (
	LayerCube LayerNumber = iota
	LayerVoxel
	LayerCross
	LayerCrop
	LayerCuboid
	LayerCubeFallback
	NumRenderLayers
)

type ModelEntry struct {
	Layer    LayerNumber `json:"layer"`
	Textures []string    `json:"textures,omitempty"`
	Template []uint32    `json:"tmpl,omitempty"`
	Bounds   []float32   `json:"-"`
	UVs      [][]float32 `json:"-"`
}

type BlockEntry struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	States      [][]string   `json:"states,omitempty"`
	Solid       bool         `json:"solid,omitempty"`
	Templates   []ModelEntry `json:"templates"`
}

type BlockEntryMetadata struct {
	Blocks       []BlockEntry `json:"blocks"`
	Version      string       `json:"version"`
	WorldVersion int          `json:"world_version"`
}

func getCubeFaces(m *rp.Model, faces [6]rp.BlockModelFace) ([]string, bool) {
	ret := []string{}
	tintCount := 0
	for _, face := range faces {
		if face.Texture == "" || face.CullFace == "" || /* face.Rotation != 0 || */ (face.UV != nil && !reflect.DeepEqual(face.UV, []float64{0, 0, 16, 16})) {
			return nil, false
		}
		if face.TintIndex != nil {
			if *face.TintIndex != 0 {
				return nil, false
			}
			tintCount++
		}
		tex := face.Texture
		for tex[0] == byte('#') {
			tex = m.Textures[tex[1:]]
		}
		ret = append(ret, tex)
	}
	if tintCount != 0 && tintCount != 6 {
		return nil, false
	}
	return ret, tintCount == 6
}

func renderCube(m *rp.Model) *ModelEntry {
	if len(m.Elements) != 1 {
		return nil
	}
	name := m.Parent
	el := m.Elements[0]
	if !reflect.DeepEqual(el.From, []float64{0, 0, 0}) || !reflect.DeepEqual(el.To, []float64{16, 16, 16}) {
		return nil
	}
	if el.Shade != nil || el.Rotation.Angle != 0 {
		fmt.Println("bailing due to", name, el.Shade, el.Rotation.Angle)
		return nil
	}
	texs, tint := getCubeFaces(m, [...]rp.BlockModelFace{el.Faces["up"], el.Faces["north"], el.Faces["east"], el.Faces["south"], el.Faces["west"], el.Faces["down"]})
	if texs == nil {
		fmt.Println("bailing due to texs", name, m, el.Faces)
		return nil
	}

	if !tint { // texs[1] != texs[2] || texs[2] != texs[3] || texs[3] != texs[4] {
		// grab texs again to match face visibility order
		texs, _ = getCubeFaces(m, [...]rp.BlockModelFace{el.Faces["west"], el.Faces["east"], el.Faces["south"], el.Faces["north"], el.Faces["up"], el.Faces["down"]})
		m := &ModelEntry{
			Layer: LayerVoxel,
		}
		// one cube output per texture
		for i, t := range texs {
			if t == "" {
				continue
			}
			tmpl := uint32(0)
			for j := i; j < len(texs); j++ {
				if texs[j] == t {
					tmpl |= 1 << j
					texs[j] = ""
				}
			}
			m.Textures = append(m.Textures, t)
			m.Template = append(m.Template, 0, tmpl)
		}
		return m
	}

	meta := uint32(0b111111)
	if tint {
		meta |= 1 << 31
	}
	if texs[0] != texs[1] || texs[0] != texs[5] {
		return &ModelEntry{
			Layer:    LayerCube,
			Textures: []string{texs[1], texs[0], texs[5]},
			Template: []uint32{0, meta | 1<<30},
		}
	}

	return &ModelEntry{
		Layer:    LayerCube,
		Textures: []string{texs[0]},
		Template: []uint32{0, meta},
	}
}

type TextureMeta struct {
	Bounds   []float32
	UVs      [][]float32
	TexNames []string
}

type StateConverter struct {
	Models        map[string]*rp.Model
	ColumnTops    map[string]string
	Debug         string
	TextureBounds map[string]TextureMeta
}

func getDefaultUV(faceName string, el *rp.ModelElement) []float32 {
	x0, y0, z0 := float32(el.From[0]), float32(el.From[1]), float32(el.From[2])
	x1, y1, z1 := float32(el.To[0]), float32(el.To[1]), float32(el.To[2])
	switch faceName {
	case "west":
		return []float32{z0, 16.0 - y1, z1, 16.0 - y0}
	case "east":
		return []float32{16.0 - z1, 16.0 - y1, 16.0 - z0, 16.0 - y0}
	case "north":
		return []float32{16.0 - x1, 16.0 - y1, 16.0 - x0, 16.0 - y0}
	case "south":
		return []float32{x0, 16.0 - y1, x1, 16.0 - y0}
	case "up":
		return []float32{x0, z0, x1, z1}
	case "down":
		return []float32{x0, 16.0 - z1, x1, 16.0 - z0}
	}
	return []float32{0, 0, 16, 16}
}

func (s *StateConverter) renderCuboid(m *rp.Model) *ModelEntry {

	if len(m.Elements) != 1 {
		return nil
	}
	el := m.Elements[0]
	if el.Shade != nil || el.Rotation.Angle != 0 {
		return nil
	}

	// Order: west, east, south, north, up, down
	texsOrder := [...]string{"west", "east", "south", "north", "up", "down"}
	texs := []string{}
	tintCount := 0
	meta := uint32(0)

	for i, fName := range texsOrder {
		face, ok := el.Faces[fName]
		if !ok || face.Texture == "" {
			// Clear bit in mask, use placeholder texture
			texs = append(texs, "air")
			continue
		}

		// Face is present, set visibility bit
		meta |= 1 << i

		if face.TintIndex != nil {
			if *face.TintIndex != 0 {
				return nil
			}
			tintCount++
		}
		tex := face.Texture
		for tex != "" && tex[0] == byte('#') {
			tex = m.Textures[tex[1:]]
		}
		if tex == "" {
			tex = "air"
		}
		texs = append(texs, tex)
	}

	tint := tintCount > 0
	if tint {
		meta |= 1 << 31
	}

	// Calculate custom or default UVs
	var uvs [][]float32
	uvs = make([][]float32, 6)
	for i, fName := range texsOrder {
		face, ok := el.Faces[fName]
		if ok && face.UV != nil && len(face.UV) == 4 {
			uvs[i] = []float32{float32(face.UV[0]), float32(face.UV[1]), float32(face.UV[2]), float32(face.UV[3])}
		} else {
			uvs[i] = getDefaultUV(fName, el)
		}
	}

	s.TextureBounds[texs[0]] = TextureMeta{
		Bounds:   []float32{float32(el.From[0]), float32(el.From[1]), float32(el.From[2]), float32(el.To[0]), float32(el.To[1]), float32(el.To[2])},
		UVs:      uvs,
		TexNames: texs,
	}

	return &ModelEntry{
		Layer:    LayerCuboid,
		Textures: texs,
		Template: []uint32{0, meta},
		Bounds:   []float32{float32(el.From[0]), float32(el.From[1]), float32(el.From[2]), float32(el.To[0]), float32(el.To[1]), float32(el.To[2])},
		UVs:      uvs,
	}
}

func (s *StateConverter) referencedTexturesModel(model *rp.Model) ([]string, bool) {
	out := []string{}
	tinted := false

	// fallback: draw ANY texture from ANY sub-model as a cube
	if model.Textures != nil {
		for key, tex := range model.Textures {
			if key != "particle" {
				out = append(out, tex)
			}
		}
	}
	for _, el := range model.Elements {
		for _, face := range el.Faces {
			if face.TintIndex != nil {
				tinted = true
			}
		}
	}
	parent := s.Models[rp.RemoveDefaultPrefix(model.Parent)]
	if parent != nil {
		for _, el := range parent.Elements {
			for _, face := range el.Faces {
				if face.TintIndex != nil {
					tinted = true
				}
			}
		}
	}
	return out, tinted
}

func (s *StateConverter) referencedTextures(st *rp.BlockState) ([]string, bool) {
	out := []string{}
	tinted := false

	// fallback: draw ANY texture from ANY sub-model as a cube
	for _, vs := range st.Variants {
		for _, v := range vs {
			model := s.Models[rp.RemoveDefaultPrefix(v.Model)]
			if model.Textures != nil {
				for _, tex := range model.Textures {
					out = append(out, tex)
				}
			}
			for _, el := range model.Elements {
				for _, face := range el.Faces {
					if face.TintIndex != nil {
						tinted = true
					}
				}
			}
			parent := s.Models[rp.RemoveDefaultPrefix(model.Parent)]
			if parent != nil {
				for _, el := range parent.Elements {
					for _, face := range el.Faces {
						if face.TintIndex != nil {
							tinted = true
						}
					}
				}
			}
		}
	}
	for _, m := range st.Multipart {
		for _, v := range m.Apply {
			model := s.Models[rp.RemoveDefaultPrefix(v.Model)]
			if model.Textures != nil {
				for _, tex := range model.Textures {
					out = append(out, tex)
				}
			}
		}
	}
	return out, tinted
}

func (s *StateConverter) applyRotations(ms *rp.ModelSpec, model *rp.Model) *rp.Model {
	// clone model
	var m rp.Model
	buf, _ := json.Marshal(model)
	json.Unmarshal(buf, &m)

	rotX := 0
	if ms.X != nil {
		rotX = *ms.X
	}
	rotY := 0
	if ms.Y != nil {
		rotY = *ms.Y
	}

	for rotX != 0 && rotX%90 == 0 {
		for i := range m.Elements {
			e := m.Elements[i]
			e.Faces["north"], e.Faces["down"], e.Faces["south"], e.Faces["up"] =
				e.Faces["down"], e.Faces["south"], e.Faces["up"], e.Faces["north"]

			// Rotate coordinates around X:
			// y_new = 16 - z
			// z_new = y
			yFrom, yTo := e.From[1], e.To[1]
			zFrom, zTo := e.From[2], e.To[2]
			e.From[1] = 16.0 - zTo
			e.To[1] = 16.0 - zFrom
			e.From[2] = yFrom
			e.To[2] = yTo

			m.Elements[i] = e
		}
		if rotX > 0 {
			rotX -= 90
		} else {
			rotX += 90
		}
	}

	for rotY != 0 && rotY%90 == 0 {
		for i := range m.Elements {
			e := m.Elements[i]
			e.Faces["north"], e.Faces["east"], e.Faces["south"], e.Faces["west"] =
				e.Faces["west"], e.Faces["north"], e.Faces["east"], e.Faces["south"]

			// Rotate coordinates around Y:
			// x_new = 16 - z
			// z_new = x
			xFrom, xTo := e.From[0], e.To[0]
			zFrom, zTo := e.From[2], e.To[2]
			e.From[0] = 16.0 - zTo
			e.To[0] = 16.0 - zFrom
			e.From[2] = xFrom
			e.To[2] = xTo

			m.Elements[i] = e
		}
		if rotY > 0 {
			rotY -= 90
		} else {
			rotY += 90
		}
	}

	return &m
}

func (s *StateConverter) resolveInheritance(model *rp.Model) {
	parentName := model.Parent
	for parentName != "" {
		parent := s.Models[rp.RemoveDefaultPrefix(parentName)]
		if model.AmbientOcclusion == nil {
			model.AmbientOcclusion = parent.AmbientOcclusion
		}
		if len(model.Elements) == 0 {
			model.Elements = parent.Elements
		}
		if model.Textures == nil {
			model.Textures = map[string]string{}
		}
		if parent.Textures != nil {
			for k, v := range parent.Textures {
				if _, ok := model.Textures[k]; !ok {
					model.Textures[k] = v
				}
			}
		}
		parentName = parent.Parent
	}
}

func (s *StateConverter) renderModelSpec(name string, ms *rp.ModelSpec) ModelEntry {
	modelName := rp.RemoveDefaultPrefix(ms.Model)
	// TODO: check for modelspec X/Y rotations etc
	model := s.Models[modelName]
	if model == nil {
		fmt.Println(lo.Keys(s.Models))
		panic(fmt.Sprintf("unable to find model for %s", modelName))
	}
	s.resolveInheritance(model)

	rotated := false

	if (ms.X != nil && *ms.X != 0) || (ms.Y != nil && *ms.Y != 0) {
		model = s.applyRotations(ms, model)
		rotated = true
	}

	from, to := getModelBounds(model)
	var uvs [][]float32
	if len(model.Elements) == 1 {
		el := model.Elements[0]
		uvs = make([][]float32, 6)
		faces := [...]string{"west", "east", "south", "north", "up", "down"}
		for i, fName := range faces {
			f := el.Faces[fName]
			if f.UV != nil && len(f.UV) == 4 {
				uvs[i] = []float32{float32(f.UV[0]), float32(f.UV[1]), float32(f.UV[2]), float32(f.UV[3])}
			} else {
				uvs[i] = []float32{0, 0, 16, 16}
			}
		}
	}

	for _, tex := range model.Textures {
		tName := rp.RemoveDefaultPrefix(tex)
		if tName != "" && tName[0] != '#' {
			if _, ok := s.TextureBounds[tName]; !ok {
				s.TextureBounds[tName] = TextureMeta{
					Bounds: []float32{from[0], from[1], from[2], to[0], to[1], to[2]},
					UVs:    uvs,
				}
			}
		}
	}

	cubeSpec := renderCube(model)
	if cubeSpec != nil {
		if rotated && len(cubeSpec.Template) == len(cubeSpec.Textures)*2 && cubeSpec.Template[1]&(1<<31) == 0 {
			cubeSpec.Layer = LayerVoxel
		}
		if s.Debug == "all" || s.Debug == name {
			fmt.Printf("CUBE %#v\n", cubeSpec)
		}
		return *cubeSpec
	}

	cleanName := rp.RemoveDefaultPrefix(name)
	if cleanName == "grass_block" || cleanName == "grass" {
		// render grass blocks as two cubes:
		// * the dirt sides and bottom (no top)
		// * the tinted grass top and side overlay (no bottom)
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("CUBE(GRASS)", name)
		}
		return ModelEntry{
			Layer:    LayerCube,
			Textures: []string{model.Textures["side"], model.Textures["bottom"], model.Textures["overlay"], model.Textures["top"]},
			Template: []uint32{0, 0b101111 | 1<<30, 0, 0b011111 | 3<<30},
		}
	}

	if cleanName == "water" && model.Textures != nil && model.Textures["particle"] == "block/water_still" {
		return ModelEntry{
			Layer:    LayerCubeFallback,
			Textures: []string{"block/water_still"},
			Template: []uint32{0, 0b111111},
		}
	}

	cuboidSpec := s.renderCuboid(model)
	if cuboidSpec != nil {
		if s.Debug == "all" || s.Debug == name {
			fmt.Printf("CUBOID %#v\n", cuboidSpec)
		}
		return *cuboidSpec
	}

	if model.Parent == "minecraft:block/cross" {
		tex := model.Textures["cross"]
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("CROSS", name, tex)
		}
		return ModelEntry{
			Layer:    LayerCross,
			Textures: []string{tex},
			Template: []uint32{0, 0b1111111}}
	} else if model.Parent == "minecraft:block/tinted_cross" {
		tex := model.Textures["cross"]
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("TINTED_CROSS", name, tex)
		}
		return ModelEntry{
			Layer:    LayerCross,
			Textures: []string{tex},
			Template: []uint32{0, 0b111111 | 1<<31}}
	} else if model.Parent == "minecraft:block/crop" {
		tex := model.Textures["crop"]
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("CROP", name, tex)
		}
		return ModelEntry{
			Layer:    LayerCrop,
			Textures: []string{tex},
			Template: []uint32{0, 0b1111111}}
	}

	if s.Debug == "all" || s.Debug == name {
		modelJ, _ := json.MarshalIndent(model, "", "  ")
		fmt.Printf("FALLBACK %#v %s\n", ms, string(modelJ))
	}

	// fallback
	textures, tinted := s.referencedTexturesModel(model)
	sort.Strings(textures)
	for _, tex := range textures {
		if tex[0] == '#' {
			continue
		}
		meta := uint32(0b111111)
		if tinted {
			meta |= 1 << 31
		}
		if s.Debug == "improper" {
			reason := ""
			if len(model.Elements) > 1 {
				reason = fmt.Sprintf("has %d elements", len(model.Elements))
			} else if len(model.Elements) == 1 {
				el := model.Elements[0]
				if !reflect.DeepEqual(el.From, []float64{0, 0, 0}) || !reflect.DeepEqual(el.To, []float64{16, 16, 16}) {
					reason = fmt.Sprintf("non-full cube element (from: %v, to: %v)", el.From, el.To)
				} else if el.Rotation.Angle != 0 {
					reason = fmt.Sprintf("has element rotation (angle: %v)", el.Rotation.Angle)
				}
			} else {
				reason = "has 0 elements"
			}
			fmt.Printf("IMPROPER %s (%s): fallback to cube, %s, textures: %v\n", name, modelName, reason, textures)
		}
		layer := LayerCubeFallback
		return ModelEntry{Layer: layer, Textures: []string{tex}, Template: []uint32{0, meta}}
	}

	if s.Debug == "improper" {
		fmt.Printf("IMPROPER %s (%s): unhandled model (no textures), elements: %d\n", name, modelName, len(model.Elements))
	}
	return ModelEntry{Layer: -1}
}

func (s *StateConverter) Render(name string, st *rp.BlockState) BlockEntry {
	slist := buildStateList(st)
	smap := BuildStateMap(slist)
	if st.Variants[""] != nil {
		model := s.renderModelSpec(name, &st.Variants[""][0])
		if model.Layer >= 0 {
			return BlockEntry{Name: name, States: slist, Templates: []ModelEntry{model}}
		}
	}
	if len(st.Variants) > 0 {
		tmpls := make([]ModelEntry, smap.Max()+1)
		for props, models := range st.Variants {
			tmpls[int(smap.Get(props))] = s.renderModelSpec(name, &models[0])
		}
		return BlockEntry{Name: name, States: slist, Templates: tmpls}
	}

	// fallback: draw ANY texture from ANY sub-model as a cube
	textures, tinted := s.referencedTextures(st)
	sort.Strings(textures)
	for _, tex := range textures {
		if tex[0] == '#' {
			continue
		}
		tint := uint32(0b111111)
		if tinted {
			tint |= 1 << 31
		}
		if s.Debug == "all" || s.Debug == name {
			modelJ, _ := json.MarshalIndent(st, "", "  ")
			fmt.Printf("FALLBACKMULTI %#v %s\n", name, string(modelJ))
		}
		if s.Debug == "improper" {
			fmt.Printf("IMPROPER MULTI %s: blockstate uses multipart/variants but fallback to single texture cube, textures: %v\n", name, textures)
		}
		return BlockEntry{Name: name, States: slist, Templates: []ModelEntry{
			{Layer: LayerCubeFallback, Textures: []string{tex}, Template: []uint32{0, tint}}}}
	}

	return BlockEntry{}
}

type cuboidKey struct {
	Bounds   [6]float32
	UVs      [6][4]float32
	Textures [6]string
}

func Prepare(pack *rp.ResourceJar, genDebug string) (BlockEntryMetadata, []*image.RGBA, map[string][]UBOModelEntry) {
	// Classify textures as opaque, transparent (cutout), translucent
	// This is used to infer solidity-- a cube with all opaque sides
	// is a definite occluder.
	textureClasses := map[string]TextureType{}
	for name, tex := range pack.Textures {
		ty := TexOpaque
		rect := tex.Bounds()
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				_, _, _, a := tex.At(x, y).RGBA()
				if a == 0 && ty == TexOpaque {
					ty = TexTranslucent
				} else if a > 0 && a < 0xffff && ty < TexTranslucent {
					ty = TexTranslucent
				}
			}
		}
		textureClasses[name] = ty
	}

	// Process block states to determine model drawing templates
	meta := BlockEntryMetadata{
		Blocks: []BlockEntry{
			{Name: "air"}, {Name: "cave_air"}, {Name: "void_air"},
		},
		Version:      pack.Version,
		WorldVersion: pack.WorldVersion,
	}

	blockEntries := &meta.Blocks

	converter := StateConverter{
		ColumnTops: map[string]string{},
		Models: lo.MapEntries(pack.Models, func(key string, m *rp.Model) (string, *rp.Model) {
			return rp.RemoveDefaultPrefix(key), m
		}),
		Debug:         genDebug,
		TextureBounds: map[string]TextureMeta{},
	}

	cuboidCache := map[cuboidKey]int{}
	cuboidEntries := map[int]UBOModelEntry{}
	cuboidCount := 0

	for name, st := range pack.BlockStates {
		entry := converter.Render(name, st)
		if len(entry.Templates) > 0 {
			*blockEntries = append(*blockEntries, entry)
		} else {
			fmt.Println("unhandled", name, st)
		}
	}

	// Generate texture atlases and finalize templates
	nameToOldID := map[string]int{"air": 0}
	for id, ent := range rp.BlockstateMap {
		if nameToOldID[ent.Name] == 0 {
			nameToOldID[ent.Name] = id
		}
	}

	atlases := []*image.RGBA{}
	texIDs := []map[string]int{}

	for i := 0; i < int(NumRenderLayers); i++ {
		atlas := image.NewRGBA(image.Rect(0, 0, 512, 512))

		draw.Draw(atlas, atlas.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 64}},
			image.ZP, draw.Src)
		for p := 0; p < 512*512/(16*16); p++ {
			x0 := (p * 16) % 512
			y0 := (p / 32) * 16
			draw.Draw(atlas, image.Rect(x0, y0, x0+8, y0+8), &image.Uniform{color.RGBA{255, 255, 255, 32}},
				image.ZP, draw.Src)
			draw.Draw(atlas, image.Rect(x0+8, y0+8, x0+16, y0+16), &image.Uniform{color.RGBA{255, 255, 255, 32}},
				image.ZP, draw.Src)
		}

		atlases = append(atlases, atlas)
		texIDs = append(texIDs, map[string]int{"air": 0})
	}

	// Reserve texture slot 1 in CUBE_FALLBACK for water, matching the
	// hardcoded WATER_ID=1 shader define used for water tint color.
	texIDs[LayerCubeFallback]["block/water_still"] = 1

	// sort blocks so that blocks with assigned block IDs
	// come first in the correct order
	sort.SliceStable(*blockEntries, func(i, j int) bool {
		a := (*blockEntries)[i].Name
		b := (*blockEntries)[j].Name
		if a == b {
			return false
		}
		if nameToOldID[a] > 0 {
			if nameToOldID[b] > 0 {
				return nameToOldID[a] < nameToOldID[b]
			}
			return true
		} else if nameToOldID[b] > 0 {
			return false
		}
		diff := (pack.StringCounts[a] + pack.StringCounts["minecraft:"+a]) - (pack.StringCounts[b] + pack.StringCounts["minecraft:"+b])
		if diff != 0 {
			return diff > 0
		}
		return a < b
	})

	for i := range *blockEntries {
		ent := &(*blockEntries)[i]
		splatTexture := func(layer LayerNumber, name string, place int) {
			if place == 0 {
				place = texIDs[layer][name]
			}
			if place == 0 {
				place = len(texIDs[layer])
				texIDs[layer][name] = place
			}
			if place > 512 {
				fmt.Println("warn: overrun for", ent.Name, place)
				return
			}
			tex := pack.Textures[name]
			if tex == nil {
				fmt.Println("warn: nil texture for", ent.Name, name)
				return
			}
			x0 := (place * 16) % 512
			y0 := (place / 32) * 16
			draw.Draw(atlases[layer], image.Rect(x0, y0, x0+16, y0+16), tex, image.Point{}, draw.Src)
		}

		if tr, ok := pack.Translations["block.minecraft."+ent.Name]; ok {
			ent.DisplayName = tr
		} else if tr, ok := pack.Translations["block.minecraft."+rp.RemoveDefaultPrefix(ent.Name)]; ok {
			ent.DisplayName = tr
		}

		for ti := range ent.Templates {
			model := &ent.Templates[ti]
			if model.Textures == nil {
				continue
			}
			for tIdx := range model.Textures {
				model.Textures[tIdx] = rp.RemoveDefaultPrefix(model.Textures[tIdx])
			}

			layer := model.Layer
			if len(model.Textures)+len(texIDs[layer]) >= 512 {
				fmt.Println("warn: overrun for", ent.Name)
				break
			}
			if layer == LayerCube {
				splatTexture(layer, model.Textures[0], 0)
				if len(model.Textures) > 1 {
					splatTexture(layer, model.Textures[1], texIDs[layer][model.Textures[0]]+256)
					if len(model.Textures) == 4 { // grass_block
						splatTexture(layer, model.Textures[2], 0)
						splatTexture(layer, model.Textures[3], texIDs[layer][model.Textures[2]]+256)
					} else if len(model.Textures) == 3 {
						splatTexture(layer, model.Textures[2], texIDs[layer][model.Textures[0]]+512)
					} else {
						splatTexture(layer, model.Textures[1], texIDs[layer][model.Textures[0]]+512)
					}
				}
			} else {
				for _, tex := range model.Textures {
					splatTexture(layer, tex, 0)
					if layer == LayerCuboid {
						splatTexture(LayerCubeFallback, tex, 0)
					}
				}
			}

			var tid int
			if layer == LayerCuboid {
				key := cuboidKey{}
				if len(model.Bounds) == 6 {
					copy(key.Bounds[:], model.Bounds)
				}
				for i := range model.UVs {
					if i < 6 && len(model.UVs[i]) == 4 {
						copy(key.UVs[i][:], model.UVs[i])
					}
				}
				for i := range model.Textures {
					if i < 6 {
						key.Textures[i] = model.Textures[i]
					}
				}

				if existingTid, ok := cuboidCache[key]; ok {
					tid = existingTid
				} else {
					if cuboidCount >= 511 {
						// UBO is full (512 max)! Fallback to LayerCubeFallback
						layer = LayerCubeFallback
						model.Layer = LayerCubeFallback
						tName := rp.RemoveDefaultPrefix(model.Textures[0])
						tid = texIDs[LayerCubeFallback][tName]
					} else {
						cuboidCount++
						tid = cuboidCount
						cuboidCache[key] = tid

						var texIds []int
						if len(model.Textures) == 6 {
							texIds = make([]int, 6)
							for i, t := range model.Textures {
								texIds[i] = texIDs[LayerCuboid][rp.RemoveDefaultPrefix(t)]
							}
						}
						cuboidEntries[tid] = UBOModelEntry{
							From:   model.Bounds[:3],
							To:     model.Bounds[3:],
							UVs:    model.UVs,
							TexIDs: texIds,
						}
						if ent.Name == "minecraft:cake" {
							fmt.Printf("DEBUG CAKE tid=%d model.UVs=%v\n", tid, model.UVs)
						}
					}
				}
			} else {
				tid = texIDs[layer][model.Textures[0]]
				if tid >= 256 && layer == LayerCube {
					panic(fmt.Sprintf("texID too large! layer %d %#v: %v\n%v", layer, ent, tid, texIDs))
				}
				if tid >= 512 {
					panic(fmt.Sprintf("texID too large! %#v: %v", ent, tid))
				}
			}

			model.Template[0] |= uint32(tid) << 24
			model.Template[1] |= uint32(tid>>8) << 30
			if ent.Name == "minecraft:grass_block" && len(model.Template) == 4 {
				// render grass blocks as two cubes:
				// * the dirt sides and bottom (no top)
				// * & len(model.Template) == 4 {the tinted grass top and side overlay (no bottom)
				model.Template[2] |= uint32(texIDs[layer][model.Textures[2]]) << 24
			} else if layer == LayerVoxel && len(model.Textures) > 1 {
				for i, t := range model.Textures {
					tid := texIDs[layer][t]
					model.Template[2*i] |= uint32(tid) << 24
					model.Template[2*i+1] |= uint32(tid>>8) << 30
				}
			}
			if rp.RemoveDefaultPrefix(ent.Name) == "water" {
				model.Template[1] |= 1 << 31
			}
			if genDebug == "all" || genDebug == ent.Name {
				fmt.Printf("L%d %s %v=%d %v %08x %08x\n",
					layer, ent.Name, model.Textures, tid, pack.Textures[model.Textures[0]].Bounds(),
					model.Template[0], model.Template[1])
			}
		}
	}

	for i := range *blockEntries {
		ent := &(*blockEntries)[i]
		ent.Solid = true

		for _, model := range ent.Templates {
			layer := model.Layer
			// A block is only solid if ALL of its templates are LayerCube or LayerVoxel (standard full cubes),
			// and all textures used by those states are opaque.
			if layer != LayerCube && layer != LayerVoxel {
				ent.Solid = false
				break
			}
			for _, tex := range model.Textures {
				if textureClasses[tex] != TexOpaque {
					ent.Solid = false
					break
				}
			}
			if !ent.Solid {
				break
			}
		}
	}

	// Build UBOs for each layer
	ubos := map[string][]UBOModelEntry{}
	for l := 0; l < int(NumRenderLayers); l++ {
		layerName := LayerNames[l]
		if l == int(LayerCuboid) {
			entries := make([]UBOModelEntry, cuboidCount+1)
			for i := range entries {
				entries[i] = UBOModelEntry{
					From: []float32{0, 0, 0},
					To:   []float32{16, 16, 16},
				}
			}
			for tid, entry := range cuboidEntries {
				if tid < len(entries) {
					entries[tid] = entry
				}
			}
			ubos[layerName] = entries
		} else {
			numTids := len(texIDs[l])
			entries := make([]UBOModelEntry, numTids)

			// Fill with default full cube bounds first
			for i := range entries {
				entries[i] = UBOModelEntry{
					From: []float32{0, 0, 0},
					To:   []float32{16, 16, 16},
				}
			}

			for texName, tid := range texIDs[l] {
				if tid < len(entries) {
					if meta, ok := converter.TextureBounds[texName]; ok {
						entries[tid] = UBOModelEntry{
							From: meta.Bounds[:3],
							To:   meta.Bounds[3:],
							UVs:  meta.UVs,
						}
					}
				}
			}
			ubos[layerName] = entries
		}
	}

	return meta, atlases, ubos
}

type UBOModelEntry struct {
	From   []float32   `json:"from"`
	To     []float32   `json:"to"`
	UVs    [][]float32 `json:"uvs,omitempty"`
	TexIDs []int       `json:"tex_ids,omitempty"`
}

func getModelBounds(model *rp.Model) ([]float32, []float32) {
	if len(model.Elements) == 0 {
		return []float32{0, 0, 0}, []float32{16, 16, 16}
	}
	minX, minY, minZ := 16.0, 16.0, 16.0
	maxX, maxY, maxZ := 0.0, 0.0, 0.0
	for _, el := range model.Elements {
		minX = min(minX, el.From[0])
		minY = min(minY, el.From[1])
		minZ = min(minZ, el.From[2])
		maxX = max(maxX, el.To[0])
		maxY = max(maxY, el.To[1])
		maxZ = max(maxZ, el.To[2])
	}
	return []float32{float32(minX), float32(minY), float32(minZ)},
		[]float32{float32(maxX), float32(maxY), float32(maxZ)}
}
