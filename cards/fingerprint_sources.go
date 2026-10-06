package cards

import "embed"

// compilerSources embeds every non-test .go file in this package so
// CompilerFingerprint can hash the source the running binary was actually
// built from. It is the package's own parse/compile source that determines
// the IR a cache holds, so two builds that differ anywhere in cards/ must never
// share an IR cache file (see CachePath).
//
// The files are listed one per directive instead of `//go:embed *.go`, because
// that pattern also matches the _test.go files: their bytes then became part of
// this PRODUCTION package's build input, so every test-only edit in cards/
// recompiled cards and invalidated the cached test result of every package
// that imports it (79 of the module's 88 test binaries). The fingerprint value
// is unchanged by the split (computeCompilerFingerprint always skipped
// _test.go files).
//
// Adding a non-test .go file to cards/ means adding its line here, in sorted
// order; TestCompilerSourcesListEveryNonTestFile fails naming the missing line.
//
//go:embed ability_class.go
//go:embed affected_defined.go
//go:embed breakdown.go
//go:embed card_probes.go
//go:embed compiled_catalog.go
//go:embed compiled_codes.go
//go:embed control_static_probe.go
//go:embed counter_keyword.go
//go:embed doc.go
//go:embed equal.go
//go:embed face.go
//go:embed fetch.go
//go:embed fingerprint.go
//go:embed fingerprint_sources.go
//go:embed gitenv.go
//go:embed hiddenkeyword.go
//go:embed intrinsic.go
//go:embed ir.go
//go:embed keywords.go
//go:embed kw_affinity.go
//go:embed kw_afflict.go
//go:embed kw_afterlife.go
//go:embed kw_annihilator.go
//go:embed kw_backup.go
//go:embed kw_battle_cry.go
//go:embed kw_cipher.go
//go:embed kw_class.go
//go:embed kw_conspire.go
//go:embed kw_craft.go
//go:embed kw_crew.go
//go:embed kw_cumulativeupkeep.go
//go:embed kw_cycling.go
//go:embed kw_demonstrate.go
//go:embed kw_dethrone.go
//go:embed kw_devour.go
//go:embed kw_echo.go
//go:embed kw_embalm.go
//go:embed kw_enchant.go
//go:embed kw_encore.go
//go:embed kw_equip.go
//go:embed kw_etbcounter.go
//go:embed kw_etbreplacement.go
//go:embed kw_evolve.go
//go:embed kw_exalted.go
//go:embed kw_exploit.go
//go:embed kw_extort.go
//go:embed kw_fabricate.go
//go:embed kw_firebending.go
//go:embed kw_flanking.go
//go:embed kw_formirrodin.go
//go:embed kw_fortify.go
//go:embed kw_graft.go
//go:embed kw_granted.go
//go:embed kw_gravestorm.go
//go:embed kw_hideaway.go
//go:embed kw_increment.go
//go:embed kw_jobselect.go
//go:embed kw_levelup.go
//go:embed kw_livingweapon.go
//go:embed kw_melee.go
//go:embed kw_mentor.go
//go:embed kw_mobilize.go
//go:embed kw_myriad.go
//go:embed kw_ninjutsu.go
//go:embed kw_offspring.go
//go:embed kw_outlast.go
//go:embed kw_partner_with.go
//go:embed kw_persist.go
//go:embed kw_prevent.go
//go:embed kw_prowess.go
//go:embed kw_ravenous.go
//go:embed kw_reconfigure.go
//go:embed kw_renown.go
//go:embed kw_replicate.go
//go:embed kw_saddle.go
//go:embed kw_soulbond.go
//go:embed kw_squad.go
//go:embed kw_storm.go
//go:embed kw_sunburst.go
//go:embed kw_training.go
//go:embed kw_transmute.go
//go:embed kw_typecycling.go
//go:embed kw_undaunted.go
//go:embed kw_undying.go
//go:embed kw_unearth.go
//go:embed kw_vanishing.go
//go:embed kw_ward.go
//go:embed layout.go
//go:embed linemodes.go
//go:embed link.go
//go:embed mana_production.go
//go:embed mayask.go
//go:embed modes.go
//go:embed mustblock_shape.go
//go:embed open.go
//go:embed params.go
//go:embed parse.go
//go:embed primitive.go
//go:embed registry.go
//go:embed saga.go
//go:embed slot.go
//go:embed strtab.go
//go:embed subset.go
//go:embed tokens.go
//go:embed validate.go
//go:embed value_heads.go
//go:embed words.go
var compilerSources embed.FS
