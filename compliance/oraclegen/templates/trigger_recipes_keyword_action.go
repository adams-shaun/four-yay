package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Probe cards for the keyword-action trigger recipes, tried in order like the
// other probe lists (spec hypothesis H4: a gorge PlaysThrough test picks the
// winner and the host replay confirms it exists in XMage).
var (
	// A creature whose enters-the-battlefield trigger explores: casting it
	// makes a creature p0 controls explore, which fires a "whenever a
	// creature you control explores" trigger. Merfolk Branchwalker is the
	// cheapest ({1}{G}); Ixalli's Diviner and Merfolk Tunnel Guide follow.
	exploreProbes = []string{"Merfolk Branchwalker", "Ixalli's Diviner", "Merfolk Tunnel Guide"}
	// A sorcery that manifests dread on resolution ({1}{G}).
	manifestDreadProbe = "Manifest Dread"
	// Discover probes: Daring Discovery ({4}{R}) discovers 4 with only
	// optional targets; Etali's Favor discovers 3 on ETB (an Aura that needs
	// a bearer).
	discoverProbes = []string{"Daring Discovery", "Etali's Favor"}
	// Gift probes: Blooming Blast ({1}{R}, gift a Treasure) targets a
	// creature, so a Grizzly Bears is placed to take the damage; Wildfire
	// Howl ({1}{R}{R}, gift a card) targets any target when the gift is
	// promised.
	giftProbes = []string{"Blooming Blast", "Wildfire Howl"}
	// Forage probes: an instant or sorcery whose cast can forage. Feed the
	// Cycle ({1}{B}) is "forage or pay {B}" (its Forage is chosen first), so
	// the three graveyard cards the Forage cost exiles are placed at setup.
	forageProbes = []string{"Feed the Cycle", "Traverse Valley"}
)

// keywordActionRecipe builds turn-1 causes for the keyword-action trigger
// sub-families (Forage, GiveGift, Explores, CollectEvidence, ManifestDread,
// Discover, ElementalBend, BecomesPlotted, BecomesSaddled). ok is false for
// any other sub-family, which triggerRecipe then treats as it always has.
func keywordActionRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) (causes []triggerCause, why string, ok bool) {
	add := func(c triggerCause, yes bool) {
		if yes {
			causes = append(causes, c)
		}
	}
	switch sub {
	case "trigger.manifest-dread":
		if oraclegen.XMageKnown(manifestDreadProbe) {
			add(castCause(reg, name, manifestDreadProbe))
		}
	case "trigger.discover":
		for _, p := range discoverProbes {
			if !oraclegen.XMageKnown(p) {
				continue
			}
			add(castCause(reg, name, p))
		}
	case "trigger.explores":
		// An explore reveals the top card of p0's library, so a trigger that
		// narrows on the revealed card's type ("explores a land" / "explores a
		// nonland", Nicanzil) seeds a matching top card. The default top is a
		// land, so only the nonland variant needs a seed.
		explored := strings.ToLower(t.ParamStr(cards.PKValidExplored))
		for _, p := range exploreProbes {
			if !oraclegen.XMageKnown(p) {
				continue
			}
			c, yes := castCause(reg, name, p)
			if !yes {
				continue
			}
			if strings.Contains(explored, "nonland") {
				c.libraryTop = []string{bearsProbe}
			}
			add(c, true)
		}
	case "trigger.give-gift":
		for _, p := range giftProbes {
			if !oraclegen.XMageKnown(p) {
				continue
			}
			add(giftCause(reg, name, p))
		}
	case "trigger.forage":
		for _, p := range forageProbes {
			if !oraclegen.XMageKnown(p) {
				continue
			}
			add(forageCause(reg, name, p))
		}
	case "trigger.collect-evidence":
		add(collectEvidenceCause(reg, name))
	case "trigger.elemental-bend":
		add(elementalBendCause(f, name))
	case "trigger.becomes-plotted":
		add(plotCause(f, name))
	case "trigger.becomes-saddled":
		add(saddleCause(f, name))
	default:
		return nil, "", false
	}
	if len(causes) == 0 {
		return nil, "probe not in corpus or no matching ability", true
	}
	return causes, "", true
}

// giftCause casts a Gift spell and promises the gift, so the resolution
// emits the events.GiveGift marker a "whenever you give a gift" trigger
// reads. The cast step answers the CR 702.168 election with the
// gift_promise option (option 0 is the decline, which would emit nothing).
func giftCause(reg *cards.Registry, name, probe string) (triggerCause, bool) {
	card, exists := reg.Lookup(probe)
	if !exists || len(card.Faces) == 0 || probe == name {
		return triggerCause{}, false
	}
	targets := []string{}
	var battlefield []string
	switch probe {
	case "Blooming Blast":
		targets = []string{"p0:Grizzly Bears"}
		battlefield = []string{bearsProbe}
	case "Wildfire Howl":
		targets = []string{"p0:Grizzly Bears"}
		battlefield = []string{bearsProbe}
	}
	st, ok := castProbe(reg, probe, targets...)
	if !ok {
		return triggerCause{}, false
	}
	st.Answers = []oraclegen.Answer{{Kind: "choose", Pick: []string{"gift_promise"}}}
	return triggerCause{hand: []string{probe}, battlefield: battlefield, steps: []oraclegen.Step{st}}, true
}

// forageCause casts a spell whose cast forages (Feed the Cycle's Forage-or-
// pay-{B} additional cost), placing the three graveyard cards the Forage cost
// exiles so the deterministic first option ("Exile three cards from your
// graveyard") is payable and chosen.
func forageCause(reg *cards.Registry, name, probe string) (triggerCause, bool) {
	card, exists := reg.Lookup(probe)
	if !exists || len(card.Faces) == 0 || probe == name {
		return triggerCause{}, false
	}
	var targets []string
	var opponentBattlefield []string
	switch probe {
	case "Feed the Cycle":
		targets = []string{"p1:Grizzly Bears"}
		opponentBattlefield = []string{bearsProbe}
	}
	st, ok := castProbe(reg, probe, targets...)
	if !ok {
		return triggerCause{}, false
	}
	return triggerCause{
		hand:                []string{probe},
		graveyard:           append([]string(nil), forageFixtures...),
		opponentBattlefield: opponentBattlefield,
		steps:               []oraclegen.Step{st},
	}, true
}

// collectEvidenceCause activates a probe permanent's ability whose cost is
// CollectEvidence<N>: paying the cost emits the events.CollectEvidenceAction
// marker a "whenever you collect evidence" trigger reads. Kylox's Voltstrider
// is the corpus carrier with an activated CollectEvidence cost.
func collectEvidenceCause(reg *cards.Registry, name string) (triggerCause, bool) {
	probe := "Kylox's Voltstrider"
	card, ok := reg.Lookup(probe)
	if !ok || len(card.Faces) == 0 || probe == name || !oraclegen.XMageKnown(probe) {
		return triggerCause{}, false
	}
	pf := card.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() || !strings.Contains(sa.ParamStr(cards.PKCost), "CollectEvidence") {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield", probe)
		if gap != "" {
			continue
		}
		prefix, okp := prefixes[i]
		if !okp {
			continue
		}
		idx := i
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, probe, cost)
		activate := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + probe, Mana: mana, AbilityIndex: &idx, Answers: activationXAnswers(cost)}
		return triggerCause{
			battlefield:  append([]string{probe}, setup.Battlefield...),
			graveyard:    append([]string(nil), setup.Graveyard...),
			steps:        []oraclegen.Step{activate, {Op: "resolve"}},
			xability:     []string{prefix},
			activateCost: cost,
		}, true
	}
	return triggerCause{}, false
}

// elementalBendCause attacks with the source: its own Firebending keyword
// trigger resolves to DB$ ElementalBend, which emits the events.ElementalBend
// marker a "whenever you ... bend" trigger reads.
func elementalBendCause(f *cards.Face, name string) (triggerCause, bool) {
	if !f.IsCreature() {
		return triggerCause{}, false
	}
	attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}}
	return triggerCause{steps: []oraclegen.Step{attack}}, true
}

// plotCause plots the source itself: the card starts in p0's hand and the
// plot special action (the runner's cast_mode "plot") exiles it and emits the
// AlterAttribute "Plotted" grant the BecomesPlotted matcher reads.
func plotCause(f *cards.Face, name string) (triggerCause, bool) {
	cost, ok := f.KeywordCostParam("Plot")
	if !ok || cost == "" {
		return triggerCause{}, false
	}
	pool, why := oraclegen.PoolFor(cost)
	if why != "" {
		return triggerCause{}, false
	}
	step := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: pool, CastMode: plotCastMode}
	return triggerCause{
		selfInHand: true,
		hand:       []string{name},
		steps:      []oraclegen.Step{step},
		xability:   []string{"Plot " + xmageManaText(cost)},
	}, true
}

// saddleCause activates the source's own Saddle ability: resolving it emits
// the AlterAttribute "Saddled" grant the BecomesSaddled matcher reads. The
// shared activation-cost fixtures supply the creatures the Saddle cost taps.
func saddleCause(f *cards.Face, name string) (triggerCause, bool) {
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range f.Abilities {
		if !sa.IsActivated() || !strings.HasPrefix(strings.ToLower(sa.ParamStr(cards.PKKeyword)), "saddle") {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield", name)
		if gap != "" {
			continue
		}
		prefix, okp := prefixes[i]
		if !okp {
			continue
		}
		idx := i
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, name, cost)
		activate := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: mana, AbilityIndex: &idx, Answers: activationXAnswers(cost)}
		return triggerCause{
			battlefield:  append([]string(nil), setup.Battlefield...),
			steps:        []oraclegen.Step{activate},
			xability:     []string{prefix},
			activateCost: cost,
		}, true
	}
	return triggerCause{}, false
}
