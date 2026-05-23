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

func convert(numProcs int, regionDir, outDir string, filters []string, prune bool, mode string) {
	files, err := os.ReadDir(regionDir)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	dataDir := path.Join(outDir, "..")
	bm, err := makeBlockMapper(dataDir)
	if err != nil || *genDebug == "force" {
		log.Println("regenerating block mapping")
		generate(dataDir)
		bm, err = makeBlockMapper(dataDir)
		if err != nil {
			log.Fatal(err)
		}
	}

	work := make(chan fs.FileInfo)
	var wg sync.WaitGroup
	for i := 0; i < numProcs; i++ {
		go func() {
			for file := range work {
				err = scanRegion(&scanRegionConfig{
					dir:    regionDir,
					outdir: outDir,
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
	wg.Wait()
	log.Println("generating map metadata...")
	if err := WriteMapMetadata(outDir, mode); err != nil {
		log.Println("error writing map metadata:", err)
	}
}

func usage() {
	fmt.Println("usage: prog <regiondir> <outputdir> [filterstrings]")
	flag.Usage()
}

func main() {
	gen := flag.String("gen", "", "generate texture atlas & data files from jar")
	numProcs := flag.Int("threads", runtime.NumCPU(), "number of parallel threads to use")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to `file`")
	noPrune := flag.Bool("noprune", false, "don't attempt to hide invisible portions")
	doConvert := flag.Bool("convert", false, "convert region files for web display")
	mode := flag.String("mode", "full", "output mode: full (cmt+bin+png), lod (bin+png), or tile (png)")
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

	var filters []string
	if len(args) > 2 {
		filters = args[2:]
	}
	if *doConvert {
		if len(args) > 1 {
			convert(*numProcs, args[0], args[1], filters, !*noPrune, *mode)
			return
		} else {
			usage()
			return
		}
	}
	if len(args) > 1 {
		serve(*numProcs, args[0], args[1], !*noPrune, *mode)
	} else {
		usage()
	}
}
