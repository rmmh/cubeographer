// simple ZVCR loader
// see https://github.com/2b2tplace/zvcr for official

package zvcr

import (
	"bytes"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/rmmh/cubeographer/go/region"
	"github.com/rmmh/cubeographer/go/render"
)

//go:embed data/*.zstd
var embeddedData embed.FS

type protocolMapping struct {
	nid    []uint16
	nstate []render.Stateval
}

var (
	decompressedCache   = make(map[uint16][]string)
	decompressedCacheMu sync.Mutex

	mappings   = make(map[uint16]*protocolMapping)
	mappingsMu sync.Mutex
)

func getDecompressedNames(protocolVersion uint16) ([]string, error) {
	decompressedCacheMu.Lock()
	defer decompressedCacheMu.Unlock()

	if names, ok := decompressedCache[protocolVersion]; ok {
		return names, nil
	}

	fileName := fmt.Sprintf("data/blockstates%d.txt.zstd", protocolVersion)
	compressedBytes, err := embeddedData.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("blockstates for protocol version %d not available: %w", protocolVersion, err)
	}

	zr, err := zstd.NewReader(bytes.NewReader(compressedBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd reader: %w", err)
	}
	defer zr.Close()

	decompressedBytes, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress blockstates: %w", err)
	}

	lines := strings.Split(string(decompressedBytes), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	decompressedCache[protocolVersion] = lines
	return lines, nil
}

func getProtocolMapping(protocolVersion uint16, bm *region.BlockMapper) (*protocolMapping, error) {
	mappingsMu.Lock()
	defer mappingsMu.Unlock()

	if pm, ok := mappings[protocolVersion]; ok {
		return pm, nil
	}

	names, err := getDecompressedNames(protocolVersion)
	if err != nil {
		return nil, err
	}

	pm := &protocolMapping{
		nid:    make([]uint16, len(names)),
		nstate: make([]render.Stateval, len(names)),
	}

	for i, name := range names {
		if name == "" {
			continue
		}

		blockName := name
		var props []string
		if idx := strings.IndexByte(name, '['); idx != -1 {
			blockName = name[:idx]
			propsStr := name[idx+1:]
			if len(propsStr) > 0 && propsStr[len(propsStr)-1] == ']' {
				propsStr = propsStr[:len(propsStr)-1]
			}
			if len(propsStr) > 0 {
				props = strings.Split(propsStr, ",")
			}
		}

		if !strings.HasPrefix(blockName, "minecraft:") {
			blockName = "minecraft:" + blockName
		}

		nid, ok := bm.NameToNid[blockName]
		if !ok {
			pm.nid[i] = 0
			pm.nstate[i] = 0
			continue
		}

		pm.nid[i] = nid
		pm.nstate[i] = bm.GetStateval(nid, props)
	}

	mappings[protocolVersion] = pm
	return pm, nil
}

type reader struct {
	data []byte
	off  int
}

func (r *reader) readByte() byte {
	if r.off >= len(r.data) {
		return 0
	}
	b := r.data[r.off]
	r.off++
	return b
}

func (r *reader) readUint16() uint16 {
	if r.off+2 > len(r.data) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.data[r.off : r.off+2])
	r.off += 2
	return v
}

func (r *reader) readUint32() uint32 {
	if r.off+4 > len(r.data) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.data[r.off : r.off+4])
	r.off += 4
	return v
}

func (r *reader) readUint64() uint64 {
	if r.off+8 > len(r.data) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.data[r.off : r.off+8])
	r.off += 8
	return v
}

func (r *reader) skip(n int) {
	if r.off+n > len(r.data) {
		r.off = len(r.data)
		return
	}
	r.off += n
}

func getBitsPerIndex(paletteSize int) int {
	if paletteSize <= 1 {
		return 1
	}
	bits := 0
	val := paletteSize - 1
	for val > 0 {
		bits++
		val >>= 1
	}
	if bits < 1 {
		return 1
	}
	return bits
}

func unpack(packedData []uint64, bits int, snapshotLength int, palette []uint16, direct bool) []uint16 {
	unpacked := make([]uint16, snapshotLength)
	valuesPerLong := 64 / bits
	mask := uint64(1<<bits) - 1
	usableBits := 64 - bits

	for cellIdx := 0; cellIdx < len(packedData); cellIdx++ {
		cell := packedData[cellIdx]
		i := cellIdx * valuesPerLong
		for bitIndex := 0; bitIndex <= usableBits && i < snapshotLength; bitIndex += bits {
			slice := (cell >> bitIndex) & mask
			if !direct {
				if int(slice) < len(palette) {
					unpacked[i] = palette[slice]
				} else {
					unpacked[i] = 0
				}
			} else {
				unpacked[i] = uint16(slice)
			}
			i++
		}
	}
	return unpacked
}

func getTotalTileEntities(protocolVersion uint16) int {
	if protocolVersion >= 768 {
		return 45
	}
	if protocolVersion >= 766 {
		return 44
	}
	if protocolVersion >= 765 {
		return 41
	}
	return 0
}

func ReadZVCR(filePath string, bm *region.BlockMapper, wanted []int) ([]region.ChunkDatum, error) {
	cdata := make([]region.ChunkDatum, 1024)

	var wantedChunks [1024]bool
	if len(wanted) > 0 {
		for _, w := range wanted {
			if w >= 0 && w < 1024 {
				wantedChunks[w] = true
			}
		}
	} else {
		for i := range wantedChunks {
			wantedChunks[i] = true
		}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return cdata, err
	}
	defer f.Close()

	decoder, err := zstd.NewReader(f)
	if err != nil {
		return cdata, err
	}
	defer decoder.Close()

	decompressed, err := io.ReadAll(decoder)
	if err != nil {
		return cdata, err
	}

	if len(decompressed) < 10 {
		return cdata, errors.New("decompressed file too small")
	}

	r := &reader{data: decompressed, off: 0}

	// 1. Magic prefix
	prefix := string(r.data[r.off : r.off+8])
	if prefix != "ZVRegion" {
		return cdata, fmt.Errorf("invalid ZVRegion prefix: %q", prefix)
	}
	r.skip(8)

	// 2. Version
	version := r.readByte()
	if version > 6 { // ZVCR3_VER_LATEST is 6
		return cdata, fmt.Errorf("unsupported ZVCR3 version: %d", version)
	}

	supportBiomes := version >= 2
	supportDynamicVersioning := version >= 3
	supportSingleValuePalette := version >= 5
	supportTileEntities := version >= 6

	// 3. Dimension Type
	dimType := r.readByte()
	if dimType > 2 {
		return cdata, fmt.Errorf("invalid dimension type: %d", dimType)
	}

	var sectionCount int
	var minY int
	switch dimType {
	case 0: // Overworld
		sectionCount = 24
		minY = -64
	case 1: // Nether
		sectionCount = 16
		minY = 0
	case 2: // End
		sectionCount = 16
		minY = 0
	}

	// 4. Protocol Version
	var protocolVersion uint16
	if supportDynamicVersioning {
		protocolVersion = r.readUint16()
	} else if version == 1 {
		protocolVersion = 765
	}

	var pm *protocolMapping
	if protocolVersion > 0 {
		var err error
		pm, err = getProtocolMapping(protocolVersion, bm)
		if err != nil {
			log.Fatalf("Error getting protocol mapping for version %d: %v", protocolVersion, err)
		}
	}

	// 5. Palette Table
	paletteTableLen := r.readUint32()
	paletteTable := make([][]uint16, paletteTableLen)

	for i := 0; i < int(paletteTableLen); i++ {
		palLen := r.readUint16()
		if palLen > 256 { // direct palette mode
			r.skip(int(palLen) * 2)
			paletteTable[i] = nil
		} else {
			atoms := make([]uint16, palLen)
			for j := 0; j < int(palLen); j++ {
				atoms[j] = r.readUint16()
			}
			paletteTable[i] = atoms
		}
	}

	// Unpack helper
	unpackSnapshot := func(snapshotLength int) ([]uint16, error) {
		_ = r.readUint64() // timestamp

		if supportSingleValuePalette {
			dataType := r.readByte()
			if dataType == 0 {
				singleValue := r.readUint16()
				res := make([]uint16, snapshotLength)
				for k := range res {
					res[k] = singleValue
				}
				return res, nil
			}
		}

		packedLength := r.readUint64()
		packedData := make([]uint64, packedLength)
		for k := 0; k < int(packedLength); k++ {
			packedData[k] = r.readUint64()
		}

		paletteIndex := r.readUint32()

		if paletteIndex == 0xffffffff {
			return unpack(packedData, 16, snapshotLength, nil, true), nil
		}

		if int(paletteIndex) >= len(paletteTable) {
			return nil, fmt.Errorf("palette index out of bounds: %d", paletteIndex)
		}

		palette := paletteTable[paletteIndex]
		if palette == nil {
			return unpack(packedData, 16, snapshotLength, nil, true), nil
		}

		if len(palette) == 1 {
			singleValue := palette[0]
			res := make([]uint16, snapshotLength)
			for k := range res {
				res[k] = singleValue
			}
			return res, nil
		}

		bits := getBitsPerIndex(len(palette))
		return unpack(packedData, bits, snapshotLength, palette, false), nil
	}

	// Skip helper
	skipSnapshot := func() {
		_ = r.readUint64() // timestamp

		if supportSingleValuePalette {
			dataType := r.readByte()
			if dataType == 0 {
				r.skip(2)
				return
			}
		}

		packedLength := r.readUint64()
		r.skip(int(packedLength) * 8)
		r.skip(4) // palette index
	}

	// 6. Region container segments
	for segmentIndex := 0; segmentIndex < 1024; segmentIndex++ {
		hasSegment := r.readByte()
		if hasSegment == 0 {
			continue
		}

		cx := segmentIndex / 32
		cz := segmentIndex % 32
		chunkNum := cx + cz*32

		isWanted := wantedChunks[chunkNum]

		var nblocks [][]uint16
		var nstates [][]render.Stateval
		minYSections := minY / 16
		nonNegativeSections := sectionCount + minYSections

		if isWanted {
			nblocks = make([][]uint16, nonNegativeSections)
			nstates = make([][]render.Stateval, nonNegativeSections)
		}

		// Block sections
		for sectionIdx := 0; sectionIdx < sectionCount; sectionIdx++ {
			deltaLength := r.readUint64()
			var unpackedBlocks []uint16
			var err error

			for dIdx := 0; dIdx < int(deltaLength); dIdx++ {
				if isWanted && dIdx == 0 {
					unpackedBlocks, err = unpackSnapshot(4096)
					if err != nil {
						return cdata, err
					}
				} else {
					skipSnapshot()
				}
			}

			if isWanted {
				sectionY := sectionIdx + minYSections
				if sectionY >= 0 && sectionY < nonNegativeSections && len(unpackedBlocks) == 4096 {
					nb := make([]uint16, 4096)
					ns := make([]render.Stateval, 4096)
					for i, atom := range unpackedBlocks {
						if pm != nil {
							if int(atom) < len(pm.nid) {
								nb[i] = pm.nid[atom]
								ns[i] = pm.nstate[atom]
							} else {
								nb[i] = 0
								ns[i] = 0
							}
						} else {
							nb[i] = atom >> 5
							ns[i] = render.Stateval(atom & 31)
						}
					}
					nblocks[sectionY] = nb
					nstates[sectionY] = ns
				}
			}
		}

		// Biome sections
		if supportBiomes {
			for sectionIdx := 0; sectionIdx < sectionCount; sectionIdx++ {
				deltaLength := r.readUint64()
				for dIdx := 0; dIdx < int(deltaLength); dIdx++ {
					skipSnapshot()
				}
			}
		}

		// Segment Info
		statesLength := r.readUint64()
		r.skip(int(statesLength) * 9)

		tileEntitiesLength := r.readUint64()
		totalTE := getTotalTileEntities(protocolVersion)
		r.skip(int(tileEntitiesLength) * (totalTE*2 + 8))

		// Tile Entities
		if supportTileEntities {
			tileEntityDeltasLength := r.readUint64()
			for dIdx := 0; dIdx < int(tileEntityDeltasLength); dIdx++ {
				_ = r.readUint64()
				listLen := r.readUint64()
				for lIdx := 0; lIdx < int(listLen); lIdx++ {
					_ = r.readUint32()
					op := r.readByte()
					if op != 0 {
						_ = r.readUint32()
						nbtLen := r.readUint64()
						r.skip(int(nbtLen))
					}
				}
			}
		}

		if isWanted {
			cdata[chunkNum] = region.ChunkDatum{
				Blocks:     nblocks,
				BlockState: nstates,
				Lights:     nil,
				LightsSky:  nil,
			}
		}
	}

	return cdata, nil
}
