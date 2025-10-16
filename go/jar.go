package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path"
	"strings"

	"github.com/rmmh/cubeographer/go/render"
	rp "github.com/rmmh/cubeographer/go/resourcepack"
)

var clientJarPath = flag.String("jar", "", "use specific client jar")
var clientJarVersion = flag.String("version", "latest", "specify client version to download")

var genDebug = flag.String("gendebug", "", "debug specific block name (or \"all\")")

func generate(outDir string) {
	fmt.Println("generating textures")
	jarPath := path.Join(outDir, "client.jar")
	if *clientJarPath != "" {
		if _, err := os.Stat(*clientJarPath); err == nil {
			jarPath = *clientJarPath
		}
	}
	if _, err := os.Stat(jarPath); err != nil {
		fmt.Println("downloading minecraft client jar")
		err := rp.DownloadMinecraftJar(jarPath, *clientJarVersion)
		if err != nil {
			fmt.Println("unable to downlaod minecraft jar:", err)
		}
	}
	jar, err := zip.OpenReader(jarPath)
	if err != nil {
		fmt.Println("unable to open jar", jarPath)
	}

	pack, err := rp.JarFromZip(jar)
	if err != nil {
		log.Fatal(err)
	}

	meta, atlases := render.Prepare(pack, *genDebug)

	os.MkdirAll(path.Join(outDir, "textures"), 0755)

	for layer, atlas := range atlases {
		f, err := os.Create(path.Join(outDir, "textures", fmt.Sprintf("atlas%d.png", layer)))
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		err = png.Encode(f, atlas)
		if err != nil {
			log.Fatal(err)
		}
	}

	layerCounts := map[int]int{}

	// Wipe unneeded texture references, and count layers for each block
	for _, b := range meta.Blocks {
		for i := range b.Templates {
			b.Templates[i].Textures = nil
			layerCounts[int(b.Templates[i].Layer)] += 1
		}
	}

	// Write out blockstate template metadata
	buf, err := json.Marshal(meta)
	if err != nil {
		log.Fatal(err)
	}
	buf = []byte(strings.Replace(strings.Replace(string(buf), "},", "},\n", -1), "\n{\"layer", "\n    {\"layer", -1))
	fmt.Println("blockmeta.json size:", len(string(buf)))
	for i := 0; i < int(render.NumRenderLayers); i++ {
		fmt.Println("layer", render.LayerNames[i], layerCounts[i])
	}

	f, err := os.Create(path.Join(outDir, "blockmeta.json"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	_, err = f.Write(buf)
	if err != nil {
		log.Fatal(err)
	}

	loadBlockMapper(buf)
}
