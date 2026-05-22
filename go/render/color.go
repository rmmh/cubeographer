package render

import (
	"fmt"
	"image"
	"math"

	rp "github.com/rmmh/cubeographer/go/resourcepack"
)

func GetAverageColor(img image.Image) (r, g, b, a uint8) {
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	totalPixels := int64(width * height)

	if totalPixels == 0 {
		return 0, 0, 0, 0
	}

	var sumR, sumG, sumB, sumA float64

	// Loop through every pixel in the image grid
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// At() returns color.Color interface
			colorRGBA := img.At(x, y)

			// RGBA returns values pre-multiplied by alpha in the range [0, 65535]
			r16, g16, b16, a16 := colorRGBA.RGBA()

			// Convert 16-bit color components to standard 8-bit [0, 255]
			r8 := float64(r16 >> 8)
			g8 := float64(g16 >> 8)
			b8 := float64(b16 >> 8)
			a8 := float64(a16 >> 8)

			// Accumulate squares for perceived brightness accuracy
			sumR += r8 * r8
			sumG += g8 * g8
			sumB += b8 * b8
			sumA += a8
		}
	}

	// Calculate final averages using square root
	avgR := uint8(math.Round(math.Sqrt(sumR / float64(totalPixels))))
	avgG := uint8(math.Round(math.Sqrt(sumG / float64(totalPixels))))
	avgB := uint8(math.Round(math.Sqrt(sumB / float64(totalPixels))))
	avgA := uint8(math.Round(sumA / float64(totalPixels)))

	return avgR, avgG, avgB, avgA
}

func (be *BlockEntry) updateColors(textures map[string]image.Image) {
	be.Colors = make([]string, len(be.Templates))
	name := rp.RemoveDefaultPrefix(be.Name)
	for i, tmpls := range be.Templates {
		if len(tmpls) == 0 {
			continue
		}
		var totr, totg, totb int
		pixelCount := 0
		tint := false
		if name == "grass" || name == "grass_block" {
			// TODO: only tint the top side of grass and partial (?) on side
			tint = true
		}
		for _, tmpl := range tmpls {
			if tmpl.Layer != LayerCuboid {
				for k := 1; k < len(tmpl.Template); k += 2 {
					if (tmpl.Template[k] & (1 << 31)) != 0 {
						tint = true
					}
				}
			}
			for _, texName := range tmpl.Textures {
				tex, ok := textures[texName]
				if !ok {
					continue
				}
				r, g, b, _ := GetAverageColor(tex)
				totr += int(r)
				totg += int(g)
				totb += int(b)
				pixelCount++
			}
		}
		if pixelCount == 0 {
			pixelCount = 1
		}

		if tint {
			totr = int(float32(totr) * 0.4)
			totg = int(float32(totg) * 0.73)
			totb = int(float32(totb) * 0.27)
		}

		totr /= pixelCount
		totg /= pixelCount
		totb /= pixelCount

		be.Colors[i] = fmt.Sprintf("%02x%02x%02x", uint8(totr), uint8(totg), uint8(totb))
	}
}
