package rules

// Class census for the CR 611.3a / 613.4c "Other ... you control" static: a
// continuous P/T static whose Affected$ carries the `Other` qualifier must
// exclude its own source. Bard's Company (HOB 146, ticket
// cli-20261006T024353Z-c6ff08b6) prompted this: its resolved snapshot read
// 4/3 instead of the printed 2/3. The cause turned out to be the upstream
// Forge script's PT:4/3 typo (see
// testdata/oracle/base-characteristics/known-divergent/bards-company.json),
// NOT a self-application bug -- but the census pins the class so a future
// regression in the `Other` predicate cannot hide behind one card.
//
// The assertion is derived != base is a failure, not derived != printed:
// a card whose printed P/T is a characteristic-defining `*/*` has no
// constant to compare against, and a card whose static also names Card.Self
// (Vampire Nocturnus) is entitled to pump itself, so both are excluded.
//
// The census can fail: neutering predicates["Other"] (effects/filter.go) to
// always match makes 225 of the 261 carriers report derived > base.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// otherCensusFloor is the number of `Creature...Other...` P/T statics this
// census must find. It guards against a vacuous pass (a corpus that failed to
// load the statics, or a filter change that silently skips them): measured at
// the 2026-10-06 pin, 261 carriers qualify (262 minus Vampire Nocturnus,
// which also names Card.Self). Neutering predicates["Other"] makes 225 of
// those 261 report derived > base.
const otherCensusFloor = 200

// affectedNamesSelf reports whether any `,`-separated Affected$ alternative
// names the source itself: `Card.Self`, or a bare `Self`/`CARDNAME` in either
// the dot- or plus-separated qualifier list. Its presence anywhere in the
// affected set entitles the source to pump itself, so such a card does not
// belong in this census.
func affectedNamesSelf(aff string) bool {
	for _, alt := range strings.Split(aff, ",") {
		for _, term := range strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' }) {
			if term == "Self" || term == "CARDNAME" {
				return true
			}
		}
	}
	return false
}

// hasOtherPumpStatic reports whether the static both qualifies its Affected$
// with `Other` and grants a P/T change.
func hasOtherPumpStatic(s map[string]string) bool {
	if s["Affected"] == "" || !strings.Contains(s["Affected"], "Other") {
		return false
	}
	return s["AddPower"] != "" || s["AddToughness"] != "" ||
		s["SetPower"] != "" || s["SetToughness"] != ""
}

func TestCorpusOtherPumpStaticExcludesItsSource(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			carrier, selfNaming := false, false
			for _, s := range f.Statics {
				if s.Mode != "Continuous" || !hasOtherPumpStatic(s.Params) {
					continue
				}
				carrier = true
				selfNaming = selfNaming || affectedNamesSelf(s.Params["Affected"])
			}
			if !carrier || selfNaming {
				continue
			}
			checked++
			name := f.Name
			e := layerEngine(t)
			id := onBoardCard(t, e, 0, c)
			if e.G.Obj(id).Zone != state.ZBattlefield {
				t.Fatalf("precondition: %s is not on the battlefield", name)
			}
			d := e.Derived(id)
			if d.Power != d.BasePower || d.Toughness != d.BaseToughness {
				t.Errorf("%s: its own 'Other ... you control' P/T static modified its source: base %d/%d, derived %d/%d (Affected$ must exclude the source, CR 611.3a/613.4c)",
					name, d.BasePower, d.BaseToughness, d.Power, d.Toughness)
			}
			break
		}
	}
	if checked < otherCensusFloor {
		t.Fatalf("census precondition: found %d 'Other ... you control' P/T statics, want at least %d; the census would pass vacuously", checked, otherCensusFloor)
	}
	t.Logf("checked %d 'Other ... you control' P/T statics", checked)
}
