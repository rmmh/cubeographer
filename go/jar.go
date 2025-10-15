package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

var clientJarPath = flag.String("jar", "", "use specific client jar")
var clientJarVersion = flag.String("version", "latest", "specify client version to download")

var genDebug = flag.String("gendebug", "", "debug specific block name (or \"all\")")

var layerNames = []string{
	"CUBE",
	"VOXEL",
	"CROSS",
	"CROP",
	"CUBE_FALLBACK",
}

type layerNumber int

const (
	layerCube layerNumber = iota
	layerVoxel
	layerCross
	layerCrop
	layerCubeFallback
	numRenderLayers
)

func jsonGrab(url string, val interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(val)
}

func downloadMinecraftJar(dest string) error {
	manifest := struct {
		Latest struct {
			Release string `json:"release"`
		} `json:"latest"`
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		}
	}{}
	err := jsonGrab("https://launchermeta.mojang.com/mc/game/version_manifest.json", &manifest)
	if err != nil {
		return err
	}
	if *clientJarVersion == "latest" {
		clientJarVersion = &manifest.Latest.Release
		fmt.Println("downloading version", *clientJarVersion)
	}
	versionManifestURL := ""
	for _, v := range manifest.Versions {
		if v.ID == *clientJarVersion {
			versionManifestURL = v.URL
			break
		}
	}
	if versionManifestURL == "" {
		return fmt.Errorf("unable to find release version %s", *clientJarVersion)
	}

	versionManifest := struct {
		Downloads map[string]struct {
			URL string `json:"url"`
		} `json:"downloads"`
	}{}
	jsonGrab(versionManifestURL, &versionManifest)

	clientJarURL := versionManifest.Downloads["client"].URL

	resp, err := http.Get(clientJarURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	size, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(dest)
		return err
	}
	fmt.Printf("done, downloaded %.2fMiB\n", float64(size)/1024/1024)

	return err
}

type modelSpec struct {
	Model  string `json:"model"`
	X      int    `json:"x,omitempty"`
	Y      int    `json:"y,omitempty"`
	UVLock bool   `json:"uvlock,omitempty"`
	Weight *int   `json:"weight,omitempty"`
}

func (m *modelSpec) render(name string) {

}

type blockState struct {
	Variants   map[string][]modelSpec
	VariantRaw map[string]json.RawMessage `json:"variants"`
	Multipart  []struct {
		When     []map[string]string
		WhenRaw  map[string]json.RawMessage `json:"when"`
		Apply    []modelSpec
		ApplyRaw json.RawMessage `json:"apply"`
	}
}

func unmarshalBlockState(buf []byte, b *blockState) error {
	err := json.Unmarshal(buf, b)
	if err != nil {
		return errors.Wrap(err, "base unmarshal failed")
	}
	b.Variants = make(map[string][]modelSpec)
	for k, v := range b.VariantRaw {
		specs := []modelSpec{}
		if v[0] == '[' {
			err = json.Unmarshal(v, &specs)
		} else {
			ms := modelSpec{}
			err = json.Unmarshal(v, &ms)
			specs = append(specs, ms)
		}
		if err != nil {
			return errors.Wrap(err, "unmarshaling variant")
		}
		b.Variants[k] = specs
		v = nil
	}
	b.VariantRaw = nil
	for i := range b.Multipart {
		mp := &b.Multipart[i]
		for k, v := range mp.WhenRaw {
			if v[0] == '[' { // OR
				whenBase := []map[string]bool{} // <= 1.15
				err = json.Unmarshal(v, &whenBase)
				if err == nil {
					for _, c := range whenBase {
						m := map[string]string{}
						for k, v := range c {
							if v == true {
								m[k] = "true"
							} else {
								m[k] = "false"
							}
						}
						mp.When = append(mp.When, m)
					}
				} else {
					err = json.Unmarshal(v, &mp.When)
				}
				if err != nil {
					return errors.Wrap(err, "unmarshaling Multipart.When: "+string(v))
				}
			} else {
				var b bool
				var s string
				err = json.Unmarshal(v, &b)
				if err == nil {
					if b {
						s = "true"
					} else {
						s = "false"
					}
				} else {
					err = json.Unmarshal(v, &s)
				}
				if err != nil {
					return errors.Wrap(err, "unmarshaling Multipart.When: "+string(v))
				}
				mp.When = []map[string]string{{k: s}}
			}
		}
		mp.WhenRaw = nil
		if len(mp.ApplyRaw) > 0 {
			if mp.ApplyRaw[0] == '[' {
				err = json.Unmarshal(mp.ApplyRaw, &mp.Apply)
			} else {
				ms := modelSpec{}
				err = json.Unmarshal(mp.ApplyRaw, &ms)
				mp.Apply = append(mp.Apply, ms)
			}
			if err != nil {
				return err
			}
			mp.ApplyRaw = nil
		}
	}
	return nil
}

type blockModelFace struct {
	UV        []float64 `json:"uv"`
	Texture   string    `json:"texture"`
	CullFace  string    `json:"cullface,omitempty"`
	Rotation  int       `json:"rotation,omitempty"`
	TintIndex *int      `json:"tintindex,omitempty"`
}

type blockModel struct {
	Parent           string `json:"parent"`
	AmbientOcclusion *bool  `json:"ambientocclusion,omitempty"`
	/* omitted: display */
	Textures map[string]string `json:"textures"`
	Elements []struct {
		From     []float64 `json:"from"`
		To       []float64 `json:"to"`
		Rotation struct {
			Origin  []float64 `json:"origin,omitempty"`
			Axis    string    `json:"axis,omitempty"`
			Angle   float64   `json:"angle"`
			Rescale *bool     `json:"rescale,omitempty"`
		} `json:"rotation"`
		Shade *bool                     `json:"shade,omitempty"`
		Faces map[string]blockModelFace `json:"faces"`
	} `json:"elements"`
}

type mcItem struct {
	Model *struct {
		Type     string `json:"type"`
		Base     string `json:"base"`
		ModelRaw any    `json:"model"`
		Tints    *[]struct {
			Type        string   `json:"type"`
			Downfall    *float64 `json:"downfall"`
			Temperature *float64 `json:"temperature"`
		} `json:"tints"`
	} `json:"model"`
}

func removeDefaultPrefix(s string) string {
	if strings.HasPrefix(s, "minecraft:") {
		return s[len("minecraft:"):]
	}
	return s
}

type modelEntry struct {
	Layer    layerNumber `json:"layer"`
	Textures []string    `json:"textures,omitempty"`
	Template []uint32    `json:"tmpl,omitempty"`
}

type blockEntry struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name"`
	States      [][]string   `json:"states,omitempty"`
	Solid       bool         `json:"solid,omitempty"`
	Templates   []modelEntry `json:"templates"`
}

type blockentryMetadata struct {
	Blocks []blockEntry `json:"blocks"`
}

type stateConverter struct {
	models     map[string]*blockModel
	columnTops map[string]string
}

func (s *stateConverter) referencedTexturesModel(model *blockModel) ([]string, bool) {
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
	parent := s.models[removeDefaultPrefix(model.Parent)]
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

func printMapSortedByValueDesc(m map[string]int) {
	type kv struct {
		Key   string
		Value int
	}
	var ss []kv
	for k, v := range m {
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})
	for _, pair := range ss {
		fmt.Printf("%s: %d\n", pair.Key, pair.Value)
	}
}

func (s *stateConverter) referencedTextures(st *blockState) ([]string, bool) {
	out := []string{}
	tinted := false

	// fallback: draw ANY texture from ANY sub-model as a cube
	for _, vs := range st.Variants {
		for _, v := range vs {
			model := s.models[removeDefaultPrefix(v.Model)]
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
			parent := s.models[removeDefaultPrefix(model.Parent)]
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
			model := s.models[removeDefaultPrefix(v.Model)]
			if model.Textures != nil {
				for _, tex := range model.Textures {
					out = append(out, tex)
				}
			}
		}
	}
	return out, tinted
}

func (s *stateConverter) applyRotations(ms *modelSpec, model *blockModel) *blockModel {
	// clone model
	var m blockModel
	buf, _ := json.Marshal(model)
	json.Unmarshal(buf, &m)

	// -Z north, -X west

	for ms.X != 0 && ms.X%90 == 0 {
		for i := range m.Elements {
			e := &m.Elements[i]
			/*
				e.From[1], e.From[2] = e.From[2], -e.From[1]
				e.To[1], e.To[2] = e.To[2], -e.To[1] */
			e.Faces["north"], e.Faces["down"], e.Faces["south"], e.Faces["up"] =
				e.Faces["down"], e.Faces["south"], e.Faces["up"], e.Faces["north"]
		}
		ms.X -= 90
	}

	for ms.Y != 0 && ms.Y%90 == 0 {
		for i := range m.Elements {
			e := &m.Elements[i]
			/*
				e.From[1], e.From[2] = e.From[2], -e.From[1]
				e.To[1], e.To[2] = e.To[2], -e.To[1] */
			e.Faces["north"], e.Faces["east"], e.Faces["south"], e.Faces["west"] =
				e.Faces["west"], e.Faces["north"], e.Faces["east"], e.Faces["south"]
		}
		ms.Y -= 90
	}

	// fmt.Printf("ROT\n>> %#v\n>> %#v\n", model.Elements[0].Faces, m.Elements[0].Faces)

	return &m
}

func (s *stateConverter) resolveInheritance(model *blockModel) {
	parentName := model.Parent
	for parentName != "" {
		parent := s.models[removeDefaultPrefix(parentName)]
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

func (m *blockModel) getCubeFaces(faces [6]blockModelFace) ([]string, bool) {
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

func (m *blockModel) renderCube() *modelEntry {
	if len(m.Elements) != 1 {
		return nil
	}
	el := m.Elements[0]
	if !reflect.DeepEqual(el.From, []float64{0, 0, 0}) || !reflect.DeepEqual(el.To, []float64{16, 16, 16}) {
		return nil
	}
	if el.Shade != nil || el.Rotation.Angle != 0 {
		return nil
	}
	texs, tint := m.getCubeFaces([...]blockModelFace{el.Faces["up"], el.Faces["north"], el.Faces["east"], el.Faces["south"], el.Faces["west"], el.Faces["down"]})
	if texs == nil {
		return nil
	}
	if !tint { // texs[1] != texs[2] || texs[2] != texs[3] || texs[3] != texs[4] {
		// grab texs again to match face visibility order
		texs, _ = m.getCubeFaces([...]blockModelFace{el.Faces["west"], el.Faces["east"], el.Faces["south"], el.Faces["north"], el.Faces["up"], el.Faces["down"]})
		m := &modelEntry{
			Layer: layerVoxel,
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
		return &modelEntry{
			Layer:    layerCube,
			Textures: []string{texs[1], texs[0], texs[5]},
			Template: []uint32{0, meta | 1<<30},
		}
	}

	return &modelEntry{
		Layer:    layerCube,
		Textures: []string{texs[0]},
		Template: []uint32{0, meta},
	}
}

func (s *stateConverter) renderModelSpec(name string, ms *modelSpec) modelEntry {
	modelName := removeDefaultPrefix(ms.Model)
	// TODO: check for modelspec X/Y rotations etc
	model := s.models[modelName]
	s.resolveInheritance(model)

	rotated := false

	if ms.X != 0 || ms.Y != 0 {
		model = s.applyRotations(ms, model)
		rotated = true
	}

	cubeSpec := model.renderCube()
	if cubeSpec != nil {
		if rotated && len(cubeSpec.Template) == len(cubeSpec.Textures)*2 && cubeSpec.Template[1]&(1<<31) == 0 {
			cubeSpec.Layer = layerVoxel
		}
		if *genDebug == "all" || *genDebug == name {
			fmt.Printf("CUBE %#v\n", cubeSpec)
		}
		return *cubeSpec
	}

	if name == "grass_block" {
		// render grass blocks as two cubes:
		// * the dirt sides and bottom (no top)
		// * the tinted grass top and side overlay (no bottom)
		if *genDebug == "all" || *genDebug == name {
			fmt.Println("CUBE(GRASS)", name)
		}
		return modelEntry{
			Layer:    layerCube,
			Textures: []string{model.Textures["side"], model.Textures["bottom"], model.Textures["overlay"], model.Textures["top"]},
			Template: []uint32{0, 0b101111 | 1<<30, 0, 0b011111 | 3<<30},
		}
	}

	if model.Parent == "minecraft:block/cross" {
		tex := model.Textures["cross"]
		if *genDebug == "all" || *genDebug == name {
			fmt.Println("CROSS", name, tex)
		}
		return modelEntry{
			Layer:    layerCross,
			Textures: []string{tex},
			Template: []uint32{0, 0b1111111}}
	} else if model.Parent == "minecraft:block/tinted_cross" {
		tex := model.Textures["cross"]
		if *genDebug == "all" || *genDebug == name {
			fmt.Println("TINTED_CROSS", name, tex)
		}
		return modelEntry{
			Layer:    layerCross,
			Textures: []string{tex},
			Template: []uint32{0, 0b111111 | 1<<31}}
	} else if model.Parent == "minecraft:block/crop" {
		tex := model.Textures["crop"]
		if *genDebug == "all" || *genDebug == name {
			fmt.Println("CROP", name, tex)
		}
		return modelEntry{
			Layer:    layerCrop,
			Textures: []string{tex},
			Template: []uint32{0, 0b1111111}}
	}

	if *genDebug == "all" || *genDebug == name {
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
		return modelEntry{Layer: layerCubeFallback, Textures: []string{tex}, Template: []uint32{0, meta}}
	}

	return modelEntry{Layer: -1}
}

func (s *stateConverter) render(name string, st *blockState) blockEntry {
	slist := st.buildStateList()
	smap := buildStateMap(slist)
	if st.Variants[""] != nil {
		model := s.renderModelSpec(name, &st.Variants[""][0])
		if model.Layer >= 0 {
			return blockEntry{Name: name, States: slist, Templates: []modelEntry{model}}
		}
	}
	if len(st.Variants) > 0 {
		tmpls := make([]modelEntry, smap.max()+1)
		for props, models := range st.Variants {
			tmpls[int(smap.getState(props))] = s.renderModelSpec(name, &models[0])
		}
		return blockEntry{Name: name, States: slist, Templates: tmpls}
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
		if *genDebug == "all" || *genDebug == name {
			modelJ, _ := json.MarshalIndent(st, "", "  ")
			fmt.Printf("FALLBACKMULTI %#v %s\n", name, string(modelJ))
		}
		return blockEntry{Name: name, States: slist, Templates: []modelEntry{
			{Layer: layerCubeFallback, Textures: []string{tex}, Template: []uint32{0, tint}}}}
	}

	return blockEntry{}
}

type textureType int

const (
	texUnknown textureType = iota
	texOpaque
	texCutout
	texTranslucent
)

func generate(outDir string) {
	fmt.Println("generating textures")
	jarPath := path.Join(outDir, "client.jar")
	if *clientJarPath != "" {
		if _, err := os.Stat(*clientJarPath); err == nil {
			jarPath = *clientJarPath
		}
	}
	if _, err := os.Stat(jarPath); err != nil {
		fmt.Println("downloading minecraft client jar")
		err := downloadMinecraftJar(jarPath)
		if err != nil {
			fmt.Println("unable to downlaod minecraft jar:", err)
		}
	}
	jar, err := zip.OpenReader(jarPath)
	if err != nil {
		fmt.Println("unable to open jar", jarPath)
	}

	// Load block state to model mapping and textures from jar
	blockStates := map[string]*blockState{}
	stateNames := []string{}
	models := map[string]*blockModel{}
	items := map[string]*mcItem{}
	textures := map[string]image.Image{}
	translations := map[string]any{}
	stringCounts := map[string]int{}

	for _, f := range jar.File {
		if strings.HasSuffix(f.Name, ".png") && strings.HasPrefix(f.Name, "assets/minecraft/textures/") {
			rc, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			name := f.Name[len("assets/minecraft/textures/") : len(f.Name)-4]
			tex, err := png.Decode(rc)
			if err != nil {
				log.Fatal(err)
			}
			textures[name] = tex
			continue
		}
		isClass := strings.HasSuffix(f.Name, ".class")
		if !strings.HasSuffix(f.Name, ".json") && !isClass {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			log.Fatal(err)
		}
		data, err := ioutil.ReadAll(rc)
		if err != nil {
			log.Fatal(err)
		}
		rc.Close()
		if isClass {
			walkClassForCounts(data, stringCounts)
			continue
		}
		name := f.Name[:len(f.Name)-len(".json")]
		fmt.Println("hum", name)
		if strings.HasPrefix(f.Name, "assets/minecraft/lang/") {
			if strings.HasSuffix(f.Name, "deprecated") {
				continue
			}
			fmt.Println(string(data))
			err = json.Unmarshal(data, &translations)
			if err != nil {
				log.Fatal(err)
			}
		}
		if strings.HasPrefix(f.Name, "assets/minecraft/items/") {
			name = strings.Replace(name, "assets/minecraft/items/", "", 1)
			it := &mcItem{}
			err = json.Unmarshal(data, &it)
			if err != nil {
				log.Fatal(err)
			}
			items[name] = it
		}
		if strings.HasPrefix(f.Name, "assets/minecraft/models/block") {
			name = strings.Replace(name, "assets/minecraft/models/", "", 1)
			models[name] = &blockModel{}
			err = json.Unmarshal(data, models[name])
			if err != nil {
				log.Fatal(err)
			}
			if *genDebug == "all" || *genDebug == name {
				fmt.Printf("BLOCKMODEL! %s\n%s\n", name, string(data)) //, models[name])
			}
		}
		if strings.HasPrefix(f.Name, "assets/minecraft/blockstates") {
			name = strings.Replace(name, "assets/minecraft/blockstates/", "", 1)
			blockStates[name] = &blockState{}
			stateNames = append(stateNames, name)
			err = unmarshalBlockState(data, blockStates[name])
			if err != nil {
				log.Fatalf("unable to parse %s: %v", f.Name, err)
			}
			if *genDebug == "all" || *genDebug == name {
				fmt.Printf("BLOCKSTATES! %s, %s => %v\n", name, string(data), blockStates[name])
			}
		}
	}

	if false {
		printMapSortedByValueDesc(stringCounts)
	}

	// Classify textures as opaque, transparent (cutout), translucent
	// This is used to infer solidity-- a cube with all opaque sides
	// is a definite occluder.
	textureClasses := map[string]textureType{}
	for name, tex := range textures {
		ty := texOpaque
		rect := tex.Bounds()
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				_, _, _, a := tex.At(x, y).RGBA()
				if a == 0 && ty == texOpaque {
					ty = texTranslucent
				} else if a > 0 && a < 0xffff && ty < texTranslucent {
					ty = texTranslucent
				}
			}
		}
		textureClasses[name] = ty
	}

	// Process block states to determine model drawing templates
	meta := blockentryMetadata{
		Blocks: []blockEntry{
			{Name: "air"}, {Name: "cave_air"}, {Name: "void_air"},
		},
	}

	blockEntries := &meta.Blocks

	converter := stateConverter{
		columnTops: map[string]string{},
		models:     models,
	}

	for _, name := range stateNames {
		st := blockStates[name]
		entry := converter.render(name, st)
		if len(entry.Templates) > 0 {
			*blockEntries = append(*blockEntries, entry)
		} else {
			fmt.Println("unhandled", name, st)
		}
	}

	// Generate texture atlases and finalize templates
	nameToOldID := map[string]int{"air": 0}
	for id, ent := range blockstateMap {
		if nameToOldID[ent.name] == 0 {
			nameToOldID[ent.name] = id
		}
	}

	atlases := []*image.RGBA{}
	texIDs := []map[string]int{}

	for i := 0; i < int(numRenderLayers); i++ {
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
		diff := (stringCounts[a] + stringCounts["minecraft:"+a]) - (stringCounts[b] + stringCounts["minecraft:"+b])
		if diff != 0 {
			return diff > 0
		}
		return a < b
	})

	for i := range *blockEntries {
		ent := &(*blockEntries)[i]
		splatTexture := func(layer layerNumber, name string, place int) {
			if place == 0 {
				place = texIDs[layer][name]
			}
			if place == 0 {
				place = len(texIDs[layer])
				texIDs[layer][name] = place
			}
			tex := textures[name]
			fmt.Printf("PSL %v %v %v %v %#v\n", layer, name, place, ent.DisplayName, ent.Templates)
			x0 := (place * 16) % 512
			y0 := (place / 32) * 16
			// fmt.Println("splat", name, place, layer, tex.Bounds(), x0, y0)
			draw.Draw(atlases[layer], image.Rect(x0, y0, x0+16, y0+16), tex, image.Point{}, draw.Src)
		}

		if tr, ok := translations["block.minecraft."+ent.Name].(string); ok {
			ent.DisplayName = tr
		}

		for _, model := range ent.Templates {
			if model.Textures == nil {
				continue
			}
			for ti := range model.Textures {
				model.Textures[ti] = removeDefaultPrefix(model.Textures[ti])
			}

			layer := model.Layer
			if len(model.Textures)+len(texIDs[layer]) >= 512 {
				fmt.Println("warn: overrun for", ent.Name)
				break
			}
			if layer == layerCube {
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

			tid := texIDs[layer][model.Textures[0]]
			if tid >= 256 && layer == layerCube {
				panic(fmt.Sprintf("texID too large! layer %d %#v: %v\n%v", layer, ent, tid, texIDs))
			}
			if tid >= 512 {
				panic(fmt.Sprintf("texID too large! %#v: %v", ent, tid))
			}
			if layer == layerCube || layer == layerVoxel || layer == layerCubeFallback {
				solid := true
				for _, tex := range model.Textures {
					if textureClasses[tex] != texOpaque {
						solid = false
					}
				}
				if solid {
					ent.Solid = true
				}
			}
			model.Template[0] |= uint32(tid) << 24
			model.Template[1] |= uint32(tid>>8) << 30
			if ent.Name == "grass_block" && len(model.Template) == 4 {
				// render grass blocks as two cubes:
				// * the dirt sides and bottom (no top)
				// * & len(model.Template) == 4 {the tinted grass top and side overlay (no bottom)
				model.Template[2] |= uint32(texIDs[layer][model.Textures[2]]) << 24
			} else if layer == layerVoxel && len(model.Textures) > 1 {
				for i, t := range model.Textures {
					tid := texIDs[layer][t]
					model.Template[2*i] |= uint32(tid) << 24
					model.Template[2*i+1] |= uint32(tid>>8) << 30
				}
			}
			if ent.Name == "water" {
				model.Template[1] |= 1 << 31
			}
			if *genDebug == "all" || *genDebug == ent.Name {
				fmt.Printf("L%d %s %v=%d %v %08x %08x\n",
					layer, ent.Name, model.Textures, tid, textures[model.Textures[0]].Bounds(),
					model.Template[0], model.Template[1])
			}
		}
	}

	os.MkdirAll(path.Join(outDir, "textures"), 0755)

	for layer, atlas := range atlases {
		f, err := os.Create(path.Join(outDir, "textures", fmt.Sprintf("atlas%d.png", layer)))
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		err = png.Encode(f, atlas)
		if err != nil {
			log.Fatal(err)
		}
	}

	layerCounts := map[int]int{}

	// Wipe unneeded texture references, and count layers for each block
	for _, b := range meta.Blocks {
		for i := range b.Templates {
			b.Templates[i].Textures = nil
			layerCounts[int(b.Templates[i].Layer)] += 1
		}
	}

	// Write out blockstate template metadata
	buf, err := json.Marshal(meta)
	if err != nil {
		log.Fatal(err)
	}
	buf = []byte(strings.Replace(strings.Replace(string(buf), "},", "},\n", -1), "\n{\"layer", "\n    {\"layer", -1))
	fmt.Println("blockmeta.json size:", len(string(buf)))
	for i := 0; i < int(numRenderLayers); i++ {
		fmt.Println("layer", layerNames[i], layerCounts[i])
	}

	f, err := os.Create(path.Join(outDir, "blockmeta.json"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	_, err = f.Write(buf)
	if err != nil {
		log.Fatal(err)
	}

	loadBlockMapper(buf)
}
