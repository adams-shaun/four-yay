package trigmatch

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Matcher answers whether one trigger fires for ev. lki is the event's LKI
// snapshot (the moving object as it was a moment ago); a matcher that does not
// need it ignores the parameter. The uniform signature is what lets every mode
// live behind one table.
type Matcher func(b Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool

// trigMatchers maps Mode$ (as its load-time cards.TriggerMode code) to its
// matcher. A mode with no entry never fires, which is exactly what the switch
// this replaced did by falling off its end with matched still false -- there
// was no default arm, and there must not be one now. Code 0 (a name outside
// the vocabulary) is never registered.
var trigMatchers [cards.TriggerModeCount]Matcher

// registerTrigMatcher installs fn for each named mode. Called from init() in
// this package's per-mode files.
//
// A duplicate registration panics rather than silently replacing. The whole
// point of the split is that many tickets edit different files at once; two
// files claiming one mode is the merge accident that costs, so it must be loud
// at startup and not a matcher that quietly stopped being reached.
func registerTrigMatcher(fn Matcher, modes ...string) {
	for _, mode := range modes {
		k := cards.TriggerModeOf(mode)
		if k == 0 {
			panic("trigmatch: Mode$ " + mode + " is not in the cards.TriggerMode vocabulary")
		}
		if trigMatchers[k] != nil {
			panic("trigmatch: duplicate trigger matcher registered for Mode$ " + mode)
		}
		trigMatchers[k] = fn
	}
}

// Lookup returns the matcher registered for mode, or nil: a mode with no
// matcher never fires. It is the one read of the table outside this package
// (rules' delayed-trigger and Effect-registration paths, which run their own
// gates before calling the matcher).
func Lookup(mode string) Matcher { return trigMatchers[cards.TriggerModeOf(mode)] }

// Match reports whether t fires for ev through its mode's registered matcher.
// A mode with no entry never fires: the switch the table replaced had no
// default arm, so an unknown mode fell off its end with matched still false.
func Match(b Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if fn := trigMatchers[t.ModeKind()]; fn != nil {
		return fn(b, t, source, ev, lki)
	}
	return false
}

// CompareIntCount evaluates Forge's <OP><N> comparison grammar (EQ1, GT1,
// EQ0, ...) against n. A value that is not a literal comparison (an X, a
// bare word, an unknown operator) fails closed: a trigger condition the
// engine cannot evaluate must stay silent, never fire wide.
func CompareIntCount(n int32, expr string) bool {
	expr = strings.TrimSpace(expr)
	for _, cand := range []struct {
		op string
		fn func(a, b int32) bool
	}{{"EQ", func(a, b int32) bool { return a == b }},
		{"NE", func(a, b int32) bool { return a != b }},
		{"GE", func(a, b int32) bool { return a >= b }},
		{"LE", func(a, b int32) bool { return a <= b }},
		{"GT", func(a, b int32) bool { return a > b }},
		{"LT", func(a, b int32) bool { return a < b }}} {
		if rest := strings.TrimPrefix(expr, cand.op); rest != expr {
			v, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return false
			}
			return cand.fn(n, int32(v))
		}
	}
	return false
}

// phaseOutAllMatches matches a Mode$ PhaseOutAll trigger (The War Doctor's
// "whenever one or more other permanents phase out") against one PhaseOut
// event. Only a phase-OUT (Amount >= 1) counts; a phase-IN (Amount -1) is the
// opposite event. The line's ValidCards$/ValidCard$ filter is evaluated
// against the phasing permanent with the trigger's source bound the way every
// object matcher binds it, so `Permanent.phasedOutOther` can exclude the
// source itself. The once-per-batch cadence is the latch in triggerMatches,
// not this matcher.
func phaseOutAllMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.PhaseOut || ev.Amount < 1 || ev.Obj == 0 {
		return false
	}
	spec := strings.TrimSpace(t.ParamStr(cards.PKValidCards))
	if spec == "" {
		spec = strings.TrimSpace(t.ParamStr(cards.PKValidCard))
	}
	if spec == "" {
		return true
	}
	return e.MatchesSpec(spec, ev.Obj, source, e.ControllerOf(source), SpecOpts{})
}

func init() {
	// CR 603.8 state trigger: the event under test is irrelevant; the trigger
	// fires when its condition holds (see triggerConditionHolds) and no
	// instance is outstanding (the checkTriggers latch).
	registerTrigMatcher(func(Board, cards.Trigger, state.ObjID, events.Event, *state.Object) bool {
		return true
	}, "Always")
	// PhaseOutAll (CR 702.25, phaseoutall1): the batch-level "whenever one or
	// more other permanents phase out" trigger (The War Doctor). It matches
	// the events.PhaseOut marker the api:Phases primitive emits, which existed
	// before the mode did; the mode itself is new to the table. Registered
	// through a func literal calling the pack-level matcher so the census can
	// read the callee (registerTrigMatcher takes a method expression or a
	// literal whose first call names the matcher).
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
		return phaseOutAllMatches(e, t, source, ev, lki)
	}, "PhaseOutAll")
}
