package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Round-2 pins for the ChangeZone OriginAlternative$ work (review findings):
//
//   - the widened cross-zone search fires ONLY when OriginAlternative$ is
//     present, so a compound `Origin$` WITHOUT one keeps its object
//     dispatcher (pinned corpus-side by Boonweaver Giant's AttachedTo
//     carrier below and effects-side by the Eladamri, Korvecdal probe);
//   - an alternative-zone pick into the battlefield carries the AttachedTo$
//     rider, so an Aura found in the graveyard or hand does not enter
//     unattached and get swept by the CR 704.5m SBA.

// TestChangeZoneOriginAlternativeGraveyardAuraEntersAttached is the Boonweaver
// Giant carrier: `DB$ ChangeZone | Hidden$ True | Origin$ Library |
// OriginAlternative$ Graveyard,Hand | Destination$ Battlefield |
// ChangeType$ Aura.YouOwn | AttachedTo$ Self`. The Aura sits in the
// GRAVEYARD; the search must offer it, move it onto the battlefield, and
// attach it to the Giant. Without the rider an unattached Aura on the
// battlefield is a CR 704.5m SBA casualty -- the exact failure this pin
// forbids.
func TestChangeZoneOriginAlternativeGraveyardAuraEntersAttached(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Boonweaver Giant", "Arachnus Web")
	web := searchMoveByName(t, e, "Arachnus Web", state.ZGraveyard)
	giant := searchMoveByName(t, e, "Boonweaver Giant", state.ZBattlefield)
	e.pending = nil
	e.priorityRound()

	// nextAsk returns the next non-priority decision, passing priority until
	// one surfaces; nil means the table settled back at priority.
	nextAsk := func() *decision.Decision {
		for i := 0; i < 20 && !e.G.Over; i++ {
			d := e.Pending()
			if d == nil {
				return nil
			}
			if d.Kind != decision.KPriority {
				return d
			}
			if len(e.G.Stack) == 0 {
				return nil
			}
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		}
		t.Fatalf("no non-priority decision within 20 passes")
		return nil
	}

	// The ETB trigger is OptionalDecider$ You: answer the election with yes,
	// then the search KChoose (ResumeKind "search") with the graveyard Web,
	// then the ShuffleNonMandatory$ confirm.
	d := nextAsk()
	var pick decision.Option
	found := false
	for i := 0; i < 15 && d != nil; i++ {
		switch d.Kind {
		case decision.KTriggerOptional, decision.KModes:
			submitChoices(t, e, 0)
		case decision.KChoose:
			if d.ResumeKind == "search_mayshuffle" {
				// ShuffleNonMandatory$ True rides the carrier: confirm.
				submitChoices(t, e, 0)
				break
			}
			if d.ResumeKind != "search" {
				t.Fatalf("unexpected %v ask: %+v", d.ResumeKind, d)
			}
			for _, o := range d.Options {
				if o.Obj == web {
					pick, found = o, true
				}
			}
			if !found {
				t.Fatalf("graveyard-resident Arachnus Web not offered: %+v", d.Options)
			}
			submitChoices(t, e, pick.Index)
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
		d = nextAsk()
	}
	if !found {
		t.Fatalf("the alternative-origin search ask never surfaced (game over: %v)", e.G.Over)
	}
	passUntilStackEmpty(t, e, 30)

	w := e.G.Obj(web)
	if w == nil || w.Zone != state.ZBattlefield {
		t.Fatalf("Arachnus Web zone = %+v, want battlefield", w)
	}
	if w.AttachedTo != giant {
		t.Fatalf("Arachnus Web AttachedTo = %d, want the Giant (%d); an unattached Aura is a CR 704.5m SBA casualty", w.AttachedTo, giant)
	}
	replayCheck(t, e, cfg)
}
