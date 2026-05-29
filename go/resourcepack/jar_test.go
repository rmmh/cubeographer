package resourcepack

import (
	"archive/zip"
	"encoding/json"
	"testing"

	"github.com/rmmh/cubeographer/go/jvm"
	"github.com/stretchr/testify/require"
)

func TestBlockstateUnmarshal(t *testing.T) {
	var b BlockState
	err := json.Unmarshal([]byte(`
	{
  "multipart": [
    {
      "apply": {
        "model": "minecraft:block/acacia_shelf"
      },
      "when": {
        "facing": "north"
      }
    },
    {
      "apply": {
        "model": "minecraft:block/acacia_shelf_unpowered"
      },
      "when": {
        "AND": [
          {
            "facing": "north"
          },
          {
            "powered": "false"
          }
        ]
      }
    }
  ]
}`), &b)
	require.NoError(t, err)
}

func TestJarFromZip(t *testing.T) {
	t.Parallel()
	for _, ver := range MajorMCVersions {
		t.Run(ver, func(t *testing.T) {
			t.Parallel()
			jarPath := "testdata/minecraft-" + ver + ".jar"
			err := DownloadMinecraftJar(jarPath, ver)
			if err != nil {
				t.Fatal(err)
			}
			jar, err := zip.OpenReader(jarPath)
			if err != nil {
				t.Fatal(err)
			}
			defer jar.Close()
			j, err := ExtractRenderData(jar)
			if err != nil {
				t.Fatal(err)
			}
			if len(j.Textures) == 0 {
				t.Errorf("no textures found in jar")
			}
			if len(j.Models) == 0 {
				t.Errorf("no models found in jar")
			}
			if len(j.BlockStates) == 0 {
				t.Errorf("no blockstates found in jar")
			}
		})
	}
}

func TestExtractWaterloggableBlocks(t *testing.T) {
	for _, ver := range MajorMCVersions {
		t.Run(ver, func(t *testing.T) {
			jarPath := "testdata/minecraft-" + ver + ".jar"
			err := DownloadMinecraftJar(jarPath, ver)
			require.NoError(t, err)

			jar, err := zip.OpenReader(jarPath)
			require.NoError(t, err)
			defer jar.Close()

			blocksClass, blockClass, registerMethod, _, err := DetectRegistryMeta(jar)
			if err != nil {
				t.Skipf("Skipping version %s: no block registry detected (%v)", ver, err)
			}

			t.Logf("Detected block registry for %s: Blocks=%s, Block=%s, registerMethod=%s", ver, blocksClass, blockClass, registerMethod)

			provider := jvm.NewZipClassProvider(jar)
			interp := jvm.NewInterpreter(provider)
			interp.BlocksClassName = blocksClass
			interp.BlockClassName = blockClass
			interp.RegisterMethodName = registerMethod

			err = interp.InterpretClassClinit(blocksClass)
			require.NoError(t, err)
			t.Logf("Successfully interpreted block registry for %s: found %d blocks", ver, len(interp.Blocks))
			waterloggableBlocks, err := FindWaterloggableBlocks(provider, interp.Blocks)
			if err != nil {
				t.Skipf("Skipping version %s: %v", ver, err)
			}

			if len(waterloggableBlocks) > 10 {
				t.Logf("Discovered %d waterloggable blocks for version %s (showing 10): %v ... and %d more", len(waterloggableBlocks), ver, waterloggableBlocks[:10], len(waterloggableBlocks)-10)
			} else {
				t.Logf("Discovered %d waterloggable blocks for version %s: %v", len(waterloggableBlocks), ver, waterloggableBlocks)
			}
		})
	}
}
