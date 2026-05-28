package render

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"sort"
	"strconv"
	"strings"

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
  - Bits  0 -  5: Active visible faces mask (West, East, South, North, Up, Down)
  - Bits  6 - 23: Per-face lighting values (3 bits per face for 6 faces; order: West, East, South, North, Up, Down)
  - Bits 24 - 31: Top 8 bits of the 16-bit cuboid ID (Tint is now stored in UBO metadata).

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
	Layer      LayerNumber `json:"layer"`
	Textures   []string    `json:"textures,omitempty"`
	Template   []uint32    `json:"tmpl,omitempty"`
	NoShade    bool        `json:"no_shade,omitempty"`
	Bounds     []float32   `json:"-"`
	UVs        [][]float32 `json:"-"`
	Rotations  []int       `json:"-"`
	RotAxis    string      `json:"-"`
	RotAngle   float32     `json:"-"`
	RotOrigin  []float32   `json:"-"`
	RotRescale bool        `json:"-"`
}

type BlockEntry struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	States      [][]string     `json:"states,omitempty"`
	Solid       bool           `json:"solid,omitempty"`
	Templates   [][]ModelEntry `json:"templates"`
	Colors      []string       `json:"colors"`
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
		if face.Texture == "" || face.CullFace == "" || (face.Rotation != nil && *face.Rotation != 0) || (face.UV != nil && !reflect.DeepEqual(face.UV, []float64{0, 0, 16, 16})) {
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

func (s *StateConverter) renderCube(m *rp.Model) *ModelEntry {
	if len(m.Elements) != 1 {
		return nil
	}
	name := m.Parent
	el := m.Elements[0]
	if !reflect.DeepEqual(el.From, []float64{0, 0, 0}) || !reflect.DeepEqual(el.To, []float64{16, 16, 16}) {
		return nil
	}
	if el.Rotation.Angle != 0 {
		if s.Debug == m.Parent {
			fmt.Println("bailing due to", name, el.Rotation.Angle)
		}
		return nil
	}
	texs, tint := getCubeFaces(m, [...]rp.BlockModelFace{el.Faces["up"], el.Faces["north"], el.Faces["east"], el.Faces["south"], el.Faces["west"], el.Faces["down"]})
	if texs == nil {
		if s.Debug == m.Parent {
			fmt.Println("bailing due to texs", name, m, el.Faces)
		}
		return nil
	}

	noShade := el.Shade != nil && !*el.Shade

	if !tint { // texs[1] != texs[2] || texs[2] != texs[3] || texs[3] != texs[4] {
		// grab texs again to match face visibility order
		texs, _ = getCubeFaces(m, [...]rp.BlockModelFace{el.Faces["west"], el.Faces["east"], el.Faces["south"], el.Faces["north"], el.Faces["up"], el.Faces["down"]})
		m := &ModelEntry{
			Layer:   LayerVoxel,
			NoShade: noShade,
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
			Textures: []string{texs[1], texs[4], texs[5]},
			Template: []uint32{0, meta | 1<<30},
			NoShade:  noShade,
		}
	}

	return &ModelEntry{
		Layer:    LayerCube,
		Textures: []string{texs[0]},
		Template: []uint32{0, meta},
		NoShade:  noShade,
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

	rotAngle := float32(0.0)
	rotAxis := ""
	var rotOrigin []float32
	rotRescale := false

	if el.Rotation.Angle != 0 {
		rotAngle = float32(el.Rotation.Angle)
		rotAxis = el.Rotation.Axis
		if rotAxis != "x" && rotAxis != "y" && rotAxis != "z" {
			return nil
		}
		if rotAngle != -45 && rotAngle != -22.5 && rotAngle != 22.5 && rotAngle != 45 {
			return nil
		}
		if len(el.Rotation.Origin) != 3 {
			return nil
		}
		rotOrigin = []float32{float32(el.Rotation.Origin[0]), float32(el.Rotation.Origin[1]), float32(el.Rotation.Origin[2])}
		if el.Rotation.Rescale != nil {
			rotRescale = *el.Rotation.Rescale
		}
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

		if face.CullFace != "" {
			// Set cullable bit in bits 18-23
			meta |= 1 << (i + 18)
		}

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
	rotations := make([]int, 6)
	for i, fName := range texsOrder {
		face, ok := el.Faces[fName]
		if ok && face.UV != nil && len(face.UV) == 4 {
			u0, v0, u1, v1 := face.UV[0], face.UV[1], face.UV[2], face.UV[3]
			if face.Rotation != nil {
				rot := *face.Rotation
				for rot >= 360 {
					rot -= 360
				}
				for rot < 0 {
					rot += 360
				}
				rotations[i] = rot
			}
			uvs[i] = []float32{float32(u0), float32(v0), float32(u1), float32(v1)}
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
		Layer:      LayerCuboid,
		Textures:   texs,
		Template:   []uint32{0, meta},
		NoShade:    el.Shade != nil && !*el.Shade,
		Bounds:     []float32{float32(el.From[0]), float32(el.From[1]), float32(el.From[2]), float32(el.To[0]), float32(el.To[1]), float32(el.To[2])},
		UVs:        uvs,
		Rotations:  rotations,
		RotAxis:    rotAxis,
		RotAngle:   rotAngle,
		RotOrigin:  rotOrigin,
		RotRescale: rotRescale,
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
			if model == nil {
				continue
			}
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
			if model == nil {
				continue
			}
			if model.Textures != nil {
				for _, tex := range model.Textures {
					out = append(out, tex)
				}
			}
		}
	}
	return out, tinted
}

func rotateUV90CW(uv []float64) []float64 {
	if len(uv) != 4 {
		return uv
	}
	cx := (uv[0] + uv[2]) / 2.0
	cy := (uv[1] + uv[3]) / 2.0
	hu := (uv[2] - uv[0]) / 2.0
	hv := (uv[3] - uv[1]) / 2.0
	return []float64{cx - hv, cy - hu, cx + hv, cy + hu}
}

func rotateUV90CCW(uv []float64) []float64 {
	if len(uv) != 4 {
		return uv
	}
	cx := (uv[0] + uv[2]) / 2.0
	cy := (uv[1] + uv[3]) / 2.0
	hu := (uv[2] - uv[0]) / 2.0
	hv := (uv[3] - uv[1]) / 2.0
	return []float64{cx - hv, cy - hu, cx + hv, cy + hu}
}

func rotateUV180(uv []float64) []float64 {
	if len(uv) != 4 {
		return uv
	}
	return []float64{uv[2], uv[3], uv[0], uv[1]}
}

func addRotation(r *int, deg int) *int {
	current := 0
	if r != nil {
		current = *r
	}
	newRot := (current + deg) % 360
	return &newRot
}

func getDefaultUV64(faceName string, el *rp.ModelElement) []float64 {
	x0, y0, z0 := el.From[0], el.From[1], el.From[2]
	x1, y1, z1 := el.To[0], el.To[1], el.To[2]
	switch faceName {
	case "west":
		return []float64{z0, 16.0 - y1, z1, 16.0 - y0}
	case "east":
		return []float64{16.0 - z1, 16.0 - y1, 16.0 - z0, 16.0 - y0}
	case "north":
		return []float64{16.0 - x1, 16.0 - y1, 16.0 - x0, 16.0 - y0}
	case "south":
		return []float64{x0, 16.0 - y1, x1, 16.0 - y0}
	case "up":
		return []float64{x0, z0, x1, z1}
	case "down":
		return []float64{x0, 16.0 - z1, x1, 16.0 - z0}
	}
	return []float64{0, 0, 16, 16}
}

func swapFaces(faces map[string]rp.BlockModelFace, order []string, uvsToTransform map[string]func([]float64) []float64) {
	orig := make(map[string]rp.BlockModelFace, len(order))
	for _, name := range order {
		if f, ok := faces[name]; ok {
			orig[name] = f
		}
	}
	n := len(order)
	for i, dst := range order {
		src := order[(i-1+n)%n]
		if face, ok := orig[src]; ok {
			if _, ok := uvsToTransform[src]; ok {
				face.Rotation = addRotation(face.Rotation, 180)
			}
			faces[dst] = face
		} else {
			delete(faces, dst)
		}
	}
}

func (s *StateConverter) applyRotations(ms *rp.ModelSpec, model *rp.Model) *rp.Model {
	// clone model
	var m rp.Model
	buf, _ := json.Marshal(model)
	json.Unmarshal(buf, &m)

	for i := range m.Elements {
		e := m.Elements[i]
		for fName, face := range e.Faces {
			if face.UV == nil {
				face.UV = getDefaultUV64(fName, e)
				e.Faces[fName] = face
			}
		}
	}

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

			swapFaces(e.Faces, []string{"north", "down", "south", "up"}, map[string]func([]float64) []float64{
				"up":    nil,
				"south": nil,
			})

			if f, ok := e.Faces["west"]; ok {
				f.Rotation = addRotation(f.Rotation, 270)
				e.Faces["west"] = f
			}
			if f, ok := e.Faces["east"]; ok {
				f.Rotation = addRotation(f.Rotation, 90)
				e.Faces["east"] = f
			}

			// Rotate coordinates around X:
			// y_new = z
			// z_new = 16 - y
			yFrom, yTo := e.From[1], e.To[1]
			zFrom, zTo := e.From[2], e.To[2]
			e.From[1] = zFrom
			e.To[1] = zTo
			e.From[2] = 16.0 - yTo
			e.To[2] = 16.0 - yFrom

			if e.Rotation.Angle != 0 {
				if len(e.Rotation.Origin) == 3 {
					oy, oz := e.Rotation.Origin[1], e.Rotation.Origin[2]
					e.Rotation.Origin[1] = oz
					e.Rotation.Origin[2] = 16.0 - oy
				}
				if e.Rotation.Axis == "y" {
					e.Rotation.Axis = "z"
					e.Rotation.Angle = -e.Rotation.Angle
				} else if e.Rotation.Axis == "z" {
					e.Rotation.Axis = "y"
				}
			}

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

			swapFaces(e.Faces, []string{"north", "east", "south", "west"}, nil)

			if f, ok := e.Faces["up"]; ok {
				f.Rotation = addRotation(f.Rotation, 90)
				e.Faces["up"] = f
			}
			if f, ok := e.Faces["down"]; ok {
				f.Rotation = addRotation(f.Rotation, 270)
				e.Faces["down"] = f
			}

			// Rotate coordinates around Y:
			// x_new = 16 - z
			// z_new = x
			xFrom, xTo := e.From[0], e.To[0]
			zFrom, zTo := e.From[2], e.To[2]
			e.From[0] = 16.0 - zTo
			e.To[0] = 16.0 - zFrom
			e.From[2] = xFrom
			e.To[2] = xTo

			if e.Rotation.Angle != 0 {
				if len(e.Rotation.Origin) == 3 {
					ox, oz := e.Rotation.Origin[0], e.Rotation.Origin[2]
					e.Rotation.Origin[0] = 16.0 - oz
					e.Rotation.Origin[2] = ox
				}
				if e.Rotation.Axis == "x" {
					e.Rotation.Axis = "z"
				} else if e.Rotation.Axis == "z" {
					e.Rotation.Axis = "x"
					e.Rotation.Angle = -e.Rotation.Angle
				}
			}

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
	visited := make(map[string]bool)
	for parentName != "" {
		cleanParent := rp.RemoveDefaultPrefix(parentName)
		if visited[cleanParent] {
			break
		}
		visited[cleanParent] = true

		parent := s.Models[cleanParent]
		if parent == nil || parent == model {
			break
		}
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

func (s *StateConverter) renderModelSpec(name string, ms *rp.ModelSpec) []ModelEntry {
	modelName := rp.RemoveDefaultPrefix(ms.Model)
	// TODO: check for modelspec X/Y rotations etc
	model := s.Models[modelName]
	if model == nil {
		fmt.Println(lo.Keys(s.Models))
		panic(fmt.Sprintf("unable to find model for %s", modelName))
	}
	s.resolveInheritance(model)

	if s.Debug == name {
		b, _ := json.MarshalIndent(model.Elements, "", "  ")
		fmt.Printf("DEBUG_MODEL %s ModelElement=%s\n", name, b)
	}

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

	cleanName := rp.RemoveDefaultPrefix(name)
	if (cleanName == "grass_block" || cleanName == "grass") && model.Textures["overlay"] != "" {
		// render grass blocks as two cubes:
		// * the dirt sides and bottom (no top)
		// * the tinted grass top and side overlay (no bottom)
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("CUBE(GRASS)", name)
		}
		return []ModelEntry{{
			Layer:    LayerCube,
			Textures: []string{model.Textures["side"], model.Textures["bottom"], model.Textures["overlay"], model.Textures["top"]},
			Template: []uint32{0, 0b101111 | 1<<30, 0, 0b011111 | 3<<30},
		}}
	}

	if cleanName == "water" && model.Textures != nil && model.Textures["particle"] == "block/water_still" {
		return []ModelEntry{{
			Layer:    LayerCubeFallback,
			Textures: []string{"block/water_still"},
			Template: []uint32{0, 0b111111},
		}}
	}

	hasTint := false
	if model.Parent == "minecraft:block/tinted_cross" || model.Parent == "minecraft:block/tinted_crop" {
		hasTint = true
	} else {
		for _, el := range model.Elements {
			for _, face := range el.Faces {
				if face.TintIndex != nil {
					hasTint = true
					break
				}
			}
			if hasTint {
				break
			}
		}
	}

	if model.Parent == "minecraft:block/cross" || model.Parent == "minecraft:block/tinted_cross" {
		tex := model.Textures["cross"]
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("CROSS", name, tex)
		}
		meta := uint32(0b1111111)
		if hasTint {
			meta = 0b111111 | 1<<31
		}
		return []ModelEntry{{
			Layer:    LayerCross,
			Textures: []string{tex},
			Template: []uint32{0, meta}}}
	} else if model.Parent == "minecraft:block/crop" || model.Parent == "minecraft:block/tinted_crop" {
		tex := model.Textures["crop"]
		if s.Debug == "all" || s.Debug == name {
			fmt.Println("CROP", name, tex)
		}
		meta := uint32(0b1111111)
		if hasTint {
			meta = 0b111111 | 1<<31
		}
		return []ModelEntry{{
			Layer:    LayerCrop,
			Textures: []string{tex},
			Template: []uint32{0, meta}}}
	}

	var out []ModelEntry
	for _, el := range model.Elements {
		subModel := *model
		subModel.Elements = []*rp.ModelElement{el}

		cubeSpec := s.renderCube(&subModel)
		if cubeSpec != nil {
			if rotated && len(cubeSpec.Template) == len(cubeSpec.Textures)*2 && cubeSpec.Template[1]&(1<<31) == 0 {
				cubeSpec.Layer = LayerVoxel
			}
			if s.Debug == "all" || s.Debug == name {
				fmt.Printf("CUBE %#v\n", cubeSpec)
			}
			out = append(out, *cubeSpec)
			continue
		}

		cuboidSpec := s.renderCuboid(&subModel)
		if cuboidSpec != nil {
			if s.Debug == "all" || s.Debug == name {
				fmt.Printf("CUBOID %#v\n", cuboidSpec)
			}
			out = append(out, *cuboidSpec)
			continue
		}
	}

	if len(out) > 0 {
		return out
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
		return []ModelEntry{{Layer: layer, Textures: []string{tex}, Template: []uint32{0, meta}}}
	}

	if s.Debug == "improper" {
		fmt.Printf("IMPROPER %s (%s): unhandled model (no textures), elements: %d\n", name, modelName, len(model.Elements))
	}
	return nil
}

func matchCondition(cond map[string]any, stateProps map[string]string) bool {
	for k, condValAny := range cond {
		stateVal, ok := stateProps[k]
		if !ok {
			return false
		}
		var condVal string
		switch v := condValAny.(type) {
		case string:
			condVal = v
		case bool:
			if v {
				condVal = "true"
			} else {
				condVal = "false"
			}
		default:
			condVal = fmt.Sprintf("%v", v)
		}
		matched := false
		for _, part := range strings.Split(condVal, "|") {
			if part == stateVal {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func matchWhen(when *rp.BlockStateWhenClause, stateProps map[string]string) bool {
	if when == nil {
		return true
	}
	if when.IsOr {
		for _, clause := range when.Clauses {
			if matchCondition(clause, stateProps) {
				return true
			}
		}
		return false
	} else {
		for _, clause := range when.Clauses {
			if !matchCondition(clause, stateProps) {
				return false
			}
		}
		return true
	}
}

func (s *StateConverter) Render(name string, st *rp.BlockState) BlockEntry {
	slist := buildStateList(name, st)
	smap := BuildStateMap(slist)
	if st.Variants[""] != nil {
		models := s.renderModelSpec(name, &st.Variants[""][0])
		if len(models) > 0 {
			return BlockEntry{Name: name, States: slist, Templates: [][]ModelEntry{models}}
		}
	}
	if len(st.Variants) > 0 {
		tmpls := make([][]ModelEntry, smap.Max()+1)
		for props, models := range st.Variants {
			tmpls[int(smap.Get(props))] = s.renderModelSpec(name, &models[0])
		}
		return BlockEntry{Name: name, States: slist, Templates: tmpls}
	}
	if len(st.Multipart) > 0 {
		tmpls := make([][]ModelEntry, smap.Max()+1)
		for sIdx := 0; sIdx <= int(smap.Max()); sIdx++ {
			if !IsValidState(sIdx, slist) {
				continue
			}
			stateProps := BuildStateMap(slist).Decode(sIdx)
			var combined []ModelEntry
			for _, part := range st.Multipart {
				if matchWhen(part.When, stateProps) {
					models := s.renderModelSpec(name, &part.Apply[0])
					combined = append(combined, models...)
				}
			}
			tmpls[sIdx] = combined
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
		return BlockEntry{Name: name, States: slist, Templates: [][]ModelEntry{
			{{Layer: LayerCubeFallback, Textures: []string{tex}, Template: []uint32{0, tint}}}}}
	}

	return BlockEntry{}
}

type cuboidKey struct {
	Bounds     [6]float32
	UVs        [6][4]float32
	Rotations  [6]int
	Textures   [6]string
	Tint       bool
	NoShade    bool
	Color      uint32
	RotAxis    string
	RotAngle   float32
	RotOrigin  [3]float32
	RotRescale bool
}

func Prepare(pack *rp.ResourceJar, genDebug string) (BlockEntryMetadata, []*image.RGBA, []byte) {
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
		w, h := 512, 512
		if LayerNumber(i) == LayerCuboid {
			w, h = 1024, 512
		}
		atlas := image.NewRGBA(image.Rect(0, 0, w, h))

		draw.Draw(atlas, atlas.Bounds(), &image.Uniform{color.RGBA{255, 255, 255, 64}},
			image.ZP, draw.Src)
		for p := 0; p < w*h/(16*16); p++ {
			x0 := (p * 16) % w
			y0 := (p / (w / 16)) * 16
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
		(*blockEntries)[i].updateColors(pack.Textures)
	}

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
			w := atlases[layer].Bounds().Dx()
			h := atlases[layer].Bounds().Dy()
			maxPlace := (w * h) / (16 * 16)
			if place > maxPlace {
				fmt.Println("warn: overrun for", ent.Name, place)
				return
			}
			tex := pack.Textures[name]
			if tex == nil {
				if genDebug == "all" {
					fmt.Println("warn: nil texture for", ent.Name, name)
				}
				return
			}
			x0 := (place * 16) % w
			y0 := (place / (w / 16)) * 16
			draw.Draw(atlases[layer], image.Rect(x0, y0, x0+16, y0+16), tex, image.Point{}, draw.Src)
		}

		if tr, ok := pack.Translations["block.minecraft."+ent.Name]; ok {
			ent.DisplayName = tr
		} else if tr, ok := pack.Translations["block.minecraft."+rp.RemoveDefaultPrefix(ent.Name)]; ok {
			ent.DisplayName = tr
		}

		for sIdx := range ent.Templates {
			for ti := range ent.Templates[sIdx] {
				model := &ent.Templates[sIdx][ti]
				if model.Textures == nil {
					continue
				}
				for tIdx := range model.Textures {
					model.Textures[tIdx] = rp.RemoveDefaultPrefix(model.Textures[tIdx])
				}

				layer := model.Layer
				if layer == LayerVoxel {
					// the voxel layer is too full, so we shunt some blocks to cuboid
					if len(texIDs[LayerVoxel])+len(model.Textures) >= 512 {
						model.Layer = LayerCuboid
						layer = LayerCuboid
						model.Bounds = []float32{0, 0, 0, 16, 16, 16}
						model.UVs = [][]float32{
							{0, 0, 16, 16}, // west
							{0, 0, 16, 16}, // east
							{0, 0, 16, 16}, // south
							{0, 0, 16, 16}, // north
							{0, 0, 16, 16}, // up
							{0, 0, 16, 16}, // down
						}
						model.Rotations = make([]int, 6)

						// Reconstruct the 6 face textures
						newTexs := make([]string, 6)
						for idx := range newTexs {
							newTexs[idx] = "air"
						}
						for k, tex := range model.Textures {
							mask := model.Template[2*k+1]
							for f := 0; f < 6; f++ {
								if (mask & (1 << f)) != 0 {
									newTexs[f] = tex
								}
							}
						}
						model.Textures = newTexs

						// Reconstruct the Template with 0 and meta
						maskUnion := uint32(0)
						tint := false
						for k := range model.Template {
							if k%2 == 1 {
								maskUnion |= model.Template[k] & 0x3F
								if (model.Template[k] & (1 << 31)) != 0 {
									tint = true
								}
							}
						}
						meta := maskUnion
						if tint {
							meta |= 1 << 31
						}
						model.Template = []uint32{0, meta}
					}
				}
				maxPlace := (atlases[layer].Bounds().Dx() * atlases[layer].Bounds().Dy()) / (16 * 16)
				if len(model.Textures)+len(texIDs[layer]) >= maxPlace {
					fmt.Println("warn: overrun for", ent.Name, "on", LayerNames[model.Layer])
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
					for i := range model.Rotations {
						if i < 6 {
							key.Rotations[i] = model.Rotations[i]
						}
					}
					for i := range model.Textures {
						if i < 6 {
							key.Textures[i] = model.Textures[i]
						}
					}
					key.Tint = (model.Template[1] & (1 << 31)) != 0
					key.NoShade = model.NoShade

					if ent.Name == "redstone_wire" || ent.Name == "minecraft:redstone_wire" {
						rgb, _ := strconv.ParseUint(ent.Colors[0], 16, 32)
						key.Color = uint32(rgb)
					}

					var customColor uint32
					if ent.Name == "redstone_wire" || ent.Name == "minecraft:redstone_wire" {
						stateProps := BuildStateMap(ent.States).Decode(sIdx)
						powerVal := 0
						if pStr, ok := stateProps["power"]; ok {
							powerVal, _ = strconv.Atoi(pStr)
						}
						r, g, b := redstoneColor(powerVal)
						if 1 == 0 {
							r, g, b = 255, 0, 0
						}
						customColor = uint32((r << 16) | (g << 8) | b)
					}
					key.Color = customColor

					if model.RotAxis != "" {
						key.RotAxis = model.RotAxis
						key.RotAngle = model.RotAngle
						if len(model.RotOrigin) == 3 {
							copy(key.RotOrigin[:], model.RotOrigin)
						}
						key.RotRescale = model.RotRescale
					}

					if existingTid, ok := cuboidCache[key]; ok {
						tid = existingTid
					} else {
						if cuboidCount >= 65535 {
							// metadata atlas is full (65535 max)! Fallback to LayerCubeFallback
							fmt.Println("cuboid metadata atlas full for", ent.DisplayName)
							layer = LayerCubeFallback
							model.Layer = LayerCubeFallback
							tName := rp.RemoveDefaultPrefix(model.Textures[0])
							splatTexture(LayerCubeFallback, tName, 0)
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
								From:       model.Bounds[:3],
								To:         model.Bounds[3:],
								UVs:        model.UVs,
								Rotations:  model.Rotations,
								TexIDs:     texIds,
								Tint:       key.Tint,
								NoShade:    key.NoShade,
								Color:      key.Color,
								RotAxis:    key.RotAxis,
								RotAngle:   key.RotAngle,
								RotOrigin:  key.RotOrigin[:],
								RotRescale: key.RotRescale,
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

				model.Template[0] |= uint32(tid&0xFF) << 24
				if layer == LayerCuboid {
					model.Template[1] &= ^uint32(0xFF000000) // clear top 8 bits (bits 24-31)
					model.Template[1] |= uint32((tid>>8)&0xFF) << 24
				} else {
					model.Template[1] |= uint32(tid>>8) << 30
				}
				cleanName := rp.RemoveDefaultPrefix(ent.Name)
				if (cleanName == "grass_block" || cleanName == "grass") && len(model.Template) == 4 {
					// render grass blocks as two cubes:
					// * the dirt sides and bottom (no top)
					// * the tinted grass top and side overlay (no bottom)
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
					var bounds image.Rectangle
					if tex := pack.Textures[model.Textures[0]]; tex != nil {
						bounds = tex.Bounds()
					}
					fmt.Printf("L%d %s %v=%d %v %08x %08x\n",
						layer, ent.Name, model.Textures, tid, bounds,
						model.Template[0], model.Template[1])
				}
			}
		}
	}

	for i := range *blockEntries {
		ent := &(*blockEntries)[i]
		ent.Solid = len(ent.Templates) > 0

		debugBlock := genDebug == ent.Name || genDebug == rp.RemoveDefaultPrefix(ent.Name)

		if debugBlock {
			fmt.Printf("DEBUG SOLID START %s: templates len: %d\n", ent.Name, len(ent.Templates))
		}
		for sIdx := range ent.Templates {
			if !IsValidState(sIdx, ent.States) {
				if debugBlock {
					fmt.Printf("  sIdx %d is invalid state combination! Skipping.\n", sIdx)
				}
				continue
			}
			variantSolid := false
			if debugBlock {
				fmt.Printf("  sIdx %d models len: %d\n", sIdx, len(ent.Templates[sIdx]))
			}
			for modelIdx, model := range ent.Templates[sIdx] {
				layer := model.Layer
				if debugBlock {
					fmt.Printf("    model %d layer: %s (%d)\n", modelIdx, LayerNames[layer], layer)
				}
				// A block is only solid if ALL of its templates are LayerCube or LayerVoxel (standard full cubes),
				// and all textures used by those states are opaque.
				if layer == LayerCube || layer == LayerVoxel {
					modelSolid := true
					for _, tex := range model.Textures {
						texClass := textureClasses[tex]
						if debugBlock {
							fmt.Printf("      texture %s class: %v\n", tex, texClass)
						}
						if texClass != TexOpaque {
							modelSolid = false
							break
						}
					}
					if modelSolid {
						variantSolid = true
						if debugBlock {
							fmt.Printf("      model solid! setting variantSolid = true\n")
						}
						break
					}
				}
			}
			if !variantSolid {
				if debugBlock {
					fmt.Printf("  variant %d not solid! marking ent.Solid = false\n", sIdx)
				}
				ent.Solid = false
				break
			}
		}
		if debugBlock {
			fmt.Printf("DEBUG SOLID END %s: Solid = %t\n", ent.Name, ent.Solid)
		}

		cleanName := rp.RemoveDefaultPrefix(ent.Name)
		if cleanName == "grass_block" || cleanName == "grass" {
			ent.Solid = true
		}
	}

	byteBuf := BuildCuboidMetadata(cuboidEntries, cuboidCount)

	return meta, atlases, byteBuf
}
