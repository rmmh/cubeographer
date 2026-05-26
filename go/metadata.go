package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/maruel/natural"
	"github.com/rmmh/cubeographer/go/region"
)

type SpaceSeparatedSlice []string

func (s SpaceSeparatedSlice) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.Join(s, " "))
}

func (s *SpaceSeparatedSlice) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	if str == "" {
		*s = nil
		return nil
	}
	*s = strings.Fields(str)
	return nil
}

func (s SpaceSeparatedSlice) Sort() {
	sort.Slice(s, func(i, j int) bool {
		return natural.Less(s[i], s[j])
	})
}

type MapMetadata struct {
	FullRegions SpaceSeparatedSlice `json:"full_regions"`
	LodRegions  SpaceSeparatedSlice `json:"lod_regions"`
	TileRegions SpaceSeparatedSlice `json:"tile_regions"`
}

func ReadMapMetadata(metadataPath string) (MapMetadata, error) {
	var meta MapMetadata
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(data, &meta)
	return meta, err
}

func WriteMapMetadata(mapDir string, regionDir string, mode string) error {
	if mode == "" {
		mode = "full"
	}

	var metadata MapMetadata

	if regionDir != "" {
		// Live server mode: scan the regionDir for region files
		files, err := region.ReadDir(regionDir)
		if err != nil {
			return err
		}

		regionRe := regexp.MustCompile(`^r\.(-?\d+)\.(-?\d+)\.(?:mca|zvcr3)$`)

		for _, file := range files {
			if file.IsDir() {
				continue
			}
			m := regionRe.FindStringSubmatch(file.Name())
			if len(m) != 3 {
				continue
			}

			coordStr := m[1] + "." + m[2]
			if mode == "tile" {
				metadata.TileRegions = append(metadata.TileRegions, coordStr)
			} else if mode == "lod" {
				metadata.LodRegions = append(metadata.LodRegions, coordStr)
			} else {
				metadata.FullRegions = append(metadata.FullRegions, coordStr)
			}
		}
	} else {
		tilesDir := path.Join(mapDir, "tiles")
		lodsDir := path.Join(mapDir, "lods")

		// Read tiles directory to find all region files
		files, err := os.ReadDir(tilesDir)
		if err != nil {
			if os.IsNotExist(err) {
				// If tiles directory doesn't exist, write empty metadata
				return writeMetadataFile(mapDir, metadata)
			}
			return err
		}

		regionRe := regexp.MustCompile(`^r\.(-?\d+)\.(-?\d+)\.png$`)

		for _, file := range files {
			if file.IsDir() {
				continue
			}
			m := regionRe.FindStringSubmatch(file.Name())
			if len(m) != 3 {
				continue
			}

			coordStr := m[1] + "." + m[2]

			if mode == "tile" {
				// In tile mode, everything is a tile region
				metadata.TileRegions = append(metadata.TileRegions, coordStr)
			} else {
				hasBin := fileExists(path.Join(lodsDir, fmt.Sprintf("r.%s.%s.bin", m[1], m[2])))

				if mode == "lod" {
					// In lod mode, never classify as full
					if hasBin {
						metadata.LodRegions = append(metadata.LodRegions, coordStr)
					} else {
						metadata.TileRegions = append(metadata.TileRegions, coordStr)
					}
				} else {
					// full mode: check for cmt too
					hasCmt := fileExists(path.Join(mapDir, fmt.Sprintf("r.%s.%s.0.cmt", m[1], m[2])))
					if hasCmt && hasBin {
						metadata.FullRegions = append(metadata.FullRegions, coordStr)
					} else if hasBin {
						metadata.LodRegions = append(metadata.LodRegions, coordStr)
					} else {
						metadata.TileRegions = append(metadata.TileRegions, coordStr)
					}
				}
			}
		}
	}

	metadata.FullRegions.Sort()
	metadata.LodRegions.Sort()
	metadata.TileRegions.Sort()

	log.Printf("Generated map metadata for %s (mode=%s): full=%d, lod=%d, tile=%d",
		mapDir, mode, len(metadata.FullRegions), len(metadata.LodRegions), len(metadata.TileRegions))

	return writeMetadataFile(mapDir, metadata)
}

func fileExists(filepath string) bool {
	info, err := os.Stat(filepath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func writeMetadataFile(mapDir string, metadata MapMetadata) error {
	if err := os.MkdirAll(mapDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(mapDir, "metadata.json"), data, 0644)
}
