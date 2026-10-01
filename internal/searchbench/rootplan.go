package searchbench

// The full root of a priority item's search (operator decision 2 of the
// 2026-09-30 replication fixes): the root decision is always searched over
// the item's canonical action space, Pass plus every cast and activation,
// and the bot's candidate is never a mana tap.
//
// azmcts's auto-payment vocabulary (Pass, the payment actions, the offered
// cast and worthwhile ability options) is one intent per candidate, so it
// misses every canonical play that needs mana tapped first: an ability with
// a mana cost, a cast in a payment mode (kicked), a cast the planner prices
// only through a scripted prefix or not at all (a chosen-colour source such
// as Heraldic Banner), and the options it leaves out on purpose (land
// plays, an ability the bot's guards decline). Each such play becomes a
// root Macro: its answers from the root decision on, recorded on a clone
// of the searched engine by lowering the planner's witness (payexec), its
// script prefix, or the exact manual-mana search (PotentialPlayScript).
//
// The auto-pay bot taps mana itself exactly when it pursues a play its
// payment plans cannot make. Its candidate is then the play it taps for --
// its first non-mana answer after the taps, followed on a clone -- as a
// vocabulary intent or that play's macro; with neither, the root has no bot
// candidate (Pass is candidate 0).

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/payexec"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// rootPlan is the full root of engine e's priority decision.
type rootPlan struct {
	// Bot is the root's bot answer for azmcts.Root.Bot: the bot's own
	// answer, or, when it taps mana, the vocabulary intent for the play it
	// taps for.
	Bot    decision.Intent
	BotKey azmcts.Key
	Macros []azmcts.Macro
	// Unreached are the canonical keys no candidate can play (no lowering
	// reached them); Covered counts the canonical keys the vocabulary
	// holds.
	Unreached []string
	Covered   int
}

// macroBudget bounds the clones one exact manual-mana search makes.
const macroBudget = 2000

// planRoot plans the full root of e's pending priority decision; raw is the
// auto-pay bot's answer there and botSeed its seed.
func planRoot(e *rules.Engine, raw decision.Intent, botSeed uint64) rootPlan {
	d := e.Pending()
	p := rootPlan{Bot: raw}
	plays, _ := canonPlays(e, d)
	b := botpolicy.BoardFromGame(e.G, e, d.Player)
	macroOf := map[string]int{} // lookup only
	for _, pl := range plays {
		if vocabularyCovers(e, &b, d, pl) {
			p.Covered++
			continue
		}
		steps, err := lowerCanonPlay(e, d, pl, botSeed)
		if err != nil {
			p.Unreached = append(p.Unreached, pl.key)
			continue
		}
		macroOf[pl.key] = len(p.Macros)
		p.Macros = append(p.Macros, azmcts.Macro{Key: azmcts.Key(azmcts.MacroKeyPrefix + pl.key), Label: pl.opt.Label, Steps: steps})
	}
	// The bot's candidate.
	key, at, ok := botPlay(e, d, raw, botSeed)
	if !ok {
		return p
	}
	if at != nil {
		p.Bot = *at // the play the bot tapped for, as a root intent
	}
	if i, ok := macroOf[key]; ok {
		p.BotKey = p.Macros[i].Key
	}
	return p
}

// vocabularyCovers reports whether azmcts's auto-payment vocabulary holds a
// candidate for play pl (paymentVocabulary's rule): a payment action, an
// offered cast option, or an offered ability the bot's guards would take.
func vocabularyCovers(e *rules.Engine, b *botpolicy.Board, d *decision.Decision, pl canonPlay) bool {
	if len(pl.payments) > 0 {
		return true
	}
	if pl.offered < 0 {
		return false
	}
	for _, o := range d.Options[pl.offered:] {
		if k, _ := priorityKey(e, o.Kind, o.Obj, o.Mode, o.Label); k != pl.key {
			continue
		}
		switch o.Kind {
		case "cast":
			return true
		case "ability":
			if b.AbilityWorthTaking(o, d.Player) {
				return true
			}
		}
	}
	return false
}

// botPlay is the play behind the bot's answer raw at d: its canonical key,
// and, when raw is a mana activation, the root intent for the play it taps
// for (nil when raw itself is the answer). ok is false when the bot taps
// mana and then passes, or its play cannot be named.
func botPlay(e *rules.Engine, d *decision.Decision, raw decision.Intent, botSeed uint64) (key string, at *decision.Intent, ok bool) {
	if !IsManaActivation(d, raw) {
		k, err := intentKey(e, d, raw)
		return k, nil, err == nil
	}
	c := e.CloneHypothetical(1)
	actor := d.Player
	in := raw
	for step := 0; step < 32; step++ {
		if err := c.SubmitHypothetical(in); err != nil {
			return "", nil, false
		}
		cd := c.Pending()
		if cd == nil || c.G.Over || cd.Player != actor || len(c.G.Stack) != 0 {
			return "", nil, false
		}
		in = botAnswer(c, botSeed)
		if cd.Kind != decision.KPriority {
			continue
		}
		if IsManaActivation(cd, in) {
			continue
		}
		k, err := intentKey(c, cd, in)
		if err != nil {
			return "", nil, false
		}
		if k == "pass" {
			pass := passIntent(d)
			return k, &pass, true
		}
		if root, found := rootIntentFor(e, d, c, cd, in); found {
			return k, &root, true
		}
		return k, nil, true
	}
	return "", nil, false
}

// intentKey is the canonical key of intent in at decision d of engine e.
func intentKey(e *rules.Engine, d *decision.Decision, in decision.Intent) (string, error) {
	switch {
	case in.Payment != nil:
		for _, pa := range e.EnsurePaymentActions() {
			if pa.ID == in.Payment.ActionID {
				k, _ := priorityKey(e, "cast", pa.Cast.Object, "", "")
				return k, nil
			}
		}
		return "", fmt.Errorf("searchbench: payment action %q not offered", in.Payment.ActionID)
	case len(in.Choices) == 1 && in.Choices[0] >= 0 && in.Choices[0] < len(d.Options):
		o := d.Options[in.Choices[0]]
		if k, _ := priorityKey(e, o.Kind, o.Obj, o.Mode, o.Label); k != "" {
			return k, nil
		}
		return "", fmt.Errorf("searchbench: %s option %q has no canonical action", o.Kind, o.Label)
	}
	return "", fmt.Errorf("searchbench: priority intent %v", in.Choices)
}

// rootIntentFor is the vocabulary intent at the root decision d of e for
// the play the bot chose at decision cd of clone c (in): the payment
// action casting the same object, else the root option of the same kind,
// object and mode.
func rootIntentFor(e *rules.Engine, d *decision.Decision, c *rules.Engine, cd *decision.Decision, in decision.Intent) (decision.Intent, bool) {
	obj, kind, mode := state.ObjID(0), "", ""
	switch {
	case in.Payment != nil:
		for _, pa := range c.EnsurePaymentActions() {
			if pa.ID == in.Payment.ActionID {
				obj, kind = pa.Cast.Object, "cast"
			}
		}
	case len(in.Choices) == 1:
		o := cd.Options[in.Choices[0]]
		obj, kind, mode = o.Obj, o.Kind, o.Mode
	}
	if kind == "cast" && mode == "" {
		for _, pa := range e.EnsurePaymentActions() {
			if pa.Cast.Object == obj && len(pa.Plans) > 0 {
				return decision.Intent{Seq: d.Seq, Player: d.Player, Payment: &decision.PaymentSelection{ActionID: pa.ID, Plan: decision.ClonePaymentPlan(pa.Plans[0])}}, true
			}
		}
	}
	for i, o := range d.Options {
		if o.Kind == kind && o.Obj == obj && o.Mode == mode && (o.Kind == "cast" || o.Kind == "ability") {
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}, true
		}
	}
	return decision.Intent{}, false
}

// lowerCanonPlay records a macro for play pl at e's root decision d: its
// first offered option, else the first of its potential plays that lowers.
func lowerCanonPlay(e *rules.Engine, d *decision.Decision, pl canonPlay, botSeed uint64) ([]azmcts.MacroStep, error) {
	if pl.offered >= 0 {
		o := d.Options[pl.offered]
		return []azmcts.MacroStep{{Player: d.Player, Kind: d.Kind, Picks: []rules.ScriptPick{pickOf(&o)}}}, nil
	}
	var errs []string
	for _, pp := range pl.plans {
		steps, err := lowerPotential(e, d.Player, pp, botSeed)
		if err == nil {
			return steps, nil
		}
		errs = append(errs, err.Error())
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("searchbench: %s: no way to play it", pl.key)
	}
	return nil, fmt.Errorf("searchbench: %s: %s", pl.key, strings.Join(errs, "; "))
}

func pickOf(o *decision.Option) rules.ScriptPick {
	return rules.ScriptPick{Kind: o.Kind, Obj: o.Obj, Label: o.Label, ManaSymbol: o.ManaSymbol}
}

// lowerPotential plays potential play pp from e's root decision on a clone,
// recording every answer, until the play's own option is chosen (or its
// payment action, for an ordinary cast): the planner's witness lowered by
// payexec, its scripted prefix, or the exact manual-mana search's script,
// then the play. An ask none of them answers (a trigger a mana ability
// caused) is the auto-pay bot's.
func lowerPotential(e *rules.Engine, actor state.PlayerID, pp rules.PotentialPlan, botSeed uint64) ([]azmcts.MacroStep, error) {
	c := e.CloneHypothetical(1)
	var steps []azmcts.MacroStep
	submit := func(d *decision.Decision, in decision.Intent) error {
		st := azmcts.MacroStep{Player: d.Player, Kind: d.Kind}
		if in.Payment != nil {
			for _, pa := range c.EnsurePaymentActions() {
				if pa.ID == in.Payment.ActionID {
					st.PayCast = pa.Cast.Object
				}
			}
			if st.PayCast == 0 {
				return fmt.Errorf("payment action %q not offered", in.Payment.ActionID)
			}
		} else {
			for _, ch := range in.Choices {
				if ch < 0 || ch >= len(d.Options) {
					return fmt.Errorf("choice %d out of range", ch)
				}
				st.Picks = append(st.Picks, pickOf(&d.Options[ch]))
			}
		}
		steps = append(steps, st)
		in.Seq, in.Player = d.Seq, d.Player
		return c.SubmitHypothetical(in)
	}
	a := pp.Action
	play := payexec.Play{Kind: a.Kind, Obj: a.Obj, Ability: a.Ability, Mode: a.Mode}
	isPlay := func(o *decision.Option) bool {
		if o.Kind != a.Kind || o.Obj != a.Obj || o.Mode != a.Mode {
			return false
		}
		return a.Kind != "ability" || o.Ability == a.Ability
	}
	cur := pp
	var x *payexec.Execution
	var script []rules.ScriptStep
	scriptAt := 0
	usedPlan, usedScript, usedExact := false, false, false
	for n := 0; n < 128; n++ {
		d := c.Pending()
		if c.G.Over || d == nil {
			return nil, fmt.Errorf("the game ended")
		}
		if x != nil {
			if d.Player != actor && d.Kind == decision.KPriority && x.Waits > 0 {
				if err := submit(d, passIntent(d)); err != nil {
					return nil, err
				}
				continue
			}
			in, st := x.StepOn(d, payexec.SurfaceOf(c, d.Player))
			switch st {
			case payexec.InProgress, payexec.Done:
				if err := submit(d, in); err != nil {
					return nil, err
				}
				if st == payexec.Done {
					return steps, nil
				}
			case payexec.Yield:
				if err := submit(d, botAnswer(c, botSeed)); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("lowering aborted: %s %s", x.Reason, x.Detail)
			}
			continue
		}
		if d.Player != actor {
			return nil, fmt.Errorf("left the seat's decisions")
		}
		if scriptAt < len(script) {
			ss := script[scriptAt]
			if d.Kind != ss.Kind {
				return nil, fmt.Errorf("script step %d wants %s, posed %s", scriptAt, ss.Kind, d.Kind)
			}
			st := azmcts.MacroStep{Player: d.Player, Kind: d.Kind, Picks: ss.Picks}
			in, err := st.Intent(c, d)
			if err != nil {
				return nil, err
			}
			if err := submit(d, in); err != nil {
				return nil, err
			}
			scriptAt++
			continue
		}
		if d.Kind != decision.KPriority {
			if err := submit(d, botAnswer(c, botSeed)); err != nil {
				return nil, err
			}
			continue
		}
		for i := range d.Options {
			if isPlay(&d.Options[i]) {
				if err := submit(d, decision.Intent{Choices: []int{i}}); err != nil {
					return nil, err
				}
				return steps, nil
			}
		}
		if a.Kind == "cast" && a.Mode == "" {
			for _, pa := range c.EnsurePaymentActions() {
				if pa.Cast.Object == a.Obj && len(pa.Plans) > 0 {
					if err := submit(d, decision.Intent{Payment: &decision.PaymentSelection{ActionID: pa.ID, Plan: decision.ClonePaymentPlan(pa.Plans[0])}}); err != nil {
						return nil, err
					}
					return steps, nil
				}
			}
		}
		if n > 0 {
			// Re-price the play from the pool the prefix floated.
			found := false
			for _, q := range c.PotentialPaymentPlans(actor) {
				if q.Action.Kind == a.Kind && q.Action.Obj == a.Obj && q.Action.Ability == a.Ability && q.Action.Mode == a.Mode {
					cur, found = q, true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("the play is no longer a potential play")
			}
		}
		switch {
		case cur.Plan != nil && !usedPlan:
			usedPlan = true
			x = payexec.StartPlay(actor, play, *cur.Plan, payexec.PoolOf(c, actor))
		case len(cur.Script) > 0 && !usedScript:
			usedScript = true
			script, scriptAt = cur.Script, 0
		case !usedExact && cur.Reason != "" && cur.Reason != "insufficient":
			usedExact = true
			sc, why := c.PotentialPlayScript(actor, cur.Action, macroBudget)
			if why != "" {
				return nil, fmt.Errorf("exact search: %s", why)
			}
			if len(sc) == 0 {
				return nil, fmt.Errorf("exact search: empty script for an unoffered play")
			}
			script, scriptAt = sc, 0
		default:
			return nil, fmt.Errorf("no lowering reaches it (verdict %q)", cur.Reason)
		}
	}
	return nil, fmt.Errorf("more than 128 answers")
}
