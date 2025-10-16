package resourcepack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/nsf/jsondiff"
	"github.com/pkg/errors"
)

var MajorMCVersions = []string{
	// may want to support some day: "1.7.10",
	"1.8.9",
	"1.9.4",
	"1.10.2",
	"1.11.2",
	"1.12.2",
	"1.13.2",
	"1.14.4",
	"1.15.2",
	"1.16.5",
	"1.17.1",
	"1.18.2",
	"1.19.4",
	"1.20.6",
	"1.21.10",
}

func jsonGrab(url string, val interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(val)
}

func DownloadMinecraftJar(dest, version string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

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
	if version == "latest" {
		version = manifest.Latest.Release
		fmt.Println("downloading version", version)
	}
	versionManifestURL := ""
	for _, v := range manifest.Versions {
		if v.ID == version {
			versionManifestURL = v.URL
			break
		}
	}
	if versionManifestURL == "" {
		return fmt.Errorf("unable to find release version %s", version)
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

type ModelSpec struct {
	Model  string `json:"model"`
	X      *int   `json:"x,omitempty"`
	Y      *int   `json:"y,omitempty"`
	UVLock *bool  `json:"uvlock,omitempty"`
	Weight *int   `json:"weight,omitempty"`
}

// SingleOrSlice wraps a slice with custom JSON marshaling/unmarshaling behavior.
// Single-element slices are encoded as that element, otherwise it's encoded as an array.
type SingleOrSlice[T any] []T

func (s *SingleOrSlice[T]) Slice() []T {
	return []T(*s)
}

func (s SingleOrSlice[T]) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s.Slice()[0])
	}
	return json.Marshal(s.Slice())
}

func (s *SingleOrSlice[T]) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		return json.Unmarshal(data, (*[]T)(s))
	}
	*s = make([]T, 1)
	return json.Unmarshal(data, &(([]T)(*s))[0])
}

type BlockStateWhenClause struct {
	IsOr    bool
	Clauses []map[string]any
}

func (c *BlockStateWhenClause) UnmarshalJSON(data []byte) error {
	var temp map[string]json.RawMessage
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if orClauses, ok := temp["OR"]; ok {
		c.IsOr = true
		return json.Unmarshal(orClauses, &c.Clauses)
	} else if andClauses, ok := temp["AND"]; ok {
		return json.Unmarshal(andClauses, &c.Clauses)
	}

	var singleClause map[string]any
	if err := json.Unmarshal(data, &singleClause); err != nil {
		return err
	}
	c.Clauses = append(c.Clauses[:0], singleClause)
	return nil
}

func (c BlockStateWhenClause) MarshalJSON() ([]byte, error) {
	if c.IsOr {
		return json.Marshal(map[string][]map[string]any{
			"OR": c.Clauses,
		})
	} else if len(c.Clauses) != 1 {
		return json.Marshal(map[string][]map[string]any{
			"AND": c.Clauses,
		})
	}
	return json.Marshal(c.Clauses[0])
}

type BlockState struct {
	Variants  map[string]SingleOrSlice[ModelSpec] `json:"variants,omitempty"`
	Multipart []struct {
		When  *BlockStateWhenClause    `json:"when,omitempty"`
		Apply SingleOrSlice[ModelSpec] `json:"apply,omitempty"`
	} `json:"multipart,omitempty"`
}

type BlockModelFace struct {
	UV        []float64 `json:"uv,omitempty"`
	Texture   string    `json:"texture"`
	Cull      *bool     `json:"cull,omitempty"`
	CullFace  string    `json:"cullface,omitempty"`
	Rotation  *int      `json:"rotation,omitempty"`
	TintIndex *int      `json:"tintindex,omitempty"`
}

type ModelElement struct {
	From     []float64 `json:"from"`
	To       []float64 `json:"to"`
	Rotation struct {
		Origin  []float64 `json:"origin,omitempty"`
		Axis    string    `json:"axis,omitempty"`
		Angle   float64   `json:"angle"`
		Rescale *bool     `json:"rescale,omitempty"`
	} `json:"rotation,omitzero"`
	Shade         *bool                     `json:"shade,omitempty"`
	Faces         map[string]BlockModelFace `json:"faces"`
	Comment       string                    `json:"__comment,omitempty"`
	Name          string                    `json:"name,omitempty"`
	LightEmission int                       `json:"light_emission,omitempty"`
}

type ModelTransform struct {
	Rotation    []float64 `json:"rotation,omitempty"`
	Scale       []float64 `json:"scale,omitempty"`
	Translation []float64 `json:"translation,omitempty"`
}

type ModelGroup struct {
	Children []int     `json:"children"`
	Color    int       `json:"color"`
	Name     string    `json:"name"`
	Origin   []float64 `json:"origin"`
}

type ModelOverride struct {
	Model     string             `json:"model"`
	Predicate map[string]float64 `json:"predicate"`
}

type Model struct {
	Parent           string                     `json:"parent,omitempty"`
	AmbientOcclusion *bool                      `json:"ambientocclusion,omitempty"`
	Textures         map[string]string          `json:"textures,omitempty"`
	TextureSize      []int                      `json:"texture_size,omitempty"`
	Elements         []*ModelElement            `json:"elements,omitempty"`
	Groups           []*ModelGroup              `json:"groups,omitempty"`
	Display          map[string]*ModelTransform `json:"display,omitempty"`
	GuiLight         string                     `json:"gui_light,omitempty"`
	Overrides        []ModelOverride            `json:"overrides,omitempty"`
}

type ModelRanges struct {
	Threshold float64   `json:"threshold"`
	Model     *ModelRef `json:"model"`
}

type ModelTint struct {
	Type        string   `json:"type"`
	Default     *float64 `json:"default,omitempty"`
	Value       *float64 `json:"value,omitempty"`
	Downfall    *float64 `json:"downfall,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
}

type ModelRef struct {
	Type               string         `json:"type"`
	Base               string         `json:"base,omitempty"`
	BlockStateProperty string         `json:"block_state_property,omitempty"`
	Property           string         `json:"property,omitempty"`
	OnFalse            *ModelRef      `json:"on_false,omitempty"` // type=minecraft:condition
	OnTrue             *ModelRef      `json:"on_true,omitempty"`
	ModelRaw           any            `json:"model,omitempty"`
	Models             []*ModelRef    `json:"models,omitempty"`    // type=minecraft:composite
	Entries            []*ModelRanges `json:"entries,omitempty"`   // type=minecraft:range_dispatch
	Scale              *float64       `json:"scale,omitempty"`     // type=minecraft:range_dispatch
	Period             *float64       `json:"period,omitempty"`    // type=minecraft:range_dispatch
	Source             string         `json:"source,omitempty"`    // type=minecraft:range_dispatch
	Target             string         `json:"target,omitempty"`    // type=minecraft:range_dispatch
	Pattern            string         `json:"pattern,omitempty"`   // type=minecraft:select
	Component          string         `json:"component,omitempty"` // type=minecraft:select
	Tints              []*ModelTint   `json:"tints,omitempty"`
	Cases              *[]struct {
		Model *ModelRef `json:"model"`
		When  any       `json:"when"`
	} `json:"cases,omitempty"`
	Fallback *ModelRef `json:"fallback,omitempty"`
}

type ResourceJar struct {
	Blocks       map[string]*Block
	BlockStates  map[string]*BlockState
	Models       map[string]*Model
	Textures     map[string]image.Image
	Translations map[string]string
	StringCounts map[string]int
}

type Block struct{}

var assetRe = regexp.MustCompile(`^assets/(\w+)/(\w+)/(.*?)\.(.*)$`)

func JarFromZip(jar *zip.ReadCloser) (*ResourceJar, error) {
	rj := &ResourceJar{
		Blocks:       map[string]*Block{},
		BlockStates:  map[string]*BlockState{},
		Models:       map[string]*Model{},
		Textures:     map[string]image.Image{},
		Translations: map[string]string{},
		StringCounts: map[string]int{},
	}

	sort.Slice(jar.File, func(i, j int) bool {
		return jar.File[i].Name < jar.File[j].Name
	})

	mm := 0
	for _, f := range jar.File {
		m := assetRe.FindStringSubmatch(f.Name)
		if m == nil {
			if strings.HasSuffix(f.Name, ".class") {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				buf, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return nil, err
				}
				walkClassForCounts(buf, rj.StringCounts)
			}
			continue
		}
		ns, kind, name, ext := m[1], m[2], m[3], m[4]
		name = ns + ":" + name
		if ext == "png" && kind == "textures" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			tex, err := png.Decode(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			rj.Textures[name] = tex
		}
		if ext == "json" {
			rc, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			var decode any

			switch kind {
			case "lang":
				if ns != "minecraft" || name == "minecraft:deprecated" {
					continue
				}
				err = json.Unmarshal(data, &rj.Translations)
				if err != nil {
					return nil, errors.Wrapf(err, "unable to decode %s", f.Name)
				}
				decode = rj.Translations
			case "models":
				model := &Model{}
				err = json.Unmarshal(data, model)
				if err != nil {
					return nil, err
				}
				rj.Models[name] = model
				decode = model
			case "blockstates":
				bs := &BlockState{}
				err = json.Unmarshal(data, bs)
				if err != nil {
					return nil, errors.Wrapf(err, "unable to decode %s", f.Name)
				}
				rj.BlockStates[name] = bs
				decode = bs
			}

			if decode != nil {
				var got, want any
				json.Unmarshal(data, &want)
				buf, _ := json.Marshal(decode)
				json.Unmarshal(buf, &got)
				if !reflect.DeepEqual(got, want) {
					mm++
					slog.Warn("mismatch decoding", "file", f.Name)
					fmt.Println(string(data))
					opts := jsondiff.DefaultConsoleOptions()
					opts.CompareNumbers = func(a, b json.Number) bool {
						av, _ := a.Float64()
						bv, _ := b.Float64()
						return av == bv
					}
					diff, str := jsondiff.Compare(data, buf, &opts)
					fmt.Println(diff, str)
				}
			}
		}
	}
	if mm > 0 {
		fmt.Println("mismatch count:", mm)
	}

	return rj, nil
}
