package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// costAffinityCount is the board an affinity probe asks for. Three permanents
// make the reduction visible without depending on the corpus-specific maximum.
const costAffinityCount = 3

// attackerFixture is the attacking/blocking stand-in creature: an ordinary
// 2/2 with no evasion the combat engine always accepts.
const attackerFixture = "Grizzly Bears"

// costConditionProbes derives the fixtures an own-spell ReduceCost static needs
// from the static's own parameters, so its reduction is visible: the count the
// Amount$ SVar tallies, and every gate the static carries (IsPresent$,
// CheckSVar$, Condition$). Each gate reuses the activation-restriction
// preludes, so the board, graveyard and turn history are built by one shared
// helper rather than a table of cards in the cost template.
//
// The result is the base probe with each candidate prelude applied, and the
// generic mana the reduction removes. A non-empty reason names a gate no
// prelude can establish; a gate with no fixture never falls back to a guess,
// because the probe would then be cast at a price the static does not give.
func costConditionProbes(reg *cards.Registry, f *cards.Face, st cards.Static, name string, base costProbe) ([]costProbe, int, string) {
	reduction, amount, gap := costAmountFixture(reg, f, st)
	if gap != "" {
		return nil, 0, gap
	}
	var sources [][]conditionPrelude
	if len(amount) > 0 {
		sources = append(sources, amount)
	}
	gates, gap := costGateFixtures(reg, f, st)
	if gap != "" {
		return nil, 0, gap
	}
	sources = append(sources, gates...)
	cands := restrictCandidates(sources)
	if kept := onP0Side(cands); len(kept) < len(cands) {
		if len(kept) == 0 {
			return nil, 0, "cost static condition needs opponent board"
		}
		cands = kept
	}
	if len(cands) == 0 {
		// Nothing to establish (a fixed Amount$ and no gate): the bare scenario.
		cands = []conditionPrelude{{}}
	}
	probes := make([]costProbe, 0, len(cands))
	for _, pre := range cands {
		probes = append(probes, base.withPrelude(name, pre))
	}
	return probes, reduction, ""
}

// onP0Side keeps the preludes the probe's setup can hold: a costProbe has no
// opponent hand, but its opponentBattlefield list is real setup (cost.go's
// costProbe carries it and costProbeItem places it), so a prelude that only
// puts permanents on p1's battlefield is kept too.
func onP0Side(cands []conditionPrelude) []conditionPrelude {
	var kept []conditionPrelude
	for _, c := range cands {
		if len(c.opponentHand) == 0 {
			kept = append(kept, c)
		}
	}
	return kept
}

// costCombatWord is the first word of a filter naming combat state a
// main-phase setup cannot give an object (attacking, blocking). The cast is a
// sorcery-speed probe, and a placed creature is not an attacker.
func costCombatWord(filter string) string {
	for _, w := range affectedWords(filter) {
		lower := strings.ToLower(w)
		if strings.HasPrefix(lower, "attacking") || strings.HasPrefix(lower, "blocking") || strings.HasPrefix(lower, "blocked") {
			return w
		}
	}
	return ""
}

// withPrelude is the probe with a condition prelude's cards, steps and seat
// state added. Battlefield and graveyard repeats are kept: a count-aware gate
// ("three or more creatures") needs its repeats.
func (p costProbe) withPrelude(name string, pre conditionPrelude) costProbe {
	p.hand = append(append([]string(nil), p.hand...), pre.hand...)
	p.battlefield = append(append([]string(nil), p.battlefield...), pre.battlefield...)
	p.opponentBattlefield = append(append([]string(nil), p.opponentBattlefield...), pre.opponentBattlefield...)
	p.graveyard = append(append([]string(nil), p.graveyard...), pre.graveyard...)
	p.pre = append(append([]oraclegen.Step(nil), p.pre...), pre.steps...)
	if len(pre.tapped) > 0 || len(pre.counters) > 0 || pre.life > 0 {
		prev := p.seat
		state := conditionPrelude{tapped: pre.tapped, counters: pre.counters, life: pre.life}
		p.seat = func(s *oraclegen.Seat) {
			if prev != nil {
				prev(s)
			}
			*s, _ = applyActivationPrelude(*s, name, state)
		}
	}
	return p
}

// costAmountFixture is the reduction the probe expects and the prelude
// candidates that make it so. A literal Amount$ needs none; affinity and an
// Amount$ SVar are served by the count the SVar body tallies.
func costAmountFixture(reg *cards.Registry, f *cards.Face, st cards.Static) (int, []conditionPrelude, string) {
	amount := st.Params["Amount"]
	if n, err := strconv.Atoi(amount); err == nil {
		if n < 1 {
			return 0, nil, "reduction below one"
		}
		return n, nil, ""
	}
	if amount == "" {
		return 0, nil, "cost static has no amount"
	}
	if reduction, pres, gap, ok := costPowerAmountFixture(reg, f, amount); ok {
		return reduction, pres, gap
	}
	if gap := costOptionalGenericPaidGap(f, amount); gap != "" {
		return 0, nil, gap
	}
	reduction, compare := 1, "GE1"
	if strings.HasPrefix(st.Params["KeywordLine"], "Affinity:") {
		reduction, compare = costAffinityCount, fmt.Sprintf("GE%d", costAffinityCount)
	}
	pres, gap := costCountPrelude(reg, f, amount, compare)
	if gap != "" {
		return 0, nil, gap
	}
	return reduction, pres, ""
}

// costGateFixtures is one candidate source per gate the static carries.
func costGateFixtures(reg *cards.Registry, f *cards.Face, st cards.Static) ([][]conditionPrelude, string) {
	var sources [][]conditionPrelude
	switch cond := strings.ToLower(st.Params["Condition"]); cond {
	case "", "playerturn":
		// The generated cast is p0's own turn.
	case "delirium":
		sources = append(sources, []conditionPrelude{{graveyard: []string{"Wastes", "Opt", "Grizzly Bears", "Silver Myr"}}})
	case "notplayerturn":
		return nil, "NotPlayerTurn needs an opponent-turn probe"
	default:
		return nil, "cost static condition (" + st.Params["Condition"] + ")"
	}
	if spec := strings.TrimSpace(st.Params["IsPresent"]); spec != "" {
		if w := costCombatWord(spec); w != "" {
			return nil, costCombatGap(spec, w)
		}
		pres, gap := activationPresentPrelude(reg, nil, "", spec, st.Params["PresentZone"], st.Params["PresentCompare"])
		if len(pres) == 0 && gap != "" {
			// An alternative no p0 setup reaches may name an opponent's
			// battlefield, which the probe's setup carries.
			if opp, ok := costOpponentPresentPreludes(reg, spec, st.Params["PresentZone"], st.Params["PresentCompare"]); ok {
				sources = append(sources, opp)
			} else {
				return nil, costGateGap(spec, gap)
			}
		} else if len(pres) > 0 {
			sources = append(sources, pres)
		}
	}
	if check := strings.TrimSpace(st.Params["CheckSVar"]); check != "" && check != st.Params["Amount"] {
		pres, gap := costCheckPrelude(reg, f, check, st.Params["SVarCompare"])
		if gap != "" {
			return nil, gap
		}
		if len(pres) > 0 {
			sources = append(sources, pres)
		}
	}
	return sources, ""
}

// costGateGap names an unbuilt gate. An opponent's board has no
// conditionPrelude field (fitConditionPrelude drops it), so it gets its own
// reason rather than the bare one.
func costGateGap(spec, gap string) string {
	for _, group := range strings.Split(spec, ",") {
		if staticOpposing(group) || costCombatWord(group) == "attackingYou" {
			return "cost static condition needs opponent board (" + spec + ")"
		}
	}
	return strings.Replace(gap, "activation restriction:", "cost static condition:", 1)
}

// costValidBody splits a Count$Valid<Zones> <filter> body into the zone a
// fixture places cards in and the filter. Zones is a comma list (Count$ValidGraveyard,Battlefield
// Cave.YouCtrl counts both); any one of them satisfies a count of at least one,
// and the battlefield is preferred. A zone the placement helpers cannot fill
// (exile, library) or any other body gives "", "".
func costValidBody(body string) (zone, filter string) {
	head, rest, ok := strings.Cut(body, " ")
	zones, found := strings.CutPrefix(head, "Count$Valid")
	if !ok || !found {
		return "", ""
	}
	if zones == "" {
		return "Battlefield", rest
	}
	var listed []string
	for _, z := range strings.Split(zones, ",") {
		switch {
		case strings.EqualFold(z, "Battlefield"), strings.EqualFold(z, "Graveyard"), strings.EqualFold(z, "Hand"):
			listed = append(listed, z)
		default:
			return "", ""
		}
	}
	for _, z := range listed {
		if strings.EqualFold(z, "Battlefield") {
			return "Battlefield", rest
		}
	}
	return listed[0], rest
}

// costCombatGap names a gate on combat state. An attacker aimed at p0 needs the
// opponent's board; any other combat state needs a combat the probe is not in.
func costCombatGap(filter, word string) string {
	if word == "attackingYou" {
		return "cost static condition needs opponent board (" + filter + ")"
	}
	return "cost static condition needs combat (" + filter + ")"
}

// costOpponentPresentPreludes builds the candidates a present filter whose
// alternatives name an opponent-controlled battlefield permanent ask for: the
// same stand-in cards the shared present helpers pick for the group text,
// carried on p1's battlefield (conditionPrelude.opponentBattlefield), where
// the cost probe's setup holds them. ok is false when no alternative names a
// placeable opponent board permanent.
func costOpponentPresentPreludes(reg *cards.Registry, spec, zone, compare string) ([]conditionPrelude, bool) {
	if strings.TrimSpace(zone) == "" {
		zone = "Battlefield"
	}
	if !strings.EqualFold(zone, "Battlefield") {
		return nil, false
	}
	n := staticCountFrom(compare)
	if n < 1 {
		n = 1
	}
	var out []conditionPrelude
	for _, group := range strings.Split(spec, ",") {
		group = strings.TrimSpace(group)
		if group == "" || !staticOpposing(group) || costCombatWord(group) != "" {
			continue
		}
		pre := conditionPrelude{}
		if name := creatureForPowerFloor(group); name != "" {
			pre.opponentBattlefield = oraclegen.Repeat(name, n)
		} else if fx, ok := staticPresence(group, zone, n); ok && len(fx.p1Battlefield) == n {
			pre.opponentBattlefield = fx.p1Battlefield
		} else if fx, ok := activationSubtypePresence(reg, group, zone, n); ok && len(fx.p1Battlefield) == n {
			pre.opponentBattlefield = fx.p1Battlefield
		} else {
			continue
		}
		out = append(out, pre)
	}
	return out, len(out) > 0
}

// costAttackPrelude is the combat a count of attacking creatures tallies: the
// word's side declares its creature attacking the defender, and the probe
// casts in the declare-attackers priority window while the attack holds. On
// p1's turn p1 holds priority after the attack, so it passes first (the same
// shape costTargetProbe's attacking target uses). ok is false for a word no
// fixture attack can make true.
//
// attackerFixture is the attacking stand-in: an ordinary 2/2 with no
// evasion the combat engine always accepts.
func costAttackPrelude(word string, count int) (conditionPrelude, bool) {
	if count < 1 {
		count = 1
	}
	side, defender := 0, "p1"
	if strings.EqualFold(word, "attackingYou") {
		side, defender = 1, "p0"
	} else if !strings.EqualFold(word, "attacking") {
		return conditionPrelude{}, false
	}
	ref := fmt.Sprintf("p%d:%s", side, attackerFixture)
	pre := conditionPrelude{steps: []oraclegen.Step{
		{Op: "pass_to", Step: "declare-attackers", Active: fmt.Sprintf("p%d", side)},
		{Op: "attack", Seat: side, Defender: defender, Attackers: oraclegen.Repeat(ref, count)},
	}}
	if side == 0 {
		pre.battlefield = oraclegen.Repeat(attackerFixture, count)
	} else {
		pre.opponentBattlefield = oraclegen.Repeat(attackerFixture, count)
		pre.steps = append(pre.steps, oraclegen.Step{Op: "pass", Seat: 1})
	}
	return pre, true
}

// costCountPrelude builds the board or graveyard an SVar body counts, at least
// compare's count. A Count$Valid / Count$ValidGraveyard body goes through the
// filter-aware present prelude (zone, count, controller, power floor, registry
// subtype); any other body goes through the SVar prelude.
func costCountPrelude(reg *cards.Registry, f *cards.Face, svar, compare string) ([]conditionPrelude, string) {
	body := strings.TrimSpace(f.SVars[svar])
	if body == "" {
		body = svar
	}
	// "for each creature that attacked this turn" is a PlayerCountPlayers$
	// head the shared condition classifier does not name; p0 attacks with the
	// counted creatures, then casts the probe in the second main.
	if attackerCountBody(body) {
		return attackerCountPrelude(reg, compare)
	}
	// A SacrificedThisTurn count filters the sacrificed objects by type: the
	// shared sacrifice prelude's creature fixture covers a Creature spec only
	// (cost_svar_amount.go), so an Artifact spec gets its own sacrifice.
	if pre, ok := costSacrificePrelude(reg, body); ok {
		return []conditionPrelude{pre}, ""
	}
	zone, filter := costValidBody(body)
	if filter != "" {
		if i := strings.IndexAny(filter, "/$"); i >= 0 {
			filter = filter[:i]
		}
		if w := costCombatWord(filter); w != "" {
			if pre, ok := costAttackPrelude(w, staticCountFrom(compare)); ok {
				return []conditionPrelude{pre}, ""
			}
			return nil, costCombatGap(filter, w)
		}
		if pres, gap := activationPresentPrelude(reg, nil, "", strings.TrimSpace(filter), zone, compare); len(pres) > 0 {
			return pres, ""
		} else if gap != "" {
			return nil, costGateGap(filter, gap)
		}
	}
	pres, gap := activationSVarPrelude(reg, f, svar, compare)
	if len(pres) == 0 {
		return nil, costGateGap(body, gap)
	}
	return pres, ""
}

// costCheckPrelude builds the setup a CheckSVar$ gate needs under its
// SVarCompare$. "At least" comparisons ask for the count; "at most" and "equals
// zero" ones are the bare scenario, except a life total, which setup sets to
// the cap.
func costCheckPrelude(reg *cards.Registry, f *cards.Face, check, compare string) ([]conditionPrelude, string) {
	compare = strings.TrimSpace(compare)
	if len(compare) < 3 {
		return costCountPrelude(reg, f, check, compare)
	}
	op, rhs := strings.ToUpper(compare[:2]), compare[2:]
	n, err := strconv.Atoi(rhs)
	if err != nil {
		return costCountPrelude(reg, f, check, compare)
	}
	body := strings.TrimSpace(f.SVars[check])
	if body == "" {
		body = check
	}
	switch op {
	case "GT":
		return costCountPrelude(reg, f, check, fmt.Sprintf("GE%d", n+1))
	case "LE", "LT":
		if op == "LT" {
			n--
		}
		if n < 0 || (n < 1 && strings.EqualFold(body, "Count$YourLifeTotal")) {
			return nil, "cost static condition: SVar (" + svarLabel(check, body) + ")"
		}
		if strings.EqualFold(body, "Count$YourLifeTotal") {
			return []conditionPrelude{{life: int32(n)}}, ""
		}
		// A count with nothing counted is below any cap of at least zero.
		return nil, ""
	case "EQ":
		if n == 0 {
			return nil, ""
		}
	}
	return costCountPrelude(reg, f, check, compare)
}
