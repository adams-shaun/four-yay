package effects

import (
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// This file is the generic DEFINED-REFERENCE tier's parameter compiler (W4
// step 4, docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md
// section 8: "start with APIs whose params are read on more than one path
// (costs, targets, Defined$)"). Defined$ names the objects or players an
// ability acts on whatever its API, and it was read on the offer, payment,
// planner and resolution paths by ~70 literal reads in ~45 files, each
// trimming (or not) and comparing (case-sensitively or not) on its own.
// compileDefined is now the ONLY reader of Defined$, DefinedCards$ and
// DefinedTarget$: every path reads the compiled Ref through DefinedOf, so two
// paths can no longer parse the selector differently. internal/codeshape's
// definedParamLeaks ratchet holds it: no read of a Defined key anywhere in
// rules/ or effects/ outside this file.
//
// Defined$, DefinedCards$ and DefinedTarget$ are read by the generic
// machinery for every API already (the parameter census attributes all three
// to every API's read set), so compiling them for every ability changes no
// known-key table. DefinedPlayer$ is NOT: only ChangeZone, the Manifest
// family (Manifest, ManifestDread, Cloak) and AddOrRemoveCounter read it, and
// a generic read would mark it read for every API and make every per-API
// known-key table demand it. It has its own single reader here instead
// (definedPlayerRef), called only from those APIs' paths -- the targeting
// tier's DividedAsYouChoose$ shape.
//
// Selector text that is not a param read at all -- a synthetic selector built
// in code, an SVar body's text, another parameter's value used in Defined$
// grammar -- classifies through RefOf, a pure function that allocates
// nothing, and resolves through DefinedRef.

// RefKind tags the common Defined$-grammar selectors a call site tests for.
// The tag is the exact (case-sensitive) trimmed text; anything else is
// RefOther and carries its text.
type RefKind uint8

const (
	// RefAbsent: the parameter is absent or blank.
	RefAbsent RefKind = iota
	// RefOther: a selector the tag set does not name (Ref.Text carries it).
	RefOther
	RefSelf
	RefYou
	RefRemembered
	RefTargeted
	RefParent
	RefImprinted
	RefImprintedLKI
	RefActivePlayer
	RefTriggeredSpellAbility
)

// refKindText is each tagged selector's exact text, indexed by its kind.
var refKindText = [...]string{
	RefSelf:                  "Self",
	RefYou:                   "You",
	RefRemembered:            "Remembered",
	RefTargeted:              "Targeted",
	RefParent:                "Parent",
	RefImprinted:             "Imprinted",
	RefImprintedLKI:          "ImprintedLKI",
	RefActivePlayer:          "ActivePlayer",
	RefTriggeredSpellAbility: "TriggeredSpellAbility",
}

// RefFlag is one compiled boolean fact of a reference.
type RefFlag uint8

const (
	// RefPresent: the parameter key is present (even blank).
	RefPresent RefFlag = 1 << iota
	// RefCompound: the selector joins independent selectors with " & ".
	RefCompound
	// RefValid: Forge's battlefield sweep form, "Valid" or "Valid <filter>".
	RefValid
	// RefPlainRemembered: the plain Remembered family (Remembered...,
	// neither ...Controller nor ...Owner), plainRememberedSelector's answer.
	RefPlainRemembered
)

// Ref is one compiled Defined$-grammar reference.
type Ref struct {
	// Raw is the value as written ("" when absent); Text is it trimmed.
	Raw, Text string
	// Kind is the selector's tag; Flags its compiled facts.
	Kind  RefKind
	Flags RefFlag
}

// RefOf classifies a Defined$-grammar selector written as text (present: a
// call with text "" is a present-but-blank reference). It allocates nothing.
func RefOf(raw string) Ref { return refOf(raw, true) }

func refOf(raw string, present bool) Ref {
	r := Ref{Raw: raw, Text: strings.TrimSpace(raw)}
	if present {
		r.Flags |= RefPresent
	}
	if r.Text == "" {
		return r
	}
	r.Kind = RefOther
	for k := RefSelf; int(k) < len(refKindText); k++ {
		if refKindText[k] == r.Text {
			r.Kind = k
			break
		}
	}
	if strings.Contains(r.Raw, " & ") {
		r.Flags |= RefCompound
	}
	if r.Raw == "Valid" || strings.HasPrefix(r.Raw, "Valid ") {
		r.Flags |= RefValid
	}
	if plainRememberedSelector(r.Raw) {
		r.Flags |= RefPlainRemembered
	}
	return r
}

// Set reports whether the reference names a selector (non-blank text).
func (r Ref) Set() bool { return r.Text != "" }

// Is reports whether the reference is the tagged selector k.
func (r Ref) Is(k RefKind) bool { return r.Kind == k }

// Has reports whether every flag in f is set.
func (r Ref) Has(f RefFlag) bool { return r.Flags&f == f }

// Present reports whether the parameter key is present (even blank).
func (r Ref) Present() bool { return r.Flags&RefPresent != 0 }

// Param is the reference as a compiled parameter (the text trimmed, the
// key's presence) for the readers that take a ParamText.
func (r Ref) Param() ParamText { return ParamText{Text: r.Text, Present: r.Present()} }

// DefinedParams is one ability's Defined-reference parameters, compiled once.
type DefinedParams struct {
	// paramBinding is the Params map the struct was compiled from
	// (typed_params.go's identity rule).
	paramBinding

	// Defined is Defined$.
	Defined Ref
	// Cards is DefinedCards$.
	Cards Ref
	// Target is DefinedTarget$.
	Target Ref
}

// noDefined answers DefinedOf for a nil ability or one with no parameters.
var noDefined = DefinedParams{}

// DefinedOf returns sa's compiled Defined-reference parameters: the
// configured record's when it is bound to sa's Params map, else the front
// cache's entry for that map, else a fresh compile stored in the front cache
// (TargetsOf's contract). A nil ability, or one with no parameters, answers
// the shared empty record. Never nil; the result is read-only.
func DefinedOf(sa *cards.SA) *DefinedParams {
	if sa == nil || len(sa.Params) == 0 {
		return &noDefined
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Defined; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &definedFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDefined(sa)
	slot.Store(p)
	return p
}

// DefinedRefOf is sa's compiled Defined$ reference (DefinedOf(sa).Defined).
func DefinedRefOf(sa *cards.SA) Ref { return DefinedOf(sa).Defined }

// definedFront is DefinedOf's direct-mapped front cache (czFront's shape).
var definedFront [1 << 10]atomic.Pointer[DefinedParams]

// compileDefined is the one reader of an ability's Defined$, DefinedCards$
// and DefinedTarget$ parameters.
func compileDefined(sa *cards.SA) *DefinedParams {
	p := &DefinedParams{paramBinding: bindParams(sa)}
	d, dOK := sa.Param(cards.PKDefined)
	p.Defined = refOf(d, dOK)
	dc, dcOK := sa.Param(cards.PKDefinedCards)
	p.Cards = refOf(dc, dcOK)
	dt, dtOK := sa.Param(cards.PKDefinedTarget)
	p.Target = refOf(dt, dtOK)
	return p
}

// definedPlayerRef is DefinedPlayer$, the one reader of the key. It is not
// part of compileDefined (see the file comment): only the APIs that honour it
// call it.
func definedPlayerRef(sa *cards.SA) Ref {
	v, ok := sa.Param(cards.PKDefinedPlayer)
	return refOf(v, ok)
}

// DefinedTierKeys are the keys compileDefined reads for every ability (the
// rules census check holds them read for every API).
func DefinedTierKeys() []string { return []string{"Defined", "DefinedCards", "DefinedTarget"} }
