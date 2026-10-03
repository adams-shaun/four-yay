package effects

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpusRevealHandCard builds a 2-seat host whose seat 1 hand holds the
// four named cards (in hand order), and returns the host plus those ids in
// the hand's zone order — the order a whole-hand reveal Note must carry.
func revealHandBoard(t *testing.T, handNames ...string) (*fakeHost, []state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	var hand []state.ObjID
	for _, n := range handNames {
		c := mkCard(t, "Name:"+n+"\nTypes:Creature\nPT:1/1\nOracle:x\n")
		id := h.g.AddObject(c, 1).ID
		hand = append(hand, id)
		h.g.Obj(id).Zone = state.ZHand
	}
	h.g.SetZone(state.ZHand, 1, hand)
	return h, hand
}

// corpusSAByAPI returns the corpus card's compiled face SA with the given
// Kind+API, failing the test when the card or the shape is missing (the
// corpus pin moved, not the behaviour under test).
func corpusSAByAPI(t *testing.T, cardName, kind, api string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup(cardName)
	if !ok {
		t.Skipf("corpus missing card %q", cardName)
	}
	for _, face := range card.Faces {
		for _, a := range face.Abilities {
			if a.Kind == kind && a.API == api {
				return a
			}
		}
		for _, tr := range face.Triggers {
			if tr.Effect != nil && tr.Effect.Kind == kind && tr.Effect.API == api {
				return tr.Effect
			}
		}
	}
	t.Fatalf("corpus card %q carries no %s$ %s SA", cardName, kind, api)
	return nil
}

// TestThoughtKnotSeerRevealHandRevealsTheWholeHand pins the no-Look$ public
// shape on its real compiled corpus SA — Thought-Knot Seer's ETB trigger
// effect (DB$ RevealHand | ValidTgts$ Opponent), the same card the golden
// chain heads seat (eldrazi-stompy), which is why TestHeads moves at the
// seat counts where eldrazi-stompy plays.
func TestThoughtKnotSeerRevealHandRevealsTheWholeHand(t *testing.T) {
	h, hand := revealHandBoard(t, "Endless One", "Skitterskin", "Bearer")
	trig := corpusSAByAPI(t, "Thought-Knot Seer", "DB", "RevealHand")
	if _, has := trig.Params["NumCards"]; has {
		t.Fatal("corpus pin moved: Thought-Knot Seer's TrigReveal now carries NumCards$")
	}
	if trig.Params["ValidTgts"] != "Opponent" {
		t.Fatalf("corpus pin moved: ValidTgts = %q", trig.Params["ValidTgts"])
	}
	reg := testutil.CorpusRegistry(t)
	src := corpusObject(t, reg, h.g, "Thought-Knot Seer")
	ctx := &Ctx{Source: src.ID, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}, TargetsOffered: true}
	Resolve(h, ctx, trig)

	// Two Notes: the public whole-hand reveal, then the sub-ability's
	// hand-move. Pre-rv2b-r2 the DBExile sub-ability silently no-op'd (the
	// DefinedPlayer$ target is a PLAYER the object path skipped); now it
	// asks (the chooser is seat 0, Chooser$ You) and the no-host stand-in
	// answers deterministically with its own Note.
	var notes []*events.Event
	for i := range h.log {
		if h.log[i].Kind == events.Note {
			notes = append(notes, &h.log[i])
		}
	}
	if len(notes) != 2 {
		t.Fatalf("%d Notes, want the reveal + the sub-ability's hand-move stand-in: %+v", len(notes), h.log)
	}
	note := notes[0]
	if note.Secret {
		t.Fatal("the public no-Look$ shape must reveal publicly")
	}
	if !slices.Equal(note.IDs, hand) {
		t.Fatalf("Note ids = %v, want the WHOLE hand %v", note.IDs, hand)
	}
	r9 := notes[1]
	if r9.Player != 0 || r9.Text != "moves the first matching card(s) from hand (no engine host to ask)" {
		t.Fatalf("hand-move Note = %+v, want the chooser's (seat 0) R-9 stand-in", r9)
	}
	// The sub-ability now really moves the first eligible nonland card from
	// the revealed hand to exile (the pre-fix silent no-op is gone).
	if o := h.g.Obj(hand[0]); o.Zone != state.ZExile {
		t.Fatalf("first eligible card on %s, want exile (DBExile no longer no-ops)", o.Zone)
	}
	if o := h.g.Obj(hand[1]); o.Zone != state.ZHand {
		t.Fatalf("second eligible card moved: on %s", o.Zone)
	}
}

// TestRevealAndPeekKeepCountBehaviour pins the siblings the fix deliberately
// does NOT touch: without NumCards$ the card-selector family Reveal and the
// library peek PeekAndReveal keep the shared default of ONE card, and a
// RevealHand that DOES write NumCards$ (a future script; zero in the corpus)
// wins over the whole-hand amount.
func TestRevealAndPeekKeepCountBehaviour(t *testing.T) {
	h, hand := revealHandBoard(t, "Alpha", "Beta", "Gamma")
	mtn := mkCard(t, "Name:Mountain\nTypes:Land\nOracle:x\n")
	lib := h.g.AddObject(mtn, 0).ID
	h.g.Obj(lib).Zone = state.ZLibrary
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{lib})

	cases := []struct {
		name string
		line string
		zone state.Zone
		want []state.ObjID
	}{
		{"Reveal default stays 1", "SP$ Reveal | Defined$ Opponent", state.ZHand, hand[:1]},
		{"PeekAndReveal default stays 1", "SP$ PeekAndReveal | Defined$ You", state.ZLibrary, []state.ObjID{lib}},
		{"RevealHand with NumCards$ honours it", "SP$ RevealHand | Defined$ Opponent | NumCards$ 2", state.ZHand, hand[:2]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h2 := *h
			h2.log = nil
			Resolve(&h2, &Ctx{Controller: 0}, sa(t, tc.line))
			var note *events.Event
			for i := range h2.log {
				if h2.log[i].Kind == events.Note {
					if note != nil {
						t.Fatalf("more than one reveal Note: %+v", h2.log)
					}
					note = &h2.log[i]
				}
			}
			if note == nil {
				t.Fatalf("no reveal Note emitted: %+v", h2.log)
			}
			if !slices.Equal(note.IDs, tc.want) {
				t.Fatalf("Note ids = %v, want %v", note.IDs, tc.want)
			}
		})
	}
}
