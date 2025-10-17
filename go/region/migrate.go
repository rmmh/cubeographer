package region

import "github.com/samber/lo"

type migrateVersionedBlockMap struct {
	version  int
	blockmap map[string]string
}

// These remappings are sourced from minecraft/datafixer/Schemas.java
// Which appears to be the single best source of truth for data-level
// format differences.
var migrateBlockMaps = []migrateVersionedBlockMap{
	{1474, map[string]string{
		"purple_shulker_box": "shulker_box",
	}},
	{1475, map[string]string{
		"flowing_water": "water",
		"flowing_lava":  "lava",
	}},
	{1480, map[string]string{
		"blue_coral":         "tube_coral_block",
		"pink_coral":         "brain_coral_block",
		"purple_coral":       "bubble_coral_block",
		"red_coral":          "fire_coral_block",
		"yellow_coral":       "horn_coral_block",
		"blue_coral_plant":   "tube_coral",
		"pink_coral_plant":   "brain_coral",
		"purple_coral_plant": "bubble_coral",
		"red_coral_plant":    "fire_coral",
		"yellow_coral_plant": "horn_coral",
		"blue_coral_fan":     "tube_coral_fan",
		"pink_coral_fan":     "brain_coral_fan",
		"purple_coral_fan":   "bubble_coral_fan",
		"red_coral_fan":      "fire_coral_fan",
		"yellow_coral_fan":   "horn_coral_fan",
		"blue_dead_coral":    "dead_tube_coral",
		"pink_dead_coral":    "dead_brain_coral",
		"purple_dead_coral":  "dead_bubble_coral",
		"red_dead_coral":     "dead_fire_coral",
		"yellow_dead_coral":  "dead_horn_coral",
	}},
	{1484, map[string]string{
		"sea_grass":      "seagrass",
		"tall_sea_grass": "tall_seagrass",
	}},
	{1487, map[string]string{
		"prismarine_bricks_slab":   "prismarine_brick_slab",
		"prismarine_bricks_stairs": "prismarine_brick_stairs",
	}},
	{1488, map[string]string{
		"kelp_top": "kelp",
		"kelp":     "kelp_plant",
	}},
	{1490, map[string]string{
		"melon_block": "melon",
	}},
	{1510, map[string]string{
		"portal":                 "nether_portal",
		"oak_bark":               "oak_wood",
		"spruce_bark":            "spruce_wood",
		"birch_bark":             "birch_wood",
		"jungle_bark":            "jungle_wood",
		"acacia_bark":            "acacia_wood",
		"dark_oak_bark":          "dark_oak_wood",
		"stripped_oak_bark":      "stripped_oak_wood",
		"stripped_spruce_bark":   "stripped_spruce_wood",
		"stripped_birch_bark":    "stripped_birch_wood",
		"stripped_jungle_bark":   "stripped_jungle_wood",
		"stripped_acacia_bark":   "stripped_acacia_wood",
		"stripped_dark_oak_bark": "stripped_dark_oak_wood",
		"mob_spawner":            "spawner",
	}},
	{1515, map[string]string{
		"tube_coral_fan":   "tube_coral_wall_fan",
		"brain_coral_fan":  "brain_coral_wall_fan",
		"bubble_coral_fan": "bubble_coral_wall_fan",
		"fire_coral_fan":   "fire_coral_wall_fan",
		"horn_coral_fan":   "horn_coral_wall_fan",
	}},
	{1802, map[string]string{
		"stone_slab": "smooth_stone_slab",
		"sign":       "oak_sign",
		"wall_sign":  "oak_wall_sign",
	}},
	{2209, map[string]string{
		"bee_hive": "beehive",
	}},
	{2508, map[string]string{
		"warped_fungi":  "warped_fungus",
		"crimson_fungi": "crimson_fungus",
	}},
	{2528, map[string]string{
		"soul_fire_torch":      "soul_torch",
		"soul_fire_wall_torch": "soul_wall_torch",
		"soul_fire_lantern":    "soul_lantern",
	}},
	{2679, map[string]string{
		// Technically this should be done based on the contents.
		"cauldron": "water_cauldron",
	}},
	{2680, map[string]string{
		"grass_path": "dirt_path",
	}},
	{2690, map[string]string{
		"weathered_copper_block":                    "oxidized_copper_block",
		"semi_weathered_copper_block":               "weathered_copper_block",
		"lightly_weathered_copper_block":            "exposed_copper_block",
		"weathered_cut_copper":                      "oxidized_cut_copper",
		"semi_weathered_cut_copper":                 "weathered_cut_copper",
		"lightly_weathered_cut_copper":              "exposed_cut_copper",
		"weathered_cut_copper_stairs":               "oxidized_cut_copper_stairs",
		"semi_weathered_cut_copper_stairs":          "weathered_cut_copper_stairs",
		"lightly_weathered_cut_copper_stairs":       "exposed_cut_copper_stairs",
		"weathered_cut_copper_slab":                 "oxidized_cut_copper_slab",
		"semi_weathered_cut_copper_slab":            "weathered_cut_copper_slab",
		"lightly_weathered_cut_copper_slab":         "exposed_cut_copper_slab",
		"waxed_semi_weathered_copper":               "waxed_weathered_copper",
		"waxed_lightly_weathered_copper":            "waxed_exposed_copper",
		"waxed_semi_weathered_cut_copper":           "waxed_weathered_cut_copper",
		"waxed_lightly_weathered_cut_copper":        "waxed_exposed_cut_copper",
		"waxed_semi_weathered_cut_copper_stairs":    "waxed_weathered_cut_copper_stairs",
		"waxed_lightly_weathered_cut_copper_stairs": "waxed_exposed_cut_copper_stairs",
		"waxed_semi_weathered_cut_copper_slab":      "waxed_weathered_cut_copper_slab",
		"waxed_lightly_weathered_cut_copper_slab":   "waxed_exposed_cut_copper_slab",
	}},
	{2691, map[string]string{
		"waxed_copper":           "waxed_copper_block",
		"oxidized_copper_block":  "oxidized_copper",
		"weathered_copper_block": "weathered_copper",
		"exposed_copper_block":   "exposed_copper",
	}},
	{2696, map[string]string{
		"grimstone":                 "deepslate",
		"grimstone_slab":            "cobbled_deepslate_slab",
		"grimstone_stairs":          "cobbled_deepslate_stairs",
		"grimstone_wall":            "cobbled_deepslate_wall",
		"polished_grimstone":        "polished_deepslate",
		"polished_grimstone_slab":   "polished_deepslate_slab",
		"polished_grimstone_stairs": "polished_deepslate_stairs",
		"polished_grimstone_wall":   "polished_deepslate_wall",
		"grimstone_tiles":           "deepslate_tiles",
		"grimstone_tile_slab":       "deepslate_tile_slab",
		"grimstone_tile_stairs":     "deepslate_tile_stairs",
		"grimstone_tile_wall":       "deepslate_tile_wall",
		"grimstone_bricks":          "deepslate_bricks",
		"grimstone_brick_slab":      "deepslate_brick_slab",
		"grimstone_brick_stairs":    "deepslate_brick_stairs",
		"grimstone_brick_wall":      "deepslate_brick_wall",
		"chiseled_grimstone":        "chiseled_deepslate",
	}},
	{2700, map[string]string{
		"cave_vines_head": "cave_vines",
		"cave_vines_body": "cave_vines_plant",
	}},
	{2717, map[string]string{
		"azalea_leaves_flowers": "flowering_azalea_leaves",
	}},
	{3438, map[string]string{
		"suspicious_sand": "brushable_block",
	}},
	{3692, map[string]string{
		"grass": "short_grass",
	}},
	{4541, map[string]string{
		"chain": "iron_chain",
	}},
}

func (bm *BlockMapper) precalculateMigrations() {
	// N^2 but N is small and it's done once.
	for i, mig1 := range migrateBlockMaps {
		m := mig1.blockmap
		if mig1.version > bm.meta.WorldVersion {
			break
		}
		for _, mig2 := range migrateBlockMaps[i+1:] {
			if mig2.version > bm.meta.WorldVersion {
				break
			}
			m = lo.Assign(
				mig2.blockmap,
				// The current migration's values are transformed according to the new one,
				// to allow transformation chains like A->B->C to collapse to A->C.
				lo.MapValues(m, func(v, k string) string {
					if v2, ok := mig2.blockmap[v]; ok {
						return v2
					}
					return v
				}),
			)
		}
		bm.migrateBlockMaps = append(bm.migrateBlockMaps, migrateVersionedBlockMap{
			version: mig1.version,
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
