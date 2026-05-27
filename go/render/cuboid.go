package render

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"

	rp "github.com/rmmh/cubeographer/go/resourcepack"
)

type UBOModelEntry struct {
	From       []float32   `json:"from"`
	To         []float32   `json:"to"`
	UVs        [][]float32 `json:"uvs,omitempty"`
	Rotations  []int       `json:"rotations,omitempty"`
	TexIDs     []int       `json:"tex_ids,omitempty"`
	Tint       bool        `json:"tint,omitempty"`
	RotAxis    string      `json:"rot_axis,omitempty"`
	RotAngle   float32     `json:"rot_angle,omitempty"`
	RotOrigin  []float32   `json:"rot_origin,omitempty"`
	RotRescale bool        `json:"rot_rescale,omitempty"`
}

func getModelBounds(model *rp.Model) ([]float32, []float32) {
	if len(model.Elements) == 0 {
		return []float32{0, 0, 0}, []float32{16, 16, 16}
	}
	minX, minY, minZ := 16.0, 16.0, 16.0
	maxX, maxY, maxZ := 0.0, 0.0, 0.0
	for _, el := range model.Elements {
		minX = min(minX, el.From[0])
		minY = min(minY, el.From[1])
		minZ = min(minZ, el.From[2])
		maxX = max(maxX, el.To[0])
		maxY = max(maxY, el.To[1])
		maxZ = max(maxZ, el.To[2])
	}
	return []float32{float32(minX), float32(minY), float32(minZ)},
		[]float32{float32(maxX), float32(maxY), float32(maxZ)}
}

func float32ToFloat16(f float32) uint16 {
	u := math.Float32bits(f)
	sign := uint16((u >> 16) & 0x8000)
	exponent := int((u >> 23) & 0xFF)
	mantissa := u & 0x7FFFFF

	if exponent == 0xFF { // NaN or Inf
		if mantissa != 0 {
			return sign | 0x7E00 // NaN
		}
		return sign | 0x7C00 // Inf
	}

	newExp := exponent - 127 + 15
	if newExp >= 31 { // Overflow to Inf
		return sign | 0x7C00
	}
	if newExp <= 0 { // Underflow to subnormal or 0
		if newExp < -10 {
			return sign // 0
		}
		// Subnormal
		mantissa = mantissa | 0x800000
		shift := uint(14 - newExp)
		return sign | uint16(mantissa>>shift)
	}

	return sign | uint16(newExp<<10) | uint16(mantissa>>13)
}

func writeMetadataValue(buf []uint32, tid int, offset int, val uint32) {
	buf[16*tid+offset] = val
}

func writeCuboidMetadata(buf []uint32, tid int, entry UBOModelEntry) {
	fx := float32ToFloat16(entry.From[0])
	fy := float32ToFloat16(entry.From[1])
	fz := float32ToFloat16(entry.From[2])
	tx := float32ToFloat16(entry.To[0])
	ty := float32ToFloat16(entry.To[1])
	tz := float32ToFloat16(entry.To[2])

	packedRot := uint32(0)
	if entry.Tint {
		packedRot = 1
	}

	// Pack Axis in Bits 1-2
	// 0: none, 1: x, 2: y, 3: z
	axisVal := uint32(0)
	switch entry.RotAxis {
	case "x":
		axisVal = 1
	case "y":
		axisVal = 2
	case "z":
		axisVal = 3
	}
	packedRot |= axisVal << 1

	// Pack Rescale in Bit 3
	if entry.RotRescale {
		packedRot |= 1 << 3
	}

	// Pack Angle in Bits 4-6
	// 0: 0, 1: -22.5, 2: 22.5, 3: -45, 4: 45
	angleVal := uint32(0)
	switch entry.RotAngle {
	case -22.5:
		angleVal = 1
	case 22.5:
		angleVal = 2
	case -45:
		angleVal = 3
	case 45:
		angleVal = 4
	}
	packedRot |= angleVal << 4

	// Pack Origin in Bits 7-30
	if len(entry.RotOrigin) == 3 {
		for i := 0; i < 3; i++ {
			c := entry.RotOrigin[i]
			byteVal := uint32(math.Max(0, math.Min(255, math.Round(float64((c+8.0)*8.0)))))
			packedRot |= byteVal << (7 + i*8)
		}
	}

	writeMetadataValue(buf, tid, 0, uint32(fx)|(uint32(fy)<<16))
	writeMetadataValue(buf, tid, 1, uint32(fz)|(uint32(tx)<<16))
	writeMetadataValue(buf, tid, 2, uint32(ty)|(uint32(tz)<<16))
	writeMetadataValue(buf, tid, 3, packedRot)

	if entry.UVs != nil {
		for f := 0; f < 6; f++ {
			if f < len(entry.UVs) && entry.UVs[f] != nil {
				texId := tid
				if f < len(entry.TexIDs) {
					texId = entry.TexIDs[f]
				}
				tileX := float32(texId % 64)
				tileY := float32(texId / 64)

				uMin := entry.UVs[f][0]/16.0 + tileX
				vMin := entry.UVs[f][1]/16.0 + tileY
				uMax := entry.UVs[f][2]/16.0 + tileX
				vMax := entry.UVs[f][3]/16.0 + tileY

				packedMin := uint32(math.Round(float64(uMin * 256.0))) | (uint32(math.Round(float64(vMin * 256.0))) << 16)
				if f < len(entry.Rotations) {
					rotVal := uint32(entry.Rotations[f]/90) % 4
					packedMin |= rotVal << 14
				}
				packedMax := uint32(math.Round(float64(uMax * 256.0))) | (uint32(math.Round(float64(vMax * 256.0))) << 16)

				writeMetadataValue(buf, tid, 4+2*f, packedMin)
				writeMetadataValue(buf, tid, 4+2*f+1, packedMax)
			}
		}
	}
}

func initCuboidMetadataDefaults(buf []uint32) {
	numEntries := len(buf) / 16
	for i := 0; i < numEntries; i++ {
		fx := float32ToFloat16(0)
		fy := float32ToFloat16(0)
		fz := float32ToFloat16(0)
		tx := float32ToFloat16(16)
		ty := float32ToFloat16(16)
		tz := float32ToFloat16(16)

		writeMetadataValue(buf, i, 0, uint32(fx)|(uint32(fy)<<16))
		writeMetadataValue(buf, i, 1, uint32(fz)|(uint32(tx)<<16))
		writeMetadataValue(buf, i, 2, uint32(ty)|(uint32(tz)<<16))
		writeMetadataValue(buf, i, 3, 0)

		tileX := float32(i % 64)
		tileY := float32(i / 64)

		for f := 0; f < 6; f++ {
			uMin := tileX
			vMin := tileY
			uMax := 1.0 + tileX
			vMax := 1.0 + tileY

			packedMin := uint32(math.Round(float64(uMin * 256.0))) | (uint32(math.Round(float64(vMin * 256.0))) << 16)
			packedMax := uint32(math.Round(float64(uMax * 256.0))) | (uint32(math.Round(float64(vMax * 256.0))) << 16)

			writeMetadataValue(buf, i, 4+2*f, packedMin)
			writeMetadataValue(buf, i, 4+2*f+1, packedMax)
		}
	}
}

func BuildCuboidMetadata(cuboidEntries map[int]UBOModelEntry, cuboidCount int) []byte {
	size := cuboidCount
	if size == 0 {
		size = 1
	}
	metadataBuf := make([]uint32, size*16)
	initCuboidMetadataDefaults(metadataBuf)

	// Overwrite with active cuboids
	for tid, entry := range cuboidEntries {
		if tid < size {
			writeCuboidMetadata(metadataBuf, tid, entry)
		}
	}

	// Convert to Little Endian bytes
	byteBuf := make([]byte, len(metadataBuf)*4)
	for i, val := range metadataBuf {
		binary.LittleEndian.PutUint32(byteBuf[i*4:], val)
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(byteBuf); err != nil {
		panic(err)
	}
	if err := gw.Close(); err != nil {
		panic(err)
	}

	return buf.Bytes()
}
