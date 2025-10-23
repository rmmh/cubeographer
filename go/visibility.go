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
type blockVis struct {
	passable  []uint64 // packed bitset representing whether a given block is passable, with 0 meaning passable
	reachable []uint32 // which faces can reach?
	// octahedral: 0: -x-y-z, 1: -x-y+z, 2: -x+y-z, ... 31: queued
}

const (
	reachableQueued = 1 << 31
	octFaceAllMask  = 0b11111111
	octFaceXPosMask = 0b11110000
	octFaceXNegMask = 0b00001111
	octFaceYPosMask = 0b11001100
	octFaceYNegMask = 0b00110011
	octFaceZPosMask = 0b10101010
	octFaceZNegMask = 0b01010101
)

func (cv *blockVis) passableIndex(x, y, z int) int {
	x >>= visDimBits
	y >>= visDimBits
	z >>= visDimBits
	return x + z*visWidth + y*visWidth*visWidth

}

func (cv *blockVis) setSolid(x, y, z int) {
	i := cv.passableIndex(x, y, z)
	cv.passable[i>>6] |= 1 << (i & 63)
}

func (cv *blockVis) isPassable(x, y, z int) bool {
	i := cv.passableIndex(x, y, z)
	return cv.passable[i>>6]&(1<<(i&63)) == 0
}

func (cv *blockVis) isVisible(x, y, z int) bool {
	return cv.reachable[(x>>visDimBits)+(z>>visDimBits)*visWidth+(y>>visDimBits)*(visWidth*visWidth)] != 0
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
	cv.reachable = make([]uint32, visWidth*visWidth*maxSectionCount*(16/visDim))

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
							if !isPassable(section, bm, x, y, z) {
								cv.setSolid(cx*16+x, ys*16+y, cz*16+z)
							}
						}
					}
				}
			}
		}
	}

	// the queue for BFS
	var todo deque.Deque[int]
	queuePush := func(idx int) {
		if cv.reachable[idx]&reachableQueued != 0 {
			return
		}
		cv.reachable[idx] |= reachableQueued
		todo.PushBack(idx)
	}
	queuePop := func() int {
		idx := todo.PopFront()
		cv.reachable[idx] &^= reachableQueued
		return idx
	}
	updateReachable := func(x, y, z int, mask uint32) {
		i := x + z*visWidth + y*visWidth*visWidth
		old := cv.reachable[i]
		if old|mask != old {
			cv.reachable[i] |= mask
			if cv.isPassable(x*visDim, y*visDim, z*visDim) {
				queuePush(i)
			}
		}
	}

	for x := range visWidth {
		for z := range visWidth {
			mask := uint32(octFaceAllMask)
			// to only allow downward octahedral faces:
			// mask = octFaceZNegMask
			updateReachable(x, maxY-1, z, mask)
		}
	}
	// Pushing the sides after the top gives faster BFS convergence.
	// Doing this inside the loop when the iteration reaches the correct
	// Y level is a few percent faster still, but makes the code even harder to read.
	// Also, pushing top down gives faster BFS convergence
	for y := maxY - 1; y >= 0; y-- {
		mask := uint32(octFaceAllMask)
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
		quickly propagate the up to four difference faces that can go in
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
		// Check if a ray in this cell could maybe reach each of the six exit faces,
		// if so, mark that adjacent cell as reachable by each octahedral faces that
		// could reach it.
		if x+1 < visWidth && r&octFaceXPosMask != 0 {
			updateReachable(x+1, y, z, r&octFaceXPosMask)
		}
		if x > 0 && r&octFaceXNegMask != 0 {
			updateReachable(x-1, y, z, r&octFaceXNegMask)
		}
		if y+1 < maxY && r&octFaceYPosMask != 0 {
			updateReachable(x, y+1, z, r&octFaceYPosMask)
		}
		if y > 0 && r&octFaceYNegMask != 0 {
			updateReachable(x, y-1, z, r&octFaceYNegMask)
		}
		if z+1 < visWidth && r&octFaceZPosMask != 0 {
			updateReachable(x, y, z+1, r&octFaceZPosMask)
		}
		if z > 0 && r&octFaceZNegMask != 0 {
			updateReachable(x, y, z-1, r&octFaceZNegMask)
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
