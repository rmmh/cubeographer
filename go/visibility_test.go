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

func sign(x int) int {
	if x < 0 {
		return -1
	} else if x > 0 {
		return 1
	}
	return 0
}

func markTunnel(section []uint16, x1, y1, z1, x2, y2, z2 int) {
	mark := func(x, y, z int) {
		section[x+z*16+y*256] = 1
	}

	cx, cy, cz := x1, y1, z1

	// draw a tunnel with two straight legs towards the center of the cube
	for i := range 2 {
		if cx == 0 || cx == 15 || i == 1 {
			for dx := sign(x2 - cx); cx != x2; cx += dx {
				mark(cx, cy, cz)
			}
		}

		if cy == 0 || cy == 15 || i == 1 {
			for dy := sign(y2 - cy); cy != y2; cy += dy {
				mark(cx, cy, cz)
			}
		}

		if cz == 0 || cz == 15 || i == 1 {
			for dz := sign(z2 - cz); cz != z2; cz += dz {
				mark(cx, cy, cz)
			}
		}
	}

	mark(x2, y2, z2)
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

func TestComputeConnected(t *testing.T) {
	sideLocs := [][]int{
		{15, -1, -1}, // +x / east
		{0, -1, -1},  // -x / west
		{-1, 15, -1}, // +y / up
		{-1, 0, -1},  // -y / down
		{-1, -1, 15}, // +z / south
		{-1, -1, 0},  // -z / north
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

	// test each side alone
	m := onlyZeroIsSolid(true)
	for i, loc := range sideLocs {
		section := make([]uint16, 4096)
		x, y, z := getPoint(loc, 8)
		section[x+y*256+z*16] = 1
		conn := computeConnected(section, m)
		assert.Equal(t, uint64(1<<i)<<(i*6), conn, "unexpected single-face connectivity: %d", i)
	}

	// test a tunnel made between each side
	for i, loc1 := range sideLocs {
		for j, loc2 := range sideLocs[:i] {
			section := make([]uint16, 4096)
			x1, y1, z1 := getPoint(loc1, 8)
			x2, y2, z2 := getPoint(loc2, 8)
			markTunnel(section, x1, y1, z1, x2, y2, z2)
			faces := uint64(1<<i | 1<<j)
			expected := faces<<(i*6) | faces<<(j*6)
			conn := computeConnected(section, m)
			if expected != conn {
				fmt.Println("tunnel from", x1, y1, z1, "to", x2, y2, z2)
				printSection(section, m)
			}
			assert.Equal(t, expected, conn, "unexpected double-face connectivity: %d and %d", i, j)
		}
	}

	// test tunnels made between TWO pairs of sides (!!)
	for a, loc1 := range sideLocs {
		for b, loc2 := range sideLocs[:a] {
			for c, loc3 := range sideLocs {
				for d, loc4 := range sideLocs[:c] {
					section := make([]uint16, 4096)
					x1, y1, z1 := getPoint(loc1, 2)
					x2, y2, z2 := getPoint(loc2, 2)
					markTunnel(section, x1, y1, z1, x2, y2, z2)
					x3, y3, z3 := getPoint(loc3, 10)
					x4, y4, z4 := getPoint(loc4, 10)
					markTunnel(section, x3, y3, z3, x4, y4, z4)
					facesAB := uint64(1<<a | 1<<b)
					facesCD := uint64(1<<c | 1<<d)
					expected := facesAB<<(a*6) | facesAB<<(b*6) | facesCD<<(c*6) | facesCD<<(d*6)
					conn := computeConnected(section, m)
					assert.Equal(t, expected, conn, "unexpected quad-face connectivity: %d and %d, %d and %d", a, b, c, d)
				}
			}
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
	} {
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
				fmt.Println(ho, y)
			}
		}

		cv := makeBlockvis(r, m)

		getReachable := func(cx, cy, cz int) uint64 {
			return cv.reachable[cx+32*cz+1024*cy]
		}
		getConnectivity := func(cx, cy, cz int) uint64 {
			return cv.connectivity[cx+32*cz+1024*cy]
		}

		for n, els := range lines {
			for ho, _ := range els {
				y := len(lines) - n
				conn := getConnectivity(3, y, ho)
				if conn == 0 {
					fmt.Print("#")
					//assert.Equal(t, "#", el)
				} else {
					if getReachable(3, y, ho) != 0 {
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
				reachable := getReachable(3, y, ho)
				if el == "0" {
					assert.Equal(t, uint64(0), reachable, "section should be invisible")
				}
			}
		}
	}
}
