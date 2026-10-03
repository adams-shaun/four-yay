// Package rules: state-based actions (CR 704) and game end (CR 104.4).
//
// checkStateBased, checkGameOver and destroyLethalDamage all predate this
// file: Task 21 added destroyLethalDamage (combat depends on lethal-damage
// and zero-toughness destruction, and nothing in this codebase checked
// either before then), and stubs.go carried checkStateBased/checkGameOver
// as a placeholder ever since Task 3 -- a checkGameOver call and nothing
// else. This task deletes stubs.go outright and relocates all three here
// rather than losing them with it, and fills in the state-based actions M1
// actually specifies: a player at 0 or less life loses (CR 704.5a); an
// eliminated player's permanents leave the battlefield; the game ends in a
// win when exactly one seat remains and a draw (CR 104.4a) when none do --
// not, as stubs.go's own checkGameOver used to, an unconditional win for
// seat 0 when nobody survived. Drawing from an empty library (CR 704.5c) is
// already a loss as of Task 14 (effects.DrawFor emits PlayerLost directly at
// the moment of the failed draw, which checkStateBased's own permanent-
// removal pass below picks up on its next run regardless of how a player
// came to be Lost); this build has no poison-counter mechanic, so CR
// 704.5b's poison loss is checked with life and commander damage below.
package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// maxSBAPasses bounds the fixed-point loop below. A game that needs more
// passes than this has a rules bug (a static-effect cycle where destroying
// one creature always makes another lethal, forever), and reporting a stuck
// game via a bounded loop is better than hanging the one goroutine running
// the whole match.
const maxSBAPasses = 32

// sbaAttempts is one checkStateBased call's memory of what its
// state-based-action passes have already tried, together with the size of
// the alive-player set they tried it under.
//
// objs is destroyLethalDamage's (Ruling T22-j), players is
// checkLoseConditions' removal sweep's (Ruling T22-n), and tokens is
// ceaseDeadTokens' own (Task 13 fix round 1, review finding "minor 1" --
// added after the fact, once the same failure shape checkLoseConditions and
// destroyLethalDamage document at length -- a replacement permanently
// blocking the move -- was pointed out as reachable here too, even though
// nothing in today's corpus is known to actually do it: with no memory of
// its own, a blocked attempt would re-match every single pass and burn the
// entire maxSBAPasses budget on one checkStateBased call, forever, rather
// than the one attempt per call every other pass here gets). All three are
// membership-only -- nothing ever ranges over any of them, so none can
// reach an event or the order of a decision's options. tokens is
// deliberately its OWN map, not shared with objs: a token that is ALSO a
// creature can be found lethal by destroyLethalDamage (which marks it in
// objs) and then, in the very same pass, need ceaseDeadTokens to move it
// again (graveyard -> ceased) -- sharing one map would have objs's mark
// wrongly block ceaseDeadTokens from ever attempting an object destroy
// LethalDamage had already touched. alive is Ruling T22-p's re-arm
// watermark, described on checkStateBased below.
type sbaAttempts struct {
	objs     map[state.ObjID]bool
	tokens   map[state.ObjID]bool
	players  map[state.PlayerID]bool
	sagas    map[state.ObjID]bool
	dungeons map[state.ObjID]bool
	alive    int
	// skips counts, for the current pass, the candidates an attempt memory
	// above made the pass skip. A stable final pass with none skipped is
	// exactly what a fresh call's first pass would see, so the run is quiet
	// even if earlier passes emitted (sbaRecordQuiet).
	skips int
}

// rearm forgets every memory when the alive-player set has shrunk since it
// was last refreshed, so an attempt that a now-eliminated player's
// permanent blocked gets exactly one more chance under the smaller alive
// set -- and none at all while nobody dies. Ruling T22-p (fix round 4):
// this is called at each of the points that consult the memories, not once
// per pass, because the sweep loop, destroyLethalDamage and ceaseDeadTokens
// all run AFTER the pass's own eliminations are marked and must not be
// re-armed out from under an attempt they made under the very same alive
// set.
//
// Player.Lost is monotone (events/apply.go's PlayerLost case is its only
// non-test writer and only ever sets it true), so alive never grows and
// this can fire at most len(Players) times per checkStateBased call: the
// loop stays bounded by work, not just by maxSBAPasses.
//
// Ruling T22-q (fix round 5, comment only -- no behaviour changed): this
// key closes ONE of the two ways a blocker can go inert mid-call, and the
// round-4 wording of this comment claimed both. It does not. Read it as a
// partial fix with a known open half, because that is what it is.
//
//   - CLOSED, the elimination sub-case: applyReplacements discovers
//     replacements through forEachObject, which walks AliveFrom(0), so
//     every replacement an eliminated player owns stops being consulted the
//     instant they are marked Lost. That is what alive tracks, and the
//     reason a blocked attempt is worth exactly one retry when it changes.
//
//   - OPEN, the predicate-flip sub-case (N9, booked for the whole-branch
//     review rather than patched here): a replacement also goes inert by
//     ceasing to MATCH. replacementMatches evaluates ValidCard$ through
//     effects.MatchesSpecFrom against the MOVING object, reading its Tapped
//     flag, its P1P1 counters, its zone, its controller and its combat
//     flags -- and every one of those is writable by a registered
//     ReplaceWith$ primitive (effects' Tap, PutCounter, RemoveCounterAll).
//     "The predicate is matched against the moving object" is therefore not
//     a safety property, which is how round 4 read it; it is precisely what
//     lets the substitute effect that blocked an attempt flip the predicate
//     that blocked it. Measured by a probe re-implementing
//     replacementMatches from outside the package: 0 outstanding-SBA
//     decision points at 29fa00d, 6 at bd3c730 and 4 here, out of ~2 261 --
//     0.18%, self-healing on the next checkStateBased call, and never
//     touching the CR 704.5a life invariant.
//
// What a future author most needs from this comment: widening the key --
// which is what a zone gate on replacements, or a ValidCard$ predicate a
// static effect can flip, would force -- must NOT be done by re-arming on
// "some other state-based action actually succeeded", nor on any signal a
// removal produces. A death chain then buys one re-arm per link, and the
// blocked removal sweep goes from a flat 2 firings per Submit back to
// 2/3/7/22/61 for chain lengths 0/1/5/20/60 -- 29fa00d's pre-round-3
// amplification. That variant has now been built and measured twice, once
// in fix round 4 and once independently by the re-review, agreeing to the
// digit. TestRemovalSweepFiringsDoNotScaleWithAnUnrelatedDeathChain is the
// assertion that catches it being made again.
//
// One more thing this file has repeatedly got wrong in prose: the residue
// above is NOT bounded by maxSBAPasses and is NOT announced by the Note
// below. A replacement that blocks an attempt forever does not exhaust the
// budget at all -- the attempted sets make the pass report no new work, the
// loop goes stable, and it returns silently. That is correct behaviour (a
// permanent block is a legitimate rules-level fixed point; there is nothing
// to warn about), and it is measured: zero Note events over five Submits
// for both the blocked destruction and the blocked removal sweep. The Note
// covers the OTHER case, an unbounded cycle of genuinely new work, and has
// never seen this one.
// markTried records k in the attempt memory *m, allocating it on first use.
func markTried[K comparable](m *map[K]bool, k K) {
	if *m == nil {
		*m = make(map[K]bool)
	}
	(*m)[k] = true
}

func (a *sbaAttempts) rearm(alive int) {
	if alive >= a.alive {
		return
	}
	a.alive = alive
	clear(a.objs)
	clear(a.tokens)
	clear(a.players)
	clear(a.sagas)
}

// checkStateBased applies state-based actions until none apply, which is
// what CR 704.3 requires: they are checked and rechecked, not just once.
// TestSBALoopsUntilStable is the regression test for the "not just once"
// half -- a lord dying can make a creature its own static was keeping alive
// newly lethal, and that second creature must not survive to see a second,
// separate checkStateBased call before it is caught.
//
// Ruling T22-h (fix round 1): "changed" must come from checkLoseConditions
// and destroyLethalDamage observing actual state after they emit, never
// from the fact that they emitted something. Engine.emit runs replacement
// effects first (rules/trigger.go), and a matching replacement can discard
// the very MoveZone this file proposes entirely -- a permanent this pass
// thinks it destroyed can simply still be sitting on the battlefield once
// the emit returns. The previous version of both functions reported
// "changed" from the event it built, not from where the object ended up,
// so a replacement that keeps a lethally-damaged permanent in play (a
// regeneration shield, "sacrifice a Clue instead", or the reviewer's own
// gain-1-life-instead reproduction) made every single pass "find" the same
// permanent lethal again: not a one-time miscount but the full
// maxSBAPasses budget, spent and reported as "changed", on every
// checkStateBased call for the rest of the match. See checkLoseConditions'
// removal sweep below for where that half of the fix lives; if
// maxSBAPasses is nonetheless exhausted (a genuine cycle, not a
// replacement -- e.g. a static that keeps making a different creature
// lethal forever), a Note event says so rather than the game quietly
// carrying on with state-based actions still outstanding.
//
// Ruling T22-j (fix round 2): destroyLethalDamage's own half of T22-h can
// UNDER-report. Reading "did it actually leave" is right for whether THAT
// object needs re-examining, but it threw away the fact that an attempt
// happened at all -- and checkLoseConditions runs before destroyLethalDamage
// within one pass, so a replacement whose substitute effect changes
// SBA-relevant state elsewhere (the reviewer's reproduction: `ReplaceWith$`
// a life-loss instead of a move) only becomes visible to checkLoseConditions
// on a LATER pass. Reporting "the shield didn't move, so nothing changed"
// denied the loop that later pass, so a player it drove to 0 life could sit
// there, un-Lost, handed a decision. attempted (below) is the fix: a set of
// object IDs destroyLethalDamage has already tried this call, threaded
// through every pass of this loop rather than rebuilt per pass, so an
// object already attempted is skipped on later passes (preserving T22-h's
// bound: one attempt per object per checkStateBased call, not thirty-two),
// while "changed" now means "attempted something NEW this pass" -- which is
// true on pass 1 regardless of outcome, giving pass 2 the chance to see
// whatever the replacement's own effect changed.
//
// Ruling T22-n (fix round 3): checkLoseConditions' own removal sweep had
// the identical shape T22-j fixed, one loop over -- "changed" came from
// whether a player's battlefield zone actually shrank, so a replacement
// that blocks the exile but changes SBA-relevant state elsewhere (some
// other player's life total) was invisible to this sweep and denied the
// later pass that would have caught it, same as T22-j's shield. swept
// (below) is destroyLethalDamage's attempted, one loop over: a set of
// PlayerIDs already swept this call, so "changed" means "swept someone new
// this pass" rather than "someone's battlefield actually shrank" -- the
// same discipline, applied to the same function's second loop, so the two
// no longer disagree about what "changed" means.
//
// Ruling T22-p (fix round 4): the discipline T22-j and T22-n share can
// under-COMPLETE, which is the mirror image of the under-report T22-j
// fixed. Whether an attempt succeeds is decided by the replacement effects
// that apply to it, and applyReplacements only ever looks at objects
// controlled by a player who is still alive (trigger.go's forEachObject
// walks AliveFrom(0)) -- so a blocker stops blocking the instant its
// controller is eliminated, and the blocked attempt's own substitute
// effect is routinely what eliminates them: a creature with lethal damage
// whose destruction a Creature.Other guardian replaces with "the
// guardian's controller loses 1 life", with that controller one life from
// zero. The attempt that failed under the old alive set would succeed
// under the new one, but the object was already in objs, so nothing
// retried it and the decision went out with a state-based action
// outstanding and nothing left on the board able to prevent it.
//
// tried.rearm is the fix, and the alive-player count is deliberately the
// ONLY thing that re-arms. It is the input that decides whether a
// replacement is consulted at all, and it is monotone, so both sets are
// cleared at most len(Players) times per call. It is NOT the only way a
// blocker can go stale -- a replacement that stops matching goes inert just
// as thoroughly as one whose controller died, and that half is still open;
// see Ruling T22-q on rearm above before widening this key. Re-arming on the
// wider "some other state-based action actually succeeded" instead would
// re-arm a blocked attempt once per link of an entirely unrelated death
// chain, which is the per-pass amplification T22-h exists to prevent. Built
// and measured rather than assumed: on
// TestRemovalSweepFiringsDoNotScaleWithAnUnrelatedDeathChain's own board,
// re-arming whenever the battlefield population actually shrank takes the
// blocked sweep from a flat 2 firings per Submit back to 2/3/7/22/61 for
// chain lengths 0/1/5/20/60 -- 29fa00d's pre-round-3 figures exactly. A
// repeatedly-blocked attempt with nobody dying is therefore still tried
// exactly once per checkStateBased call, as T22-h requires.
func (e *Engine) checkStateBased() {
	// A parked CR 704.5j batch whose ask is no longer outstanding re-poses it
	// before any SBA work: the batch is parked only together with its ask, and
	// an outstanding ask is the ONE reason the pass below refuses to apply. A
	// caller that posed its own decision at an unguarded point (the pre-fix
	// completeCombatPass tail) could displace the ask; without this re-pose the
	// parked batch would never be answered and -- because the pass below halts
	// while it is parked -- no state-based action would ever apply again. The
	// re-pose restores exactly the invariant parkLegendChoice establishes:
	// parked implies asked. It does not fire while another decision is
	// outstanding (nothing is re-asked under one); a stale flow marker in
	// e.choosing is deliberately NOT consulted, because the displaced ask left
	// e.choosing == chooseLegend behind and askLegendChoice re-sets it anyway.
	if e.legendBatch != nil && e.pending == nil {
		e.askLegendChoice()
		return
	}
	stable := false
	// The attempt memories start nil and are allocated on their first mark
	// (markTried): a read of a nil map is false, so the common call -- one
	// that attempts nothing -- allocates none of them.
	tried := &sbaAttempts{alive: e.G.AliveCount()}
	// Safety net for a duration-ending change folded outside Engine.emit
	// (the Updated replacement paths call events.Emit directly).
	e.expireControl(controlOnEvent)
	// The same safety for a static GainControl$ transfer: an SBA-pass change
	// (e.g. a legend rule binning the Aura) can end or newly want a static
	// grant without Engine.emit's tail having run the reconcile.
	e.reconcileControlStatics()
	// The quiet skip (rules/sbaquiet.go): the pass loop last ran to a fixed
	// point without emitting anything, and nothing an SBA reads has moved
	// since, so running it again would apply nothing.
	ep0 := len(e.L.Events)
	quiet := e.sbaQuietNow()
	if quiet && !sbaQuietVerify {
		e.checkGameOver()
		e.releasePendingDecisionOfDepartedPlayer()
		return
	}
	e.sbaQuiet = sbaQuietKey{}
	e.sbaUnquiet = false
	// The fused per-permanent prefilter (rules/sba_prefilter.go): an action
	// whose flag is down would find nothing on the board.
	facts := &sbaBoardFacts{at: -1}
	for pass := 0; pass < maxSBAPasses; pass++ {
		tried.skips = 0
		changed := e.checkLoseConditions(tried)
		if e.sbaFacts(facts).counterPair && e.annihilateOppositeCounters() {
			changed = true
		}
		if e.destroyLethalDamage(tried, facts) {
			changed = true
		}
		if e.legendBatch != nil {
			// The CR 704.5j legend choice is parked WITH the batch it belongs
			// to (rules/sba.go parkLegendChoice): stop the pass loop so the
			// parked batch's board stays the board the controller chooses
			// against (CR 704.3) and nothing applies under the outstanding
			// ask. The answer's own Submit tail re-runs every remaining SBA.
			if quiet {
				e.verifySBAQuiet(ep0)
			}
			return
		}
		// CR 704.5k: the world rule. Deterministic -- it poses no choice, so
		// it sits with the other automatic SBAs rather than on the parked-
		// batch channel the legend rule uses. It runs only once no legend
		// batch is parked (the halt above), so a pass that parks a legend ask
		// settles it before applying the world rule on the answer's next pass.
		if e.sbaFacts(facts).world && e.worldRule() {
			changed = true
		}
		if e.sbaFacts(facts).pw && e.planeswalkerZeroLoyalty(tried) {
			changed = true
		}
		if e.sbaFacts(facts).battle && e.battleZeroDefense(tried) {
			changed = true
		}
		if e.ceaseDeadTokens(tried) {
			changed = true
		}
		if e.sbaFacts(facts).combat && e.removeNoncreatureCombatants() {
			changed = true
		}
		if e.sbaFacts(facts).attach && e.attachmentSBAs() {
			changed = true
		}
		if e.sbaFacts(facts).saga && e.checkSagas(tried) {
			changed = true
		}
		// CR 704.5t: a dungeon whose marker sits on its bottommost room and
		// whose room ability has left the stack is completed. It runs after
		// checkSagas in the pass, both are deterministic scans that pose no
		// choice, and neither reads the other's writes.
		if e.dungeonCompletion(tried) {
			changed = true
		}
		if !changed {
			stable = true
			break
		}
	}
	if quiet {
		e.verifySBAQuiet(ep0)
	}
	if !stable {
		e.emit(events.Event{Kind: events.Note,
			Text: "state-based actions did not reach a fixed point within the pass budget"})
	} else {
		e.sbaRecordQuiet(ep0, tried.skips == 0)
	}
	e.checkGameOver()
	e.releasePendingDecisionOfDepartedPlayer()
}

// legendGroup is one CR 704.5j duplicate set: two or more legendary permanents
// with the same name under ONE controller, in battlefield scan order. The
// controller of the set chooses which member survives; the rest go to their
// owners' graveyards.
type legendGroup struct {
	player state.PlayerID
	name   string
	ids    []state.ObjID
}

// legendBatch is one parked CR 704.5j application: the duplicate set whose
// controller is choosing which member to keep, the lethal-damage/toughness
// casualties destroyLethalDamage found in the same SBA pass (they are applied
// only when the answer lands, so the WHOLE batch observes one pre-batch board,
// CR 704.3), and that board -- the same immutable trigger look-back snapshot
// cmdZoneMove.before shares. Plain value data (casualty values, object-id
// slices, the shared immutable snapshot), so Clone deep-copies the slices
// (clone.go) the way it deep-copies cmdZone.
type legendBatch struct {
	group  legendGroup
	dead   []casualty
	before *triggerSnapshot
}

// legendGroups collects CR 704.5j's duplicate sets: if two or more legendary
// permanents with the same name are controlled by the same player, the set's
// controller chooses one and the rest are put into their owners' graveyards.
// "Legendary" is a CHARACTERISTIC the layer system can change --
// CopyPermanent's NonLegendary$ True strips the supertype at layer 4 -- so
// the check reads the DERIVED type list (typeCharacteristics), never just the
// printed face: a non-legendary copy of a legend must not be gathered against
// its original. Sets with a single member are not duplicates and are dropped.
// A permanent a live `S:Mode$ IgnoreLegendRule` static exempts (Council of
// Reeds' "The 'legend rule' doesn't apply to creatures you control") never
// joins a set at all: exempt members are skipped before grouping, so two
// exempt permanents form no duplicate set and a set that loses members to an
// exemption can fall below the two-member threshold and drop entirely.
// The scan is deterministic (AliveFrom(0) seat order, each battlefield zone a
// slice, seen keyed on the current derived name), so the event stream is
// reproducible run to run; membership maps are never iterated.
func (e *Engine) legendGroups(mayPair bool) []legendGroup {
	// The exemption statics are collected once, in activeStatics' canonical
	// deterministic order, and reused for every candidate; each candidate is
	// matched with the static's own source/controller context so
	// `Creature.YouCtrl` is scoped to the static's controller, not the
	// duplicate set's. mayPair is mayHaveLegendPair's answer, which the
	// caller's fused board scan (sbaBoardFacts.legend) already holds.
	if !mayPair {
		return nil
	}
	exempt := e.activeStatics("IgnoreLegendRule")
	var all []legendGroup
	for _, p := range e.G.AliveFrom(0) {
		seen := make(map[string]int)
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil || !o.Face().IsLegendary() {
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so it is not a duplicate legend.
				continue
			}
			if !legendaryUnderLayers(e, id) {
				continue
			}
			if e.legendRuleExempt(exempt, id) {
				continue
			}
			name := e.Name(id)
			if gi, ok := seen[name]; ok {
				all[gi].ids = append(all[gi].ids, id)
				continue
			}
			seen[name] = len(all)
			all = append(all, legendGroup{player: p, name: name, ids: []state.ObjID{id}})
		}
	}
	var groups []legendGroup
	for _, g := range all {
		if len(g.ids) >= 2 {
			groups = append(groups, g)
		}
	}
	return groups
}

// legendRuleExempt reports whether one of the given live IgnoreLegendRule
// statics exempts the permanent from CR 704.5j. Each static's own "as long
// as" gate runs first through the shared continuousGateHolds grammar -- the
// same IsPresent$/IsPresent2$/PresentCompare$/CheckSVar$/Condition$ evaluator
// every other static consumer uses -- so a conditional exemption is live only
// while its condition holds. Brothers Yamazaki and Syr Joshua and Syr Saxon
// both require EXACTLY two permanents named on the battlefield
// (`IsPresent$ ... | PresentCompare$ EQ2`), so a third copy must NOT inherit
// the exemption; without this gate the statics would exempt unconditionally.
// Then each static's `ValidCard$` is evaluated against the candidate with
// `staticSpecCtx`, so `YouCtrl` and the rest of the player-relative grammar
// resolve against THAT static's source and controller (Council of Reeds
// exempts the creatures its own controller controls, not every creature on
// the board). An absent `ValidCard$` is Forge's "all cards" spelling --
// Mirror Gallery's unconditional "The legend rule doesn't apply." -- so it
// exempts every candidate. statics is passed in already collected
// (legendGroups calls activeStatics once) to keep the walk to a single
// deterministic pass.
func (e *Engine) legendRuleExempt(statics []staticView, id state.ObjID) bool {
	if len(statics) == 0 {
		return false
	}
	for _, sv := range statics {
		if !e.continuousGateHolds(sv) {
			continue
		}
		spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard))
		if spec == "" || e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
			return true
		}
	}
	return false
}

// parkLegendChoice parks the whole SBA batch -- the duplicate set, the lethal
// casualties found in the same pass, and the pre-batch look-back board -- and
// asks the set's controller which member to keep, in ONE step. The atomicity
// is the point: nothing is applied under an outstanding ask, so the board the
// controller chooses against is exactly the pre-batch board the casualties
// were scanned against (CR 704.3), and the parked state is always exactly
// "batch found, choice pending". The options are offered in battlefield scan
// order, so the deterministic bot's clamp fallback (option 0) keeps the
// battlefield-order first -- the survivor the pre-decision-channel build
// always picked. Only called when e.pending is nil and no flow owns e.choosing
// (destroyLethalDamage guards this); a pose is never stranded without its ask.
func (e *Engine) parkLegendChoice(g legendGroup, dead []casualty) {
	e.legendBatch = &legendBatch{
		group:  g,
		dead:   append([]casualty(nil), dead...),
		before: e.snapshotTriggerBoard(),
	}
	e.askLegendChoice()
}

// askLegendChoice poses the CR 704.5j choice for the parked batch's duplicate
// set to its controller, in battlefield scan order (so the deterministic bot's
// clamp fallback keeps the battlefield-order first -- the survivor the
// pre-decision build always picked). It is the ONE construction site for the
// ask, shared by parkLegendChoice (the fresh pose) and checkStateBased's
// recovery re-pose (a parked batch whose decision was displaced): the two
// must offer an identical decision, or a recovered ask would accept an answer
// the original never offered.
func (e *Engine) askLegendChoice() {
	g := e.legendBatch.group
	if int(g.player) < len(e.G.Players) && e.G.Players[g.player].Lost {
		// CR 800.4a: a player who has left the game makes no choices. The
		// departed controller's CR 704.5j choice is therefore unexercised and
		// declines deterministically to the battlefield-order first member --
		// the same outcome parkCommanderZoneMove gives a departed commander
		// owner. Applying rather than asking also means a parked batch can
		// never re-pose to a seat that can no longer answer it.
		e.applyLegendBatch(g.ids[0])
		return
	}
	opts := make([]decision.Option, len(g.ids))
	for i, id := range g.ids {
		opts[i] = decision.Option{Index: i, Kind: "keep", Label: g.name, Obj: id, Player: g.player}
	}
	d := &decision.Decision{Player: g.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt:  "Choose which " + g.name + " to keep; the rest are put into their owners' graveyards",
		Options: opts}
	e.choosing = chooseLegend
	e.ask(d)
}

// legendAnswer applies an answered CR 704.5j choice: the kept member is
// recorded through a Choose "legend_keep" event (a log marker, like riot's --
// the outcome itself is the MoveZone events below, so a log-only replay
// reproduces both branches), the parked lethal casualties are applied first
// with the parked pre-batch board (a KEPT member keeps its lethal-damage
// destruction path, so a regeneration shield can still save it -- exactly the
// treatment the pre-decision build gave its scan-order survivor), and the
// non-kept members go to their owners' graveyards as legend-rule departures
// (placement, not destruction -- no regeneration, no destruction replacement).
// The Submit tail's next checkStateBased pass re-runs every SBA on the settled
// board, so a further duplicate set is parked and asked there and any SBA the
// moves themselves caused is picked up. An answer with no batch parked (only
// reachable from a hand-built decision -- every real ask parks one) degrades
// to a Note, the same totality stance as handleCmdZone.
func (e *Engine) legendAnswer(d *decision.Decision, in decision.Intent) {
	b := e.legendBatch
	if b == nil {
		e.legendBatch = nil
		e.choosing = chooseNone
		e.emit(events.Event{Kind: events.Note, Player: in.Player,
			Text: "legend-rule decision answered with no batch parked"})
		return
	}
	kept := b.group.ids[0]
	if chosen := d.Chosen(in); len(chosen) == 1 {
		kept = chosen[0].Obj
	}
	e.applyLegendBatch(kept)
}

// applyLegendBatch settles the parked CR 704.5j batch with the given member
// kept: the kept member is recorded through a Choose "legend_keep" event (a
// log marker, like riot's -- the outcome itself is the MoveZone events below,
// so a log-only replay reproduces both branches), the parked lethal casualties
// are applied first with the parked pre-batch board (a KEPT member keeps its
// lethal-damage destruction path, so a regeneration shield can still save it),
// and the non-kept members go to their owners' graveyards as legend-rule
// departures (placement, not destruction -- no regeneration, no destruction
// replacement). It is the ONE application site, shared by legendAnswer (an
// answered choice) and askLegendChoice's departed-controller decline (CR
// 800.4a), so the two paths can never settle a batch differently. The Submit
// tail's next checkStateBased pass re-runs every SBA on the settled board, so
// a further duplicate set is parked and asked there and any SBA the moves
// themselves caused is picked up.
func (e *Engine) applyLegendBatch(kept state.ObjID) {
	b := e.legendBatch
	e.legendBatch = nil
	e.choosing = chooseNone
	if b == nil {
		return
	}
	e.emit(events.Event{Kind: events.Choose, Obj: kept,
		Counter: "legend_keep", Player: b.group.player})
	nonKept := make(map[state.ObjID]bool, len(b.group.ids))
	for _, id := range b.group.ids {
		if id != kept {
			nonKept[id] = true
		}
	}
	before := e.triggerBefore
	e.triggerBefore = b.before
	for _, c := range b.dead {
		// A non-kept duplicate's departure is serialized by the legend rule,
		// not by its own lethal damage -- the same single-serialization
		// discipline the pre-decision batch used for a member that was both.
		if nonKept[c.id] {
			continue
		}
		if c.text == "lethal damage" && effects.ReplaceDestruction(e, c.id) {
			continue
		}
		// Umbra armor (CR 702.90) after the regeneration shield, the same
		// deterministic shield-first stand-in the Destroy effects use. A
		// bearer saved here has had all its damage removed inside the
		// replacement, so the next sweep cannot re-kill it.
		if c.text == "lethal damage" && effects.ReplaceUmbraArmor(e, c.id) {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: c.id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: c.text})
	}
	for _, id := range b.group.ids {
		if nonKept[id] {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: state.ZBattlefield, To: state.ZGraveyard, Text: "legend rule"})
		}
	}
	e.triggerBefore = before
}

// legendaryUnderLayers reports whether the object's DERIVED type list still
// carries the Legendary supertype. A printed legend whose layer-4 effects
// strip it (CopyPermanent's NonLegendary$) is not legendary for CR 704.5j.
func legendaryUnderLayers(e *Engine, id state.ObjID) bool {
	for _, t := range e.typeCharacteristics(id, 0) {
		if strings.EqualFold(t, "Legendary") {
			return true
		}
	}
	return false
}

// worldUnderLayers reports whether the object's DERIVED type list still
// carries the World supertype. "World" is a characteristic the layer system
// can change (CR 613.1c AddTypes$/RemoveCardTypes$ at layer 4), so the world
// rule reads the derived list exactly as the legend rule reads Legendary -- a
// printed World permanent whose layers stripped the supertype is not a World
// permanent for CR 704.5k, and a printed non-World permanent a layer-4 effect
// granted it to is.
func worldUnderLayers(e *Engine, id state.ObjID) bool {
	for _, t := range e.typeCharacteristics(id, 0) {
		if strings.EqualFold(t, "World") {
			return true
		}
	}
	return false
}

// worldPermanents returns every permanent on the battlefield that carries the
// World supertype, GLOBALLY across all controllers, in a deterministic order:
// seat order (AliveFrom(0)), then each seat's battlefield zone in scan order.
// CR 704.5k's scope is global -- the rule counts every world permanent, not
// just one controller's -- and the departure order must not depend on a map
// iteration, so the result is one flat slice in that fixed order.
func (e *Engine) worldPermanents() []state.ObjID {
	var out []state.ObjID
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			if !worldUnderLayers(e, id) {
				continue
			}
			out = append(out, id)
		}
	}
	return out
}

// worldRule applies CR 704.5k: "If two or more permanents have the supertype
// world, all except the one that has had the world supertype for the shortest
// amount of time are put into their owners' graveyards. In the event of a tie
// for the shortest amount of time, all are put into their owners'
// graveyards."
//
// The rule poses NO choice -- unlike the CR 704.5j legend rule it is fully
// deterministic -- so it is an automatic SBA, not a parked ask. It is called
// directly from checkStateBased's pass loop (beside planeswalkerZeroLoyalty),
// not through a tried-set batch helper: it reads the whole battlefield every
// pass and converges because each call leaves at most one World permanent, so
// the next pass sees <2 and returns false. It does not consume the same-pass
// lethal casualties the tried-set batches carry, because a World enchantment
// is almost never a lethal casualty and nothing about CR 704.5k depends on it.
// With fewer than two world permanents it falls through false. Otherwise it
// finds the YOUNGEST permanent(s) by entry order: Object.Timestamp is assigned from
// Game.Clock on every battlefield entry and increases monotonically, so the
// greatest Timestamp is the most recently entered permanent, the one that has
// had the world supertype for the shortest amount of time. If exactly one
// permanent holds the maximum it survives and every other world permanent is
// put into its owner's graveyard; a tie for the maximum (two permanents that
// entered at the same instant, so neither has held it longer) puts ALL of them
// into their owners' graveyards.
//
// APPROXIMATION (recorded in the commit message and the ticket report, not in
// AGENTS.md): the engine tracks when a permanent ENTERED, not when it gained
// the world supertype. For the printed World permanents the two coincide (they
// have the supertype as they enter); a layer-4 effect that GRANTS World to an
// older permanent makes "shortest amount of time" really mean "most recently
// granted", which would need a supertype-gain timestamp the engine does not
// record. Entry order is therefore the honest stand-in, and the tie case still
// fires exactly as the CR requires.
//
// The departures are placements, not destruction: an ordinary MoveZone
// battlefield->graveyard with Text "world rule", so no regeneration or
// destruction-replacement applies (the same treatment the legend rule gives
// its non-kept members).
func (e *Engine) worldRule() bool {
	if !e.mayHaveWorldPair() {
		return false
	}
	worlds := e.worldPermanents()
	if len(worlds) < 2 {
		return false
	}
	newest := worlds[0]
	newestCount := 0
	for _, id := range worlds {
		o := e.G.Obj(id)
		n := e.G.Obj(newest)
		switch {
		case o.Timestamp > n.Timestamp:
			newest, newestCount = id, 1
		case o.Timestamp == n.Timestamp:
			newestCount++
		}
	}
	// A unique youngest survives; a tie for the youngest spares none. Every
	// permanent that is not the unique survivor departs, in the deterministic
	// scan order worldPermanents established.
	//
	// CR 704.3/603.10a: every departure in this simultaneous batch observes the
	// SAME pre-departure board, including sources already serialized into the
	// graveyard -- exactly the snapshot discipline destroyLethalDamage,
	// planeswalkerZeroLoyalty and battleZeroDefense follow. Without it a
	// departing World permanent's leaves-the-battlefield trigger is matched
	// against the live board and misses a sibling's simultaneous departure
	// (3 queued triggers where CR 603.10a requires 4 when two World permanents
	// tie and both depart; TestWorldRuleTieSendsAllUsesPreDepartureBoard pins
	// it). The snapshot never receives mutations and the log retains ordinary
	// MoveZone events.
	before := e.triggerBefore
	own := e.snapshotTriggerBoard()
	e.triggerBefore = own
	defer e.closeTriggerWindow(own, before)
	changed := false
	for _, id := range worlds {
		if newestCount == 1 && id == newest {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: "world rule"})
		changed = true
	}
	return changed
}

// chooseLegend is the CR 704.5j legend-rule controller choice (rules/sba.go),
// the one decision an SBA poses outside a resolution -- the commander-zone
// and Siege asks share the property through their replacement parks. 44 is
// the next free value after chooseTokenReplace (43); the numbers matter only
// inside the package's switch tables.
const chooseLegend chooseFor = 44

// annihilateOppositeCounters applies CR 704.5q to permanents in fixed seat
// and battlefield order. Both removals are events so replay reconstructs the
// same counter state as the live game.
func (e *Engine) annihilateOppositeCounters() bool {
	changed := false
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so no state-based action sees it.
				continue
			}
			n := min(o.Counter("P1P1"), o.Counter("M1M1"))
			if n <= 0 {
				continue
			}
			e.emit(events.Event{Kind: events.CounterChange, Obj: id,
				Counter: "P1P1", Amount: -n})
			e.emit(events.Event{Kind: events.CounterChange, Obj: id,
				Counter: "M1M1", Amount: -n})
			changed = true
		}
	}
	return changed
}

// checkLoseConditions applies CR 704.5a (life 0 or less) to every player not
// already marked Lost, in fixed seat order (Players is a slice walked by
// index, never a map, so simultaneous eliminations always emit in the same
// order run to run). It then separately sweeps every Lost player -- from
// life loss just above, from an empty-library draw (effects.DrawFor, which
// emits PlayerLost directly, independent of this loop, at the moment the
// draw fails), or from any future cause -- and applies CR 800.4a: their
// owned cards and controlled battlefield/stack objects cease to exist.
// Doing this generically, keyed only on p.Lost rather than on which branch
// just set it, gives every kind of departure the same cleanup path.
//
// Reports whether anything changed, which is what checkStateBased's own
// pass loop uses to decide whether to run again.
//
// Ruling T22-h (fix round 1), superseded by T22-n (fix round 3, see
// checkStateBased): the first fix measured the removal sweep's own outcome
// (the zone's length before versus after the departure sweep) rather than
// reporting "changed" just because the zone was non-empty going in -- right
// for bounding a replacement's amplification, but, like destroyLethal-
// Damage's own T22-h before T22-j, it threw away the fact that an attempt
// happened at all. tried.players is this loop's memory of that, exactly
// mirroring destroyLethalDamage's tried.objs: a player already swept this
// call is skipped entirely on later passes (one sweep attempt per player
// per checkStateBased call, never re-run for no reason), while "changed"
// now means "swept a player NOT already swept this pass" -- true the moment
// a new sweep is attempted, regardless of whether the departure sweep's own
// MoveZone events actually moved anything, so a later pass still gets to
// see whatever a blocking replacement's own substitute effect changed
// elsewhere.
//
// Ruling T22-p (fix round 4): the rearm sits BETWEEN the two loops, not at
// the top of the function. The elimination this pass's own first loop just
// marked is exactly the one that can have made a previous pass's blocked
// sweep worth retrying -- and doing it here rather than at the top of the
// next pass also means a player whose ward has just gone inert is swept in
// the same checkStateBased call, not the one after. Sweeping in the second
// loop then records the attempt under the alive set it was actually made
// under, so this pass's own new sweeps are not re-armed by this pass's own
// eliminations.
func (e *Engine) checkLoseConditions(tried *sbaAttempts) bool {
	changed := false
	for i := range e.G.Players {
		p := &e.G.Players[i]
		if !p.Lost && p.Life <= 0 {
			if e.playerLoses(p.ID, loseReasonLifeReachedZero, "life total is 0 or less") {
				changed = true
			}
		}
	}
	// CR 704.5b: a player with ten or more poison counters loses. Ward's
	// AddCounterYou<N/POISON> cost emits PlayerCounterChange, so this belongs
	// in the shared SBA pass rather than in Ward's payment implementation.
	for i := range e.G.Players {
		p := &e.G.Players[i]
		if !p.Lost && p.Counter("POISON") >= 10 {
			if e.playerLoses(p.ID, loseReasonPoisoned, "ten or more poison counters") {
				changed = true
			}
		}
	}
	// CR 903.10 (commander damage, Task m33): a player that has been dealt 21
	// or more COMBAT damage by the same commander over the course of the game
	// loses. This is a state-based action exactly like the life loss above,
	// checked with all the others in the same fixed seat order (reusing
	// PlayerLost means the loss itself is processed exactly the way a life
	// loss is -- same event, same removal sweep below, same GameOver). It is
	// gated on the construction format so that NOTHING here runs in a
	// non-Commander game: a Constructed-format Config has no Commander book-
	// keeping at all (cmdDamage is nil), so the loop over it is skipped
	// wholesale. Commanders record damage per-commander, so the slice is
	// walked as two indices -- candidate player, then that player's commander
	// slots -- in the same deterministic order a life check uses.
	if e.format == FormatCommander {
		for i := range e.G.Players {
			p := &e.G.Players[i]
			if p.Lost {
				continue
			}
			for _, dmg := range p.CmdDamage {
				if dmg >= 21 {
					if e.playerLoses(p.ID, loseReasonCommanderDamage,
						"commander damage (21 or more from one commander)") {
						changed = true
					}
					break
				}
			}
		}
	}
	tried.rearm(e.G.AliveCount())
	for i := range e.G.Players {
		p := &e.G.Players[i]
		if !p.Lost {
			continue
		}
		if tried.players[p.ID] {
			tried.skips++
			continue
		}
		markTried(&tried.players, p.ID)
		// A sweep that emitted no event at all (every departed object already
		// ceased -- the steady state of every call after the first once a
		// seat has left) changed nothing, so it is not "new work" for the
		// fixed-point test: the extra pass it used to force re-ran every other
		// SBA over an unchanged board and found nothing. Any emission at all
		// -- including a blocking replacement's own substitute effect, the
		// T22-n case -- still reports changed.
		n0 := len(e.L.Events)
		e.ceaseDepartedObjects(p.ID)
		if len(e.L.Events) != n0 {
			changed = true
		}
	}
	return changed
}

// ceaseDepartedObjects applies CR 800.4a and 800.4e in stable arena order. A
// departed player's card-backed owned objects leave regardless of their current
// zone; stack objects that player controls cease whether card-backed or
// cardless. Ownership is deliberate on the battlefield: an opponent-owned card
// does not leave merely because the departed player controlled it. ZCeased
// retains the replay-stable arena tombstone without adding it to a zone.
//
// Moving an owned attacker clears its combat state, so that case continues
// before the 800.4e check rather than emitting a redundant EndCombatReset. The
// owner check also prevents that duplicate when multiple departed players are
// swept in seat order and an attacker owned by a later seat attacks an earlier
// one.
func (e *Engine) ceaseDepartedObjects(p state.PlayerID) {
	// CR 800.4a: the departed player's objects leave simultaneously, so every
	// battlefield departure in this sweep observes ONE pre-sweep board -- the
	// batch discipline destroyLethalDamage and the world rule follow. Without
	// it emit snapshots the whole game per departing permanent, which is
	// quadratic in a large board (60,001 tokens in
	// TestLargeEliminationSweepDoesNotTripLivelockWatcher took minutes).
	if e.triggerBefore == nil && e.ceaseSweepLeavesBattlefield(p) {
		own := e.snapshotTriggerBoard()
		e.triggerBefore = own
		defer e.closeTriggerWindow(own, nil)
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZCeased {
			continue
		}
		ownedCard := o.Owner == p && o.Card != nil
		controlledStackObject := o.Controller == p && o.Zone == state.ZStack
		if ownedCard || controlledStackObject {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID,
				From: o.Zone, To: state.ZCeased, Text: "player left the game"})
			continue
		}
		if !o.IsAttacking || int(o.Attacking) >= len(e.G.Players) || !e.G.Players[o.Attacking].Lost {
			continue
		}
		if o.Card != nil && int(o.Owner) < len(e.G.Players) && e.G.Players[o.Owner].Lost {
			continue
		}
		e.emit(events.Event{Kind: events.EndCombatReset, Obj: o.ID})
	}
}

// ceaseSweepLeavesBattlefield reports whether ceaseDepartedObjects(p) will
// move at least one permanent off the battlefield (the only departures whose
// triggers look back at the pre-departure board).
func (e *Engine) ceaseSweepLeavesBattlefield(p state.PlayerID) bool {
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Owner == p && o.Card != nil {
			return true
		}
	}
	return false
}

// casualty is one creature destroyLethalDamage has found lethal, together
// with which CR 704.5 clause is why -- Ruling T22-i (fix round 1): 704.5f
// (toughness <= 0) and 704.5g (lethal damage/deathtouch) are rules-distinct
// (only one of them is actually "destruction", so only one of them is
// something Indestructible or a replacement effect keyed on Destroy could
// ever apply to), and the log should be able to tell them apart even though
// Text carries no rules weight of its own.
type casualty struct {
	id   state.ObjID
	text string
}

// destroyLethalDamage performs the two CR 704.5 state-based actions combat
// damage depends on: a creature with toughness 0 or less (704.5f) is
// put into its owner's graveyard (not destroyed), and a creature with
// damage marked on it greater than or equal to its toughness, or with any
// damage at all from a source with Deathtouch (704.5g, via the Deathtouched
// counter damageStep marks), is destroyed unless Indestructible.
//
// This is general state-based-action logic, not combat-specific -- a
// creature killed by an ordinary DealDamage spell dies here exactly the same
// way one killed in combat does -- which is why it lives here rather than in
// combat.go, where Task 21 first added it.
//
// The Indestructible check reads the same way effDestroy/effDestroyAll
// (effects/zone.go) already do; it is not one of the eight keywords Task 21
// registers as implemented, so a card that actually carries it still reads
// as unsupported for deck-routing purposes, but a creature that is
// Indestructible for some other already-working reason should not
// spuriously die to lethal damage.
//
// Every candidate is found in one pass before any of them actually leaves
// (dead is collected, then destroyed), so one creature's departure this same
// pass can never retroactively change whether another one here counts as
// lethal, matching CR 704.3's "state-based actions are performed
// simultaneously" for one round of this check.
//
// Ruling T22-h (fix round 1), superseded by T22-j (fix round 2, see
// checkStateBased): the first fix reported whether a candidate actually
// left the battlefield, not whether a MoveZone was emitted for it --
// correct for bounding the amplification a replacement causes, but it threw
// away the fact that an attempt happened at all. tried.objs is this
// function's memory of that, shared across every pass of one
// checkStateBased call (constructed once there, passed down here each
// pass, never rebuilt per pass): an object already in it is skipped
// entirely -- not re-examined, not re-emitted -- and "changed" now means
// "found and attempted an object NOT already tried this pass", which is
// true exactly once per object per call regardless of whether the
// attempt actually moved it. That is what lets a later pass's
// checkLoseConditions see whatever a replacement's own substitute effect
// changed (T22-j's fix), while still bounding this function to one attempt
// per object per checkStateBased call (T22-h's fix, preserved): the object
// will not be found lethal-and-new again until the NEXT checkStateBased
// call, whether or not it actually left -- unless a player has been
// eliminated in the meantime, which is Ruling T22-p's one re-arm and the
// reason for the rearm call below (an elimination during THIS function's
// own emits, from a substitute effect that decks a player out, is picked up
// by the next pass's rearm rather than mid-loop).
func (e *Engine) destroyLethalDamage(tried *sbaAttempts, facts *sbaBoardFacts) bool {
	tried.rearm(e.G.AliveCount())
	var dead []casualty
	anyLType := e.activeHasLType()
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if tried.objs[id] {
				tried.skips++
				continue
			}
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so lethal damage does not destroy it.
				continue
			}
			f := o.Face()
			// CR 702.114e: a bestowed-attached card is an Aura, not a creature,
			// so the creature SBAs (lethal damage/toughness) do not hit it.
			// CR 702.150c: the same for an attached Reconfigure card (not a
			// creature while attached -- marked damage does not destroy it).
			if f == nil || o.BestowedAttached() || o.ReconfiguredAttached() {
				continue
			}
			// Creature-ness for both sweeps below is the LAYER-DERIVED type
			// list, not the printed face: CR 704.5f/g read the permanent's
			// current types after all continuous effects. A non-creature
			// permanent an animation effect made a creature (an earthbent land,
			// a crewed Vehicle, an animated artifact) must be swept, or it is
			// immortal to both zero toughness and lethal damage. A creature
			// that LOST Creature to a layer effect is correctly not swept.
			// This is the same predicate combat legality uses
			// (rules/combat.go), and for a face-down permanent
			// typeCharacteristics already bases the derived set on
			// FaceDownTypeWords (CR 708.5/Yedora), so the previous face-down
			// special case gave the same answer through it.
			if !e.sbaIsCreature(o, f, anyLType) {
				continue
			}
			if e.Toughness(id) <= 0 {
				dead = append(dead, casualty{id, "toughness <= 0"})
				continue
			}
			// CR 704.5g reads "damage from a source with deathtouch", not
			// "marked damage": the Deathtouched mark can stand alone when the
			// damage was dealt in COUNTER form (infect, CR 702.90b -- a 1/1
			// deathtouch infect creature deals its 1 as a -1/-1 counter and
			// nothing is marked), so the mark alone is lethal. The mark is
			// emitted only alongside damage that actually landed (both emit
			// sites guard on the applied amount) and is cleared in the same
			// cleanup block that clears marked damage, so a mark with no
			// damage and no counter-form hit behind it is unreachable.
			dtMark := o.Counter("Deathtouched")
			if o.Damage <= 0 && dtMark == 0 {
				continue
			}
			if o.Damage < e.Toughness(id) && dtMark == 0 {
				continue
			}
			if e.hasKeywordH(id, kwhIndestructible) {
				continue
			}
			dead = append(dead, casualty{id, "lethal damage"})
		}
	}
	// CR 704.5j: a duplicate legendary set asks its controller which member
	// to keep. The ask is parked ATOMICALLY with the whole batch (see
	// parkLegendChoice): nothing below applies while the choice is pending,
	// and checkStateBased returns as soon as the park exists. If a pose is
	// impossible (a decision is already outstanding -- a re-entrant pass
	// after another SBA parked its own ask), the legends are left un-binned
	// AND the lethal batch is left unapplied: no board change happens under
	// an outstanding ask, and the pass after the answer re-scans everything.
	if groups := e.legendGroups(e.sbaFacts(facts).legend); len(groups) > 0 {
		if e.pending == nil && e.choosing == chooseNone && e.legendBatch == nil {
			e.parkLegendChoice(groups[0], dead)
			return true
		}
		// Deferred on a runtime input (an outstanding decision): the pass
		// loop's no-op here is not a function of the board alone.
		e.sbaUnquiet = true
		return false
	}
	if len(dead) == 0 {
		return false
	}
	// CR 704.3/603.10a: every departure in this batch observes the SAME
	// pre-batch board, including sources that have already been serialized
	// into the graveyard. Replacement/prevention still decides which moves
	// actually occur; only those actual events are matched. The snapshot
	// never receives mutations, and the log retains ordinary MoveZone events.
	before := e.triggerBefore
	own := e.snapshotTriggerBoard()
	e.triggerBefore = own
	defer e.closeTriggerWindow(own, before)
	for _, c := range dead {
		markTried(&tried.objs, c.id)
		if c.text == "lethal damage" && effects.ReplaceDestruction(e, c.id) {
			continue
		}
		// Umbra armor (CR 702.90) after the regeneration shield, the same
		// deterministic shield-first stand-in the Destroy effects use. A
		// bearer saved here has had all its damage removed inside the
		// replacement, so the next sweep cannot re-kill it.
		if c.text == "lethal damage" && effects.ReplaceUmbraArmor(e, c.id) {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: c.id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: c.text})
	}
	return len(dead) > 0
}

// tokenCasualty is a token ceaseDeadTokens found to have left the
// battlefield, together with the zone it left FROM -- captured before the
// move, mirroring casualty above and the same real-zone-over-claimed-zone
// discipline events.Move applies to the removal itself.
type tokenCasualty struct {
	id   state.ObjID
	from state.Zone
}

// planeswalkerZeroLoyalty performs CR 704.5i: a planeswalker with loyalty 0
// or less is put into its owner's graveyard (not destroyed -- the modern CR
// has no planeswalker-uniqueness SBA; the legend rule already handles
// legendary walkers through legendCasualties). The walker's face carries its
// starting loyalty, the Move-zone-change grant (events/apply.go, CR 306.5b)
// put it on, and spell/ability damage removes counters from it (CR 306.8,
// effects/damage.go); between the three, loyalty can only reach zero through
// damage or a face whose printed starting loyalty is literally 0 (6 corpus
// files), and either way this sweep is what removes the walker.
//
// Discipline follows destroyLethalDamage's: a tried-set (shared objs -- a
// walker is never a creature, so destroyLethalDamage can never mark the same
// object for "lethal damage" and the two actions cannot disagree over a
// membership map), one move per object per call, the deterministic
// AliveFrom(0) seat / battlefield-slice order so the event stream is
// reproducible, and -- fixed in review round 2 -- the same pre-departure
// triggerBefore board snapshot around the whole move loop, so simultaneous
// zero-loyalty departures observe each other (CR 603.10a);
// TestPlaneswalkerSBABatchUsesPreDepartureBoard pins it. The move is not
// destruction (no ReplaceDestruction, no regeneration -- the same treatment
// "toughness <= 0" gets).
//
// A face whose printed starting loyalty this engine cannot READ (absent, or
// Loyalty:X -- Nissa, Steward of Elements, 2 corpus files) never reaches the
// sweep: CR 704.5i needs a known loyalty of 0 or less, and an unreadable one
// is not known-zero. Such a walker keeps its place on the battlefield with no
// loyalty counters -- the stand-in is recorded in AGENTS.md.
func (e *Engine) planeswalkerZeroLoyalty(tried *sbaAttempts) bool {
	tried.rearm(e.G.AliveCount())
	var dead []casualty
	// stat:IgnorePlaneswalkerZeroLoyaltyRule (Sanctum Lurker): collected
	// lazily, once per sweep, only when a zero-loyalty walker is found.
	var exempt []staticView
	exemptRead := false
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if tried.objs[id] {
				tried.skips++
				continue
			}
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so zero loyalty does not bin it.
				continue
			}
			f := o.Face()
			if f == nil || !f.IsPlaneswalker() {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(f.Loyalty))
			if err != nil || n < 0 {
				continue
			}
			if o.Counter("LOYALTY") > 0 {
				continue
			}
			if !exemptRead {
				exempt, exemptRead = e.activeStatics("IgnorePlaneswalkerZeroLoyaltyRule"), true
			}
			if e.zeroLoyaltyExempt(exempt, id) {
				continue
			}
			dead = append(dead, casualty{id, "zero loyalty"})
		}
	}
	if len(dead) == 0 {
		return false
	}
	// CR 704.3/603.10a: every departure in this batch observes the SAME
	// pre-departure board, including sources that have already been serialized
	// into the graveyard -- exactly the snapshot discipline destroyLethalDamage's
	// batch below follows (review finding r2 on this task: without it, when two
	// walkers reach zero loyalty in the same pass the walker moved earlier in
	// the loop is already gone from the battlefield when the later move is
	// matched, so its leaves-the-battlefield trigger misses the sibling's
	// departure -- 3 queued triggers where CR 603.10a requires 4;
	// TestPlaneswalkerSBABatchUsesPreDepartureBoard pins it). The snapshot
	// never receives mutations, and the log retains ordinary MoveZone events.
	before := e.triggerBefore
	own := e.snapshotTriggerBoard()
	e.triggerBefore = own
	defer e.closeTriggerWindow(own, before)
	for _, c := range dead {
		markTried(&tried.objs, c.id)
		e.emit(events.Event{Kind: events.MoveZone, Obj: c.id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: c.text})
	}
	return true
}

// battleZeroDefense is the defeat state-based action for battles (CR
// 704.5h/310.11): a battle with no defense counters is DEFEATED -- exiled,
// not put into the graveyard -- and its owner's "may cast it transformed
// without paying its mana cost" offer (CR 310.11) is queued by Engine.emit's
// exile feed for startDefeatedCast to pose at the next step(). Modeled
// line-for-line on planeswalkerZeroLoyalty above (CR 704.5i): same tried-set,
// same AliveFrom(0) battlefield-slice determinism, same pre-departure
// trigger-board snapshot so a batch of battles reaching zero in one pass all
// observe the same board (CR 704.3/603.10a). The move is not destruction: no
// ReplaceDestruction, no regeneration shield consulted -- exactly the
// zero-loyalty/zero-toughness treatment.
func (e *Engine) battleZeroDefense(tried *sbaAttempts) bool {
	tried.rearm(e.G.AliveCount())
	var dead []casualty
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			if tried.objs[id] {
				tried.skips++
				continue
			}
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so zero defense does not defeat it.
				continue
			}
			// A face-down card is a vanilla 2/2 creature (CR 708.5), never a
			// Battle: Face() returns the printed front face regardless of
			// FaceDown (and the entry grant above grants a face-down entry
			// no defense counters), so without this guard a manifested or
			// cloaked Battle would be swept out of the battlefield by the
			// defeat SBA the
			// instant it entered.
			if o.FaceDown {
				continue
			}
			f := o.Face()
			if f == nil || !f.IsBattle() {
				continue
			}
			if o.Counter("DEFENSE") > 0 {
				continue
			}
			dead = append(dead, casualty{id, "defeated"})
		}
	}
	if len(dead) == 0 {
		return false
	}
	before := e.triggerBefore
	own := e.snapshotTriggerBoard()
	e.triggerBefore = own
	defer e.closeTriggerWindow(own, before)
	for _, c := range dead {
		markTried(&tried.objs, c.id)
		e.emit(events.Event{Kind: events.MoveZone, Obj: c.id,
			From: state.ZBattlefield, To: state.ZExile, Text: c.text})
	}
	return true
}

// ceaseDeadTokens is CR 704.5d: a token ceases to exist the instant it
// leaves the battlefield. The state-based action is a plain zone check --
// any token whose CURRENT zone is neither the battlefield nor the stack nor
// already ceased is moved to ZCeased, the replay-stable arena tombstone with
// no game-zone membership, with Text "ceased to exist". The stack is excluded
// for the same reason Ephemeral's own IsCopy half exists: a token copy of a
// spell or ability legitimately sits there without being a permanent yet
// (Task 13 does not implement CopySpellAbility, so no card can produce this
// today, but the exclusion costs nothing and matches the brief exactly).
//
// Task 13 fix round 1 (review finding "minor 1"): this now takes the same
// tried memory checkLoseConditions and destroyLethalDamage do, in its own
// tried.tokens map (see sbaAttempts' own doc for why it cannot share
// tried.objs). The original version relied only on the zone update itself
// -- moving a token to ZCeased changes its own zone to the value the check
// excludes, so it naturally stops matching -- which is correct for the
// ordinary case and remains exactly how a SUCCESSFUL move retires itself
// here. What it did not bound was a replacement PERMANENTLY blocking the
// move (some "cards can't leave your graveyard" effect intercepting the
// move FROM graveyard, say): with no memory of the attempt, that token
// would be rediscovered and re-attempted on every single one of the 32
// passes in the budget, every checkStateBased call, forever -- exactly the
// amplification T22-h/T22-j/T22-n exist to prevent for the other two
// passes. tried.tokens closes the same gap here: an object already
// attempted this call is skipped on later passes, "changed" means
// "attempted something NEW this pass" (true the moment a new attempt
// happens, regardless of whether the move actually lands, mirroring
// destroyLethalDamage's own convention), and tried.rearm at the top gives a
// blocked attempt one more chance the instant the alive-player set shrinks,
// the same as its siblings.
//
// Walks e.G.Objs by index -- the dense arena, never a map -- so multiple
// tokens dying at once cease in a fixed, reproducible order.
func (e *Engine) ceaseDeadTokens(tried *sbaAttempts) bool {
	tried.rearm(e.G.AliveCount())
	var dead []tokenCasualty
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		// The field test first: the attempt memory is only consulted for a
		// token that would otherwise be ceased (the same set as before).
		if o.IsToken && o.Zone != state.ZBattlefield && o.Zone != state.ZStack && o.Zone != state.ZCeased {
			if tried.tokens[o.ID] {
				tried.skips++
				continue
			}
			dead = append(dead, tokenCasualty{o.ID, o.Zone})
		}
	}
	for _, c := range dead {
		markTried(&tried.tokens, c.id)
		e.emit(events.Event{Kind: events.MoveZone, Obj: c.id,
			From: c.from, To: state.ZCeased, Text: "ceased to exist"})
	}
	return len(dead) > 0
}

// checkGameOver ends the game once at most one seat remains (CR 104.2a's
// "the last player left in the game wins", CR 104.4a's draw when nobody is).
// A concession is a way to be Lost (M2d-3 emits the same PlayerLost the
// life-loss path does), so a conceded game ends here exactly like one whose
// losers lost to life or the library.
// Ruling T22-b: this used to default the winner to seat 0 whenever nobody
// survived -- PlayerID's zero value is a real seat, so an unconditional
// `w := state.PlayerID(0)` silently crowned it every time elimination was
// simultaneous. There is no winner in that case; Amount: 1 tells Apply's
// GameOver case to record a draw (Game.Draw) instead of a winner.
func (e *Engine) checkGameOver() {
	// New deals opening hands as one genesis operation. Losses and every
	// other SBA are already real at that point, but the terminal GameOver is
	// deferred until New has emitted the public toss Note; GameOver must stay
	// last for the host's persisted burst-boundary contract.
	if e.deferGameOver || e.G.Over {
		return
	}
	alive := e.G.AliveFrom(0)
	if len(alive) > 1 {
		return
	}
	if len(alive) == 1 {
		w := alive[0]
		// The CR 104.2a last-player-standing win is a win like any other, so a
		// live GameWin CantHappen replacement for the survivor stops it (CR
		// 104.3's "can't win"; Herald of Eternal Dawn, Platinum Angel). This
		// runs after checkLoseConditions has swept every departed seat's
		// objects, so a replacement whose source has just left with its
		// controller no longer applies -- which is why a concession still ends
		// the game (the Angel leaves the game, then the survivor wins).
		if !e.playerWins(w, e.G.Players[w].Name) {
			return
		}
	} else {
		e.emit(events.Event{Kind: events.GameOver, Amount: 1, Text: "draw"})
	}
	e.pending = nil
}

// dropDepartedFlow ends the flow that was waiting on d, the released decision
// of a player who has left the game. That player makes no further choices
// (CR 800.4a) and every stack object they controlled has ceased
// (ceaseDepartedObjects), so a cast they were proposing cannot continue, and
// a KChoose's routing marker -- with the mana-ability continuation it guarded
// -- belongs to nobody now. Left in place, both survive into the next seat's
// priority: the proposal as a cast "in flight", the marker read by the next
// KChoose answer's dispatch and by the legend-rule SBA gate.
func (e *Engine) dropDepartedFlow(d *decision.Decision) {
	if e.cast != nil && (e.cast.player == d.Player || e.cast.player == d.Acting()) {
		e.cast = nil
		e.deferredPush, e.deferredPushLKI = nil, nil
		if e.choosing == chooseCast {
			e.choosing = chooseNone // the dropped cast's own marker
		}
	}
	if d.Kind != decision.KChoose {
		return
	}
	switch e.choosing {
	case chooseMana:
		e.manaActivation = nil
	case chooseManaColor:
		e.manaColorActivation = nil
	case chooseManaDiscard, chooseManaExile, chooseManaSacrifice:
		e.manaDiscardActivation = nil
	case chooseManaUnless:
		e.manaUnlessActivation = nil
	}
	e.choosing = chooseNone
}

// releasePendingDecisionOfDepartedPlayer keeps a decision asked of a player
// who has since left the game from stranding the match. Only that player may
// answer it, Advance does nothing at all while e.pending is set, and with
// three or more seats the elimination does not end the game -- so without
// this the one goroutine running the match waits forever on an answer that
// can never come. With two seats checkGameOver above has already cleared
// e.pending, so this is the three-or-more case.
//
// Fix round 1, review finding F2: this used to fire only for Task 27's two
// trigger kinds, on the stated grounds that they were the only decisions
// asked from inside handle -- Submit runs handle, then this, then Advance, so
// a decision asked from inside handle is the only one state-based actions run
// underneath. That premise was FALSE, and the review traced the
// counterexample: handlePriority (legal.go) -> castSpell (stack.go) ->
// askTarget asks a KTarget decision from inside handle too. It is now
// deliberately keyed on nothing but "the player cannot answer", because
// enumerating which decisions can reach this state is precisely the reasoning
// that was wrong the first time.
//
// The continuation is resumeTriggerDrain for every kind, and it is the right
// one for all of them: state-based actions to a fixed point, then any queued
// triggers onto the stack, then priority to the next player who can actually
// take it. Re-entering priorityRound instead of resumeTriggerDrain would put
// a second checkStateBased call on priorityRound's own path, where step() has
// already reached the same fixed point -- giving a replacement-blocked
// state-based action a second attempt per step and moving sba.go's own
// measured firing counts (Ruling T22-p; see resumeTriggerDrain's header in
// turn.go). (Ruling T28-b: this used to say re-entering priorityRound "would
// re-run the draw step's draw" -- true before Task 28 moved the draw out of
// priorityRound entirely; the reason above is what actually survives that
// move.)
//
// This cannot loop. dropDepartedTriggers has already discarded the departed
// player's own triggers (CR 800.4a); an optional trigger whose DECIDER
// departed but whose controller lives is declined rather than re-asked; and
// e.pending is cleared before either continuation runs. If resuming a stack
// resolution reaches another ask, that ask sets e.pending before
// resumeTriggerDrain, whose pending guard makes the drain inert rather than
// overwriting the new decision or recursing through checkStateBased.
func (e *Engine) releasePendingDecisionOfDepartedPlayer() {
	d := e.pending
	if d == nil || e.G.Over {
		return
	}
	// A CR 722-redirected decision is stranded when EITHER seat departed:
	// the answering controller, or the controlled seat it acts for.
	departed := func(p state.PlayerID) bool { return int(p) < len(e.G.Players) && e.G.Players[p].Lost }
	if int(d.Player) >= len(e.G.Players) || !(departed(d.Player) || departed(d.Acting())) {
		return
	}
	e.pending = nil
	e.dropDepartedFlow(d)
	if e.resume != nil {
		// CR 800.4f: the departed player does not make the outstanding
		// choice. Resume with an empty answer so the asking instruction gets
		// no selection (or no payment) and the rest of the effect chain still
		// runs. Clearing only resume would leave the half-resolved object on
		// the stack, where a later resolution could replay its completed
		// prefix; moving the object off the stack here would instead discard
		// any suffix after the unanswered instruction. resumeResolution runs
		// the remaining instructions and then performs CR 608.2n's ordinary
		// completion tail.
		rp := e.resume
		e.resume = nil
		e.resumeResolution(rp, nil)
	}
	e.resumeTriggerDrain()
}
