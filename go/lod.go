package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path"

	"github.com/rmmh/cubeographer/go/render"
)

func computeRegionLODs(rs *regionState, conf *scanRegionConfig) error {
	W := 256
	H := 160
	D := 256

	air := rs.bm.NameToNid["minecraft:air"]

	blocks := make([]uint16, W*H*D)
	states := make([]render.Stateval, W*H*D)
	for x := 0; x < W; x++ {
		for y := 0; y < H; y++ {
			for z := 0; z < D; z++ {
			voxelScan:
				for oy := 1; oy >= 0; oy-- {
					for oz := 0; oz < 2; oz++ {
						for ox := 0; ox < 2; ox++ {
							b, state, _, _ := rs.get(2*x+ox, 2*y+oy+1, 2*z+oz)
							if b != 0 && b != air {
								blocks[x+y*W+z*W*H] = b
								states[x+y*W+z*W*H] = state
								break voxelScan
							}
						}
					}
				}
			}
		}
	}

	getSideColor := func(b uint16, state render.Stateval, side int) color.RGBA {
		if b == 0 {
			return color.RGBA{0, 0, 0, 0}
		}
		var cList []color.RGBA
		if int(b) < len(rs.bm.Colors) {
			sList := rs.bm.Colors[b]
			if int(state) < len(sList) {
				cList = sList[state]
			} else if len(sList) > 0 {
				cList = sList[0]
			}
		}
		if len(cList) == 6 {
			return cList[side]
		}
		if len(cList) > 0 {
			return cList[0]
		}
		return color.RGBA{0, 0, 0, 0}
	}

	topColorImg := image.NewRGBA(image.Rect(0, 0, W, D))
	topDepthImg := image.NewGray(image.Rect(0, 0, W, D))

	northColorImg := image.NewRGBA(image.Rect(0, 0, W, H))
	northDepthImg := image.NewGray(image.Rect(0, 0, W, H))

	southColorImg := image.NewRGBA(image.Rect(0, 0, W, H))
	southDepthImg := image.NewGray(image.Rect(0, 0, W, H))

	eastColorImg := image.NewRGBA(image.Rect(0, 0, D, H))
	eastDepthImg := image.NewGray(image.Rect(0, 0, D, H))

	westColorImg := image.NewRGBA(image.Rect(0, 0, D, H))
	westDepthImg := image.NewGray(image.Rect(0, 0, D, H))

	water := rs.bm.NameToNid["minecraft:water"]

	for u := 0; u < W; u++ {
		for v := 0; v < D; v++ {
			hit := false
			for t := H - 1; t >= 0; t-- {
				idx := u + t*W + v*W*H
				b := blocks[idx]
				// TODO: do the same checks as visibility
				// probably need to handle cave_air, maybe scan for solid or do transparent
				// composition?
				if b != 0 && b != air {
					state := states[idx]
					col := getSideColor(b, state, 4) // Up face is 4
					if col.A == 0 {
						continue
					}
					if b == water {
						// return vec3(0.2, 0.4, 0.93);
						col = color.RGBA{51, 102, 237, 255}
					}
					topColorImg.SetRGBA(u, v, col)
					depthVal := int(math.Round(float64(t+1) / float64(H) * 255.0))
					if depthVal >= 255 {
						depthVal = 254
					}
					topDepthImg.SetGray(u, v, color.Gray{uint8(depthVal)})
					hit = true
					break
				}
			}
			if !hit {
				topDepthImg.SetGray(u, v, color.Gray{255})
			}
		}
	}

	projectSide := func(width, height int, tStart, tEnd, tStep int, side int, getCoords func(u, v, t int) (int, int, int), getDepthVal func(x, y, z int) int, colorImg *image.RGBA, depthImg *image.Gray) {
		for u := 0; u < width; u++ {
			for v := 0; v < height; v++ {
				hit := false
				for t := tStart; t != tEnd; t += tStep {
					x, y, z := getCoords(u, v, t)
					idx := x + y*W + z*W*H
					b := blocks[idx]
					if b != 0 && b != air {
						state := states[idx]
						col := getSideColor(b, state, side)
						colorImg.SetRGBA(u, height-1-v, col)
						depthVal := min(254, getDepthVal(x, y, z))
						depthImg.SetGray(u, height-1-v, color.Gray{uint8(depthVal)})
						hit = true
						break
					}
				}
				if !hit {
					depthImg.SetGray(u, height-1-v, color.Gray{255})
				}
			}
		}
	}

	// North view: ray travels along -Z (t goes from D-1 down to 0). It hits the South face (2) first.
	projectSide(W, H, D-1, -1, -1, 2,
		func(u, v, t int) (int, int, int) { return u, v, t },
		func(x, y, z int) int { return int(math.Round(float64(z+1) / float64(D) * 255.0)) },
		northColorImg, northDepthImg)

	// South view: ray travels along +Z (t goes from 0 to D-1). It hits the North face (3) first.
	projectSide(W, H, 0, D, 1, 3,
		func(u, v, t int) (int, int, int) { return u, v, t },
		func(x, y, z int) int { return int(math.Round(float64(z) / float64(D) * 255.0)) },
		southColorImg, southDepthImg)

	// East view: ray travels along -X (t goes from W-1 down to 0). It hits the East face (1) first.
	projectSide(D, H, W-1, -1, -1, 1,
		func(u, v, t int) (int, int, int) { return t, v, u },
		func(x, y, z int) int { return int(math.Round(float64(x+1) / float64(W) * 255.0)) },
		eastColorImg, eastDepthImg)

	// West view: ray travels along +X (t goes from 0 to W-1). It hits the West face (0) first.
	projectSide(D, H, 0, W, 1, 0,
		func(u, v, t int) (int, int, int) { return t, v, u },
		func(x, y, z int) int { return int(math.Round(float64(x) / float64(W) * 255.0)) },
		westColorImg, westDepthImg)

	tilesDir := path.Join(conf.outdir, "tiles")
	lodsDir := path.Join(conf.outdir, "lods")
	os.MkdirAll(tilesDir, 0755)
	os.MkdirAll(lodsDir, 0755)

	topColorPath := path.Join(tilesDir, fmt.Sprintf("r.%d.%d.png", rs.rx, rs.rz))
	fTop, err := os.Create(topColorPath)
	if err != nil {
		return err
	}
	defer fTop.Close()
	err = png.Encode(fTop, topColorImg)
	if err != nil {
		return err
	}

	type assetInfo struct {
		id  byte
		img image.Image
	}

	assets := []assetInfo{
		{0, topDepthImg},
		{1, northColorImg},
		{2, northDepthImg},
		{3, southColorImg},
		{4, southDepthImg},
		{5, eastColorImg},
		{6, eastDepthImg},
		{7, westColorImg},
		{8, westDepthImg},
	}

	if conf.mode != "tile" {
		binPath := path.Join(lodsDir, fmt.Sprintf("r.%d.%d.bin", rs.rx, rs.rz))
		fBin, err := os.Create(binPath)
		if err != nil {
			return err
		}
		defer fBin.Close()

		for _, asset := range assets {
			var imgBuf bytes.Buffer
			err = png.Encode(&imgBuf, asset.img)
			if err != nil {
				return err
			}

			dataBytes := imgBuf.Bytes()
			_, err = fBin.Write([]byte{asset.id})
			if err != nil {
				return err
			}
			lenBuf := make([]byte, 4)
			binary.LittleEndian.PutUint32(lenBuf, uint32(len(dataBytes)))
			_, err = fBin.Write(lenBuf)
			if err != nil {
				return err
			}
			_, err = fBin.Write(dataBytes)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
