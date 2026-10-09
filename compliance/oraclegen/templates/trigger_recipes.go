package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// triggerCause is one candidate way of making a trigger's cause happen on
// turn 1 with ops the XMage driver already has (cast, attack, pass_to).
type triggerCause struct {
	hand        []string                  // probe cards added to p0's hand
	exile       []string                  // probe cards added to p0's exile (a cast-from-exile cause)
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
	// libraryTop seeds p0's top-of-library cards, for a cause whose keyword
	// action reads the revealed top card (an explore a "explores a land" /
	// "explores a nonland" trigger narrows on).
	libraryTop []string
	// opponentBattlefield are p1 permanents (a blocker, an attacker, a tap
	// target, an opponent-comparison gate) the cause needs on the other side
	// of the table.
	opponentBattlefield []string
	// opponentCounters puts counters on p1's battlefield cards at setup
	// (card name -> kind -> count), the p1 side of counters.
	opponentCounters map[string]map[string]int
	activateCost     string // Forge cost of the activate step in steps (Crew/Saddle tap choice); "" when none
	// preludeActivationCost is the Forge cost of a prelude activate step
	// whose cost carries a choice (scriptPreludeActivationCost exports its
	// picks); "" when no prelude activates with such a cost.
	preludeActivationCost string
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
	// Sign in Blood ({B}{B}) makes a TARGET player draw two and lose 2 life,
	// so it is the only probe that can make an opponent draw (the "their
	// second card each turn" triggers).
	drawOtherProbe = "Sign in Blood"
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
			// A non-creature permanent dies to the removal spell its type
			// admits; an Aura is cast on a bearer first (setup cannot place
			// an unattached Aura) and destroyed on it.
			return selfDiesNonCreatureCauses(reg, f, name)
		}
		for _, p := range destroyProbes {
			cast(p, "p0:"+name)
		}
	case "trigger.attacks", "trigger.attacks-one-target", "trigger.combat-damage", "trigger.combat-damage-all":
		combatDamage := sub == "trigger.combat-damage" || sub == "trigger.combat-damage-all"
		var attackers []string
		var extra []string
		if combatDamage {
			if !creature {
				return nil, "combat-damage needs a creature"
			}
			attackers = []string{"p0:" + name}
		} else {
			attackers, extra = triggerAttacker(reg, f, name, t)
		}
		attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers}
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
		out = spellCastProbeCauses(reg, f, name, t)
		present := strings.ToLower(t.ParamStr(cards.PKIsPresent) + t.ParamStr(cards.PKIsPresent2))
		// A "Solved —" cast trigger fires only once the source Case is
		// solved: the cast cause runs after the solve sequence, whose
		// activation preludes end at p0's next main phase.
		if solvedSelfSpec(t.ParamStr(cards.PKIsPresent)) || solvedSelfSpec(t.ParamStr(cards.PKIsPresent2)) {
			if preludes, ok := solvedCasePreludes(reg, f); ok {
				var with []triggerCause
				for _, c := range out {
					for _, p := range preludes {
						with = append(with, applyPrelude(c, p))
					}
				}
				out = append(out, with...)
			}
		}
		// "Whenever you cast a spell while CARDNAME is attacking": the cast
		// cause runs from the post-attackers priority round, where the source
		// is attacking and an instant cast is legal.
		if strings.Contains(present, "attacking") && f.IsCreature() {
			atk := conditionPrelude{steps: []oraclegen.Step{{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}}}}
			var with []triggerCause
			for _, c := range out {
				with = append(with, applyPrelude(c, atk))
			}
			out = append(out, with...)
		}
		return out, ""
	case "trigger.becomes-target":
		if !creature {
			// A non-creature permanent is targeted by the opponent's removal
			// spell its type admits (the ward ask is declined as ever).
			return becomesTargetNonCreatureCauses(reg, f, name)
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
	case "trigger.life-lost":
		// Shock at the losing player: the source's controller for You/Player
		// ("whenever you lose life" / "whenever a player loses life"), an
		// opponent for Opponent ("whenever an opponent loses life during your
		// turn", Kefka). Damage to a player is a loss of life, so the engine's
		// LifeLost matcher sees it; PlayerTurn$ True is satisfied because turn
		// 1 is p0's turn.
		target := "p0"
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "Opponent") {
			target = "p1"
		}
		if c, ok := castCause(reg, name, shockProbe, target); ok {
			out = append(out, c)
		}
	case "trigger.drawn":
		for _, p := range drawProbes {
			if c, ok := castCause(reg, name, p); ok {
				out = append(out, withDrawCheckpoint(c))
			}
		}
	case "trigger.drawn-other":
		// Sign in Blood makes the target player draw two, so the "second card
		// drawn" trigger fires. The drawer is p1 when the trigger names an
		// opponent (ValidPlayer Opponent, or a ValidCard Card.OppOwn filter),
		// else p0 (ValidPlayer Player / a bare each-player draw).
		target := "p0"
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "Opponent") || filterHasTokenFold(t.ParamStr(cards.PKValidCard), "oppown") {
			target = "p1"
		}
		if c, ok := castCause(reg, name, drawOtherProbe, target); ok {
			out = append(out, withDrawCheckpoint(c))
		}
	case levelb.ManaExpendSub:
		return manaExpendCauses(reg, name, t)
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
		if active == "p0" && step != "main1" && remembersOwnChoices(t.Effect) {
			// "Choose one that hasn't been chosen" (Demonic Pact): setup
			// passes turn 1's upkeep with the card in place, so the trigger
			// fires there first on a fallback answer and narrows the modes the
			// observed firing offers -- a hidden setup choice XMage's driver
			// does not make alike. Cast the card on turn 1 so the observed
			// firing is its first.
			cast := base
			cast.castSelfX = true
			out = append(out, cast)
		}
		out = append(out, base)
		for _, condition := range conditionPreludes(reg, t.Params, f.SVars) {
			out = append(out, applyPrelude(base, condition))
		}
		fixtures := triggerConditionFixtures(reg, f, t)
		for _, condition := range fixtures {
			out = append(out, applyPrelude(base, condition))
			if !condition.solvedCase {
				continue
			}
			// A solved Case is true only from the solve resolve on, so the
			// row trigger's own phase must be p0's next one: a begin-combat
			// or end-step You-gated row trigger would otherwise stop at p1's
			// first matching phase, where the You gate holds it back.
			if vp := t.ParamStr(cards.PKValidPlayer); vp == "" || strings.EqualFold(vp, "You") {
				forced := applyPrelude(base, condition)
				forced.steps = append([]oraclegen.Step(nil), base.steps...)
				if forced.steps[0].Active != "p0" {
					forced.steps[0].Active = "p0"
					out = append(out, forced)
				}
			}
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
		if causes, why, ok := keywordActionRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := castFamilyRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := phaseOtherCauses(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := tapCombatRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := stateTriggerRecipe(reg, f, name, t, sub); ok {
			return causes, why
		}
		if causes, why, ok := remainderTriggerRecipe(reg, f, name, t, sub); ok {
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
	mana, gap := activationCostIn(cost, "battlefield", "")
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
	addActivationCostFixtures(&setup, name, cost, activationX(cost))
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

// remembersOwnChoices reports whether an effect chain's choices depend on
// its own earlier resolutions: a Charm whose ChoiceRestriction$ removes the
// modes already chosen this game.
func remembersOwnChoices(sa *cards.SA) bool {
	seen := map[*cards.SA]bool{}
	for ; sa != nil && !seen[sa]; sa = sa.Sub {
		seen[sa] = true
		if strings.EqualFold(sa.ParamStr(cards.PKChoiceRestriction), "ThisGame") {
			return true
		}
	}
	return false
}
