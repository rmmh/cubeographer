dist/cubiomes.js: Makefile cubiomes/finders.c cubiomes/generator.c cubiomes/layers.c cubiomes/util.c
	emcc -O2 -s EXPORT_NAME="'Cubiomes'" -s 'EXPORTED_FUNCTIONS=["_initBiomes","_setupGenerator","_allocCache","_setWorldSeed","_genArea"]' -s MINIMAL_RUNTIME_STREAMING_WASM_COMPILATION=0 cubiomes/finders.c cubiomes/generator.c cubiomes/layers.c cubiomes/util.c -o dist/cubiomes.js

dist/textures/atlas.png: extract_textures_from_overviewer.py
	montage $(shell python3 extract_textures_from_overviewer.py textures/minecraft) -background none -geometry 16x16+0+0 -crop 16x16+0+0 -tile 32x32 dist/textures/atlas.png

dist/map/r.0.0.cmt: go/parse_regions.go
	go run go/parse_regions.go maps/salc1/region/ dist/map
