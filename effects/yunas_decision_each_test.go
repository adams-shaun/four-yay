package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestYunasDecisionPilgrimageEachCreatureAndLand(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("no corpus")
	}
	yuna, ok := reg.Lookup("Yuna's Decision")
	if !ok || len(yuna.Faces) == 0 {
		t.Fatal("Yuna's Decision is absent from the corpus")
	}
	ability := cards.ResolveSVar(yuna.Faces[0].SVars, "DBChangeZone")
	if ability == nil || ability.API != "ChangeZone" || ability.Params["ChangeType"] != "EACH Creature & Land" {
		t.Fatalf("DBChangeZone = %+v, want Yuna's EACH Creature & Land ChangeZone", ability)
	}
	creature, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("Grizzly Bears is absent from the corpus")
	}
	land, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("Forest is absent from the corpus")
	}

	for _, tc := range []struct {
		name            string
		includeCreature bool
		includeLand     bool
		wantOptions     int
		wantMax         int
	}{
		{name: "creature only", includeCreature: true, wantOptions: 1, wantMax: 1},
		{name: "land only", includeLand: true, wantOptions: 1, wantMax: 1},
		{name: "both", includeCreature: true, includeLand: true, wantOptions: 2, wantMax: 2},
		{name: "none", wantOptions: 0, wantMax: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			src := h.g.AddObject(yuna, 0)
			var hand []state.ObjID
			var creatureID, landID state.ObjID
			if tc.includeCreature {
				creatureID = h.g.AddObject(creature, 0).ID
				hand = append(hand, creatureID)
			}
			if tc.includeLand {
				landID = h.g.AddObject(land, 0).ID
				hand = append(hand, landID)
			}
			eachSetZone(h.g, 0, state.ZHand, append([]state.ObjID(nil), hand...)...)
			if len(hand) != tc.wantOptions {
				t.Fatalf("bad fixture: hand has %d cards, want %d", len(hand), tc.wantOptions)
			}
			for _, id := range hand {
				if o := h.g.Obj(id); o == nil || o.Zone != state.ZHand {
					t.Fatalf("precondition failed: eligible card %d is not in hand: %+v", id, o)
				}
			}

			sh := &suspendHost{fakeHost: *h}
			ctx := &Ctx{Source: src.ID, Controller: 0}
			effChangeZone(sh, ctx, ability)
			d := sh.asked
			// The ability's explicit Optional$ True marker poses Forge's
			// confirm-before-pick gate first -- even over an empty hand (Forge
			// asks before the fetch list is consulted).
			if d == nil || d.ResumeKind != "hand_move_confirm" {
				t.Fatalf("first ask = %+v, want the hand_move_confirm gate (Optional$ True asks before the pick)", d)
			}
			sh.suspended = false // the double's suspension flag: the resume is a fresh pass
			sh.asked = nil
			effChangeZone(sh, &Ctx{Source: src.ID, Controller: 0,
				HandMoveConfirmDone: true, HandMoveConfirm: "yes", HandMoveConfirmTarget: 0}, ability)
			d = sh.asked
			if tc.wantOptions == 0 {
				if d != nil {
					t.Fatalf("empty eligible set posed a decision after its accepted confirmation: %+v", d)
				}
				for _, ev := range sh.log {
					if ev.Kind == events.Note && ev.Text == "unimplemented API ChangeZone" {
						t.Fatalf("ChangeZone handler did not run: %+v", sh.log)
					}
				}
				return
			}
			if d == nil {
				t.Fatal("no choice was posed for eligible hand cards")
			}
			if d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
				t.Fatalf("decision = %+v, want KChoose hand_move", d)
			}
			if d.Min != 0 || d.Max != tc.wantMax || len(d.Options) != tc.wantOptions {
				t.Fatalf("choice range/options = %d..%d/%d, want 0..%d/%d: %+v", d.Min, d.Max, len(d.Options), tc.wantMax, tc.wantOptions, d)
			}
			groups := map[state.ObjID]string{}
			for _, option := range d.Options {
				groups[option.Obj] = option.Group
			}
			if tc.includeCreature && groups[creatureID] != "0" {
				t.Fatalf("creature option group = %q, want EACH clause 0: %+v", groups[creatureID], d.Options)
			}
			if tc.includeLand && groups[landID] != "1" {
				t.Fatalf("land option group = %q, want EACH clause 1: %+v", groups[landID], d.Options)
			}
			if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: optionIndexes(d.Options)}); err != nil {
				t.Fatalf("one-per-present-type answer rejected: %v", err)
			}
		})
	}
}

func optionIndexes(options []decision.Option) []int {
	out := make([]int, len(options))
	for i, option := range options {
		out[i] = option.Index
	}
	return out
}
