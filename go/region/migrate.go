package region

import (
	"github.com/rmmh/cubeographer/go/resourcepack"
	"github.com/samber/lo"
)

type migrateVersionedBlockMap struct {
	version  int
	blockmap map[string]string
}

func (bm *BlockMapper) precalculateMigrations() {
	// N^2 but N is small and it's done once.
	for i, mig1 := range resourcepack.MigrateBlockMaps {
		m := mig1.Blockmap
		if mig1.Version > bm.meta.WorldVersion {
			break
		}
		for _, mig2 := range resourcepack.MigrateBlockMaps[i+1:] {
			if mig2.Version > bm.meta.WorldVersion {
				break
			}
			m = lo.Assign(
				mig2.Blockmap,
				// The current migration's values are transformed according to the new one,
				// to allow transformation chains like A->B->C to collapse to A->C.
				lo.MapValues(m, func(v, k string) string {
					if v2, ok := mig2.Blockmap[v]; ok {
						return v2
					}
					return v
				}),
			)
		}
		bm.migrateBlockMaps = append(bm.migrateBlockMaps, migrateVersionedBlockMap{
			version: mig1.Version,
			blockmap: lo.MapEntries(m, func(k, v string) (string, string) {
				// restore the prefixes
				return "minecraft:" + k, "minecraft:" + v
			}),
		})
	}
}

func (bm *BlockMapper) migrate(vfrom int, palettes [][]paletteEntry) {
	vto := bm.meta.WorldVersion
	if vfrom >= vto {
		// TODO: warn on reading files that are newer than blockmeta
		return
	}

	m := make(map[string]string)

	// TODO: figure out if 2527's BitStorageAlignFix is relevant

	for _, migration := range bm.migrateBlockMaps {
		if migration.version > vfrom {
			m = migration.blockmap
			break
		}
	}

	if len(m) == 0 {
		return
	}

	// actually apply the map
	for _, ps := range palettes {
		for i := range ps {
			if newName, ok := m[ps[i].name]; ok {
				ps[i].name = newName
			}
		}
	}
}
