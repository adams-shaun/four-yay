package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// shroudedLore casts Shrouded Lore at the opponent with three Mountains in
// the caster's graveyard, paying {B} to repeat `pays` times. It returns the
// seats the card picks were asked of, the unless-pay asks posed, and the
// card that ended in the caster's hand from the graveyard.
func shroudedLore(t *testing.T, pays int) (pickers []state.PlayerID, unless int, returned state.ObjID, lastPick state.ObjID) {
	t.Helper()
	e, cfg, id, caster := corpusCardConfig(t, 7311, "Shrouded Lore")
	addMana(t, e, caster, "BBBB") // {B} + up to three repeats
	opp := 1 - caster
	for i := 0; i < 3; i++ {
		moveByName(t, e, caster, "Mountain", state.ZGraveyard)
	}
	d := castFixture(t, e, id, int(opp))
	for i := 0; i < 60 && d != nil && len(e.G.Stack) > 0; i++ {
		switch {
		case d.Kind == decision.KPriority:
			castFirst(t, e, "pass")
		case d.ResumeKind == "unless_pay":
			if d.Player != caster {
				t.Fatalf("the {B} repeat cost was asked of seat %d, want the caster %d", d.Player, caster)
			}
			want := decision.ModeUnlessDecline
			if unless < pays {
				want = decision.ModeUnlessPay
			}
			unless++
			idx := -1
			for _, o := range d.Options {
				if o.Mode == want {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("unless ask has no %q option: %+v", want, d.Options)
			}
			submitChoices(t, e, idx)
		default:
			pickers = append(pickers, d.Player)
			lastPick = d.Options[len(d.Options)-1].Obj
			submitChoices(t, e, d.Options[len(d.Options)-1].Index)
		}
		d = e.Pending()
	}
	if len(e.G.Stack) > 0 {
		t.Fatalf("the spell never finished resolving (pending %+v)", d)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.From == state.ZGraveyard && ev.To == state.ZHand {
			returned = ev.Obj
		}
	}
	replayCheck(t, e, cfg)
	return pickers, unless, returned, lastPick
}

// TestShroudedLoreOpponentPicksFromYourGraveyard: Shrouded Lore's target
// opponent chooses a card in the CASTER's graveyard (Choices$ Card.YouOwn,
// "You" = the activator, as Forge's ChooseCardEffect reads it), the caster
// may pay {B} to have them choose again (Defined$ ParentTarget, a card not
// already chosen), and the last chosen card returns to the caster's hand.
// The repeat is gated on SVar$ChoiceNum/Times.CheckNotPaid, an arithmetic
// gate body. Before the fix the choice filter read "You" as the CHOOSER, so
// the opponent's pool (their own cards in that graveyard) was empty and
// they were never asked; the caster only ever saw the unless-pay prompts.
func TestShroudedLoreOpponentPicksFromYourGraveyard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		pays, picks, unless int
	}{
		{pays: 0, picks: 1, unless: 1}, // decline at once: one pick
		{pays: 1, picks: 2, unless: 2}, // one repeat, then decline
		{pays: 2, picks: 3, unless: 2}, // two repeats exhaust the graveyard
	} {
		pickers, unless, returned, last := shroudedLore(t, tc.pays)
		if len(pickers) != tc.picks || unless != tc.unless {
			t.Errorf("pays=%d: %d picks (%v), %d unless asks; want %d picks, %d unless asks",
				tc.pays, len(pickers), pickers, unless, tc.picks, tc.unless)
			continue
		}
		for _, p := range pickers {
			if p != 1 {
				t.Errorf("pays=%d: a pick was asked of seat %d, want the target opponent (seat 1): %v", tc.pays, p, pickers)
				break
			}
		}
		if returned == 0 || returned != last {
			t.Errorf("pays=%d: card %d returned to hand, want the last chosen card %d", tc.pays, returned, last)
		}
	}
}
