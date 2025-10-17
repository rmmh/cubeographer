package region

func (bm *BlockMapper) migrate(vfrom int, palettes [][]paletteEntry) {
	vto := bm.meta.WorldVersion
	if vfrom >= vto {
		// TODO: warn on reading files that are newer than blockmeta
		return
	}

	// These remappings are sourced from minecraft/datafixer/Schemas.java
	// Which appears to be the single best source of truth for data-level
	// format differences.

	// TODO: cache and reuse these maps (?)
	m := make(map[string]string)
	if vfrom < 1474 && vto >= 1474 {
		m["minecraft:purple_shulker_box"] = "minecraft:shulker_box"
	}
	if vfrom < 1475 && vto >= 1475 {
		m["minecraft:flowing_water"] = "minecraft:water"
		m["minecraft:flowing_lava"] = "minecraft:lava"
	}
	if vfrom < 1480 && vto >= 1480 {
		m["minecraft:blue_coral"] = "minecraft:tube_coral_block"
		m["minecraft:pink_coral"] = "minecraft:brain_coral_block"
		m["minecraft:purple_coral"] = "minecraft:bubble_coral_block"
		m["minecraft:red_coral"] = "minecraft:fire_coral_block"
		m["minecraft:yellow_coral"] = "minecraft:horn_coral_block"
		m["minecraft:blue_coral_plant"] = "minecraft:tube_coral"
		m["minecraft:pink_coral_plant"] = "minecraft:brain_coral"
		m["minecraft:purple_coral_plant"] = "minecraft:bubble_coral"
		m["minecraft:red_coral_plant"] = "minecraft:fire_coral"
		m["minecraft:yellow_coral_plant"] = "minecraft:horn_coral"
		m["minecraft:blue_coral_fan"] = "minecraft:tube_coral_fan"
		m["minecraft:pink_coral_fan"] = "minecraft:brain_coral_fan"
		m["minecraft:purple_coral_fan"] = "minecraft:bubble_coral_fan"
		m["minecraft:red_coral_fan"] = "minecraft:fire_coral_fan"
		m["minecraft:yellow_coral_fan"] = "minecraft:horn_coral_fan"
		m["minecraft:blue_dead_coral"] = "minecraft:dead_tube_coral"
		m["minecraft:pink_dead_coral"] = "minecraft:dead_brain_coral"
		m["minecraft:purple_dead_coral"] = "minecraft:dead_bubble_coral"
		m["minecraft:red_dead_coral"] = "minecraft:dead_fire_coral"
		m["minecraft:yellow_dead_coral"] = "minecraft:dead_horn_coral"
	}
	if vfrom < 1484 && vto >= 1484 {
		m["minecraft:sea_grass"] = "minecraft:seagrass"
		m["minecraft:tall_sea_grass"] = "minecraft:tall_seagrass"
	}
	if vfrom < 1487 && vto >= 1487 {
		m["minecraft:prismarine_bricks_slab"] = "minecraft:prismarine_brick_slab"
		m["minecraft:prismarine_bricks_stairs"] = "minecraft:prismarine_brick_stairs"
	}
	if vfrom < 1488 && vto >= 1488 {
		m["minecraft:kelp_top"] = "minecraft:kelp"
		m["minecraft:kelp"] = "minecraft:kelp_plant"
	}
	if vfrom < 1490 && vto >= 1490 {
		m["minecraft:melon_block"] = "minecraft:melon"
	}
	if vfrom < 1510 && vto >= 1510 {
		m["minecraft:portal"] = "minecraft:nether_portal"
		m["minecraft:oak_bark"] = "minecraft:oak_wood"
		m["minecraft:spruce_bark"] = "minecraft:spruce_wood"
		m["minecraft:birch_bark"] = "minecraft:birch_wood"
		m["minecraft:jungle_bark"] = "minecraft:jungle_wood"
		m["minecraft:acacia_bark"] = "minecraft:acacia_wood"
		m["minecraft:dark_oak_bark"] = "minecraft:dark_oak_wood"
		m["minecraft:stripped_oak_bark"] = "minecraft:stripped_oak_wood"
		m["minecraft:stripped_spruce_bark"] = "minecraft:stripped_spruce_wood"
		m["minecraft:stripped_birch_bark"] = "minecraft:stripped_birch_wood"
		m["minecraft:stripped_jungle_bark"] = "minecraft:stripped_jungle_wood"
		m["minecraft:stripped_acacia_bark"] = "minecraft:stripped_acacia_wood"
		m["minecraft:stripped_dark_oak_bark"] = "minecraft:stripped_dark_oak_wood"
		m["minecraft:mob_spawner"] = "minecraft:spawner"
	}
	if vfrom < 1515 && vto >= 1515 {
		m["minecraft:tube_coral_fan"] = "minecraft:tube_coral_wall_fan"
		m["minecraft:brain_coral_fan"] = "minecraft:brain_coral_wall_fan"
		m["minecraft:bubble_coral_fan"] = "minecraft:bubble_coral_wall_fan"
		m["minecraft:fire_coral_fan"] = "minecraft:fire_coral_wall_fan"
		m["minecraft:horn_coral_fan"] = "minecraft:horn_coral_wall_fan"
	}
	if vfrom < 1802 && vto >= 1802 {
		m["minecraft:stone_slab"] = "minecraft:smooth_stone_slab"
		m["minecraft:sign"] = "minecraft:oak_sign"
		m["minecraft:wall_sign"] = "minecraft:oak_wall_sign"
	}
	if vfrom < 2209 && vto >= 2209 {
		m["minecraft:bee_hive"] = "minecraft:beehive"
	}
	if vfrom < 2508 && vto >= 2508 {
		m["minecraft:warped_fungi"] = "minecraft:warped_fungus"
		m["minecraft:crimson_fungi"] = "minecraft:crimson_fungus"
	}
	// TODO: figure out 2527's BitStorageAlignFix

	if vfrom < 2528 && vto >= 2528 {
		m["minecraft:soul_fire_torch"] = "minecraft:soul_torch"
		m["minecraft:soul_fire_wall_torch"] = "minecraft:soul_wall_torch"
		m["minecraft:soul_fire_lantern"] = "minecraft:soul_lantern"
	}
	if vfrom < 2679 && vto >= 2679 {
		// Technically this should be done based on the contents.
		m["minecraft:cauldron"] = "minecraft:water_cauldron"
	}
	if vfrom < 2680 && vto >= 2680 {
		m["minecraft:grass_path"] = "minecraft:dirt_path"
	}
	if vfrom < 2690 && vto >= 2690 {
		m["minecraft:weathered_copper_block"] = "minecraft:oxidized_copper_block"
		m["minecraft:semi_weathered_copper_block"] = "minecraft:weathered_copper_block"
		m["minecraft:lightly_weathered_copper_block"] = "minecraft:exposed_copper_block"
		m["minecraft:weathered_cut_copper"] = "minecraft:oxidized_cut_copper"
		m["minecraft:semi_weathered_cut_copper"] = "minecraft:weathered_cut_copper"
		m["minecraft:lightly_weathered_cut_copper"] = "minecraft:exposed_cut_copper"
		m["minecraft:weathered_cut_copper_stairs"] = "minecraft:oxidized_cut_copper_stairs"
		m["minecraft:semi_weathered_cut_copper_stairs"] = "minecraft:weathered_cut_copper_stairs"
		m["minecraft:lightly_weathered_cut_copper_stairs"] = "minecraft:exposed_cut_copper_stairs"
		m["minecraft:weathered_cut_copper_slab"] = "minecraft:oxidized_cut_copper_slab"
		m["minecraft:semi_weathered_cut_copper_slab"] = "minecraft:weathered_cut_copper_slab"
		m["minecraft:lightly_weathered_cut_copper_slab"] = "minecraft:exposed_cut_copper_slab"
		m["minecraft:waxed_semi_weathered_copper"] = "minecraft:waxed_weathered_copper"
		m["minecraft:waxed_lightly_weathered_copper"] = "minecraft:waxed_exposed_copper"
		m["minecraft:waxed_semi_weathered_cut_copper"] = "minecraft:waxed_weathered_cut_copper"
		m["minecraft:waxed_lightly_weathered_cut_copper"] = "minecraft:waxed_exposed_cut_copper"
		m["minecraft:waxed_semi_weathered_cut_copper_stairs"] = "minecraft:waxed_weathered_cut_copper_stairs"
		m["minecraft:waxed_lightly_weathered_cut_copper_stairs"] = "minecraft:waxed_exposed_cut_copper_stairs"
		m["minecraft:waxed_semi_weathered_cut_copper_slab"] = "minecraft:waxed_weathered_cut_copper_slab"
		m["minecraft:waxed_lightly_weathered_cut_copper_slab"] = "minecraft:waxed_exposed_cut_copper_slab"
	}
	if vfrom < 2691 && vto >= 2691 {
		m["minecraft:waxed_copper"] = "minecraft:waxed_copper_block"
		m["minecraft:oxidized_copper_block"] = "minecraft:oxidized_copper"
		m["minecraft:weathered_copper_block"] = "minecraft:weathered_copper"
		m["minecraft:exposed_copper_block"] = "minecraft:exposed_copper"
	}
	if vfrom < 2696 && vto >= 2696 {
		m["minecraft:grimstone"] = "minecraft:deepslate"
		m["minecraft:grimstone_slab"] = "minecraft:cobbled_deepslate_slab"
		m["minecraft:grimstone_stairs"] = "minecraft:cobbled_deepslate_stairs"
		m["minecraft:grimstone_wall"] = "minecraft:cobbled_deepslate_wall"
		m["minecraft:polished_grimstone"] = "minecraft:polished_deepslate"
		m["minecraft:polished_grimstone_slab"] = "minecraft:polished_deepslate_slab"
		m["minecraft:polished_grimstone_stairs"] = "minecraft:polished_deepslate_stairs"
		m["minecraft:polished_grimstone_wall"] = "minecraft:polished_deepslate_wall"
		m["minecraft:grimstone_tiles"] = "minecraft:deepslate_tiles"
		m["minecraft:grimstone_tile_slab"] = "minecraft:deepslate_tile_slab"
		m["minecraft:grimstone_tile_stairs"] = "minecraft:deepslate_tile_stairs"
		m["minecraft:grimstone_tile_wall"] = "minecraft:deepslate_tile_wall"
		m["minecraft:grimstone_bricks"] = "minecraft:deepslate_bricks"
		m["minecraft:grimstone_brick_slab"] = "minecraft:deepslate_brick_slab"
		m["minecraft:grimstone_brick_stairs"] = "minecraft:deepslate_brick_stairs"
		m["minecraft:grimstone_brick_wall"] = "minecraft:deepslate_brick_wall"
		m["minecraft:chiseled_grimstone"] = "minecraft:chiseled_deepslate"
	}
	if vfrom < 2700 && vto >= 2700 {
		m["minecraft:cave_vines_head"] = "minecraft:cave_vines"
		m["minecraft:cave_vines_body"] = "minecraft:cave_vines_plant"
	}
	if vfrom < 2717 && vto >= 2717 {
		m["minecraft:azalea_leaves_flowers"] = "minecraft:flowering_azalea_leaves"
	}
	if vfrom < 3692 && vto >= 3692 {
		m["minecraft:grass"] = "minecraft:short_grass"
	}
	if vfrom < 4541 && vto >= 4541 {
		m["minecraft:chain"] = "minecraft:iron_chain"
	}

	if len(m) == 0 {
		return
	}

	// actually apply the map
	for _, ps := range palettes {
		for i := range ps {
			if new, ok := m[ps[i].name]; ok {
				ps[i].name = new
			}
		}
	}
}
