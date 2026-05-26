package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/rmmh/cubeographer/go/region"
)

var (
	cmtRe = regexp.MustCompile(`([^/]*)/map/(?:tiles/|lods/)?r\.(-?\d+)\.(-?\d+)(?:\.\d+\.cmt|\.png|\.bin)$`)
)

type workItem struct {
	world string
	rx    int
	rz    int
	done  chan<- struct{}
}

type server struct {
	regionDir  map[string]string
	readRegion map[string]region.ReadRegionFunc
	dataDir    string
	pruneCaves bool
	mode       string
	maps       []minecraftMap

	binaryTime time.Time
	bm         *region.BlockMapper

	workQueue chan *workItem

	working  map[int64][]*workItem
	workLock sync.Mutex
}

func (s *server) serveFile(w http.ResponseWriter, r *http.Request, filename string) {
	// 1. Try to serve from the local disk directory
	diskPath := path.Join(s.dataDir, filename)
	if _, err := os.Stat(diskPath); err == nil {
		http.ServeFile(w, r, diskPath)
		return
	}

	// 2. Fall back to serving from the embedded filesystem
	subFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	fileServer := http.FileServer(http.FS(subFS))
	// Adjust the URL path to match the root of subFS
	r.URL.Path = "/" + filename
	fileServer.ServeHTTP(w, r)
}

func (s *server) indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "no-cache")
	vars := mux.Vars(r)
	world := vars["world"]

	if world != "" {
		s.serveFile(w, r, "map.html")
	} else {
		if _, hasRoot := s.regionDir[""]; hasRoot {
			s.serveFile(w, r, "map.html")
		} else {
			s.serveFile(w, r, "index.html")
		}
	}
}

func (s *server) staticHandler(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimPrefix(r.URL.Path, "/")
	s.serveFile(w, r, filename)
}

func (s *server) isStale(r string) bool {
	st, err := os.Stat(path.Join(s.dataDir, r))
	if err != nil {
		return true
	}
	if st.ModTime().Before(s.binaryTime) {
		return true
	}
	return false
}

func (s *server) mapWorker() {
	for item := range s.workQueue {
		itemKey := int64(item.rx) + int64(item.rz)<<32
		s.workLock.Lock()
		_, exists := s.working[itemKey]
		s.working[itemKey] = append(s.working[itemKey], item)
		s.workLock.Unlock()
		if exists {
			// another worker is already processing this region, and will
			// dispatch the event when ready
			continue
		}
		scanRegion(&scanRegionConfig{
			dir:        s.regionDir[item.world],
			readRegion: s.readRegion[item.world],
			outdir:     path.Join(s.dataDir, item.world, "map"),
			file:       fmt.Sprintf("r.%d.%d.mca", item.rx, item.rz),
			bm:         s.bm,
			prune:      s.pruneCaves,
			mode:       s.mode,
		})
		s.workLock.Lock()
		for _, wait := range s.working[itemKey] {
			close(wait.done)
		}
		delete(s.working, itemKey)
		s.workLock.Unlock()
	}
}

func (s *server) awaitUpdate(filename string) {
	m := cmtRe.FindStringSubmatch(filename)
	if len(m) == 0 {
		return
	}
	world := m[1]
	if s.regionDir[world] == "" && (s.readRegion == nil || s.readRegion[world] == nil) {
		return
	}
	rx, _ := strconv.Atoi(m[2])
	rz, _ := strconv.Atoi(m[3])
	done := make(chan struct{})
	work := &workItem{
		world: world,
		rx:    rx,
		rz:    rz,
		done:  done,
	}
	s.workQueue <- work
	<-done
}

func (s *server) mapHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	world := vars["world"]
	filePath := vars["path"]

	if filePath == "metadata.json" {
		mDir := path.Join(s.dataDir, world, "map")
		metadataPath := path.Join(mDir, "metadata.json")
		regionDirPath := s.regionDir[world]

		stale := true
		if metaStat, err := os.Stat(metadataPath); err == nil {
			if regionStat, err := os.Stat(regionDirPath); err == nil {
				if !regionStat.ModTime().After(metaStat.ModTime()) && s.binaryTime.Before(metaStat.ModTime()) {
					stale = false
				}
			}
		}

		if stale {
			log.Printf("generating map metadata on demand for world %q...", world)
			if err := WriteMapMetadata(mDir, regionDirPath, s.mode); err != nil {
				log.Printf("error writing map metadata for world %q: %v", world, err)
			}
			// Update worlds.json with new region counts
			if err := writeWorldsJSON(s.dataDir, s.maps); err != nil {
				log.Printf("error writing worlds.json on demand: %v", err)
			}
		}
	}

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && strings.HasSuffix(r.URL.Path, ".cmt") {
		w.Header().Add("Content-Encoding", "gzip")
	}
	if strings.Contains(r.URL.Path, "..") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Add("Cache-Control", "no-cache")
	log.Printf("%s stale=%v", r.URL.Path, s.isStale(r.URL.Path))
	if s.isStale(r.URL.Path) {
		s.awaitUpdate(r.URL.Path)
	}
	http.ServeFile(w, r, path.Join(s.dataDir, r.URL.Path))
}

func (s *server) worldRedirHandler(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL.Path[strings.IndexByte(r.URL.Path[1:], '/')+2:])
	http.Redirect(w, r, r.URL.Path[strings.IndexByte(r.URL.Path[1:], '/')+1:], http.StatusFound)
}

func serve(numProcs int, regionDir string, dataDir string, pruneCaves bool, mode string) {
	binaryStat, err := os.Stat(os.Args[0])
	if err != nil {
		log.Fatal(err)
	}

	_, err = makeBlockMapper(dataDir)
	if err != nil || *genDebug == "force" {
		log.Println("regenerating block mapping")
		generate(dataDir)
	}
	bm, err := makeBlockMapper(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	maps, err := findMaps(regionDir)
	if err != nil {
		log.Fatalf("error finding maps: %v", err)
	}

	log.Printf("Discovered %d maps in %s:", len(maps), regionDir)
	regDirs := make(map[string]string)
	for _, m := range maps {
		log.Printf("  - %q -> %s (dim=%s)", m.Name, m.RegionDir, m.Dimension)
		regDirs[m.Name] = m.RegionDir
	}

	r := mux.NewRouter()
	s := &server{
		regionDir: regDirs,
		readRegion: map[string]region.ReadRegionFunc{
			"test": region.FakeReadRegion,
		},
		dataDir:    dataDir,
		pruneCaves: pruneCaves,
		mode:       mode,
		maps:       maps,
		bm:         bm,
		binaryTime: binaryStat.ModTime(),
		workQueue:  make(chan *workItem),
		working:    make(map[int64][]*workItem),
	}

	for w := range s.regionDir {
		log.Printf("generating map metadata for world %q...", w)
		if err := WriteMapMetadata(path.Join(dataDir, w, "map"), s.regionDir[w], s.mode); err != nil {
			log.Printf("error writing map metadata for world %q: %v", w, err)
		}
	}

	log.Printf("writing worlds.json...")
	if err := writeWorldsJSON(dataDir, s.maps); err != nil {
		log.Println("error writing worlds.json:", err)
	}

	for i := 0; i < numProcs; i++ {
		go s.mapWorker()
	}

	r.HandleFunc("/", s.indexHandler)
	r.HandleFunc("/worlds.json", s.staticHandler)
	r.HandleFunc("/index.css", s.staticHandler)
	r.HandleFunc("/index.js", s.staticHandler)
	r.HandleFunc("/textures/{texture}", s.staticHandler)
	r.HandleFunc("/map/{path:.*}", s.mapHandler)

	r.HandleFunc("/{world}/", s.indexHandler)
	r.HandleFunc("/{world}/map/{path:.*}", s.mapHandler)
	r.HandleFunc("/{world}/index.js", s.worldRedirHandler)
	r.HandleFunc("/{world}/index.css", s.worldRedirHandler)
	r.HandleFunc("/{world}/textures/{texture}", s.worldRedirHandler)

	srv := &http.Server{
		Handler:      r,
		Addr:         "127.0.0.1:9999",
		WriteTimeout: 120 * time.Second,
		ReadTimeout:  10 * time.Second,
	}

	log.Println("listening on", srv.Addr)

	log.Fatal(srv.ListenAndServe())
}
