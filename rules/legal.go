package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// modalLandBack identifies the only Modal DFC face that may be selected by
// the hand-zone modal-land action. Keeping the shape check here makes offer
// and consumption use the same validation and prevents non-land modal backs
// from becoming castable through this path.
func modalLandBack(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.FaceIdx != 0 ||
		o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 ||
		o.Card.Faces[0] == nil || o.Card.Faces[1] == nil ||
		o.Zone != state.ZHand {
		return nil
	}
	if !o.Card.Faces[1].IsLand() {
		return nil
	}
	return o.Card.Faces[1]
}

// modalSpellBack identifies a modal DFC's nonland back face when it is in
// hand. This is the ordinary CR 712 "Modal" MDFC only. A Secrets of
// Strixhaven "Prepare" pair is NOT a modal DFC: CR 722.3 says preparation
// cards "can't be cast using the alternative characteristics found within
// their inset frames", so the inset prepare spell is never offered from hand
// (the only way to cast it is the CR 722.3c copy made in exile by the
// prepared permanent; rules/legal_walk_alt.go's "prepared_copy" mode). A nameless back
// is rejected so an unresolved stub can never put an empty-named cast on the
// stack; every real Modal back is named.
func modalSpellBack(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.FaceIdx != 0 ||
		o.Card.AlternateMode != "Modal" || len(o.Card.Faces) != 2 ||
		o.Card.Faces[0] == nil || o.Card.Faces[1] == nil ||
		o.Zone != state.ZHand || o.Card.Faces[1].IsLand() ||
		o.Card.Faces[1].Name == "" {
		return nil
	}
	return o.Card.Faces[1]
}

// sorcerySpeed reports whether p may take a sorcery-speed action right now.
func (e *Engine) sorcerySpeed(p state.PlayerID) bool {
	return e.G.Active == p && e.G.Step.IsMain() && len(e.G.Stack) == 0
}

// legalActions enumerates everything p may legally do with priority. The
// result is the complete rules surface a client ever sees.
func (e *Engine) legalActions(p state.PlayerID) []decision.Option {
	return e.legalActionsPriced(p, nil)
}

func (e *Engine) legalActionsWithWindow(p state.PlayerID, w *windowCollector) []decision.Option {
	return e.legalActionsWalkWithWindow(p, nil, false, w, true, nil, false)
}

// aftermathAlternateFace returns the Aftermath alternate face (face 1 --
// ALTERNATE starts face 1 in cards/parse.go) of a two-face Split card whose
// front face is current, or nil when the object is not a well-formed
// aftermath carrier: AlternateMode must be Split (a Room is Split too, but
// no Room half carries K:Aftermath, and the keyword gate is what keeps Rooms
// and Adventures on their own paths), the object must have exactly two
// faces, and the card must still be at its front face -- the aftermath half
// is cast only from a graveyard card whose printed front is showing.
func aftermathAlternateFace(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.Card.AlternateMode != "Split" || len(o.Card.Faces) != 2 || int(o.FaceIdx) != 0 {
		return nil
	}
	af := o.Card.Faces[1]
	if af == nil || !af.HasKeyword("Aftermath") {
		return nil
	}
	return af
}

// legalActionsPriced is legalActions with the mana affordability priced
// against an OVERBOUND hypothetical pool instead of the seat's floating one:
// hyp nil keeps the ordinary floating-pool pricing, hyp non-nil prices every
// cast/activation gate against *hyp -- the pool the seat would hold if it
// first floated every mana its untapped sources could produce (PotentialMana).
// The walk itself is otherwise IDENTICAL: same zones (hand, command zone,
// graveyard flashback, battlefield abilities), same timing, restriction,
// target and non-mana-cost gates, same live RaiseCost/ReduceCost composition
// (offerCostFor) -- so a potential action is by construction the same option
// the engine WOULD offer once the mana floated, never a client-side
// re-derivation that can drift from the engine's own cost rules.
//
// It is a pure read: no event is emitted, no state field is written, and the
// hypothetical pool lives only in local copies, so replay is untouched.
func (e *Engine) legalActionsPriced(p state.PlayerID, hyp *state.Mana) []decision.Option {
	return e.legalActionsWalk(p, hyp, false)
}

// legalActionsWalk is legalActionsPriced's body. castsOnly skips the
// sections that append only non-cast options -- mana activations, activated
// and granted abilities, station, unlock, turn face up and specialize -- so
// its result is exactly legalActionsPriced's "cast" options, in the same
// order with the same labels, objects, modes and costs (their Index values
// differ: the skipped options never took a slot). Every cast section runs
// first and reads nothing the skipped sections write, and the post-walk
// filters (no-mana-cost, inert hold-out, split second) decide each option
// on its own. The payment offer builder, which reads only plain casts, uses
// it to avoid pricing every battlefield ability it would discard.
func (e *Engine) legalActionsWalk(p state.PlayerID, hyp *state.Mana, castsOnly bool) []decision.Option {
	return e.legalActionsWalkWithWindow(p, hyp, castsOnly, nil, false, nil, false)
}

// legalActionsWalkTemp is legalActionsWalk for a caller that only reads the
// result before it returns: the options are built into a scratch list
// borrowed from e (hypclone.go) instead of a fresh exactly-sized array. The
// caller hands it back with e.optRelease once it, and every value it took
// a pointer into, is done; the options' contents are the walk's own.
func (e *Engine) legalActionsWalkTemp(p state.PlayerID, hyp *state.Mana, castsOnly bool) []decision.Option {
	return e.legalActionsWalkWithWindow(p, hyp, castsOnly, nil, false, e.optBorrow(), true)
}

// legalActionsWalkWithWindow is the walk's body. forAsk marks the walk
// whose result becomes a posed priority decision's Options (askPriority),
// which is the one result the decision arena (decision_arena.go) may back;
// temp builds the result into dst (legalActionsWalkTemp's borrowed list).
func (e *Engine) legalActionsWalkWithWindow(p state.PlayerID, hyp *state.Mana, castsOnly bool, window *windowCollector, forAsk bool, dst []decision.Option, temp bool) []decision.Option {
	// Count the walk before anything can early-return. A test-visible
	// diagnostic only: no event, no state mutation, no effect on replay or
	// chain heads (legalActionWalks is not copied by Clone and never reaches
	// a view, an option list or a log).
	e.legalActionWalks++
	// The walk is a pure read, so every Derived it makes is memoized for the
	// walk's duration (rules/derivedmemo.go).
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	// Build into the engine's scratch list (legalOptBuf) and hand the caller
	// an exactly-sized copy at the end: the growth reallocations stay on the
	// reusable buffer, never on the returned, retained Options.
	out := e.legalOptBuf[:0]
	e.legalOptBuf = nil
	// A posed priority walk on an arena engine builds straight into the
	// arena's option tail (decision_arena.go), so the result needs neither
	// the copy out of the scratch nor the scratch's clear.
	var tail optTail
	var scratch []decision.Option
	if forAsk {
		if tail = e.arenaOptionsTail(); tail.buf != nil {
			scratch, out = out, tail.buf
		}
	}
	add := func(kind, label string, obj state.ObjID) {
		out = append(out, decision.Option{Index: len(out), Kind: kind, Label: label, Obj: obj})
	}
	// The shared offer gates and the section walkers live on legalWalk
	// (legal_walk.go, legal_walk_hand.go, legal_walk_alt.go,
	// legal_walk_battlefield.go); every section prices through the one
	// statics memo, so an offered action and the cost it charges cannot drift.
	w := &legalWalk{
		e:             e,
		p:             p,
		hyp:           hyp,
		castsOnly:     castsOnly,
		sorcery:       e.sorcerySpeed(p),
		out:           out,
		costStatics:   costStaticSource{e: e},
		actionStatics: actionStaticSource{e: e},
	}
	// Pool-independent block reuse (walk_block_reuse.go): a potential walk
	// takes the reuse its caller armed (nested walks never see it); the
	// priority walk records on an engine with full potential demand.
	if reuse := e.walkReuse; reuse != nil {
		e.walkReuse = nil
		if hyp != nil && !castsOnly {
			w.reuse = reuse
			e.recordedBoardFacts(reuse, &w.actionStatics, p)
		}
	} else if forAsk && hyp == nil && !castsOnly && e.potentialFullDemand && e.WalkRecDemand {
		w.rec = e.walkBlockRecorder(p)
	}
	w.handWalk()
	w.mayPlayLandWalk()
	w.mayhemLandWalk()
	w.mayPlaySpellWalk()
	plotZoneWalk(w)
	w.commandZoneWalk()
	w.graveyardCastsWalk()
	w.exileCastsWalk()
	w.battlefieldWalk()
	out = w.out
	walkHW := max(len(out), w.outHW)
	if w.rec != nil {
		w.rec.finish(out)
	}

	// K:Split second (CR 702.62, rules/split_second.go): while a split-second
	// spell is on the stack, players can't cast spells or activate abilities
	// that aren't mana abilities. The filter runs here -- at the ONE choke
	// point every cast source (hand, command zone, may-play, flashback/
	// aftermath/harmonize/warp/escape, exile recasts) and both ability loops
	// flow through -- rather than at each of the ~30 append sites, so the next
	// cast source added to this walk is covered by construction. Playing a
	// land, mana abilities, Station and Room unlock stay legal; the Suspend
	// and Foretell offers ride the "cast" Kind but are special actions, not
	// spell casts, so they stay too.
	// CR 118.6: a no-mana-cost card is never cast by paying its mana cost
	// (rules/nomanacost.go).
	out = e.filterNoManaCostCasts(p, out)
	// Options the inert backstop caught changing nothing this window
	// (rules/priority_guard.go) stay out until the game changes state.
	out = e.filterInertHeldOut(out)
	if e.splitSecondHolds() {
		out = e.filterSplitSecondActions(out)
	}

	// Pass is second-to-last. A client that wants to do nothing must choose
	// it explicitly: from M2d-3 the FINAL option is "concede" (R-M3, always
	// last), and a client defaulting to the final option would concede on
	// every single priority decision.
	add("pass", "Pass priority", 0)
	// M2d-3 (R-M3): concession, last after pass. Choosing it emits the
	// existing PlayerLost event with Text "conceded" (CR 104.3a) -- see
	// handlePriority. Offered on every priority decision, i.e. to every
	// living seat: grantPriority never hands a Lost seat priority, so no
	// extra guard is needed here.
	add("concede", "Concede", 0)
	var res []decision.Option
	switch {
	case tail.buf != nil && tail.commit(out, max(walkHW, len(out))):
		res = out[:len(out):len(out)]
		if window != nil {
			e.windowClassify(p, res, window)
		}
		e.legalOptBuf = scratch
		if castsOnly && castsOnlyWalkVerify {
			verifyCastsOnlyWalk(res, e.legalActionsPriced(p, hyp))
		}
		return res
	case tail.buf != nil:
		// The walk outgrew the tail (commit cleared it): the grown heap
		// array is this walk's scratch from here on.
		res = e.arenaOptions(len(out))
		copy(res, out)
	case forAsk:
		res = e.arenaOptions(len(out))
		copy(res, out)
	case temp:
		res = append(dst[:0], out...)
	default:
		res = make([]decision.Option, len(out))
		copy(res, out)
	}
	if window != nil {
		// Classify each of p's own visible candidates the walk did not
		// offer, keeping the first gate in walk order that withheld it. The
		// caller that owns the decision finishes (filters offered
		// candidates and bounds) the collector.
		e.windowClassify(p, res, window)
	}
	// Drop the scratch's string/Grant references so a retained buffer does
	// not pin the last walk's labels, then keep the grown array.
	clear(out)
	e.legalOptBuf = out[:0]
	if castsOnly && castsOnlyWalkVerify {
		verifyCastsOnlyWalk(res, e.legalActionsPriced(p, hyp))
	}
	return res
}
