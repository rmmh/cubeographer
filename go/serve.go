package main

import (
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type server struct {
	regionDir string
	dataDir   string
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

func (s *server) mapHandler(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Add("Content-Encoding", "gzip")
	}
	w.Header().Add("Cache-Control", "no-cache")
	http.ServeFile(w, r, path.Join(s.dataDir, r.URL.Path))
}

func serve(numProcs int, regionDir string, dataDir string) {
	r := mux.NewRouter()
	s := &server{regionDir: regionDir, dataDir: dataDir}

	r.HandleFunc("/", s.indexHandler)
	r.HandleFunc("/index.js", s.indexJsHandler)
	r.HandleFunc("/textures/{texture}", s.textureHandler)
	r.HandleFunc("/map/{path}", s.mapHandler)

	srv := &http.Server{
		Handler:      r,
		Addr:         "127.0.0.1:9999",
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  60 * time.Second,
	}

	log.Println("listening on", srv.Addr)

	log.Fatal(srv.ListenAndServe())
}
