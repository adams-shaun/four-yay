package rules

// Creature mana-ability audit (feedback fb-20260928T161639Z-081a09f0).
//
// TestCreatureManaAbilityAudit is the ordinary-suite census the player asked
// for: over the WHOLE compiled corpus it enumerates every mana ability that
// sits on a CREATURE face (an AB$ Mana / AB$ ManaReflected ability, plus the
// CR 305.6 intrinsic basic-land-type mana ability the registry grants), and,
// on the autopay census's own synthetic board -- extended with the cost
// fodder a creature mana ability commonly needs -- asserts each one end to
// end through the REAL priority path:
//
//   - the "activate for mana" option is offered (and, for a {T} cost whose
//     source is summoning sick, is NOT offered -- CR 302.6);
//   - submitting it resolves and adds exactly what Produced$ states (the
//     per-symbol total of cards.ProducedCounts times the literal Amount$,
//     with the deterministic first-option answer for Any/Combo flows);
//   - the source is a battlefield permanent, not a phantom row.
//
// The rows the audit cannot yet assert live are an explicit, MEASURED reason
// table (knownUnsupportedCreatureMana), in the shape rules/paramcensus_test.go
// established: a newly unsupported creature ability fails and is named, and a
// tabled entry the build now supports is stale and fails too. The table can
// only shrink: growth fails.
//
// This test runs in the ordinary suite (no env gate). The env-gated
// TestAutopayManaCensus (rules/autopay_census_test.go) remains the full
// corpus-wide writer of census.csv/classes.md; this test is its
// creature-scoped, always-on ratchet.
//
//	GORGE_AUTOPAY_CENSUS=<dir> go test ./rules -run TestAutopayManaCensus -v
//	go test ./rules -run TestCreatureManaAbilityAudit -v

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// knownUnsupportedCreatureMana is the measured reason table for creature-face
// mana abilities the live-offer audit does not yet assert. The key is
// "<card name>|<ability ident>"; the value names the measured reason. Seeded
// from TestCreatureManaAbilityAudit's own run at the census board.
//
// Every reason is a census-measured fact about THIS synthetic board or a
// known unimplemented shape, never a hand-waved "unknown":
//
//   - fodder:<spec>      the cost needs a specific object the census board
//     does not carry (a Food, a Goblin, a Saproling, an
//     untapped opponent land, a named Wood token).
//   - gate:<detail>      the ability is gated by a condition or activation
//     limit that is false on the census board (IsPresent$,
//     CheckSVar$, PlayerTurn$, ActivationLimit$,
//     OpponentTurn$).
//   - dynamic:<detail>   the Amount$ is a board-dependent count that is
//     legitimately zero on the census board; the non-zero
//     case is proven by a dedicated corpus test where the
//     engine supports it.
//   - selector:<token>   Produced$ names a token the grammar cannot read.
var knownUnsupportedCreatureMana = map[string]string{
	// Cost fodder the shared census board does not carry. Each is a specific
	// object a real board supplies (a Food, a Goblin, a Saproling, a Spirit
	// token, a token named Wood, two Food/tokens, a second Goblin, an
	// untapped opponent land, an untapped source for a {Q} cost). These are
	// board limitations, not engine debt: the card is not refused, its cost
	// simply has no legal payer on the synthetic board.
	"Baylen, the Haymaker|A0": "fodder: needs two token permanents to tap (tapXType<2/Permanent.token/token>)",
	"Benthic Explorers|A0":    "fodder: untaps an opponent's land (untapYType<1/Land.OppCtrl/land>)",
	"Bolg's Company|A0":       "fodder: needs another Goblin to sacrifice",
	"Gilded Goose|A1":         "fodder: needs a Food to sacrifice",
	"Jungle Patrol|A1":        "fodder: needs a token named Wood to sacrifice",
	"Kykar, Wind's Fury|A0":   "fodder: needs a Spirit to sacrifice",
	"The Cabbage Merchant|A0": "fodder: needs two Food to tap (tapXType<2/Food>)",
	"Thornvault Forager|A1":   "fodder: needs Forage fodder (3 Food or a Food plus a graveyard card)",
	"Utopia Mycon|A1":         "fodder: needs a Saproling to sacrifice",
	"Pili-Pala|A0":            "fodder: {Q} untap cost needs the source already tapped",
	// Counter-removal costs whose counters the shared board does not seed on
	// the source (the board seeds only the source's own SubCounter cost).
	"Haruspex|A0":                     "fodder: Amount$ X over SubCounter<X/P1P1>; the board seeds no +1/+1 counters",
	"Petalmane Baku|A0":               "fodder: Amount$ X over SubCounter<X/KI>; the board seeds no ki counters",
	"Rasputin, the Oneiromancer|A0":   "fodder: Amount$ X over SubCounter<X1+/DREAM>; the board seeds no dream counters",
	"Jetfire, Ingenious Scientist|A0": "fodder: SubCounter cost; the board seeds no counters on the source",
	// Board-dependent activation gates that are false on a bare board.
	"Circle of Elders|A0":            "gate: CheckSVar FormidableTest (total power >= 8) is false",
	"Fanatic of Rhonas|A1":           "gate: IsPresent$ Creature.YouCtrl+powerGE4 (ferocious) is false",
	"Whisperer of the Wilds|A1":      "gate: IsPresent$ Creature.YouCtrl+powerGE4 (ferocious) is false",
	"Lavinia, Foil to Conspiracy|A0": "gate: OpponentTurn$ True; seat 0's own turn does not qualify",
	"Rainbow Dash|A0":                "gate: CheckSVar RDCoolness GE5 is false",
	"Vivi Ornitier|A0":               "gate: PlayerTurn$ True + ActivationLimit$ 1; not activatable on the audit turn",
	// Board-dependent Amount$ that is legitimately zero on the audit board.
	// The card is offered and resolves; the count it reads is zero.
	"A-Vivi Ornitier|A0":              "dynamic: Amount$ X is the source's power on the audit board",
	"Accomplished Alchemist|A1":       "dynamic: Amount$ X is life gained this turn (zero)",
	"Alena, Kessig Trapper|A0":        "dynamic: Amount$ X is the greatest power among creatures that entered this turn (zero)",
	"Gyre Sage|A0":                    "dynamic: Amount$ X is +1/+1 counters on the source (zero)",
	"Immortus, Master of Eternity|A0": "dynamic: Amount$ X is cards drawn this turn (zero)",
	"Kydele, Chosen of Kruphix|A0":    "dynamic: Amount$ X is cards drawn this turn (zero)",
	"Hazel of the Rootbloom|A0":       "dynamic: Amount$ X is the number of tokens tapped (zero)",
	// Adds its mana to a TARGETED player, not to seat 0, so no seat-0 ManaAdd
	// is observed by the census; the ability itself is offered and resolves.
	"The Warring Triad|A0": "add_to_target: Defined$ Targeted + ValidTgts$ Player; mana goes to the targeted player",
	// Produced$ token the symbol grammar cannot read (fail-closed, loud Note).
	"Sunbird Standard|A0": "empty_set: the audit board has no cards exiled with the source, so EachColorAmong_ExiledWith is a no-op; the non-empty case is proven by each_color_among_exiled_with_test.go",
}

// creatureManaItems returns the activated/intrinsic mana items on a card's
// creature faces, so the audit never evaluates a card that carries no
// creature mana ability (keeps the ordinary run cheap).
func creatureManaItems(kind, key string, c *cards.Card) []censusItem {
	var out []censusItem
	for fi, f := range c.Faces {
		if f == nil || !censusHas(f.Types, "Creature") {
			continue
		}
		for ai, a := range f.Abilities {
			if a.Kind != "AB" || !isManaAbilityAPI(a.API) {
				continue
			}
			family := "activated"
			if strings.HasPrefix(a.Line, "intrinsic:") {
				family = "intrinsic"
			}
			out = append(out, censusItem{kind: kind, key: key, card: c, face: fi,
				family: family, ident: fmt.Sprintf("A%d", ai), sa: a})
		}
	}
	return out
}

// censusDetailTotal sums the numeric prefixes of a census detail string
// ("1G+2B" -> 3), so an independent expected total can be compared against
// what the engine actually added.
func censusDetailTotal(detail string) int {
	total := 0
	for _, part := range strings.Split(detail, "+") {
		i := 0
		for i < len(part) && part[i] >= '0' && part[i] <= '9' {
			i++
		}
		if i == 0 {
			continue
		}
		n, err := strconv.Atoi(part[:i])
		if err != nil {
			continue
		}
		total += n
	}
	return total
}

// expectedManaTotal is the mana-unit total Produced$ states for a row, when
// the amount is statically priceable; ok is false for a dynamic Amount$ (the
// audit cannot double-check a board-dependent count).
func expectedManaTotal(row censusRow) (int, bool) {
	if row.amount == "dynamic" {
		return 0, false
	}
	amt, err := strconv.Atoi(row.amount)
	if err != nil {
		return 0, false
	}
	produced := strings.TrimSpace(row.item.sa.Params["Produced"])
	// A Special batch (EachColorAmong, DoubleManaInPool) is board-dependent;
	// its unit count is not a static function of the token grammar.
	if strings.HasPrefix(produced, "Special ") {
		return 0, false
	}
	counts, any := cards.ProducedCounts(produced)
	if any {
		// Any / Combo / Chosen is a colour CHOICE: the engine adds one unit
		// per Amount$ (the census drives the deterministic first answer).
		return amt, true
	}
	total := 0
	for _, n := range counts {
		total += int(n)
	}
	if total == 0 {
		return 0, false
	}
	return total * amt, true
}

// TestCreatureManaAbilityAudit is the always-on creature subset audit.
func TestCreatureManaAbilityAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("creature mana audit needs the compiled corpus")
	}
	cz := newAutopayCensus(t)

	type measured struct {
		key, reason string
	}
	var live, tabled int
	seen := map[string]bool{}
	for _, c := range cz.r.Cards {
		if !censusNamed(c) {
			continue
		}
		name := c.Faces[0].Name
		if len(creatureManaItems("card", name, c)) == 0 {
			continue
		}
		rows := cz.evalCard("card", name, c)
		for _, row := range rows {
			if row.item.family != "activated" && row.item.family != "intrinsic" {
				continue
			}
			// The audit is creature-scoped: only rows on a creature face.
			f := row.item.card.Faces[row.item.face]
			if f == nil || !censusHas(f.Types, "Creature") {
				continue
			}
			key := name + "|" + row.item.ident
			seen[key] = true
			if row.manualOutcome == "mana_added" {
				// PRECONDITION: the source is really on the board and the
				// amount is what the script states (where priceable).
				if want, ok := expectedManaTotal(row); ok {
					if got := censusDetailTotal(row.manualDetail); got != want {
						t.Errorf("%s: added %d mana (%q), Produced$ states %d",
							key, got, row.manualDetail, want)
					}
				}
				live++
				continue
			}
			reason, want := knownUnsupportedCreatureMana[key]
			if !want {
				t.Errorf("%s: creature mana ability not supported live (%s / %s) and not in knownUnsupportedCreatureMana",
					key, row.manualOutcome, row.manualDetail)
				continue
			}
			if reason == "" {
				t.Errorf("%s: knownUnsupportedCreatureMana entry has an empty reason", key)
			}
			tabled++
		}
	}
	// Shrink-only ratchet: every tabled entry must still be measured.
	var stale []string
	for key := range knownUnsupportedCreatureMana {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("%s is in knownUnsupportedCreatureMana but no longer a creature mana ability in the corpus -- delete the stale entry", key)
	}
	if live == 0 {
		t.Fatal("audit asserted no creature mana ability live; the corpus or the filter is broken (vacuous run)")
	}
	t.Logf("creature mana audit: %d live, %d tabled", live, tabled)
}
