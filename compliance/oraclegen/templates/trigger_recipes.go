package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// triggerCause is one candidate way of making a trigger's cause happen on
// turn 1 with ops the XMage driver already has (cast, attack, pass_to).
type triggerCause struct {
	hand        []string                  // probe cards added to p0's hand
	battlefield []string                  // extra p0 permanents (an attacker for a non-creature card)
	tapped      []string                  // extra p0 permanents that start tapped
	graveyard   []string                  // extra p0 graveyard cards
	counters    map[string]map[string]int // counters on setup permanents
	prelude     []oraclegen.Step          // steps before the actual trigger cause
	steps       []oraclegen.Step          // the cause steps emitted into the item
	probeSteps  []oraclegen.Step          // steps whose snapshots show the trigger on the stack; nil means steps
	selfInHand  bool                      // the card starts in p0's hand (a "when you discard this card" trigger), not on the battlefield
	xability    []string                  // XMage rule-text prefix per step (an activate step); nil when no step activates
	// preludeXAbility is parallel to prelude: the XMage rule-text prefix of a
	// prelude activate step (a Class level-up), "" on every other prelude
	// step. A ClassBand$ trigger prepends the level-up prelude, so its
	// activate steps need selectors exactly as the cause's do.
	preludeXAbility []string // XMage rule-text prefix per prelude step; nil when no prelude activates
	castSelfX       bool     // the card is cast from hand first (an X creature that setup would leave 0/0, or a p0 upkeep/draw trigger whose fixture turn 1 would otherwise spend)
	opponentHand    []string // probes held by p1 for opponent-cast causes
	// opponentBattlefield are p1 permanents (a blocker, an attacker, a tap
	// target, an opponent-comparison gate) the cause needs on the other side
	// of the table.
	opponentBattlefield []string
	activateCost        string // Forge cost of the activate step in steps (Crew/Saddle tap choice); "" when none
}

// Probe cards, each named with why. Spec hypothesis H4: the probe exists in
// XMage's database; the host replay confirms it. Each list is tried in order
// and the first that plays through gorge and fires the trigger wins.
var (
	// Murder ({1}{B}{B}, destroy target creature) is unconditional; Doom
	// Blade and Terror are the fallbacks and refuse black / artifact cards.
	destroyProbes = []string{"Murder", "Doom Blade", "Terror"}
	// Angel's Mercy ({2}{W}{W}, gain 7 life) and Revitalize ({1}{W}, gain 3
	// and draw) gain life with no choice to answer; Healing Salve asks a mode.
	lifegainProbes = []string{"Angel's Mercy", "Revitalize", "Healing Salve"}
	// Divination ({2}{U}, draw two) is the plain draw; Concentrate and
	// Inspiration are the fallbacks.
	drawProbes = []string{"Divination", "Concentrate", "Inspiration"}
	// Shock (instant, {R}, 2 damage to any target) is the instant/sorcery
	// cast cause; Grizzly Bears ({1}{G}) the creature one; and Giant Growth
	// ({G}) the self-controlled becomes-target cause. All three are level-A fixtures.
	shockProbe, bearsProbe, growthProbe = "Shock", "Grizzly Bears", "Giant Growth"
)

// castProbe builds the cast step of probe, or false when the probe is not in
// the corpus or its mana cost has no pool.
func castProbe(reg *cards.Registry, probe string, targets ...string) (oraclegen.Step, bool) {
	c, ok := reg.Lookup(probe)
	if !ok || len(c.Faces) == 0 {
		return oraclegen.Step{}, false
	}
	pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
	if why != "" {
		return oraclegen.Step{}, false
	}
	return oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + probe, Mana: pool, Targets: targets}, true
}

// baseTriggerRecipe returns the candidate causes for one trigger sub-family, or
// the reason none exists for this card.
func baseTriggerRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) ([]triggerCause, string) {
	var out []triggerCause
	cast := func(probe string, targets ...string) {
		if c, ok := castCause(reg, name, probe, targets...); ok {
			out = append(out, c)
		}
	}
	creature := f.IsCreature()
	switch sub {
	case "trigger.etb-land":
		// A land's own ETB fires when the land is played from hand, never
		// cast, so the cause is the play itself (the runner's `play` op
		// submits the play_land offered for the card). The card starts in
		// hand, not on the battlefield, because setup placement would move
		// it to the battlefield without playing it and never trigger.
		return []triggerCause{{
			selfInHand: true,
			hand:       []string{name},
			steps:      []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + name}},
		}}, ""
	case "trigger.etb-other":
		return etbProbeCauses(reg, name, t)
	case "trigger.leaves-graveyard", "trigger.ltb-other", "trigger.zone-change-residue":
		return zoneTriggerRecipe(reg, name, t, sub)
	case ltbSelfSub:
		return ltbSelfRecipe(reg, f, name, t)
	case "trigger.dies":
		if !creature {
			return nil, "dies needs a creature"
		}
		for _, p := range destroyProbes {
			cast(p, "p0:"+name)
		}
	case "trigger.attacks", "trigger.attacks-one-target", "trigger.combat-damage", "trigger.combat-damage-all":
		combatDamage := sub == "trigger.combat-damage" || sub == "trigger.combat-damage-all"
		attacker, extra := "p0:"+name, []string(nil)
		if !creature {
			if sub == "trigger.combat-damage" {
				return nil, "combat-damage needs a creature"
			}
			attacker, extra = "p0:"+bearsProbe, []string{bearsProbe}
		}
		attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{attacker}}
		c := triggerCause{battlefield: extra, steps: []oraclegen.Step{attack}}
		if !combatDamage {
			if prepared, ok := attackActivationCause(reg, f, name, t); ok {
				c = prepared
			}
		}
		if combatDamage {
			// The trigger resolves inside the pass to main2, so the emitted
			// item shows no stack; the probe stops in end-combat, where the
			// trigger is still on it.
			c.steps = append(c.steps, oraclegen.Step{Op: "pass_to", Step: "main2"})
			c.probeSteps = []oraclegen.Step{attack, {Op: "pass_to", Step: "end-combat"}}
		}
		out = append(out, c)
	case "trigger.spell-cast":
		return spellCastProbeCauses(reg, name, t), ""
	case "trigger.becomes-target":
		if !creature {
			return nil, "becomes-target needs a creature"
		}
		// Keep both controller shapes: YouCtrl target triggers need p0's own
		// spell, while ward and OppCtrl triggers need p1's spell.
		cast(growthProbe, "p0:"+name)
		if st, ok := castProbe(reg, shockProbe, "p0:"+name); ok {
			st.Seat, st.Card = 1, "p1:"+shockProbe
			out = append(out, triggerCause{opponentHand: []string{shockProbe}, steps: []oraclegen.Step{{Op: "pass", Seat: 0}, st}})
		}
	case "trigger.life-gained":
		for _, p := range lifegainProbes {
			cast(p)
		}
	case "trigger.drawn":
		for _, p := range drawProbes {
			if c, ok := castCause(reg, name, p); ok {
				out = append(out, withDrawCheckpoint(c))
			}
		}
	case "trigger.phase":
		step, ok := phaseStep(t.ParamStr(cards.PKPhase))
		if !ok {
			return nil, "unsupported phase"
		}
		active := ""
		if step == "upkeep" || step == "draw" || step == "main1" {
			// p0's turn-1 upkeep/draw/main1 have passed. For "each player"
			// and "each opponent" phase triggers, stop at p1's first matching
			// step on turn 2; for You, wait for p0's next turn (turn 3).
			if vp := t.ParamStr(cards.PKValidPlayer); strings.EqualFold(vp, "Player") || strings.EqualFold(vp, "Opponent") {
				active = "p1"
			} else {
				active = "p0"
			}
		}
		steps := []oraclegen.Step{{Op: "pass_to", Step: step, Active: active}}
		if step == "main1" {
			// The game starts in turn 1's main1, which pass_to would stop
			// in at once: leave it first, then wait for p0's next main1.
			steps = append([]oraclegen.Step{{Op: "pass_to", Step: "main2"}}, steps...)
		}
		base := triggerCause{steps: steps}
		out = append(out, base)
		for _, condition := range conditionPreludes(reg, t.Params, f.SVars) {
			out = append(out, applyPrelude(base, condition))
		}
		fixtures := triggerConditionFixtures(reg, f, t)
		for _, condition := range fixtures {
			out = append(out, applyPrelude(base, condition))
		}
		if active == "p0" && step != "main1" {
			// Setup passes turn 1's upkeep and draw with the fixture in
			// place, so a source that acts on it (transforms, makes a token)
			// has already done so by turn 3: cast the source on turn 1 instead.
			for _, condition := range fixtures {
				cast := applyPrelude(base, condition)
				cast.castSelfX = true
				out = append(out, cast)
			}
		}
	default:
		if causes, why, ok := eventTriggerRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := castFamilyRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := tapCombatRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := stateTriggerRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		return nil, "no recipe for " + sub
	}
	if len(out) == 0 {
		return nil, "probe not in corpus"
	}
	return out, ""
}

// classLevelBandCausePrepends raises every cause of a ClassBand$ trigger to
// the band's level before the cause runs. A granted trigger body is live only
// from level N on, so without the prelude a level-1 scenario never fires it.
// The prelude goes ahead of any condition prelude so the sorcery-speed
// level-up runs in turn 1's first main phase. A ClassLevelGained cause is
// excluded: its own recipe already carries the level-up that fires it.
func classLevelBandCausePrepends(f *cards.Face, name string, t *cards.Trigger, sub string, causes []triggerCause) []triggerCause {
	if sub == classLevelGainedSub {
		return causes
	}
	band := classBandLevel(t)
	if band < 2 {
		return causes
	}
	steps, xab, ok := classLevelPrelude(f, name, band)
	if !ok {
		return causes
	}
	for i := range causes {
		causes[i].prelude = append(append([]oraclegen.Step(nil), steps...), causes[i].prelude...)
		causes[i].preludeXAbility = append(append([]string(nil), xab...), causes[i].preludeXAbility...)
	}
	return causes
}

// attackActivationCause builds the activation-and-attack cause for saddled
// creatures, Vehicles and self-animating lands. The existing activate cost
// fixture machinery supplies Crew/Saddle's tapped creatures and XMage answers.
func attackActivationCause(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (triggerCause, bool) {
	needsSaddle := strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidCard)), "issaddled")
	abilityIndex := -1
	for i, sa := range f.Abilities {
		if !sa.IsActivated() {
			continue
		}
		keyword := strings.ToLower(sa.Params["Keyword"])
		defined := strings.EqualFold(sa.Params["Defined"], "Self")
		animatesCreature := strings.Contains(strings.ToLower(sa.Params["Types"]), "creature") && sa.Params["Power"] != ""
		if (needsSaddle && strings.HasPrefix(keyword, "saddle")) || (!needsSaddle && strings.HasPrefix(keyword, "crew")) || (!needsSaddle && defined && animatesCreature && f.IsLand()) {
			abilityIndex = i
			break
		}
	}
	if abilityIndex < 0 {
		return triggerCause{}, false
	}
	sa := f.Abilities[abilityIndex]
	cost := sa.ParamStr(cards.PKCost)
	mana, gap := activationCostIn(cost, "battlefield")
	if gap != "" {
		return triggerCause{}, false
	}
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return triggerCause{}, false
	}
	prefix, ok := prefixes[abilityIndex]
	if !ok {
		return triggerCause{}, false
	}
	idx := abilityIndex
	activate := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: mana, AbilityIndex: &idx, Answers: activationXAnswers(cost)}
	setup := oraclegen.Seat{}
	addActivationCostFixtures(&setup, cost)
	battlefield := append([]string(nil), setup.Battlefield...)
	attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}}
	return triggerCause{battlefield: battlefield, steps: []oraclegen.Step{activate, {Op: "resolve"}, attack}, xability: []string{prefix}, activateCost: cost}, true
}

// phaseStep maps the Phase$ vocabulary admitted by levelb to ParseStep's
// scenario spelling.
func phaseStep(phase string) (string, bool) {
	switch phase {
	case "BeginCombat":
		return "begin-combat", true
	case "End of Turn":
		return "end", true
	case "Main1":
		return "main1", true
	case "Upkeep":
		return "upkeep", true
	case "Draw":
		return "draw", true
	default:
		return "", false
	}
}
