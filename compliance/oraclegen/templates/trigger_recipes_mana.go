package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// expendProbes are the probes a ManaExpend cause casts, in order. Each has
// mana value 4, resolves with no target and no choice, and is a level-A
// fixture. Distinct names are deliberate: two casts of the SAME card would
// both resolve to one setup ref, so the second cast would name the first
// spell still on the stack instead of the copy in hand.
var expendProbes = []string{"Inspiration", "Concentrate"}

// manaExpendCauses builds the causes of a Mode$ ManaExpend trigger ("Whenever
// you expend N ..."): each probe cast adds 4 to the turn's expend tally, so
// the sequence crosses N on the cast that reaches it. A threshold no probe
// sequence reaches fails closed with a named reason. The trigger uses the
// stack, so the ordinary abilityOnStack fire probe confirms it.
func manaExpendCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string) {
	amount, ok := manaExpendThreshold(t.ParamStr(cards.PKAmount))
	if !ok || amount <= 0 {
		return nil, "ManaExpend amount unreadable"
	}
	var steps []oraclegen.Step
	var hand []string
	spent := 0
	for _, probe := range expendProbes {
		if spent >= amount {
			break
		}
		st, ok := castProbe(reg, probe)
		if !ok {
			continue
		}
		if len(steps) > 0 {
			// A sorcery cannot be cast with a spell still on the stack, and
			// the earlier cast queued the trigger it crossed: resolve it (and
			// the earlier spell) before the next cast. The expend tally is
			// per turn, so it survives the resolve.
			steps = append(steps, oraclegen.Step{Op: "resolve", Seat: 0})
		}
		steps = append(steps, st)
		hand = append(hand, probe)
		spent += len(st.Mana)
	}
	if spent < amount {
		return nil, "ManaExpend amount not reachable with the expend probes"
	}
	return []triggerCause{{hand: hand, steps: steps}}, ""
}

// manaExpendThreshold reads a literal Amount$ N. A variable or SVar threshold
// is left to the matcher (the trigger simply does not fire from this cause),
// so the recipe fails closed rather than guess a spend.
func manaExpendThreshold(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// manaTapTokenProducer makes the probe token: Argothian Opportunist's enters
// trigger (TrigToken, TokenScript$ c_a_powerstone) has no choice to answer.
const (
	manaTapTokenProducer = "Argothian Opportunist"
	manaTapTokenScript   = "c_a_powerstone"
)

// manaTapSubject is the mana source a TapsForMana cause taps: the probe land
// plus the XMage selector of its mana ability. cardName/selfInHand carry an
// Aura trigger source, which setup cannot place unattached and so is cast on
// the probe land first. tokenProbe marks the artifact-token shape: land then
// names the token PRODUCER, which the prelude casts from hand (the token
// itself does not exist at setup), and activateCard is the tap target's ref.
type manaTapSubject struct {
	land       string
	label      string
	prefix     string
	prelude    []oraclegen.Step
	cardName   string
	selfInHand bool
	tokenProbe bool
	// activateCard is the activate step's tap target when it is not
	// "p0:"+land (the token ref the prelude creates).
	activateCard string
}

// tapRef is the ref of the mana source the cause taps.
func (s manaTapSubject) tapRef() string {
	if s.activateCard != "" {
		return s.activateCard
	}
	return "p0:" + s.land
}

// manaTapSubjects lists the probe sources a TapsForMana cause can tap: a
// Forest for the common Land/Land.Basic filters, or a Wastes when the trigger
// restricts Produced$ C. An Aura trigger source is cast onto the probe land
// first, and the artifact-token shape (levelb.ManaTapTokenProbe) casts the
// token producer first. ok is false for a filter the probes cannot satisfy
// (a non-land permanent, a token shape with a qualifier), which then skips
// with a named reason.
func manaTapSubjects(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []manaTapSubject {
	if levelb.ManaTapTokenProbe(t) {
		return manaTapTokenSubject(reg)
	}
	land, label := manaTapProbe(t)
	lf, ok := reg.Lookup(land)
	if !ok || len(lf.Faces) == 0 {
		return nil
	}
	prefixes, why := oraclegen.XMageAbility(lf.Faces[0])
	if why != "" {
		return nil
	}
	prefix, ok := prefixes[0]
	if !ok {
		return nil
	}
	s := manaTapSubject{land: land, label: label, prefix: prefix}
	if oraclegen.HasType(f, "Aura") {
		cast, ok := castProbe(reg, name, "p0:"+land)
		if !ok {
			return nil
		}
		s.prelude = []oraclegen.Step{cast, {Op: "resolve"}}
		s.cardName, s.selfInHand = name, true
	}
	return []manaTapSubject{s}
}

// manaTapTokenSubject serves the servable artifact-token shape (Roxanne,
// Starfall Savant's `ValidCard$ Artifact.token`): the probe source is the
// Powerstone token, whose Add {C} the trigger's reflection mirrors, so the
// prelude casts the producer, resolves its ETB (which creates the token
// tapped) and passes to p0's next-turn upkeep, whose untap step makes the
// token tappable. The XMage selector is the token face's own rule text, not
// the producer's.
func manaTapTokenSubject(reg *cards.Registry) []manaTapSubject {
	tok, ok := reg.Token(manaTapTokenScript)
	if !ok || len(tok.Faces) == 0 {
		return nil
	}
	tf := tok.Faces[0]
	prefixes, why := oraclegen.XMageAbility(tf)
	if why != "" {
		return nil
	}
	prefix, ok := prefixes[0]
	if !ok {
		return nil
	}
	cast, ok := castProbe(reg, manaTapTokenProducer)
	if !ok {
		return nil
	}
	return []manaTapSubject{{
		land:         manaTapTokenProducer,
		label:        "Add {C}",
		prefix:       prefix,
		tokenProbe:   true,
		activateCard: "p0:token:" + tf.Name,
		prelude: []oraclegen.Step{
			cast,
			{Op: "resolve"},
			// The token is created tapped; the pass waits out p1's whole
			// turn so p0's untap step makes it tappable. `upkeep` rather
			// than `main1`: step+active both still match turn 1's main1.
			{Op: "pass_to", Seat: 0, Step: "upkeep", Active: "p0"},
		},
	}}
}

// manaTapProbe picks the mana source and its gorge ability label for the
// trigger's filter: a Wastes for a Produced$ C restriction, a Llanowar Elves
// for a Creature filter ("whenever you tap a creature for mana"), and a Forest
// for the common Land/Land.Basic and Card.AttachedBy filters. The
// artifact-token shape never reaches it (manaTapSubjects takes the token
// subject first).
func manaTapProbe(t *cards.Trigger) (land, label string) {
	if strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKProduced)), "C") {
		return "Wastes", "Add {C}"
	}
	if filterHasTokenFold(t.ParamStr(cards.PKValidCard), "creature") {
		return "Llanowar Elves", "Add {G}"
	}
	return "Forest", "Add {G}"
}

// manaTapFires builds the scenario serving one Static$ True TapsForMana
// requirement. The trigger is a CR 605.1b triggered mana ability: it resolves
// off the stack, so the ordinary abilityOnStack fire probe cannot see it.
// Instead the cause taps a probe land for mana and the fire check compares the
// mana pool with the same tap run without the trigger source: a pool the
// source changes is the trigger's own added mana, so an item is never emitted
// for a trigger that did not fire.
func manaTapFires(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger " + why}
	}
	idx, err := strconv.Atoi(req.Slot)
	if err != nil || idx < 0 || idx >= len(f.Triggers) {
		return skip("index " + req.Slot)
	}
	subs := manaTapSubjects(reg, f, name, &f.Triggers[idx])
	if len(subs) == 0 {
		return skip("no mana source the recipe can tap")
	}
	fired := false
	for _, s := range subs {
		it, ok, didFire := manaTapWith(reg, f, name, req, s)
		fired = fired || didFire
		if ok {
			return it, nil
		}
	}
	if !fired {
		return skip("did not fire")
	}
	return skip("no fixture gorge can play")
}

// manaTapWith tries one probe subject. fired reports that the trigger source
// changed the tapped land's mana pool, so a caller can tell "did not fire" from
// a later replay failure.
func manaTapWith(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, s manaTapSubject) (it oraclegen.Item, ok, fired bool) {
	c := triggerCause{
		prelude: s.prelude,
		steps:   []oraclegen.Step{{Op: "activate", Seat: 0, Card: s.tapRef(), Ability: s.label}},
	}
	if s.tokenProbe {
		// The token does not exist at setup: the producer starts in hand and
		// its ETB creates the token the activate step taps.
		c.hand = []string{s.land}
	} else {
		c.battlefield = []string{s.land}
	}
	if s.selfInHand {
		c.selfInHand = true
		c.hand = []string{s.cardName}
	}
	fx := oraclegen.Fixture{}
	sc := triggerScenario(f, name, c, req, c.steps, &fx)
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return it, false, false
	}
	with, ok := activatePool(res)
	if !ok {
		return it, false, false
	}
	without, ok := controlTapPool(reg, f, s)
	if !ok || with == without {
		return it, false, false
	}
	fired = true
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok {
		return it, false, fired
	}
	it = oraclegen.NewLevelBItem(name, req.Key, TriggerFires.Version, []string{"603.2"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	it.XAbility = make([]string, len(sc.Steps))
	it.XAbility[activateStepIndex(sc.Steps)] = s.prefix
	return it, true, fired
}

// controlTapPool runs the probe tap alone (no trigger source) and returns p0's
// pool at the activate checkpoint. The trigger fired exactly when the source's
// pool differs from this. A token probe's control run replays the subject's
// prelude, so it also casts the producer and activates the real token; without
// it the control's tap target would not exist. An Aura subject's prelude is
// NOT replayed: it casts the trigger source itself, which would put the
// trigger back into the control run.
func controlTapPool(reg *cards.Registry, f *cards.Face, s manaTapSubject) (string, bool) {
	p0 := oraclegen.Seat{Battlefield: []string{s.land}}
	if s.tokenProbe {
		p0 = oraclegen.Seat{Hand: []string{s.land}}
	}
	steps := []oraclegen.Step{{Op: "activate", Seat: 0, Card: s.tapRef(), Ability: s.label}}
	if s.tokenProbe {
		steps = append(append([]oraclegen.Step(nil), s.prelude...), steps...)
	}
	ctrl := oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": p0},
		Steps: steps,
	}
	oraclegen.Baseline(ctrl.Setup, f)
	_, res, ok := oraclegen.Settle(reg, ctrl)
	if !ok {
		return "", false
	}
	return activatePool(res)
}

// activatePool is p0's mana pool at the activate checkpoint of a settled
// scenario.
func activatePool(res rules.OracleResult) (string, bool) {
	for i := range res.Snapshots {
		if strings.Contains(res.Snapshots[i].Checkpoint, "(activate)") {
			return res.Snapshots[i].Players[0].Pool, true
		}
	}
	return "", false
}
