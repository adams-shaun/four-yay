package templates

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The MayLookAt$ statics lookAtLibraryTopItem cannot serve. The Case rows'
// permission is gated on the source's own Case being solved -- the Case
// solves itself at the end step, so the scenario carries the counted
// permanents and passes through the end step (static#0.1 and #0.2, the MKM
// Case's "Solved --" statics). Found Footage's and Keeper of the Lens's
// permission is over a face-down battlefield PERMANENT, not the library top,
// so the scenario casts a plain Disguise card face down and asserts the
// runner's look_at expectation over it. Every assertion here is gorge's own
// permission read; the control (source gone, want unchanged) must FAIL, or
// the item would pass with the whole feature unregistered.

// lookAtItem generates one static.continuous requirement's item.
func lookAtItem(t *testing.T, reg *cards.Registry, card, key string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("precondition: %s absent from corpus", card)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		if r.Sub != "static.continuous" {
			t.Fatalf("precondition: %s %s = %s, want static.continuous", card, key, r.Sub)
		}
		it, skip := GenerateB(reg, card, r)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", card, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s has no requirement %s", card, key)
	return oraclegen.Item{}
}

// withoutLookAtSource is the item's scenario with the card under test gone
// from p0's battlefield and every assertion kept: the want=true look-at
// permission must stop holding, or it was never the static's doing.
func withoutLookAtSource(it oraclegen.Item) oraclegen.Item {
	out := it
	out.Setup = cloneOracleSetup(it.Setup)
	p0 := out.Setup["p0"]
	p0.Battlefield = removeFixture(p0.Battlefield, it.Card)
	out.Setup["p0"] = p0
	out.Steps = append([]oraclegen.Step(nil), it.Steps...)
	return out
}

func TestStaticLookAtSolvedCaseGenerates(t *testing.T) {
	for _, tc := range []struct{ card, key string }{
		{"Case of the Locked Hothouse", "static#0.1"},
		{"Case of the Locked Hothouse", "static#0.2"},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			reg := loadGenRegistry(t)
			it := lookAtItem(t, reg, tc.card, tc.key)
			last := it.Steps[len(it.Steps)-1]
			if last.Step != "begin-combat" {
				t.Fatalf("precondition: checkpoint %q, want begin-combat", last.Step)
			}
			if len(last.Expect) != 1 || last.Expect[0].LookAtLibraryTop["p0"] != true {
				t.Fatalf("item lacks the look_at_library_top p0=true assertion: %+v", last.Expect)
			}
			// The fixture supplies the state the solve trigger reads: the Case
			// and exactly the counted permanents (seven lands) on p0's
			// battlefield. A setup that placed fewer would replay the unsolved
			// Case and the want=true assertion would fail loudly.
			p0 := it.Setup["p0"]
			if !slices.Contains(p0.Battlefield, it.Card) || len(p0.Battlefield) != 8 {
				t.Fatalf("precondition: p0 battlefield %v, want the Case plus seven lands", p0.Battlefield)
			}
			if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("with the source: ok=%v %v", ok, res.Fails)
			}
			// The control keeps the lands and the steps but drops the Case:
			// nothing solves, the gate never opens, the want=true assertion
			// must fail.
			if res, ok := runStatic(reg, withoutLookAtSource(it).Scenario); !ok || len(res.Fails) == 0 {
				t.Fatalf("without the source the control must fail, got ok=%v fails=%v", ok, res.Fails)
			}
		})
	}
}

func TestStaticLookAtFaceDownGenerates(t *testing.T) {
	for _, tc := range []struct{ card, key, probe string }{
		{"Found Footage", "static#0.0", "Bolrac-Clan Basher"},
		{"Keeper of the Lens", "static#0.0", "Bolrac-Clan Basher"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			reg := loadGenRegistry(t)
			it := lookAtItem(t, reg, tc.card, tc.key)
			last := it.Steps[len(it.Steps)-1]
			if last.Step != "begin-combat" {
				t.Fatalf("precondition: checkpoint %q, want begin-combat", last.Step)
			}
			want := map[string]string{"p0": "p1:" + tc.probe}
			if len(last.Expect) != 1 || last.Expect[0].LookAt == nil || last.Expect[0].LookAt["p0"] != want["p0"] {
				t.Fatalf("item lacks the look_at p0 probe assertion: %+v", last.Expect)
			}
			// The fixture casts the probe face down from p1's hand and the
			// source is on p0's battlefield from setup; a setup without either
			// would replay a checkpoint the look_at assertion cannot judge.
			if !slices.Contains(it.Setup["p0"].Battlefield, it.Card) {
				t.Fatalf("precondition: %s absent from p0's battlefield: %v", it.Card, it.Setup["p0"].Battlefield)
			}
			if !slices.Contains(it.Setup["p1"].Hand, tc.probe) {
				t.Fatalf("precondition: %s absent from p1's hand: %v", tc.probe, it.Setup["p1"].Hand)
			}
			if res, ok := runStatic(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("with the source: ok=%v %v", ok, res.Fails)
			}
			if res, ok := runStatic(reg, withoutLookAtSource(it).Scenario); !ok || len(res.Fails) == 0 {
				t.Fatalf("without the source the control must fail, got ok=%v fails=%v", ok, res.Fails)
			}
		})
	}
}

// TestStaticLookAtRemainingSkips keeps the look-at rows the two new paths
// cannot serve in their named skip: the hand reveals and the remembered /
// chosen exile grants are neither an IsSolved-gated library-top grant nor a
// face-down-permanent grant, and no path here may widen to claim them.
func TestStaticLookAtRemainingSkips(t *testing.T) {
	for _, tc := range []struct{ card, key string }{
		{"Kheru Mind-Eater", "static#0.0"},
		{"Telepathy", "static#0.0"},
		{"Wandering Eye", "static#0.0"},
		{"Shared Fate", "static#0.0"},
		{"Bane Alley Broker", "static#0.0"},
		{"Ranger Class", "static#0.0"},
		{"Colfenor's Plans", "static#0.0"},
		{"Wisedrafter's Will", "static#0.0"},
		{"Seer's Vision", "static#0.0"},
		{"Grimoire Thief", "static#0.0"},
		{"Zur's Weirding", "static#0.0"},
		{"Revelation", "static#0.0"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			reg := loadGenRegistry(t)
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("precondition: %s absent from corpus", tc.card)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != "static.continuous" {
					t.Fatalf("precondition: %s %s = %s", tc.card, tc.key, r.Sub)
				}
				_, skip := GenerateB(reg, tc.card, r)
				if skip == nil || skip.Reason != "static look-at not observable" {
					t.Errorf("%s %s skip = %v, want the named look-at skip", tc.card, tc.key, skip)
				}
				return
			}
			t.Fatalf("precondition: %s has no requirement %s", tc.card, tc.key)
		})
	}
}
