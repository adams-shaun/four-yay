package effects

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The random-SELECTION class: a script parameter that makes the engine, not
// a player, pick an ability's targets or the choices a player is offered.
// Every member must draw from the game rng (Host.Rand), never a fixed
// "first option" stand-in and never ambient randomness:
//
//   - TargetsAtRandom$ (with its RandomNumTargets$ rider): every target ask
//     site hands its decision to RandomTargetsAsk, whatever the API, so a
//     carrier is honoured exactly when its targeting compiles (TgtAtRandom
//     on a Targeted ability).
//   - NumRandomChoices$: charmRandomOffer narrows effCharm's offer, so a
//     carrier is honoured exactly when its API resolves through effCharm.
//   - DividedRandomly$: PutCounter's random division (putCounterSplitRandom).
//
// The tables pin the measured corpus population at the FORGE_REF pin, by
// "card/ability" key, in both directions: a new carrier fails until it is
// classified here (and is honoured), a vanished one is stale.

// randomTargetCarriers is every TargetsAtRandom$ carrier with its API.
var randomTargetCarriers = map[string]string{
	"Cinderheart Giant/TrigDealDamage": "DealDamage",
	"Explosion of Riches/TrigDamage":   "DealDamage",
	"Furnace Layer/FurnaceDiscard":     "Discard",
	"Goblin Polka Band/A0":             "Tap",
	"Goblin Test Pilot/A0":             "DealDamage",
	"Orcish Catapult/A0":               "PutCounter",
	"Power Pack/TrigExile":             "ChangeZone",
	"Power Struggle/DBExchangeControl": "ExchangeControl",
	"Power Struggle/TrigPump":          "Pump",
	"Scab-Clan Giant/TrigFight":        "Fight",
	"Witch Hunt/TrigGainControl":       "GainControl",
}

// randomNumTargetCarriers is every RandomNumTargets$ carrier.
var randomNumTargetCarriers = []string{"Orcish Catapult/A0"}

// numRandomChoicesCarriers is every NumRandomChoices$ carrier with its API.
var numRandomChoicesCarriers = map[string]string{
	"Davriel, Soul Broker/A1":          "GenericChoice",
	"Davriel, Soul Broker/DBCondition": "GenericChoice",
}

// dividedRandomlyCarriers is every DividedRandomly$ carrier with its API.
var dividedRandomlyCarriers = map[string]string{
	"Faerie Dragon/DBPutCounter20": "PutCounter",
	"Orcish Catapult/A0":           "PutCounter",
}

// eachFaceSA calls fn for every A: ability (keyed "A<index>") and every
// SVar body that parses as an ability (keyed by its SVar name) of face, in
// a deterministic order.
func eachFaceSA(face *cards.Face, fn func(key string, sa *cards.SA)) {
	for i, sa := range face.Abilities {
		if sa != nil {
			fn("A"+strconv.Itoa(i), sa)
		}
	}
	names := make([]string, 0, len(face.SVars))
	for name := range face.SVars {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sa := cards.ResolveSVar(face.SVars, name); sa != nil {
			fn(name, sa)
		}
	}
}

func TestRandomSelectionCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	gotTargets := map[string]string{}
	var gotNum []string
	gotChoices := map[string]string{}
	gotDivided := map[string]string{}
	for _, card := range reg.AllCards() {
		for _, face := range card.Faces {
			eachFaceSA(face, func(key string, sa *cards.SA) {
				key = face.Name + "/" + key
				if strings.TrimSpace(sa.Params["TargetsAtRandom"]) != "" {
					gotTargets[key] = sa.API
					tp := TargetsOf(sa)
					if !tp.Has(TgtAtRandom) || !tp.Targeted() {
						t.Errorf("%s: TargetsAtRandom$ carrier is not a random-target ability (flags %b): no target ask can honour it", key, tp.Flags)
					}
				}
				if strings.TrimSpace(sa.Params["RandomNumTargets"]) != "" {
					gotNum = append(gotNum, key)
					if !TargetsOf(sa).Has(TgtRandomNum | TgtAtRandom) {
						t.Errorf("%s: RandomNumTargets$ outside a TargetsAtRandom$ ability is unread", key)
					}
				}
				if strings.TrimSpace(sa.Params["NumRandomChoices"]) != "" {
					gotChoices[key] = sa.API
					if !isCharmAPI(sa) || !CharmOf(sa).NumRandomChoices.Present {
						t.Errorf("%s: NumRandomChoices$ on api:%s, which does not resolve through effCharm's random offer", key, sa.API)
					}
				}
				if strings.TrimSpace(sa.Params["DividedRandomly"]) != "" {
					gotDivided[key] = sa.API
					if sa.API != "PutCounter" || !PutCounterOf(sa).DividedRandomly {
						t.Errorf("%s: DividedRandomly$ on api:%s is unread", key, sa.API)
					}
				}
			})
		}
	}
	sort.Strings(gotNum)
	pinMap(t, "TargetsAtRandom$", gotTargets, randomTargetCarriers)
	pinMap(t, "NumRandomChoices$", gotChoices, numRandomChoicesCarriers)
	pinMap(t, "DividedRandomly$", gotDivided, dividedRandomlyCarriers)
	if strings.Join(gotNum, "|") != strings.Join(randomNumTargetCarriers, "|") {
		t.Errorf("RandomNumTargets$ carriers = %q, want %q", gotNum, randomNumTargetCarriers)
	}
}

// pinMap holds got equal to want in both directions.
func pinMap(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	for k, api := range got {
		if w, ok := want[k]; !ok {
			t.Errorf("new %s carrier %s (api:%s): classify it in the census table", label, k, api)
		} else if w != api {
			t.Errorf("%s carrier %s is api:%s, table says %s", label, k, api, w)
		}
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("stale %s census entry %s: no longer a carrier", label, k)
		}
	}
}
