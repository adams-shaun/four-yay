package rules

// Kernel-era restoration of effects/fx42_reentry_scope_test.go's
// TestNestedDiscardDoesNotInheritOuterAnswer on the REAL corpus Gruesome
// Discovery: its TgtChoose Discard chains a RevealYouChoose Discard
// (MorbidDiscard), and the two legs are mutually exclusive on the Morbid
// gate. The inner discard must never inherit the outer's answer: each leg,
// when it runs, poses its own ask, and a gated-off leg stays silent.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestKr1NestedDiscardDoesNotInheritOuterAnswer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	gd, ok := reg.Lookup("Gruesome Discovery")
	if !ok {
		t.Fatal("corpus has no Gruesome Discovery")
	}
	for i, morbid := range []bool{false, true} {
		morbid := morbid
		seed := uint64(240 + i)
		name := "not morbid: outer asks the discarding player, inner gated off"
		if morbid {
			name = "morbid: outer gated off, inner asks the caster fresh"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hand := kr1Cards(t, kr1Creatures("Frog", "Bird", "Cat", "Dog"))
			e, cfg, id := kr1Build(t, seed, gd, kr1Cards(t, []string{kr1Creature("Ox")}), hand)
			addMana(t, e, 0, "BBBB")
			kr1ClearHand(t, e, 1, 0)
			var ids []state.ObjID
			for _, n := range []string{"Frog", "Bird", "Cat", "Dog"} {
				ids = append(ids, kr1Put(t, e, 1, n, state.ZHand))
			}
			if morbid {
				ox := kr1Put(t, e, 0, "Ox", state.ZBattlefield)
				e.emit(events.Event{Kind: events.MoveZone, Obj: ox, From: state.ZBattlefield, To: state.ZGraveyard, Text: "died"})
			}
			d := kr1CastTargeting(t, e, id, true, 1, 0)
			if d == nil {
				t.Fatal("the resolution posed no discard ask")
			}
			if !morbid {
				if d.Player != 1 {
					t.Fatalf("outer discard chooser = seat %d, want the discarding seat 1", d.Player)
				}
				if p := kr1Pick(t, e, kr1OptIndex(t, d, ids[0]), kr1OptIndex(t, d, ids[1])); p != nil {
					t.Fatalf("a second ask after the outer choice (the gated-off inner ran or inherited): %+v", p)
				}
				for i, c := range ids {
					if inGy := kr1Zone(e, c) == state.ZGraveyard; inGy != (i < 2) {
						t.Fatalf("hand card %d in graveyard = %v, want %v", i, inGy, i < 2)
					}
				}
				replayCheck(t, e, cfg)
				return
			}
			if d.Player != 0 {
				t.Fatalf("inner discard chooser = seat %d, want the caster seat 0", d.Player)
			}
			if d.Kind != decision.KModes || d.Min != 2 || d.Max != 2 || len(d.Options) != 4 {
				t.Fatalf("inner discard decision = %+v, want a Min==Max==2 KModes over the whole four-card hand", d)
			}
			if p := kr1Pick(t, e, kr1OptIndex(t, d, ids[2]), kr1OptIndex(t, d, ids[3])); p != nil {
				t.Fatalf("unexpected further ask %+v", p)
			}
			for i, c := range ids {
				if inGy := kr1Zone(e, c) == state.ZGraveyard; inGy != (i >= 2) {
					t.Fatalf("hand card %d in graveyard = %v, want %v", i, inGy, i >= 2)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}
