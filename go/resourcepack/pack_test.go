package resourcepack

import (
	"archive/zip"
	"bytes"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourcePackSliceFlag(t *testing.T) {
	var slice ResourcePackSlice
	err := slice.Set("pack1.zip")
	require.NoError(t, err)
	err = slice.Set("pack2/")
	require.NoError(t, err)

	assert.Equal(t, "pack1.zip, pack2/", slice.String())
	assert.Equal(t, []string{"pack1.zip", "pack2/"}, []string(slice))
}

func TestFlagParsingIntegration(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var slice ResourcePackSlice
	fs.Var(&slice, "resourcepack", "test pack flag")

	err := fs.Parse([]string{"-resourcepack", "my_pack1.zip", "-resourcepack", "my_pack2/"})
	require.NoError(t, err)

	assert.Equal(t, []string{"my_pack1.zip", "my_pack2/"}, []string(slice))
}

func TestOverlayPack(t *testing.T) {
	// Create base ResourceJar
	rj := &ResourceJar{
		Blocks:      map[string]*Block{},
		BlockStates: map[string]*BlockState{},
		Models:      map[string]*Model{},
		Textures:    map[string]image.Image{},
		Translations: map[string]string{
			"key.existing": "Old Translation",
		},
		WorldVersion: 3400, // mock a newer version (1.20)
	}

	tempDir := t.TempDir()

	// 1. Prepare dummy data
	// 1.1. Dummy PNG
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var pngBuf bytes.Buffer
	err := png.Encode(&pngBuf, img)
	require.NoError(t, err)
	pngBytes := pngBuf.Bytes()

	// 1.2. Dummy JSON files
	modelJSON := []byte(`{"parent":"block/cube_all"}`)
	blockstateJSON := []byte(`{"variants":{"":{"model":"minecraft:block/stone"}}}`)
	langJSON := []byte(`{"key.new":"New Translation","key.existing":"Overridden Translation"}`)

	// 2. Test Overlay from Directory
	dirPack := filepath.Join(tempDir, "dir_pack")
	err = os.MkdirAll(filepath.Join(dirPack, "assets/minecraft/textures/block"), 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(dirPack, "assets/minecraft/models/block"), 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(dirPack, "assets/minecraft/blockstates"), 0755)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(dirPack, "assets/minecraft/lang"), 0755)
	require.NoError(t, err)

	// Write pack.mcmeta with pack_format: 6 (represents 1.16, which has soul_fire_torch)
	mcmetaJSON := []byte(`{"pack":{"pack_format":6,"description":"Test Pack"}}`)
	err = os.WriteFile(filepath.Join(dirPack, "pack.mcmeta"), mcmetaJSON, 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/textures/block/stone.png"), pngBytes, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/models/block/stone.json"), modelJSON, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/blockstates/stone.json"), blockstateJSON, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/lang/en_us.json"), langJSON, 0644)
	require.NoError(t, err)

	// Write files that should be migrated (soul_fire_torch -> soul_torch)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/textures/block/soul_fire_torch.png"), pngBytes, 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/models/block/soul_fire_torch.json"), []byte(`{"parent":"block/soul_fire_torch"}`), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/blockstates/soul_fire_torch.json"), []byte(`{"variants":{"":{"model":"minecraft:block/soul_fire_torch"}}}`), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dirPack, "assets/minecraft/lang/en_gb.json"), []byte(`{"block.minecraft.soul_fire_torch":"Soul Fire Torch"}`), 0644)
	require.NoError(t, err)

	err = rj.OverlayPack(dirPack)
	require.NoError(t, err)

	// Verify directory overlay values
	require.Contains(t, rj.Textures, "block/stone")
	require.Contains(t, rj.Models, "minecraft:block/stone")
	require.Contains(t, rj.BlockStates, "minecraft:stone")
	require.Equal(t, "block/cube_all", rj.Models["minecraft:block/stone"].Parent)
	require.Equal(t, "New Translation", rj.Translations["key.new"])
	require.Equal(t, "Overridden Translation", rj.Translations["key.existing"])

	// Verify migrated directory overlay values
	require.Contains(t, rj.Textures, "block/soul_torch")
	require.NotContains(t, rj.Textures, "block/soul_fire_torch")
	require.Contains(t, rj.Models, "minecraft:block/soul_torch")
	require.NotContains(t, rj.Models, "minecraft:block/soul_fire_torch")
	require.Equal(t, "block/soul_torch", rj.Models["minecraft:block/soul_torch"].Parent)
	require.Contains(t, rj.BlockStates, "minecraft:soul_torch")
	require.NotContains(t, rj.BlockStates, "minecraft:soul_fire_torch")
	require.Equal(t, "minecraft:block/soul_torch", rj.BlockStates["minecraft:soul_torch"].Variants[""][0].Model)
	require.Contains(t, rj.Translations, "block.minecraft.soul_torch")
	require.NotContains(t, rj.Translations, "block.minecraft.soul_fire_torch")

	// 3. Test Overlay from ZIP
	zipPath := filepath.Join(tempDir, "zip_pack.zip")
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)
	defer zipFile.Close()

	zw := zip.NewWriter(zipFile)

	// Create zip files (also format 6)
	filesToZip := map[string][]byte{
		"pack.mcmeta": mcmetaJSON,
		"assets/minecraft/textures/block/grass_block_top.png": pngBytes,
		"assets/minecraft/models/block/grass_block.json":      []byte(`{"parent":"block/cube_column"}`),
		"assets/minecraft/blockstates/grass_block.json":       []byte(`{"variants":{"":{"model":"minecraft:block/grass_block"}}}`),
		"assets/minecraft/lang/en_gb.json":                    []byte(`{"key.gb":"GB Translation"}`),
	}

	for name, data := range filesToZip {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	err = zw.Close()
	require.NoError(t, err)

	// Re-open and check overlay
	err = rj.OverlayPack(zipPath)
	require.NoError(t, err)

	// Verify ZIP overlay values
	require.Contains(t, rj.Textures, "block/grass_block_top")
	require.Contains(t, rj.Models, "minecraft:block/grass_block")
	require.Contains(t, rj.BlockStates, "minecraft:grass_block")
	require.Equal(t, "block/cube_column", rj.Models["minecraft:block/grass_block"].Parent)
	require.Equal(t, "GB Translation", rj.Translations["key.gb"])
}
