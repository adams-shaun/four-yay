// Package mzenc is a byte-identical Go port of MageZero v0.2's feature encoder
// (XMage fork WillWroble/mage @ master cb7e9c6f, package
// mage.player.ai.encoder), starting with the hash core: Features.mix64,
// hash64, indexFor, the feature tree (occurrence cardinality via `name#n`, the
// numeric thermometer), and the FeatureMap research table.
//
// The contract is exact id-set equality: the golden vectors in
// testdata/golden, generated from the vendored Java source by
// testdata/javaharness/FeaturesProbe, are replayed in TestHashOracle and must
// match byte for byte. The JVM is needed only to regenerate goldens, never by
// `go test`.
//
// The StateEncoder walkers now turn a gorGE view into feature names: the
// globals (step, decision type, decisions text), the stack (targets, kicks, X
// and chosen modes included), the flat exile list, and each player's scalars,
// counters, land-drop state, mana pool, attachments, battlefield permanents
// (colours, subtypes, flags, attachments, imprinted/paired/own-exile links,
// TargetedBy, CanAttack/CanBlock), hand, graveyard and command zone. As of
// 2026-10-08 that walker emits 31 of the 42 design §5 families (counting the
// caveat entries) and records the other 11 as unsupportedFeatures:
//
//   - the engine has no such state: DayNight, GlobalWatchers (spells cast /
//     life gained / life lost / tokens created this turn), InPayManaMode,
//     Activating, MicroDecisions (casting is atomic here), Emblem (emblems are
//     continuous effects, not objects);
//   - the view would need a layered or per-card text projection it should not
//     pay for on the hot path or on every wire card: DynamicTypes (layer-4/5
//     types and colours), DynamicAbilities, CardAbilities (rule-text lists);
//   - caveats whose view shape cannot carry the upstream key: ExileZoneNames
//     (one flat exile list), StackCostTags (no per-cast cost-tag map).
//
// Colours are the WUBRG symbols of the printed mana cost and CanAttack /
// CanBlock are derived from tapped / summoning-sick / haste / defender, so a
// colour indicator, a token's own colour and a "can't attack/block" static are
// not reflected. TestExtractorCoverageRatchetMatches holds both directions,
// and TestProcessStateIsDeterministic plus TestProcessStateNoMapRange pin that
// the walk is a pure, map-order-independent function of the view.
//
// See docs/superpowers/specs/2026-10-07-mzenc-design.md and
// docs/superpowers/plans/2026-10-07-mzenc-hash-port.md.
package mzenc
