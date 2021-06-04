package main

import (
	"fmt"
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
)

var (
	cmtRe = regexp.MustCompile(`^/map/r\.(\d+)\.(\d+)\.\d+\.cmt$`)
)

type workItem struct {
	rx   int
	rz   int
	done chan<- struct{}
}

type server struct {
	regionDir  string
	dataDir    string
	pruneCaves bool

	binaryTime time.Time
	bm         *blockMapper

	workQueue chan *workItem

	working  map[int64][]*workItem
	workLock sync.Mutex
}

func (s *server) indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "no-cache")
	http.ServeFile(w, r, path.Join(s.dataDir, "index.html"))
}

func (s *server) indexJsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "no-cache")
	http.ServeFile(w, r, path.Join(s.dataDir, "index.js"))
}

func (s *server) textureHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "no-cache")
	http.ServeFile(w, r, path.Join(s.dataDir, r.URL.Path))
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
			dir:        s.regionDir,
			outdir:     path.Join(s.dataDir, "map"),
			file:       fmt.Sprintf("r.%d.%d.mca", item.rx, item.rz),
			bm:         s.bm,
			pruneCaves: s.pruneCaves,
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
	m := regionMatchRE.FindStringSubmatch(filename)
	if len(m) == 0 {
		return
	}
	rx, _ := strconv.Atoi(m[1])
	rz, _ := strconv.Atoi(m[2])
	done := make(chan struct{})
	work := &workItem{
		rx:   rx,
		rz:   rz,
		done: done,
	}
	s.workQueue <- work
	<-done
}

func (s *server) mapHandler(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Add("Content-Encoding", "gzip")
	}
	w.Header().Add("Cache-Control", "no-cache")
	log.Println(r.URL.Path, s.isStale(r.URL.Path))
	if s.isStale(r.URL.Path) {
		s.awaitUpdate(r.URL.Path)
	}
	http.ServeFile(w, r, path.Join(s.dataDir, r.URL.Path))
}

func serve(numProcs int, regionDir string, dataDir string, pruneCaves bool) {
	binaryStat, err := os.Stat(os.Args[0])
	if err != nil {
		log.Fatal(err)
	}

	bm, err := makeBlockMapper(dataDir)

	r := mux.NewRouter()
	s := &server{
		regionDir:  regionDir,
		dataDir:    dataDir,
		pruneCaves: pruneCaves,
		bm:         bm,
		binaryTime: binaryStat.ModTime(),
		workQueue:  make(chan *workItem),
		working:    make(map[int64][]*workItem),
	}

	for i := 0; i < numProcs; i++ {
		go s.mapWorker()
	}

	r.HandleFunc("/", s.indexHandler)
	r.HandleFunc("/index.js", s.indexJsHandler)
	r.HandleFunc("/textures/{texture}", s.textureHandler)
	r.HandleFunc("/map/{path}", s.mapHandler)

	srv := &http.Server{
		Handler:      r,
		Addr:         "127.0.0.1:9999",
		WriteTimeout: 120 * time.Second,
		ReadTimeout:  10 * time.Second,
	}

	log.Println("listening on", srv.Addr)

	log.Fatal(srv.ListenAndServe())
}
