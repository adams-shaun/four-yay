package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// Each case chooses between two different-power candidates; the Note records
// the paid object, and damage proves Revealed$CardPower used that object.
func TestCloseEncounterRaiseCostChooseCard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		warped bool
		want   int32
	}{
		{"battlefield", false, 3}, {"warped exile", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := paidCostEngine(t, []string{"Close Encounter", "Hill Giant", "Grizzly Bears", "Timeline Culler"}, []string{"Ancient Brontodon"})
			spell := paidCostMoveTo(t, e, 0, "Close Encounter", state.ZHand)
			hill := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZBattlefield)
			bear := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZBattlefield)
			receiver := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
			chosen := hill
			if tc.warped {
				// Actually warp-cast the card, then let its delayed end-step trigger exile it.
				warp := paidCostMoveTo(t, e, 0, "Timeline Culler", state.ZGraveyard)
				addMana(t, e, 0, "B")
				submitChoices(t, e, castModeOption(t, e, warp, "warped"))
				passUntilStackEmpty(t, e, 40)
				if o := e.G.Obj(warp); o == nil || o.Zone != state.ZBattlefield || o.CastFlags&state.FlagWarped == 0 {
					t.Fatalf("precondition: warp cast did not enter: %+v", o)
				}
				driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
				passUntilStackEmpty(t, e, 40)
				if o := e.G.Obj(warp); o == nil || o.Zone != state.ZExile || o.Owner != 0 || o.CastFlags != 0 || o.Face().Power() != 2 {
					t.Fatalf("precondition: warped card not exiled with reset flags and distinct power: %+v", o)
				}
				chosen = warp
			}
			if o := e.G.Obj(hill); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.Face().Power() != 3 {
				t.Fatalf("precondition: Hill Giant: %+v", o)
			}
			if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 || o.Face().Power() != 2 {
				t.Fatalf("precondition: Grizzly Bears: %+v", o)
			}
			if tc.warped && e.G.Obj(chosen).Face().Power() == e.G.Obj(hill).Face().Power() {
				t.Fatal("precondition: exile and battlefield powers equal")
			}
			if o := e.G.Obj(receiver); o == nil || o.Zone != state.ZBattlefield || o.Damage != 0 {
				t.Fatalf("precondition: target not undamaged battlefield creature: %+v", o)
			}
			if tc.warped {
				// The warp exile fires in the end step; Close Encounter is an instant.
				for _, mana := range "G1" {
					e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(mana), Amount: 1})
				}
				e.priorityRound()
				pick := -1
				for _, opt := range e.Pending().Options {
					if opt.Kind == "cast" && opt.Obj == spell {
						pick = opt.Index
					}
				}
				if pick < 0 {
					t.Fatalf("warped exile did not enable Close Encounter: %+v", e.Pending().Options)
				}
				submitChoices(t, e, pick)
			} else {
				paidCostCast(t, e, spell, "G1")
			}
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose {
				t.Fatalf("expected multiple-candidate choice: %+v", d)
			}
			pick := -1
			for _, opt := range d.Options {
				if opt.Obj == chosen && opt.Kind == "choosecost" {
					pick = opt.Index
				}
				if opt.Kind != "choosecost" {
					t.Fatalf("unexpected reveal arm: %+v", opt)
				}
			}
			if pick < 0 || len(d.Options) < 2 {
				t.Fatalf("chosen candidate not offered among alternatives: %+v", d.Options)
			}
			submitChoices(t, e, pick)
			d = e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("expected target choice after cost: %+v", d)
			}
			pick = -1
			for _, opt := range d.Options {
				if opt.Obj == receiver {
					pick = opt.Index
				}
			}
			if pick < 0 {
				t.Fatalf("receiver not targetable: %+v", d.Options)
			}
			submitChoices(t, e, pick)
			paid := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.Note && strings.Contains(ev.Text, "chose ") && strings.Contains(ev.Text, " as a cost") && len(ev.IDs) == 1 && ev.IDs[0] == chosen {
					paid = true
				}
			}
			if !paid {
				t.Fatalf("no cost Note for chosen object %d", chosen)
			}
			passUntilStackEmpty(t, e, 40)
			if got := e.G.Obj(receiver).Damage; got != tc.want {
				t.Fatalf("damage=%d, want chosen creature's power %d", got, tc.want)
			}
			if o := e.G.Obj(chosen); o == nil || (tc.warped && o.Zone != state.ZExile) || (!tc.warped && o.Zone != state.ZBattlefield) {
				t.Fatalf("chosen object moved instead of being designated: %+v", o)
			}
		})
	}
}

func TestCloseEncounterRaiseCostChooseCardIneligible(t *testing.T) {
	for _, tc := range []struct {
		name            string
		opponent, exile bool
	}{
		{"no candidate", false, false}, {"opponent creature", true, false}, {"ordinary exile", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An absent cast offer alone is vacuous if the cost was never
			// registered. Require the supported cost before testing its gate.
			parsed := cost.ParseCost("ChooseCard<1/" + cost.CloseEncounterChooseSpec + ">")
			if len(parsed.Unknown) != 0 || len(parsed.RevealOrChoose) != 1 || !parsed.RevealOrChoose[0].ChooseCard {
				t.Fatalf("precondition: ChooseCard cost not modelled: %+v", parsed)
			}
			e, _ := paidCostEngine(t, []string{"Close Encounter", "Hill Giant"}, []string{"Ancient Brontodon"})
			spell := paidCostMoveTo(t, e, 0, "Close Encounter", state.ZHand)
			if tc.opponent {
				id := paidCostMoveTo(t, e, 1, "Ancient Brontodon", state.ZBattlefield)
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 || !o.EffectiveIsCreature() {
					t.Fatalf("precondition: opponent creature: %+v", o)
				}
			}
			if tc.exile {
				id := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZExile)
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile || o.Owner != 0 || !o.Face().IsCreature() {
					t.Fatalf("precondition: ordinary exile: %+v", o)
				}
			}
			// Assert the precondition the cost gate reads, not just an absent offer.
			for _, id := range e.G.Zone(state.ZBattlefield, 0) {
				if o := e.G.Obj(id); o != nil && o.EffectiveIsCreature() {
					t.Fatalf("precondition: own battlefield creature %d", id)
				}
			}
			for _, id := range e.G.Zone(state.ZExile, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().IsCreature() && o.CastFlags&state.FlagWarped != 0 {
					t.Fatalf("precondition: warped exile %d", id)
				}
			}
			addMana(t, e, 0, "G1")
			for _, opt := range e.Pending().Options {
				if opt.Kind == "cast" && opt.Obj == spell {
					t.Fatalf("unpayable Close Encounter offered: %+v", opt)
				}
			}
		})
	}
}

func TestCloseEncounterChooseCardCostParse(t *testing.T) {
	const token = "ChooseCard<1/Creature.YouCtrl+inZoneBattlefield;Creature.YouOwn+inZoneExile+warped>"
	c := cost.ParseCost(token)
	if len(c.Unknown) != 0 || c.Generic != 0 || len(c.RevealOrChoose) != 1 || !c.RevealOrChoose[0].ChooseCard || c.RevealOrChoose[0].Spec != cost.CloseEncounterChooseSpec {
		t.Fatalf("supported cost not retained: %+v", c)
	}
	formatted := cost.FormatCost(c)
	if formatted != token {
		t.Fatalf("cost round-trip text %q, want %q", formatted, token)
	}
	again := cost.ParseCost(formatted)
	if len(again.RevealOrChoose) != 1 || again.RevealOrChoose[0] != c.RevealOrChoose[0] {
		t.Fatalf("cost round-trip changed kind/filter: %+v", again)
	}
	if phrase := cost.CostPhrase(c); !strings.Contains(phrase, "warped creature card you own in exile") || strings.Contains(phrase, "reveal") {
		t.Fatalf("cost phrase %q", phrase)
	}
	for _, bad := range []string{"ChooseCard<2/Creature.YouCtrl+inZoneBattlefield;Creature.YouOwn+inZoneExile+warped>", "ChooseCard<1/Creature.YouCtrl+inZoneBattlefield>", "ChooseCard<1/Creature.YouCtrl+inZoneBattlefield;Creature.YouOwn+inZoneExile>"} {
		broken := cost.ParseCost(bad)
		if len(broken.Unknown) != 1 || len(broken.RevealOrChoose) != 0 {
			t.Fatalf("unsupported %q did not fail closed: %+v", bad, broken)
		}
	}
}
