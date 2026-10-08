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
// Scope so far is the encoder mechanics (spec milestone 1). The StateEncoder
// walkers that turn a gorGE game into feature names are the follow-up plan.
// See docs/superpowers/specs/2026-10-07-mzenc-design.md and
// docs/superpowers/plans/2026-10-07-mzenc-hash-port.md.
package mzenc
