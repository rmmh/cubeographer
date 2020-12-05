package main

import (
	"io/ioutil"
	"runtime"
	"sync"
	"os"
	"sort"
	"log"
	"strings"
	"flag"
	"fmt"
)

func convert(regionDir, outDir string, filters []string) {
	files, err := ioutil.ReadDir(regionDir)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	work := make(chan os.FileInfo)
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for file := range work {
				err = scanRegion(regionDir, outDir, file)
				if err != nil {
					log.Fatal(err)
				}
				wg.Done()
			}
		}()
		break
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
}

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) > 2 {
		convert(args[0], args[1], args[2:])
	} else if len(args) == 2 {
		convert(args[0], args[1], nil)
	} else {
		usage()
	}
}
