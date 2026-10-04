package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// TestParamCodesMatchTextReads holds every load-time parameter code
// (cards.RegisterParamCoder: Origin$, ExcludedOrigins$, Destination$,
// EffectZone$, ExcludeZone$, Condition$) equal to the text read it replaced,
// over every printed static, trigger and replacement line in the corpus and
// every zone, and checks the code a bound node stores is the coder's answer
// for its text.
func TestParamCodesMatchTextReads(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	zones := []state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard, state.ZExile,
		state.ZStack, state.ZCommand, state.ZCeased, state.ZSideboard, state.ZPlanarDeck}
	checkZoneParams := func(name string, param func(cards.ParamKey) (string, bool), code func(cards.ParamKey) (uint16, bool)) {
		t.Helper()
		if o, ok := param(cards.PKOrigin); ok {
			c, _ := code(cards.PKOrigin)
			zl, all, okList := effects.ParseZones(o)
			for _, z := range zones {
				if got, want := effects.ZoneList(c).Admits(z), okList && (all || slices.Contains(zl, z)); got != want {
					t.Errorf("%s Origin$ %q zone %v: code %v, text %v", name, o, z, got, want)
				}
			}
		}
		if x, ok := param(cards.PKExcludedOrigins); ok {
			c, _ := code(cards.PKExcludedOrigins)
			for _, z := range zones {
				want := false
				for p := range strings.SplitSeq(x, ",") {
					if pp := strings.TrimSpace(p); pp != "" && effects.ParseZone(pp) == z {
						want = true
					}
				}
				if got := effects.ZoneWords(c).Has(z); got != want {
					t.Errorf("%s ExcludedOrigins$ %q zone %v: code %v, text %v", name, x, z, got, want)
				}
			}
		}
		if d, ok := param(cards.PKDestination); ok {
			c, _ := code(cards.PKDestination)
			dc := effects.Destination(c)
			if dc.IsAny() != (d == "Any") || dc.IsEmpty() != (d == "") || dc.Zone() != effects.ParseZone(d) {
				t.Errorf("%s Destination$ %q: code %+v", name, d, dc)
			}
		}
	}
	for _, card := range reg.Cards {
		for _, f := range card.Faces {
			for i := range f.Statics {
				st := f.Statics[i]
				checkZoneParams(f.Name+" static", st.Param, st.ParamCode)
				for _, z := range zones {
					if got, want := chars.StaticEffectZoneOK(st, z), chars.EffectZoneOK(st.ParamStr(cards.PKEffectZone), z); got != want {
						t.Errorf("%s EffectZone$ %q zone %v: code %v, text %v", f.Name, st.ParamStr(cards.PKEffectZone), z, got, want)
					}
					if got, want := chars.StaticZoneAdmitsStatic(st, z), chars.StaticZoneAdmits(st.ParamStr(cards.PKExcludeZone), st.ParamStr(cards.PKEffectZone), z); got != want {
						t.Errorf("%s ExcludeZone$ %q zone %v: code %v, text %v", f.Name, st.ParamStr(cards.PKExcludeZone), z, got, want)
					}
				}
				sv := staticView{Params: st.Params, PS: st.ParamSetOf()}
				want := staticConditionCodes.Code(strings.TrimSpace(st.ParamStr(cards.PKCondition)))
				if got := sv.condition(); got != want {
					t.Errorf("%s Condition$ %q: code %v, text %v", f.Name, st.ParamStr(cards.PKCondition), got, want)
				}
			}
			for i := range f.Triggers {
				tr := f.Triggers[i]
				checkZoneParams(f.Name+" trigger", tr.Param, tr.ParamCode)
			}
			for i := range f.Repls {
				r := f.Repls[i]
				checkZoneParams(f.Name+" replacement", r.Param, r.ParamCode)
			}
		}
	}
	// A node built after load (no ParamSet) answers through the coder.
	tr := cards.Trigger{Mode: "ChangesZone", Params: map[string]string{"Origin": "Graveyard, Exile", "Destination": "Any"}}
	if c, ok := tr.ParamCode(cards.PKOrigin); !ok || !effects.ZoneList(c).Admits(state.ZExile) || effects.ZoneList(c).Admits(state.ZHand) {
		t.Fatalf("unbound Origin$ code = %v, %v", c, ok)
	}
	if c, ok := tr.ParamCode(cards.PKDestination); !ok || !effects.Destination(c).IsAny() {
		t.Fatalf("unbound Destination$ code = %v, %v", c, ok)
	}
	if _, ok := tr.ParamCode(cards.PKExcludedOrigins); ok {
		t.Fatal("absent key reported present")
	}
}
