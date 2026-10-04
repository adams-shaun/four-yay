package rules

import (
	"strconv"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// replRedirect records one destination-changing move replacement in flight:
// the move it replaced and the identities (replIdentity) of every
// replacement already applied to that event.
type replRedirect struct {
	orig    events.Event
	applied []string
}

// replIdentity names one replacement match stably across re-collection: an
// Effect-created one by its registration key, a printed one by its source
// object and line.
func replIdentity(m replMatch) string {
	if m.key != "" {
		return m.key
	}
	return strconv.Itoa(int(m.id)) + "|" + m.repl.Event + "|" + m.repl.ParamStr(cards.PKReplaceWith) +
		"|" + m.repl.ParamStr(cards.PKDescription)
}

// redirectRecheck reports whether a move emitted inside a replacement body is
// the MODIFIED event of the in-flight destination-changing replacement: the
// replaced object going to a zone other than the one it originally would
// have. CR 616.1f: once a replacement has applied, any other replacement
// that now applies to the modified event gets its opportunity -- Magus of
// the Will exiling the Mox Diamond its own replacement puts into the
// graveyard. The one already applied never re-applies (CR 614.5).
func (e *Engine) redirectRecheck(ev events.Event) bool {
	r := e.replRedirect
	return r != nil && ev.Kind == events.MoveZone && r.orig.Kind == events.MoveZone &&
		ev.Obj == r.orig.Obj && ev.To != r.orig.To
}

// applyRedirectReplacements is the CR 616.1f pass over a redirect's modified
// move, skipping every replacement already applied to it.
func (e *Engine) applyRedirectReplacements(ev events.Event) (events.Event, bool) {
	savedExclude, savedRedirect := e.replExclude, e.replRedirect
	e.replExclude = e.replRedirect.applied
	e.replRedirect = nil
	replaced, handled := e.applyReplacements(ev)
	e.replExclude, e.replRedirect = savedExclude, savedRedirect
	return replaced, handled
}

// dropAppliedReplacements removes the matches a CR 616.1f recheck excludes.
func (e *Engine) dropAppliedReplacements(matches []replMatch) []replMatch {
	if len(e.replExclude) == 0 {
		return matches
	}
	out := matches[:0:0]
	for _, m := range matches {
		skip := false
		id := replIdentity(m)
		for _, x := range e.replExclude {
			if x == id {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, m)
		}
	}
	return out
}

// composeUpdatedReplacements applies every applicable "Updated" replacement
// to one MoveZone event: the original event is emitted and triggers fire
// once, then each With is resolved in the deterministic scan order (CR
// 616.1f, each applicable replacement gets one opportunity). This is the
// composition a competing set of entry replacements needs -- a permanent that
// both "enters tapped" (Blind Obedience) and "enters with three +1/+1
// counters" (Triskelion) finishes BOTH attrs set, not whichever the scan
// reached first.
func (e *Engine) composeUpdatedReplacements(ev events.Event, matches []replMatch) (events.Event, bool) {
	// CR 616.1's order choice for the one Updated shape where the order can
	// change the result: a tap and an untap competing over the same tapped
	// bit (the last body applied wins), or any body the commute
	// classification cannot name. The pure-augment competitions (taps with
	// PutCounter riders) land the same result in every order, so no decision
	// nobody answers differently is posed. The pose is a queue append: a
	// competition that arrived while another decision was outstanding parks
	// behind it and is asked when the queue drains (Submit's tail), and a
	// competition whose affected controller has left the game makes no
	// choices (CR 800.4a) and falls through to the deterministic scan-order
	// composition below.
	var cands []replMatch
	for _, m := range matches {
		if m.repl.With != nil {
			cands = append(cands, m)
		}
	}
	if p, ok := e.moveAffectedPlayer(ev); ok && len(cands) > 1 && !updatedReplacementsCommute(cands) {
		e.replChoices = append(e.replChoices, replChoice{kind: replChoiceUpdated,
			ev: ev, cands: cands, before: e.retainTriggerBefore(),
			damaging: e.damaging, combatDamaging: e.combatDamaging, dmgSrcOverride: e.dmgSrcOverride,
			inResolution: e.resolvingObj != 0 || e.answerInResolution})
		if e.pending == nil {
			e.askReplacementChoice(p)
		}
		return events.Event{Kind: events.Note, Obj: ev.Obj, Player: ev.Player,
			Text: "entry awaiting replacement-order choice"}, true
	}
	departing, link, controller := e.captureSourceLifelinkLKI(ev)
	stored, absorbed := e.foldEntryMove(ev)
	e.loop.observeFrom(&stored, e.damaging, len(e.G.Objs))
	// The move-driven Effect lifetimes, replayed inline exactly as the
	// single-match Updated branch does (the raw events.Emit above bypasses
	// Engine.emit's own sweep point).
	e.effectMoveSweep(ev)
	e.finishSourceLifelinkLKI(ev, departing, link, controller)
	for _, m := range matches {
		if m.repl.With == nil {
			continue
		}
		if bodyAbsorbed(absorbed, m) {
			// Its PutCounter|ETB$ True placement was folded into the move's
			// Pairs payload (rules/entry_counters.go); running the body would
			// place the counters twice.
			continue
		}
		e.runReplaceWith(e.replCtx(m, ev), ev.Obj, m.repl.With, nil)
	}
	// Matched after every Updated body, as in the single-match branch.
	e.checkTriggers(&stored, nil, 0, 0, false)
	if e.pending == nil && stored.Kind == events.MoveZone && stored.To == state.ZBattlefield {
		e.finishLandPlay(stored.Obj)
	}
	return stored, true
}

// moveAffectedPlayer is the CR 616.1 affected player of an object-carried
// event: its controller. The shared affected-player read of the Updated
// composition's pose and resume.
func (e *Engine) moveAffectedPlayer(ev events.Event) (state.PlayerID, bool) {
	o := e.G.Obj(ev.Obj)
	if o == nil || int(o.Controller) >= len(e.G.Players) {
		return 0, false
	}
	return o.Controller, true
}

// updatedBodyClass classifies one Updated replacement's With body for the
// commute check: "tap", "untap", "counter" (a PutCounter rider), or
// "other" for anything the classification cannot name.
// updatedNeutralBody reports whether one body in a ReplaceWith$ chain
// records an as-enters CHOICE without touching any characteristic another
// Updated body could reorder: a colour or type pick, a hand reveal -- the
// answer lands in the entering object's own record (ChosenColor,
// Remembered), so composing the record before or after a tap/untap/counter
// rider changes nothing. Any other API fails closed (the caller poses).
func updatedNeutralBody(sa *cards.SA) bool {
	if v, ok := updatedNeutralBodyTab.Get(sa.API); ok {
		return v
	}
	return false
}

func updatedBodyClass(m replMatch) string {
	body := m.repl.With
	if body == nil {
		return ""
	}
	// The class of a whole chain is the most order-sensitive member of it:
	// a Reveal whose SubAbility$ chain ends in a conditional Tap fights an
	// untap competitor as a tap does (the conditional depends only on the
	// chain's own recorded choice, but the conservative direction is to
	// pose). A chain of neutral recording bodies alone commutes with
	// everything ("record").
	tap, untap := false, false
	for sa := body; sa != nil; sa = sa.Sub {
		if updatedNeutralBody(sa) {
			continue
		}
		switch updatedBodyClassCodes.Code(string(sa.API)) {
		case updatedBodyClassTap:
			tap = true
		case updatedBodyClassUntap:
			untap = true
		case updatedBodyClassPutCounter:
			// Additive on a fresh entry: rides with anything.
		default:
			return "other"
		}
	}
	switch {
	case tap && untap:
		// One chain tapping and untapping the same entry is order-sensitive
		// within itself; the conservative direction is to pose.
		return "other"
	case tap:
		return "tap"
	case untap:
		return "untap"
	default:
		return "record"
	}
}

// updatedReplacementsCommute reports whether the all-Updated competition's
// bodies compose order-insensitively: taps, untaps and PutCounter riders
// touch disjoint or purely additive characteristics (two "enters tapped"
// bodies land the same state either order; two PutCounter riders add),
// while a tap and an untap fight over the same tapped bit and any body the
// classification cannot name is conservatively order-sensitive.
func updatedReplacementsCommute(matches []replMatch) bool {
	tap, untap := false, false
	for _, m := range matches {
		switch updatedReplacementsCommuteCodes.Code(string(updatedBodyClass(m))) {
		case updatedReplacementsCommuteTap:
			tap = true
		case updatedReplacementsCommuteUntap:
			untap = true
		case updatedReplacementsCommuteCounter:
		default:
			return false
		}
	}
	return !(tap && untap)
}

// resumeUpdatedComposition continues a parked all-Updated competition: the
// original event is emitted once (the composeUpdatedReplacements preamble,
// exactly what a lone Updated replacement's applyReplacement arm does), the
// chosen body resolves, and the remaining candidates re-check their gates
// against the event as it now stands (CR 616.1e) before the composition
// either re-poses a live non-commuting remainder or finishes it in
// deterministic scan order.
func (e *Engine) resumeUpdatedComposition(rc replChoice, selected int) {
	if !rc.emitted {
		departing, link, controller := e.captureSourceLifelinkLKI(rc.ev)
		// The fold, not a raw Emit: the original event may carry entry-
		// characteristic counter grants (rules/entry_counters.go), which a
		// raw Emit would silently drop -- a walker entering under a parked
		// tap-vs-untap competition would land at zero loyalty. foldEntryMove
		// folds the grants with the move exactly as applyReplacement's
		// Updated arm does, and never re-runs the replacement dispatch (so
		// the just-answered competition cannot re-pose).
		stored, absorbed := e.foldEntryMove(rc.ev)
		rc.absorbed = absorbed
		e.loop.observeFrom(&stored, e.damaging, len(e.G.Objs))
		// The move-driven Effect lifetimes, replayed inline exactly as the
		// synchronous composition does (see applyReplacement's Updated arm).
		e.effectMoveSweep(rc.ev)
		e.checkTriggers(&stored, nil, 0, 0, false)
		e.finishSourceLifelinkLKI(rc.ev, departing, link, controller)
		rc.emitted = true
	}
	chosen := rc.cands[selected]
	if !bodyAbsorbed(rc.absorbed, chosen) {
		e.runReplaceWith(e.replCtx(chosen, rc.ev), rc.ev.Obj, chosen.repl.With, nil)
	}
	var remaining []replMatch
	for i, m := range rc.cands {
		if i == selected {
			continue
		}
		if e.replacementMatches(*m.repl, m.id, rc.ev) {
			remaining = append(remaining, m)
		}
	}
	if p, ok := e.moveAffectedPlayer(rc.ev); ok && !e.G.Players[p].Lost &&
		len(remaining) > 1 && !updatedReplacementsCommute(remaining) {
		rc.cands = remaining
		e.replChoices = append([]replChoice{rc}, e.replChoices...)
		if e.pending == nil {
			e.askReplacementChoice(p)
		}
		return
	}
	for _, m := range remaining {
		if bodyAbsorbed(rc.absorbed, m) {
			continue
		}
		e.runReplaceWith(e.replCtx(m, rc.ev), rc.ev.Obj, m.repl.With, nil)
	}
	if e.pending == nil && rc.ev.Kind == events.MoveZone && rc.ev.To == state.ZBattlefield {
		e.finishLandPlay(rc.ev.Obj)
	}
}

var updatedNeutralBodyTab = state.NewStrTable[bool](
	state.StrEntry[bool]{Key: "Reveal", Val: true},
	state.StrEntry[bool]{Key: "ChooseColor", Val: true},
	state.StrEntry[bool]{Key: "ChooseType", Val: true},
	state.StrEntry[bool]{Key: "ChooseNumber", Val: true},
	state.StrEntry[bool]{Key: "ChooseCard", Val: true},
	state.StrEntry[bool]{Key: "Cleanup", Val: true},
	state.StrEntry[bool]{Key: "Hideaway", Val: true},
)

type updatedBodyClassCode uint16

const (
	updatedBodyClassTap updatedBodyClassCode = iota + 1
	updatedBodyClassUntap
	updatedBodyClassPutCounter
)

var updatedBodyClassCodes = state.NewStrCodes(
	state.StrEntry[updatedBodyClassCode]{Key: "Tap", Val: updatedBodyClassTap},
	state.StrEntry[updatedBodyClassCode]{Key: "Untap", Val: updatedBodyClassUntap},
	state.StrEntry[updatedBodyClassCode]{Key: "PutCounter", Val: updatedBodyClassPutCounter},
)

type updatedReplacementsCommuteCode uint16

const (
	updatedReplacementsCommuteTap updatedReplacementsCommuteCode = iota + 1
	updatedReplacementsCommuteUntap
	updatedReplacementsCommuteCounter
)

var updatedReplacementsCommuteCodes = state.NewStrCodes(
	state.StrEntry[updatedReplacementsCommuteCode]{Key: "tap", Val: updatedReplacementsCommuteTap},
	state.StrEntry[updatedReplacementsCommuteCode]{Key: "untap", Val: updatedReplacementsCommuteUntap},
	state.StrEntry[updatedReplacementsCommuteCode]{Key: "counter", Val: updatedReplacementsCommuteCounter},
	state.StrEntry[updatedReplacementsCommuteCode]{Key: "record", Val: updatedReplacementsCommuteCounter},
)
