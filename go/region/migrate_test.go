package region

import (
	"testing"

	"github.com/rmmh/cubeographer/go/render"
	"github.com/stretchr/testify/require"
)

func TestMigration(t *testing.T) {
	for _, tc := range []struct {
		vfrom, vto      int
		input, expected string
	}{
		{2680, 2690, "minecraft:weathered_copper_block", "minecraft:oxidized_copper_block"},
		{2680, 2691, "minecraft:weathered_copper_block", "minecraft:oxidized_copper"},
		{2689, 2690, "minecraft:weathered_copper_block", "minecraft:oxidized_copper_block"},
		{2690, 2691, "minecraft:weathered_copper_block", "minecraft:weathered_copper"},
		{2690, 2691, "minecraft:grimstone", "minecraft:grimstone"},
		{2690, 2696, "minecraft:grimstone", "minecraft:deepslate"},
	} {
		bm := BlockMapper{
			meta: render.BlockEntryMetadata{
				WorldVersion: tc.vto,
			},
		}
		bm.precalculateMigrations()
		pal := [][]paletteEntry{
			{
				{
					name: tc.input,
				},
			},
		}
		bm.migrate(tc.vfrom, pal)
		require.Equal(t, tc.expected, pal[0][0].name, "migrate(%d, %d, %q)", tc.vfrom, tc.vto, tc.input)
	}
}
