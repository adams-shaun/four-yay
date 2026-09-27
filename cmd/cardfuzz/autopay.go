package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// autoPay is a run's auto-pay configuration (-autopay, -explore-autopay):
// which seats play with the hosted bot's payment-plan wrapper
// (seat.Bot.EnableAutoPayMana -- the seat host.NewBotPolicySeatWithAutoPayMana
// builds, and the one gorged's -bot-auto-mana default puts on every hosted
// bot). An auto-pay seat casts through the engine's offered PaymentActions
// plan instead of floating mana by hand.
//
//   - "off": no seat auto-pays. Seats, games, state and failure records are
//     byte-identical to cardfuzz before the flag existed.
//   - "all": every production-bot seat auto-pays.
//   - "mixed": each production-bot seat auto-pays or not by one bit of
//     mix(game seed, seat index), so a run holds auto-pay and manual seats
//     (and auto-pay-vs-manual pairings) in roughly even shares, reproducible
//     from the seed alone.
//
// The explore seat (-explore) stays MANUAL unless explore is set
// (-explore-autopay). Deliberately: the wrapper removes every priority
// "activate" (mana ability) option whenever at least one plan is offered,
// which cuts exactly the mana-ability and odd-timing coverage the explore
// seat exists to reach. With explore set, the explore seat auto-pays wherever
// the mode would have selected a production seat in its place (all: always;
// mixed: by its seed bit), which exercises planned casts at the explore
// policy's off-main-phase and in-response timings -- more planner/executor
// surface at the price of mana-ability coverage.
type autoPay struct {
	mode    string
	explore bool
	// measureManualSeatPlans is a MEASUREMENT-ONLY diagnostic
	// (-measure-manual-seat-plans, default false): in off mode there is no
	// payment-plan consumer, so the drive loop builds no seat's extension and
	// manual_seat_priority_with_plan stays zero. Setting this makes the guard
	// build every seat's extension exactly as the pre-lazy eager publisher
	// did, restoring the counter at the cost of the full planner on every
	// priority window. It never changes seats, intents, replay or a failure
	// record: it is deliberately absent from autoPayOf, and autoPayFor stamps
	// only when a.on().
	measureManualSeatPlans bool
}

var autoPayModes = []string{"off", "all", "mixed"}

func parseAutoPay(mode string, explore bool) (autoPay, error) {
	for _, m := range autoPayModes {
		if mode == m {
			return autoPay{mode: mode, explore: explore}, nil
		}
	}
	return autoPay{}, fmt.Errorf("-autopay %q: want one of %s", mode, strings.Join(autoPayModes, "|"))
}

// autoPayOf rebuilds a failure record's configuration for -repro. A record
// written before the flag existed (or in "off" mode) carries no mode.
func autoPayOf(f failure) (autoPay, error) {
	mode := f.AutoPay
	if mode == "" {
		mode = "off"
	}
	return parseAutoPay(mode, f.ExploreAutoPay)
}

// on reports whether this configuration can put any auto-pay seat in a game.
func (a autoPay) on() bool { return a.mode != "" && a.mode != "off" }

// measurePlans reports whether the drive loop must build every seat's payment
// extension even when no seat consumes it, so the manual-seat plan counters
// are populated. It is true for any auto-pay mode (the guard covers every
// seat there unconditionally) and otherwise only under the explicit off-mode
// diagnostic opt-in.
func (a autoPay) measurePlans() bool { return a.on() || a.measureManualSeatPlans }

// seats reports, per seat index, whether that seat auto-pays in the game
// played at seed. exploreIdx is the explore seat's index, or -1 for none.
// It is a pure function of its arguments, so -repro rebuilds the same seats.
func (a autoPay) seats(seed uint64, n, exploreIdx int) []bool {
	out := make([]bool, n)
	for i := range out {
		if i == exploreIdx && !a.explore {
			continue
		}
		switch a.mode {
		case "all":
			out[i] = true
		case "mixed":
			// A salt distinct from every other per-seat derivation of the
			// game seed (the seat seeds use 0x9e3779b97f4a7c15*(i+1)).
			out[i] = mix(seed^0x6175746f70617931, uint64(i))&1 == 1
		}
	}
	return out
}

// seatList is the indices of the auto-pay seats, for the failure record.
func seatList(ap []bool) []int {
	var out []int
	for i, on := range ap {
		if on {
			out = append(out, i)
		}
	}
	return out
}

// apStats is the auto-pay counter set, per game and summed per run. Every
// counter is read off the decisions the drive loop hands its Decision hook
// (after the seat answered, before Submit) and the finished log; none
// changes the game.
type apStats struct {
	// Games is the games whose counters were collected (a game that errors
	// before finishing or hangs contributes none).
	Games int64 `json:"games"`
	// Priority / PriorityPlan: priority decisions posed to an auto-pay seat,
	// and those carrying at least one PaymentAction with a plan.
	Priority     int64 `json:"priority"`
	PriorityPlan int64 `json:"priority_with_plan"`
	// Planned: intents submitted with a Payment selector (planned casts).
	Planned int64 `json:"planned"`
	// PlannedReversed: planned casts whose spell was reversed (a MoveZone
	// "reversed", the CR 733.1 abort) before the next priority answer -- an
	// offered plan the engine could not execute.
	PlannedReversed int64 `json:"planned_reversed"`
	// Fallback: distinct plan ids that surfaced as a PaymentFallback on a
	// decision (the manual mana window a failed plan falls back to).
	Fallback int64 `json:"fallback"`
	// ManualCast: legacy "cast" options an auto-pay seat chose at priority
	// (a cast it paid by hand while auto-pay was on); ManualCastPlan the
	// subset whose object also had an offered plan (the wrapper should
	// always submit the plan then).
	ManualCast     int64 `json:"manual_cast"`
	ManualCastPlan int64 `json:"manual_cast_with_plan"`
	// ManualMana: priority "activate" (mana ability) options an auto-pay
	// seat chose -- floating mana by hand, only possible when no plan was
	// offered at that decision.
	ManualMana int64 `json:"manual_mana"`
	// ManualSeatPriority / ManualSeatPriorityPlan: the same priority counts
	// for manual (non-auto-pay) seats -- how often a plan was on offer to a
	// seat that ignores it.
	ManualSeatPriority     int64 `json:"manual_seat_priority"`
	ManualSeatPriorityPlan int64 `json:"manual_seat_priority_with_plan"`
}

func (s *apStats) add(o *apStats) {
	if o == nil {
		return
	}
	s.Games += o.Games
	s.Priority += o.Priority
	s.PriorityPlan += o.PriorityPlan
	s.Planned += o.Planned
	s.PlannedReversed += o.PlannedReversed
	s.Fallback += o.Fallback
	s.ManualCast += o.ManualCast
	s.ManualCastPlan += o.ManualCastPlan
	s.ManualMana += o.ManualMana
	s.ManualSeatPriority += o.ManualSeatPriority
	s.ManualSeatPriorityPlan += o.ManualSeatPriorityPlan
}

func (s *apStats) String() string {
	return fmt.Sprintf("planned %d (reversed %d) | fallback %d | auto-pay priority %d, with plan %d (%.1f%%) | manual cast %d (with plan %d), manual mana %d | manual-seat priority %d, with plan %d",
		s.Planned, s.PlannedReversed, s.Fallback, s.Priority, s.PriorityPlan, pct(int(s.PriorityPlan), int(s.Priority)),
		s.ManualCast, s.ManualCastPlan, s.ManualMana, s.ManualSeatPriority, s.ManualSeatPriorityPlan)
}

// apProbe collects one game's apStats. install (gbench.Hooks.Setup) binds the
// engine; decision is the gbench.Hooks.Decision callback; finish walks the
// finished log once.
type apProbe struct {
	e       *rules.Engine
	ap      []bool
	st      apStats
	fbSeen  map[string]bool
	planned []plannedCast
	// firstFB is the first PaymentFallback window of the game (planfb).
	firstFB *fallbackSeen
	// firstRev is the first reversed planned cast, set by finish (planrev).
	firstRev *reversal
}

// plannedCast is one planned submission: the log index its DecisionMade
// lands at, the cast object and the submitted witness.
type plannedCast struct {
	at   int
	obj  state.ObjID
	plan decision.PaymentPlan
	// legacy records that the same cast was also an ordinary option
	// (BaseOptionIndex set): a reversal then is not plan-specific.
	legacy bool
}

type fallbackSeen struct {
	at     int
	card   state.ObjID
	reason string
	planID string
}

type reversal struct {
	pc   plannedCast
	note string // the abortCast Note that closed the reversal
	end  int    // log index of that Note
}

func (p *apProbe) install(e *rules.Engine) { p.e = e }

func (p *apProbe) decision(seatIdx int, d *decision.Decision, in decision.Intent, _ *botpolicy.Board) error {
	if d.PaymentFallback != nil {
		if p.fbSeen == nil {
			p.fbSeen = map[string]bool{}
		}
		if !p.fbSeen[d.PaymentFallback.PlanID] {
			p.fbSeen[d.PaymentFallback.PlanID] = true
			p.st.Fallback++
			if p.firstFB == nil && p.e != nil {
				p.firstFB = &fallbackSeen{at: len(p.e.L.Events), card: d.Source, reason: d.PaymentFallback.Reason, planID: d.PaymentFallback.PlanID}
			}
		}
	}
	if in.Payment != nil {
		p.st.Planned++
		if p.e != nil {
			pc := plannedCast{at: len(p.e.L.Events), plan: decision.ClonePaymentPlan(in.Payment.Plan)}
			for _, a := range d.PaymentActions {
				if a.ID == in.Payment.ActionID {
					pc.obj, pc.legacy = a.Cast.Object, a.BaseOptionIndex != nil
				}
			}
			p.planned = append(p.planned, pc)
		}
	}
	if d.Kind != decision.KPriority {
		return nil
	}
	var planned map[state.ObjID]bool
	for _, a := range d.PaymentActions {
		if len(a.Plans) == 0 {
			continue
		}
		if planned == nil {
			planned = map[state.ObjID]bool{}
		}
		planned[a.Cast.Object] = true
	}
	if seatIdx < 0 || seatIdx >= len(p.ap) || !p.ap[seatIdx] {
		p.st.ManualSeatPriority++
		if planned != nil {
			p.st.ManualSeatPriorityPlan++
		}
		return nil
	}
	p.st.Priority++
	if planned != nil {
		p.st.PriorityPlan++
	}
	if in.Payment != nil || len(in.Choices) != 1 || in.Choices[0] < 0 || in.Choices[0] >= len(d.Options) {
		return nil
	}
	switch o := d.Options[in.Choices[0]]; o.Kind {
	case "cast":
		p.st.ManualCast++
		if planned[o.Obj] {
			p.st.ManualCastPlan++
		}
	case "activate":
		p.st.ManualMana++
	}
	return nil
}

// finish counts the planned casts that were reversed: a MoveZone "reversed"
// (abortCast) between the planned DecisionMade and the next priority answer,
// the window in which only that cast's own announcement and payment run.
func (p *apProbe) finish() *apStats {
	p.st.Games = 1
	if p.e == nil {
		return &p.st
	}
	evs := p.e.L.Events
	for _, pc := range p.planned {
		for j := pc.at + 1; j < len(evs); j++ {
			ev := evs[j]
			if ev.Kind == events.DecisionMade && strings.HasPrefix(ev.Text, string(decision.KPriority)+":") {
				break
			}
			if ev.Kind == events.MoveZone && ev.Text == "reversed" {
				p.st.PlannedReversed++
				if p.firstRev == nil {
					r := &reversal{pc: pc, end: j}
					for k := j + 1; k < len(evs) && k < j+64; k++ {
						if evs[k].Kind == events.Note {
							r.note, r.end = evs[k].Text, k
							break
						}
					}
					p.firstRev = r
				}
				break
			}
		}
	}
	return &p.st
}

// contractFailure reports the game's first plan-contract violation as a
// failure (kind, diag, sig), or ok false: "planrev" -- an offered, submitted
// plan whose cast was then reversed (the spec's "no landed stage may
// advertise an unexecutable action") -- ahead of "planfb", a PaymentFallback
// window (the executor abandoned the plan for the manual window). Only a
// game with no engine failure reaches this check (playGame).
func (p *apProbe) contractFailure() (kind, diag, sig string, ok bool) {
	if p.e == nil {
		return "", "", "", false
	}
	name := func(id state.ObjID) string {
		if n := realCardName(p.e, id); n != "" {
			return n
		}
		if o := p.e.G.Obj(id); o != nil && o.Card != nil {
			return cardName(o.Card)
		}
		return fmt.Sprintf("obj %d", id)
	}
	if r := p.firstRev; r != nil {
		card := name(r.pc.obj)
		offer := "plan-only"
		if r.pc.legacy {
			offer = "legacy also offered"
		}
		diag = fmt.Sprintf("planned cast of %s (object %d, %s) reversed at event %d: %q\nplan %s\n-- events --\n%s",
			card, r.pc.obj, offer, r.end, r.note, planText(p.e, r.pc.plan), renderEvents(p.e, r.pc.at, r.end+1))
		return "planrev", diag, "planrev: " + trunc(stripDigits(r.note), 120) + " (" + offer + ") {" + card + "}", true
	}
	if f := p.firstFB; f != nil {
		card := name(f.card)
		from := f.at - 40
		plan := ""
		for _, pc := range p.planned {
			if pc.plan.ID == f.planID {
				from, plan = pc.at, planText(p.e, pc.plan)
			}
		}
		if from < 0 {
			from = 0
		}
		diag = fmt.Sprintf("PaymentFallback %s for %s (object %d) posed at event %d, plan %s %s\n-- events --\n%s",
			f.reason, card, f.card, f.at, f.planID, plan, renderEvents(p.e, from, f.at))
		return "planfb", diag, "planfb: " + f.reason + " {" + card + "}", true
	}
	return "", "", "", false
}

// planText renders a plan's activations with their source names.
func planText(e *rules.Engine, pl decision.PaymentPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cost generic %d mana %v:", pl.Cost.Generic, pl.Cost.Mana)
	for _, a := range pl.Activations {
		n := ""
		if o := e.G.Obj(a.Source); o != nil && o.Card != nil {
			n = cardName(o.Card)
		}
		fmt.Fprintf(&b, " [%s#%d %+v -> %v]", n, a.Source, a.Ability, a.Produces)
	}
	fmt.Fprintf(&b, " pool_spend %v pool_after %v", pl.PoolSpend, pl.PoolAfter)
	return b.String()
}

// renderEvents renders log events [from, to) with object names, in
// tailContext's line format.
func renderEvents(e *rules.Engine, from, to int) string {
	evs := e.L.Events
	if to > len(evs) {
		to = len(evs)
	}
	if from < 0 {
		from = 0
	}
	var b strings.Builder
	for _, ev := range evs[from:to] {
		nm := ""
		if o := e.G.Obj(ev.Obj); ev.Obj != 0 && o != nil && o.Card != nil {
			nm = cardName(o.Card)
		}
		fmt.Fprintf(&b, "%d %s p=%d obj=%d(%s) %s->%s amt=%d %q\n", ev.Seq, ev.Kind, ev.Player, ev.Obj, nm, ev.From, ev.To, ev.Amount, ev.Text)
	}
	return b.String()
}

// runStats is the -stats file: the run's configuration, failure counts by
// kind and the summed auto-pay counters.
type runStats struct {
	AutoPay        string         `json:"autopay"`
	ExploreAutoPay bool           `json:"explore_autopay"`
	Explore        bool           `json:"explore"`
	Seed           uint64         `json:"seed"`
	Games          int            `json:"games"`
	Failures       int            `json:"failures"`
	Kinds          map[string]int `json:"kinds"`
	Sigs           map[string]int `json:"sigs"`
	Stats          apStats        `json:"stats"`
	MirrorVerdicts map[string]int `json:"mirror_verdicts,omitempty"`
	Seconds        float64        `json:"seconds"`
	// GameSeconds sums every finished game's harness wall-clock time (hung
	// and skipped games excluded); with Workers it gives throughput free of
	// the -hang budget a hung game adds to the run's wall time.
	GameSeconds float64 `json:"game_seconds"`
	Workers     int     `json:"workers"`
}

func optionalMirrorVerdicts(enabled bool, counts map[string]int) map[string]int {
	if !enabled {
		return nil
	}
	return counts
}

func formatMirrorVerdicts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func (r *runStats) save(path string) error {
	b, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// journal is the -journal crash log. A fatal runtime error (a goroutine
// stack overflow from an unbounded engine recursion) kills the process past
// every recover, taking all in-flight games with it; the journal line written
// before each game maps the crash trace's goroutine id to the game's seed, so
// a runner can -skip that game and record it as a "fatal" failure.
type journal struct {
	mu sync.Mutex
	f  *os.File
}

// crashLog is nil unless -journal is set; its methods are nil-safe.
var crashLog *journal

func (j *journal) start(gid string, seed uint64) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	fmt.Fprintf(j.f, "start %s %d\n", gid, seed)
}

func (j *journal) end(seed uint64) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	fmt.Fprintf(j.f, "end %d\n", seed)
}

// skipped is one -skip line: a game known to kill the process.
type skipped struct {
	Seed uint64 `json:"seed"`
	Sig  string `json:"sig"`
	Diag string `json:"diag"`
}

func loadSkip(path string) (map[uint64]skipped, error) {
	out := map[uint64]skipped{}
	if path == "" {
		return out, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var s skipped
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("-skip %s: %w", path, err)
		}
		out[s.Seed] = s
	}
	return out, nil
}

// dumpAt is -dump-at (a -repro triage aid): the log index from which the
// first pending decision is printed by dumpDecision.
var dumpAt uint64

// dumpDecision prints a pending decision and the deciding seat's mana
// context: its options, its PaymentActions with their plans, its pool and
// its battlefield.
func dumpDecision(e *rules.Engine, d *decision.Decision) {
	nm := func(id state.ObjID) string {
		if o := e.G.Obj(id); id != 0 && o != nil && o.Card != nil {
			return cardName(o.Card)
		}
		return ""
	}
	fmt.Printf("DUMP decision seq=%d kind=%s player=%d source=%d(%s) prompt=%q turn=%d step=%v\n", d.Seq, d.Kind, d.Player, d.Source, nm(d.Source), d.Prompt, e.G.Turn, e.G.Step)
	for _, o := range d.Options {
		fmt.Printf("  option %d %s obj=%d(%s) mode=%q alt=%d label=%q cost=%q\n", o.Index, o.Kind, o.Obj, nm(o.Obj), o.Mode, o.AltCostIndex, o.Label, o.Cost)
	}
	for _, a := range d.PaymentActions {
		base := -1
		if a.BaseOptionIndex != nil {
			base = *a.BaseOptionIndex
		}
		for _, pl := range a.Plans {
			fmt.Printf("  payment %s obj=%d(%s) base=%d plan %s\n", a.Label, a.Cast.Object, nm(a.Cast.Object), base, planText(e, pl))
		}
	}
	if d.PaymentFallback != nil {
		fmt.Printf("  fallback %+v\n", *d.PaymentFallback)
	}
	pl := e.G.Players[d.Player]
	fmt.Printf("  pool %v life %d\n", pl.Pool, pl.Life)
	for _, id := range e.G.Zone(state.ZBattlefield, d.Player) {
		o := e.G.Obj(id)
		fmt.Printf("  battlefield %d %s tapped=%v sick=%v\n", id, nm(id), o.Tapped, o.SummonSick)
	}
	for _, id := range e.G.Zone(state.ZHand, d.Player) {
		fmt.Printf("  hand %d %s\n", id, nm(id))
	}
	for _, q := range e.G.Players {
		if q.ID == d.Player {
			continue
		}
		for _, id := range e.G.Zone(state.ZBattlefield, q.ID) {
			o := e.G.Obj(id)
			fmt.Printf("  opponent %d battlefield %d %s tapped=%v\n", q.ID, id, nm(id), o.Tapped)
		}
	}
}
