package main

import (
	"fmt"
	"os"

	"github.com/gammazero/deque"
	"github.com/rmmh/cubeographer/go/region"
)

const visDimBits = 2
const visDim = 1 << visDimBits
const visWidth = (32 * 16) / visDim
const visWidthBits = 9 - visDimBits

// blockVis computes reachability for each 4x4x4 part of a region.
// Originally it computed it for 16x16x16 sections, but this was found to be
// too coarse.
// 0: +x, 1: -x, 2: +y, 3: -y, 4: +z, 5: -z
type blockVis struct {
	passable  []uint64 // packed bitset representing whether a given block is passable
	reachable []uint64 // 0-5 +x ray can maybe look at (+x,-x,+y,...), 6-11 -x can maybe look at (+x, -x ...), ..., 37: in queue
}

func (cv *blockVis) passableIndex(x, y, z int) int {
	x >>= visDimBits
	y >>= visDimBits
	z >>= visDimBits
	return x + z*visWidth + y*visWidth*visWidth

}

func (cv *blockVis) setPassable(x, y, z int) {
	i := cv.passableIndex(x, y, z)
	cv.passable[i>>6] |= 1 << (i & 63)
}

func (cv *blockVis) isPassable(x, y, z int) bool {
	i := cv.passableIndex(x, y, z)
	return cv.passable[i>>6]&(1<<(i&63)) != 0
}

func (cv *blockVis) isVisible(x, y, z int) bool {
	return cv.reachable[(x>>visDimBits)+(z>>visDimBits)*visWidth+(y>>visDimBits)*(visWidth*visWidth)] != 0
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

type Solider interface {
	IsSolid(uint16) bool
}

func isPassable(section []uint16, bm Solider, qx, qy, qz int) bool {
	// if two faces of this 4x4x4 section have non-solid pieces, return true
	faces := 0
	// test +y/-y first
yneg:
	for z := range visDim {
		for x := range visDim {
			if !bm.IsSolid(section[qx+x+(qz+z)*16+qy*256]) {
				faces++
				break yneg
			}
		}
	}
ypos:
	for z := range visDim {
		for x := range visDim {
			if !bm.IsSolid(section[qx+x+(qz+z)*16+(qy+visDim-1)*256]) {
				faces++
				break ypos
			}
		}
	}
	if faces >= 2 {
		return true
	}
xneg:
	for y := range visDim {
		for z := range visDim {
			if !bm.IsSolid(section[qx+(qz+z)*16+(qy+y)*256]) {
				faces++
				break xneg
			}
		}
	}
xpos:
	for y := range visDim {
		for z := range visDim {
			if !bm.IsSolid(section[qx+visDim-1+(qz+z)*16+(qy+y)*256]) {
				faces++
				break xpos
			}
		}
	}
	if faces >= 2 {
		return true
	}
zneg:
	for y := range visDim {
		for x := range visDim {
			if !bm.IsSolid(section[qx+x+qz*16+(qy+y)*256]) {
				faces++
				break zneg
			}
		}
	}
zpos:
	for y := range visDim {
		for x := range visDim {
			if !bm.IsSolid(section[qx+x+(qz+visDim-1)*16+(qy+y)*256]) {
				faces++
				break zpos
			}
		}
	}
	return faces >= 2
}

func makeBlockvis(chunks []region.ChunkDatum, bm Solider) *blockVis {
	var cv blockVis

	maxSectionCount := 0
	for cx := range 32 {
		for cz := range 32 {
			if len(chunks[cx+cz*32].Blocks) > maxSectionCount {
				maxSectionCount = len(chunks[cx+cz*32].Blocks)
			}
		}
	}
	cv.passable = make([]uint64, visWidth*visWidth*maxSectionCount*(16/visDim)/64)
	cv.reachable = make([]uint64, visWidth*visWidth*maxSectionCount*(16/visDim))

	if maxSectionCount == 0 {
		// empty region?
		return &cv
	}

	maxY := maxSectionCount * 16 / visDim

	for cx := range 32 {
		for cz := range 32 {
			for ys, section := range chunks[cx+cz*32].Blocks {
				for y := 0; y < 16; y += visDim {
					for z := 0; z < 16; z += visDim {
						for x := 0; x < 16; x += visDim {
							if isPassable(section, bm, x, y, z) {
								cv.setPassable(cx*16+x, ys*16+y, cz*16+z)
							}

						}
					}
				}
			}
			// fill in empty sections as passable
			for ys := len(chunks[cx+cz*32].Blocks); ys < maxSectionCount; ys++ {
				for y := 0; y < 16; y += visDim {
					for z := 0; z < 16; z += visDim {
						for x := 0; x < 16; x += visDim {
							cv.setPassable(cx*16+x, ys*16+y, cz*16+z)
						}
					}
				}
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
		if mask == 0 {
			fmt.Println("updateReachable", x, y, z, mask)
			panic("wtf")
		}
		i := x + z*visWidth + y*visWidth*visWidth
		old := cv.reachable[i]
		// fmt.Println("updateReachable", x, y, z, mask, old|mask != old)
		if old|mask != old {
			cv.reachable[i] |= mask
			if cv.isPassable(x*visDim, y*visDim, z*visDim) {
				queuePush(i)
			}
		}
	}

	// 0: +x, 1: -x, 2: +y, 3: -y, 4: +z, 5: -z
	for x := range visWidth {
		for z := range visWidth {
			mask := uint64(0b111011)
			maskMult := uint64(1 << 12)
			updateReachable(x, maxY-1, z, mask*maskMult)
			if x == 0 {
				mask &^= 1 << 1
				maskMult |= 1 << 6
			} else if x == visWidth-1 {
				mask &^= 1 << 0
				maskMult |= 1 << 0
			}
			if z == 0 {
				mask &^= 1 << 5
				maskMult |= 1 << (6 * 5)
			} else if z == visWidth-1 {
				mask &^= 1 << 4
				maskMult |= 1 << (6 * 4)
			}
			if mask == 0b111011 { // i.e., not on edge
				continue
			}
			for y := 0; y < maxY; y++ {
				// TODO: use cadj to make this prune more
				updateReachable(x, y, z, mask*maskMult)
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

	iterLimit := visWidth * visWidth * maxY * 4
	for ; iterLimit > 0 && todo.Len() > 0; iterLimit-- {
		i := queuePop()
		x, y, z := i&(visWidth-1), i>>(visWidthBits*2), (i>>visWidthBits)&(visWidth-1)
		r := cv.reachable[i]
		// fmt.Println(x, y, z, todo.Len(), r)
		// check if a ray in this cell could maybe reach each of the six exit faces
		// if so, mark the reverse direction as not allowed for the new mask for each
		// for each input face that can reach that face minus the reverse direction.

		if x+1 < visWidth && r&(0b1*allDirsMultipler) != 0 {
			updateReachable(x+1, y, z, (fold36to6(smear6(r&(0b1*allDirsMultipler))&r)&^0b10)<<0)
		}
		if x > 0 && r&(0b10*allDirsMultipler) != 0 {
			updateReachable(x-1, y, z, (fold36to6(smear6(r&(0b10*allDirsMultipler))&r)&^0b1)<<6)
		}
		if y+1 < maxY && r&(0b100*allDirsMultipler) != 0 {
			updateReachable(x, y+1, z, (fold36to6(smear6(r&(0b100*allDirsMultipler))&r)&^0b1000)<<12)
		}
		if y > 0 && r&(0b1000*allDirsMultipler) != 0 {
			updateReachable(x, y-1, z, (fold36to6(smear6(r&(0b1000*allDirsMultipler))&r)&^0b100)<<18)
		}
		if z+1 < visWidth && r&(0b10000*allDirsMultipler) != 0 {
			updateReachable(x, y, z+1, (fold36to6(smear6(r&(0b10000*allDirsMultipler))&r)&^0b100000)<<24)
		}
		if z > 0 && r&(0b100000*allDirsMultipler) != 0 {
			updateReachable(x, y, z-1, (fold36to6(smear6(r&(0b100000*allDirsMultipler))&r)&^0b10000)<<30)
		}
	}

	if false {
		fmt.Println("SUMMARY:")
		for y := maxY - 1; y >= 0; y-- {
			for cz := range visWidth {
				for cx := range visWidth {
					c := cv.isPassable(cx*visDim, y*visDim, cz*visDim)
					r := cv.reachable[cx+cz*visWidth+y*visWidth*visWidth] != 0
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
		err := cv.dumpJsonMetadata("vis_metadata.json", maxY)
		if err != nil {
			// Handle the error appropriately, maybe log it
			fmt.Fprintf(os.Stderr, "Error dumping visibility metadata: %v\n", err)
		}
	}

	if iterLimit == 0 {
		panic(fmt.Sprintf("welp %d %d", 32*32*maxSectionCount*32, todo.Len()))
	}

	return &cv
}
