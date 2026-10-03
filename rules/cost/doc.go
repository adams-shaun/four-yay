// Package cost is the cost vocabulary every rules subsystem shares: the
// parsed Cost and its CostPart components, the Forge cost-string parsers
// (ParseCost, the strict ParseUnlessCost), the client-notation and prose
// renderers (FormatCost, CostPhrase) and the pure predicates and arithmetic
// over a Cost (CMC, WithX, Plus, Priceable, HasNonMana, the mana-symbol
// helpers).
//
// It is layer L2 of the rules-engine lasagna (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §3, W5
// step E1): a leaf that imports only state, holds no *rules.Engine and reads
// no game state. internal/archtest TestCostVocabularyIsALeaf pins its import
// set. Payment -- pip expansion, the resolveMana search, the cast-flow
// stages -- stays in package rules until the rules/pay extraction (E7).
//
// Package rules reaches this package through one bridge file,
// rules/cost_vocab.go, which aliases the types and forwards the functions
// under their historical rules names, so the move left rules' call sites
// untouched.
package cost
