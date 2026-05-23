package render

import (
	"fmt"
	"image"
	"math"
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
func accumulateColor(texName string, textures map[string]image.Image, tint bool, totr, totg, totb, pixelCount int) (int, int, int, int) {
	if texName == "" {
		return totr, totg, totb, pixelCount
	}
	tex, ok := textures[texName]
	if !ok {
		return totr, totg, totb, pixelCount
	}
	r, g, b, _ := GetAverageColor(tex)
	tr, tg, tb := int(r), int(g), int(b)
	if tint {
		tr = int(float32(tr) * 0.4)
		tg = int(float32(tg) * 0.73)
		tb = int(float32(tb) * 0.27)
	}
	return totr + tr, totg + tg, totb + tb, pixelCount + 1
}

func getCubeTexture(tmpl ModelEntry, face int, compIdx int, mask uint32) string {
	hasSideSpecial := (mask & (1 << 30)) != 0
	if !hasSideSpecial {
		if len(tmpl.Textures) > 0 {
			return tmpl.Textures[0]
		}
		return ""
	}

	if face >= 4 {
		switch len(tmpl.Textures) {
		case 4:
			if face == 4 {
				return tmpl.Textures[3]
			}
			return tmpl.Textures[1]
		case 3:
			if face == 4 {
				return tmpl.Textures[1]
			}
			return tmpl.Textures[2]
		case 2:
			return tmpl.Textures[1]
		}
	} else {
		baseTexIdx := 2 * compIdx
		if baseTexIdx < len(tmpl.Textures) {
			return tmpl.Textures[baseTexIdx]
		}
	}
	return ""
}

func (be *BlockEntry) updateColors(textures map[string]image.Image) {
	be.Colors = make([]string, len(be.Templates))
	for i, tmpls := range be.Templates {
		if len(tmpls) == 0 {
			continue
		}

		var faceColors [6]string
		allFacesSame := true
		var firstFaceColor string

		for face := 0; face < 6; face++ {
			var totr, totg, totb int
			pixelCount := 0

			// Find all active textures and tint for this face
			for _, tmpl := range tmpls {
				if tmpl.Layer == LayerCube {
					for compIdx := 0; compIdx < len(tmpl.Template)/2; compIdx++ {
						k := compIdx * 2
						mask := tmpl.Template[k+1]
						if (mask & (1 << face)) != 0 {
							texName := getCubeTexture(tmpl, face, compIdx, mask)
							totr, totg, totb, pixelCount = accumulateColor(texName, textures, (mask&(1<<31)) != 0, totr, totg, totb, pixelCount)
						}
					}
				} else if tmpl.Layer == LayerVoxel {
					for compIdx, texName := range tmpl.Textures {
						k := compIdx * 2
						if k+1 < len(tmpl.Template) {
							mask := tmpl.Template[k+1]
							if (mask & (1 << face)) != 0 {
								totr, totg, totb, pixelCount = accumulateColor(texName, textures, (mask&(1<<31)) != 0, totr, totg, totb, pixelCount)
							}
						}
					}
				} else if tmpl.Layer == LayerCuboid {
					mask := tmpl.Template[1]
					if (mask & (1 << face)) != 0 {
						if face < len(tmpl.Textures) {
							texName := tmpl.Textures[face]
							totr, totg, totb, pixelCount = accumulateColor(texName, textures, (mask&(1<<31)) != 0, totr, totg, totb, pixelCount)
						}
					}
				} else {
					// LayerCubeFallback, LayerCross, LayerCrop
					if len(tmpl.Textures) > 0 {
						texName := tmpl.Textures[0]
						mask := uint32(0)
						if len(tmpl.Template) >= 2 {
							mask = tmpl.Template[1]
						}
						totr, totg, totb, pixelCount = accumulateColor(texName, textures, (mask&(1<<31)) != 0, totr, totg, totb, pixelCount)
					}
				}
			}

			if pixelCount == 0 {
				// Fallback: search for ANY texture in the entire tmpls list
				for _, tmpl := range tmpls {
					for _, texName := range tmpl.Textures {
						prevCount := pixelCount
						tint := false
						for k := 1; k < len(tmpl.Template); k += 2 {
							if (tmpl.Template[k] & (1 << 31)) != 0 {
								tint = true
								break
							}
						}
						totr, totg, totb, pixelCount = accumulateColor(texName, textures, tint, totr, totg, totb, pixelCount)
						if pixelCount > prevCount {
							break
						}
					}
					if pixelCount > 0 {
						break
					}
				}
			}

			if pixelCount == 0 {
				pixelCount = 1
			}

			totr /= pixelCount
			totg /= pixelCount
			totb /= pixelCount

			hexColor := fmt.Sprintf("%02x%02x%02x", uint8(totr), uint8(totg), uint8(totb))
			faceColors[face] = hexColor

			if face == 0 {
				firstFaceColor = hexColor
			} else if hexColor != firstFaceColor {
				allFacesSame = false
			}
		}

		if allFacesSame {
			be.Colors[i] = firstFaceColor
		} else {
			be.Colors[i] = fmt.Sprintf("%s,%s,%s,%s,%s,%s",
				faceColors[0], faceColors[1], faceColors[2],
				faceColors[3], faceColors[4], faceColors[5])
		}
	}
}
