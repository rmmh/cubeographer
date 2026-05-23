package resourcepack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type ResourcePackSlice []string

func (r *ResourcePackSlice) String() string {
	return strings.Join(*r, ", ")
}

func (r *ResourcePackSlice) Set(value string) error {
	*r = append(*r, value)
	return nil
}

var ResourcePacks ResourcePackSlice

func init() {
	flag.Var(&ResourcePacks, "resourcepack", "path to a resource pack (zip or folder), can be specified multiple times")
}

type PackMcMeta struct {
	Pack struct {
		PackFormat int `json:"pack_format"`
	} `json:"pack"`
}

func (rj *ResourceJar) getZipPackFormat(zipPath string) (int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "pack.mcmeta" {
			rc, err := f.Open()
			if err != nil {
				return 0, err
			}
			defer rc.Close()
			var meta PackMcMeta
			if err := json.NewDecoder(rc).Decode(&meta); err != nil {
				return 0, err
			}
			return meta.Pack.PackFormat, nil
		}
	}
	return 0, nil
}

func (rj *ResourceJar) getDirPackFormat(dirPath string) (int, error) {
	metaPath := filepath.Join(dirPath, "pack.mcmeta")
	f, err := os.Open(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	var meta PackMcMeta
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		return 0, err
	}
	return meta.Pack.PackFormat, nil
}

func (rj *ResourceJar) compileRenameMap(vfrom, vto int) map[string]string {
	renameMap := make(map[string]string)
	for _, m := range MigrateBlockMaps {
		if m.Version > vfrom && m.Version <= vto {
			for k, v := range renameMap {
				if next, ok := m.Blockmap[v]; ok {
					renameMap[k] = next
				}
			}
			for k, v := range m.Blockmap {
				renameMap[k] = v
			}
		}
	}
	return renameMap
}

func (rj *ResourceJar) OverlayPack(packPath string) error {
	fi, err := os.Stat(packPath)
	if err != nil {
		return err
	}

	packFormat := 0
	if fi.IsDir() {
		packFormat, err = rj.getDirPackFormat(packPath)
	} else {
		packFormat, err = rj.getZipPackFormat(packPath)
	}
	if err != nil {
		slog.Warn("failed to read pack.mcmeta", "path", packPath, "err", err)
	}

	vfrom := 0
	vto := rj.WorldVersion
	if vto == 0 {
		vto = 4000
	}

	renameMap := rj.compileRenameMap(vfrom, vto)
	if len(renameMap) > 0 {
		slog.Info("migrating resource pack", "packFormat", packFormat, "vfrom", vfrom, "vto", vto, "renamesCount", len(renameMap))
	}

	if fi.IsDir() {
		return rj.overlayFromDir(packPath, renameMap)
	}
	return rj.overlayFromZip(packPath, renameMap)
}

func (rj *ResourceJar) overlayFromZip(zipPath string, renameMap map[string]string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		m := assetRe.FindStringSubmatch(f.Name)
		if m == nil {
			continue
		}
		ns, kind, name, ext := m[1], m[2], m[3], m[4]
		name = ns + ":" + name

		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}

		if err := rj.overlayAsset(kind, name, ext, data, f.Name, renameMap); err != nil {
			return err
		}
	}
	return nil
}

func (rj *ResourceJar) overlayFromDir(dirPath string, renameMap map[string]string) error {
	return filepath.WalkDir(dirPath, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		m := assetRe.FindStringSubmatch(rel)
		if m == nil {
			return nil
		}
		ns, kind, name, ext := m[1], m[2], m[3], m[4]
		name = ns + ":" + name

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		return rj.overlayAsset(kind, name, ext, data, rel, renameMap)
	})
}

func (rj *ResourceJar) overlayAsset(kind, name, ext string, data []byte, pathInPack string, renameMap map[string]string) error {
	if ext == "png" && kind == "textures" {
		tex, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			slog.Warn("failed to decode PNG in resource pack", "file", pathInPack, "err", err)
			return nil
		}
		name = MigrateName(name, renameMap)
		rj.Textures[RemoveDefaultPrefix(name)] = tex
	}
	if ext == "json" {
		switch kind {
		case "lang":
			parts := strings.SplitN(name, ":", 2)
			if len(parts) == 2 {
				ns := parts[0]
				if ns != "minecraft" || name == "minecraft:deprecated" {
					return nil
				}
			}
			var trans map[string]string
			if err := json.Unmarshal(data, &trans); err != nil {
				slog.Warn("failed to decode lang JSON in resource pack", "file", pathInPack, "err", err)
				return nil
			}
			for k, v := range trans {
				migratedK := k
				for oldB, newB := range renameMap {
					if strings.Contains(k, oldB) {
						migratedK = strings.ReplaceAll(k, oldB, newB)
						break
					}
				}
				rj.Translations[migratedK] = v
			}
		case "models":
			model := &Model{}
			if err := json.Unmarshal(data, model); err != nil {
				slog.Warn("failed to decode model JSON in resource pack", "file", pathInPack, "err", err)
				return nil
			}
			MigrateModel(model, renameMap)
			name = MigrateName(name, renameMap)
			rj.Models[name] = model
		case "blockstates":
			bs := &BlockState{}
			if err := json.Unmarshal(data, bs); err != nil {
				slog.Warn("failed to decode blockstate JSON in resource pack", "file", pathInPack, "err", err)
				return nil
			}
			MigrateBlockState(bs, renameMap)
			name = MigrateName(name, renameMap)
			rj.BlockStates[name] = bs
		}
	}
	return nil
}
