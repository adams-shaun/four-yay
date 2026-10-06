//go:build autopayaudit

// Command autopayaudit is the goal-1 audit harness for the hosted bot's
// auto-pay adapter (seat/bot.go paymentIntent). It plays bot-auto-pay
// against bot (or a mirror) through internal/bench exactly the way
// cmd/botbench does -- same seed derivation, same per-seat seeds, same
// Config -- and observes every decision with PURE engine reads (no Submit,
// no event, no RNG): the counterfactual manual policy is asked on a fresh
// seat.Bot, whose priority branch consumes no RNG, so observing never
// changes a game.
//
// Built only with -tags autopayaudit, because it reads the build-tagged
// rules audit helpers (rules/zz_autopayaudit.go). Production binaries,
// tests and goldens are untouched.
//
//	go run -tags autopayaudit ./cmd/autopayaudit -pairs all -games 10 -seed 5400000 -workers 6 -out audit.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

const (
	armAuto   = 0
	armManual = 1
)

var armNames = [2]string{"auto", "manual"}

// labeled is a bench seat that remembers which arm it is. Embedding *seat.Bot
// promotes Decide and DecideBoard, so it is exactly the production seat.
type labeled struct {
	*seat.Bot
	auto bool
}

func autoCtor(seed uint64) seat.Seat {
	return &labeled{Bot: seat.NewBot(seed).EnableAutoPayMana(), auto: true}
}
func manualCtor(seed uint64) seat.Seat { return &labeled{Bot: seat.NewBot(seed)} }

type example struct {
	Cat  string `json:"cat"`
	Text string `json:"text"`
}

type gameResult struct {
	counts   [2]map[string]int64
	examples []example
}

func (r *gameResult) add(a int, k string, n int64) { r.counts[a][k] += n }

// addDeck counts k twice: pooled under the arm, and under the arm's deck, so
// the report can say which decks carry an event.
func (gs *gameState) addDeck(seatIdx int, k string) {
	a := gs.arm[seatIdx]
	gs.res.add(a, k, 1)
	gs.res.add(a, "deck:"+gs.deck[seatIdx]+":"+k, 1)
}

type turnMode struct {
	seq []byte
}

type pendingStrand struct {
	seat int
	b    state.ObjID
	turn int32
}

type gameState struct {
	e      *rules.Engine
	arm    [2]int
	res    *gameResult
	cf     *seat.Bot // counterfactual manual policy (priority consumes no RNG)
	pairID string
	seed   uint64
	deck   [2]string
	// manual-arm tap-sequence snapshot (task d)
	snap     [2]*rules.Engine
	snapTurn [2]int32
	snapStep [2]state.Step
	// per (seat, turn) answer sequence (task c)
	modes   map[[2]int32]*turnMode
	pending []pendingStrand
	maxEx   int
	maxObjs int
	exCount map[string]int
}

func (gs *gameState) ex(cat, format string, a ...any) {
	if gs.exCount[cat] >= gs.maxEx {
		return
	}
	gs.exCount[cat]++
	gs.res.examples = append(gs.res.examples, example{Cat: cat,
		Text: fmt.Sprintf("[%s seed %d t%d %s %s] ", gs.pairID, gs.seed, gs.e.G.Turn, gs.window(gs.e.Pending()), "") + fmt.Sprintf(format, a...)})
}

func (gs *gameState) name(id state.ObjID) string {
	if o := gs.e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return fmt.Sprintf("obj%d", id)
}

// window names where a decision sits: whose turn, which step, and whether the
// stack is empty.
func (gs *gameState) window(d *decision.Decision) string {
	if d == nil {
		return "?"
	}
	g := gs.e.G
	who := "opp"
	if g.Active == d.Player {
		who = "own"
	}
	st := g.Step.String()
	if len(g.Stack) > 0 {
		st += "+stack"
	}
	return who + ":" + st
}

func optionAt(d *decision.Decision, i int) (decision.Option, bool) {
	if i < 0 || i >= len(d.Options) {
		return decision.Option{}, false
	}
	return d.Options[i], true
}

func answerKind(d *decision.Decision, in decision.Intent) (string, decision.Option) {
	if in.Payment != nil {
		return "payment", decision.Option{}
	}
	if len(in.Choices) != 1 {
		return "other", decision.Option{}
	}
	o, ok := optionAt(d, in.Choices[0])
	if !ok {
		return "other", decision.Option{}
	}
	switch o.Kind {
	case "activate", "play_land", "cast", "ability", "pass":
		return o.Kind, o
	}
	return "other:" + o.Kind, o
}

// manaCost reports whether an offer-time cost string carries a mana part.
func manaCost(cost string) bool {
	for _, f := range strings.Fields(cost) {
		if f == "T" || f == "Q" {
			continue
		}
		if strings.ContainsAny(f[:1], "0123456789WUBRGCX") && !strings.Contains(f, "<") {
			return true
		}
	}
	return false
}

func manaOf(m decision.ManaAmount) state.Mana {
	var out state.Mana
	for i, n := range m {
		out[i] = int32(n)
	}
	return out
}

// syntheticPick asks the manual policy which of objs it would cast if every
// one were offered (the chooseCast ranking), returning 0 when it would pass.
func (gs *gameState) syntheticPick(brd botpolicy.Board, d *decision.Decision, objs []state.ObjID, modes []string) state.ObjID {
	syn := decision.Decision{Seq: d.Seq, Player: d.Player, Kind: decision.KPriority, Min: 1, Max: 1}
	for i, id := range objs {
		m := ""
		if modes != nil {
			m = modes[i]
		}
		syn.Options = append(syn.Options, decision.Option{Index: len(syn.Options), Kind: "cast", Obj: id, Mode: m})
	}
	syn.Options = append(syn.Options, decision.Option{Index: len(syn.Options), Kind: "pass"})
	in, err := gs.cf.DecideBoard(context.Background(), brd, syn)
	if err != nil || len(in.Choices) != 1 {
		return 0
	}
	o, ok := optionAt(&syn, in.Choices[0])
	if !ok || o.Kind != "cast" {
		return 0
	}
	return o.Obj
}

// unplannedReason classifies why a potential cast carries no V1 plan.
func (gs *gameState) unplannedReason(p state.PlayerID, pa decision.PotentialAction) string {
	o := gs.e.G.Obj(pa.Obj)
	if o == nil || o.Face() == nil {
		return "unknown"
	}
	if o.Zone != state.ZHand {
		return "origin:" + zoneName(o.Zone)
	}
	if pa.Mode != "" {
		return "mode:" + pa.Mode
	}
	out := gs.e.PlanCastPayment(p, decision.PlannedCast{Object: pa.Obj, Face: 0, Origin: "hand"})
	switch out.Reason {
	case "unsupported":
		mc := o.Face().ManaCost
		switch {
		case strings.Contains(mc, "X"):
			return "unsupported:X"
		case strings.Contains(mc, "/"):
			return "unsupported:hybrid-phyrexian"
		case strings.Contains(mc, "S"):
			return "unsupported:snow"
		}
		return "unsupported:shape-or-board"
	case "insufficient":
		return "insufficient:v1-sources"
	case "search_limit":
		return "search_limit"
	case "":
		if out.Plan != nil {
			return "planned-now" // raced: plan exists (should not happen)
		}
	}
	return "reason:" + out.Reason
}

func zoneName(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "hand"
	case state.ZCommand:
		return "command"
	case state.ZGraveyard:
		return "graveyard"
	case state.ZExile:
		return "exile"
	case state.ZBattlefield:
		return "battlefield"
	case state.ZLibrary:
		return "library"
	}
	return fmt.Sprintf("zone%d", z)
}

func (gs *gameState) checkPending() {
	if len(gs.pending) == 0 {
		return
	}
	g := gs.e.G
	kept := gs.pending[:0]
	for _, ps := range gs.pending {
		if g.Turn > ps.turn && int(g.Active) == ps.seat {
			a := gs.arm[ps.seat]
			if o := g.Obj(ps.b); o != nil && o.Zone == state.ZHand {
				gs.res.add(a, "d:stranded_still_in_hand_next_own_turn", 1)
			} else {
				gs.res.add(a, "d:stranded_left_hand_by_next_own_turn", 1)
			}
			continue
		}
		kept = append(kept, ps)
	}
	gs.pending = kept
}

func (gs *gameState) observe(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board) {
	e := gs.e
	a := gs.arm[seatIdx]
	r := gs.res
	r.add(a, "intents", 1)
	gs.checkPending()
	if d.Kind != decision.KPriority {
		r.add(a, "dec:"+string(d.Kind), 1)
		return
	}
	p := d.Player
	r.add(a, "priority", 1)
	plans := 0
	planned := map[state.ObjID]decision.PaymentAction{}
	for _, pa := range d.PaymentActions {
		if len(pa.Plans) > 0 {
			plans++
			planned[pa.Cast.Object] = pa
		}
	}
	if plans > 0 {
		gs.addDeck(seatIdx, "priority_with_plans")
	}
	kind, opt := answerKind(d, in)
	win := gs.window(d)
	r.add(a, "ans:"+kind, 1)
	if plans > 0 {
		r.add(a, "ans_with_plans:"+kind, 1)
	} else {
		r.add(a, "ans_no_plans:"+kind, 1)
	}
	switch kind {
	case "ability":
		if manaCost(opt.Cost) {
			r.add(a, "ans:ability_manacost", 1)
		}
		if opt.Attach {
			r.add(a, "ans:ability_attach", 1)
		}
	case "payment", "cast":
		r.add(a, "castwin:"+win, 1)
		if e.G.Active == p && !e.G.Step.IsMain() {
			gs.addDeck(seatIdx, "cast_own_turn_non_main")
			obj := opt.Obj
			if in.Payment != nil {
				for _, pa := range d.PaymentActions {
					if pa.ID == in.Payment.ActionID {
						obj = pa.Cast.Object
					}
				}
			}
			gs.ex("e:own_turn_non_main_cast:"+gs.deck[seatIdx], "%s cast %s (%s)", armNames[a], gs.name(obj), kind)
		}
	}
	if kind == "ability" && manaCost(opt.Cost) {
		gs.addDeck(seatIdx, "ability_manacost_activations")
	}
	// Per-turn mode sequence (task c).
	mk := [2]int32{int32(seatIdx), e.G.Turn}
	tm := gs.modes[mk]
	if tm == nil {
		tm = &turnMode{}
		gs.modes[mk] = tm
	}
	switch kind {
	case "payment":
		tm.seq = append(tm.seq, 'P')
	case "activate":
		tm.seq = append(tm.seq, 'M')
	case "cast":
		tm.seq = append(tm.seq, 'L')
	}

	// Board-size guard: a token explosion (ulalek-eldrazi reaches 70k+
	// objects) makes every pure-read audit below cost seconds per decision.
	// The game itself is untouched; the decision is only counted as skipped.
	if gs.maxObjs > 0 && len(e.G.Objs) > gs.maxObjs {
		r.add(a, "skipped_big_board_decisions", 1)
		return
	}
	ownMainEmpty := e.G.Active == p && e.G.Step.IsMain() && len(e.G.Stack) == 0

	// Task (e): mana-costed abilities the potential walk reaches but the
	// pool-priced offer withholds, in the seat's own empty-stack main phase.
	var pot []decision.PotentialAction
	potDone := false
	potential := func() []decision.PotentialAction {
		if !potDone {
			pot = e.PotentialActions(p)
			potDone = true
		}
		return pot
	}
	if ownMainEmpty {
		unoffered := 0
		for _, pa := range potential() {
			if pa.Kind != "ability" {
				continue
			}
			found := false
			for _, o := range d.Options {
				if o.Kind == "ability" && o.Obj == pa.Obj && o.Ability == pa.Ability {
					found = true
					break
				}
			}
			if !found {
				unoffered++
			}
		}
		r.add(a, "e:own_main_empty_decisions", 1)
		if unoffered > 0 {
			r.add(a, "e:own_main_empty_with_unoffered_ability", 1)
		}
	}

	if a == armAuto && plans > 0 {
		gs.observeAutoPlanDecision(seatIdx, d, in, brd, kind, planned, potential, win)
	}
	if a == armAuto && plans == 0 && kind == "activate" {
		r.add(a, "c:fallback_manual_tap", 1)
	}

	// Task (e): the adapter keeps a legacy cast option (any mode) in place of
	// the plan-only entry for the same object, then maps the policy's pick of
	// that legacy option back to the ORDINARY cast's plan. A non-ordinary
	// legacy option (alternative cost, evoke, pitch, ...) is therefore
	// replaced by the hard-cast plan.
	if a == armAuto && kind == "payment" {
		var obj state.ObjID
		for _, pa := range d.PaymentActions {
			if pa.ID == in.Payment.ActionID {
				obj = pa.Cast.Object
			}
		}
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj == obj && (o.Mode != "" || o.AltCostIndex != 0) {
				// The candidate kept only the legacy options for obj; if every
				// legacy option for obj is non-ordinary, the policy's pick
				// was one of them.
				ordinary := false
				for _, q := range d.Options {
					if q.Kind == "cast" && q.Obj == obj && q.Mode == "" && q.AltCostIndex == 0 {
						ordinary = true
					}
				}
				if !ordinary {
					gs.addDeck(seatIdx, "e:mode_substituted_by_plan")
					gs.ex("e:mode_substituted_by_plan", "policy picked %s (mode %q alt %d); adapter submitted the ordinary plan", gs.name(obj), o.Mode, o.AltCostIndex)
				}
				break
			}
		}
	}
	// Task (d): planned cast by the auto arm.
	if a == armAuto && kind == "payment" {
		gs.observeAutoPair(seatIdx, d, in, brd, planned)
	}
	// Task (d): manual arm tap sequence and legacy cast.
	if a == armManual {
		gs.observeManualPair(seatIdx, d, in, brd, kind, opt)
	}
}

func (gs *gameState) observeAutoPlanDecision(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board,
	kind string, planned map[state.ObjID]decision.PaymentAction, potential func() []decision.PotentialAction, win string) {
	e := gs.e
	r := gs.res
	a := armAuto
	p := d.Player
	// Task (a): every "activate" the adapter hides.
	v1 := e.AuditV1SourceIDs(p)
	dropped := 0
	for _, o := range d.Options {
		if o.Kind != "activate" {
			continue
		}
		dropped++
		r.add(a, "a:dropped_activate", 1)
		apis, costs := e.AuditManaAbilityAPIs(p, o.Obj)
		nonMana := false
		for _, api := range apis {
			switch api {
			case "Mana", "ManaReflected", "intrinsic":
			default:
				nonMana = true
			}
			r.add(a, "a:dropped_api:"+api, 1)
		}
		if nonMana {
			gs.ex("a:non_mana_activate", "%s apis=%v", gs.name(o.Obj), apis)
		}
		if o.Cost != "" {
			r.add(a, "a:dropped_costly", 1)
			gs.ex("a:dropped_costly", "%s cost=%q costs=%v", gs.name(o.Obj), o.Cost, costs)
		}
		if ob := e.G.Obj(o.Obj); ob != nil && ob.Zone != state.ZBattlefield {
			r.add(a, "a:dropped_zone:"+zoneName(ob.Zone), 1)
		}
		if !v1[o.Obj] {
			r.add(a, "a:dropped_non_v1_source", 1)
			gs.ex("a:dropped_non_v1_source", "%s cost=%q apis=%v", gs.name(o.Obj), o.Cost, apis)
		}
	}
	if dropped > 0 {
		r.add(a, "a:decisions_with_dropped_activate", 1)
	}
	// Counterfactual: the SAME policy on the full, unfiltered decision.
	cfIn, err := gs.cf.DecideBoard(context.Background(), *brd, *d)
	cfKind := "err"
	if err == nil {
		cfKind, _ = answerKind(d, cfIn)
	}
	r.add(a, "a:cf_manual:"+cfKind, 1)
	r.add(a, "a:cf_manual:"+cfKind+"|auto:"+kind, 1)

	// Task (b): castable-with-potential-mana spells that have no plan.
	var unplanned []decision.PotentialAction
	seen := map[state.ObjID]bool{}
	var allObjs []state.ObjID
	var allModes []string
	for _, pa := range potential() {
		if pa.Kind != "cast" {
			continue
		}
		allObjs = append(allObjs, pa.Obj)
		allModes = append(allModes, pa.Mode)
		if _, ok := planned[pa.Obj]; ok && pa.Mode == "" {
			continue
		}
		if _, ok := planned[pa.Obj]; ok {
			continue // an alternate mode of a card that is planned anyway
		}
		if seen[pa.Obj] {
			continue
		}
		seen[pa.Obj] = true
		unplanned = append(unplanned, pa)
	}
	if len(unplanned) == 0 {
		return
	}
	r.add(a, "b:plan_decisions_with_unplanned_castable", 1)
	reasonOf := map[state.ObjID]string{}
	for _, pa := range unplanned {
		rs := gs.unplannedReason(p, pa)
		reasonOf[pa.Obj] = rs
		r.add(a, "b:unplanned_reason:"+rs, 1)
	}
	pick := gs.syntheticPick(*brd, d, allObjs, allModes)
	if pick == 0 {
		return
	}
	if _, ok := planned[pick]; ok {
		return
	}
	rs := reasonOf[pick]
	gs.addDeck(seatIdx, "b:preferred_is_unplanned")
	r.add(a, "b:preferred_is_unplanned|auto:"+kind, 1)
	r.add(a, "b:preferred_unplanned_reason:"+rs, 1)
	if kind == "pass" {
		gs.addDeck(seatIdx, "b:starved_pass")
		r.add(a, "b:starved_pass_win:"+win, 1)
		gs.ex("b:starved_pass", "preferred %s (%s) unplanned; auto passed; planned=%v cf=%s", gs.name(pick), rs, gs.names(planned), cfKind)
	} else {
		gs.ex("b:diverted", "preferred %s (%s) unplanned; auto %s", gs.name(pick), rs, kind)
	}
}

func (gs *gameState) names(m map[state.ObjID]decision.PaymentAction) []string {
	var out []string
	for id := range m {
		out = append(out, gs.name(id))
	}
	sort.Strings(out)
	return out
}

func (gs *gameState) observeAutoPair(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board, planned map[state.ObjID]decision.PaymentAction) {
	e := gs.e
	r := gs.res
	p := d.Player
	var act decision.PaymentAction
	for _, pa := range d.PaymentActions {
		if pa.ID == in.Payment.ActionID {
			act = pa
		}
	}
	A := act.Cast.Object
	var chosen []state.ObjID
	for _, x := range in.Payment.Plan.Activations {
		chosen = append(chosen, x.Source)
	}
	r.add(armAuto, "d:planned_casts", 1)
	var others []state.ObjID
	for id := range planned {
		if id != A {
			others = append(others, id)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i] < others[j] })
	if len(others) == 0 {
		return
	}
	r.add(armAuto, "d:planned_casts_with_other_planned", 1)
	next := gs.syntheticPick(*brd, d, others, nil)
	stranded := false
	for _, b := range others {
		res := e.AuditPlanPair(p, A, b, chosen, manaOf(in.Payment.Plan.PoolAfter))
		r.add(armAuto, "d:pairs_checked", 1)
		if !res.Known {
			r.add(armAuto, "d:pairs_unknown", 1)
			continue
		}
		if res.Joint {
			r.add(armAuto, "d:pairs_joint", 1)
		}
		if res.AfterChosen {
			r.add(armAuto, "d:pairs_after_ok", 1)
		}
		if res.Joint && !res.AfterChosen {
			stranded = true
			gs.addDeck(seatIdx, "d:pairs_stranded")
			if b == next {
				r.add(armAuto, "d:pairs_stranded_is_next_pick", 1)
				gs.ex("d:auto_stranded_next", "cast %s via %v; stranded %s (joint payable)", gs.name(A), gs.srcNames(chosen), gs.name(b))
			}
			gs.pending = append(gs.pending, pendingStrand{seat: seatIdx, b: b, turn: e.G.Turn})
		}
	}
	if stranded {
		r.add(armAuto, "d:casts_with_stranding", 1)
	}
}

func (gs *gameState) srcNames(ids []state.ObjID) []string {
	var out []string
	for _, id := range ids {
		out = append(out, gs.name(id))
	}
	return out
}

func (gs *gameState) observeManualPair(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board, kind string, opt decision.Option) {
	e := gs.e
	r := gs.res
	p := d.Player
	g := e.G
	if kind == "activate" && g.Players[p].Pool.Total() == 0 {
		if gs.snap[seatIdx] == nil || gs.snapTurn[seatIdx] != g.Turn || gs.snapStep[seatIdx] != g.Step {
			gs.snap[seatIdx] = e.Clone()
			gs.snapTurn[seatIdx] = g.Turn
			gs.snapStep[seatIdx] = g.Step
		}
		return
	}
	if kind != "cast" {
		return
	}
	snap := gs.snap[seatIdx]
	if snap == nil || gs.snapTurn[seatIdx] != g.Turn || gs.snapStep[seatIdx] != g.Step {
		return
	}
	gs.snap[seatIdx] = nil
	A := opt.Obj
	if opt.Mode != "" || opt.AltCostIndex != 0 {
		return
	}
	if snap.PlanCastPayment(p, decision.PlannedCast{Object: A, Face: 0, Origin: "hand"}).Plan == nil {
		return
	}
	r.add(armManual, "d:planned_casts", 1) // legacy cast of a V1-plannable card after a tap sequence
	var others []state.ObjID
	for _, id := range snap.G.Zone(state.ZHand, p) {
		if id == A {
			continue
		}
		if snap.PlanCastPayment(p, decision.PlannedCast{Object: id, Face: 0, Origin: "hand"}).Plan != nil {
			others = append(others, id)
		}
	}
	if len(others) == 0 {
		return
	}
	r.add(armManual, "d:planned_casts_with_other_planned", 1)
	next := gs.syntheticPick(*brd, d, others, nil)
	stranded := false
	for _, b := range others {
		joint, k1 := snap.AuditJointNow(p, A, b)
		after, k2 := e.AuditAfterPoolCast(p, A, b)
		r.add(armManual, "d:pairs_checked", 1)
		if !k1 || !k2 {
			r.add(armManual, "d:pairs_unknown", 1)
			continue
		}
		if joint {
			r.add(armManual, "d:pairs_joint", 1)
		}
		if after {
			r.add(armManual, "d:pairs_after_ok", 1)
		}
		if joint && !after {
			stranded = true
			gs.addDeck(seatIdx, "d:pairs_stranded")
			if b == next {
				r.add(armManual, "d:pairs_stranded_is_next_pick", 1)
				gs.ex("d:manual_stranded_next", "cast %s after taps; stranded %s (joint payable)", gs.name(A), gs.name(b))
			}
			gs.pending = append(gs.pending, pendingStrand{seat: seatIdx, b: b, turn: g.Turn})
		}
	}
	if stranded {
		r.add(armManual, "d:casts_with_stranding", 1)
	}
}

// finish folds the log-derived facts: spells cast by window and mana value,
// mana produced/spent/lost per seat, and the per-turn mode sequences.
func (gs *gameState) finish(o bench.Outcome, e *rules.Engine) {
	r := gs.res
	for s := 0; s < 2; s++ {
		a := gs.arm[s]
		r.add(a, "games", 1)
		r.add(a, "turns", int64(o.Turns))
		switch {
		case o.IsStalled():
			r.add(a, "stalls", 1)
			r.add(a, "stall:"+o.StallOn, 1)
		case o.Draw:
			r.add(a, "draws", 1)
		case o.WinnerSeat == s:
			r.add(a, "wins", 1)
		default:
			r.add(a, "losses", 1)
		}
	}
	if e == nil {
		return
	}
	var pool [2]int64
	active := state.PlayerID(0)
	step := state.StepUntap
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.TurnChange:
			active = ev.Player
		case events.StepChange:
			step = ev.Step
		case events.ManaAdd:
			if int(ev.Player) < 2 {
				a := gs.arm[ev.Player]
				pool[ev.Player] += int64(ev.Amount)
				if ev.Amount > 0 {
					r.add(a, "mana:produced", int64(ev.Amount))
				} else {
					r.add(a, "mana:spent", int64(-ev.Amount))
				}
			}
		case events.ManaClear:
			if int(ev.Player) < 2 {
				a := gs.arm[ev.Player]
				if pool[ev.Player] > 0 {
					r.add(a, "mana:lost_at_step_end", pool[ev.Player])
					who := "opp"
					if ev.Player == active {
						who = "own"
					}
					r.add(a, "mana:lost_"+who, pool[ev.Player])
				}
				pool[ev.Player] = 0
			}
		case events.PutOnStack:
			if int(ev.Player) < 2 {
				a := gs.arm[ev.Player]
				who := "opp"
				if ev.Player == active {
					who = "own"
				}
				r.add(a, "spells_cast", 1)
				r.add(a, "spellwin:"+who+":"+step.String(), 1)
				if ob := e.G.Obj(ev.Obj); ob != nil && ob.Card != nil && len(ob.Card.Faces) > 0 && ob.Card.Faces[0] != nil {
					r.add(a, "spells_mv", int64(botpolicy.CmcOf(ob.Card.Faces[0].ManaCost)))
				}
			}
		case events.LandPlayed:
			if int(ev.Player) < 2 {
				r.add(gs.arm[ev.Player], "lands_played", 1)
			}
		case events.AbilityPush:
			if int(ev.Player) < 2 {
				r.add(gs.arm[ev.Player], "abilities_activated", 1)
			}
		}
	}
	// First command-zone cast turn: rescan with the turn counter.
	turn := int32(0)
	done := [2]bool{}
	for _, ev := range e.L.Events {
		if ev.Kind == events.TurnChange {
			turn = ev.Amount
		}
		if ev.Kind == events.PutOnStack && ev.From == state.ZCommand && int(ev.Player) < 2 && !done[ev.Player] {
			done[ev.Player] = true
			a := gs.arm[ev.Player]
			r.add(a, "cmd:first_cast_turn_sum", int64(turn))
			r.add(a, "cmd:first_cast_n", 1)
		}
	}
	for s := 0; s < 2; s++ {
		if e.G.Players[s].Commanders != nil && !done[s] {
			r.add(gs.arm[s], "cmd:never_cast", 1)
		}
	}
	// Mode sequences (task c): deterministic order of keys.
	keys := make([][2]int32, 0, len(gs.modes))
	for k := range gs.modes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	for _, k := range keys {
		seq := gs.modes[k].seq
		a := gs.arm[k[0]]
		var hasP, hasM bool
		switches := 0
		last := byte(0)
		for _, c := range seq {
			if c == 'P' {
				hasP = true
			}
			if c == 'M' {
				hasM = true
			}
			if c == 'P' || c == 'M' {
				if last != 0 && last != c {
					switches++
				}
				last = c
			}
		}
		r.add(a, "c:turns_observed", 1)
		if hasP && hasM {
			r.add(a, "c:turns_mixed_plan_and_manual_tap", 1)
		}
		r.add(a, "c:mode_switches", int64(switches))
	}
}

func main() {
	os.Exit(mainExit(os.Args[1:], os.Stdout, os.Stderr))
}

func mainExit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("autopayaudit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pairsSpec := fs.String("pairs", "all", "deck pairs: all or a:b,c:d")
	games := fs.Int("games", 10, "games per pair")
	seed := fs.Uint64("seed", 5400000, "base seed (dev range; keep clear of held-out [1000000,2000000))")
	format := fs.String("format", "constructed", "constructed or commander")
	mode := fs.String("mode", "auto-vs-manual", "auto-vs-manual, auto-mirror or manual-mirror")
	workers := fs.Int("workers", 6, "parallel games")
	maxTurns := fs.Int("max-turns", 200, "turn cap")
	maxIntents := fs.Int("max-intents", 20000, "intent cap")
	dir := fs.String("dir", ".cards", "corpus")
	out := fs.String("out", "", "write JSON result here")
	maxEx := fs.Int("examples", 3, "examples kept per category per game")
	verify := fs.Bool("verify", false, "replay every game without hooks and require the identical chain head (proves observation is pure)")
	maxObjs := fs.Int("audit-max-objects", 0, "skip every per-decision audit (counted) once the game holds more objects than this; 0 = no cap")
	maxUnits := fs.Int("audit-max-units", 0, "skip the (d) hand-pair audit (counted unknown) when the board has more V1 source units than this; 0 = no cap")
	logReplay := fs.Bool("log-replay", false, "replay every finished game's log through replay.Replay (the host restart/feedback path) and require the identical head")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rules.AuditMaxUnits = *maxUnits
	reg, err := testutil.OpenCorpusRegistry(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	commander := *format == "commander"
	names := testutil.RepoDeckNames()
	if commander {
		var cmd []string
		for _, n := range names {
			f, err := testutil.LoadRepoDeckFile(n)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if len(f.CommanderNames()) > 0 {
				cmd = append(cmd, n)
			}
		}
		names = cmd
	}
	pairs, err := bench.ParsePairs(*pairsSpec, names)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	decks := map[string][]*cards.Card{}
	cmdrs := map[string][]int{}
	for _, pd := range pairs {
		for _, n := range []string{pd.A, pd.B} {
			if _, ok := decks[n]; ok {
				continue
			}
			d, err := testutil.LoadRepoDeck(reg, n)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			decks[n] = d
			if commander {
				f, _ := testutil.LoadRepoDeckFile(n)
				if verr := f.ValidateCommander(reg); verr != nil {
					fmt.Fprintln(stderr, verr)
					return 1
				}
				cmdrs[n] = f.CommanderIndices()
			}
		}
	}
	aCtor, bCtor := bench.SeatCtor(autoCtor), bench.SeatCtor(manualCtor)
	switch *mode {
	case "auto-vs-manual":
	case "auto-mirror":
		bCtor = autoCtor
	case "manual-mirror":
		aCtor = manualCtor
	default:
		fmt.Fprintln(stderr, "unknown -mode", *mode)
		return 2
	}
	results := make([][]*gameResult, len(pairs))
	for i := range results {
		results[i] = make([]*gameResult, *games)
	}
	var mu sync.Mutex
	play := func(pos int, s uint64, g int, seats [2]seat.Seat) (bench.Outcome, error) {
		pd := pairs[pos]
		cfg := rules.Config{Seed: s, Names: []string{pd.A, pd.B}, Decks: [][]*cards.Card{decks[pd.A], decks[pd.B]}}
		if commander {
			cfg.Format = rules.FormatCommander
			cfg.StartingLife = 40
			cfg.Commanders = [][]int{cmdrs[pd.A], cmdrs[pd.B]}
		}
		cfg.Tokens = reg.Tokens
		cfg.NameUniverse = reg.AllCards()
		res := &gameResult{counts: [2]map[string]int64{{}, {}}}
		gs := &gameState{res: res, cf: seat.NewBot(0), pairID: pd.String(), seed: s, deck: [2]string{pd.A, pd.B},
			modes: map[[2]int32]*turnMode{}, maxEx: *maxEx, maxObjs: *maxObjs, exCount: map[string]int{}}
		for i := 0; i < 2; i++ {
			gs.arm[i] = armManual
			if l, ok := seats[i].(*labeled); ok && l.auto {
				gs.arm[i] = armAuto
			}
		}
		hooks := bench.Hooks{
			Setup: func(e *rules.Engine) { gs.e = e },
			Decision: func(seatIdx int, d *decision.Decision, in decision.Intent, brd *botpolicy.Board) error {
				if brd != nil {
					gs.observe(seatIdx, d, in, brd)
				}
				return nil
			},
		}
		o, e, err := bench.PlayGame(cfg, seats[:], *maxTurns, *maxIntents, hooks)
		if err != nil {
			return o, err
		}
		gs.finish(o, e)
		if *logReplay && e != nil {
			res.add(armAuto, "logreplay:games", 1)
			re, rerr := replay.Replay(e.L.Clone(), cfg)
			if rerr != nil {
				res.add(armAuto, "logreplay:ERROR", 1)
				gs.res.examples = append(gs.res.examples, example{Cat: "logreplay:ERROR", Text: fmt.Sprintf("[%s seed %d] %v", pd.String(), s, rerr)})
			} else if re.L.Head() != e.L.Head() {
				res.add(armAuto, "logreplay:MISMATCH", 1)
			}
		}
		if *verify {
			var fresh [2]seat.Seat
			for idx := 0; idx < 2; idx++ {
				if bench.PlaysSeat(g, idx) {
					fresh[idx] = aCtor(s ^ uint64(idx+1))
				} else {
					fresh[idx] = bCtor(s ^ uint64(idx+1))
				}
			}
			o2, e2, err := bench.PlayGame(cfg, fresh[:], *maxTurns, *maxIntents, bench.Hooks{})
			if err != nil {
				return o, err
			}
			res.add(armAuto, "verify:games", 1)
			if e2.L.Head() != e.L.Head() || o2 != o {
				res.add(armAuto, "verify:MISMATCH", 1)
			}
		}
		mu.Lock()
		results[pos][g] = res
		mu.Unlock()
		return o, nil
	}
	if _, err := bench.RunPairs(*seed, *games, pairs, aCtor, bCtor, play, *workers, nil); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// Deterministic fold in (pair, game) order.
	total := [2]map[string]int64{{}, {}}
	var exs []example
	exPerCat := map[string]int{}
	for _, row := range results {
		for _, res := range row {
			if res == nil {
				continue
			}
			for a := 0; a < 2; a++ {
				for k, v := range res.counts[a] {
					total[a][k] += v
				}
			}
			for _, x := range res.examples {
				if exPerCat[x.Cat] < 25 {
					exPerCat[x.Cat]++
					exs = append(exs, x)
				}
			}
		}
	}
	doc := map[string]any{
		"pairs": len(pairs), "games_per_pair": *games, "seed": *seed, "format": *format, "mode": *mode,
		"counts":   map[string]map[string]int64{armNames[0]: total[0], armNames[1]: total[1]},
		"examples": exs,
	}
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", " ")
		if err := enc.Encode(doc); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		f.Close()
	}
	for a := 0; a < 2; a++ {
		keys := make([]string, 0, len(total[a]))
		for k := range total[a] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(stdout, "== %s\n", armNames[a])
		for _, k := range keys {
			fmt.Fprintf(stdout, "%-60s %d\n", k, total[a][k])
		}
	}
	return 0
}
