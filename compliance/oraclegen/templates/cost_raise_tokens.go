package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
)

// The RaiseCost additional-cost tokens this table pays (ticket
// levelb-cost-additional-costs). The engine already prices and asks each one
// on a cast: rules/pay/costmods.go folds the static's Cost$ into
// mods.extra, the offer gate enforces it, and the cast's payment stages ask
// it (xAsk's announcement, blightCostAsk, revealCostOrChooseAsk's choose arm,
// castasks.go's waterbend tap offer). The generator's job is only the
// fixture and the scripted answers, so a probe whose precondition is false
// never reaches an item. Every token the table does not recognize is left
// untouched: the probe stays unpayable, fails PlaysThrough and keeps the
// named "raise cost payment" skip.
//
// The fixtures mirror the shapes the level-A cast template (cast.go) and the
// activation-cost table (activate_keyword_costs.go) already serve, so both
// levels exercise the same payment.

const (
	// raiseBlightXFixture is the 1/1 a Blight<X> cast cost is paid with: it
	// bounds the announced X to 1 (the X cap is the greatest toughness among
	// creatures you control, CR 601.2b, and the cast template's own
	// xAnswers already announces X = 1 over this elf).
	raiseBlightXFixture = "Llanowar Elves"

	// raiseBlightNFixture is the 3/3 a fixed Blight<N> is paid with: it
	// survives two -1/-1 counters, so the resolved spell sees a real board.
	raiseBlightNFixture = "Hill Giant"
)

// raiseCostTokenProbes pays the RaiseCost additional-cost tokens the table
// owns, adding the fixture and the cast answers to p. One row per token:
//
//   - Waterbend<N>: N generic in the pool (waterbend lets a player tap
//     artifacts and creatures for {1}, it never requires it, CR 701.67a);
//     the level-A cast template casts Water Whip at mana+"CCCCC" the same
//     way. Waterbend<X> is NOT owned here: its X is announced by the cast's
//     ordinary X ask, and X = 0 (the pool-bound fallback) already makes
//     Crashing Wave and Foggy Swamp Visions castable probes.
//   - Blight<N>: N -1/-1 counters on the surviving 3/3 fixture (N <= 2).
//   - Blight<X>: one counter on the 1/1 fixture, announced as the cast's X.
//   - Close Encounter's ChooseCard: one creature you control on the
//     battlefield; with exactly one candidate the engine settles the
//     choose-cost without an ask (cast_asks.go's exactly-N auto-settle), and
//     the exile arm stays empty, so the probe pays the battlefield form.
//
// handled is true only when every token of cost is one the table owns and
// paid; anything else (a mana pip, BeholdExile, Waterbend<X>, PayLife, an
// unmodelled filter) leaves p untouched and false, so the caller keeps its
// existing price and the named skip stands.
func raiseCostTokenProbes(p *costProbe, cost string) (handled bool) {
	handled = cost != ""
	for _, tok := range costTokens(cost) {
		payload, ok := bracketPayload(tok)
		switch tokenHead(tok) {
		case "Waterbend":
			n, err := strconv.Atoi(payload)
			if !ok || err != nil || n <= 0 {
				return false
			}
			p.mana += strings.Repeat("C", n)
		case "Blight":
			if !ok {
				return false
			}
			if n, err := strconv.Atoi(payload); err == nil {
				if n >= 1 && n <= 2 {
					p.battlefield = appendUnique(p.battlefield, raiseBlightNFixture)
					continue
				}
				return false
			}
			if payload == "X" {
				p.battlefield = appendUnique(p.battlefield, raiseBlightXFixture)
				p.answers = append(p.answers, oraclegen.Answer{Kind: "choose", Pick: []string{"X = 1"}})
				continue
			}
			return false
		case "ChooseCard":
			if ok && payload == "1/"+costvocab.CloseEncounterChooseSpec {
				p.battlefield = appendUnique(p.battlefield, raiseBlightXFixture)
				continue
			}
			return false
		default:
			return false
		}
	}
	return handled
}
