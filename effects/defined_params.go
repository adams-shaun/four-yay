package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects/params"
)

// The generic Defined-reference tier's compiler lives in the leaf package
// effects/params (params/defined.go, W5 step E7): the payment planner reads
// a compiled Ref without importing effects. These are the effects spellings.

// RefKind tags the common Defined$-grammar selectors (params.RefKind).
type RefKind = params.RefKind

// The RefKind tags (params).
const (
	RefAbsent                = params.RefAbsent
	RefOther                 = params.RefOther
	RefSelf                  = params.RefSelf
	RefYou                   = params.RefYou
	RefRemembered            = params.RefRemembered
	RefTargeted              = params.RefTargeted
	RefParent                = params.RefParent
	RefImprinted             = params.RefImprinted
	RefImprintedLKI          = params.RefImprintedLKI
	RefActivePlayer          = params.RefActivePlayer
	RefTriggeredSpellAbility = params.RefTriggeredSpellAbility
)

// RefFlag is one compiled boolean fact of a reference (params.RefFlag).
type RefFlag = params.RefFlag

// The RefFlag facts (params).
const (
	RefPresent         = params.RefPresent
	RefCompound        = params.RefCompound
	RefValid           = params.RefValid
	RefPlainRemembered = params.RefPlainRemembered
)

// Ref is one compiled Defined$-grammar reference (params.Ref).
type Ref = params.Ref

// DefinedParams is one ability's Defined-reference parameters
// (params.DefinedParams).
type DefinedParams = params.DefinedParams

// RefOf classifies a Defined$-grammar selector written as text (params.RefOf).
func RefOf(raw string) Ref { return params.RefOf(raw) }

// DefinedOf returns sa's compiled Defined-reference parameters
// (params.DefinedOf).
func DefinedOf(sa *cards.SA) *DefinedParams { return params.DefinedOf(sa) }

// DefinedRefOf is sa's compiled Defined$ reference (DefinedOf(sa).Defined).
func DefinedRefOf(sa *cards.SA) Ref { return params.DefinedRefOf(sa) }

// definedPlayerRef is DefinedPlayer$, the one reader of the key
// (params.DefinedPlayerRef).
func definedPlayerRef(sa *cards.SA) Ref { return params.DefinedPlayerRef(sa) }

// DefinedTierKeys are the keys the Defined tier reads for every ability.
func DefinedTierKeys() []string { return params.DefinedTierKeys() }
