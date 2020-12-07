dist/cubiomes.js: Makefile cubiomes/finders.c cubiomes/generator.c cubiomes/layers.c cubiomes/util.c
	emcc -O2 -s EXPORT_NAME="'Cubiomes'" -s 'EXPORTED_FUNCTIONS=["_initBiomes","_setupGenerator","_allocCache","_setWorldSeed","_genArea"]' -s MINIMAL_RUNTIME_STREAMING_WASM_COMPILATION=0 cubiomes/finders.c cubiomes/generator.c cubiomes/layers.c cubiomes/util.c -o dist/cubiomes.js

dist/textures/atlas0.png: $(wildcard go/*.go)
	go run ./go/ -gen dist/

dist/map/r.0.0.cmt: $(wildcard go/*.go)
	go run ./go/ maps/Novigrad/region/ dist/map r.0. r.1 r.2 r.3
