// Level-B template for a self-spell OptionalCost static ("As an additional
// cost to cast this spell, you may collect evidence N / blight N / behold a
// Dragon / waterbend {N}"). Level A always DECLINES the cost; this template
// casts the card WITH it paid (the runner's cast_mode "optionalcost"), so the
// cost's own trace (cards exiled from the graveyard, a -1/-1 counter, a Dragon
// revealed, mana spent) and the spell's "if the additional cost was paid"
// branch reach the snapshots both engines compare.
//
// The scenario is the level-A cast of the card (castResolveWith) with the
// cost's fixture added, re-played with the cast step's CastMode set. The item
// is kept only when gorge's final snapshot DIFFERS from the same scenario
// declined: an identical snapshot would test nothing about the static.
package templates

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// optionalCostCastMode is the cast option the runner selects to pay the cost
// (rules/legal_walk_hand.go: Mode "optionalcost").
const optionalCostCastMode = "optionalcost"

// optionalCostSetup is what one OptionalCost part needs on top of the
// ordinary cast: the cost's fixture, the extra pool mana and the XMage picks
// that follow the "pay it?" yes.
type optionalCostSetup struct {
	fixture func(*oraclegen.Fixture)
	mana    string   // extra generic mana the cost spends from the pool
	picks   []string // names XMage's cost selector is answered with when gorge poses no ask for it
}

// optionalCostSetupFor maps the static's single cost token to its setup, or
// returns the reason it has none.
func optionalCostSetupFor(tok string) (optionalCostSetup, string) {
	head, _, _ := strings.Cut(tok, "<")
	payload, _ := bracketPayload(tok)
	switch head {
	case "CollectEvidence":
		graveyard := evidenceCostFixtures(tok)
		if len(graveyard) == 0 {
			return optionalCostSetup{}, "has no graveyard fixture for " + tok
		}
		// No pick is scripted here: gorge poses the evidence choose, and its
		// decision routes into the cast step's answers as one "A^B" record,
		// XMage's native multi-pick spelling for the cost's single selector.
		return optionalCostSetup{
			fixture: func(fx *oraclegen.Fixture) {
				fx.P0().Graveyard = append(fx.P0().Graveyard, graveyard...)
			},
		}, ""
	case "Blight":
		if _, ok := keywordCostMana(tok, 0); !ok {
			return optionalCostSetup{}, "has no creature fixture for " + tok
		}
		return optionalCostSetup{
			fixture: func(fx *oraclegen.Fixture) {
				fx.P0().Battlefield = appendFixtureUnique(fx.P0().Battlefield, blightFixture)
			},
			picks: []string{blightFixture},
		}, ""
	case "Behold":
		typ, ok := strings.CutPrefix(payload, "1/")
		typ, _, _ = strings.Cut(typ, "/")
		fixture := beholdFixture[typ]
		if !ok || fixture == "" {
			return optionalCostSetup{}, "has no behold fixture for " + tok
		}
		return optionalCostSetup{
			fixture: func(fx *oraclegen.Fixture) {
				fx.P0().Hand = appendFixtureUnique(fx.P0().Hand, fixture)
			},
			picks: []string{fixture},
		}, ""
	case "Waterbend":
		// XMage's waterbend reads the pool and taps nothing here.
		n, err := strconv.Atoi(payload)
		if err != nil || n <= 0 {
			return optionalCostSetup{}, "has no pool payment for " + tok
		}
		return optionalCostSetup{mana: strings.Repeat("C", n)}, ""
	}
	return optionalCostSetup{}, "cost " + tok + " is not modelled"
}

// optionalCostStatics counts the faces's self-spell OptionalCost statics: a
// cast_mode of "optionalcost" cannot say WHICH of two to pay.
func optionalCostStatics(f *cards.Face) int {
	n := 0
	for i := range f.Statics {
		if strings.EqualFold(f.Statics[i].Mode, "OptionalCost") {
			n++
		}
	}
	return n
}

// optionalCostItem serves one static.optional-cost requirement.
func optionalCostItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static OptionalCost " + why}
	}
	st, _ := staticSlotOf(f, req)
	if st.Mode == "" {
		return skip("static slot not found")
	}
	if optionalCostStatics(f) != 1 {
		return skip("card has several OptionalCost statics: cast_mode cannot choose between them")
	}
	toks := costTokens(st.ParamStr(cards.PKCost))
	if len(toks) != 1 {
		return skip("compound cost " + st.ParamStr(cards.PKCost) + " needs an as-cast creature-type choice")
	}
	setup, why := optionalCostSetupFor(toks[0])
	if why != "" {
		return skip(why)
	}
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return skip("mana: " + why)
	}
	// Spare generic mana lets an "unless its controller pays" body show the
	// paid branch: a counter that cannot be paid for is the same either way.
	for spare := 0; spare <= optionalCostSpareMana; spare++ {
		mana := pool + setup.mana + strings.Repeat("C", spare)
		if it, ok := paidOptionalCostItem(reg, f, name, req, mana, setup); ok {
			return it, nil
		}
	}
	return skip("paid cast is not castable, or leaves the same board as the declined cast: nothing observes the cost")
}

// optionalCostSpareMana bounds the spare generic mana tried on top of the
// base cost.
const optionalCostSpareMana = 2

// lastSnapshot replays it in gorge and returns its final snapshot.
func lastSnapshot(reg *cards.Registry, it oraclegen.Item) (string, bool) {
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Snapshots) == 0 {
		return "", false
	}
	b, err := json.Marshal(res.Snapshots[len(res.Snapshots)-1])
	return string(b), err == nil
}

// paidOptionalCostItem casts the card with the cost paid (the level-A cast
// pipeline, with the cast step's cast_mode set) and keeps the item only when
// the declined cast of the same pool ends in a different snapshot. XMage's
// answers for the cast step start with the chooseUse "pay it?" yes, then the
// cost's picks.
func paidOptionalCostItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, mana string, setup optionalCostSetup) (oraclegen.Item, bool) {
	paid, sk := castResolveWith(reg, f, name, mana, nil, func(fx *oraclegen.Fixture) {
		if setup.fixture != nil {
			setup.fixture(fx)
		}
		fx.SetCastMode(optionalCostCastMode)
	})
	if sk != nil {
		return oraclegen.Item{}, false
	}
	castIdx := -1
	for i, st := range paid.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:"+name {
			castIdx = i
		}
	}
	if castIdx < 0 || paid.Scenario.Steps[castIdx].CastMode != optionalCostCastMode {
		return oraclegen.Item{}, false
	}
	declined, sk := castResolveWith(reg, f, name, paid.Scenario.Steps[castIdx].Mana, nil, setup.fixture)
	if sk != nil || declined.Scenario.Steps[castIdx].Mana != paid.Scenario.Steps[castIdx].Mana {
		return oraclegen.Item{}, false
	}
	pSnap, ok1 := lastSnapshot(reg, paid)
	dSnap, ok2 := lastSnapshot(reg, declined)
	if !ok1 || !ok2 || pSnap == dSnap {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"601.2b", "118.8"}, paid.Scenario)
	it.XAnswers = paid.XAnswers
	it.XTargetSkips = paid.XTargetSkips
	it.Ignore = paid.Ignore
	if len(it.XAnswers) < len(it.Scenario.Steps) {
		grown := make([][]oraclegen.XAnswer, len(it.Scenario.Steps))
		copy(grown, it.XAnswers)
		it.XAnswers = grown
	}
	lead := []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}
	for _, p := range setup.picks {
		lead = append(lead, oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: p})
	}
	it.XAnswers[castIdx] = append(lead, it.XAnswers[castIdx]...)
	return it, true
}
