package templates

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// Legality statics on a host other than the card itself (spec section 7):
// an Aura's "enchanted creature can't attack / can't block" (Pacifism), and
// a creature's "can't be blocked by <filter>" (Gate Colossus's power-2-or-
// less, Stromkirk Noble's Humans). Like the self shapes in
// combat_legality.go they stop at the declare decision and assert, through
// Step.Expect, what the decision offers, beside a control that is offered.

// enchantedHost is the creature the Aura under test enchants.
const enchantedHost = "Hill Giant"

// auraOnOwnCreature casts the Aura under test from p0's hand onto p0's
// enchantedHost and resolves it.
func auraOnOwnCreature(reg *cards.Registry, name string) ([]oraclegen.Step, bool) {
	st, ok := castProbe(reg, name, cardAt(0, enchantedHost))
	if !ok {
		return nil, false
	}
	return []oraclegen.Step{st, {Op: "resolve"}}, true
}

// cantAttackEnchantedItem serves an Aura's "enchanted creature can't
// attack": once the Aura resolves onto p0's creature, the declare-attackers
// decision does not offer it while the vanilla probe beside it is offered.
// The control (no Aura) offers both.
func cantAttackEnchantedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantAttack"
	pre, ok := auraOnOwnCreature(reg, name)
	if !ok {
		return staticSkip(name, mode, "aura has no mana pool")
	}
	host, probe := cardAt(0, enchantedHost), cardAt(0, blockerProbe)
	declare := func(hostWant bool) oraclegen.Step {
		return oraclegen.Step{Op: "pass_to", Seat: 0, Step: "declare-attackers", Decision: "attackers",
			Expect: []oraclegen.Expect{canAttackExpect(probe, true), canAttackExpect(host, hostWant)}}
	}
	bf := []string{enchantedHost, blockerProbe}
	control := staticScenario(f, name, bf, []string{name}, []oraclegen.Step{declare(true)})
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, fmt.Sprintf("control without the Aura does not offer the host: %v", res.Fails))
	}
	sc := staticScenario(f, name, bf, []string{name}, append(pre, declare(false)))
	return finishHostLegalityItem(reg, f, name, mode, req, sc, []string{"508.1a", "303.4"})
}

// cantBlockEnchantedItem serves an Aura's "enchanted creature can't block":
// the Aura resolves onto p0's creature, p1 attacks, and p0's declare-blockers
// decision lets the vanilla probe block but not the enchanted creature. The
// control (no Aura) lets both block.
func cantBlockEnchantedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlock"
	pre, ok := auraOnOwnCreature(reg, name)
	if !ok {
		return staticSkip(name, mode, "aura has no mana pool")
	}
	attacker := cardAt(1, smallAttackerProbe)
	host, probe := cardAt(0, enchantedHost), cardAt(0, blockerProbe)
	scenario := func(pre []oraclegen.Step, hostWant bool) oraclegen.Scenario {
		steps := append(append([]oraclegen.Step(nil), pre...),
			oraclegen.Step{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{attacker}},
			oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: []oraclegen.Expect{
				canBlockExpect(probe, attacker, true), canBlockExpect(host, attacker, hostWant)}})
		sc := staticScenario(f, name, []string{enchantedHost, blockerProbe}, []string{name}, steps)
		p1 := sc.Setup["p1"]
		p1.Battlefield = []string{smallAttackerProbe}
		sc.Setup["p1"] = p1
		return sc
	}
	if res, ok := runStatic(reg, scenario(nil, true)); !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, fmt.Sprintf("control without the Aura does not let the host block: %v", res.Fails))
	}
	return finishHostLegalityItem(reg, f, name, mode, req, scenario(pre, false), []string{"509.1a", "303.4"})
}

// finishHostLegalityItem is finishLegalityItem for an Aura: the card under
// test must be on the battlefield attached, which onBattlefield checks the
// same way.
func finishHostLegalityItem(reg *cards.Registry, f *cards.Face, name, mode string, req levelb.Requirement, sc oraclegen.Scenario, cr []string) (oraclegen.Item, *oraclegen.Skip) {
	return finishLegalityItem(reg, f, name, mode, req, sc, cr)
}

// filterBlockerProbes are vanilla creatures both engines know, in the order
// the blocker-filter template tries them as the matching and the
// non-matching blocker: a Human, a power-2 Bear, a 3/3, a 1/1 Elf, a Wall.
var filterBlockerProbes = []string{"Elite Vanguard", "Grizzly Bears", "Hill Giant", "Llanowar Elves", defenderProbe}

// filterBlockerSplitProbes are the non-vanilla creature probes the blocker
// filter search tries AFTER every vanilla pair, so a filter the vanilla
// probes cannot split (Cynical Loner's Creature.Glimmer, a type the vanilla
// set does not carry) still finds a matching/non-matching pair and every
// already-served row keeps its first-passing vanilla pair, and so its
// scenario bytes. The probe is a real DSK Glimmer card; the expectations, not
// the probe's own abilities, are what the row asserts.
var filterBlockerSplitProbes = []string{"Enduring Innocence"}

// cantBlockByAliveLands is the number of Wastes p0's battlefield needs when
// the attacking card's own P/T is a characteristic-defining count of lands
// you control (Sandman, Shifting Scoundrel): without a land the card reads
// 0/0 and dies to state-based actions before the attack step, so no blocker
// pair is ever reached. Wastes produce no mana and carry no ability, so the
// fixture contributes nothing else the observation could read. 0 when the
// face's P/T is printed or the count does not read lands.
func cantBlockByAliveLands(f *cards.Face) int {
	if !strings.Contains(f.PT, "*") {
		return 0
	}
	for _, st := range f.Statics {
		for _, key := range []cards.ParamKey{cards.PKSetPower, cards.PKSetToughness} {
			name := strings.TrimSpace(st.ParamStr(key))
			if name == "" {
				continue
			}
			if body, ok := f.SVars[name]; ok {
				low := strings.ToLower(body)
				if strings.Contains(low, "count$") && strings.Contains(low, "land") {
					return 2
				}
			}
		}
	}
	return 0
}

// cantBlockByBlockerFilterItem serves "CARDNAME can't be blocked by
// <filter>" (CantBlockBy, the card as the attacker, ValidBlocker$ a creature
// filter). p0 attacks with the card and the large probe; p1 fields a
// blocker the filter matches and one it does not. The matching blocker may
// block the probe but not the card; the other may block the card. Gorge
// picks the first probe pair for which all three hold, so a filter it reads
// differently yields no item rather than a vacuous one.
func cantBlockByBlockerFilterItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBlockBy"
	if !f.IsCreature() || name == largeAttackerProbe {
		return staticSkip(name, mode, "no creature card distinct from the probes")
	}
	if hasEvasion(f) {
		return staticSkip(name, mode, "another evasion would make the card unblockable without the static")
	}
	for i := range f.Statics {
		if strings.EqualFold(f.Statics[i].Mode, "MustAttack") {
			// XMage declares a must-attack creature itself; the scripted
			// attack is then a leftover action (measured: Juggernaut).
			return staticSkip(name, mode, "the card must attack each combat")
		}
	}
	if _, err := strconv.Atoi(req.Slot); err != nil {
		return staticSkip(name, mode, "requirement slot names no static")
	}
	self, spider := cardAt(0, name), cardAt(0, largeAttackerProbe)
	alive := cantBlockByAliveLands(f)
	probes := make([]string, 0, len(filterBlockerProbes)+len(filterBlockerSplitProbes))
	probes = append(probes, filterBlockerProbes...)
	probes = append(probes, filterBlockerSplitProbes...)
	for _, m := range probes {
		for _, n := range probes {
			if m == n || m == name || n == name {
				continue
			}
			mRef, nRef := cardAt(1, m), cardAt(1, n)
			steps := []oraclegen.Step{
				{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{self, spider}},
				{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: []oraclegen.Expect{
					canBlockExpect(mRef, spider, true),
					canBlockExpect(mRef, self, false),
					canBlockExpect(nRef, self, true),
				}},
			}
			sc := withBackFace(staticScenario(f, name, []string{name, largeAttackerProbe}, nil, steps), name, req)
			p1 := sc.Setup["p1"]
			p1.Battlefield = []string{m, n}
			sc.Setup["p1"] = p1
			if alive > 0 {
				// The card's characteristic-defining P/T counts lands you
				// control; the Wastes keep it alive to attack (measured:
				// Sandman at 0/0 dies before the attack step).
				p0 := sc.Setup["p0"]
				for i := 0; i < alive; i++ {
					p0.Battlefield = append(p0.Battlefield, "Wastes")
				}
				sc.Setup["p0"] = p0
			}
			if res, ok := runStatic(reg, sc); !ok || len(res.Fails) != 0 {
				continue
			}
			return finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b"})
		}
	}
	return staticSkip(name, mode, "no probe pair splits the blocker filter")
}

// lifeGainProbe is an instant whose only effect is "You gain 5 life."
const lifeGainProbe = "Whitesun's Passage"

// cantGainLifeItem serves an unconditional "players can't gain life" (or
// "you can't gain life"): p0 casts the life-gain probe with the card on the
// battlefield and its life total stays put, a compared snapshot field. The
// control without the card gains the 5 life, so the item is never vacuous.
func cantGainLifeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantGainLife"
	cast, ok := castProbe(reg, lifeGainProbe)
	if !ok {
		return staticSkip(name, mode, "life-gain probe has no mana pool")
	}
	steps := []oraclegen.Step{cast, {Op: "resolve"}}
	life := func(res rules.OracleResult) int32 {
		if len(res.Snapshots) == 0 || len(res.Snapshots[len(res.Snapshots)-1].Players) == 0 {
			return -1
		}
		return res.Snapshots[len(res.Snapshots)-1].Players[0].Life
	}
	control := staticScenario(f, name, nil, []string{lifeGainProbe}, steps)
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 || life(cres) != 25 {
		return staticSkip(name, mode, fmt.Sprintf("control does not gain the probe's life (life %d, %v)", life(cres), cres.Fails))
	}
	sc := withBackFace(staticScenario(f, name, []string{name}, []string{lifeGainProbe}, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || life(res) != 20 {
		return staticSkip(name, mode, fmt.Sprintf("life still changes with the card on the battlefield (life %d, %v)", life(res), res.Fails))
	}
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"119.7"})
}

// manaConvertProbe is a creature spell with a green pip; offColourMana pays
// its cost only "as though it were mana of any type".
const (
	manaConvertProbe = "Grizzly Bears"
	offColourMana    = "RR"
)

// manaConvertCreatureItem serves "You may spend mana as though it were mana
// of any type to cast creature spells" (ManaConvert on Creature.YouCtrl
// spells): with the card on the battlefield p0 casts the probe creature from
// red mana and it resolves; the control without the card cannot cast it.
func manaConvertCreatureItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "ManaConvert"
	if name == manaConvertProbe {
		return staticSkip(name, mode, "card is the probe")
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: cardAt(0, manaConvertProbe), Mana: offColourMana},
		{Op: "resolve"},
	}
	control := staticScenario(f, name, nil, []string{manaConvertProbe}, steps)
	if res, ok := runStatic(reg, control); ok && len(res.Fails) == 0 && onBattlefield(res, cardAt(0, manaConvertProbe)) {
		return staticSkip(name, mode, "control casts the probe from off-colour mana without the card")
	}
	sc := withBackFace(staticScenario(f, name, []string{name}, []string{manaConvertProbe}, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || !onBattlefield(res, cardAt(0, manaConvertProbe)) {
		return staticSkip(name, mode, fmt.Sprintf("the probe is not cast from off-colour mana with the card (%v)", res.Fails))
	}
	return finishLegalityItem(reg, f, name, mode, req, sc, []string{"609.4b"})
}

// lookAtLibraryTopItem serves a "you may look at the top card of your
// library any time" grant (Continuous MayLookAt$): hidden information no
// snapshot carries, so the item asserts gorge's own permission read at
// p0's begin-combat checkpoint (the runner's look_at_library_top
// expectation, frozen into `fails` like can_block), beside a control without
// the card where the permission is absent. ok is false when the control or
// the observation does not hold.
func lookAtLibraryTopItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, bool) {
	step := func(want bool) []oraclegen.Step {
		return []oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "begin-combat",
			Expect: []oraclegen.Expect{{LookAtLibraryTop: map[string]bool{"p0": want}}}}}
	}
	control := staticScenario(f, name, nil, nil, step(false))
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := withBackFace(staticScenario(f, name, []string{name}, nil, step(true)), name, req)
	it, skip := finishLegalityItem(reg, f, name, "MayLookAt", req, sc, []string{"401.2"})
	return it, skip == nil
}

// cantBeActivatedNamedItem serves Sorcerous Spyglass's chosen-name lock
// (CantBeActivated ValidCard$ Card.NamedCard, the name chosen as it enters):
// p0 casts the card and names the activated probe, whose ability is offered
// at the cast's checkpoint (the control) and absent once the card resolves.
// The probe's name sits on top of p0's library, which is the name gorge's
// legacy no-universe NameCard picks; XMage is scripted the same name.
func cantBeActivatedNamedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "CantBeActivated"
	if req.Face > 0 {
		return staticSkip(name, mode, "named-card observation casts the front face")
	}
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return staticSkip(name, mode, "cost unpayable: "+why)
	}
	probe := cardAt(0, activatedProbe)
	offered := func(want bool) []oraclegen.Expect {
		return []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "activate", Card: probe}, Want: boolPtr(want)}}
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: mana, Expect: offered(true)},
		{Op: "resolve"},
		{Op: "pass_to", Seat: 0, Decision: "priority", Expect: offered(false)},
	}
	sc := staticScenario(f, name, []string{activatedProbe}, []string{name}, steps)
	p0 := sc.Setup["p0"]
	p0.LibraryTop = []string{activatedProbe}
	sc.Setup["p0"] = p0
	it, skip := serveOffered(reg, f, name, mode, req, "602.5", sc, func() string { return "" })
	if skip != nil {
		return it, skip
	}
	// gorge's runner has no name universe, so its NameCard offers one name
	// (the caster's library top) and the answer generator drops it as a
	// forced one-option ask; XMage offers every card name, so the chosen
	// name is scripted at the resolve step that poses it.
	res, _ := runStatic(reg, sc)
	for _, d := range res.Decisions {
		if d.Step >= 0 && d.Step < len(it.XAnswers) && len(d.PickKinds) == 1 && d.PickKinds[0] == "name" && len(d.Picks) == 1 {
			it.XAnswers[d.Step] = append(it.XAnswers[d.Step], oraclegen.XAnswer{Seat: d.Seat, Kind: "choice", Value: d.Picks[0]})
		}
	}
	return it, nil
}
