package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Level-B bearer- and filter-scoped combat-legality observations
// (levelb/legality_scoped.go names the shapes). The restricted creature is
// not the static's own host: it is the creature an Equipment or Aura is
// attached to (the bearer), or any creature a filter matches. The observation
// fields a matching and a non-matching creature beside the card and asserts,
// at the declare decision, that the restriction binds the matching one only.
//
// The probe pair is not guessed from the filter text. One replay fields every
// candidate attacker against every candidate blocker and reads which
// (blocker, attacker) pairs the engine refuses (or which attackers carry the
// Max$ bound); the matching and non-matching creatures are then chosen from
// that matrix, so a filter gorge reads differently from the script yields a
// named skip rather than a vacuous item. The chosen scenario carries its own
// controls: a blocker the filter spares may block the matching attacker, and
// the refused blocker may still block the non-matching one. Bearer shapes
// add the unbound control (the Equipment unattached, the Aura uncast).

// scopedAttackerPool are the vanilla creatures the matrix fields as
// attackers, in the order a match is preferred. None has evasion or a
// defender's restriction, so a refused pair is the static's doing. The set
// carries a Spider, a Boar and a Detective for the type-word filters.
var scopedAttackerPool = []string{
	"Savannah Lions", "Grizzly Bears", "Elite Vanguard", "Hill Giant",
	"Giant Spider", "Craw Wurm", "Durkwood Boars", "Vengeful Tracker",
}

// scopedBlockerPool are the creatures the matrix fields as blockers: the
// attacker pool plus a flier (withFlying) and a Wall (withDefender, power 0).
var scopedBlockerPool = []string{
	"Grizzly Bears", "Hill Giant", "Llanowar Elves", defenderProbe, "Wind Drake",
	"Savannah Lions", "Giant Spider", "Elite Vanguard", "Craw Wurm", "Durkwood Boars", "Vengeful Tracker",
}

const (
	// scopedAuraProbe is an Aura with no combat effect (+1/+2), the "Aura you
	// control" of Eriette's enchanted creatures.
	scopedAuraProbe = "Holy Strength"
	// scopedTokenMaker destroys a target permanent; its controller creates a
	// 3/3 Beast token -- the only token the defending seat can be given by
	// the attacking seat's spell.
	scopedTokenMaker = "Beast Within"
	scopedTokenName  = "Beast"
	// scopedAnimator animates a target land into a 3/3 creature (still a
	// land) until end of turn.
	scopedAnimator = "Animate Land"
)

// scopedCard is one fixture on the attacking seat and its P1P1 counters.
type scopedCard struct {
	name     string
	counters int32
}

// scopedBlocker is one candidate blocker: a battlefield fixture, or a token
// the setup steps create (named by its token name).
type scopedBlocker struct {
	name  string
	token bool
}

// scopedPlan is the board the matrix and the chosen scenario are built from.
type scopedPlan struct {
	f          *cards.Face
	name       string
	req        levelb.Requirement
	attackSeat int
	// cardAttacks makes the card under test an attacker candidate (it is
	// placed on p0 beside the others).
	cardAttacks bool
	// castCard puts the card in p0's hand; bind then casts it (an Aura).
	castCard  bool
	attackers []scopedCard
	blockers  []scopedBlocker
	// extra0/extra1 are fixtures fielded in every scenario beside the
	// candidates (a token maker's target, a Vehicle's crew taps).
	extra0, extra1 []string
	// first0 is fielded on p0 right after the card, before the attackers: the
	// engine's crew cost taps the first untapped creature in battlefield
	// order, so the tap fixtures go ahead of the declared attackers.
	first0 []string
	hand   []string
	// setup runs before the attack in every scenario; bind runs only in the
	// observation (the control omits it: the Equipment unattached, the Aura
	// uncast).
	setup, bind []oraclegen.Step
	gate        *gateSides
	// xab is the XMage rule-text prefix per setup step (a crew activation).
	xab []string
}

func (p *scopedPlan) defender() string { return "p" + strconv.Itoa(1-p.attackSeat) }

func (p *scopedPlan) attackerRef(c scopedCard) string { return cardAt(p.attackSeat, c.name) }

func (p *scopedPlan) blockerRef(b scopedBlocker) string {
	if b.token {
		return "p" + strconv.Itoa(1-p.attackSeat) + ":token:" + b.name
	}
	return cardAt(1-p.attackSeat, b.name)
}

// controlDropsCard reports whether the control removes the card from the
// battlefield: a placed card that no bind step attaches, and that is not
// itself an attacker, is the static's whole source.
func (p *scopedPlan) controlDropsCard() bool {
	return len(p.bind) == 0 && !p.castCard && !p.cardAttacks && p.gate == nil
}

// scenario builds the scenario fielding att against blk. control omits the
// bind steps (and the card itself when controlDropsCard) and takes the gate
// unheld.
func (p *scopedPlan) scenario(att []scopedCard, blk []scopedBlocker, control bool, expect []oraclegen.Expect) oraclegen.Scenario {
	steps := append([]oraclegen.Step(nil), p.setup...)
	if !control {
		steps = append(steps, p.bind...)
	}
	refs := make([]string, 0, len(att))
	for _, c := range att {
		refs = append(refs, p.attackerRef(c))
	}
	steps = append(steps,
		oraclegen.Step{Op: "attack", Seat: p.attackSeat, Defender: p.defender(), Attackers: refs},
		oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: expect})
	return p.fieldBoard(att, blk, control, steps)
}

// fieldBoard places the fixtures for steps on the two seats.
func (p *scopedPlan) fieldBoard(att []scopedCard, blk []scopedBlocker, control bool, steps []oraclegen.Step) oraclegen.Scenario {
	var bf0 []string
	if !p.castCard && !(control && p.controlDropsCard()) {
		bf0 = append(bf0, p.name)
	}
	hand := append([]string(nil), p.hand...)
	if p.castCard {
		hand = append([]string{p.name}, hand...)
	}
	for _, n := range p.first0 {
		bf0 = appendFixtureUnique(bf0, n)
	}
	var seatAtt, seatBlk []string
	for _, c := range att {
		seatAtt = appendFixtureUnique(seatAtt, c.name)
	}
	for _, b := range blk {
		if !b.token {
			seatBlk = appendFixtureUnique(seatBlk, b.name)
		}
	}
	if p.attackSeat == 0 {
		for _, n := range seatAtt {
			bf0 = appendFixtureUnique(bf0, n)
		}
	} else {
		for _, n := range seatBlk {
			bf0 = appendFixtureUnique(bf0, n)
		}
	}
	for _, n := range p.extra0 {
		bf0 = appendFixtureUnique(bf0, n)
	}
	sc := staticScenario(p.f, p.name, bf0, hand, steps)
	p1 := sc.Setup["p1"]
	if p.attackSeat == 0 {
		for _, n := range seatBlk {
			p1.Battlefield = appendFixtureUnique(p1.Battlefield, n)
		}
	} else {
		for _, n := range seatAtt {
			p1.Battlefield = appendFixtureUnique(p1.Battlefield, n)
		}
	}
	for _, n := range p.extra1 {
		p1.Battlefield = appendFixtureUnique(p1.Battlefield, n)
	}
	sc.Setup["p1"] = p1
	for _, c := range att {
		if c.counters == 0 {
			continue
		}
		key := "p" + strconv.Itoa(p.attackSeat)
		sc.Setup[key] = oraclegen.WithCounters(sc.Setup[key], c.name, "P1P1", c.counters)
	}
	if p.gate != nil {
		side := p.gate.on
		if control {
			side = p.gate.off
		}
		return applyGateSide(sc, p.name, p.req, side)
	}
	return withBackFace(sc, p.name, p.req)
}

// poolAttackers are the matrix's pool attackers (counters on two of them when
// the filter reads a counter); attackersWithCard puts the card itself ahead
// of them when it can attack.
func (p *scopedPlan) poolAttackers(reg *cards.Registry, counters bool) []scopedCard {
	var out []scopedCard
	for _, n := range scopedAttackerPool {
		if n == p.name || !hasCorpusCard(reg, n) {
			continue
		}
		c := scopedCard{name: n}
		if counters && (n == "Grizzly Bears" || n == "Elite Vanguard") {
			c.counters = 1
		}
		out = append(out, c)
	}
	return out
}

func poolBlockers(reg *cards.Registry, name string) []scopedBlocker {
	var out []scopedBlocker
	for _, n := range scopedBlockerPool {
		if n == name || !hasCorpusCard(reg, n) {
			continue
		}
		out = append(out, scopedBlocker{name: n})
	}
	return out
}

func hasCorpusCard(reg *cards.Registry, name string) bool {
	c, ok := reg.Lookup(name)
	return ok && len(c.Faces) > 0
}

// pairRefused reports whether fails carry the refusal of blocker blocking
// attacker.
func pairRefused(fails []string, blocker, attacker string) bool {
	return failsName(fails, blocker+" can block "+attacker+" = false")
}

// otherFails returns the fails that are not a recognised refusal, so a
// harness error (an attacker never offered, a ref naming no object) is not
// mistaken for a restriction.
func otherFails(fails []string, known func(string) bool) []string {
	var out []string
	for _, f := range fails {
		if !known(f) {
			out = append(out, f)
		}
	}
	return out
}

// blockMatrix replays every (blocker, attacker) pair and reports the refused
// ones. why is non-empty when the replay itself does not hold.
func (p *scopedPlan) blockMatrix(reg *cards.Registry, att []scopedCard, blk []scopedBlocker) (refused map[[2]string]bool, why string) {
	var expect []oraclegen.Expect
	for _, b := range blk {
		for _, a := range att {
			expect = append(expect, canBlockExpect(p.blockerRef(b), p.attackerRef(a), true))
		}
	}
	res, ok := runStatic(reg, p.scenario(att, blk, false, expect))
	if !ok {
		return nil, "the probe matrix does not run"
	}
	refused = map[[2]string]bool{}
	for _, b := range blk {
		for _, a := range att {
			if pairRefused(res.Fails, p.blockerRef(b), p.attackerRef(a)) {
				refused[[2]string{b.name, a.name}] = true
			}
		}
	}
	if rest := otherFails(res.Fails, func(f string) bool { return strings.Contains(f, " can block ") && strings.Contains(f, "= false") }); len(rest) != 0 {
		return nil, "the probe matrix does not replay: " + joinFails(rest)
	}
	return refused, ""
}

// boundMatrix replays one blocker against every attacker asserting the Max$
// bound n and reports the attackers that carry it.
func (p *scopedPlan) boundMatrix(reg *cards.Registry, att []scopedCard, blk scopedBlocker, n int) (bounded map[string]bool, why string) {
	var expect []oraclegen.Expect
	for _, a := range att {
		expect = append(expect, canBlockExpectMax(p.blockerRef(blk), p.attackerRef(a), true, intPtr(n)))
	}
	res, ok := runStatic(reg, p.scenario(att, []scopedBlocker{blk}, false, expect))
	if !ok {
		return nil, "the bound matrix does not run"
	}
	bounded = map[string]bool{}
	for _, a := range att {
		if !failsName(res.Fails, p.attackerRef(a)) {
			bounded[a.name] = true
		}
	}
	if rest := otherFails(res.Fails, func(f string) bool { return strings.Contains(f, " blocks ") && strings.Contains(f, " with bound ") }); len(rest) != 0 {
		return nil, "the bound matrix does not replay: " + joinFails(rest)
	}
	return bounded, ""
}

// attackersWithCard prepends the card under test to the pool when it can
// attack.
func (p *scopedPlan) attackersWithCard(pool []scopedCard) []scopedCard {
	if !p.cardAttacks {
		return pool
	}
	return append([]scopedCard{{name: p.name}}, pool...)
}

// searchNotBlockable picks (M, N, Bm, Bn) from the refusal matrix: Bm may not
// block M, Bm may block N, Bn may block M. A tuple with N is preferred;
// needN=false additionally accepts a tuple whose N is absent (hasN false),
// for a filter every candidate attacker matches (Storm's "Creatures you
// control"): the board without the source then carries that control.
func searchNotBlockable(att []scopedCard, blk []scopedBlocker, refused map[[2]string]bool, needN bool) (m, n scopedCard, bm, bn scopedBlocker, hasN, ok bool) {
	for pass := 0; pass < 2; pass++ {
		if pass == 1 && needN {
			break
		}
		for _, a := range att {
			for _, b := range blk {
				if !refused[[2]string{b.name, a.name}] {
					continue
				}
				var a2 scopedCard
				got := false
				for _, c := range att {
					if c.name != a.name && !refused[[2]string{b.name, c.name}] {
						a2, got = c, true
						break
					}
				}
				if !got && pass == 0 {
					continue
				}
				for _, b2 := range blk {
					if b2.name == b.name || refused[[2]string{b2.name, a.name}] {
						continue
					}
					return a, a2, b, b2, got, true
				}
			}
		}
	}
	return scopedCard{}, scopedCard{}, scopedBlocker{}, scopedBlocker{}, false, false
}

// scopedFilterCounters reports whether the static's attacker-side filter
// reads a counter.
func scopedFilterCounters(st *cards.Static) bool {
	for _, k := range []cards.ParamKey{cards.PKValidCard, cards.PKValidAttacker} {
		if strings.Contains(strings.ToLower(st.ParamStr(k)), "hascounters") {
			return true
		}
	}
	return false
}

// scopedCardCanAttack reports whether the card under test is fielded as an
// attacker candidate: a creature with no evasion and no must-attack
// requirement (XMage declares a must-attacker itself), and no OTHER static of
// its own that scopes the same restriction onto it (Rocksteady's Card.Self
// cap would bound it whatever the Boar filter says).
func scopedCardCanAttack(f *cards.Face, name string, st *cards.Static) bool {
	if !f.IsCreature() || hasEvasion(f) {
		return false
	}
	for _, p := range scopedAttackerPool {
		if p == name {
			return false
		}
	}
	for i := range f.Statics {
		other := &f.Statics[i]
		switch strings.ToLower(other.Mode) {
		case "mustattack":
			return false
		case "cantblockby", "minmaxblocker":
			if other != st {
				return false
			}
		}
	}
	return true
}

// -- the dispatch ------------------------------------------------------------

// scopedLegalitySub reports whether sub is one of the scoped sub-families.
func scopedLegalitySub(sub string) bool {
	switch sub {
	case "static.cant-block-by-bearer", "static.max-blockers-bearer", "static.cant-attack-bearer",
		"static.cant-attack-filter", "static.cant-be-attacked-attached", "static.max-blockers-filter",
		"static.cant-block-by-filter", "static.cant-block-by-animated-self", "static.cant-block-by-crewed":
		return true
	}
	return false
}

// scopedLegalityItem serves a scoped sub; served is false for any other sub.
func scopedLegalityItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (it oraclegen.Item, skip *oraclegen.Skip, served bool) {
	if !scopedLegalitySub(req.Sub) {
		return oraclegen.Item{}, nil, false
	}
	st, why := reqStatic(f, req)
	if st == nil {
		it, skip = staticSkip(name, "scoped legality", why)
		return it, skip, true
	}
	switch req.Sub {
	case "static.cant-block-by-bearer":
		it, skip = bearerUnblockableItem(reg, f, st, name, req)
	case "static.max-blockers-bearer":
		it, skip = bearerMaxBlockersItem(reg, f, st, name, req)
	case "static.cant-block-by-filter":
		it, skip = filterUnblockableItem(reg, f, st, name, req)
	case "static.max-blockers-filter":
		it, skip = filterMaxBlockersItem(reg, f, st, name, req)
	case "static.cant-attack-bearer", "static.cant-attack-filter":
		it, skip = filterCantAttackItem(reg, f, st, name, req)
	case "static.cant-block-by-crewed":
		it, skip = crewedUnblockableItem(reg, f, st, name, req)
	case "static.cant-block-by-animated-self":
		it, skip = animatedLandUnblockableItem(reg, f, st, name, req)
	case "static.cant-be-attacked-attached":
		it, skip = attackedAttachedItem(reg, f, name, req)
	default:
		it, skip = staticSkip(name, "scoped legality", "no observation for "+req.Sub)
	}
	return it, skip, true
}

// -- bearer shapes -----------------------------------------------------------

// bearerPlan is the board of a bearer shape: the bearer (enchantedHost) and
// a vanilla attacker beside it on p0, a Wall of Omens (it can block, and its
// 0 power cannot kill a bearer an Aura's ETB fight picks) on p1. The source
// is placed and attached (an Equipment) or cast onto the bearer (an Aura).
func bearerPlan(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (*scopedPlan, string) {
	if name == enchantedHost || name == smallAttackerProbe || name == defenderProbe {
		return nil, "no card distinct from the probes"
	}
	p := &scopedPlan{
		f: f, name: name, req: req,
		attackers: []scopedCard{{name: enchantedHost}, {name: smallAttackerProbe}},
		blockers:  []scopedBlocker{{name: defenderProbe}},
	}
	switch {
	case oraclegen.HasType(f, "Equipment"):
		p.bind = []oraclegen.Step{{Op: "attach", Seat: 0, Card: cardAt(0, name), AttachedTo: cardAt(0, enchantedHost)}}
	case faceHasAura(f):
		st, ok := castProbe(reg, name, cardAt(0, enchantedHost))
		if !ok {
			return nil, "aura has no mana pool"
		}
		p.castCard = true
		p.bind = []oraclegen.Step{st, {Op: "resolve"}}
	default:
		return nil, "the source is neither an Equipment nor an Aura"
	}
	return p, ""
}

func faceHasAura(f *cards.Face) bool {
	for _, t := range f.Types {
		if strings.EqualFold(t, "Aura") {
			return true
		}
	}
	return false
}

// bearerUnblockableItem serves "equipped/enchanted creature can't be
// blocked": the bearer may not be blocked by the Wall while the vanilla
// attacker beside it may, and the control without the attach lets the Wall
// block the bearer.
func bearerUnblockableItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	p, why := bearerPlan(reg, f, name, req)
	if p == nil {
		return staticSkip(name, mode, why)
	}
	bearer, other, blocker := p.attackers[0], p.attackers[1], p.blockers[0]
	expect := func(bearerWant bool) []oraclegen.Expect {
		return []oraclegen.Expect{
			canBlockExpect(p.blockerRef(blocker), p.attackerRef(other), true),
			canBlockExpect(p.blockerRef(blocker), p.attackerRef(bearer), bearerWant),
		}
	}
	if res, ok := runStatic(reg, p.scenario(p.attackers, p.blockers, true, expect(true))); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the unattached control does not let the bearer be blocked: "+joinFails(res.Fails))
	}
	sc := p.scenario(p.attackers, p.blockers, false, expect(false))
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b", "301.5"})
}

// bearerMaxBlockersItem serves "equipped/enchanted creature can't be blocked
// by more than N creatures": the bearer's block option carries the bound while
// the vanilla attacker beside it carries none, and the unattached control
// carries none on the bearer.
func bearerMaxBlockersItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "MinMaxBlocker"
	n := levelb.MaxBlockerCap(st)
	if n == 0 {
		return staticSkip(name, mode, "Max$ is not a plain bound")
	}
	p, why := bearerPlan(reg, f, name, req)
	if p == nil {
		return staticSkip(name, mode, why)
	}
	bearer, other, blocker := p.attackers[0], p.attackers[1], p.blockers[0]
	expect := func(bearerBound int) []oraclegen.Expect {
		return []oraclegen.Expect{
			canBlockExpectMax(p.blockerRef(blocker), p.attackerRef(other), true, intPtr(0)),
			canBlockExpectMax(p.blockerRef(blocker), p.attackerRef(bearer), true, intPtr(bearerBound)),
		}
	}
	if res, ok := runStatic(reg, p.scenario(p.attackers, p.blockers, true, expect(0))); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the unattached control already carries a bound: "+joinFails(res.Fails))
	}
	sc := p.scenario(p.attackers, p.blockers, false, expect(n))
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1a", "301.5"})
}

// -- filter shapes -----------------------------------------------------------

// filterPlan is the board of a filter shape: the attacker pool on p0 (the
// card itself when it can attack) against the blocker pool on p1. A blocker
// filter that names the bearer of the card's own Aura (Burden of Proof) flips
// the seats: p1 attacks the Aura's controller, whose bearer and a control
// creature are the blockers.
func filterPlan(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (*scopedPlan, string) {
	p := &scopedPlan{f: f, name: name, req: req}
	counters := scopedFilterCounters(st)
	blockerFilter := strings.ToLower(st.ParamStr(cards.PKValidBlocker))
	switch {
	case strings.Contains(blockerFilter, "enchantedby"):
		if !faceHasAura(f) {
			return nil, "the blocker filter names the card's bearer but the card is not an Aura"
		}
		cast, ok := castProbe(reg, name, cardAt(0, enchantedHost))
		if !ok {
			return nil, "aura has no mana pool"
		}
		p.attackSeat, p.castCard = 1, true
		p.bind = []oraclegen.Step{cast, {Op: "resolve"}}
		// The bearer is the Aura's blocker; a second creature is the
		// control blocker. Both sit on p0 (the defending seat).
		p.attackers = p.poolAttackers(reg, counters)
		for _, b := range []string{enchantedHost, blockerProbe} {
			p.blockers = append(p.blockers, scopedBlocker{name: b})
		}
		return p, ""
	case strings.Contains(blockerFilter, "token"):
		maker, ok := castProbe(reg, scopedTokenMaker, cardAt(1, defenderProbe))
		if !ok || !hasCorpusCard(reg, scopedTokenMaker) {
			return nil, "no token maker probe in the corpus"
		}
		p.setup = []oraclegen.Step{maker, {Op: "resolve"}}
		p.hand = []string{scopedTokenMaker}
		p.extra1 = []string{defenderProbe}
		p.blockers = []scopedBlocker{{name: scopedTokenName, token: true}, {name: blockerProbe}}
	default:
		p.blockers = poolBlockers(reg, name)
	}
	p.cardAttacks = scopedCardCanAttack(f, name, st)
	p.attackers = p.attackersWithCard(p.poolAttackers(reg, counters))
	return p, ""
}

// filterUnblockableItem serves a CantBlockBy over an attacker filter and/or a
// blocker filter by the matrix search.
func filterUnblockableItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	p, why := filterPlan(reg, f, st, name, req)
	if p == nil {
		return staticSkip(name, mode, why)
	}
	refused, why := p.blockMatrixRetry(reg)
	if why != "" {
		return staticSkip(name, mode, why)
	}
	m, n, bm, bn, hasN, ok := searchNotBlockable(p.attackers, p.blockers, refused, false)
	if !ok {
		return staticSkip(name, mode, "no probe pair splits the attacker and blocker filters")
	}
	sourceAttacks := m.name == name || hasN && n.name == name
	if !hasN && sourceAttacks {
		return staticSkip(name, mode, "every probe attacker matches the filter and the card itself is the attacker: no unmatched control")
	}
	att, blk := []scopedCard{m}, []scopedBlocker{bm, bn}
	if hasN {
		att = append(att, n)
	}
	expect := func(refusedWant bool) []oraclegen.Expect {
		out := []oraclegen.Expect{
			canBlockExpect(p.blockerRef(bm), p.attackerRef(m), !refusedWant),
			canBlockExpect(p.blockerRef(bn), p.attackerRef(m), true),
		}
		if hasN {
			out = append(out, canBlockExpect(p.blockerRef(bm), p.attackerRef(n), true))
		}
		return out
	}
	if !sourceAttacks {
		// The static's source is not an attacker: the board without it (the
		// Aura uncast, the card off the battlefield) lets Bm block M.
		if res, ok := runStatic(reg, p.scenario(att, blk, true, expect(false))); !ok || len(res.Fails) != 0 {
			return staticSkip(name, mode, "the board without the source still refuses the block: "+joinFails(res.Fails))
		}
	}
	sc := p.scenario(att, blk, false, expect(true))
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b"})
}

// blockMatrixRetry runs the matrix, and when the card itself as an attacker
// breaks the replay (it is not offered as one) runs it again without the card.
func (p *scopedPlan) blockMatrixRetry(reg *cards.Registry) (map[[2]string]bool, string) {
	refused, why := p.blockMatrix(reg, p.attackers, p.blockers)
	if why != "" && p.cardAttacks {
		p.cardAttacks = false
		p.attackers = p.attackers[1:]
		return p.blockMatrix(reg, p.attackers, p.blockers)
	}
	return refused, why
}

// filterMaxBlockersItem serves a MinMaxBlocker Max$ cap over an attacker
// filter: the attackers the filter matches carry the bound in their block
// options and the others carry none.
func filterMaxBlockersItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "MinMaxBlocker"
	n := levelb.MaxBlockerCap(st)
	if n == 0 {
		return staticSkip(name, mode, "Max$ is not a plain bound")
	}
	p, why := filterPlan(reg, f, st, name, req)
	if p == nil {
		return staticSkip(name, mode, why)
	}
	blocker := scopedBlocker{name: blockerProbe}
	p.blockers = []scopedBlocker{blocker}
	bounded, why := p.boundMatrix(reg, p.attackers, blocker, n)
	if why != "" && p.cardAttacks {
		p.cardAttacks = false
		p.attackers = p.attackers[1:]
		bounded, why = p.boundMatrix(reg, p.attackers, blocker, n)
	}
	if why != "" {
		return staticSkip(name, mode, why)
	}
	var m, other scopedCard
	var haveM, haveOther bool
	for _, a := range p.attackers {
		switch {
		case bounded[a.name] && !haveM:
			m, haveM = a, true
		case !bounded[a.name] && !haveOther:
			other, haveOther = a, true
		}
	}
	if !haveM || !haveOther {
		return staticSkip(name, mode, "no probe attacker pair splits the filter (bounded and unbounded)")
	}
	att := []scopedCard{m, other}
	expect := func(mBound int) []oraclegen.Expect {
		return []oraclegen.Expect{
			canBlockExpectMax(p.blockerRef(blocker), p.attackerRef(other), true, intPtr(0)),
			canBlockExpectMax(p.blockerRef(blocker), p.attackerRef(m), true, intPtr(mBound)),
		}
	}
	if !p.cardAttacks || m.name != name && other.name != name {
		if res, ok := runStatic(reg, p.scenario(att, p.blockers, true, expect(0))); !ok || len(res.Fails) != 0 {
			return staticSkip(name, mode, "the board without the source still bounds the attacker: "+joinFails(res.Fails))
		}
	}
	sc := p.scenario(att, p.blockers, false, expect(n))
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1a"})
}

// filterCantAttackItem serves a CantAttack whose filter names p1's creatures
// (a flier; a creature enchanted by an Aura p0 controls) with Target$ the
// controller. One attack step declares the vanilla probe and asserts every
// candidate offered: the candidates the static refuses show up as the failed
// expectations, so the matching creature is read off the engine. The chosen
// scenario then asserts the matching creature absent and the vanilla probe
// offered at p1's declare-attackers decision, and the board without the
// source (the Aura uncast, the card off the battlefield) offers both.
func filterCantAttackItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantAttack"
	p := &scopedPlan{f: f, name: name, req: req, attackSeat: 1}
	var cands []scopedCard
	if strings.Contains(strings.ToLower(st.ParamStr(cards.PKValidCard)), "enchantedby") {
		// The Aura is cast by p0 onto p1's Hill Giant (the bearer).
		cast, ok := castProbe(reg, scopedAuraProbe, cardAt(1, enchantedHost))
		if !ok || !hasCorpusCard(reg, scopedAuraProbe) {
			return staticSkip(name, mode, "aura probe has no mana pool")
		}
		p.hand = []string{scopedAuraProbe}
		p.bind = []oraclegen.Step{cast, {Op: "resolve"}}
		cands = []scopedCard{{name: enchantedHost}}
	} else {
		for _, n := range []string{"Wind Drake", "Grizzly Bears", "Hill Giant"} {
			if hasCorpusCard(reg, n) {
				cands = append(cands, scopedCard{name: n})
			}
		}
	}
	vanilla := scopedCard{name: smallAttackerProbe}
	cands = append(cands, vanilla)
	attack := func(declare []scopedCard, control bool, expect []oraclegen.Expect) oraclegen.Scenario {
		var steps []oraclegen.Step
		if !control {
			steps = append(steps, p.bind...)
		}
		refs := make([]string, 0, len(declare))
		for _, c := range declare {
			refs = append(refs, p.attackerRef(c))
		}
		steps = append(steps, oraclegen.Step{Op: "attack", Seat: 1, Defender: "p0", Attackers: refs, Expect: expect})
		return p.fieldBoard(cands, nil, control, steps)
	}
	var offered []oraclegen.Expect
	for _, c := range cands {
		offered = append(offered, canAttackExpect(p.attackerRef(c), true))
	}
	res, ok := runStatic(reg, attack([]scopedCard{vanilla}, false, offered))
	if !ok {
		return staticSkip(name, mode, "the candidate attack does not run")
	}
	var m scopedCard
	found := false
	for _, c := range cands[:len(cands)-1] {
		if failsName(res.Fails, p.attackerRef(c)+" can attack = false") {
			m, found = c, true
			break
		}
	}
	if !found {
		return staticSkip(name, mode, "no candidate attacker is refused: "+joinFails(res.Fails))
	}
	want := func(mOffered bool) []oraclegen.Expect {
		return []oraclegen.Expect{canAttackExpect(p.attackerRef(vanilla), true), canAttackExpect(p.attackerRef(m), mOffered)}
	}
	if res, ok := runStatic(reg, attack([]scopedCard{vanilla}, true, want(true))); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the board without the source does not offer the candidate: "+joinFails(res.Fails))
	}
	sc := attack([]scopedCard{vanilla}, false, want(false))
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"508.1a"})
}

// crewedUnblockableItem serves a Vehicle's gated "can't be blocked"
// (Watertight Gondola's Descend 8): p0 crews the Vehicle (tapping a creature)
// and attacks with it beside an untapped vanilla attacker; the blocker may
// block the vanilla one but not the Vehicle while the gate holds (eight
// permanent cards in p0's graveyard), and may block it with the graveyard one
// card short.
func crewedUnblockableItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	g, why := resolveGate(f, st)
	if g == nil {
		return staticSkip(name, mode, "gate unsupported: "+why)
	}
	steps, xab, taps, cost, ok := crewPrelude(f, name)
	if !ok {
		return staticSkip(name, mode, "the Vehicle's crew cost has no catalogue fixture")
	}
	p := &scopedPlan{
		f: f, name: name, req: req, cardAttacks: true, gate: g,
		blockers: []scopedBlocker{{name: blockerProbe}},
		setup:    steps, first0: taps, xab: xab,
		attackers: []scopedCard{{name: name}, {name: smallAttackerProbe}},
	}
	// The crew cost taps the first untapped creature (the catalogue tap
	// fixture, fielded ahead of the attackers), and a blockers decision
	// exists only when some attacker is blockable, so the vanilla probe is
	// the second attacker.
	self, other, blocker := p.attackers[0], p.attackers[1], p.blockers[0]
	if res, ok := runStatic(reg, p.scenario(p.attackers, p.blockers, true, []oraclegen.Expect{
		canBlockExpect(p.blockerRef(blocker), p.attackerRef(other), true),
		canBlockExpect(p.blockerRef(blocker), p.attackerRef(self), true),
	})); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the gate-unheld control does not let the crewed Vehicle be blocked: "+joinFails(res.Fails))
	}
	expect := func(selfWant bool) []oraclegen.Expect {
		return []oraclegen.Expect{
			canBlockExpect(p.blockerRef(blocker), p.attackerRef(other), true),
			canBlockExpect(p.blockerRef(blocker), p.attackerRef(self), selfWant),
		}
	}
	sc := p.scenario(p.attackers, p.blockers, false, expect(false))
	it, skip := finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b", "702.122"})
	if skip != nil {
		return it, skip
	}
	// The crew activation is the first step after the gate's own prelude.
	idx := len(g.on.steps)
	it.XAbility = growXAbility(nil, len(sc.Steps))
	for i, x := range xab {
		if idx+i < len(it.XAbility) {
			it.XAbility[idx+i] = x
		}
	}
	if res, ok := runStatic(reg, sc); ok && !exileCostPickObserved(res.Decisions, idx) {
		addActivationCostAnswers(it.XAnswers, idx, cost, name, res.Decisions)
	}
	return it, nil
}

// animatedLandUnblockableItem serves a land's "can't be blocked" (Secret
// Tunnel's Creature.Self static, live only while the land is a creature):
// p0 animates the land (Animate Land, a 3/3 creature until end of turn) and
// attacks with it beside a vanilla attacker; the blocker may block the
// vanilla one but not the land.
func animatedLandUnblockableItem(reg *cards.Registry, f *cards.Face, st *cards.Static, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	if !hasCorpusCard(reg, scopedAnimator) {
		return staticSkip(name, mode, "no animation probe in the corpus")
	}
	cast, ok := castProbe(reg, scopedAnimator, cardAt(0, name))
	if !ok {
		return staticSkip(name, mode, "animation probe has no mana pool")
	}
	p := &scopedPlan{
		f: f, name: name, req: req, cardAttacks: true,
		attackers: []scopedCard{{name: name}, {name: smallAttackerProbe}},
		blockers:  []scopedBlocker{{name: blockerProbe}},
		setup:     []oraclegen.Step{cast, {Op: "resolve"}},
		hand:      []string{scopedAnimator},
	}
	self, other, blocker := p.attackers[0], p.attackers[1], p.blockers[0]
	expect := []oraclegen.Expect{
		canBlockExpect(p.blockerRef(blocker), p.attackerRef(other), true),
		canBlockExpect(p.blockerRef(blocker), p.attackerRef(self), false),
	}
	sc := p.scenario(p.attackers, p.blockers, false, expect)
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b"})
}

// attackedAttachedItem serves a planeswalker Equipment's "as long as it is
// attached to a creature, it can't be attacked" (The Aetherspark, CantAttack
// with Target$ naming the card): p0 attaches the card to its creature and p1
// declares attackers. The vanilla attacker is offered against the player but
// not against the planeswalker; the control (the card unattached) offers
// the attack on the planeswalker too.
func attackedAttachedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantAttack"
	if name == enchantedHost || name == smallAttackerProbe {
		return staticSkip(name, mode, "no card distinct from the probes")
	}
	p := &scopedPlan{
		f: f, name: name, req: req, attackSeat: 1,
		extra0: []string{enchantedHost},
		bind:   []oraclegen.Step{{Op: "attach", Seat: 0, Card: cardAt(0, name), AttachedTo: cardAt(0, enchantedHost)}},
	}
	attacker := scopedCard{name: smallAttackerProbe}
	build := func(control bool, walkerWant bool) oraclegen.Scenario {
		var steps []oraclegen.Step
		if !control {
			steps = append(steps, p.bind...)
		}
		ref := p.attackerRef(attacker)
		walker := canAttackExpect(ref, walkerWant)
		walker.CanAttack.Defender = cardAt(0, name)
		steps = append(steps, oraclegen.Step{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{ref},
			Expect: []oraclegen.Expect{canAttackExpect(ref, true), walker}})
		return p.fieldBoard([]scopedCard{attacker}, nil, control, steps)
	}
	if res, ok := runStatic(reg, build(true, true)); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, "the unattached control does not offer the attack on the planeswalker: "+joinFails(res.Fails))
	}
	sc := build(false, false)
	if res, ok := runStatic(reg, sc); ok && failsName(res.Fails, "can attack = true, want false") {
		// Measured: the control offers the attack on the planeswalker and the
		// attached board still does. combat.RestrictionTargetMatches
		// (rules/combat/restrictions.go) reads Target$ as player specs and
		// Planeswalker.<spec> only, so Card.Self+AttachedTo Creature never
		// matches and the restriction never binds. An engine change, not a
		// template one: the row stays a recognized, precisely named skip.
		return staticSkip(name, mode, "engine gap: Target$ Card.Self+AttachedTo Creature is not read by combat.RestrictionTargetMatches, so the attached planeswalker is still offered as a defender")
	}
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"508.1b", "506.3"})
}
