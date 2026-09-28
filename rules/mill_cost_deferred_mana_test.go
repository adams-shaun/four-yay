package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDeferredManaMillCostMillsAfterDiscardOrExile covers the mana-payment
// continuation used when a mana ability asks its controller to discard or
// exile a card. Mill remains a cost on that deferred path and is paid before
// the ability adds mana.
func TestDeferredManaMillCostMillsAfterDiscardOrExile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		cost   string
		option string
		inHand bool
	}{
		{name: "discard", cost: "Discard<1/Card>", option: "mana_discard", inHand: true},
		{name: "exile", cost: "Exile<1/Artifact>", option: "mana_exile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sourceScript := "Name:Deferred Mill Engine\nTypes:Artifact\n" +
				"A:AB$ Mana | Cost$ T " + tc.cost + " Mill<1> | Produced$ C\nOracle:x\n"
			var hand []*cards.Card
			if tc.inHand {
				hand = append(hand, card(t, discardCostJunk))
			}
			e := smallLibraryEngine(t, 1, hand...)
			source := onBoard(t, e, 0, sourceScript)
			payment := state.ObjID(0)
			if tc.inHand {
				payment = e.G.Zone(state.ZHand, 0)[0]
			} else {
				payment = onBoard(t, e, 0, discardCostJunk)
			}
			libCard := e.G.Zone(state.ZLibrary, 0)[0]
			if e.G.Obj(source) == nil || e.G.Obj(source).Zone != state.ZBattlefield ||
				e.G.Obj(payment) == nil || e.G.Obj(payment).Zone == state.ZLibrary ||
				e.G.Obj(libCard) == nil || e.G.Obj(libCard).Zone != state.ZLibrary {
				t.Fatalf("precondition: source=%+v payment=%+v library=%+v", e.G.Obj(source), e.G.Obj(payment), e.G.Obj(libCard))
			}
			ma := e.G.Obj(source).Face().Abilities[0]
			parsed := ParseCost(ma.Params["Cost"])
			if len(parsed.Mill) != 1 || parsed.Mill[0].N != 1 {
				t.Fatalf("precondition: Mill<1> was not parsed: %+v", parsed.Mill)
			}
			if !e.manaAbilityPayable(0, source, ma) {
				t.Fatal("precondition: deferred mana ability is not payable")
			}

			e.resolveManaAbility(0, source, ma, false)
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != tc.option {
				t.Fatalf("deferred payment decision = %+v, want %s", d, tc.option)
			}
			choice := -1
			for _, opt := range d.Options {
				if opt.Obj == payment {
					choice = opt.Index
				}
			}
			if choice < 0 {
				t.Fatalf("payment card %d was not offered: %+v", payment, d.Options)
			}
			submitChoices(t, e, choice)

			wantPaymentZone := state.ZGraveyard
			if tc.option == "mana_exile" {
				wantPaymentZone = state.ZExile
			}
			if e.G.Obj(payment).Zone != wantPaymentZone {
				t.Fatalf("selected payment card ended in %s, want %s", e.G.Obj(payment).Zone, wantPaymentZone)
			}
			if e.G.Obj(libCard).Zone != state.ZGraveyard {
				t.Fatalf("Mill cost left library card in %s, want graveyard", e.G.Obj(libCard).Zone)
			}
			millAt, manaAt := -1, -1
			for i, ev := range e.L.Events {
				if ev.Obj == libCard && ev.From == state.ZLibrary && ev.To == state.ZGraveyard {
					millAt = i
				}
				if ev.Kind == events.ManaAdd && ev.Player == 0 {
					manaAt = i
				}
			}
			if millAt < 0 || manaAt < 0 || millAt >= manaAt {
				t.Fatalf("mill event %d and mana event %d, want mill before mana", millAt, manaAt)
			}
		})
	}
}
