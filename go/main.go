package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
)

func convert(numProcs int, regionDir, outDir string, filters []string) {
	files, err := ioutil.ReadDir(regionDir)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	blockmeta, err := ioutil.ReadFile(path.Join(outDir, "..", "blockmeta.json"))
	if err != nil {
		log.Fatal(err)
	}
	bm, err := loadBlockMapper(blockmeta)
	if err != nil {
		log.Fatal(err)
	}

	work := make(chan os.FileInfo)
	var wg sync.WaitGroup
	for i := 0; i < numProcs; i++ {
		go func() {
			for file := range work {
				err = scanRegion(regionDir, outDir, file, bm)
				if err != nil {
					log.Fatal(err)
				}
				wg.Done()
			}
		}()
	}

	for _, file := range files {
		if len(filters) > 0 {
			good := false
			for _, filter := range filters {
				if strings.Contains(file.Name(), filter) {
					good = true
					break
				}
			}
			if !good {
				continue
			}
		}
		wg.Add(1)
		work <- file
	}
	wg.Wait()
}

func usage() {
	fmt.Println("usage: prog <regiondir> <outputdir> [filterstrings]")
	flag.Usage()
}

func main() {
	gen := flag.String("gen", "", "generate texture atlas & data files from jar")
	numProcs := flag.Int("threads", runtime.NumCPU(), "number of parallel threads to use")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to `file`")
	flag.Parse()

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

	if len(args) > 2 {
		convert(*numProcs, args[0], args[1], args[2:])
	} else if len(args) == 2 {
		convert(*numProcs, args[0], args[1], nil)
	} else {
		usage()
	}
}
