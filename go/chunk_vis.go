package main

import (
	"math/bits"

	"github.com/rmmh/cubeographer/go/region"
)

type chunkVis [32 * 32 * (25)]struct {
	dirReachable int   // 0: +y, 1: -y, 2: +x, 3: -x, 4: +z, 5: -z
	dirVisited   int   // which dirReachable states has this been visited with?
	connected    int64 // 0-5 +y can reach (+y,-y,+x,...), 6-11 -y can reach (+y, -y ...)
}

type tinybitset struct {
	nonzeros uint64
	vals     [4096 / 64]uint64
}

func (t *tinybitset) set(x int) {
	t.vals[x>>6] |= 1 << (x & 63)
	t.nonzeros |= 1 << ((x >> 6) & 63)
}

func (t *tinybitset) clear(x int) {
	t.vals[x>>6] &^= 1 << (x & 63)
	if t.vals[x>>6] == 0 {
		t.nonzeros &^= 1 << ((x >> 6) & 63)
	}
}

func (t *tinybitset) has(x int) bool {
	return t.vals[x>>6]&(1<<(x&63)) != 0
}

func (t *tinybitset) pop() int {
	if t.nonzeros != 0 {
		nzvo := (bits.Len64(t.nonzeros) - 1)
		o := nzvo
		v := t.vals[o]
		off := bits.Len64(v) - 1
		t.vals[o] ^= 1 << off
		if t.vals[o] == 0 {
			t.nonzeros &^= (1 << nzvo)
		}
		return o*64 + off
	}
	return -1
}

func computeConnected(chunklet []uint16, bm *region.BlockMapper) int64 {
	var passable tinybitset
	for i, b := range chunklet {
		if !bm.IsSolid(b) {
			passable.set(i)
		}
	}
	var conn int64
	for cur := passable.pop(); cur != -1; cur = passable.pop() {
		// layout: x+z*16+y*256
		faces := 0
		var todo tinybitset
		todo.set(cur)
		for cur = todo.pop(); cur != -1; cur = todo.pop() {
			passable.clear(cur)
			// fmt.Println(cur, faces, todo.has(cur), passable.has(cur))
			if cur < 256 { // -y, i.e. an exit to the negative y face (down)
				faces |= 1 << 1
				if passable.has(cur + 256) {
					todo.set(cur + 256)
				}
			} else if cur >= 15*256 { // +y
				faces |= 1 << 0
				if passable.has(cur - 256) {
					todo.set(cur - 256)
				}
			} else {
				if passable.has(cur - 256) {
					todo.set(cur - 256)
				}
				if passable.has(cur + 256) {
					todo.set(cur + 256)
				}
			}
			if (cur & 0xFF) < 16 { // -z
				faces |= 1 << 5
				if passable.has(cur + 16) {
					todo.set(cur + 16)
				}
			} else if (cur & 0xFF) >= 15*16 { // +z
				faces |= 1 << 4
				if passable.has(cur - 16) {
					todo.set(cur - 16)
				}
			} else {
				if passable.has(cur - 16) {
					todo.set(cur - 16)
				}
				if passable.has(cur + 16) {
					todo.set(cur + 16)
				}
			}
			if (cur & 0xF) == 0 { // -x
				faces |= 1 << 3
				if passable.has(cur + 1) {
					todo.set(cur + 1)
				}
			} else if (cur & 0xF) >= 15 { // +x
				faces |= 1 << 2
				if passable.has(cur - 1) {
					todo.set(cur - 1)
				}
			} else {
				if passable.has(cur - 1) {
					todo.set(cur - 1)
				}
				if passable.has(cur + 1) {
					todo.set(cur + 1)
				}
			}
		}
		for i := 0; i < 6; i++ {
			if faces&(1<<i) != 0 {
				conn |= int64(faces) << (6 * i)
			}
		}
	}
	return conn
}

func makeChunkvis(chunks [1024]region.ChunkDatum, bm *region.BlockMapper) *chunkVis {
	var cv chunkVis

	for cx := 0; cx < 32; cx++ {
		for cz := 0; cz < 32; cz++ {
			for ys, chunklet := range chunks[cx+cz*32].Blocks {
				cv[cx+cz*32+ys*1024].connected = computeConnected(chunklet, bm)
			}
		}
	}

	// 0: +y, 1: -y, 2: +x, 3: -x, 4: +z, 5: -z
	for cx := 0; cx < 32; cx++ {
		for cz := 0; cz < 32; cz++ {
			chunk := chunks[cx+cz*32]
			if len(chunk.Blocks) == 0 {
				continue
			}
			mask := 0b111101 // top chunklet reachable every dir but below
			cv[cx+cz*32+(len(chunk.Blocks)-1)*1024].dirReachable |= mask
			if cx == 0 {
				mask &^= 1 << 2
			} else if cx == 31 {
				mask &^= 1 << 3
			}
			if cz == 0 {
				mask &^= 1 << 4
			} else if cz == 31 {
				mask &^= 1 << 5
			}
			if mask == 0b111101 { // i.e., not on edge
				continue
			}
			for ys := 0; ys < len(chunk.Blocks); ys++ {
				cv[cx+cz*32+ys*1024].dirReachable |= mask // side chunklet reachable every dir
			}
		}
	}

	// this algorithm is vaguely based on https://tomcc.github.io/2014/08/31/visibility-2.html
	// ...or it would be, but we end up mostly just following straight down, oh well
	for cx := 0; cx < 32; cx++ {
		for cz := 0; cz < 32; cz++ {
			for ys := len(chunks[cx+cz*32].Blocks) - 1; ys >= 0; ys-- {
				ccv := &cv[cx+cz*32+ys*1024]
				ccv.dirReachable |= 1 << 1
				if ys > 3 {
					if ccv.connected&0b000010_000010_000010_000010_000000_000010 == 0 {
						break
					}
				} else if ccv.connected&0b000010 == 0 {
					break
				}
			}
		}
	}

	return &cv
}
