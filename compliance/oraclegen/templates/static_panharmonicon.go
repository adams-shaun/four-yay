// Level-B template for the Panharmonicon static ("that ability triggers an
// additional time"). The effect is visible in the stack snapshot both engines
// already compare: fire a trigger the static covers with the card on the
// battlefield and the stack holds TWO ability entries from the listener; in
// the card-removed control it holds ONE. The item is kept only when gorge
// shows exactly 2 vs 1, so a recipe the static's filter rejects (a cause it
// does not name, a listener its ValidCard$ does not take) is never served by
// accident -- gorge's own matcher decides, the filter is not parsed here.
//
// Two shapes, chosen by the static's own parameters:
//   - an ETB cause (ValidMode$ ChangesZone, ValidCause$ ...): a fixed listener
//     triggers on another permanent entering, and the recipe casts or plays
//     the cause (panharmoniconETBRecipes).
//   - a creature filter with no cause (ValidCard$ only): the listener is the
//     cast probe itself, a creature with an untargeted, non-optional ETB
//     draw/gain-life trigger the filter accepts, chosen from the corpus by
//     newFilterProbe (panharmoniconCreatureProbes).
//
// Every other shape returns a NAMED skip.
package templates

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// panharmoniconRecipe is one way to fire a trigger through an ETB cause: the
// listener sits on the battlefield and triggers on the cause entering.
type panharmoniconRecipe struct {
	listener string
	cause    string
	play     bool // play the cause (a land) rather than cast it
}

// panharmoniconETBRecipes are tried in order; the first for which gorge shows
// 2 vs 1 serves the row. Soul Warden triggers on OTHER creatures entering and
// is listed after the static card in the setup, so placing it never fires it;
// it has no once-per-turn limit (Welcoming Vampire's would make "triggers an
// additional time" a question about that limit, not about the static). Steppe
// Lynx triggers on a land entering (landfall). Both are checked in XMage's
// card database.
var panharmoniconETBRecipes = []panharmoniconRecipe{
	{listener: "Soul Warden", cause: "Savannah Lions"},
	{listener: "Steppe Lynx", cause: "Forest", play: true},
}

// panharmoniconCreatureTries caps the creature probes run for one row; each
// costs two scenario runs.
const panharmoniconCreatureTries = 6

// panharmoniconSafeKeywords are the keywords a creature probe may carry: none
// adds a cast-time decision or an optional cost.
var panharmoniconSafeKeywords = map[string]bool{
	"Flying": true, "Vigilance": true, "Lifelink": true, "Haste": true,
	"Reach": true, "Trample": true, "Menace": true, "Deathtouch": true,
	"First Strike": true, "Defender": true, "Hexproof": true,
}

// panharmoniconParams are the static parameters this template reads; a
// static carrying any other is not a shape the recipes were built for.
var panharmoniconParams = map[string]bool{
	"Mode": true, "ValidMode": true, "ValidCard": true, "ValidCause": true,
	"Destination": true, "Description": true,
}

// panharmoniconUnmodelledParams returns the static's parameter keys outside
// panharmoniconParams, sorted so the skip reason written to the skips file is
// the same on every run (map order must not reach it).
func panharmoniconUnmodelledParams(params map[string]string) []string {
	var extra []string
	for k := range params {
		if !panharmoniconParams[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return extra
}

// panharmoniconItem serves one Panharmonicon static requirement.
func panharmoniconItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static Panharmonicon " + why}
	}
	st, _ := staticSlotOf(f, req)
	if st.Mode == "" {
		return skip("static slot not found")
	}
	valid := strings.ToLower(st.ParamStr(cards.PKValidCard))
	if extra := panharmoniconUnmodelledParams(st.Params); len(extra) != 0 {
		return skip("parameters " + strings.Join(extra, ",") + " are not modelled by the probes")
	}
	switch {
	case valid == "":
		return skip("has no ValidCard filter")
	case strings.Contains(valid, "chosentype"):
		return skip("chosen-type filter needs an as-enters creature-type choice on a setup permanent")
	case strings.Contains(valid, "equipped") || strings.Contains(valid, "attached"):
		return skip("equipped-self filter: the card's only trigger is its own enters, which fires before it can be equipped")
	}

	var (
		recipes []panharmoniconRun
		cr      = []string{"603.2d"}
	)
	if modes := st.ParamStr(cards.PKValidMode); modes != "" {
		if !strings.Contains(","+modes+",", ",ChangesZone,") {
			return skip("cause mode " + modes + " has no single-card ChangesZone trigger the cast probe fires")
		}
		if !strings.EqualFold(st.ParamStr(cards.PKDestination), "Battlefield") {
			return skip("cause mode " + modes + " is not an enters-the-battlefield cause")
		}
		recipes = panharmoniconETBRuns(reg)
	} else {
		if st.HasParam(cards.PKValidCause) {
			return skip("ValidCause without ValidMode is not an enters-the-battlefield cause")
		}
		var why string
		recipes, why = panharmoniconCreatureRuns(reg, name, st.ParamStr(cards.PKValidCard))
		if why != "" {
			return skip(why)
		}
	}
	for _, r := range recipes {
		sc := r.scenario(f, name, req, true)
		res, ok := runStatic(reg, sc)
		if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
			continue
		}
		if n, others := listenerAbilities(res.Snapshots[len(res.Snapshots)-1], "p0:"+r.listener); n != 2 || others != 0 {
			continue
		}
		control, ok := runStatic(reg, r.scenario(f, name, req, false))
		if !ok || len(control.Fails) != 0 || len(control.Snapshots) == 0 {
			continue
		}
		if n, others := listenerAbilities(control.Snapshots[len(control.Snapshots)-1], "p0:"+r.listener); n != 1 || others != 0 {
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, cr, sc)
		it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
		return it, nil
	}
	return skip("no probe's trigger was doubled by the card (2 stack entries with it, 1 without)")
}

// panharmoniconRun is one probe recipe: the listener, the cause step, and the
// extra cards the scenario needs.
type panharmoniconRun struct {
	listener string
	// setupListener places the listener on the battlefield at setup (an ETB
	// cause); false when the listener is the cast probe itself.
	setupListener bool
	hand          string
	steps         []oraclegen.Step
}

// scenario builds the recipe's scenario; withCard=false is the control, with
// the card under test absent.
func (r panharmoniconRun) scenario(f *cards.Face, name string, req levelb.Requirement, withCard bool) oraclegen.Scenario {
	var bf []string
	if withCard {
		bf = append(bf, name)
	}
	if r.setupListener {
		bf = append(bf, r.listener)
	}
	var hand []string
	if r.hand != "" {
		hand = []string{r.hand}
	}
	sc := staticScenario(f, name, bf, hand, r.steps)
	if withCard {
		p0 := sc.Setup["p0"]
		setupBackFace(&p0, name, req)
		sc.Setup["p0"] = p0
	}
	return sc
}

// castSteps casts card with its own mana and passes once each so the spell
// resolves, then stops at priority with its triggers on the stack.
func castSteps(reg *cards.Registry, card string) ([]oraclegen.Step, bool) {
	c, ok := reg.Lookup(card)
	if !ok || len(c.Faces) == 0 {
		return nil, false
	}
	pool, why := oraclegen.PoolFor(c.Faces[0].ManaCost)
	if why != "" {
		return nil, false
	}
	return []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + card, Mana: pool},
		{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
		// Two identical triggers pose a trigger_order decision before they
		// reach the stack; this answers it (the runner's fallback orders them)
		// and stops at priority with both still on the stack.
		{Op: "pass_to", Seat: 0, Decision: "priority"},
	}, true
}

// panharmoniconETBRuns are the ETB-cause recipes whose cards the corpus has.
func panharmoniconETBRuns(reg *cards.Registry) []panharmoniconRun {
	var out []panharmoniconRun
	for _, r := range panharmoniconETBRecipes {
		if _, ok := reg.Lookup(r.listener); !ok {
			continue
		}
		run := panharmoniconRun{listener: r.listener, setupListener: true, hand: r.cause}
		if r.play {
			run.steps = []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + r.cause}}
		} else {
			steps, ok := castSteps(reg, r.cause)
			if !ok {
				continue
			}
			run.steps = steps
		}
		out = append(out, run)
	}
	return out
}

// panharmoniconCreatureRuns picks creature probes for a creature-filter
// static: cast from hand, the probe is its own listener. Candidates come from
// the corpus in (mana value, name) order, so the choice is deterministic.
func panharmoniconCreatureRuns(reg *cards.Registry, name, filter string) ([]panharmoniconRun, string) {
	fp := newFilterProbe(filter, state.ZBattlefield)
	if !fp.decided {
		return nil, "filter " + filter + " is not decidable by the matcher"
	}
	type cand struct {
		name string
		cmc  int32
	}
	var cands []cand
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 {
			continue
		}
		f := c.Faces[0]
		if f.Name == name || !panharmoniconSimpleETB(f) || !oraclegen.XMageKnown(f.Name) {
			continue
		}
		if _, why := oraclegen.PoolFor(f.ManaCost); why != "" {
			continue
		}
		cands = append(cands, cand{f.Name, f.Cmc()})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].cmc != cands[j].cmc {
			return cands[i].cmc < cands[j].cmc
		}
		return cands[i].name < cands[j].name
	})
	var out []panharmoniconRun
	for _, c := range cands {
		card, ok := reg.Lookup(c.name)
		if !ok || !fp.accepts(card) {
			continue
		}
		steps, ok := castSteps(reg, c.name)
		if !ok {
			continue
		}
		out = append(out, panharmoniconRun{listener: c.name, hand: c.name, steps: steps})
		if len(out) == panharmoniconCreatureTries {
			break
		}
	}
	if len(out) == 0 {
		return nil, "no creature probe with a simple enters trigger the filter " + filter + " accepts"
	}
	return out, ""
}

// panharmoniconSimpleETB reports whether f is a plain creature whose only
// printed feature is an untargeted, non-optional, unconditional self-ETB draw
// or life gain: no answer is ever needed and nothing else can reach the stack.
func panharmoniconSimpleETB(f *cards.Face) bool {
	if !f.IsCreature() || f.IsLand() || f.CharacteristicDefining() || len(f.Triggers) != 1 ||
		len(f.Abilities) != 0 || len(f.Statics) != 0 || len(f.Repls) != 0 || f.ManaCost == "" {
		return false
	}
	if _, _, ok := parsePT(f.PT); !ok {
		return false
	}
	for _, k := range f.Keywords {
		if !panharmoniconSafeKeywords[k] {
			return false
		}
	}
	tr := &f.Triggers[0]
	if tr.Mode != "ChangesZone" || !strings.EqualFold(tr.ParamStr(cards.PKDestination), "Battlefield") ||
		!strings.EqualFold(tr.ParamStr(cards.PKValidCard), "Card.Self") ||
		tr.HasParam(cards.PKOptional) || tr.HasParam(cards.PKOptionalDecider) || tr.HasParam(cards.PKCondition) ||
		tr.HasParam(cards.PKIsPresent) || tr.HasParam(cards.PKCheckSVar) || tr.HasParam(cards.PKNumLimitEachTurn) {
		return false
	}
	e := tr.Effect
	if e == nil || e.Sub != nil || (e.API != "Draw" && e.API != "GainLife") ||
		e.HasParam(cards.PKValidTgts) || e.HasParam(cards.PKOptional) || e.HasParam(cards.PKSubAbility) ||
		e.HasParam(cards.PKCondition) || e.HasParam(cards.PKIsPresent) {
		return false
	}
	return true
}

// listenerAbilities counts the ability stack entries from ref and from every
// other source in snap.
func listenerAbilities(snap rules.OracleSnapshot, ref string) (own, others int) {
	for _, e := range snap.Stack {
		if e.Kind != "ability" {
			continue
		}
		if e.Source == ref {
			own++
		} else {
			others++
		}
	}
	return own, others
}
