package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestCostStaticWaterbendXProbes covers the RaiseCost Cost$ Waterbend<X> rows
// (ticket levelb-cost-waterbend-x): the cast ANNOUNCES X = 1 and pays a real
// waterbend of one as one generic beyond the printed price (waterbend lets a
// player tap artifacts and creatures for {1}, it never requires it, CR
// 701.67a), and the spell's X-dependent effect reads that announced X.
//
// The sensitivity follows TestCostStaticRaiseTokenProbes's mute shape, with
// one difference the X announcement forces: without the static the cast never
// poses an X ask at all, so a muted scenario that kept the "X = 1" answer
// would fail there on an unconsumed answer rather than on the payment. The
// test therefore runs two mutes of the same item:
//
//   - the announcement kept and the payment muted (the printed price) must
//     FAIL with the static present: an engine or generator that stops
//     enforcing the Waterbend<X> additional cost passes that cast and the
//     observation fails;
//   - the same printed price with the announcement dropped must cast cleanly
//     once the static is gone, so the failure above is the additional cost's
//     and nothing else in the mute.
func TestCostStaticWaterbendXProbes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, mana, printed, target string }{
		{"Crashing Wave", "UUC", "UU", "p1:Grizzly Bears"},
		{"Foggy Swamp Visions", "CBBC", "CBB", "p0:Grizzly Bears"},
		{"Waterbender's Restoration", "UUC", "UU", "p0:Grizzly Bears"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := staticCostItem(t, reg, tc.name)
			cast := probeCast(t, it)
			if cast.Card != "p0:"+tc.name || cast.Mana != tc.mana {
				t.Fatalf("probe = %+v paying %q, want cast %s paying %q", cast, cast.Mana, "p0:"+tc.name, tc.mana)
			}
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: probe %s absent", tc.name)
			}
			pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
			if why != "" || pool != tc.printed {
				t.Fatalf("precondition: %s prints %q (%s), table says %q", tc.name, pool, why, tc.printed)
			}
			if got := len(cast.Targets); got != 1 || cast.Targets[0] != tc.target {
				t.Fatalf("precondition: cast at X = 1 targets %v, want exactly [%s] (the X-dependent slot)", cast.Targets, tc.target)
			}
			found := false
			for _, a := range cast.Answers {
				for _, pick := range a.Pick {
					found = found || pick == "X = 1"
				}
			}
			if !found {
				t.Fatalf("precondition: cast answers %v never announce X = 1", cast.Answers)
			}
			found = false
			for _, step := range it.XAnswers {
				for _, a := range step {
					found = found || (a.Kind == "choice" && a.Value == "X=1")
				}
			}
			if !found {
				t.Fatalf("precondition: XMage answers %v never carry the X=1 choice", it.XAnswers)
			}
			if res := runSteps(t, reg, it.Scenario, it.Steps); len(res.Fails) != 0 {
				t.Fatalf("waterbend-X probe fails with the static present: %v", res.Fails)
			}
			// Sensitivity, arm 1: the payment muted, the announcement kept.
			muted := waterbendXMute(it.Scenario, tc.name, tc.printed, true)
			if res := runSteps(t, reg, muted, muted.Steps); len(res.Fails) == 0 {
				t.Fatalf("probe is not sensitive to %s's Waterbend<X> payment", tc.name)
			}
			// Sensitivity, arm 2: the same printed price with the
			// announcement dropped casts cleanly without the static.
			unannounced := waterbendXMute(it.Scenario, tc.name, tc.printed, false)
			if res := runSteps(t, withoutStatics(reg, tc.name), unannounced, unannounced.Steps); len(res.Fails) != 0 {
				t.Fatalf("unannounced probe still fails with the static gone: %v", res.Fails)
			}
		})
	}
}

// waterbendXMute drops the Waterbend<X> additional-cost payment from sc: the
// cast step pays the printed price only, and with the announcement dropped it
// also loses the "X = 1" answer and the X-dependent targets (without the
// static the X-dependent slot bounds at zero).
func waterbendXMute(sc oraclegen.Scenario, name, printed string, keepX bool) oraclegen.Scenario {
	out := sc
	out.Steps = append([]oraclegen.Step(nil), sc.Steps...)
	for i, s := range out.Steps {
		if s.Op != "cast" || s.Card != "p0:"+name {
			continue
		}
		s.Mana = printed
		if !keepX {
			s.Answers = nil
			s.Targets = nil
		}
		out.Steps[i] = s
	}
	return out
}

// The corpus cards this table serves are real RaiseCost Waterbend<X> carries:
// without this guard a corpus move that renames one away would leave the test
// above silently covering two cards instead of three.
func TestCostStaticWaterbendXCorpusCarriers(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Crashing Wave", "Foggy Swamp Visions", "Waterbender's Restoration"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s missing from corpus", name)
		}
		found := false
		for _, st := range c.Faces[0].Statics {
			found = found || st.Params["Cost"] == "Waterbend<X>"
		}
		if !found {
			t.Fatalf("precondition: %s carries no Cost$ Waterbend<X> static", name)
		}
	}
}
