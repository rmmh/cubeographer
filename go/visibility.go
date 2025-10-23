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
	passable  []uint64 // packed bitset representing whether a given cell is passable, with 0 meaning passable
	reachable []uint32 // which faces can reach this cell?
	// octahedral: 0: -x-y-z, 1: -x-y+z, 2: -x+y-z, ... 31: queued
}

const reachableQueued = 1 << 31

const (
	// octahedron masks
	// https://dmccooey.com/polyhedra/Octahedron.html
	octAll  = 0b11111111
	octXPos = 0b11110000
	octXNeg = 0b00001111
	octYPos = 0b11001100
	octYNeg = 0b00110011
	octZPos = 0b10101010
	octZNeg = 0b01010101
)

const (
	// triakis octahedron masks
	// (split each of the 8 faces of an octahedron into 3 triangles)
	// https://dmccooey.com/polyhedra/TriakisOctahedron.html
	triOctAll = (1 << 24) - 1

	// each axis has 8 touching faces
	triOctXPos = 0b101101_101101_000000_000000
	triOctXNeg = 0b000000_000000_101101_101101
	triOctYPos = 0b011011_000000_011011_000000
	triOctYNeg = 0b000000_011011_000000_011011
	triOctZPos = 0b110000_110000_110000_110000
	triOctZNeg = 0b000110_000110_000110_000110

	// each cube corner has 3 faces touching
	triOctXNegYNegZNeg = 0b000000_000000_000000_000111
	triOctXNegYNegZPos = 0b000000_000000_000000_111000
	triOctXNegYPosZNeg = 0b000000_000000_000111_000000
	triOctXNegYPosZPos = 0b000000_000000_111000_000000
	triOctXPosYNegZNeg = 0b000000_000111_000000_000000
	triOctXPosYNegZPos = 0b000000_111000_000000_000000
	triOctXPosYPosZNeg = 0b000111_000000_000000_000000
	triOctXPosYPosZPos = 0b111000_000000_000000_000000
)

type visibilityMode int

const (
	visOctahedral visibilityMode = iota
	visTriakisOctahedral
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

func makeBlockvis(chunks []region.ChunkDatum, bm Solider, mode visibilityMode) *blockVis {
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

	mask := uint32(octAll)
	// to only allow downward octahedral faces:
	// mask = octFaceZNegMask
	if mode == visTriakisOctahedral {
		mask = triOctAll
	}

	for x := range visWidth {
		for z := range visWidth {
			updateReachable(x, maxY-1, z, mask)
		}
	}
	// Pushing the sides after the top gives faster BFS convergence.
	// Doing this inside the loop when the iteration reaches the correct
	// Y level is a few percent faster still, but makes the code even harder to read.
	// Also, pushing top down gives faster BFS convergence
	for y := maxY - 1; y >= 0; y-- {
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
		Reachable tracks which polyhedra faces might be able to reach
			a given cell, and is an approximation of visibility. It is
			what this loop is computing. Rays from each face
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

		As a further refinement, we can trace rays from the faces of a
		"triakis octahedra", a polyhedra with 24 faces built by dividing each
		of the faces of an octahedra into a further 3 triangles. This helps
		restrict each ray's visibility/transmission space even further.

		This algorithm is inspired by previous work on Minecraft occlusion
		culling, but adapted to work for multiple viewpoints instead
		of a single camera position:
		https://tomcc.github.io/2014/08/31/visibility-2.html
	*/

	iterLimit := visWidth * visWidth * maxY * 16
	for ; iterLimit > 0 && todo.Len() > 0; iterLimit-- {
		i := queuePop()
		x, y, z := i&(visWidth-1), i>>(visWidthBits*2), (i>>visWidthBits)&(visWidth-1)
		r := cv.reachable[i]
		// Check if a ray in this cell could maybe reach each of the six exit faces,
		// if so, mark that adjacent cell as reachable by each face that could reach it.

		const (
			dXP = 1 << iota
			dXN
			dYP
			dYN
			dZP
			dZN
		)

		dirs := 0
		if x+1 < visWidth {
			dirs |= dXP
		}
		if x > 0 {
			dirs |= dXN
		}
		if y+1 < maxY {
			dirs |= dYP
		}
		if y > 0 {
			dirs |= dYN
		}
		if z+1 < visWidth {
			dirs |= dZP
		}
		if z > 0 {
			dirs |= dZN
		}

		switch mode {
		case visOctahedral:
			if r&octXPos != 0 && dirs&dXP != 0 {
				updateReachable(x+1, y, z, r&octXPos)
			}
			if r&octXNeg != 0 && dirs&dXN != 0 {
				updateReachable(x-1, y, z, r&octXNeg)
			}
			if r&octYPos != 0 && dirs&dYP != 0 {
				updateReachable(x, y+1, z, r&octYPos)
			}
			if r&octYNeg != 0 && dirs&dYN != 0 {
				updateReachable(x, y-1, z, r&octYNeg)
			}
			if r&octZPos != 0 && dirs&dZP != 0 {
				updateReachable(x, y, z+1, r&octZPos)
			}
			if r&octZNeg != 0 && dirs&dZN != 0 {
				updateReachable(x, y, z-1, r&octZNeg)
			}
		case visTriakisOctahedral:
			// axes
			if r&triOctXPos != 0 && dirs&dXP != 0 {
				updateReachable(x+1, y, z, r&triOctXPos)
			}
			if r&triOctXNeg != 0 && dirs&dXN != 0 {
				updateReachable(x-1, y, z, r&triOctXNeg)
			}
			if r&triOctYPos != 0 && dirs&dYP != 0 {
				updateReachable(x, y+1, z, r&triOctYPos)
			}
			if r&triOctYNeg != 0 && dirs&dYN != 0 {
				updateReachable(x, y-1, z, r&triOctYNeg)
			}
			if r&triOctZPos != 0 && dirs&dZP != 0 {
				updateReachable(x, y, z+1, r&triOctZPos)
			}
			if r&triOctZNeg != 0 && dirs&dZN != 0 {
				updateReachable(x, y, z-1, r&triOctZNeg)
			}

			// diag
			if r&triOctXPosYPosZPos != 0 && dirs&(dXP|dYP|dZP) == (dXP|dYP|dZP) {
				updateReachable(x+1, y+1, z+1, r&triOctXPosYPosZPos)
			}
			if r&triOctXPosYPosZNeg != 0 && dirs&(dXP|dYP|dZN) == (dXP|dYP|dZN) {
				updateReachable(x+1, y+1, z-1, r&triOctXPosYPosZNeg)
			}
			if r&triOctXPosYNegZPos != 0 && dirs&(dXP|dYN|dZP) == (dXP|dYN|dZP) {
				updateReachable(x+1, y-1, z+1, r&triOctXPosYNegZPos)
			}
			if r&triOctXPosYNegZNeg != 0 && dirs&(dXP|dYN|dZN) == (dXP|dYN|dZN) {
				updateReachable(x+1, y-1, z-1, r&triOctXPosYNegZNeg)
			}
			if r&triOctXNegYPosZPos != 0 && dirs&(dXN|dYP|dZP) == (dXN|dYP|dZP) {
				updateReachable(x-1, y+1, z+1, r&triOctXNegYPosZPos)
			}
			if r&triOctXNegYPosZNeg != 0 && dirs&(dXN|dYP|dZN) == (dXN|dYP|dZN) {
				updateReachable(x-1, y+1, z-1, r&triOctXNegYPosZNeg)
			}
			if r&triOctXNegYNegZPos != 0 && dirs&(dXN|dYN|dZP) == (dXN|dYN|dZP) {
				updateReachable(x-1, y-1, z+1, r&triOctXNegYNegZPos)
			}
			if r&triOctXNegYNegZNeg != 0 && dirs&(dXN|dYN|dZN) == (dXN|dYN|dZN) {
				updateReachable(x-1, y-1, z-1, r&triOctXNegYNegZNeg)
			}
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
