package resourcepack

import (
	"archive/zip"
	"encoding/json"
	"testing"

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
			j, err := JarFromZip(jar)
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
