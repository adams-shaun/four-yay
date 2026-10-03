package searchbench

// The benchmark's clairvoyant search, exported for a LIVE game: a self-play
// driver that plays whole games on the real engine and searches every
// decision of both seats (internal/mzplay, the DraftZero loop's stand-in for
// the XMage JVM). It is search() over the real engine and nothing more: the
// full priority root (planRoot: Pass, every cast and activation, every land
// play, the plays that need mana tapped first as macros), the ordinary root
// for the other searched kinds, the redacted auto-pay bot's answer for every
// decision that is not searched. One difference from the benchmark's root:
// guardedMacros.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// LiveAction names one root candidate of a live priority search by what it
// plays, not by how the engine numbered it.
type LiveAction struct {
	// Kind is "pass", "cast", "ability", "land" or "special" (the canonical
	// kinds of priorityKey); "" when the candidate could not be named.
	Kind string
	// Name is the card cast or played, or the ability's source.
	Name string
	// Mode is a cast's payment mode ("" the card's own cost).
	Mode string
	// Obj is the card or source object.
	Obj state.ObjID
	// Ability is the source face's activated-ability index for an "ability"
	// candidate, -1 when it is not known (a granted or keyword-granted
	// ability).
	Ability int
	// Label is the canonical label: "Pass", "Cast <name>", "Play <name>",
	// "Activate <source>: <text>".
	Label string
	// Macro marks a candidate played as several submits.
	Macro bool
}

// LiveRoot is one search of a live game's pending decision.
type LiveRoot struct {
	// Result is the search's. Result.Intent is the bot's answer whenever
	// Chosen is false.
	Result azmcts.Result
	// Actions parallels Result.Keys at a priority root (nil at every other
	// kind and when no tree was built).
	Actions []LiveAction

	macros []azmcts.Macro
}

// Chosen reports whether the answer came from the tree (a tree was built,
// at least one simulation completed and the wall-clock bail-out did not
// fire).
func (l LiveRoot) Chosen() bool { return chosen(l.Result) }

// SearchLive searches e's pending decision for the seat it belongs to, on
// clairvoyant worlds (clones of e, hidden zones and future chance included:
// the driving command must have called clairvoyant.AllowClairvoyant).
// botSeed seeds the redacted auto-pay bot whose answer is the root's bot
// candidate and the answer of every decision that is not searched. e is
// only read.
func SearchLive(ctx context.Context, e *rules.Engine, botSeed uint64, net *policynet.Model, opts azmcts.Options) (LiveRoot, error) {
	return SearchLiveReuse(ctx, e, botSeed, net, opts, nil)
}

// SearchLiveReuse is SearchLive with the seat's tree carrier
// (azmcts.Options.ReuseTree, which must be set exactly when reuse is
// non-nil): one carrier per seat and game, and the caller plays the
// returned choice before the seat's next search.
func SearchLiveReuse(ctx context.Context, e *rules.Engine, botSeed uint64, net *policynet.Model, opts azmcts.Options, reuse *azmcts.Reuse) (LiveRoot, error) {
	return SearchLiveCombat(ctx, e, botSeed, net, opts, reuse, nil)
}

// SearchLiveCombat is SearchLiveReuse at one creature of a split attack or
// block declaration (azmcts.Options.CombatSteps): combat holds the answers
// already given to the creatures before it (azmcts.Root.Combat). The
// Result's Step describes the creature and StepAnswer is its answer; the
// caller searches every creature in turn and submits
// azmcts.CombatDeclaration after the last. Play must not be called on a
// step.
func SearchLiveCombat(ctx context.Context, e *rules.Engine, botSeed uint64, net *policynet.Model, opts azmcts.Options, reuse *azmcts.Reuse, combat []int) (LiveRoot, error) {
	if e == nil || e.G.Over || e.Pending() == nil {
		return LiveRoot{}, errors.New("searchbench: SearchLive needs an engine at a pending decision")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	d := e.Pending()
	obs, err := rootObserver(e, d.Player)
	if err != nil {
		return LiveRoot{}, err
	}
	src, err := clairvoyant.NewClairvoyant(e, obs)
	if err != nil {
		return LiveRoot{}, err
	}
	root := azmcts.Root{Engine: e, Decision: d, Bot: botAnswer(e, botSeed), Observer: obs, Reuse: reuse, Combat: combat}
	var plan *rootPlan
	if d.Kind == decision.KPriority && opts.AutoPayment {
		p := planRoot(e, root.Bot, botSeed)
		p.Macros = guardedMacros(e, d, p.Macros, p.BotKey)
		plan = &p
		root.Bot, root.BotKey, root.Macros, root.NoBot = p.Bot, p.BotKey, p.Macros, true
	}
	r, err := azmcts.Search(ctx, root, src, net, opts)
	if err != nil {
		return LiveRoot{Result: r}, err
	}
	out := LiveRoot{Result: r}
	if plan != nil {
		out.macros = plan.Macros
	}
	if r.Kind == "priority" && len(r.Keys) > 0 {
		out.Actions = liveActions(e, d, r, out.macros)
	}
	return out, nil
}

// guardedMacros drops the root macros a LIVE game must not offer: an
// activated ability the decision offers as it stands (a one-step macro)
// that the bot's own activation guards decline
// (botpolicy.Board.AbilityWorthTaking: a provable no-op, or the per-turn
// activation budget spent). The benchmark's root keeps them -- a benchmark
// item is one decision -- but a game is many: a free repeatable activation
// the guards decline (an Equip made free by a cost reducer, moved from
// creature to creature) would be searched, chosen and offered again without
// end, exactly the non-termination azmcts.worthOptions exists to prevent
// inside a walk. The macro keyed keep (the bot's own candidate) stays.
func guardedMacros(e *rules.Engine, d *decision.Decision, macros []azmcts.Macro, keep azmcts.Key) []azmcts.Macro {
	var b botpolicy.Board
	built := false
	out := macros[:0:0]
	for _, m := range macros {
		if m.Key != keep && len(m.Steps) == 1 && m.Steps[0].PayCast == 0 && len(m.Steps[0].Picks) == 1 {
			if in, err := m.Steps[0].Intent(e, d); err == nil && len(in.Choices) == 1 && in.Choices[0] >= 0 && in.Choices[0] < len(d.Options) {
				if o := d.Options[in.Choices[0]]; o.Kind == "ability" {
					if !built {
						b, built = botpolicy.BoardFromGame(e.G, e, d.Player), true
					}
					if !b.AbilityWorthTaking(o, d.Player) {
						continue
					}
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// liveActions names every root candidate of a priority search.
func liveActions(e *rules.Engine, d *decision.Decision, r azmcts.Result, macros []azmcts.Macro) []LiveAction {
	out := make([]LiveAction, len(r.Keys))
	var plays []canonPlay
	var payments []decision.PaymentAction
	for i, k := range r.Keys {
		a := LiveAction{Ability: -1}
		if azmcts.IsMacroKey(k) {
			if plays == nil {
				plays, _ = canonPlays(e, d)
			}
			key := strings.TrimPrefix(string(k), azmcts.MacroKeyPrefix)
			for _, pl := range plays {
				if pl.key != key {
					continue
				}
				a.Kind, a.Name, a.Mode, a.Label = pl.opt.Kind, pl.opt.Name, pl.opt.Mode, pl.opt.Label
				switch {
				case pl.offered >= 0:
					o := d.Options[pl.offered]
					a.Obj = o.Obj
					if o.Kind == "ability" && o.SVar == "" && o.Keyword == "" {
						a.Ability = o.Ability
					}
				case len(pl.plans) > 0:
					act := pl.plans[0].Action
					a.Obj = act.Obj
					if act.Kind == "ability" {
						a.Ability = act.Ability
					}
				}
				break
			}
			a.Macro = true
			out[i] = a
			continue
		}
		if i >= len(r.Candidates) {
			out[i] = a
			continue
		}
		in := r.Candidates[i]
		switch {
		case in.Payment != nil:
			if payments == nil {
				payments = e.EnsurePaymentActions()
			}
			for _, pa := range payments {
				if pa.ID == in.Payment.ActionID {
					_, opt := priorityKey(e, "cast", pa.Cast.Object, "", "")
					a.Kind, a.Name, a.Label, a.Obj = opt.Kind, opt.Name, opt.Label, pa.Cast.Object
				}
			}
		case len(in.Choices) == 1 && in.Choices[0] >= 0 && in.Choices[0] < len(d.Options):
			o := d.Options[in.Choices[0]]
			_, opt := priorityKey(e, o.Kind, o.Obj, o.Mode, o.Label)
			a.Kind, a.Name, a.Mode, a.Label, a.Obj = opt.Kind, opt.Name, opt.Mode, opt.Label, o.Obj
			if o.Kind == "ability" && o.SVar == "" && o.Keyword == "" {
				a.Ability = o.Ability
			}
		}
		out[i] = a
	}
	return out
}

// Play submits root candidate choice to the live engine e, the engine the
// search ran on: one submit, or every step of a macro. It returns the number
// of submits made; after an error the engine stands wherever the failing
// step left it (a later step of a macro can fail only if the live engine
// diverged from the clone the macro was recorded on).
func (l LiveRoot) Play(e *rules.Engine, choice int) (int, error) {
	r := l.Result
	if r.Step != nil {
		return 0, errors.New("searchbench: a creature step is an answer, not a submit (azmcts.CombatDeclaration)")
	}
	if choice < 0 || choice >= len(r.Keys) || choice >= len(r.Candidates) {
		return 0, fmt.Errorf("searchbench: root candidate %d of %d", choice, len(r.Keys))
	}
	k := r.Keys[choice]
	if !azmcts.IsMacroKey(k) {
		if err := e.Submit(r.Candidates[choice]); err != nil {
			return 0, err
		}
		return 1, nil
	}
	for i := range l.macros {
		m := &l.macros[i]
		if m.Key != k {
			continue
		}
		for n, st := range m.Steps {
			d := e.Pending()
			if e.G.Over || d == nil {
				return n, fmt.Errorf("searchbench: macro %s: the game ended at step %d", m.Label, n)
			}
			in, err := st.Intent(e, d)
			if err != nil {
				return n, fmt.Errorf("searchbench: macro %s step %d: %w", m.Label, n, err)
			}
			if err := e.Submit(in); err != nil {
				return n, fmt.Errorf("searchbench: macro %s step %d: %w", m.Label, n, err)
			}
		}
		return len(m.Steps), nil
	}
	return 0, fmt.Errorf("searchbench: the chosen macro %s is not in the root plan", k)
}
