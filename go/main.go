package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"

	"github.com/rmmh/cubeographer/go/region"
	"github.com/samber/lo"
)

var debugFlag = flag.String("debug", "", "comma-separated debug options")

func makeBlockMapper(outDir string) (*region.BlockMapper, error) {
	blockmeta, err := os.ReadFile(path.Join(outDir, "blockmeta.json"))
	if err != nil {
		return nil, err
	}
	return region.LoadBlockMapper(blockmeta)
}

func writeAssetIfMissing(outDir, filename string) {
	targetPath := path.Join(outDir, filename)
	if _, err := os.Stat(targetPath); err == nil {
		// File already exists, don't overwrite
		return
	}

	data, err := distFS.ReadFile(path.Join("dist", filename))
	if err != nil {
		log.Printf("warning: failed to read embedded asset %s: %v", filename, err)
		return
	}

	if err := os.MkdirAll(path.Dir(targetPath), 0755); err != nil {
		log.Printf("error creating directory for %s: %v", filename, err)
		return
	}

	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		log.Printf("error writing asset %s: %v", filename, err)
	} else {
		log.Printf("extracted embedded asset %s -> %s", filename, targetPath)
	}
}

func convert(numProcs int, inputDirs []string, outDir string, filters []string, prune bool, mode string) {
	maps, err := findMaps(inputDirs)
	if err != nil {
		log.Fatal(err)
	}
	if len(maps) == 0 {
		log.Fatalf("No maps found in %v", inputDirs)
	}

	// Extract standard assets if missing
	writeAssetIfMissing(outDir, "index.js")
	writeAssetIfMissing(outDir, "index.css")
	writeAssetIfMissing(outDir, "index.html")

	dataDir := outDir
	if _, err := os.Stat(path.Join(outDir, "blockmeta.json")); err != nil {
		parentDir := path.Join(outDir, "..")
		if _, errParent := os.Stat(path.Join(parentDir, "blockmeta.json")); errParent == nil {
			dataDir = parentDir
		}
	}

	bm, err := makeBlockMapper(dataDir)
	if err != nil || *genDebug == "force" {
		log.Println("regenerating block mapping")
		generate(dataDir)
		bm, err = makeBlockMapper(dataDir)
		if err != nil {
			log.Fatal(err)
		}
	}

	for _, m := range maps {
		var targetOutDir string
		if m.Name == "" {
			targetOutDir = outDir
		} else {
			targetOutDir = path.Join(outDir, m.Name, "map")
		}

		log.Printf("Converting map %q (%s) -> %s", m.Name, m.RegionDir, targetOutDir)

		files, err := region.ReadDir(m.RegionDir)
		if err != nil {
			log.Printf("error reading region dir %s: %v", m.RegionDir, err)
			continue
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

		work := make(chan fs.FileInfo)
		var wg sync.WaitGroup
		for i := 0; i < numProcs; i++ {
			go func() {
				for file := range work {
					err = scanRegion(&scanRegionConfig{
						dir:    m.RegionDir,
						outdir: targetOutDir,
						file:   file.Name(),
						bm:     bm,
						prune:  prune,
						debug:  *debugFlag,
						mode:   mode,
					})
					if err != nil {
						log.Fatal("error converting ", file.Name(), ": ", err)
					}
					wg.Done()
				}
			}()
		}

		filtersRe := lo.Map(filters, func(f string, idx int) *regexp.Regexp {
			return regexp.MustCompile(f)
		})

		for _, file := range files {
			if len(filters) > 0 {
				good := 0
				for _, filter := range filtersRe {
					if filter.MatchString(file.Name()) {
						good++
					}
				}
				if good != len(filters) {
					continue
				}
			}
			info, err := file.Info()
			if err != nil {
				log.Println("error getting file info: ", file.Name(), err)
			}
			if info.Size() == 0 || info.IsDir() {
				continue
			}
			wg.Add(1)
			work <- info
		}
		close(work)
		wg.Wait()

		log.Printf("generating map metadata for %q...", m.Name)
		if err := WriteMapMetadata(targetOutDir, "", mode); err != nil {
			log.Println("error writing map metadata:", err)
		}

		// Copy map.html into each map's folder as index.html
		mapHtmlPath := path.Join(dataDir, "map.html")
		var targetIndexHtml string
		if m.Name == "" {
			targetIndexHtml = path.Join(outDir, "index.html")
		} else {
			targetIndexHtml = path.Join(outDir, m.Name, "index.html")
		}

		var mapHtmlData []byte
		if data, err := os.ReadFile(mapHtmlPath); err == nil {
			mapHtmlData = data
		} else {
			// Fallback to embedded map.html
			if data, err := distFS.ReadFile("dist/map.html"); err == nil {
				mapHtmlData = data
			} else {
				log.Printf("warning: map.html not found on disk at %s or embedded: %v", mapHtmlPath, err)
			}
		}

		if len(mapHtmlData) > 0 {
			os.MkdirAll(path.Dir(targetIndexHtml), 0755)
			content := string(mapHtmlData)
			if m.Name != "" {
				content = strings.Replace(content, `href="index.css"`, `href="../index.css"`, 1)
				content = strings.Replace(content, `src="index.js"`, `src="../index.js"`, 1)
				content = strings.Replace(content, `<head>`, "<head>\n    <script>window.ASSET_PREFIX = \"../\";</script>", 1)
			}
			if err := os.WriteFile(targetIndexHtml, []byte(content), 0644); err != nil {
				log.Println("error writing index.html for map:", err)
			}
		}
	}

	log.Printf("writing worlds.json...")
	if err := writeWorldsJSON(outDir, maps); err != nil {
		log.Println("error writing worlds.json:", err)
	}
}

type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func usage() {
	fmt.Printf("usage: %s [-in <path1> [-in <path2>...]] [-out <path>] [filterstrings]\n", os.Args[0])
	flag.Usage()
}

func main() {
	gen := flag.String("gen", "", "generate texture atlas & data files from jar")
	numProcs := flag.Int("threads", runtime.NumCPU(), "number of parallel threads to use")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to `file`")
	noPrune := flag.Bool("noprune", false, "don't attempt to hide invisible portions")
	doConvert := flag.Bool("convert", false, "convert region files for web display")
	mode := flag.String("mode", "full", "output mode: full (cmt+bin+png), lod (bin+png), or tile (png)")

	var inputFlags stringSlice
	flag.Var(&inputFlags, "in", "input paths (can be specified multiple times)")
	outFlag := flag.String("out", "out", "output directory")

	flag.Parse()

	if *mode != "full" && *mode != "lod" && *mode != "tile" {
		log.Fatalf("invalid mode %q: must be one of full, lod, or tile", *mode)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	args := flag.Args()

	if *gen != "" {
		generate(*gen)
		return
	}

	inputs := []string(inputFlags)
	if len(inputs) == 0 {
		inputs = GetDefaultInputPaths()
	}

	if len(inputs) == 0 || *outFlag == "" {
		usage()
		os.Exit(1)
	}

	output := *outFlag
	filters := args

	if *doConvert {
		convert(*numProcs, inputs, output, filters, !*noPrune, *mode)
	} else {
		serve(*numProcs, inputs, output, !*noPrune, *mode)
	}
}
