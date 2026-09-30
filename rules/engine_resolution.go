package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// engineResolution groups the Engine's in-flight resolution context, its LKI/answer-cursor maps and the replacement windows. It is embedded by value in
// Engine (rules/engine_struct.go), so every field keeps its documented
// contract comment and every existing e.<field> access keeps compiling
// unchanged through Go's field promotion. Clone's per-field copy
// classes (rules/clone.go) are unchanged by the move.
type engineResolution struct {
	// fusedResolving is the target slice of the fused half whose resolution is
	// CURRENTLY running (rules/split.go's runFusedHalves), set around the
	// whole of that half's effects.Resolve -- the half's root SA and every
	// sub-ability in its chain -- and restored afterwards. fusedResolvingSet
	// is the presence bit: a half whose own ValidTgts$ produced an empty
	// slice is still a fused half whose sub-abilities must read that empty
	// list, never the stack object's flat one. Ask captures the pair onto the
	// pending resumePoint, so a mid-resolution ask posed by ANY frame of the
	// half (its root, a SubAbility$, a loop body) resumes with the half's own
	// targets rather than both halves' (Flesh // Blood's DBPutCounter reads
	// ParentTargeted$CardPower off this binding). Transient scratch, cleared
	// when the half's resolve returns: rebuilt identically by replay.
	fusedResolving    []state.Target
	fusedResolvingSet bool
	// fusedResolvingSVars is the SVar table of the fused half whose resolution
	// is currently running -- the ALTERNATE half's table when Blood is the
	// frame, never the object's front-face table. A fused spell keeps FaceIdx
	// 0, so o.Face().SVars is the FRONT half's table and a resumed alternate
	// half's sub reading its own SVar (Blood's NumDmg$ Y = Y:ParentTargeted$
	// CardPower) would resolve against the wrong table. Set and restored
	// alongside fusedResolving, captured by Ask onto the resumePoint. Nil
	// outside a fused half's resolution.
	fusedResolvingSVars map[string]string
	// resolvingTargetControllerLKI is the target-controller snapshot of the
	// Resolve chain whose effect is CURRENTLY running, published by
	// effects.Resolve through Host.SetResolutionTargetControllerLKI around
	// the whole chain and restored on return. Ask captures it onto the
	// pending resumePoint (Engine.Ask), so a resumed continuation -- which
	// rebuilds its Ctx from the already-reset live objects -- restores the
	// controller a target had at the start of resolution (a target destroyed
	// before a chained TokenOwner$ TargetedController resolves). Transient
	// scratch: rebuilt identically by replay, nil outside a chain.
	resolvingTargetControllerLKI map[state.ObjID]state.PlayerID
	// resolutionCtx is the live Ctx of the Resolve chain whose effect is
	// CURRENTLY running, published by effects.Resolve through the optional
	// resolutionCtxHost interface around the whole chain and restored on
	// return. It is the one home of the chain's in-flight TargetUnique$
	// accumulator: Engine.Ask reads resolutionCtx.TargetsUnique and stamps it
	// onto every decision whose own resume state did not carry it, so an
	// intervening ask of ANY kind (a modal election, a ward pay, a
	// dig/scry/arrange pick) preserves the picks earlier TargetUnique$ riders
	// chose at the resumed Ctx's rebuild. Transient scratch: rebuilt
	// identically by replay, nil outside a chain (combat, mulligan and other
	// non-resolution asks).
	resolutionCtx *effects.Ctx
	// resolvingFlipMemory is the coin-flip memory of the Resolve chain whose
	// effect is CURRENTLY running, published by effects.Resolve (and by
	// effFlipCoin when it lazily allocates the memory) through the optional
	// Host.SetResolutionFlipMemory seam and restored on return. Ask captures it
	// onto the pending resumePoint, so a resumed continuation re-attaches the
	// SAME pointer and a chained Defined$ FlippedTails / Wins reader keeps
	// every flip performed before the suspension. Transient scratch: rebuilt
	// identically by replay, nil outside a chain or before any flip.
	resolvingFlipMemory *effects.FlipMemory
	// resolvingExchangeMemory is the ExchangeLife rider memory of the Resolve
	// chain whose effect is CURRENTLY running, published by effects.Resolve
	// (and by effExchangeLife when it lazily allocates the memory) through
	// the optional Host.SetResolutionExchangeMemory seam and restored on
	// return. Ask captures it onto the pending resumePoint, so a resumed
	// continuation re-attaches the SAME pointer and a chained
	// Count$RememberedNumber reader keeps the value the exchange transaction
	// settled after the suspension. Transient scratch: rebuilt identically by
	// replay, nil outside a chain or before any exchange rider.
	resolvingExchangeMemory *effects.ExchangeMemory
	// villainousRemembered is the victim of the VillainousChoice whose chosen
	// body is CURRENTLY resolving, kept as ambient engine state for the
	// duration of that body's effects.Resolve — the fusedResolving pattern.
	// A nested ask the body poses captures it through Ask onto the pending
	// resumePoint (and buildContinuationChain stamps it onto the body's
	// continuation frames), so the nested ask's re-entry still resolves
	// Defined$ Remembered / Player.IsRemembered to the victim rather than
	// rebuilding the trigger's own capture. villainousRememberedSet is the
	// presence bit (a victim set is never empty, but the bit keeps the "no
	// villainous body in flight" case explicit). Transient scratch,
	// restored with the same defer discipline as fusedResolving; rebuilt
	// identically by replay.
	villainousRemembered    []state.Target
	villainousRememberedSet bool
	// windowPaidX is the X the triggered-cost window's payment announced
	// (rules/cumulative.go's X fold, tc.xPaid at the pay arm), kept as AMBIENT
	// engine state while the paid body resolves — the fusedResolving pattern:
	// rules/resolution.go's resumeResolution arms it from the frame's
	// rp.winPaidX around the re-entry's effects.Resolve, Ask captures it onto
	// every pending resumePoint it poses, and buildContinuationChain stamps it
	// onto the continuation frames — so a body that suspends on a
	// mid-resolution ask (Leyline Tyrant's "pay any amount of {R}" death
	// trigger, whose DB$ DealDamage target pick is exactly such an ask)
	// resumes with its X instead of rebuilding ctx.X from a trigger object
	// that was never paid one (0). Transient scratch, restored with the same
	// defer discipline as fusedResolving; rebuilt identically by replay.
	windowPaidX int32
	// exploitedLKI maps an EXPLOITED creature's object id to the LKI snapshot
	// of it at the instant it was sacrificed to pay an exploit (CR 702.58a),
	// published by effects/exploit.go through Host.RememberExploitedLKI while
	// the resolving marker holds Ctx.Sacrificed. The events.Exploit marker
	// carries only the exploited id, and Move has already cleared the
	// creature's counters and battlefield layers by emit time, so this map is
	// what lets a trig:Exploited body read TriggeredExploited$CardPower/
	// CardToughness as last-known information (rules' attachExploitedLKI).
	// Engine-only and replay-derived like the other LKI maps: replay re-runs
	// the same effect resolution, so it repopulates identically, and the entry
	// is removed when the exploited object leaves a zone.
	exploitedLKI map[state.ObjID]state.SacrificedInfo
	// sourceLifelinkLKI maps an independently resolving ability's stack object
	// to its source permanent's derived lifelink state at the last moment that
	// source existed on the battlefield. The map's presence is the validity
	// bit: false is authoritative LKI too. It is captured before a source is
	// sacrificed as its own activation cost, refreshed for already-stacked and
	// pending abilities when their source later departs, cloned at intent
	// boundaries, and removed with the stack object. Resolution copies it into
	// effects.Ctx; effects uses it only when the source is no longer live.
	sourceLifelinkLKI map[state.ObjID]bool
	// sourceControllerLKI is the matching pre-departure controller snapshot.
	// It is separate from sourceLifelinkLKI because false lifelink is still a
	// valid snapshot, and a controller may be seat zero.
	sourceControllerLKI map[state.ObjID]state.PlayerID
	// damageSourceLKI carries snapshots keyed first by the waiting stack
	// object and then by a departed named DamageSource$ object. Unlike the
	// own-source maps above, every waiting resolution receives departures: the
	// named source can be TriggeredCard, Targeted, or Remembered.
	damageSourceLKI map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI
	// moveCounterAsk carries a MoveCounter resolution's ANSWERED asks across
	// the later suspensions of the same SA (the movecounter1 livelock fix).
	// A MoveCounter sub the placement/announcement ask never covered poses
	// its own ValidTgts$ target ask (the mvts1 pre-ask) AND, for
	// CounterType$ Any / CounterNum$ Any, asks of its own; every resume
	// builds a fresh Ctx and re-enters the SA from its top, so an earlier
	// round's answer (the target set, the chosen kind, the chosen amount)
	// must be re-seeded into that Ctx or the two asks alternate forever and
	// the resolution never drains (Nesting Grounds, Rikku, Goldberry's
	// second ability). rules/resolution.go's "tgts", "move_counter_kind"
	// and "move_counter" arms store their answers here and the re-entry
	// seeds them into the fresh Ctx before effects.Resolve; the entry is
	// deleted when the resolution completes. Decision-derived engine
	// scratch, in the triggerLKI discipline: replay re-submits the recorded
	// Intents through the same arms, so the map re-derives identically and
	// no event carries it. Never nil-checked on read outside recordAsk
	// (which lazy-inits).
	moveCounterAsk map[state.ObjID]*moveCounterPending
	// aorAsk carries an AddOrRemoveCounter resolution's ANSWERED per-kind
	// elections across the later suspensions of the same SA (the
	// moveCounterAsk discipline — counterchoice1). An EachExistingCounter$
	// walk (Dramatist's Puppet, Quarry Hauler) asks one add/remove election
	// per counter kind; every resume builds a fresh Ctx, so without this map
	// an already-answered PUT kind (whose counter count is still positive and
	// therefore still enumerates) would be re-asked forever. rules/
	// resolution.go's "aor_elect" arm records the answered kind here and the
	// re-entry seeds it into Ctx.AorAnswered; the entry is deleted when the
	// resolution completes. Decision-derived engine scratch, in the
	// moveCounterAsk discipline: replay re-submits the recorded Intents
	// through the same arms, so the map re-derives identically and no event
	// carries it.
	aorAsk map[state.ObjID]map[string]bool
	// counterTypeAsk carries per-recipient comma-list PutCounter answers across
	// suspensions. It is replay-derived engine scratch, never game state.
	counterTypeAsk map[state.ObjID]*counterTypePending
	// targetsPickAsk carries an ANSWERED generic ValidTgts$ pre-ask (the
	// mvts1 "tgts" arm) across a LATER suspension of the same SA, for every
	// API -- the general form of the moveCounterAsk cursor above, which
	// solved exactly this for MoveCounter alone. chosenTargetsFor CONSUMES
	// Ctx.TargetsPick before dispatching the body (fx42 scoping, so a nested
	// SA cannot inherit it), and every resume builds a FRESH Ctx; so if the
	// body then suspends on an ask of its own, the next resume re-enters the
	// SA from its top with no answer, re-poses the pre-ask, and the two asks
	// alternate forever. Kozilek's Command is the live carrier: its Charm
	// picks DBScry alongside another targeting mode, so the stack object's
	// one undivided target list is not DBScry's player, the pre-ask fires at
	// resolution, and the Scry's own KArrange is the second ask that loops
	// (arrange -> tgts -> arrange ...). Keyed by resolving stack object and
	// then by the SA's Line -- ResolveSVar parses fresh on every call, so
	// pointer identity never holds across a resume, the same matching
	// convention charmModeTarget and chosenTargetsFor's OfferedSA check use.
	// The per-SA key keeps one sub's answer off another sub's ask, and the
	// entry is deleted when THAT SA's resolution completes so a later
	// re-entry (a Repeat loop) asks afresh. Decision-derived engine scratch
	// in the moveCounterAsk discipline: replay re-submits the recorded
	// Intents through the same arm, so the map re-derives identically and no
	// event carries it.
	targetsPickAsk map[state.ObjID]map[string][]state.Target

	// replReplaced is the ev.Obj of the replacement applyReplacements is
	// currently resolving — the object the replaced event was about. It is
	// seeded by applyReplacements (Ctx.Replaced = ev.Obj) and read by Ask to
	// thread that object across a mid-resolution suspension: a ReplaceWith$
	// body that poses an ask needs Replaced (and Remembered = [that object],
	// and the X value) restored on the resume, or its Defined$ ReplacedCard
	// resolution and SVar:X Remembered$Amount gating see nothing and the
	// completed move never happens (fx44, Mox Diamond). Zero whenever no
	// replacement is in flight.
	replReplaced state.ObjID
	// replReplacedCards is the ordered plural batch (Ctx.ReplacedCards) of the
	// replacement currently resolving -- the cascade instruction's exiled
	// cards, the counterpart of replReplaced for Averna's Defined$
	// ReplacedCards selector. Ask captures it onto the resume point so a
	// ReplaceWith$ body that suspends at its hidden pick re-resolves
	// ReplacedCards.<qual> against the same batch. nil outside a Cascade
	// replacement.
	replReplacedCards []state.ObjID
	// cascadeResidue is the synthetic SA effCascade wants run after a Cascade
	// replacement body (bottom the non-found exiled cards, then the free-cast
	// election). It is scoped to one ProposeCascadeReplacement call, the same
	// scratch pattern as scrySA/scryTarget; nil outside one.
	cascadeResidue *cards.SA
	// replacingEvent is the in-flight Damage event a DB$ ReplaceEffect body's
	// ReplaceEvent call may rewrite (Amount/Affected). It exists only during
	// emit, before the event is logged, so it is never part of
	// cloned/replayed engine state.
	replacingEvent  *events.Event
	replacingSource state.ObjID
	// replRemembered is the in-flight replacement body's remembered referents,
	// visible to that one body and restored right after it, the same scratch
	// pattern as replacingEvent. ReplaceEvent carries no Ctx, so the
	// VarValue$ Remembered rewrite reads its binding here. Never part of
	// cloned/replayed engine state: it lives only during the body run, before
	// the held event is logged.
	replRemembered []state.Target
	// replAction is the action marker (events.ActionMarker) of the event the
	// in-flight destination-changing replacement discarded: "sacrificed",
	// "discarded" or "discarded as a cost". emit re-labels the replacement
	// body's move of replReplaced with it (events.CarryAction), so a
	// sacrifice or discard redirected by a replacement is still seen as that
	// action by Sacrificed/Discarded triggers. Empty whenever no such
	// replacement is in flight; threaded across a suspension by resumePoint.
	replAction string
	// replReplacedPlayer is the player a replaced DRAW event was about (the
	// draw-er), threaded the same way replReplaced threads the replaced
	// object: a ReplaceWith$ body over R:Event$ Draw poses mid-resolution
	// asks (Breathstealer's Crypt's unless-pay discard) and the resume must
	// restore Ctx.ReplacedPlayer. Only a Draw replacement sets it.
	replReplacedPlayer state.Target
	// replRedirect is the destination-changing ("Replaced") move replacement
	// whose ReplaceWith$ body is resolving, with every replacement already
	// applied to that event (CR 614.5). A body move of the same object to a
	// DIFFERENT zone is the modified event of CR 616.1f and gets one more
	// replacement pass that skips those (Engine.emit). Immutable once set;
	// threaded across a suspension by resumePoint.redirect; nil at every
	// intent boundary outside a suspended body.
	replRedirect *replRedirect
	// replExclude is the applied set replRedirect carried into that one
	// recheck pass: applyReplacementsDispatch drops those matches and
	// applyReplacement extends it for a nested redirect. Nil otherwise.
	replExclude []string
}
