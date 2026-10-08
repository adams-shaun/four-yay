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
// globals (step, decision type, decisions text), the stack, the flat exile
// list, and each player's scalars, mana pool, battlefield permanents, hand and
// graveyard. As of 2026-10-07 that walker emits 16 of the design §5 feature
// families and records the other 24 as unsupportedFeatures, because the gorGE
// view projection does not yet carry them (colours, subtypes, dynamic
// types/abilities, CanAttack/CanBlock, permanent flags, attachments,
// imprinted/paired/targeted-by/ability lists, player counters, day/night,
// watchers and the rest). TestExtractorCoverageRatchetMatches holds both
// directions, and TestProcessStateIsDeterministic plus TestProcessStateNoMapRange
// pin that the walk is a pure, map-order-independent function of the view.
//
// See docs/superpowers/specs/2026-10-07-mzenc-design.md and
// docs/superpowers/plans/2026-10-07-mzenc-hash-port.md.
package mzenc
