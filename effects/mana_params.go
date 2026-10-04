package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects/params"
)

// api:Mana's parameter compiler lives in the leaf package effects/params
// (params/mana.go, W5 step E7 of
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md): the
// payment layer (rules/pay) reads a mana ability's production there without
// importing effects. These are the effects spellings every existing caller
// uses, plus the two Amount$ evaluations that need the resolution's Host.

// ManaParams is one mana ability's production parameters (params.ManaParams).
type ManaParams = params.ManaParams

// ManaOf returns sa's compiled mana parameters (params.ManaOf).
func ManaOf(sa *cards.SA) *ManaParams { return params.ManaOf(sa) }

// ManaKnownKeys is a copy of the api:Mana known-key table, for the census
// check.
func ManaKnownKeys() []string { return params.ManaKnownKeys() }

// ManaAmountNum is effects.Num over p's compiled Amount$ (default def).
func ManaAmountNum(p *ManaParams, h Host, c *Ctx, def int32) int32 {
	return numText(h, c, p.Amount, def)
}

// ManaAmountResolvedStrict is effects.NumResolvedStrict over p's compiled
// Amount$.
func ManaAmountResolvedStrict(p *ManaParams, h Host, c *Ctx, def int32) (int32, bool) {
	return numResolvedStrictText(h, c, p.Amount, def)
}
