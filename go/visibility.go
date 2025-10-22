package main

import (
	"fmt"

	"github.com/gammazero/deque"
	"github.com/rmmh/cubeographer/go/region"
)

const visDimBits = 1
const visDim = 1 << visDimBits
const visWidth = (32 * 16) / visDim
const visWidthBits = 9 - visDimBits

// blockVis computes reachability for each 2x2x2 part of a region.
// Originally it computed it for 16x16x16 sections, but this was found to be
// too coarse.
// 0: +x, 1: -x, 2: +y, 3: -y, 4: +z, 5: -z
type blockVis struct {
	passable  []uint64 // packed bitset representing whether a given block is passable
	reachable []uint64 // 0-5 rays through this face can look at (+x,-x,+y,...), 6-11 face can look at (+x, -x ...), ..., 49: in queue
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

const (
	allOctahedralFacesDirs = 0b101010_101001_100110_100101_011010_011001_010110_010101
	allFacesMultiplier     = 0b000001_000001_000001_000001_000001_000001_000001_000001
)

// smear6 makes it so if any bit is set in a 6-bit group, all bits in that group are set.
// This is useful for making masks of groups that have certain bits set.
func smear6(x uint64) uint64 {
	x |= (x &^ 0b000001_000001_000001_000001_000001_000001_000001_000001_000001_000001_000001) >> 1
	x |= x >> 2
	x |= x >> 3
	return (x & 0b000001_000001_000001_000001_000001_000001_000001_000001_000001_000001_000001) * 0b111111
}

type Solider interface {
	IsSolid(uint16) bool
}

func isPassable(section []uint16, bm Solider, qx, qy, qz int) bool {
	if visDim <= 2 {
		// any clear block in this section means it's passable
		for y := range visDim {
			for z := range visDim {
				for x := range visDim {
					if !bm.IsSolid(section[qx+x+(qz+z)*16+(qy+y)*256]) {
						return true
					}
				}
			}
		}
		return false
	}

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
		if cv.reachable[idx]&(1<<49) != 0 {
			return
		}
		cv.reachable[idx] |= 1 << 49
		todo.PushBack(idx)
	}
	queuePop := func() int {
		idx := todo.PopFront()
		cv.reachable[idx] &^= 1 << 49
		return idx
	}
	updateReachable := func(x, y, z int, mask uint64) {
		i := x + z*visWidth + y*visWidth*visWidth
		old := cv.reachable[i]
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
			mask := uint64(allOctahedralFacesDirs)
			// to only allow downward octahedral faces:
			// mask = smear6(mask&(0b1000*allFacesMultiplier)) & mask
			updateReachable(x, maxY-1, z, mask)
		}
	}
	// Pushing the sides after the top gives faster BFS convergence.
	// Doing this inside the loop when the iteration reaches the correct
	// Y level is a few percent faster still, but makes the code even harder to read.
	// Also, pushing top down gives faster BFS convergence
	for y := maxY - 1; y >= 0; y-- {
		mask := uint64(allOctahedralFacesDirs)
		for x := range visWidth {
			updateReachable(x, y, 0, mask)
			updateReachable(x, y, visWidth-1, mask)
		}
		for z := range visWidth {
			updateReachable(0, y, z, mask)
			updateReachable(visWidth-1, y, z, mask)
		}
	}

	/*
		Run a BFS to compute visibility throughout the scene.

		Passable tracks whether rays might be able to traverse a
			given cell, and is precomputed in a single pass.
		Reachable tracks which octahedral faces might be able to reach
			a given cell, and is an approximation of visibility. It is
			what this loop is computing. Rays from each octahedral face
			can only move in three directions (e.g. +x/-y/+z), which
			helps slightly in constraining the exploration to be better
			than naive flood-fill BFS which would find fully surrounded
			regions.
			A nonzero reachable value indicates that a given cell can
			be entered somehow, and thus might be visible!

		Consider the 2D case:

		   ^
		  / \
		 < * >
		  \ /        +-+
		   v   +---- |#|
		       |#### +-+
		       |####
		       |####

		The light source * is a diamond to indicate the shape of its faces,
		and the # are shadows. The bottom-right face of the diamond emits
		rays that can pathfind *only* downward or to the right, producing
		the shadowed regions after performing a BFS.

		In 3D we use a regular octahedron that similarly has its vertices
		touching the centers of the cell's cube, and each octahedral face
		can go only in one direction on each axis. These rays moving in 3
		directions is a large improvement over a previous hemisphere-based
		occlusion method where rays can move in 5 directions.

		To do this efficiently, we use bit operations so that each cell has
		reachability from each octahedral face, and when it's visited we can
		quickly propagate the up to four difference faces that might go in
		a certain direction.

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
		// check if a ray in this cell could maybe reach each of the six exit faces,
		// if so, mark that adjacent cell as reachable by each octahedral face that
		// could reach it.
		if x+1 < visWidth && r&((1<<0)*allFacesMultiplier) != 0 {
			updateReachable(x+1, y, z, r&smear6(r&((1<<0)*allFacesMultiplier)))
		}
		if x > 0 && r&((1<<1)*allFacesMultiplier) != 0 {
			updateReachable(x-1, y, z, r&smear6(r&((1<<1)*allFacesMultiplier)))
		}
		if y+1 < maxY && r&((1<<2)*allFacesMultiplier) != 0 {
			updateReachable(x, y+1, z, r&smear6(r&((1<<2)*allFacesMultiplier)))
		}
		if y > 0 && r&((1<<3)*allFacesMultiplier) != 0 {
			updateReachable(x, y-1, z, r&smear6(r&((1<<3)*allFacesMultiplier)))
		}
		if z+1 < visWidth && r&((1<<4)*allFacesMultiplier) != 0 {
			updateReachable(x, y, z+1, r&smear6(r&((1<<4)*allFacesMultiplier)))
		}
		if z > 0 && r&((1<<5)*allFacesMultiplier) != 0 {
			updateReachable(x, y, z-1, r&smear6(r&((1<<5)*allFacesMultiplier)))
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
	}

	if iterLimit == 0 {
		panic(fmt.Sprintf("BFS convergence took too long? %d %d", 32*32*maxSectionCount*32, todo.Len()))
	}

	return &cv
}
