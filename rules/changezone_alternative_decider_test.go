package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestUnchartedVoyageAlternativeDeciderOwnerChoosesTopOrBottom is the driver
// for api:ChangeZone's `AlternativeDecider$` parameter: the OWNER of the
// targeted card -- which for an opponent's creature is the OPPONENT, not the
// caster -- chooses whether the card goes to the top or the bottom of their
// own library. Before the parameter was read, the owner was never asked and
// the card always took the deterministic placement (bottom, where Move
// appends it), so the spell's whole choice was lost.
//
// The two sub-cases cover the two seats the ask can go to:
//   - targeting an OPPONENT's creature: the ask is posed to seat 1 (the owner)
//     and "bottom" leaves the card at the bottom of seat 1's library;
//   - targeting the CASTER's own creature: the ask is posed to seat 0 and
//     "top" puts the card on top of seat 0's library.
//
// Uncharted Voyage's `SubAbility$ DBSurveil` follows the choice, so the
// caster surveils 1 after the card's placement -- asserted through the
// Surveil event and the KArrange ask's chooser.
func TestUnchartedVoyageAlternativeDeciderOwnerChoosesTopOrBottom(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	voyage := mustCorpusCard(t, reg, "Uncharted Voyage")
	// An inline fixture creature, never a committed Forge script: it is the
	// target whose owner is asked.
	bear := card(t, "Name:Alternative Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	for _, tc := range []struct {
		name      string
		ownerSeat state.PlayerID
		answer    string
		wantTop   bool
	}{
		{name: "opponent_owner_chooses_bottom", ownerSeat: 1, answer: "bottom", wantTop: false},
		{name: "caster_owner_chooses_top", ownerSeat: 0, answer: "top", wantTop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Seat 0 holds the spell; the target creature is seeded into the
			// seat that must own it (seat 1 = opponent, seat 0 = caster).
			seat0 := []*cards.Card{voyage}
			seat1 := []*cards.Card{bear}
			if tc.ownerSeat == 0 {
				seat0 = append(seat0, bear)
				seat1 = []*cards.Card{}
			}
			e, cfg := tokenReplGameSeats(t, 91, seat0, seat1)

			// The target must be a real battlefield permanent of its owner:
			// moveSeededCard logs the MoveZone, so the setup replays.
			bearID := moveSeededCard(t, e, tc.ownerSeat, bear, state.ZBattlefield)
			if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield || o.Owner != tc.ownerSeat {
				t.Fatalf("precondition: target = %+v, want a battlefield creature owned by seat %d", o, tc.ownerSeat)
			}

			// Uncharted Voyage is {3}{U}; fund it and cast it by name.
			addMana(t, e, 0, "UUUU")
			castFixtureNamed(t, e, "Uncharted Voyage")

			// CR 601.2c: the target is chosen at cast.
			targetAsk := e.Pending()
			if targetAsk == nil || targetAsk.Kind != decision.KTarget {
				t.Fatalf("cast did not ask for the creature target: %+v", targetAsk)
			}
			targetIdx := -1
			for _, o := range targetAsk.Options {
				if o.Obj == bearID {
					targetIdx = o.Index
				}
			}
			if targetIdx < 0 {
				t.Fatalf("precondition: the seeded bear %d is not an offered target: %+v", bearID, targetAsk.Options)
			}
			submitChoices(t, e, targetIdx)

			// The owner, not the caster, is asked which library position.
			ask := passPriorityUntil(t, e, decision.KChoose)
			if ask.Player != tc.ownerSeat {
				t.Fatalf("alternative asker = seat %d, want the OWNER seat %d", ask.Player, tc.ownerSeat)
			}
			if ask.ResumeKind != "changezone_alternative" {
				t.Fatalf("alternative ask ResumeKind = %q, want changezone_alternative", ask.ResumeKind)
			}
			if len(ask.Options) != 2 {
				t.Fatalf("alternative ask offers %d options, want top/bottom: %+v", len(ask.Options), ask.Options)
			}
			choice := -1
			for _, o := range ask.Options {
				if o.Label == tc.answer {
					choice = o.Index
				}
			}
			if choice < 0 {
				t.Fatalf("answer %q not offered: %+v", tc.answer, ask.Options)
			}
			submitChoices(t, e, choice)

			// The move has applied and the Surveil sub-ability is pending, so
			// this is the point at which the placement is settled.
			lib := e.G.Zone(state.ZLibrary, tc.ownerSeat)
			found := -1
			for i, id := range lib {
				if id == bearID {
					found = i
				}
			}
			if found < 0 {
				t.Fatalf("target %d is not in seat %d's library after the choice: %v", bearID, tc.ownerSeat, lib)
			}
			wantPos := len(lib) - 1
			if tc.wantTop {
				wantPos = 0
			}
			if found != wantPos {
				t.Fatalf("target library position = %d of %d, want %d (answer %q)", found, len(lib), wantPos, tc.answer)
			}

			// The Surveil 1 sub-ability follows: the CASTER chooses what to
			// keep, and a Surveil event lands.
			arrange := passPriorityUntil(t, e, decision.KArrange)
			if arrange.Player != 0 {
				t.Fatalf("Surveil chooser = seat %d, want the caster seat 0", arrange.Player)
			}
			submitChoices(t, e) // keep every card (Min 0)

			sawSurveil := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.Surveil {
					sawSurveil = true
				}
			}
			if !sawSurveil {
				t.Fatal("Uncharted Voyage's Surveil 1 sub-ability emitted no Surveil event")
			}

			// The whole resolution replays byte-identically.
			replayCheck(t, e, cfg)
		})
	}
}

// TestAlternativeDeciderSecondFromTopStaysLoudAndUnasked pins the boundary of
// the new support: the 6 corpus AlternativeDecider cards whose PRIMARY
// position is LibraryPosition$ 1 ("second from top") are NOT the top-or-
// bottom shape this engine can place. They must stay loud -- a Note naming
// the unsupported shape -- and must NOT pose the alternative ask, whose two
// options (top/bottom) would offer a choice the card never printed.
//
// Deem Inferior is the representative: without the shape gate the ask is
// posed (suspending on a KChoose this drain cannot answer) and the top/bottom
// placement would be taken; with it, the resolution completes, the target
// sits at the bottom (the engine's pre-existing placement for this path,
// unchanged) and the owner is never asked.
func TestAlternativeDeciderSecondFromTopStaysLoudAndUnasked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deem := mustCorpusCard(t, reg, "Deem Inferior")
	bear := card(t, "Name:Alternative Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e, cfg := tokenReplGameSeats(t, 92, []*cards.Card{deem}, []*cards.Card{bear})
	bearID := moveSeededCard(t, e, 1, bear, state.ZBattlefield)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield || o.Owner != 1 {
		t.Fatalf("precondition: target = %+v, want a battlefield creature owned by seat 1", o)
	}

	addMana(t, e, 0, "UUUU")
	castFixtureNamed(t, e, "Deem Inferior")
	targetAsk := e.Pending()
	if targetAsk == nil || targetAsk.Kind != decision.KTarget {
		t.Fatalf("cast did not ask for the creature target: %+v", targetAsk)
	}
	targetIdx := -1
	for _, o := range targetAsk.Options {
		if o.Obj == bearID {
			targetIdx = o.Index
		}
	}
	if targetIdx < 0 {
		t.Fatalf("precondition: the seeded bear %d is not an offered target: %+v", bearID, targetAsk.Options)
	}
	submitChoices(t, e, targetIdx)

	// The whole resolution completes WITHOUT an alternative ask: a KChoose
	// with ResumeKind changezone_alternative would fail here.
	passUntilStackEmpty(t, e, 20)

	// The handler ran (loud): the shape Note is in the log.
	sawShapeNote := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "not the top-or-bottom library shape") {
			sawShapeNote = true
		}
		if ev.Kind == events.DecisionAsk && ev.Text == string(decision.KChoose) {
			t.Fatalf("the unsupported second-from-top shape posed the top/bottom ask: %+v", ev)
		}
	}
	if !sawShapeNote {
		t.Fatalf("no shape Note for the unsupported LibraryPosition$ 1 alternative: %+v", e.L.Events)
	}

	// The target moved (the effect still took its primary destination), at the
	// bottom -- the pre-existing placement for a path that reads no
	// LibraryPosition$.
	lib := e.G.Zone(state.ZLibrary, 1)
	if len(lib) == 0 || lib[len(lib)-1] != bearID {
		t.Fatalf("target not at the bottom of seat 1's library: %v", lib)
	}
	replayCheck(t, e, cfg)
}
