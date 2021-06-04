all: dist/textures/atlas0.png dist/map/r.0.0.cmt

dist/textures/atlas0.png: $(wildcard go/*.go)
	go run ./go/ -gen dist/

dist/map/r.0.0.cmt: $(wildcard go/*.go)
	go run ./go/ maps/Novigrad/region/ dist/map r.0. r.1 r.2 r.3

watch:
	~/src/go/bin/reflex -sr '\.go$' -- go run ./go/ maps/Novigrad/region/ dist/
