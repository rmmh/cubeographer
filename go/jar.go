package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"path"
	"sort"
	"strings"
)

const numRenderLayers = 3

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func findMinecraftJar() string {
	dirs := []string{
		path.Join(os.Getenv("HOME"), ".minecraft/versions"),
	}
	latestJar := ""
	latestTime := ""
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			files, err := ioutil.ReadDir(d)
			if err != nil {
				continue
			}
			for _, f := range files {
				if !f.IsDir() {
					continue
				}
				fp := path.Join(d, f.Name())
				jp := path.Join(fp, f.Name()+".jar")
				ip := path.Join(fp, f.Name()+".json")

				if !fileExists(jp) || !fileExists(ip) {
					continue
				}
				ib, err := ioutil.ReadFile(ip)
				if err != nil {
					continue
				}
				info := struct {
					Time string `json:"time"`
				}{}
				json.Unmarshal(ib, &info)
				if info.Time > latestTime {
					latestTime = info.Time
					latestJar = jp
				}
			}
		}
	}
	return latestJar
}

type modelSpec struct {
	Model  string `json:"model"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	UVLock bool   `json:"uvlock"`
	Weight *int   `json:"weight"`
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
		return err
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
			return err
		}
		b.Variants[k] = specs
		v = nil
	}
	b.VariantRaw = nil
	for i := range b.Multipart {
		mp := &b.Multipart[i]
		for k, v := range mp.WhenRaw {
			if v[0] == '[' { // OR
				err = json.Unmarshal(v, &mp.When)
				if err != nil {
					return err
				}
			} else {
				mp.When = []map[string]string{{k: string(v)}}
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

type blockModel struct {
	Parent           string `json:"parent"`
	AmbientOcclusion *bool  `json:"ambientocclusion"`
	/* omitted: display */
	Textures map[string]string `json:"textures"`
	Elements []struct {
		From     []float64 `json:"from"`
		To       []float64 `json:"to"`
		Rotation struct {
			Origin  []float64 `json:"origin"`
			Axis    string    `json:"axis"`
			Angle   float64   `json:"angle"`
			Rescale *bool     `json:"rescale"`
		} `json:"rotation"`
		Shade *bool `json:"shade"`
		Faces map[string]struct {
			UV        []float64 `json:"uv"`
			Texture   string    `json:"texture"`
			CullFace  string    `json:"cullface"`
			Rotation  int       `json:"rotation"`
			TintIndex *int      `json:"tintindex"`
		}
	} `json:"elements"`
}

func removeDefaultPrefix(s string) string {
	if strings.HasPrefix(s, "minecraft:") {
		return s[len("minecraft:"):]
	}
	return s
}

type blockEntry struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Textures    []string `json:"textures,omitempty"`
	Template    []uint32 `json:"tmpl,omitempty"`
	Solid       bool     `json:"solid,omitempty"`
}

type blockentryMetadata struct {
	Layers [numRenderLayers][]blockEntry `json:"layers"`
}

type stateConverter struct {
	models     map[string]*blockModel
	columnTops map[string]string
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
				if parent.Textures != nil {
					for _, tex := range parent.Textures {
						out = append(out, tex)
					}
				}
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
			parent := s.models[removeDefaultPrefix(model.Parent)]
			if parent != nil && parent.Textures != nil {
				for _, tex := range model.Textures {
					out = append(out, tex)
				}
			}
		}
	}
	return out, tinted
}

func (s *stateConverter) render(name string, st *blockState) (int, []blockEntry) {
	if st.Variants[""] != nil {
		modelName := removeDefaultPrefix(st.Variants[""][0].Model)
		model := s.models[modelName]
		if model.Parent == "minecraft:block/cube_all" {
			tex := model.Textures["all"]
			fmt.Println("CUBE_ALL", name, tex)
			return 0, []blockEntry{{
				Name: name, Textures: []string{tex},
				Template: []uint32{0, 0}}}
		} else if model.Parent == "minecraft:block/cube_column" {
			side := model.Textures["side"]
			end := model.Textures["end"]
			if s.columnTops[side] == "" {
				s.columnTops[side] = end
				fmt.Println("CUBE_COLUMN", name, side, end)
				return 0, []blockEntry{{
					Name: name, Textures: []string{side, end},
					Template: []uint32{0, 1 << 30}}}
			}
		} else if model.Parent == "minecraft:block/cube_bottom_top" {
			side := model.Textures["side"]
			top := model.Textures["top"]
			bottom := model.Textures["bottom"]
			if s.columnTops[side] == "" {
				s.columnTops[side] = top
				fmt.Println("CUBE_BOTTOM_TOP", name, side, top, bottom)
				return 0, []blockEntry{{
					Name: name, Textures: []string{side, top, bottom},
					Template: []uint32{0, 1 << 30}}}
			}
		} else if model.Parent == "minecraft:block/cross" {
			tex := model.Textures["cross"]
			fmt.Println("CROSS", name, tex)
			return 1, []blockEntry{{Name: name, Textures: []string{tex},
				Template: []uint32{0, 0}}}
		} else if model.Parent == "minecraft:block/tinted_cross" {
			tex := model.Textures["cross"]
			fmt.Println("TINTED_CROSS", name, tex)
			return 1, []blockEntry{{Name: name, Textures: []string{tex},
				Template: []uint32{0, 1 << 31}}}
		}
		// fmt.Println(name, model)
	}

	// fallback: draw ANY texture from ANY sub-model as a cube
	textures, tinted := s.referencedTextures(st)
	sort.Strings(textures)
	for _, tex := range textures {
		if tex[0] == '#' {
			continue
		}
		tint := uint32(0)
		if tinted {
			tint |= 1 << 31
		}
		return 2, []blockEntry{{Name: name, Textures: []string{tex},
			Template: []uint32{0, tint}}}
	}

	return -1, nil
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
	jarPath := findMinecraftJar()
	if jarPath == "" {
		fmt.Println("couldn't find minecraft jar")
		return
	}
	jar, err := zip.OpenReader(jarPath)
	if err != nil {
		fmt.Println("unable to open jar", jarPath)
	}

	// Load block state to model mapping and textures from jar
	blockStates := map[string]*blockState{}
	stateNames := []string{}
	models := map[string]*blockModel{}
	textures := map[string]image.Image{}
	translations := map[string]string{}

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
		if !strings.HasSuffix(f.Name, ".json") {
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
		name := f.Name[:len(f.Name)-len(".json")]
		if strings.HasPrefix(f.Name, "assets/minecraft/lang/") {
			err = json.Unmarshal(data, &translations)
			if err != nil {
				log.Fatal(err)
			}
		}
		if strings.HasPrefix(f.Name, "assets/minecraft/models/block") {
			name = strings.Replace(name, "assets/minecraft/models/", "", 1)
			models[name] = &blockModel{}
			err = json.Unmarshal(data, models[name])
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("BLOCKMODEL! %s\n%s\n", name, string(data)) //, models[name])
		}
		if strings.HasPrefix(f.Name, "assets/minecraft/blockstates") {
			name = strings.Replace(name, "assets/minecraft/blockstates/", "", 1)
			blockStates[name] = &blockState{}
			stateNames = append(stateNames, name)
			err = unmarshalBlockState(data, blockStates[name])
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("BLOCKSTATES!", name, string(data)) //, blockStates[name], err)
		}
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
		Layers: [numRenderLayers][]blockEntry{
			{{Name: "air"}, {Name: "cave_air"}, {Name: "void_air"}},
		},
	}

	blockEntries := &meta.Layers

	converter := stateConverter{
		columnTops: map[string]string{},
		models:     models,
	}

	for _, name := range stateNames {
		st := blockStates[name]
		model := blockModel{}

		layer, entries := converter.render(name, st)
		if layer >= 0 {
			blockEntries[layer] = append(blockEntries[layer], entries...)
		} else {
			fmt.Println("unhandled", name, model.Parent, st)
		}
	}

	// Generate texture atlases and finalize templates
	nameToOldID := map[string]int{"air": 0}
	for id, ent := range blockstateMap {
		if nameToOldID[ent.name] == 0 {
			nameToOldID[ent.name] = id
		}
	}

	for layer, ents := range blockEntries {
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

		texIDs := map[string]int{"air": 0}

		splatTexture := func(name string, place int) {
			if place == 0 {
				place = texIDs[name]
			}
			if place == 0 {
				place = len(texIDs)
				texIDs[name] = place
			}
			tex := textures[name]
			x0 := (place * 16) % 512
			y0 := (place / 32) * 16
			// fmt.Println("splat", name, place, tex.Bounds(), x0, y0)
			draw.Draw(atlas, image.Rect(x0, y0, x0+16, y0+16), tex, image.ZP, draw.Src)
		}

		fmt.Println("=======\nlayer:", layer, "blocks:", len(ents))

		// sort blocks so that blocks with assigned block IDs
		// come first in the correct order
		sort.SliceStable(ents, func(i, j int) bool {
			a := ents[i].Name
			b := ents[j].Name
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
			return a < b
		})

		for i := range ents {
			ent := &ents[i]
			ent.DisplayName = translations["block.minecraft."+ent.Name]

			if ent.Textures == nil {
				continue
			}
			for ti := range ent.Textures {
				ent.Textures[ti] = removeDefaultPrefix(ent.Textures[ti])
			}

			if layer == 0 { // first layer handles columns specially (top/bottom)
				splatTexture(ent.Textures[0], 0)
				if len(ent.Textures) > 1 {
					splatTexture(ent.Textures[1], texIDs[ent.Textures[0]]+256)
					if len(ent.Textures) == 3 {
						splatTexture(ent.Textures[2], texIDs[ent.Textures[0]]+512)
					} else {
						splatTexture(ent.Textures[1], texIDs[ent.Textures[0]]+512)
					}
				}
			} else {
				for _, tex := range ent.Textures {
					splatTexture(tex, 0)
				}
			}

			tid := texIDs[ent.Textures[0]]
			if tid >= 256 && layer == 0 {
				panic(fmt.Sprintf("texID too large! %#v: %v", ent, tid))
			}
			if tid >= 1024 {
				panic(fmt.Sprintf("texID too large! %#v: %v", ent, tid))
			}
			if layer == 0 || layer == 2 {
				solid := true
				for _, tex := range ent.Textures {
					if textureClasses[tex] != texOpaque {
						solid = false
					}
				}
				if solid {
					ent.Solid = true
				}
			}
			ent.Template[0] |= uint32(tid) << 24
			ent.Template[1] |= uint32(tid>>8) << 30
			if ent.Name == "water" {
				ent.Template[1] |= 1 << 31
			}
			fmt.Printf("%s %v=%d %v %08x %08x\n",
				ent.Name, ent.Textures, tid, textures[ent.Textures[0]].Bounds(),
				ent.Template[0], ent.Template[1])
		}

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

	// Write out blockstate template metadata
	buf, err := json.Marshal(meta)
	if err != nil {
		log.Fatal(err)
	}
	buf = []byte(strings.Replace(string(buf), "}", "}\n", -1))
	fmt.Println(len(string(buf)))

	f, err := os.Create(path.Join(outDir, "blockmeta.json"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	_, err = f.Write(buf)
	if err != nil {
		log.Fatal(err)
	}
}
