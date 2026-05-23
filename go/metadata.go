package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type MapMetadata struct {
	FullRegions string `json:"full_regions"`
	LodRegions  string `json:"lod_regions"`
	TileRegions string `json:"tile_regions"`
}

type regionCoord struct {
	rx, rz int
}

func WriteMapMetadata(mapDir string, mode string) error {
	tilesDir := path.Join(mapDir, "tiles")
	lodsDir := path.Join(mapDir, "lods")

	if mode == "" {
		mode = "full"
	}

	// TODO: make this do the right thing for live server by
	// scanning for region files and reporting them all as full.

	// Read tiles directory to find all region files
	files, err := os.ReadDir(tilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// If tiles directory doesn't exist, write empty metadata
			metadata := MapMetadata{
				FullRegions: "",
				LodRegions:  "",
				TileRegions: "",
			}
			return writeMetadataFile(mapDir, metadata)
		}
		return err
	}

	regionRe := regexp.MustCompile(`^r\.(-?\d+)\.(-?\d+)\.png$`)

	var fullList []regionCoord
	var lodList []regionCoord
	var tileList []regionCoord

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		m := regionRe.FindStringSubmatch(file.Name())
		if len(m) != 3 {
			continue
		}
		rx, err1 := strconv.Atoi(m[1])
		rz, err2 := strconv.Atoi(m[2])
		if err1 != nil || err2 != nil {
			continue
		}

		coord := regionCoord{rx: rx, rz: rz}

		if mode == "tile" {
			// In tile mode, everything is a tile region
			tileList = append(tileList, coord)
		} else {
			hasBin := fileExists(path.Join(lodsDir, fmt.Sprintf("r.%d.%d.bin", rx, rz)))

			if mode == "lod" {
				// In lod mode, never classify as full
				if hasBin {
					lodList = append(lodList, coord)
				} else {
					tileList = append(tileList, coord)
				}
			} else {
				// full mode: check for cmt too
				hasCmt := fileExists(path.Join(mapDir, fmt.Sprintf("r.%d.%d.0.cmt", rx, rz)))
				if hasCmt && hasBin {
					fullList = append(fullList, coord)
				} else if hasBin {
					lodList = append(lodList, coord)
				} else {
					tileList = append(tileList, coord)
				}
			}
		}
	}

	// Sort helper to ensure deterministic output
	sortCoords := func(coords []regionCoord) string {
		sort.Slice(coords, func(i, j int) bool {
			if coords[i].rx != coords[j].rx {
				return coords[i].rx < coords[j].rx
			}
			return coords[i].rz < coords[j].rz
		})
		var sb strings.Builder
		for i, coord := range coords {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(fmt.Sprintf("%d.%d", coord.rx, coord.rz))
		}
		return sb.String()
	}

	metadata := MapMetadata{
		FullRegions: sortCoords(fullList),
		LodRegions:  sortCoords(lodList),
		TileRegions: sortCoords(tileList),
	}

	log.Printf("Generated map metadata for %s (mode=%s): full=%d, lod=%d, tile=%d",
		mapDir, mode, len(fullList), len(lodList), len(tileList))

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
