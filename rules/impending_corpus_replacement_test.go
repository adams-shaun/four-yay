package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The corpus carrier pins the printed Impending shape through the same entry
// replacement path as the focused inline fixture.
func TestImpendingEntryTimeCountersApplyDoublingSeasonOnCorpusCard(t *testing.T) {
	t.Parallel()
	season := tokenReplCorpusCard(t, "Doubling Season")
	carrier := tokenReplCorpusCard(t, "Overlord of the Balemurk")
	e, cfg := tokenReplGame(t, 9877, season, carrier)

	seasonID := moveSeededCard(t, e, 0, season, state.ZBattlefield)
	if o := e.G.Obj(seasonID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Doubling Season is not on the battlefield")
	}
	id := findCardObj(t, e, 0, "Overlord of the Balemurk", state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Overlord is not in hand: %+v", o)
	}
	face := e.G.Obj(id).Face()
	if got, ok := face.KeywordParam("Impending"); !ok || got != "5:1 B" {
		t.Fatalf("precondition: corpus Impending param = %q (present %v), want %q", got, ok, "5:1 B")
	}

	addMana(t, e, 0, "CB") // exactly {1}{B}, the printed Impending cost
	submitChoices(t, e, castModeOption(t, e, id, "impended"))
	// The real Overlord has an optional graveyard return trigger; decline it
	// so this test stays focused on its entry counters.
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving Overlord's entry trigger")
		}
		if d.ResumeKind == "hidden_pick_confirm" {
			foundNo := false
			for _, option := range d.Options {
				if option.Kind == "no" {
					submitChoices(t, e, option.Index)
					foundNo = true
					break
				}
			}
			if !foundNo {
				t.Fatalf("Overlord optional trigger has no decline option: %+v", d.Options)
			}
			continue
		}
		passPriorityOnce(t, e)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("Overlord trigger stack did not drain: %d objects remain", len(e.G.Stack))
	}

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Overlord did not enter the battlefield: %+v", o)
	}
	if o.CastFlags&state.FlagImpending == 0 {
		t.Fatal("precondition: Overlord lacks paid-Impending provenance")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: paid Impending cost left %d mana", got)
	}
	if got := o.Counter("TIME"); got != 10 {
		t.Fatalf("Impending 5 entered with %d TIME counters under Doubling Season, want 10", got)
	}

	foundEntry, foundCastInfo := false, false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == id && events.FlagsFrom(ev.Counter)&state.FlagImpending != 0 {
			foundCastInfo = true
		}
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			foundEntry = true
			// TIME is a string-encoded entry-counter grant. Its amount is the
			// second word, tagged with the entry-counter marker (bit 31).
			const marker = state.ObjID(1) << 31
			if len(ev.Pairs) != 3 || ev.Pairs[0][0] != marker|(state.ObjID(1)<<30)|4 ||
				ev.Pairs[0][1] != marker|10 {
				t.Fatalf("entry MoveZone payload = %#v, want finalized TIME=10 grant", ev.Pairs)
			}
			if ev.Pairs[1][0] != marker|(state.ObjID(1)<<30)|(state.ObjID('T')|state.ObjID('I')<<8|state.ObjID('M')<<16) ||
				ev.Pairs[2][0] != marker|(state.ObjID(1)<<30)|state.ObjID('E') {
				t.Fatalf("entry MoveZone TIME kind payload = %#v", ev.Pairs)
			}
		}
	}
	if !foundEntry {
		t.Fatal("precondition: no battlefield entry MoveZone event for Overlord")
	}
	if !foundCastInfo {
		t.Fatal("precondition: no CastInfo event records the paid Impending mode")
	}
	replayCheck(t, e, cfg)
}
