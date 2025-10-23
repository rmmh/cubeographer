package main

import (
	"fmt"
	"math/bits"
	"regexp"
	"strings"
	"testing"

	"github.com/rmmh/cubeographer/go/region"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

type onlyZeroIsSolid bool

func (t onlyZeroIsSolid) IsSolid(b uint16) bool {
	return (b == 0) == t
}

func printSection(section []uint16, solider Solider) {
	brailleBitMap := [2][4]byte{
		{1 << 0, 1 << 1, 1 << 2, 1 << 6}, // dx = 0
		{1 << 3, 1 << 4, 1 << 5, 1 << 7}, // dx = 1
	}

	empty := 0
	for _, b := range section {
		if !solider.IsSolid(b) {
			empty++
		}
	}

	fmt.Printf("section has %d empty blocks\n", empty)

	// Print slices in a 4x4 grid
	for yBase := 0; yBase < 16; yBase += 4 {
		var header strings.Builder
		for yOffset := 0; yOffset < 4; yOffset++ {
			header.WriteString(fmt.Sprintf("Y = %-4d    ", yBase+yOffset))
		}
		fmt.Println(header.String())

		// Print the content for the row of slices
		for z := 0; z < 16; z += 4 {
			var line strings.Builder
			for yOffset := 0; yOffset < 4; yOffset++ {
				y := yBase + yOffset
				for x := 0; x < 16; x += 2 { // Each Braille char is 2 voxels wide (in x)
					var bitmask byte
					for dz := range 4 {
						for dx := range 2 {
							if solider.IsSolid(section[x+dx+(z+dz)*16+y*256]) {
								bitmask |= brailleBitMap[dx][dz]
							}
						}
					}
					// the Braille block starts at 0x2800
					line.WriteRune(0x2800 + rune(bitmask))
				}
				line.WriteString("  ")
			}
			fmt.Println(line.String())
		}
		fmt.Println()
	}
}

func TestIsPassable(t *testing.T) {
	if visDim <= 2 {

		// any clear bit = passable
		m := onlyZeroIsSolid(true)
		for i := range visDim * visDim * visDim {
			section := make([]uint16, 4096)
			x := (i & (visDim - 1))
			y := ((i >> visDimBits) & (visDim - 1))
			z := (i >> (visDimBits * 2))
			section[x+y*16+z*256] = 1
			conn := isPassable(section, m, 0, 0, 0)
			assert.True(t, conn, "expected single-voxel passability: %d %d %d", x, y, z)
		}

		return // passability is trivial if the cubes are <=2
	}

	sideLocs := [][]int{
		{3, -1, -1}, // +x / east
		{0, -1, -1}, // -x / west
		{-1, 3, -1}, // +y / up
		{-1, 0, -1}, // -y / down
		{-1, -1, 3}, // +z / south
		{-1, -1, 0}, // -z / north
	}

	getPoint := func(loc []int, o int) (int, int, int) {
		x, y, z := loc[0], loc[1], loc[2]
		if x == -1 {
			x = o
		}
		if y == -1 {
			y = o
		}
		if z == -1 {
			z = o
		}
		return x, y, z
	}

	// one clear bit = not passable
	m := onlyZeroIsSolid(true)
	for i := range visDim * visDim * visDim {
		section := make([]uint16, 4096)
		x := (i & (visDim - 1))
		y := ((i >> visDimBits) & (visDim - 1))
		z := (i >> (visDimBits * 2))
		faces := 0
		for _, v := range []int{x, y, z} {
			if v == 0 || v == visDim-1 {
				faces++
			}
		}
		if faces > 1 {
			continue
		}
		section[x+y*16+z*256] = 1
		conn := isPassable(section, m, 0, 0, 0)
		assert.False(t, conn, "unexpected single-voxel passability: %d %d %d", x, y, z)
	}

	// two sides clear = passable
	for i, loc1 := range sideLocs {
		for _, loc2 := range sideLocs[:i] {
			section := make([]uint16, 4096)
			x1, y1, z1 := getPoint(loc1, 1)
			x2, y2, z2 := getPoint(loc2, 1)
			section[x1+z1*16+y1*256] = 1
			section[x2+z2*16+y2*256] = 1
			assert.True(t, isPassable(section, m, 0, 0, 0), "expected passability... (%d,%d,%d) (%d,%d,%d)",
				x1, y1, z1, x2, y2, z2)
		}
	}
}

func TestMakeChunkvis(t *testing.T) {
	m := onlyZeroIsSolid(false)

	empty := make([]uint16, 4096)
	solid := make([]uint16, 4096)
	for i := range solid {
		solid[i] = 1
	}

	for _, scene := range []string{
		`
		Y # # # #
		Y # 0 y #
		Y # # y #
		* * * * #`,
		`
		Y # # # # # #
		Y # 0 y * * #
		Y # # y # 0 #
		* * * * # # #`,
		`
		Y # # # # # #
		Y # 0 y * * #
		Y # # y # 0 #
		* * * * # * * * #  `,
		`
		# Y # # # Y #
		# Y # 0 # Y #
		# Y * * * * #
		# # # * # # #
		# * * * * * #
		# * # # # * #
		# * 0 0 0 * #`,
	} {
		fmt.Println()
		lines := lo.Map(strings.Split(strings.TrimSpace(scene), "\n"), func(line string, idx int) []string {
			return regexp.MustCompile(`\S+`).FindAllString(line, -1)
		})
		r := make([]region.ChunkDatum, 1024)
		for cx := range 32 {
			for cz := range 32 {
				b := make([][]uint16, len(lines)+2)
				for i := range b {
					if i == 0 {
						b[i] = solid
					} else {
						b[i] = empty
					}
				}
				r[cx+cz*32].Blocks = b
			}
		}

		set := func(cx, cy, cz int) {
			r[cx+cz*32].Blocks[cy] = solid
		}

		for n, els := range lines {
			for ho, el := range els {
				y := len(lines) - n
				set(2, y, ho)
				if el == "#" {
					set(3, y, ho)
				}
				set(4, y, ho)
			}
		}

		for _, mode := range []visibilityMode{visOctahedral, visTriakisOctahedral} {
			cv := makeBlockvis(r, m, mode)
			for n, els := range lines {
				for ho, _ := range els {
					y := len(lines) - n
					if !cv.isPassable(3*16, y*16, ho*16) {
						fmt.Print("#")
						//assert.Equal(t, "#", el)
					} else {
						if cv.isVisible(3*16, y*16, ho*16) {
							fmt.Print("+")
						} else {
							fmt.Print(" ")
						}
					}
				}
				fmt.Println()
			}

			for n, els := range lines {
				for ho, el := range els {
					y := len(lines) - n
					visible := cv.isVisible(3*16, y*16, ho*16)
					if el == "0" {
						assert.False(t, visible, "section should be invisible")
					} else if el != "#" {
						assert.True(t, visible, "section should be visible")
					}
				}
			}

		}
	}
}

func TestOctahedronProperties(t *testing.T) {
	octAxes := []uint{
		octXPos, octXNeg,
		octYPos, octYNeg,
		octZPos, octZNeg,
	}

	assert.Equal(t, 8, bits.OnesCount(octAll))

	for _, axis := range octAxes {
		assert.Equal(t, 4, bits.OnesCount(axis), "axis %b should have 4 faces", axis)
	}

	assert.Equal(t, octAll, octXPos|octXNeg)
	assert.Zero(t, octXPos&octXNeg)

	assert.Equal(t, octAll, octYPos|octYNeg)
	assert.Zero(t, octYPos&octYNeg)

	assert.Equal(t, octAll, octZPos|octZNeg)
	assert.Zero(t, octZPos&octZNeg)

	for i := range 8 {
		mask := uint(1 << i)
		count := 0
		for _, axis := range octAxes {
			if axis&mask != 0 {
				count++
			}
		}
		assert.Equal(t, 3, count, "face %b is in %d axis constants, expected 3", i, mask, count)
	}
}

func TestTriakisOctahedronProperties(t *testing.T) {
	triOctAxes := []uint{
		triOctXPos, triOctXNeg,
		triOctYPos, triOctYNeg,
		triOctZPos, triOctZNeg,
	}

	triOctDiagonals := []uint{
		triOctXNegYNegZNeg, triOctXNegYNegZPos,
		triOctXNegYPosZNeg, triOctXNegYPosZPos,
		triOctXPosYNegZNeg, triOctXPosYNegZPos,
		triOctXPosYPosZNeg, triOctXPosYPosZPos,
	}

	assert.Equal(t, 24, bits.OnesCount(triOctAll))

	for i, axis := range triOctAxes {
		assert.Equal(t, 8, bits.OnesCount(axis), "axis constant %b should have 8 faces", i)
	}

	for i, diag := range triOctDiagonals {
		assert.Equal(t, 3, bits.OnesCount(diag), "diag constant %b should have 3 faces", i)
	}

	for i := range 24 {
		mask := uint(1 << i)
		axisCount := 0
		diagCount := 0

		for _, axis := range triOctAxes {
			if axis&mask != 0 {
				axisCount++
			}
		}

		for _, diag := range triOctDiagonals {
			if diag&mask != 0 {
				diagCount++
			}
		}

		assert.Equal(t, 2, axisCount, "face %b is in %d axis constants, expected 2", mask, axisCount)
		assert.Equal(t, 1, diagCount, "face %b is in %d diagonal constants, expected 1", mask, diagCount)
	}

	var allAxes uint
	for _, axis := range triOctAxes {
		allAxes |= axis
	}
	assert.Equal(t, uint(triOctAll), allAxes)

	var allDiags uint
	for _, diag := range triOctDiagonals {
		allDiags |= diag
	}
	assert.Equal(t, uint(triOctAll), allDiags)
}
