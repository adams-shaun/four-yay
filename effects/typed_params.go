package effects

import (
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

// This file is the machinery the per-API typed parameter compilers share (W4
// step 3 of docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md,
// section 8): the identity rule a compiled struct answers by, the
// compile-time unread-parameter check, and its loud-degrade Note. Each
// compiler (charm_params.go, pump_params.go, draw_params.go) is the ONLY
// reader of its API's own parameters; its XxxOf accessor returns the
// configured record from SAFacts, else a front-cache entry, else a fresh
// compile -- ChangeZoneOf's shape (changezone_params.go).

// paramBinding is the Params map a typed struct was compiled from and the
// map's size then: the struct answers only for that exact map instance
// (cards.SameParamMap), so a cards.ResolveSVar copy sharing its template's
// parse reads the template's struct while a copy whose Params were rewritten
// recompiles.
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

// unreadParams lists, sorted, the keys present on sa that are not in known
// (a sorted table). Compile time only: one map walk per ability.
func unreadParams(sa *cards.SA, known []string) []string {
	var out []string
	for _, k := range sa.ParamNames() {
		if _, ok := slices.BinarySearch(known, k); !ok {
			out = append(out, k)
		}
	}
	return out
}

// noteUnreadParams is the loud degrade for parameters a compiler found no
// reader for: one Note naming every such key; the effect resolves without
// them.
func noteUnreadParams(h Host, c *Ctx, api string, unread []string) {
	if len(unread) == 0 {
		return
	}
	text := api + " ignores unread parameter(s)"
	for i, k := range unread {
		if i > 0 {
			text += ","
		}
		text += " " + k + "$"
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: text})
}

// compileTypedHalves fills f's Charm/Pump/Draw typed parameter structs for
// the APIs their compilers serve (NewSAFacts' second half).
func compileTypedHalves(f *SAFacts, sa *cards.SA) {
	if isModalSA(sa) {
		f.Charm = compileCharm(sa)
	}
}
