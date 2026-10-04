// Package params is the leaf home of the compiled ability-parameter records
// that both the resolution (package effects) and the payment layer
// (rules/pay) read: the mana production record (ManaOf), the generic
// targeting tier (TargetsOf), the Defined-reference tier (DefinedOf, Ref) and
// api:DealDamage's record (DealDamageOf), plus the pure Produced$ Combo
// classifier (ComboColours).
//
// It exists for the lasagna's W5 step E7 (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §9): the
// mana and payment ring reads these records on nearly every path, and
// rules/pay may not import effects (it sits above effects in no sense -- it
// is a sibling that must not reach the resolution's Host/Ctx machinery). The
// records are pure functions of an ability's text, so they belong below
// both. Package effects aliases every exported name here, so effects callers
// and rules callers spelling effects.ManaOf are unchanged.
//
// It imports only the L0/L1 vocabulary (cards, state, decision);
// internal/archtest pins that (layering_pay_test.go paramsImports).
package params

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// ParamText is one compiled parameter's text and presence. Parsed parameter
// values are already trimmed (cards' parseParams), so Text is the value as the
// script spells it.
type ParamText struct {
	Text    string
	Present bool
}

// paramBinding is the Params map a typed struct was compiled from and the
// map's size then: the struct answers only for that exact map instance
// (cards.SameParamMap), so a cards.ResolveSVar copy sharing its template's
// parse reads the template's struct while a copy whose Params were rewritten
// recompiles. (effects' typed_params.go holds the same rule for the records
// that still live there.)
type paramBinding struct {
	src map[string]string
	n   int
}

func bindParams(sa *cards.SA) paramBinding {
	return paramBinding{src: sa.Params, n: len(sa.Params)}
}

func (b *paramBinding) boundTo(m map[string]string) bool {
	return b.n == len(m) && cards.SameParamMap(b.src, m)
}

// paramMapSlot is a front cache's direct-mapped slot for a Params map's
// identity (effects' changezone_params.go holds the same function).
func paramMapSlot(m map[string]string) uint {
	h := uint64(cards.ParamMapIdentity(m)>>3) ^ uint64(len(m))*0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uint(h) & (1<<10 - 1)
}

func isTrue(v string) bool { return strings.EqualFold(strings.TrimSpace(v), "True") }

// Facts is the leaf half of effects.SAFacts, the one per-ability compiled
// facts record hung on cards.SA's ExtSlot: the records this package owns.
// effects.SAFacts embeds it as its FIRST field, so a pointer to the record is
// a pointer to its Facts (TestFactsIsSAFactsPrefix in effects pins the
// offset), and LoadFacts reads the configured record without package effects.
type Facts struct {
	// Targets is the generic targeting tier's compiled parameter set, non-nil
	// for EVERY ability whatever its API.
	Targets *TargetParams
	// Defined is the generic Defined-reference tier's compiled parameter set,
	// non-nil for EVERY ability whatever its API.
	Defined *DefinedParams
	// Mana is api:Mana's compiled production parameters, non-nil exactly when
	// the API is Mana.
	Mana *ManaParams
	// DealDamage is api:DealDamage's compiled parameter set, non-nil exactly
	// when the API is DealDamage.
	DealDamage *DealDamageParams
}

// CompileFacts compiles sa's leaf records (effects' NewSAFacts calls it for
// the record's first half).
func CompileFacts(sa *cards.SA) Facts {
	f := Facts{Targets: compileTargets(sa), Defined: compileDefined(sa)}
	if sa.API == "Mana" {
		f.Mana = compileMana(sa, f.Defined)
	}
	if isDealDamageSA(sa) {
		f.DealDamage = compileDealDamage(sa)
	}
	return f
}

// LoadFacts returns the leaf half of the record published on sa's slot, or
// nil (callers apply their own identity check).
func LoadFacts(sa *cards.SA) *Facts {
	if sa == nil {
		return nil
	}
	return (*Facts)(sa.ExtSlot().Load())
}

// unreadKeys lists, sorted, the keys present on sa that are not in known (a
// sorted table): an API compiler's unread report (one map walk per compile).
// The walk is unsorted and the result sorted, so map order never reaches it,
// and an ability with nothing unread allocates nothing. (effects'
// changezoneall_params.go holds the same function for its compilers.)
func unreadKeys(sa *cards.SA, known []string) []string {
	var out []string
	for k := range sa.Params {
		if _, ok := slices.BinarySearch(known, k); !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}
