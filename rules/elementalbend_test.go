package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestAllFourBendCountHead(t *testing.T) {
	e := layerEngine(t)
	reg := testutil.CorpusRegistry(t)
	card := tlaCorpusCard(t, reg, "Avatar Aang")
	id := onBoardCard(t, e, 0, card)
	ctx := &effects.Ctx{Controller: 0, Source: id}
	body := "Count$AllFourBend.1.0"
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("precondition/count before bends = %d (ok %v), want evaluated zero", n, ok)
	}
	for i, verb := range []string{"water", "earth", "fire", "air"} {
		e.emit(events.Event{Kind: events.ElementalBend, Obj: id, Player: 0, Text: verb})
		want := int32(0)
		if i == 3 {
			want = 1
		}
		if got, ok := effects.EvalCountOK(e, ctx, body); !ok || got != want {
			t.Fatalf("after %s: count = %d (ok %v), want %d", verb, got, ok, want)
		}
	}
}

func TestElementalBendAvatarAangTriggerAndTransform(t *testing.T) {
	for _, nBends := range []int{3, 4} {
		t.Run(map[int]string{3: "three", 4: "all-four"}[nBends], func(t *testing.T) {
			e := layerEngine(t)
			reg := testutil.CorpusRegistry(t)
			card := tlaCorpusCard(t, reg, "Avatar Aang")
			id := onBoardCard(t, e, 0, card)
			obj := e.G.Obj(id)
			if obj == nil || obj.Zone != state.ZBattlefield || obj.Face().Name != "Avatar Aang" {
				t.Fatalf("precondition: Avatar Aang not on battlefield/front face: %+v", obj)
			}
			for i, verb := range []string{"water", "earth", "fire", "air"} {
				if i == nBends {
					break
				}
				e.emit(events.Event{Kind: events.ElementalBend, Obj: id, Player: 0, Text: verb})
				if len(e.pendingTriggers) == 0 && len(e.G.Stack) == 0 {
					t.Fatalf("precondition: %s marker failed to queue Avatar Aang's trigger", verb)
				}
				airbendSettle(t, e, 0, 100)
			}
			draws := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Draw && ev.Player == 0 {
					draws++
				}
			}
			if draws < nBends {
				t.Fatalf("draw triggers = %d, want at least %d", draws, nBends)
			}
			wantFace := "Avatar Aang"
			if nBends == 4 {
				wantFace = "Aang, Master of Elements"
			}
			if got := e.G.Obj(id).Face().Name; got != wantFace {
				t.Fatalf("face after %d bends = %q, want %q", nBends, got, wantFace)
			}
		})
	}
}

func TestElementalBendCorpusCensusAndSupport(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Avatar Aang")
	if !ok {
		t.Fatal("corpus precondition: Avatar Aang missing")
	}
	if missing := reg.Unsupported(card, effects.Supported()); len(missing) != 0 {
		t.Fatalf("Avatar Aang remains unsupported: %v", missing)
	}
	if len(card.Faces) == 0 {
		t.Fatal("precondition: Avatar Aang has no faces")
	}
	found := false
	for _, f := range card.Faces {
		for _, tr := range f.Triggers {
			if tr.Mode == "ElementalBend" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("census precondition: Avatar Aang lacks ElementalBend")
	}
}

// bendMarkerCount counts the ElementalBend marker events in the log carrying
// the given verb and player.
func bendMarkerCount(e *Engine, verb string, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ElementalBend && ev.Text == verb && ev.Player == p {
			n++
		}
	}
	return n
}

// TestElementalBendEmittedByEachRealVerb drives each of the four bend actions
// through its REAL engine path (not the synthetic marker the trigger tests
// emit) and asserts the marker the Mode$ ElementalBend matcher fires on is in
// the log exactly once, named with the right verb and the bending seat:
//
//   - water: a real Water Whip cast paying waterbend {5} with five creatures
//     (the payCast tap-commit loop, rules/cast.go);
//   - fire: Fire Sages attacking, whose Firebending 1 trigger adds {R} (the
//     DB$ Mana body's SubAbility$ marker, cards/kw_firebending.go);
//   - air: Airbending Lesson exiling the target (effAirbend);
//   - earth: Ba Sing Se's activated ability animating a Forest (effEarthbend).
func TestElementalBendEmittedByEachRealVerb(t *testing.T) {
	t.Run("water", func(t *testing.T) {
		e, _ := raiseEngine(t, []string{"Water Whip"}, nil)
		whip := blightMove(t, e, 0, "Water Whip", state.ZHand)
		var bears []state.ObjID
		for i := 0; i < 4; i++ {
			bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
		}
		addMana(t, e, 0, "UU")
		if raiseCastOffered(e, whip) {
			t.Fatal("precondition: Water Whip offered with only four waterbendable creatures")
		}
		bears = append(bears, blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield))
		if !raiseCastOffered(e, whip) {
			t.Fatalf("precondition: Water Whip not offered with five creatures: %+v", e.Pending().Options)
		}
		if got := bendMarkerCount(e, "water", 0); got != 0 {
			t.Fatalf("precondition: %d water markers before the cast, want 0", got)
		}
		raiseCast(t, e, whip)
		raiseDrive(t, e, func(d *decision.Decision) []int {
			var pick []int
			for _, o := range d.Options {
				if o.Kind == "waterbend_generic" {
					pick = append(pick, o.Index)
				}
			}
			return pick
		})
		if z := raiseZone(e, whip); z != state.ZStack {
			t.Fatalf("Water Whip in %s after the waterbend cast, want stack", z)
		}
		if got := bendMarkerCount(e, "water", 0); got != 1 {
			t.Fatalf("waterbend payment emitted %d ElementalBend water markers, want exactly 1", got)
		}
	})
	t.Run("fire", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		e, _, _ := newFixtureDeck(t, 30, "Name:Placeholder\nManaCost:0\nTypes:Creature Bear\nPT:1/1\nOracle:x\n")
		sages := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Fire Sages"))
		if got := bendMarkerCount(e, "fire", 0); got != 0 {
			t.Fatalf("precondition: %d fire markers before the attack, want 0", got)
		}
		tlaAttackTriggerPump(t, e, sages)
		if got := e.G.Players[0].Pool[state.MR]; got != 1 {
			t.Fatalf("precondition: Firebending 1 added R=%d to the pool, want 1", got)
		}
		if got := bendMarkerCount(e, "fire", 0); got != 1 {
			t.Fatalf("the Firebending attack trigger emitted %d ElementalBend fire markers, want exactly 1", got)
		}
	})
	t.Run("air", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		e, _ := searchEngine(t, reg, "Airbending Lesson")
		bear := onBoardCard(t, e, 1, tlaCorpusCard(t, reg, "Grizzly Bears"))
		if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: Grizzly Bears on seat 1's battlefield: %+v", o)
		}
		if _, ok := tlaCastByName(t, e, "Airbending Lesson", "WWW"); !ok {
			t.Fatal("precondition: Airbending Lesson not castable for {2}{W}")
		}
		d := e.Pending()
		if d != nil && d.Kind == decision.KTarget {
			picked := false
			for _, o := range d.Options {
				if o.Obj == bear {
					submitChoices(t, e, o.Index)
					picked = true
					break
				}
			}
			if !picked {
				t.Fatalf("precondition: airbend target ask did not offer the Bear: %+v", d.Options)
			}
		}
		passUntilStackEmpty(t, e, 40)
		if got := e.G.Obj(bear).Zone; got != state.ZExile {
			t.Fatalf("precondition: airbent Bear zone = %v, want ZExile", got)
		}
		if got := bendMarkerCount(e, "air", 0); got != 1 {
			t.Fatalf("the resolved airbend emitted %d ElementalBend air markers, want exactly 1", got)
		}
	})
	t.Run("earth", func(t *testing.T) {
		reg := testutil.CorpusRegistry(t)
		e, _ := searchEngine(t, reg, "Ba Sing Se", "Grizzly Bears")
		_, forest := earthbendActivateBaSingSe(t, e, "Forest")
		assertEarthbendAnimated(t, e, forest, 2)
		if got := bendMarkerCount(e, "earth", 0); got != 1 {
			t.Fatalf("the resolved earthbend emitted %d ElementalBend earth markers, want exactly 1", got)
		}
	})
}
