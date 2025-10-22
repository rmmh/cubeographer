package main

import (
	"fmt"
	"math/bits"

	"github.com/gammazero/deque"
	"github.com/rmmh/cubeographer/go/region"
)

// 0: +x, 1: -x, 2: +y, 3: -y, 4: +z, 5: -z
type blockVis struct {
	connectivity []uint64 // 0-5 player entering +x can reach (+x,-x,+y,...), 6-11 -x can reach (+x, -x ...)
	reachable    []uint64 // 0-5 +y ray can maybe look at (+x,-x,+y,...), 6-11 -x can maybe look at (+x, -x ...), 37: in queue
}

func (cv *blockVis) isVisible(x, y, z int) bool {
	return cv.reachable[(x>>4)+(z>>4)*32+(y>>4)*1024] != 0
}

const allDirsMultipler = 0b1_000001_000001_000001_000001_000001

func smear6(x uint64) uint64 {
	x |= (x &^ 0b000001_000001_000001_000001_000001_000001_000001_000001_000001_000001_000001) >> 1
	x |= x >> 2
	x |= x >> 3
	return (x & 0b000001_000001_000001_000001_000001_000001_000001_000001_000001_000001_000001) * 0b111111
}

func fold36to6(x uint64) uint64 {
	// (a, b, c, d, e, f) => (a, a|b, c, c|d, e, e|f)
	x |= (x & 0b111111_000000_111111_000000_111111_000000) >> 6
	x |= (x & (0b111111 << 12)) >> 12 // (a, a|b, c, c|d, e, e|f) => (a, a|b, c, c|d, e, c|d|e|f)
	x |= (x & (0b111111 << 24)) >> 24 // (a, a|b, c, c|d, e, c|d|e|f) => (a, a|b, c, c|d, e, a|b|c|d|e|f)
	return x & 0b111111
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

type Solider interface {
	IsSolid(uint16) bool
}

func computeConnected(section []uint16, bm Solider) uint64 {
	var passable tinybitset
	for i, b := range section {
		if !bm.IsSolid(b) {
			passable.set(i)
		}
	}
	var conn uint64
	for cur := passable.pop(); cur != -1; cur = passable.pop() {
		// layout: x+z*16+y*256
		faces := 0
		var todo tinybitset
		todo.set(cur)
		for cur = todo.pop(); cur != -1; cur = todo.pop() {
			passable.clear(cur)
			if (cur & 0xF) == 0 { // -x, i.e. an exit to the negative x face (west)
				faces |= 1 << 1
				if passable.has(cur + 1) {
					todo.set(cur + 1)
				}
			} else if (cur & 0xF) >= 15 { // +x
				faces |= 1 << 0
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
			if cur < 256 { // -y, i.e. an exit to the negative y face (down)
				faces |= 1 << 3
				if passable.has(cur + 256) {
					todo.set(cur + 256)
				}
			} else if cur >= 15*256 { // +y
				faces |= 1 << 2
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
		}
		for i := 0; i < 6; i++ {
			if faces&(1<<i) != 0 {
				conn |= uint64(faces) << (6 * i)
			}
		}
	}
	return conn
}

func makeBlockvis(chunks []region.ChunkDatum, bm Solider) *blockVis {
	var cv blockVis

	maxY := 0
	for cx := range 32 {
		for cz := range 32 {
			if len(chunks[cx+cz*32].Blocks) > maxY {
				maxY = len(chunks[cx+cz*32].Blocks)
			}
		}
	}
	cv.connectivity = make([]uint64, 32*32*maxY)
	cv.reachable = make([]uint64, 32*32*maxY)

	if maxY == 0 {
		// empty region?
		return &cv
	}

	for cx := range 32 {
		for cz := range 32 {
			for ys, section := range chunks[cx+cz*32].Blocks {
				cv.connectivity[cx+cz*32+ys*1024] = computeConnected(section, bm)
			}
			// fill in empty chunks as air (full connectivity)
			for y := len(chunks[cx+cz*32].Blocks); y < maxY; y++ {
				cv.connectivity[cx+cz*32+y*1024] = 0b111111 * allDirsMultipler
			}
		}
	}

	// the queue for BFS
	var todo deque.Deque[int]
	queuePush := func(idx int) {
		if cv.reachable[idx]&(1<<37) != 0 {
			return
		}
		// fmt.Println("queuePush", idx&31, idx>>10, (idx>>5)&31, idx)
		cv.reachable[idx] |= 1 << 37
		todo.PushBack(idx)
	}
	queuePop := func() int {
		idx := todo.PopFront()
		cv.reachable[idx] &^= 1 << 37
		return idx
	}
	updateReachable := func(x, y, z int, mask uint64) {
		i := x + z*32 + y*1024
		old := cv.reachable[i]
		if old|mask != old {
			cv.reachable[i] |= mask
			conns := cv.connectivity[i]
			if fold36to6((old|mask)&conns) != fold36to6(old&conns) {
				// we only need to revisit the cell if the potential new links
				// are different
				// fmt.Printf("conns=%36b, %36b -> %36b (%b)\n", conns, old&conns, (old|mask)&conns, fold36to6((old|mask)&conns))
				queuePush(i)
			}
		}
	}

	// 0: +x, 1: -x, 2: +y, 3: -y, 4: +z, 5: -z
	for cx := range 32 {
		for cz := range 32 {
			chunk := chunks[cx+cz*32]
			if len(chunk.Blocks) == 0 {
				continue
			}
			mask := uint64(0b111111) // sky rays going down can go in any direction
			updateReachable(cx, maxY-1, cz, mask<<12)
			if cx == 0 {
				mask &^= 1 << 1
			} else if cx == 31 {
				mask &^= 1 << 0
			}
			if cz == 0 {
				mask &^= 1 << 5
			} else if cz == 31 {
				mask &^= 1 << 4
			}
			if mask == 0b111111 { // i.e., not on edge
				continue
			}
			for ys := 0; ys < len(chunk.Blocks); ys++ {
				// TODO: use cadj to make this prune more
				updateReachable(cx, ys, cz, mask<<12)
			}
		}
	}

	/*
		Run a BFS to compute visibility throughout the scene.

		Connectivity tracks how a ray could traverse a given region:
			which faces a ray entering a given face could exit.
			Connectivity is read-only after this point.
		Reachable tracks which directions a ray entering from a given
			face can traverse next.
			Reachable is the thing we are updating. A nonzero value
			indicates that a given cell can be entered somehow, and
			might be visible!

		Consider this 2D case where a ray is entering A from the left:

		┼───┼───┼───┼
		│           │
		│ B──►C──►D │
		│ ▲         │
		┼ │ ┼───┼   ┼
		  │ │   │   │
		─►A │ X │ E │
			│   │   │
		┼───┼───┼───┼

		Connectivity says that A's left and top faces are connected,
		B's bottom and right faces are connected, and so on.

		Reachable says that A can be entered from the left, and that
		this ray can explore up, right, or down.

		The tricky part: to vaguely approximate a viewing cone without
		using too many bits and not say that E is visible from A, when
		we traverse up from A to B, B gets a reachable flag based on
		the reachable bits from, with "down" marked off-- so reachable
		says that B can be entered from the bottom, and that this ray
		can explore up, or right (but not left or down!).

		This algorithm is inspired by previous work on Minecraft occlusion
		culling, but adapted to work for multiple viewpoints instead
		of a single camera position:
		https://tomcc.github.io/2014/08/31/visibility-2.html
	*/

	iterLimit := 32 * 32 * maxY * 4
	for ; iterLimit > 0 && todo.Len() > 0; iterLimit-- {
		i := queuePop()
		x, y, z := i&31, i>>10, (i>>5)&31
		r := cv.reachable[i]
		// fmt.Println(x, y, z, todo.Len(), r)
		// check if a ray in this cell could maybe reach each of the six exit faces
		// if so, check if the connectivity rules permit it, then fold together
		// allowed new reachability based on ORing the reachability for each
		// input face that can reach that face minus the reverse direction.

		if x+1 < 32 && r&(0b1*allDirsMultipler) != 0 {
			updateReachable(x+1, y, z, (fold36to6(smear6(r&cv.connectivity[i]&(0b1*allDirsMultipler))&r)&^0b10)<<0)
		}
		if x > 0 && r&(0b10*allDirsMultipler) != 0 {
			updateReachable(x-1, y, z, (fold36to6(smear6(r&cv.connectivity[i]&(0b10*allDirsMultipler))&r)&^0b1)<<6)
		}
		if y+1 < maxY && r&(0b100*allDirsMultipler) != 0 {
			updateReachable(x, y+1, z, (fold36to6(smear6(r&cv.connectivity[i]&(0b100*allDirsMultipler))&r)&^0b1000)<<12)
		}
		if y > 0 && r&(0b1000*allDirsMultipler) != 0 {
			updateReachable(x, y-1, z, (fold36to6(smear6(r&cv.connectivity[i]&(0b1000*allDirsMultipler))&r)&^0b100)<<18)
		}
		if z+1 < 32 && r&(0b10000*allDirsMultipler) != 0 {
			updateReachable(x, y, z+1, (fold36to6(smear6(r&cv.connectivity[i]&(0b10000*allDirsMultipler))&r)&^0b100000)<<24)
		}
		if z > 0 && r&(0b100000*allDirsMultipler) != 0 {
			updateReachable(x, y, z-1, (fold36to6(smear6(r&cv.connectivity[i]&(0b100000*allDirsMultipler))&r)&^0b10000)<<30)
		}
	}

	if false {
		fmt.Println("SUMMARY:")
		for y := maxY - 1; y >= 0; y-- {
			for cz := range 32 {
				for cx := range 32 {
					c := cv.connectivity[cx+cz*32+y*1024] != 0
					r := cv.reachable[cx+cz*32+y*1024] != 0
					if c && r {
						fmt.Print(".")
					} else if c {
						fmt.Print("X")
					} else if r {
						fmt.Print("#")
					} else {
						fmt.Print(" ")
					}
				}
				fmt.Println()
			}
			fmt.Println()
		}
	}

	if iterLimit == 0 {
		panic(fmt.Sprintf("welp %d %d", 32*32*maxY*32, todo.Len()))
	}

	return &cv
}
