package main

import (
	"fmt"
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
	for i := range 64 {
		section := make([]uint16, 4096)
		x := (i & 3)
		y := ((i >> 2) & 3)
		z := (i >> 4)
		faces := 0
		for _, v := range []int{x, y, z} {
			if v == 0 || v == 3 {
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
			x1, y1, z1 := getPoint(loc1, 2)
			x2, y2, z2 := getPoint(loc2, 2)
			section[x1+z1*16+y1*256] = 1
			section[x2+z2*16+y2*256] = 1
			assert.True(t, isPassable(section, m, 0, 0, 0), "expected passability... (%d,%d,%d) (%d,%d,%d)",
				x1, y1, z1, x2, y2, z2)
		}
	}

}

func TestSmear6(t *testing.T) {
	for i := range 9 {
		for j := range 6 {
			x := uint64(1<<j) << (6 * i)
			assert.Equal(t, uint64((1<<6)-1)<<(6*i), smear6(x))
		}
	}
}

func TestFold36to6(t *testing.T) {
	for i := range 6 {
		for j := range 6 {
			x := uint64(1<<j) << (6 * i)
			assert.Equal(t, uint64(1<<j), fold36to6(x))
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

		cv := makeBlockvis(r, m)

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
