package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/karrick/godirwalk"
	"github.com/maruel/natural"
	"github.com/rmmh/cubeographer/go/region"
)

// minecraftMap represents a discovered playable Minecraft dimension/map
type minecraftMap struct {
	Name              string `json:"name"`              // Terse, unique name used in URLs and display, e.g. "hermitcraft10_nether"
	RegionDir         string `json:"regionDir"`         // Absolute or relative path to the region directory containing .mca files
	WorldDir          string `json:"worldDir"`          // Absolute or relative path to the world directory containing level.dat
	Dimension         string `json:"dimension"`         // "overworld", "nether", "end", or custom name
	FullRegionsCount  int    `json:"fullRegionsCount"`  // Number of full regions
	LodRegionsCount   int    `json:"lodRegionsCount"`   // Number of LOD regions
	TileRegionsCount  int    `json:"tileRegionsCount"`  // Number of tile regions
	TotalRegionsCount int    `json:"totalRegionsCount"` // Total number of regions
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

var nonUrlSafeRe = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

// cleanWorldName converts spaces to underscores and strips non-url-safe characters.
func cleanWorldName(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = nonUrlSafeRe.ReplaceAllString(s, "")
	if s == "" {
		return "world"
	}
	return s
}

// findMaps scans the input folders, discovers worlds and dimensions,
// and returns them with unique terse names assigned.
func findMaps(inputDirs []string) ([]minecraftMap, error) {
	if len(inputDirs) == 1 && inputDirs[0] == "test" {
		return []minecraftMap{
			{
				Name:      "",
				RegionDir: "test",
				WorldDir:  "test",
				Dimension: "overworld",
			},
		}, nil
	}

	if len(inputDirs) == 1 {
		inputDir := inputDirs[0]
		// 1. Check if inputDir itself contains .mca files directly.
		entries, err := region.ReadDir(inputDir)
		if err == nil {
			hasMcaDirectly := false
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mca") {
					hasMcaDirectly = true
					break
				}
			}
			if hasMcaDirectly {
				// Case 1: a region directory that contains .mca files.
				// Serve it at the root.
				return []minecraftMap{
					{
						Name:      "",
						RegionDir: inputDir,
						WorldDir:  inputDir,
						Dimension: "overworld",
					},
				}, nil
			}
		}
	}

	// 2. Perform a full walk to gather all level.dat files and region directories (.mca files).
	var levelDatDirs []string
	var mcaDirs []string

	for _, inputDir := range inputDirs {
		visitedDirs := make(map[string]bool)
		err := godirwalk.Walk(inputDir, &godirwalk.Options{
			Callback: func(path string, d *godirwalk.Dirent) error {
				if d.IsDir() {
					realPath, err := filepath.EvalSymlinks(path)
					if err != nil {
						return err
					}
					if visitedDirs[realPath] {
						return filepath.SkipDir
					}
					visitedDirs[realPath] = true
					return nil
				}
				if strings.HasSuffix(strings.ToLower(d.Name()), ".zip") {
					// Walk zip file contents (only one level!)
					zipReader, err := region.GetZipReader(path)
					if err != nil {
						log.Printf("warning: failed to open zip file %s: %v", path, err)
						return nil
					}
					for _, f := range zipReader.File {
						internalPath := filepath.ToSlash(f.Name)
						baseName := filepath.Base(internalPath)
						fullPath := path + "/" + internalPath

						if baseName == "level.dat" {
							levelDatDirs = append(levelDatDirs, filepath.Dir(fullPath))
						} else if strings.HasSuffix(baseName, ".mca") {
							parentDir := filepath.Dir(fullPath)
							parentBase := strings.ToLower(filepath.Base(parentDir))
							if parentBase == "poi" || parentBase == "entities" {
								continue
							}
							if !contains(mcaDirs, parentDir) {
								mcaDirs = append(mcaDirs, parentDir)
							}
						}
					}
					return nil
				}
				if d.Name() == "level.dat" {
					levelDatDirs = append(levelDatDirs, filepath.Dir(path))
				} else if strings.HasSuffix(d.Name(), ".mca") {
					parentDir := filepath.Dir(path)
					base := strings.ToLower(filepath.Base(parentDir))
					if base == "poi" || base == "entities" {
						return nil
					}
					if !contains(mcaDirs, parentDir) {
						mcaDirs = append(mcaDirs, parentDir)
					}
				}
				return nil
			},
			FollowSymbolicLinks: true,
		})
		if err != nil {
			return nil, err
		}
	}

	// 3. For each region directory, find its world directory.
	sort.Strings(mcaDirs)

	type mapCandidate struct {
		regionDir string
		worldDir  string
		relPath   string // relative path from worldDir to regionDir
	}

	var candidates []mapCandidate
	worldDirsSet := make(map[string]bool)

	for _, rd := range mcaDirs {
		// Find closest ancestor containing level.dat
		var bestWorldDir string
		for _, ld := range levelDatDirs {
			// Check if ld is an ancestor of rd (or rd itself)
			if rd == ld || strings.HasPrefix(rd, ld+string(filepath.Separator)) {
				if bestWorldDir == "" || len(ld) > len(bestWorldDir) {
					bestWorldDir = ld
				}
			}
		}

		if bestWorldDir == "" {
			// Fallback if no level.dat is found in any ancestor
			if filepath.Base(rd) == "region" {
				bestWorldDir = filepath.Dir(rd)
			} else {
				bestWorldDir = rd
			}
		}

		rel, err := filepath.Rel(bestWorldDir, rd)
		if err != nil {
			rel = "."
		}

		candidates = append(candidates, mapCandidate{
			regionDir: rd,
			worldDir:  bestWorldDir,
			relPath:   rel,
		})
		worldDirsSet[bestWorldDir] = true
	}

	// 4. Assign unique names to each unique worldDir.
	var uniqueWorldDirs []string
	for wd := range worldDirsSet {
		uniqueWorldDirs = append(uniqueWorldDirs, wd)
	}
	sort.Slice(uniqueWorldDirs, func(i, j int) bool {
		return natural.Less(uniqueWorldDirs[i], uniqueWorldDirs[j])
	})

	worldNames := make(map[string]string)
	assignedNames := make(map[string]bool)

	for _, wd := range uniqueWorldDirs {
		// Find the inputDir that is an ancestor of wd
		var parentInputDir string
		for _, id := range inputDirs {
			if wd == id || strings.HasPrefix(wd, id+string(filepath.Separator)) {
				parentInputDir = id
				break
			}
		}
		if parentInputDir == "" {
			parentInputDir = inputDirs[0]
		}

		rel, err := filepath.Rel(parentInputDir, wd)
		var baseName string
		if strings.Contains(wd, ".zip") {
			zipPath, internalPath, isZip := region.SplitZipPath(wd)
			if isZip {
				zipBase := strings.TrimSuffix(filepath.Base(zipPath), ".zip")
				if internalPath == "" {
					baseName = zipBase
				} else {
					baseName = zipBase + "_" + filepath.Base(internalPath)
				}
			}
		}
		if baseName == "" {
			if err != nil || rel == "." || rel == "" {
				baseName = filepath.Base(wd)
			} else {
				baseName = filepath.Base(rel)
			}
		}

		baseName = cleanWorldName(baseName)
		name := baseName
		if assignedNames[name] {
			suffix := 2
			for {
				candidate := fmt.Sprintf("%s_%d", baseName, suffix)
				if !assignedNames[candidate] {
					name = candidate
					break
				}
				suffix++
			}
		}
		assignedNames[name] = true
		worldNames[wd] = name
	}

	// 5. Build final MinecraftMaps list.
	var maps []minecraftMap
	for _, cand := range candidates {
		worldName := worldNames[cand.worldDir]

		var dim string
		var suffix string

		relLower := strings.ToLower(cand.relPath)
		if strings.Contains(relLower, "dim-1") || strings.Contains(relLower, "the_nether") {
			dim = "nether"
			suffix = "_nether"
		} else if strings.Contains(relLower, "dim1") || strings.Contains(relLower, "the_end") {
			dim = "end"
			suffix = "_end"
		} else if strings.Contains(relLower, "dimensions") {
			parts := strings.Split(cand.relPath, string(filepath.Separator))
			dimName := "custom"
			for i, part := range parts {
				if strings.ToLower(part) == "dimensions" && i+2 < len(parts) {
					dimName = parts[i+2]
					break
				}
			}
			dimName = cleanWorldName(dimName)
			if dimName == "overworld" {
				dim = "overworld"
				suffix = ""
			} else {
				dim = dimName
				suffix = "_" + dimName
			}
		} else {
			dim = "overworld"
			suffix = ""
		}

		maps = append(maps, minecraftMap{
			Name:      worldName + suffix,
			RegionDir: cand.regionDir,
			WorldDir:  cand.worldDir,
			Dimension: dim,
		})
	}

	// Sort maps by Name to make the deduplication order deterministic (natural)
	sort.Slice(maps, func(i, j int) bool {
		return natural.Less(maps[i].Name, maps[j].Name)
	})

	// Ensure all final map names are globally unique
	assignedMapNames := make(map[string]bool)
	for i := range maps {
		originalName := maps[i].Name
		if originalName == "" {
			continue
		}
		originalName = cleanWorldName(originalName)
		name := originalName
		if assignedMapNames[name] {
			suffix := 2
			for {
				candidate := fmt.Sprintf("%s_%d", originalName, suffix)
				if !assignedMapNames[candidate] {
					name = candidate
					break
				}
				suffix++
			}
		}
		assignedMapNames[name] = true
		maps[i].Name = name
	}

	// Final sort after names are guaranteed unique
	sort.Slice(maps, func(i, j int) bool {
		return natural.Less(maps[i].Name, maps[j].Name)
	})

	if len(maps) == 1 {
		maps[0].Name = ""
	}

	return maps, nil
}

func writeWorldsJSON(outDir string, maps []minecraftMap) error {
	for i := range maps {
		var metadataPath string
		if maps[i].Name == "" {
			metadataPath = filepath.Join(outDir, "map", "metadata.json")
		} else {
			metadataPath = filepath.Join(outDir, maps[i].Name, "map", "metadata.json")
		}
		if meta, err := ReadMapMetadata(metadataPath); err == nil {
			maps[i].FullRegionsCount = len(meta.FullRegions)
			maps[i].LodRegionsCount = len(meta.LodRegions)
			maps[i].TileRegionsCount = len(meta.TileRegions)
			maps[i].TotalRegionsCount = len(meta.FullRegions) + len(meta.LodRegions) + len(meta.TileRegions)
		}
	}

	data, err := json.MarshalIndent(maps, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := filepath.Join(outDir, "worlds.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(outDir, "worlds.json"))
}
