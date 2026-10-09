package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// historyKind is one Count$ head that counts this turn's events and the
// builder that produces n of them. The table is the one place a head maps to
// an event, so a new head is one row, not one branch per card.
type historyKind struct {
	// needle is matched against the lowercased SVar body.
	needle string
	// build returns the candidate preludes for n events; it returns none when
	// the corpus lacks n distinct probes or the body asks for something no
	// prelude can supply (the engine is still the authority on a fire).
	build func(reg *cards.Registry, src, lower string, n int) []conditionPrelude
}

var historyKinds = []historyKind{
	{"thisturnentered_graveyard_", deathHistory},
	{"thisturnentered_battlefield_", enterHistory},
	{"thisturnentered_exile_", exileHistory},
	{"countersaddedthisturn", counterHistory},
	{"leftgraveyardthisturn", leftGraveyardHistory},
	{"thisturncast_", castHistory},
	{"numdamagethisturn", damageHistory},
	{"count$attackersdeclared", attackHistory},
	{"count$isprime", primeHistory},
	{"lifelostthisturn", lifeHistory},
	{"youscrythisturn", scryHistory},
	{"yousurveilthisturn", surveilHistory},
}

var (
	// historyVictims are the plain creatures a removal spell is aimed at,
	// each a distinct name so every cast reference is unambiguous.
	historyVictims = []string{"Grizzly Bears", "Llanowar Elves", "Elvish Mystic", "Nessian Asp"}
	// historyKillers destroy a nonblack, nonartifact creature, distinct names.
	historyKillers = []string{"Murder", "Doom Blade", "Go for the Throat", "Terror"}
	// historyBurn deal damage to a player, distinct names.
	historyBurn = []string{"Shock", "Lightning Bolt", "Lava Spike"}
	// historyCreatureSpells and historyNoncreatureSpells are the spells a
	// "you cast" count takes in order, with the targets each one needs.
	historyCreatureSpells    = []string{"Grizzly Bears", "Llanowar Elves", "Elvish Mystic", "Nessian Asp"}
	historyNoncreatureSpells = []string{"Shock", "Lightning Bolt", "Lava Spike", "Divination", "Angel's Mercy"}
	historyMixedSpells       = []string{"Grizzly Bears", "Llanowar Elves", "Elvish Mystic", "Divination"}
	historyBurnTargets       = map[string]bool{"Shock": true, "Lightning Bolt": true, "Lava Spike": true}
)

// historyPreludes builds, from an SVar body that counts this turn's events,
// the preludes that produce n of them with distinct probe names. src is the
// card under test, which a self-directed head (a counter on the source)
// aims the probe at. An unrecognised body yields none.
func historyPreludes(reg *cards.Registry, src, body string, n int) []conditionPrelude {
	if n < 1 {
		n = 1
	}
	lower := strings.ToLower(body)
	var out []conditionPrelude
	for _, k := range historyKinds {
		if strings.Contains(lower, k.needle) {
			out = append(out, k.build(reg, src, lower, n)...)
		}
	}
	return out
}

// historyCasts casts each probe in turn and resolves it. It reports false when
// a probe is not castable from the corpus.
func historyCasts(reg *cards.Registry, probes []string, target func(string) []string) ([]oraclegen.Step, bool) {
	var steps []oraclegen.Step
	for _, p := range probes {
		var targets []string
		if target != nil {
			targets = target(p)
		}
		st, ok := castProbe(reg, p, targets...)
		if !ok {
			return nil, false
		}
		steps = append(steps, st, oraclegen.Step{Op: "resolve"})
	}
	return steps, true
}

// firstN is the first n names of pool other than skip, or nil when the pool is
// short: a count of n distinct events needs n distinct probes.
func firstN(reg *cards.Registry, pool []string, skip string, n int) []string {
	var out []string
	for _, p := range existingCards(reg, pool) {
		if p != skip {
			out = append(out, p)
		}
	}
	if len(out) < n {
		return nil
	}
	return out[:n]
}

// deathHistory kills n creatures: creature cards reaching a graveyard from the
// battlefield. An OppCtrl head kills the opponent's creatures.
func deathHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	if !strings.Contains(lower, "creature") {
		return nil
	}
	victims, killers := firstN(reg, historyVictims, src, n), firstN(reg, historyKillers, src, n)
	if victims == nil || killers == nil {
		return nil
	}
	side := "p0"
	if strings.Contains(lower, "oppctrl") {
		side = "p1"
	}
	i := 0
	steps, ok := historyCasts(reg, killers, func(string) []string { i++; return []string{side + ":" + victims[i-1]} })
	if !ok {
		return nil
	}
	pre := conditionPrelude{hand: killers, steps: steps}
	if side == "p1" {
		pre.opponentBattlefield = victims
	} else {
		pre.battlefield = victims
	}
	return []conditionPrelude{pre}
}

// enterHistory casts n distinct creatures of the head's type. A face-down
// entry has no cause (a morph or disguise cast), so it yields none and the
// skip namer names it.
func enterHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	if strings.Contains(lower, "facedown") {
		return nil
	}
	var out []conditionPrelude
	for _, tp := range typedBoardProbes {
		if !strings.Contains(lower, "_"+tp.word) {
			continue
		}
		probes := firstN(reg, tp.probes, src, n)
		if probes == nil {
			continue
		}
		if steps, ok := historyCasts(reg, probes, nil); ok {
			out = append(out, conditionPrelude{hand: probes, steps: steps})
		}
	}
	return out
}

// exileHistory exiles n of p0's own creatures: cards put into exile.
func exileHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	victims, spells := firstN(reg, historyVictims, src, n), firstN(reg, exileZoneProbes, src, n)
	if victims == nil || spells == nil {
		return nil
	}
	i := 0
	steps, ok := historyCasts(reg, spells, func(string) []string { i++; return []string{"p0:" + victims[i-1]} })
	if !ok {
		return nil
	}
	return []conditionPrelude{{battlefield: victims, hand: spells, steps: steps}}
}

// counterHistory puts a counter on a creature with Battlegrowth, on the
// source when the head names Card.Self.
func counterHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	const probe = "Battlegrowth"
	if n != 1 || len(existingCards(reg, []string{probe})) == 0 {
		return nil
	}
	target := "p0:" + historyVictims[0]
	pre := conditionPrelude{hand: []string{probe}, battlefield: []string{historyVictims[0]}}
	if strings.Contains(lower, "card.self") {
		target, pre.battlefield = "p0:"+src, nil
	}
	st, ok := castProbe(reg, probe, target)
	if !ok {
		return nil
	}
	pre.steps = []oraclegen.Step{st, {Op: "resolve"}}
	return []conditionPrelude{pre}
}

// leftGraveyardHistory returns n creature cards from p0's graveyard to hand.
func leftGraveyardHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	// The graveyard cards are the pool's last names: an attack cause's own
	// attacker is Grizzly Bears, and one name in two zones is ambiguous.
	victims, spells := firstN(reg, reverseNames(historyVictims), src, n), firstN(reg, reanimateProbes, src, n)
	if victims == nil || spells == nil {
		return nil
	}
	i := 0
	steps, ok := historyCasts(reg, spells, func(string) []string { i++; return []string{"p0:" + victims[i-1]} })
	if !ok {
		return nil
	}
	return []conditionPrelude{{graveyard: victims, hand: spells, steps: steps}}
}

// castHistory casts n distinct spells of the head's kind. A cast from
// anywhere but hand has no cause here, so it yields none.
func castHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	if strings.Contains(lower, "wascastfrom") {
		return nil
	}
	pool := historyMixedSpells
	switch {
	case strings.Contains(lower, "instant") || strings.Contains(lower, "sorcery") || strings.Contains(lower, "noncreature"):
		pool = historyNoncreatureSpells
	case strings.Contains(lower, "creature"):
		pool = historyCreatureSpells
	}
	spells := firstN(reg, pool, src, n)
	if spells == nil {
		return nil
	}
	steps, ok := historyCasts(reg, spells, func(p string) []string {
		if historyBurnTargets[p] {
			return []string{"p1"}
		}
		return nil
	})
	if !ok {
		return nil
	}
	return []conditionPrelude{{hand: spells, steps: steps}}
}

// damageHistory deals damage to the opponent from n distinct sources.
func damageHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	spells := firstN(reg, historyBurn, src, n)
	if spells == nil {
		return nil
	}
	steps, ok := historyCasts(reg, spells, func(string) []string { return []string{"p1"} })
	if !ok {
		return nil
	}
	return []conditionPrelude{{hand: spells, steps: steps}}
}

// attackHistory attacks with n creatures. One attacker is the shared
// conditionPreludes candidate already.
func attackHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	if n < 2 {
		return nil
	}
	fillers := firstN(reg, attackFillers, src, n)
	if fillers == nil {
		return nil
	}
	attackers := make([]string, len(fillers))
	for i, f := range fillers {
		attackers[i] = "p0:" + f
	}
	return []conditionPrelude{{battlefield: fillers, steps: []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers},
		{Op: "pass_to", Step: "main2"},
	}}}
}

// primeHistory plays a land with one or two lands already in play: a land
// entered this turn and the lands controlled number two or three.
func primeHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	lands := existingCards(reg, []string{"Plains", "Island", "Swamp"})
	if len(lands) < 3 {
		return nil
	}
	var out []conditionPrelude
	for setup := 1; setup <= 2; setup++ {
		play := lands[setup]
		out = append(out, conditionPrelude{
			battlefield: lands[:setup],
			hand:        []string{play},
			steps:       []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + play}},
		})
	}
	return out
}

// lifeHistory loses p0 life with Shock, after gaining some when the body also
// counts life gained ("gained and lost life this turn").
func lifeHistory(reg *cards.Registry, src, lower string, n int) []conditionPrelude {
	if !strings.Contains(lower, "playercountpropertyyou") {
		return nil
	}
	shock, ok := castProbe(reg, "Shock", "p0")
	if !ok {
		return nil
	}
	loss := []oraclegen.Step{shock, {Op: "resolve"}}
	out := []conditionPrelude{{hand: []string{"Shock"}, steps: loss}}
	if steps, ok := historyCasts(reg, []string{"Angel's Mercy"}, nil); ok {
		out = append(out, conditionPrelude{hand: []string{"Angel's Mercy", "Shock"}, steps: append(steps, loss...)})
	}
	return out
}

// historyNamedGap names a turn-history head no prelude can build: the cause
// that makes it true is not a cast or an ordinary event.
func historyNamedGap(lower string) string {
	switch {
	case strings.Contains(lower, "facedown"):
		return "face-down entry has no cause"
	case strings.Contains(lower, "wascastfrom"):
		return "cast not from hand has no cause"
	}
	return ""
}

// reverseNames is names back to front, as a new slice.
func reverseNames(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[len(names)-1-i] = n
	}
	return out
}
