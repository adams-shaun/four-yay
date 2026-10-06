package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// zoneActivateItem generates name's activate requirement key and replays it
// through gorge, returning the item and the last snapshot. It asserts the
// requirement's sub-family first, so a classifier that stops routing the card
// to activate.hand / activate.graveyard fails here and not only in the census.
func zoneActivateItem(t *testing.T, name, key, wantSub string) (oraclegen.Item, []string, []string, []string) {
	t.Helper()
	reg := loadGenRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	var req levelb.Requirement
	for _, r := range levelb.Requirements(c) {
		if r.Key == key {
			req = r
		}
	}
	if req.Sub != wantSub || req.Gap != "" {
		t.Fatalf("precondition: %s %s = (sub %q, gap %q), want (%q, no gap)", name, key, req.Sub, req.Gap, wantSub)
	}
	it, skip := GenerateB(reg, name, req)
	if skip != nil {
		t.Fatalf("%s %s: %s", name, key, skip.Reason)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) < 2 {
		t.Fatalf("%s does not play through gorge: ok=%v fails=%v snapshots=%d", name, ok, res.Fails, len(res.Snapshots))
	}
	last := res.Snapshots[len(res.Snapshots)-1].Players[0]
	return it, last.Hand, last.Graveyard, last.Exile
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// TestActivateFromHandSureshotSower pins the hand-zone case: the card starts
// in p0's hand (not on the battlefield), the cost's "Discard<1/CARDNAME/this
// card>" needs no extra fixture card, and after the activation the card is in
// the graveyard and the ability is on the stack and resolved.
func TestActivateFromHandSureshotSower(t *testing.T) {
	const name = "Sureshot Sower"
	it, hand, graveyard, _ := zoneActivateItem(t, name, "activate#0.0", "activate.hand")
	p0 := it.Scenario.Setup["p0"]
	if !contains(p0.Hand, name) || contains(p0.Battlefield, name) || contains(p0.Graveyard, name) {
		t.Fatalf("setup must hold %s in p0's hand only: hand=%v bf=%v gy=%v", name, p0.Hand, p0.Battlefield, p0.Graveyard)
	}
	// The self-discard cost brings no fixture card of its own.
	if contains(p0.Hand, "Wastes") {
		t.Errorf("a Discard<1/CARDNAME> cost must not add a fixture discard card: hand=%v", p0.Hand)
	}
	if contains(hand, name) || !contains(graveyard, name) {
		t.Errorf("after activation %s must be discarded: hand=%v graveyard=%v", name, hand, graveyard)
	}
	if got := it.XAbility[activateStepIndex(it.Scenario.Steps)]; !strings.Contains(got, "Discard this card") {
		t.Errorf("xmage_ability = %q, want the channel-style rule text prefix", got)
	}
	if it.Scenario.Steps[0].Mana == "" {
		t.Errorf("the activate step must carry the cost's mana: %+v", it.Scenario.Steps[0])
	}
	// The ability resolves: the item ends with a resolve step.
	if last := it.Scenario.Steps[len(it.Scenario.Steps)-1]; last.Op != "resolve" {
		t.Errorf("last step = %q, want resolve", last.Op)
	}
}

// TestActivateFromGraveyardTheoreticalNecromancer pins the graveyard-zone
// case: the card starts in p0's graveyard, "ExileFromGrave<1/CARDNAME/this
// card>" exiles it as the cost, and the ability returns another creature card.
func TestActivateFromGraveyardTheoreticalNecromancer(t *testing.T) {
	const name = "Theoretical Necromancer"
	it, hand, graveyard, exile := zoneActivateItem(t, name, "activate#0.0", "activate.graveyard")
	p0 := it.Scenario.Setup["p0"]
	if !contains(p0.Graveyard, name) || contains(p0.Battlefield, name) || contains(p0.Hand, name) {
		t.Fatalf("setup must hold %s in p0's graveyard only: hand=%v bf=%v gy=%v", name, p0.Hand, p0.Battlefield, p0.Graveyard)
	}
	if !contains(exile, name) || contains(graveyard, name) {
		t.Errorf("after activation %s must be exiled as the cost: graveyard=%v exile=%v", name, graveyard, exile)
	}
	// "Return another target creature card from your graveyard to your hand":
	// the fixture graveyard holds a second creature card and it reached the hand.
	if len(p0.Graveyard) < 2 {
		t.Fatalf("precondition: the graveyard fixture must hold a target besides the source: %v", p0.Graveyard)
	}
	returned := false
	for _, c := range p0.Graveyard {
		returned = returned || (c != name && contains(hand, c))
	}
	if !returned {
		t.Errorf("no graveyard fixture card reached p0's hand: setup gy=%v hand=%v", p0.Graveyard, hand)
	}
}

// TestActivateFromGraveyardGalliaExilesOtherCreature pins the filtered
// graveyard cost: ExileFromGrave<1/Creature.Other> needs a second creature
// card in the graveyard, which the template supplies.
func TestActivateFromGraveyardGalliaExilesOtherCreature(t *testing.T) {
	const name = "Gallia, Tragic Host"
	it, _, _, exile := zoneActivateItem(t, name, "activate#0.0", "activate.graveyard")
	p0 := it.Scenario.Setup["p0"]
	if !contains(p0.Graveyard, name) || !contains(p0.Graveyard, "Grizzly Bears") {
		t.Fatalf("setup must hold the source and a Grizzly Bears cost card in p0's graveyard: %v", p0.Graveyard)
	}
	if !contains(exile, "Grizzly Bears") {
		t.Errorf("the other creature card must be exiled as the cost: exile=%v", exile)
	}
}

// TestActivateSelfZoneCostsStayGapsElsewhere guards the zone scoping: the
// channel-style self costs are a cost gap on a battlefield source, and one
// zone's cost is a gap in the other zone. A non-self graveyard filter cost
// (ExileFromGrave naming another card's filter) is served from the
// graveyard, its costFixtures supplying the exiled card.
func TestActivateSelfZoneCostsStayGapsElsewhere(t *testing.T) {
	cases := []struct {
		cost, zone, wantGap string
	}{
		{"3 G Discard<1/CARDNAME/this card>", "hand", ""},
		{"3 G Discard<1/CARDNAME/this card>", "battlefield", "Discard<...>"},
		{"3 G Discard<1/CARDNAME/this card>", "graveyard", "Discard<...>"},
		{"3 ExileFromHand<1/CARDNAME>", "hand", ""},
		{"3 ExileFromHand<1/CARDNAME>", "graveyard", "ExileFromHand<...>"},
		{"3 B ExileFromGrave<1/CARDNAME/this card>", "graveyard", ""},
		{"3 B ExileFromGrave<1/CARDNAME/this card>", "hand", "ExileFromGrave<...>"},
		{"4 B ExileFromGrave<1/Creature.Other/another creature card>", "graveyard", ""},
		{"4 B ExileFromGrave<1/Land.Other>", "graveyard", ""},
	}
	for _, tc := range cases {
		pool, gap := activationCostIn(tc.cost, tc.zone)
		if gap != tc.wantGap || (gap == "" && pool == "") {
			t.Errorf("activationCostIn(%q, %q) = (%q, %q), want a pool and gap %q", tc.cost, tc.zone, pool, gap, tc.wantGap)
		}
	}
}
