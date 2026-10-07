package paymirror

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// GameSpec is one driver game: the engine seed (also the root of every
// seat's bot seed), the repo deck each seat plays, and the format.
type GameSpec struct {
	Seed      uint64   `json:"seed"`
	Decks     []string `json:"decks"`
	Commander bool     `json:"commander"`
	// Policy is "bot" (the hosted default) or "lethal" (lethal-pressure),
	// both with auto-pay enabled.
	Policy string `json:"policy"`
	// Lists, when set, are the seats' explicit card lists (random decks);
	// Decks then holds their labels. A finding carrying Lists replays
	// without the generator.
	Lists [][]string `json:"lists,omitempty"`
}

// DriverOptions bounds and configures a driver game.
type DriverOptions struct {
	MaxIntents int // 0 = 20000
	MaxTurns   int32
	// Control enables the live-vs-clone control replay on every check.
	Control bool
	// Trace records full event streams in every report (one-game debugging).
	Trace bool
	// Resolve runs the post-resolution horizon on every equivalent route.
	Resolve bool
	// MaxObjects ends a game whose object arena passes this size (a runaway
	// token board makes every clone and diff expensive); 0 = 2500.
	MaxObjects int
	// BudgetExceeded, when set, truncates a game between intents if the
	// harness-owned budget has expired. It can end a game early (Err
	// "truncated: budget"), never change a check's verdict. This package
	// must not import time: the sweep CLIs keep every clock read in their
	// exempt cmd boundary, so a caller that wants the per-game wall-time
	// bound builds the predicate from its own clock.
	BudgetExceeded func() bool
	// OnReport, when set, receives every report as it is produced (the
	// driver keeps only a compact copy of equivalent ones).
	OnReport func(spec GameSpec, rep *Report)
}

// GameResult is one driver game's outcome.
type GameResult struct {
	Spec    GameSpec  `json:"spec"`
	Reports []*Report `json:"-"`
	Intents int       `json:"intents"`
	Turns   int32     `json:"turns"`
	Over    bool      `json:"over"`
	// Err is a game-level failure: livelock, a rejected bot intent, a run-A
	// error (the live game cannot continue past it), or an engine panic.
	Err string `json:"err,omitempty"`
	// RestViolation is the first priority decision of the game (planned or
	// not) posed with a cast pending or a choose flow armed, with the events
	// that led to it. It locates leaks that later trip the planned executor.
	RestViolation string `json:"rest_violation,omitempty"`
}

// Decks is the resolved deck pool the driver seats from.
type Decks struct {
	Reg         *cards.Registry
	Constructed []string
	Commander   []string
	cards       map[string][]*cards.Card
	cmdIdx      map[string][]int
}

// LoadDecks resolves every repo deck once. Commander decks (a deck file that
// names a commander) are validated with the CR 903 deck gate.
func LoadDecks(reg *cards.Registry) (*Decks, error) {
	d := &Decks{Reg: reg, cards: map[string][]*cards.Card{}, cmdIdx: map[string][]int{}}
	for _, name := range testutil.RepoDeckNames() {
		f, err := testutil.LoadRepoDeckFile(name)
		if err != nil {
			return nil, err
		}
		cs, err := testutil.LoadRepoDeck(reg, name)
		if err != nil {
			return nil, err
		}
		d.cards[name] = cs
		if len(f.CommanderNames()) > 0 {
			if err := f.ValidateCommander(reg); err != nil {
				return nil, fmt.Errorf("commander deck %q: %w", name, err)
			}
			d.cmdIdx[name] = f.CommanderIndices()
			d.Commander = append(d.Commander, name)
			continue
		}
		d.Constructed = append(d.Constructed, name)
	}
	sort.Strings(d.Constructed)
	sort.Strings(d.Commander)
	return d, nil
}

// Pick seats `seats` distinct decks from the format's pool, a pure function
// of seed.
func (d *Decks) Pick(seed uint64, seats int, commander bool) []string {
	pool := d.Constructed
	if commander {
		pool = d.Commander
	}
	r := rand.New(rand.NewPCG(seed, seed^0x5eed5eed))
	perm := r.Perm(len(pool))
	out := make([]string, seats)
	for i := range out {
		out[i] = pool[perm[i%len(perm)]]
	}
	return out
}

func (d *Decks) config(spec GameSpec) (rules.Config, error) {
	names := make([]string, len(spec.Decks))
	decks := make([][]*cards.Card, len(spec.Decks))
	var cmd [][]int
	for i, n := range spec.Decks {
		names[i] = n
		if spec.Lists != nil {
			cs, err := ResolveList(d.Reg, spec.Lists[i])
			if err != nil {
				return rules.Config{}, err
			}
			decks[i] = cs
			continue
		}
		cs, ok := d.cards[n]
		if !ok {
			return rules.Config{}, fmt.Errorf("unknown repo deck %q", n)
		}
		decks[i] = cs
		if spec.Commander {
			cmd = append(cmd, d.cmdIdx[n])
		}
	}
	cfg := rules.Config{Seed: spec.Seed, Names: names, Decks: decks, Tokens: d.Reg.Tokens, NameUniverse: d.Reg.Universe()}
	if spec.Commander {
		cfg.Format = rules.FormatCommander
		cfg.StartingLife = 40
		cfg.Commanders = cmd
	}
	return cfg, nil
}

// botFor builds seat i's auto-pay bot for the spec.
func botFor(spec GameSpec, i int) *seat.Bot {
	s := spec.Seed*1000003 + uint64(i)
	var b *seat.Bot
	if spec.Policy == "lethal" {
		b = seat.NewLethalPressureBot(s)
	} else {
		b = seat.NewBot(s)
	}
	return b.EnableAutoPayMana()
}

// PlayGame plays one auto-pay bot game and mirror-checks every planned cast
// the bots submit. Run A of each check is the live game itself (CheckLive),
// so the game is exactly the game the same bots would play unobserved.
func PlayGame(d *Decks, spec GameSpec, opt DriverOptions) (res GameResult) {
	cfg, err := d.config(spec)
	if err != nil {
		res.Spec = spec
		res.Err = "config: " + err.Error()
		return res
	}
	return PlayConfig(cfg, spec, opt)
}

// PlayConfig is PlayGame over an explicit engine Config (authored fixtures,
// or any deck source); spec supplies the bot seeds, policy and labels.
func PlayConfig(cfg rules.Config, spec GameSpec, opt DriverOptions) (res GameResult) {
	if opt.MaxIntents <= 0 {
		opt.MaxIntents = 20000
	}
	if opt.MaxTurns <= 0 {
		opt.MaxTurns = 60
	}
	if opt.MaxObjects <= 0 {
		opt.MaxObjects = 2500
	}
	res.Spec = spec

	e := rules.NewStartingPlayerChoice(cfg)
	bots := make([]*seat.Bot, len(cfg.Names))
	for i := range bots {
		bots[i] = botFor(spec, i)
	}
	ctx := context.Background()
	answer := func(e *rules.Engine, dec *decision.Decision) (decision.Intent, error) {
		return bots[dec.Player].DecideBoard(ctx, botpolicy.BoardFromGame(e.G, e, dec.Player), *dec)
	}
	defer func() {
		if r := recover(); r != nil {
			if lle, ok := r.(*rules.LivelockError); ok {
				res.Err = "livelock: " + lle.Error()
			} else {
				res.Err = fmt.Sprintf("panic: %v", r)
			}
		}
		res.Turns, res.Over = e.G.Turn, e.G.Over
	}()
	e.AskStartingPlayer()
	e.Advance()
	for !e.G.Over && e.Pending() != nil && res.Intents < opt.MaxIntents && e.G.Turn <= opt.MaxTurns {
		if len(e.G.Objs) > opt.MaxObjects {
			res.Err = "truncated: bigboard"
			return res
		}
		if opt.BudgetExceeded != nil && opt.BudgetExceeded() {
			res.Err = "truncated: budget"
			return res
		}
		dec := e.Pending()
		e.EnsurePaymentActions()
		if res.RestViolation == "" {
			if v := atRestViolation(e); v != "" {
				res.RestViolation = fmt.Sprintf("seq %d: %s after %v", dec.Seq, v, recentEvents(e, restContext))
			}
		}
		in, err := answer(e, dec)
		if err != nil {
			res.Err = "bot: " + err.Error()
			return res
		}
		if in.Payment != nil {
			rep := CheckLive(e, in, answer, Options{Control: opt.Control, Trace: opt.Trace, Resolve: opt.Resolve})
			if opt.OnReport != nil {
				opt.OnReport(spec, rep)
			}
			if opt.Trace {
				res.Reports = append(res.Reports, rep)
			} else {
				res.Reports = append(res.Reports, compact(rep))
			}
			res.Intents += 1 + len(rep.FollowUps)
			if rep.AError != "" {
				res.Err = "run A: " + rep.AError
				return res
			}
			if rep.AInvariant != "" {
				// The live engine now offers priority mid-cast; every later
				// check would inherit that corruption. The finding is recorded.
				res.Err = "stopped: run A left the engine mid-cast (" + rep.AInvariant + ")"
				return res
			}
			continue
		}
		if err := e.Submit(in); err != nil {
			res.Err = fmt.Sprintf("intent %d rejected: %v", res.Intents, err)
			return res
		}
		res.Intents++
	}
	return res
}

// compact drops the bulky parts of an equivalent report. A report whose
// resolve horizon panicked is kept whole: the panic is a finding.
func compact(r *Report) *Report {
	if s, _ := r.Verdict(); s != Equivalent {
		return r
	}
	for _, rr := range r.Routes {
		if rr.HorizonPanic != "" {
			return r
		}
	}
	c := *r
	c.FollowUps = nil
	c.Plan.Activations = nil
	routes := make([]RouteResult, len(r.Routes))
	for i, rr := range r.Routes {
		routes[i] = RouteResult{Route: rr.Route, Status: rr.Status, Reason: rr.Reason, Signature: rr.Signature,
			EventOrderDiffers: rr.EventOrderDiffers, ShapeNotes: rr.ShapeNotes, Resolved: rr.Resolved}
	}
	c.Routes = routes
	return &c
}

// recentEvents renders the last n events of e's log.
func recentEvents(e *rules.Engine, n int) []string {
	evs := e.L.Events
	if len(evs) > n {
		evs = evs[len(evs)-n:]
	}
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		out = append(out, fmt.Sprintf("%d %s", ev.Seq, eventKey(ev)))
	}
	return out
}

// restContext is how many trailing events a RestViolation records.
var restContext = 24
