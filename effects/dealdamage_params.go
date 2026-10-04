package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects/params"
)

// api:DealDamage's parameter compiler lives in the leaf package
// effects/params (params/dealdamage.go, W5 step E7): the payment planner's
// self-damage rider probe reads it there. These are the effects spellings.

// DealDamageParams is one DealDamage ability's parameters
// (params.DealDamageParams).
type DealDamageParams = params.DealDamageParams

// DealDamageOf returns sa's compiled DealDamage parameters
// (params.DealDamageOf).
func DealDamageOf(sa *cards.SA) *DealDamageParams { return params.DealDamageOf(sa) }

// DamageAmount is a damage-dealing ability's NumDmg$ (params.DamageAmount).
func DamageAmount(sa *cards.SA) ParamText { return params.DamageAmount(sa) }

// DealDamageKnownKeys is a copy of the api:DealDamage known-key table, for
// the census check.
func DealDamageKnownKeys() []string { return params.DealDamageKnownKeys() }

// damageSourceParam is DamageSource$'s one reader (params.DamageSourceParam).
func damageSourceParam(sa *cards.SA) string { return params.DamageSourceParam(sa) }

// replaceDyingParam is ReplaceDyingDefined$'s one reader
// (params.ReplaceDyingParam).
func replaceDyingParam(sa *cards.SA) string { return params.ReplaceDyingParam(sa) }
