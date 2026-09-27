package paymirror

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Answerer answers one follow-up decision of run A (a target, a mode, a
// trigger's target, a replacement order, ...). It must be deterministic for
// the check to be reproducible; the driver passes the live seats' bots.
type Answerer func(e *rules.Engine, d *decision.Decision) (decision.Intent, error)

// Route names one way a manual player can reproduce a planned payment.
type Route string

const (
	// RouteFloat activates every planned source through the ordinary
	// priority "activate" option (floating the mana), then chooses the
	// ordinary cast option and replays run A's follow-up answers. It is the
	// only manual route for a plan-only cast: the legacy offer gate prices a
	// cast against the floating pool, so such a cast is absent from Options
	// until its mana has been floated.
	RouteFloat Route = "float_then_cast"
	// RouteBase chooses Options[BaseOptionIndex] -- the legacy cast option
	// the action names -- and pays any remaining planned sources through the
	// cast's CR 601.2g mana window. It applies only when BaseOptionIndex is
	// set, and also proves that index names the planned cast.
	RouteBase Route = "base_option_window"
)

// Status is a route's verdict.
type Status string

const (
	Equivalent   Status = "equivalent"
	Mismatch     Status = "mismatch"
	Unmirrorable Status = "unmirrorable"
)

// Options tunes one check.
type Options struct {
	// MaxFollowUps bounds the decisions run A answers after the planned
	// submit before its cast transaction is declared unbounded (0 = 256).
	MaxFollowUps int
	// Trace records every event (decision events included) each side logged
	// since the fork, for a debugging run on one game.
	Trace bool
	// afterRoute is a test hook run on each mirror engine after its route
	// completes and before the comparison (the sensitivity test perturbs B).
	afterRoute func(b *rules.Engine)
	// Resolve extends every route that is equivalent at the cast boundary:
	// clones of both engines are driven with one deterministic passer (pass
	// priority, first-Min answers) until the planned spell has left the
	// stack, and compared again. A payment difference that only matters when
	// the spell resolves (a colour of mana spent, a cast-provenance read) is
	// then reported as reason "post_resolution".
	Resolve bool
	// Control (CheckLive only) replays run A's exact intent and recorded
	// answers on a pre-submit clone and compares that clone with the live
	// engine. A difference there is an Engine.Clone fidelity problem, not a
	// payment-route one; with Control set the routes are compared against
	// the control clone, so both sides of the route verdict are clones.
	Control bool
}

// Recorded is one follow-up decision run A answered, with the options it was
// offered, so a mirror can map the answer onto its own decision by option
// identity rather than by index.
type Recorded struct {
	Kind    decision.Kind     `json:"kind"`
	Player  state.PlayerID    `json:"player"`
	Prompt  string            `json:"prompt"`
	Min     int               `json:"min"`
	Max     int               `json:"max"`
	Options []decision.Option `json:"options"`
	Choices []int             `json:"choices"`
	Rest    []int             `json:"rest,omitempty"`
	// Fallback is the PaymentFallback reason this decision carried, if any.
	Fallback string `json:"fallback,omitempty"`
}

// RouteResult is one manual route's verdict against run A.
type RouteResult struct {
	Route  Route  `json:"route"`
	Status Status `json:"status"`
	// Reason is a stable class token ("state_differs", "cast_not_offered_after_float", ...).
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	Diffs  []Diff `json:"diffs,omitempty"`
	// EventsOnlyA/B are the route-independent events one side emitted since
	// the fork and the other did not (a multiset difference).
	EventsOnlyA []string `json:"events_only_a,omitempty"`
	EventsOnlyB []string `json:"events_only_b,omitempty"`
	// EventOrderDiffers reports that the game events, although the same
	// multiset, arrived in a different order (expected for RouteFloat).
	EventOrderDiffers bool `json:"event_order_differs,omitempty"`
	// ShapeNotes records follow-up decisions whose offered options differed
	// between A and the mirror although A's answer still mapped.
	ShapeNotes []string `json:"shape_notes,omitempty"`
	Signature  string   `json:"signature,omitempty"`
	// Invariant is the mirror's own at-rest violation (see Report.AInvariant).
	Invariant string `json:"invariant,omitempty"`
	// Panic is set when the mirror route crashed the engine.
	Panic string `json:"panic,omitempty"`
	// Events is the mirror's full event stream since the fork (Options.Trace).
	Events []string `json:"events,omitempty"`
	// Resolved records the post-resolution horizon (Options.Resolve): "" when
	// not run, "equivalent", "mismatch", or "skipped:<why>".
	Resolved string `json:"resolved,omitempty"`
}

// Report is the result of one mirror check of one planned cast.
type Report struct {
	Seq         uint64               `json:"seq"`
	Player      state.PlayerID       `json:"player"`
	Turn        int32                `json:"turn"`
	Card        string               `json:"card"`
	Object      state.ObjID          `json:"object"`
	Activations int                  `json:"activations"`
	BaseOption  bool                 `json:"base_option"`
	Plan        decision.PaymentPlan `json:"plan"`
	FollowUps   []Recorded           `json:"follow_ups,omitempty"`
	// AFallback is the PaymentFallback reason run A's cast fell back with.
	AFallback string `json:"a_fallback,omitempty"`
	// AOutcome is where the planned cast ended: "stack", "over", or the zone
	// the card stayed in (an aborted proposal).
	AOutcome string `json:"a_outcome,omitempty"`
	// AError is set when run A itself could not complete (rejected submit,
	// unbounded follow-ups, answerer error, panic). No route is compared.
	AError string `json:"a_error,omitempty"`
	// PreInvariant is the at-rest violation already present at the planned
	// submit (left by an earlier transaction in the same game).
	PreInvariant string `json:"pre_invariant,omitempty"`
	// AInvariant is set when run A reached its priority boundary with the
	// engine NOT at rest: a cast transaction still pending (rules.Engine.cast
	// non-nil) or a choose flow still armed (choosing != chooseNone). The
	// engine then offers priority in the middle of casting a spell.
	AInvariant string `json:"a_invariant,omitempty"`
	// AWitness is set when run A's actual production differs from the
	// selected witness: a planned source tapped for other mana than its
	// Produces, or a planned source never activated.
	AWitness string `json:"a_witness,omitempty"`
	// ASideEffects lists damage the planned sources dealt while producing
	// their mana (an Ancient Tomb-shaped producer): the witness names only
	// the tap and the mana, so this is life the plan spent unannounced.
	ASideEffects []string `json:"a_side_effects,omitempty"`
	// AEvents is run A's full event stream since the fork (Options.Trace).
	AEvents []string      `json:"a_events,omitempty"`
	Control *RouteResult  `json:"control,omitempty"`
	Routes  []RouteResult `json:"routes"`
}

// Verdict folds the routes into one per-cast verdict: equivalent if any
// route mirrored the cast equivalently, mismatch if a route mirrored it and
// ended in a different state, otherwise unmirrorable.
func (r *Report) Verdict() (Status, string) {
	if r.AError != "" {
		return Unmirrorable, "a_error:" + firstToken(r.AError)
	}
	// Run A's own contract violations are the root of whatever the routes
	// then disagree about, so they name the mismatch.
	if r.PreInvariant != "" {
		return Mismatch, "pre_invariant:" + r.PreInvariant
	}
	if r.AInvariant != "" {
		return Mismatch, "a_invariant:" + r.AInvariant
	}
	if r.AWitness != "" {
		return Mismatch, "a_witness:" + firstToken(r.AWitness)
	}
	var mismatch, unmirror string
	for _, rr := range r.Routes {
		switch rr.Status {
		case Equivalent:
			return Equivalent, ""
		case Mismatch:
			if mismatch == "" {
				mismatch = rr.Signature
			}
		case Unmirrorable:
			if unmirror == "" {
				unmirror = string(rr.Route) + ":" + rr.Reason
			}
		}
	}
	prefix := ""
	if r.AFallback != "" {
		prefix = "a_fallback=" + r.AFallback + "|"
	}
	if mismatch != "" {
		return Mismatch, prefix + mismatch
	}
	return Unmirrorable, prefix + unmirror
}

func firstToken(s string) string {
	if i := strings.IndexAny(s, ": "); i >= 0 {
		return s[:i]
	}
	return s
}

// Check mirrors the planned intent in on clones of e, leaving e untouched:
// run A submits in on a clone and answers its follow-ups with answer; each
// applicable manual route runs on its own clone taken before A's submit.
func Check(e *rules.Engine, in decision.Intent, answer Answerer, opt Options) *Report {
	return check(e, e.Clone(), in, answer, opt, false)
}

// CheckLive is Check with run A performed on e itself: e ends exactly where
// the planned cast leaves it (a driver continues its game from there), and
// only the manual routes (and the optional control) run on clones.
func CheckLive(e *rules.Engine, in decision.Intent, answer Answerer, opt Options) *Report {
	return check(e, e, in, answer, opt, opt.Control)
}

func check(base, a *rules.Engine, in decision.Intent, answer Answerer, opt Options, control bool) (rep *Report) {
	if opt.MaxFollowUps <= 0 {
		opt.MaxFollowUps = 256
	}
	d := base.Pending()
	rep = &Report{Turn: base.G.Turn}
	if d == nil || d.Kind != decision.KPriority || in.Payment == nil {
		rep.AError = "not_a_planned_priority_intent"
		return rep
	}
	rep.Seq, rep.Player = d.Seq, d.Player
	// A priority decision already posed mid-cast (an earlier corruption in
	// the same game) is not this plan's doing: record it and attribute.
	rep.PreInvariant = atRestViolation(base)
	var action *decision.PaymentAction
	for i := range d.PaymentActions {
		if d.PaymentActions[i].ID == in.Payment.ActionID {
			act := decision.ClonePaymentAction(d.PaymentActions[i])
			action = &act
			break
		}
	}
	if action == nil {
		rep.AError = "payment_action_not_offered"
		return rep
	}
	plan := decision.ClonePaymentPlan(in.Payment.Plan)
	rep.Plan, rep.Object = plan, action.Cast.Object
	rep.Activations = len(plan.Activations)
	rep.BaseOption = action.BaseOptionIndex != nil
	if o := base.G.Obj(action.Cast.Object); o != nil && o.Face() != nil {
		rep.Card = o.Face().Name
	}

	// Every mirror starts from the pre-submit position. In live mode a == base,
	// so these clones must be taken before A mutates it.
	fork := len(base.L.Events)
	bFloat := base.Clone()
	var bBase, ctrl *rules.Engine
	if action.BaseOptionIndex != nil {
		bBase = base.Clone()
	}
	if control {
		ctrl = base.Clone()
	}

	runA(a, in, answer, opt.MaxFollowUps, rep)
	if rep.AError != "" {
		return rep
	}
	rep.AInvariant = atRestViolation(a)
	rep.ASideEffects = sideEffects(a, fork, plan)
	if rep.AFallback == "" {
		// A plan that fell back to the manual window was abandoned by design;
		// what it tapped afterwards is the answerer's choice, not the witness.
		rep.AWitness = witnessViolation(a, fork, plan)
		if rep.AWitness == "" && rep.AInvariant == "" && rep.AOutcome == "stack" {
			// The witness also promises the pool left after payment.
			var got decision.ManaAmount
			for i, n := range a.G.Players[rep.Player].Pool {
				if n > 0 {
					got[i] = uint32(n)
				}
			}
			if got != plan.PoolAfter {
				rep.AWitness = fmt.Sprintf("pool_after: planned %s got %s", manaString(plan.PoolAfter), manaString(got))
			}
		}
	}
	ref := a
	if ctrl != nil {
		cr := RouteResult{Route: "control"}
		replayControl(ctrl, in, rep, &cr)
		if cr.Status == "" {
			// The control replayed run A completely, so the route verdicts
			// compare clone with clone; a live-vs-clone difference is then
			// reported only here, as Clone fidelity.
			ref = ctrl
			compareEngines(a, ctrl, fork, &cr)
		}
		cr.Signature = signature(&cr)
		rep.Control = &cr
	}
	rep.Routes = append(rep.Routes, runRoute(ref, bFloat, fork, rep, RouteFloat, action, opt.afterRoute))
	if bBase != nil {
		rep.Routes = append(rep.Routes, runRoute(ref, bBase, fork, rep, RouteBase, action, opt.afterRoute))
	}
	if opt.Resolve {
		for i, eng := range []*rules.Engine{bFloat, bBase} {
			if eng == nil || i >= len(rep.Routes) || rep.Routes[i].Status != Equivalent {
				continue
			}
			resolveHorizon(ref, eng, fork, rep, &rep.Routes[i])
		}
	}
	if opt.Trace {
		rep.AEvents = traceEvents(a, fork)
		for i, eng := range []*rules.Engine{bFloat, bBase} {
			if eng != nil && i < len(rep.Routes) {
				rep.Routes[i].Events = traceEvents(eng, fork)
			}
		}
	}
	return rep
}

func traceEvents(e *rules.Engine, fork int) []string {
	var out []string
	for _, ev := range e.L.Events[fork:] {
		out = append(out, strconv.FormatUint(ev.Seq, 10)+" "+eventKey(ev))
	}
	return out
}

func runA(a *rules.Engine, in decision.Intent, answer Answerer, max int, rep *Report) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*rules.LivelockError); ok {
				panic(r) // the driver owns livelock handling
			}
			rep.AError = fmt.Sprintf("panic: %v", r)
		}
	}()
	if err := a.Submit(decision.CloneIntent(in)); err != nil {
		rep.AError = "submit_rejected: " + err.Error()
		return
	}
	for n := 0; ; n++ {
		if a.G.Over {
			rep.AOutcome = "over"
			return
		}
		d := a.Pending()
		if d == nil {
			rep.AError = "no_pending_decision"
			return
		}
		if d.Kind == decision.KPriority {
			break
		}
		if n >= max {
			rep.AError = "unbounded_follow_ups"
			return
		}
		fallback := ""
		if d.PaymentFallback != nil {
			fallback = d.PaymentFallback.Reason
			if rep.AFallback == "" {
				rep.AFallback = fallback
			}
		}
		ans, err := answer(a, d)
		if err != nil {
			rep.AError = "answer_error: " + err.Error()
			return
		}
		rep.FollowUps = append(rep.FollowUps, Recorded{Kind: d.Kind, Player: d.Player, Prompt: d.Prompt,
			Min: d.Min, Max: d.Max, Options: append([]decision.Option(nil), d.Options...),
			Choices: append([]int(nil), ans.Choices...), Rest: append([]int(nil), ans.Rest...), Fallback: fallback})
		if err := a.Submit(ans); err != nil {
			rep.AError = "follow_up_rejected: " + err.Error()
			return
		}
	}
	rep.AOutcome = zoneOutcome(a, rep.Object)
}

func zoneOutcome(e *rules.Engine, id state.ObjID) string {
	if e.G.Over {
		return "over"
	}
	o := e.G.Obj(id)
	if o == nil {
		return "gone"
	}
	if o.Zone == state.ZStack {
		return "stack"
	}
	return "stayed_" + o.Zone.String()
}

// replayControl re-submits run A's own intent and recorded answers on a
// pre-submit clone. It is a pure determinism/Clone-fidelity control.
func replayControl(c *rules.Engine, in decision.Intent, rep *Report, res *RouteResult) {
	defer recoverRoute(res)
	d := c.Pending()
	in = decision.CloneIntent(in)
	in.Seq = d.Seq
	if err := c.Submit(in); err != nil {
		setUnmirrorable(res, "control_submit_rejected", err.Error())
		return
	}
	replayFollowUps(c, rep, nil, res)
}

func recoverRoute(res *RouteResult) {
	if r := recover(); r != nil {
		if _, ok := r.(*rules.LivelockError); ok {
			res.Status, res.Reason, res.Panic = Mismatch, "livelock", fmt.Sprint(r)
			return
		}
		res.Status, res.Reason, res.Panic = Mismatch, "panic", fmt.Sprint(r)
	}
}

func setUnmirrorable(res *RouteResult, reason, detail string) {
	if res.Status == "" {
		res.Status, res.Reason, res.Detail = Unmirrorable, reason, detail
	}
}

func setMismatch(res *RouteResult, reason, detail string) {
	if res.Status == "" {
		res.Status, res.Reason, res.Detail = Mismatch, reason, detail
	}
}

func runRoute(ref, b *rules.Engine, fork int, rep *Report, route Route, action *decision.PaymentAction, after func(*rules.Engine)) (res RouteResult) {
	res.Route = route
	func() {
		defer recoverRoute(&res)
		switch route {
		case RouteFloat:
			mirrorFloat(b, rep, &res)
		case RouteBase:
			mirrorBase(b, rep, action, &res)
		}
		if after != nil && res.Status == "" {
			after(b)
		}
	}()
	if res.Panic == "" {
		res.Invariant = atRestViolation(b)
		switch {
		case res.Status == "" && ref.G.Players[rep.Player].Lost && b.G.Players[rep.Player].Lost && !(ref.G.Over && b.G.Over):
			// The caster died inside the transaction on both sides (a
			// self-damaging planned source at low life): the manual player
			// dies on the first such activation, the planned cast only at the
			// post-cast state-based check. The outcome is the same.
			res.Status = Equivalent
			res.ShapeNotes = append(res.ShapeNotes, "caster lost on both sides")
		case res.Status == "" && ref.G.Over && b.G.Over:
			// Both games ended inside the transaction (a lethal self-damage
			// producer): the outcome, not the frozen board, is what a manual
			// player reproduces.
			if outcome(ref) == outcome(b) {
				res.Status = Equivalent
				res.ShapeNotes = append(res.ShapeNotes, "both games over: "+outcome(b))
			} else {
				setMismatch(&res, "game_outcome_differs", outcome(ref)+" vs "+outcome(b))
			}
		case res.Status == "":
			compareEngines(ref, b, fork, &res)
		case res.Status == Mismatch:
			// A decision-sequence mismatch still carries the state evidence.
			collectDiffs(ref, b, fork, &res)
		}
	}
	res.Signature = signature(&res)
	return res
}

// mirrorFloat is RouteFloat: activate each planned source at priority with
// the witness's production, then cast through the ordinary option.
func mirrorFloat(b *rules.Engine, rep *Report, res *RouteResult) {
	p := rep.Player
	for i, act := range rep.Plan.Activations {
		if b.G.Over || b.G.Players[p].Lost {
			return // the game (or the caster) ended while floating; compare
		}
		d := b.Pending()
		if d == nil || d.Kind != decision.KPriority || d.Player != p {
			setUnmirrorable(res, "not_at_priority", fmt.Sprintf("activation %d", i))
			return
		}
		idx := findOption(d, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == act.Source })
		if idx < 0 {
			setUnmirrorable(res, "no_priority_activate_option", fmt.Sprintf("source %d (%s)", act.Source, objName(b, act.Source)))
			return
		}
		before := b.G.Players[p].Pool
		if err := submitChoices(b, d, []int{idx}); err != nil {
			setUnmirrorable(res, "activate_rejected", err.Error())
			return
		}
		if reason := answerManaAsks(b, p, act); reason != "" {
			setUnmirrorable(res, reason, fmt.Sprintf("source %d (%s)", act.Source, objName(b, act.Source)))
			return
		}
		if b.G.Over || b.G.Players[p].Lost {
			return
		}
		d = b.Pending()
		if d == nil || d.Kind != decision.KPriority || d.Player != p {
			kind := "none"
			if d != nil {
				kind = string(d.Kind) + ":" + d.Prompt
			}
			setUnmirrorable(res, "activation_left_priority", kind)
			return
		}
		got := manaDelta(b.G.Players[p].Pool, before)
		if got != act.Produces {
			setMismatch(res, "production_differs", fmt.Sprintf("source %d (%s): planned %v, manual %v",
				act.Source, objName(b, act.Source), act.Produces, got))
			return
		}
	}
	if b.G.Over || b.G.Players[p].Lost {
		return
	}
	d := b.Pending()
	idx := findOption(d, func(o decision.Option) bool {
		return o.Kind == "cast" && o.Obj == rep.Object && o.Mode == "" && o.AltCostIndex == 0
	})
	if idx < 0 {
		why := fmt.Sprintf("pool %v", b.G.Players[p].Pool)
		if n := len(b.G.Stack); n > 0 {
			// Floating at priority let a mana ability's trigger (or its
			// consequence) reach the stack before the cast, which a
			// sorcery-speed spell cannot be cast over.
			why = fmt.Sprintf("stack holds %d object(s) after floating; %s", n, why)
			setUnmirrorable(res, "cast_blocked_by_float_trigger", why)
			return
		}
		setUnmirrorable(res, "cast_not_offered_after_float", why)
		return
	}
	if err := submitChoices(b, d, []int{idx}); err != nil {
		setUnmirrorable(res, "cast_rejected", err.Error())
		return
	}
	replayFollowUps(b, rep, nil, res)
}

// mirrorBase is RouteBase: choose Options[BaseOptionIndex] and pay any
// planned sources in the cast's own CR 601.2g window.
func mirrorBase(b *rules.Engine, rep *Report, action *decision.PaymentAction, res *RouteResult) {
	d := b.Pending()
	i := *action.BaseOptionIndex
	if i < 0 || i >= len(d.Options) {
		setMismatch(res, "base_option_out_of_range", strconv.Itoa(i))
		return
	}
	o := d.Options[i]
	if o.Kind != "cast" || o.Obj != action.Cast.Object || o.Mode != "" || o.AltCostIndex != 0 {
		setMismatch(res, "base_option_names_other_action", fmt.Sprintf("%+v", o))
		return
	}
	if err := submitChoices(b, d, []int{i}); err != nil {
		setUnmirrorable(res, "cast_rejected", err.Error())
		return
	}
	replayFollowUps(b, rep, rep.Plan.Activations, res)
}

// replayFollowUps answers the mirror's decisions after its cast submit with
// run A's recorded answers, mapped by option identity. window, when non-nil,
// is the list of planned activations to pay through the cast's mana window
// whenever that window is posed.
func replayFollowUps(b *rules.Engine, rep *Report, window []decision.PaymentActivation, res *RouteResult) {
	ri, ai := 0, 0
	limit := len(rep.FollowUps) + 4*len(window) + 16
	for step := 0; ; step++ {
		if b.G.Over {
			break
		}
		d := b.Pending()
		if d == nil {
			setMismatch(res, "mirror_no_pending", "")
			return
		}
		if d.Kind == decision.KPriority {
			break
		}
		if step >= limit {
			setMismatch(res, "mirror_unbounded", string(d.Kind)+":"+d.Prompt)
			return
		}
		if window != nil && castWindow(d, rep.Object) {
			if ai < len(window) {
				act := window[ai]
				ai++
				idx := findOption(d, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == act.Source })
				if idx < 0 {
					setUnmirrorable(res, "window_missing_source", fmt.Sprintf("source %d (%s)", act.Source, objName(b, act.Source)))
					return
				}
				if err := submitChoices(b, d, []int{idx}); err != nil {
					setUnmirrorable(res, "window_activate_rejected", err.Error())
					return
				}
				if reason := answerManaAsks(b, rep.Player, act); reason != "" {
					setUnmirrorable(res, reason, fmt.Sprintf("source %d (%s)", act.Source, objName(b, act.Source)))
					return
				}
				continue
			}
			if ri >= len(rep.FollowUps) || rep.FollowUps[ri].Kind != d.Kind || rep.FollowUps[ri].Prompt != d.Prompt {
				setMismatch(res, "window_posed_after_plan", d.Prompt)
				return
			}
		}
		if ri >= len(rep.FollowUps) {
			setMismatch(res, "mirror_extra_decision", string(d.Kind)+":"+d.Prompt)
			return
		}
		r := rep.FollowUps[ri]
		ri++
		choices, rest, note, ok := mapAnswer(r, d)
		if note != "" && len(res.ShapeNotes) < 8 {
			res.ShapeNotes = append(res.ShapeNotes, note)
		}
		if !ok {
			setUnmirrorable(res, "follow_up_unmappable", note)
			return
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices, Rest: rest}
		if err := b.Submit(in); err != nil {
			setMismatch(res, "follow_up_rejected", string(d.Kind)+":"+err.Error())
			return
		}
	}
	if ri < len(rep.FollowUps) {
		setMismatch(res, "mirror_missing_decision", string(rep.FollowUps[ri].Kind)+":"+rep.FollowUps[ri].Prompt)
		return
	}
	if window != nil && ai < len(window) {
		setMismatch(res, "window_activations_unused", fmt.Sprintf("%d of %d", len(window)-ai, len(window)))
	}
}

// passAnswer is the post-resolution horizon's deterministic answerer: pass
// priority, otherwise the first Min options (the first option when a choice
// is mandatory but Min is zero is never needed: Min 0 means "none" is legal).
func passAnswer(d *decision.Decision) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KPriority {
		if i := findOption(d, func(o decision.Option) bool { return o.Kind == "pass" }); i >= 0 {
			in.Choices = []int{i}
			return in
		}
	}
	n := d.Min
	if n > len(d.Options) {
		n = len(d.Options)
	}
	for i := 0; i < n; i++ {
		in.Choices = append(in.Choices, d.Options[i].Index)
	}
	return in
}

// resolveHorizon continues clones of ref and b with passAnswer until the
// planned spell is no longer on the stack (or the game ends), then compares.
func resolveHorizon(ref, b *rules.Engine, fork int, rep *Report, res *RouteResult) {
	ra, rb := ref.Clone(), b.Clone()
	drive := func(e *rules.Engine) (reason string) {
		defer func() {
			if r := recover(); r != nil {
				reason = fmt.Sprintf("panic:%v", r)
			}
		}()
		for i := 0; i < 400; i++ {
			if e.G.Over {
				return ""
			}
			d := e.Pending()
			if d == nil {
				return "no_pending"
			}
			if o := e.G.Obj(rep.Object); d.Kind == decision.KPriority && (o == nil || o.Zone != state.ZStack) {
				return ""
			}
			if err := e.Submit(passAnswer(d)); err != nil {
				return "rejected:" + string(d.Kind)
			}
		}
		return "unbounded"
	}
	why := failureClass(drive(ra))
	whyB := failureClass(drive(rb))
	if why != "" || whyB != "" {
		if why != whyB {
			res.Resolved = "mismatch"
			setPostResolution(res, fmt.Sprintf("horizon A=%q mirror=%q", why, whyB))
			return
		}
		res.Resolved = "skipped:" + why
		return
	}
	var post RouteResult
	if ra.G.Over && rb.G.Over {
		if outcome(ra) == outcome(rb) {
			res.Resolved = string(Equivalent)
			return
		}
		post.Diffs = []Diff{{Path: "outcome", A: outcome(ra), B: outcome(rb)}}
	} else {
		collectDiffs(ra, rb, fork, &post)
	}
	if len(post.Diffs) == 0 && len(post.EventsOnlyA) == 0 && len(post.EventsOnlyB) == 0 {
		res.Resolved = string(Equivalent)
		return
	}
	res.Resolved = "mismatch"
	res.Diffs, res.EventsOnlyA, res.EventsOnlyB = post.Diffs, post.EventsOnlyA, post.EventsOnlyB
	setPostResolution(res, "")
}

// failureClass strips a horizon failure to its class ("panic:livelock
// detected (repeating cycle)"): event sequence numbers inside a livelock
// diagnostic are offset by the route's extra decision events.
func failureClass(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		if j := strings.IndexAny(s[i+1:], ":"); j >= 0 {
			return s[:i+1+j]
		}
	}
	return s
}

func setPostResolution(res *RouteResult, detail string) {
	res.Status, res.Reason, res.Detail = Mismatch, "post_resolution", detail
	res.Signature = signature(res)
}

// castWindow reports whether d is the cast's CR 601.2g mana window
// (manaWindowAsk): a KChoose of "activate" sources plus "done" whose Source
// is the card being cast.
func castWindow(d *decision.Decision, card state.ObjID) bool {
	if d.Kind != decision.KChoose || d.Source != card || len(d.Options) == 0 {
		return false
	}
	done := false
	for _, o := range d.Options {
		switch o.Kind {
		case "activate":
		case "done":
			done = true
		default:
			return false
		}
	}
	return done
}

// optionIdentity is everything about an option except its Index.
func optionIdentity(o decision.Option) string {
	o.Index = 0
	o.Grant = nil
	return fmt.Sprintf("%+v", o)
}

// mapAnswer maps run A's recorded answer onto the mirror's decision by option
// identity. note describes any difference in the offered options.
func mapAnswer(r Recorded, d *decision.Decision) (choices, rest []int, note string, ok bool) {
	if r.Kind != d.Kind || r.Player != d.Player {
		return nil, nil, fmt.Sprintf("decision differs: A %s/p%d %q, mirror %s/p%d %q", r.Kind, r.Player, r.Prompt, d.Kind, d.Player, d.Prompt), false
	}
	byID := make(map[string][]int, len(d.Options))
	for _, o := range d.Options {
		k := optionIdentity(o)
		byID[k] = append(byID[k], o.Index)
	}
	same := len(r.Options) == len(d.Options) && r.Prompt == d.Prompt && r.Min == d.Min && r.Max == d.Max
	if same {
		for i := range r.Options {
			if optionIdentity(r.Options[i]) != optionIdentity(d.Options[i]) {
				same = false
				break
			}
		}
	}
	if !same {
		note = fmt.Sprintf("%s %q: A offered %d options, mirror %d%s", d.Kind, d.Prompt, len(r.Options), len(d.Options), optionSetDelta(r.Options, d.Options))
	}
	used := make(map[string]int)
	mapIdx := func(i int) (int, bool) {
		if i < 0 || i >= len(r.Options) {
			return 0, false
		}
		k := optionIdentity(r.Options[i])
		cands := byID[k]
		if used[k] >= len(cands) {
			return 0, false
		}
		j := cands[used[k]]
		used[k]++
		return j, true
	}
	for _, c := range r.Choices {
		j, ok := mapIdx(c)
		if !ok {
			return nil, nil, note + " [chosen option absent in mirror: " + optionLabel(r.Options, c) + "]", false
		}
		choices = append(choices, j)
	}
	for _, c := range r.Rest {
		j, ok := mapIdx(c)
		if !ok {
			return nil, nil, note + " [rest option absent in mirror]", false
		}
		rest = append(rest, j)
	}
	return choices, rest, note, true
}

func optionLabel(opts []decision.Option, i int) string {
	if i < 0 || i >= len(opts) {
		return "?"
	}
	o := opts[i]
	return fmt.Sprintf("%s obj=%d %q", o.Kind, o.Obj, o.Label)
}

// optionSetDelta names up to three options only one side offered.
func optionSetDelta(a, b []decision.Option) string {
	ca := make(map[string]int)
	for _, o := range a {
		ca[fmt.Sprintf("%s obj=%d %q", o.Kind, o.Obj, o.Label)]++
	}
	for _, o := range b {
		ca[fmt.Sprintf("%s obj=%d %q", o.Kind, o.Obj, o.Label)]--
	}
	var onlyA, onlyB []string
	for k, n := range ca {
		if n > 0 {
			onlyA = append(onlyA, k)
		} else if n < 0 {
			onlyB = append(onlyB, k)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	clip := func(s []string) []string {
		if len(s) > 3 {
			return append(s[:3:3], fmt.Sprintf("+%d", len(s)-3))
		}
		return s
	}
	out := ""
	if len(onlyA) > 0 {
		out += "; only A: " + strings.Join(clip(onlyA), ", ")
	}
	if len(onlyB) > 0 {
		out += "; only mirror: " + strings.Join(clip(onlyB), ", ")
	}
	return out
}

func findOption(d *decision.Decision, pred func(decision.Option) bool) int {
	if d == nil {
		return -1
	}
	for _, o := range d.Options {
		if pred(o) {
			return o.Index
		}
	}
	return -1
}

func submitChoices(e *rules.Engine, d *decision.Decision, choices []int) error {
	return e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices})
}

func objName(e *rules.Engine, id state.ObjID) string {
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return "?"
}

func manaDelta(after, before state.Mana) decision.ManaAmount {
	var out decision.ManaAmount
	for i := range after {
		if n := after[i] - before[i]; n > 0 {
			out[i] = uint32(n)
		}
	}
	return out
}

// answerManaAsks answers the stage-1 "choose a mana ability" wheel and the
// stage-2 colour ask a manual activation of act's source may pose, choosing
// the option whose production matches the witness. It returns "" when the
// engine is past them, or an unmirrorable reason.
func answerManaAsks(b *rules.Engine, p state.PlayerID, act decision.PaymentActivation) string {
	for i := 0; i < 4; i++ {
		d := b.Pending()
		if b.G.Over || d == nil || d.Kind != decision.KChoose || d.Player != p || !manaAsk(d, act.Source) {
			return ""
		}
		idx := matchProduction(d, act.Produces, printedManaRank(b, act))
		if idx < 0 {
			return "no_matching_mana_option"
		}
		if err := submitChoices(b, d, []int{idx}); err != nil {
			return "mana_option_rejected"
		}
	}
	return "mana_ask_loop"
}

func manaAsk(d *decision.Decision, source state.ObjID) bool {
	if len(d.Options) == 0 {
		return false
	}
	for _, o := range d.Options {
		if o.Kind != "mana" || o.Obj != source {
			return false
		}
	}
	return true
}

// labelProduction parses the "Add ..." tail of a mana option label.
func labelProduction(label string) (amt decision.ManaAmount, any bool, combo []int, ok bool) {
	i := strings.LastIndex(label, "Add ")
	if i < 0 {
		return amt, false, nil, false
	}
	tail := strings.TrimSpace(label[i+len("Add "):])
	if tail == "any color" {
		return amt, true, nil, true
	}
	if strings.Contains(tail, " or ") {
		for _, part := range strings.FieldsFunc(tail, func(r rune) bool { return r == ',' || r == ' ' }) {
			if part == "or" {
				continue
			}
			if len(part) == 1 && strings.Contains("WUBRGC", part) {
				combo = append(combo, state.ManaIndex(part[0]))
			}
		}
		return amt, false, combo, len(combo) > 0
	}
	for _, r := range tail {
		if r > 127 || !strings.ContainsRune("WUBRGC", r) {
			return amt, false, nil, false
		}
		amt[state.ManaIndex(byte(r))]++
	}
	return amt, false, nil, true
}

func singleColour(m decision.ManaAmount) (int, bool) {
	idx := -1
	for i, n := range m {
		if n == 0 {
			continue
		}
		if idx >= 0 {
			return -1, false
		}
		idx = i
	}
	return idx, idx >= 0
}

// matchProduction picks the mana option that produces want: an exact label
// production first, then a colour-ask option naming its single colour, then
// an "any color"/"X or Y" option that leads to a colour ask for it.
func matchProduction(d *decision.Decision, want decision.ManaAmount, prefer int) int {
	col, single := singleColour(want)
	// pick returns the preferred candidate (a stage-1 wheel option whose
	// Ability index is the planned printed ability's rank) or the first.
	pick := func(ok func(decision.Option) bool) int {
		first := -1
		for _, o := range d.Options {
			if !ok(o) {
				continue
			}
			if prefer >= 0 && o.Ability == prefer && o.ManaSymbol == "" {
				return o.Index
			}
			if first < 0 {
				first = o.Index
			}
		}
		return first
	}
	if i := pick(func(o decision.Option) bool {
		amt, any, combo, ok := labelProduction(o.Label)
		return ok && !any && combo == nil && amt == want
	}); i >= 0 {
		return i
	}
	if single {
		if i := pick(func(o decision.Option) bool {
			return len(o.ManaSymbol) == 1 && state.ManaIndex(o.ManaSymbol[0]) == col
		}); i >= 0 {
			return i
		}
		if i := pick(func(o decision.Option) bool {
			_, any, combo, ok := labelProduction(o.Label)
			return ok && (any || containsInt(combo, col))
		}); i >= 0 {
			return i
		}
	}
	return -1
}

// printedManaRank is the planned printed ability's position among its face's
// mana abilities -- the stage-1 wheel's Option.Ability when every one of them
// is available -- or -1. It only breaks ties between options whose labels
// match the witness production equally ("Add any color" twice on Sceptre of
// Eternal Glory, whose second ability adds three).
func printedManaRank(b *rules.Engine, act decision.PaymentActivation) int {
	if act.Ability.Kind != decision.PaymentAbilityPrinted {
		return -1
	}
	o := b.G.Obj(act.Source)
	if o == nil || o.Face() == nil || int(act.Ability.Index) >= len(o.Face().Abilities) {
		return -1
	}
	rank := 0
	for i, sa := range o.Face().Abilities {
		if i == int(act.Ability.Index) {
			return rank
		}
		if sa != nil && (sa.API == "Mana" || sa.API == "ManaReflected") {
			rank++
		}
	}
	return -1
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// compareEngines is the equivalence criterion: the reflective state diff of
// the two engines (minus the documented exclusions), the pending decision
// with its Seq-bound identities masked, and the route-independent events
// both logged since the fork.
func compareEngines(a, b *rules.Engine, fork int, res *RouteResult) {
	collectDiffs(a, b, fork, res)
	if len(res.Diffs) > 0 || len(res.EventsOnlyA) > 0 || len(res.EventsOnlyB) > 0 {
		reason := "state_differs"
		if len(res.Diffs) == 0 {
			reason = "events_differ"
		}
		setMismatch(res, reason, "")
		return
	}
	res.Status = Equivalent
}

// collectDiffs fills res's state and event differences without judging them.
func collectDiffs(a, b *rules.Engine, fork int, res *RouteResult) {
	df := newDiffer()
	df.walk("", reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
	comparePending(df, a.Pending(), b.Pending())
	res.Diffs = df.diffs
	ev := compareEvents(a.L.Events[fork:], b.L.Events[fork:])
	res.EventsOnlyA, res.EventsOnlyB, res.EventOrderDiffers = ev.OnlyA, ev.OnlyB, ev.OrderDiffers
}

// atRestViolation reports whether an engine sitting at a priority decision
// still has a cast transaction or a choose flow in progress -- the state
// Engine.Clone's contract calls an intent boundary must never carry into a
// priority window (CR 601.2: nobody holds priority while a spell is being
// cast). The fields are unexported, so they are read through reflect.
func atRestViolation(e *rules.Engine) string {
	d := e.Pending()
	if e.G.Over || d == nil || d.Kind != decision.KPriority {
		return ""
	}
	v := reflect.ValueOf(e).Elem()
	var bad []string
	if f := v.FieldByName("cast"); f.IsValid() && !f.IsNil() {
		bad = append(bad, "cast_pending")
	}
	if f := v.FieldByName("choosing"); f.IsValid() && !f.IsZero() {
		bad = append(bad, "choosing="+render(f, 0))
	}
	if len(bad) == 0 {
		return ""
	}
	return "priority_mid_cast(" + strings.Join(bad, ",") + ")"
}

// outcome renders a finished game's result: the winner (or draw) and the
// set of players who lost.
func outcome(e *rules.Engine) string {
	var sb strings.Builder
	if e.G.Draw {
		sb.WriteString("draw")
	} else {
		sb.WriteString("winner=" + strconv.Itoa(int(e.G.Winner)))
	}
	for _, p := range e.G.Players {
		if p.Lost {
			sb.WriteString(" lost=" + strconv.Itoa(int(p.ID)))
		}
	}
	return sb.String()
}

// sideEffects names the damage events attributed to a planned source.
func sideEffects(a *rules.Engine, fork int, plan decision.PaymentPlan) []string {
	src := make(map[state.ObjID]bool, len(plan.Activations))
	for _, act := range plan.Activations {
		src[act.Source] = true
	}
	// Attribute to a planned source every event between its Tap and the next
	// planned Tap or the payment's first spend: the witness promises a tap and
	// mana only, so anything else there (damage, life, counters, a draw) is an
	// unannounced side effect of the selected activation.
	var out []string
	var cur state.ObjID
	for _, ev := range a.L.Events[fork:] {
		switch {
		case ev.Kind == events.Tap && src[ev.Obj]:
			cur = ev.Obj
			continue
		case ev.Kind == events.ManaAdd && ev.Amount < 0, ev.Kind == events.DecisionAsk, ev.Kind == events.Priority:
			cur = 0
			continue
		case cur == 0, ev.Kind == events.ManaAdd, ev.Kind == events.DamageProvenance:
			continue
		}
		what := ev.Kind.String()
		switch ev.Kind {
		case events.Damage:
			what = fmt.Sprintf("damage %d", ev.Amount)
		case events.LifeChange:
			what = fmt.Sprintf("life %+d", ev.Amount)
		case events.CounterChange:
			what = fmt.Sprintf("counter %+d %s", ev.Amount, ev.Counter)
		}
		out = append(out, fmt.Sprintf("%s from %q", what, objName(a, cur)))
	}
	return out
}

// witnessViolation compares run A's actual production with the selected
// witness: each planned source's first Tap after the fork must be followed by
// ManaAdd events adding exactly its Produces.
func witnessViolation(a *rules.Engine, fork int, plan decision.PaymentPlan) string {
	evs := a.L.Events[fork:]
	var bad []string
	for i, act := range plan.Activations {
		at := -1
		for j, ev := range evs {
			if ev.Kind == events.Tap && ev.Obj == act.Source {
				at = j
				break
			}
		}
		if at < 0 {
			bad = append(bad, fmt.Sprintf("unexecuted#%d(src %d %q)", i, act.Source, objName(a, act.Source)))
			continue
		}
		var got decision.ManaAmount
		for _, ev := range evs[at+1:] {
			if ev.Kind != events.ManaAdd || ev.Amount <= 0 || ev.Counter == "" {
				break
			}
			sym := ev.Counter[len(ev.Counter)-1]
			got[state.ManaIndex(sym)] += uint32(ev.Amount)
		}
		if got != act.Produces {
			bad = append(bad, fmt.Sprintf("produced#%d(src %d %q planned %s got %s)", i, act.Source, objName(a, act.Source), manaString(act.Produces), manaString(got)))
		}
	}
	if len(bad) == 0 {
		return ""
	}
	kind := "wrong_production"
	if strings.HasPrefix(bad[0], "unexecuted") {
		kind = "unexecuted_activation"
		if len(bad) == len(plan.Activations) {
			// Nothing was tapped at all: the cast was reversed before its
			// payment stage (an announcement the witness cannot carry).
			if o := a.G.Obj(castObject(a, fork)); o == nil || o.Zone != state.ZStack {
				kind = "cast_aborted_before_payment"
			}
		}
	}
	for _, b := range bad {
		if strings.HasPrefix(b, "produced") {
			kind = "wrong_production"
			break
		}
	}
	return kind + ": " + strings.Join(bad, "; ")
}

// castObject is the object the fork's first StackPush put on the stack (the
// planned spell), or 0.
func castObject(a *rules.Engine, fork int) state.ObjID {
	for _, ev := range a.L.Events[fork:] {
		if ev.Kind == events.PutOnStack {
			return ev.Obj
		}
	}
	return 0
}

func manaString(m decision.ManaAmount) string {
	var sb strings.Builder
	for i, n := range m {
		for k := uint32(0); k < n && k < 20; k++ {
			sb.WriteByte("WUBRGC"[i])
		}
	}
	if sb.Len() == 0 {
		return "-"
	}
	return sb.String()
}

// comparePending compares the two pending decisions with Seq and the
// Seq-bound payment identities masked.
func comparePending(df *differ, a, b *decision.Decision) {
	mask := func(d *decision.Decision) *decision.Decision {
		if d == nil {
			return nil
		}
		c := d.Clone()
		c.Seq = 0
		for i := range c.PaymentActions {
			c.PaymentActions[i].ID = ""
			for j := range c.PaymentActions[i].Plans {
				c.PaymentActions[i].Plans[j].ID = ""
				// SourceZoneSeq is a log position: a source that entered after
				// the fork is logged at a Seq offset by the route's extra
				// decision events. The incarnation itself is compared through
				// the object (G.Objs[*].Zone/Incarnation).
				for k := range c.PaymentActions[i].Plans[j].Activations {
					c.PaymentActions[i].Plans[j].Activations[k].SourceZoneSeq = 0
				}
			}
		}
		if c.PaymentFallback != nil {
			f := *c.PaymentFallback
			f.PlanID = ""
			c.PaymentFallback = &f
		}
		return c
	}
	df.walk("pending", reflect.ValueOf(mask(a)), reflect.ValueOf(mask(b)))
}

// signature is the stable grouping key of a non-equivalent route result.
func signature(res *RouteResult) string {
	if res.Status == Equivalent {
		return ""
	}
	parts := []string{string(res.Route), string(res.Status), res.Reason}
	if res.Invariant != "" {
		parts = append(parts, "invariant:"+res.Invariant)
	}
	if res.Panic != "" {
		parts = append(parts, firstLine(res.Panic))
	}
	seen := map[string]bool{}
	var paths []string
	for _, d := range res.Diffs {
		p := normalizePath(d.Path)
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, k := range res.EventsOnlyA {
		p := "eventA:" + eventKindOf(k)
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, k := range res.EventsOnlyB {
		p := "eventB:" + eventKindOf(k)
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	if len(paths) > 6 {
		paths = append(paths[:6], fmt.Sprintf("+%d", len(paths)-6))
	}
	if len(paths) > 0 {
		parts = append(parts, strings.Join(paths, ","))
	}
	return strings.Join(parts, "|")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
