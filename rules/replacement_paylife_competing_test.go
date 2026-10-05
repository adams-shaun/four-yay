package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func TestPayLifeReplacementCompetitionAsksPayer(t *testing.T) {
	mk := func(name, amount string) *cards.Card {
		return card(t, gateRepl(name,
			"Event$ PayLife | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ Ex | Description$ "+name,
			"Ex:DB$ GainLife | Defined$ You | LifeAmount$ "+amount))
	}
	a, b := mk("PayLife Gain Two", "2"), mk("PayLife Gain Five", "5")
	e, _ := tokenReplGame(t, 0x6161, a, b)
	for _, c := range []*cards.Card{a, b} {
		id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("precondition: replacement source %s is not active under payer: %+v", c.Faces[0].Name, o)
		}
	}
	if len(e.G.Zone(state.ZLibrary, 0)) == 0 || e.G.Players[0].Life != 20 {
		t.Fatalf("precondition: payer life/library = %d/%d", e.G.Players[0].Life, len(e.G.Zone(state.ZLibrary, 0)))
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -1, Text: pay.PayLifeProposalText})
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("pending = %+v, want payer's KReplacement choice between both effects", d)
	}
	chosen := -1
	for _, o := range d.Options {
		if o.Label == "Apply PayLife Gain Five's replacement" {
			chosen = o.Index
		}
	}
	if chosen < 0 {
		t.Fatalf("choice does not offer the distinct second replacement: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{chosen}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Players[0].Life; got != 25 {
		t.Fatalf("life = %d, want 25 from the payer-chosen five-life replacement", got)
	}
}
