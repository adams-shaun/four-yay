package rules

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Layer, Sublayer and ContinuousEffect moved to state/continuous.go in Task
// 19c, so effects primitives (which sit below rules and must never import
// it) can build a ContinuousEffect and hand it to this engine through
// effects.Host. These aliases and re-exported constants keep this package's
// own API -- and every existing caller and test in this package -- unchanged:
// only the canonical type definition moved, not its name or behaviour here.
type (
	Layer            = state.Layer
	Sublayer         = state.Sublayer
	ContinuousEffect = state.ContinuousEffect
)

const (
	LCopy      = state.LCopy
	LControl   = state.LControl
	LText      = state.LText
	LType      = state.LType
	LColor     = state.LColor
	LAbilities = state.LAbilities
	LPT        = state.LPT
)

const (
	SubNone     = state.SubNone
	SubCDA      = state.SubCDA
	SubSet      = state.SubSet
	SubModify   = state.SubModify
	SubCounters = state.SubCounters
	SubSwitch   = state.SubSwitch
)

// Derived is a permanent's current characteristics after every applicable
// continuous effect has been applied in CR 613 order. Nothing outside this
// file may read printed power, toughness or keywords directly — Derived (or
// the Power/Toughness/HasKeyword/Keywords accessors below) is the only path.
type derivedPTSnapshot struct {
	id                                         state.ObjID
	power, toughness, basePower, baseToughness int32
	// preCounterPower/preCounterToughness are the same running value WITHOUT
	// the layer-7d counters. A layer-7c static whose amount reads the P/T of
	// an object the walk is currently deriving (Snowblind's AddToughness$
	// -NotAttackingY, sized from the enchanted creature's own toughness) must
	// see the value before this effect; CR 613.4 orders counters after every
	// 7c modify, so that value excludes them while power/toughness -- the
	// counter-inclusive pair FilterDerivedPT hands to Count$Valid -- includes
	// them.
	preCounterPower, preCounterToughness int32
}

type Derived struct {
	Power, Toughness int32
	// BasePower/BaseToughness are the object's BASE power and toughness: the
	// value through layer 7b (CR 613.4) -- the printed or characteristic-
	// defining value, after a 7b setting effect, and BEFORE any 7c modify or
	// any 7d counter. The base filter predicates (`basePowerEQ1`,
	// `powerGTbasePower`) read these; a 7c pump or a +1/+1 counter must move
	// Power/Toughness but never BasePower/BaseToughness. A face-down
	// battlefield permanent's base is its CR 708.5 face-down P/T (or a
	// FaceDownPower$ override), the same basis the walk starts from.
	BasePower, BaseToughness int32
	Keywords                 []string
	Types                    []string
	// Name is the current layer-3 name. SetName$ overwrites the printed name.
	Name string
	// Text is the object's current CR 613.1d text: its printed Oracle text
	// ("" while a battlefield object is face down, CR 708.5) after every
	// applicable layer-3 effect in timestamp order -- a TextSet outright
	// replacement (api:ExchangeTextBox) then each TextFrom/TextTo
	// whole-word substitution (api:ChangeText). It is the one place the
	// engine renders an object's changed rules text.
	Text string
	// Colors is the object's current colour set as WUBRG letters (CR 613.1e):
	// its face's colours (effects.ColorsOf, which already applies Devoid)
	// then every applicable layer-5 effect in timestamp order -- an
	// OverwriteColors grant replaces the set so far, a plain one extends it.
	// "" is a colourless object, not "no read": a battlefield land reads "".
	Colors string
}

// AddContinuous registers one continuous effect. A zero Timestamp is
// stamped from the game clock, so callers that do not care about relative
// ordering against other effects created in the same instant need not touch
// the clock themselves; the layer tests that DO care set Timestamp
// explicitly and bypass this.
//
// Ruling T19-a: the clock advances only through a logged ClockTick event,
// never a direct write to e.G.Clock. Object.Timestamp (see events.Move) is
// stamped from this same clock whenever a permanent enters the battlefield,
// so a direct write here would leave a game reconstructed from the log alone
// off by one on every later Timestamp — the same bug class Ruling T11-a
// already fixed for Passes/Priority.
func (e *Engine) AddContinuous(ce ContinuousEffect) {
	if ce.SourceIncarnation == 0 && strings.EqualFold(strings.TrimSpace(ce.Affects), "Card.Self") {
		if source := e.G.Obj(ce.Source); source != nil && source.Zone == state.ZBattlefield {
			ce.SourceIncarnation = source.Incarnation
		}
	}
	if ce.Timestamp == 0 {
		e.emit(events.Event{Kind: events.ClockTick})
		ce.Timestamp = e.G.Clock
	}
	// A Duration$ that spans the controller's NEXT turn (UntilYourNextTurn /
	// UntilTheEndOfYourNextTurn) gets a real turn-boundary lifetime: compute
	// the turn at whose end the effect expires from the live rotation, so it
	// outlives its source (a one-shot spell is already off the battlefield by
	// the time it registered) and is dropped by EndOfTurnCleanup when e.G.Turn
	// reaches UntilTurn -- not at the end of the current turn (UntilEOT) nor
	// never (source-leaves). Recomputing it here each re-execution is what
	// makes the boundary byte-identical on replay. If the controller cannot
	// be found in the alive rotation (eliminated) the effect gets no turn
	// boundary and falls back to the source-leaves rule.
	if effects.IsNextTurnDuration(ce.Duration) && ce.UntilTurn == 0 {
		ce.UntilTurn = e.nextTurnFor(ce.Controller)
		// UntilYourNextTurn ends as that turn begins. Cleanup is the
		// preceding turn's boundary, while UntilTheEndOfYourNextTurn
		// remains active through the next turn's cleanup.
		if effects.IsUntilYourNextTurn(ce.Duration) && ce.UntilTurn > e.G.Turn {
			ce.UntilTurn--
		}
		// The frozen value is the FALLBACK, not the authority: expiry
		// (EndOfTurnCleanup, via rescheduleNextTurnBoundaries) re-derives the
		// boundary from the live rotation and pending extra-turn queue, so an
		// extra turn granted after this registration moves the boundary with
		// the controller's next actual turn.
	}
	e.continuous = append(e.continuous, ce)
	if ce.AddTrigger != nil || len(ce.GainedTriggerFaces) > 0 {
		e.trigGrant.free = false // trigger_grantfree.go
	}
	if ce.ReplacementEvent != "" {
		e.trigGrant.replSeen = true // trigger_grantfree.go
	}
	// A REGISTERED layer-3 rename (an Effect-delivered SetName$, which has no
	// printed static for the genesis pool probe to find) arms the rename
	// table for the rest of the match; see rules/setname.go.
	if ce.SetName != "" {
		e.setNameInPool = true
	}
	// A REGISTERED layer-4 type effect (an Effect-delivered Animate, an
	// activated manland animation -- neither has a printed static for the
	// genesis pool probe to find) arms the derived-type table for the rest of
	// the match; see rules/layer4types.go.
	if ce.Layer == LType {
		e.layer4InPool = true
	}
	// A REGISTERED "loses all abilities" effect ends the no-loss proof
	// (abilityloss.go).
	if ce.RemoveAbilities {
		e.lossProof.seen = true
	}
	// Bump the cache version: active() (below) caches its sorted effect list
	// on (log head, continuousVersion), and this is the write that changes
	// e.continuous. The ClockTick above moved the log head too, but naming
	// the dependency explicitly here keeps active()'s invalidation correct
	// even if a future caller adds a continuous effect without an event.
	e.continuousChanged()
}

// EndEffect ends the one continuous-effect registration identified by
// (source, stamp) -- the one-shot Effect self-exile (`DB$ ChangeZone |
// Defined$ Self | Origin$ Command | Destination$ Exile`) that Forge models by
// exiling the implicit effect object it keeps in the Command zone. It drops
// exactly the entries carrying that identity (the same identity the
// replacement key and applyReplaceDamageTail's shield depletion use), so a
// source's OTHER registrations and its printed abilities are untouched.
// Engine-runtime only, rebuilt by re-execution on replay exactly like every
// other continuous-registry write; it emits no event.
func (e *Engine) EndEffect(source state.ObjID, stamp uint32) {
	if source == 0 {
		return
	}
	kept := e.continuous[:0]
	changed := false
	for _, ce := range e.continuous {
		if ce.Source == source && ce.Timestamp == stamp {
			changed = true
			continue
		}
		kept = append(kept, ce)
	}
	if !changed {
		return
	}
	e.continuous = kept
	e.continuousChanged()
}

// EndEffectSource ends every Effect-created continuous-effect registration
// from the named source -- the source-scoped counterpart of EndEffect, used
// by the one-shot self-exile idiom when the resolving body carries a frame
// with no per-registration stamp (Ctx.EffectFrame{Source: src}): an Effect's
// OWN Triggers$ body (rules' delayed-trigger fire) or the chain of the
// spell/ability that registered the Effect. Only registrations marked
// state.ContinuousEffect.FromEffect are dropped, so the source's printed
// statics survive. Engine-runtime only, rebuilt by re-execution on replay
// exactly like EndEffect; it emits no event.
func (e *Engine) EndEffectSource(source state.ObjID) {
	if source == 0 {
		return
	}
	// The Effect's own lifetime also ends its EffectRepeat DELAYED-trigger
	// registrations (the |EF form): Out of Time's comeback trigger is a
	// registration, not a continuous effect, and its DBExileSelf body ends
	// the effect that owns it. Removal is logged (DelayedRemove) so a replay
	// folds the same registration set.
	var remove []uint32
	for i := range e.G.Delayed {
		if e.G.Delayed[i].Source == source && e.G.Delayed[i].EffectRepeat {
			remove = append(remove, e.G.Delayed[i].ID)
		}
	}
	for _, id := range remove {
		e.emit(events.Event{Kind: events.DelayedRemove, Amount: int32(id)})
	}
	kept := e.continuous[:0]
	changed := false
	for _, ce := range e.continuous {
		if ce.Source == source && ce.FromEffect {
			changed = true
			continue
		}
		kept = append(kept, ce)
	}
	if !changed {
		return
	}
	e.continuous = kept
	e.continuousChanged()
}

// EndImprintedEffects ends every live DB$ Effect registration that an
// ImprintOnHost$ True Effect imprinted on the named host card (the entries
// carrying state.ContinuousEffect.ImprintOnHost with that Source) -- the
// analogue of Forge's later `DB$ ChangeZone | Defined$ Imprinted | Origin$
// Command | Destination$ Exile` exiling the imprinted effect token from the
// Command zone (Superior Foes of Spider-Man's "until you exile another card
// with this creature": the second dig's trigger exiles the FIRST effect's
// token, ending its may-play grant, before the new dig's Effect registers;
// Word of Command and Semester's End run the same idiom inside one chain).
// A registration without the marker -- the same source's OTHER effects and
// its printed abilities -- is untouched. Engine-runtime only, rebuilt by
// re-execution on replay exactly like EndEffect; it emits no event of its
// own (the delayed-trigger half emits DelayedRemove so a replay folds the
// same registration set).
func (e *Engine) EndImprintedEffects(source state.ObjID) {
	if source == 0 {
		return
	}
	e.endImprintedDelayed(source)
	kept := e.continuous[:0]
	changed := false
	for _, ce := range e.continuous {
		if ce.Source == source && ce.ImprintOnHost {
			changed = true
			continue
		}
		kept = append(kept, ce)
	}
	if !changed {
		return
	}
	e.continuous = kept
	e.continuousChanged()
}

// nextTurnFor returns the turn number of the next turn (strictly after the
// current one) whose active player is p -- i.e. p's NEXT turn, the
// controller's-next-turn boundary of an UntilYourNextTurn effect.
// It walks pending extra turns and the alive rotation from the current
// active player, consuming pending skips in a local copy. It returns 0
// (no turn boundary) if p is not alive; a full cycle with no remaining
// skips consumed and no p turn proves there is no reachable boundary.
// ContinuousNamed reports whether an ACTIVE continuous effect registered by
// p carries the given name (an Effect's Name$): the ask effEffect's
// Stackable$ False dedup makes before it would register a second copy of the
// same named effect (Wrenn and Six's emblem). Scans active() so an expired
// effect never blocks a fresh registration.
func (e *Engine) ContinuousNamed(p state.PlayerID, name string) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Controller == p && ce.Name == name {
			return true
		}
	}
	return false
}

// rescheduleNextTurnBoundaries re-derives UntilTurn for every live
// next-turn-duration effect (Duration$ UntilYourNextTurn /
// UntilTheEndOfYourNextTurn) from the live rotation and the pending
// extra-turn queue. AddContinuous freezes the boundary at registration,
// which goes stale the moment a +1 ExtraTurn grant is emitted AFTER the
// effect began: the granted turn is inserted before the ordinary rotation
// (most recently created grant first), moving the controller's next actual
// turn either earlier (their own grant -- the boundary becomes the extra
// turn) or later (another seat's grant). SkipTurn grants can also move that
// boundary: a skipped turn has no TurnChange. A -1 ExtraTurn consumption
// converts an unskipped entry into an actual turn in lockstep, and a
// TurnChange advances the base the count starts from. EndOfTurnCleanup
// reschedules first thing, so the once-per-turn expiry decision sees every
// grant made during the turn now ending.
//
// The one case the strictly-after walk cannot see is an
// UntilTheEndOfYourNextTurn whose boundary turn is the CURRENT turn:
// nextTurnFor never returns the turn in progress, so a plain recompute
// would push the boundary past it and the effect would survive its own
// expiry forever. The tracked boundary names the current turn exactly when
// it is the controller's first turn since registration (the only way a
// turn-boundary effect is alive while its controller is active with the
// boundary not strictly future), so that value is kept. A start-boundary
// effect is never alive during its controller's turn -- it drops at the
// PRECEDING cleanup -- so the override cannot misfire on it. A controller
// the rotation cannot reach (eliminated; nextTurnFor returns 0) keeps the
// frozen registration-time value.
func (e *Engine) rescheduleNextTurnBoundaries() {
	changed := false
	for i := range e.continuous {
		ce := &e.continuous[i]
		if ce.UntilTurn == 0 || !effects.IsNextTurnDuration(ce.Duration) {
			continue
		}
		start := effects.IsUntilYourNextTurn(ce.Duration)
		if !start && e.G.Active == ce.Controller && ce.UntilTurn == e.G.Turn {
			continue // the boundary is the turn now being cleaned up
		}
		next := e.nextTurnFor(ce.Controller)
		if next == 0 {
			continue
		}
		b := next
		if start {
			b = next - 1
		}
		if b != ce.UntilTurn {
			ce.UntilTurn = b
			changed = true
		}
	}
	// GainControl's LoseControl$ UntilTheEndOfYourNextTurn carries the same
	// boundary in controlGrant.untilTurn (rules/control.go). A late +1 grant
	// moves it exactly as it moves a continuous effect's: the granted turn
	// can be the effect controller's next turn (their own grant -- the
	// boundary moves earlier) or insert turns before it (another seat's
	// grant -- the boundary moves later). The spelling is the END boundary
	// (no -1), and expireControl(controlAtCleanup) reads untilTurn AFTER this
	// reschedule, so a stale value would end the steal on the wrong cleanup.
	// The same current-turn override applies: nextTurnFor is strictly-after,
	// so an end-boundary grant already standing on the turn now being cleaned
	// up must keep it. A controller the rotation cannot reach (nextTurnFor
	// returns 0; RegisterControl already stored e.G.Turn for that case) keeps
	// its value.
	for i := range e.controlGrants {
		g := &e.controlGrants[i]
		if !g.Duration.NextTurn {
			continue
		}
		if e.G.Active == g.You && g.untilTurn == e.G.Turn {
			continue
		}
		next := e.nextTurnFor(g.You)
		if next == 0 {
			continue
		}
		if next != g.untilTurn {
			g.untilTurn = next
			changed = true
		}
	}
	if changed {
		// Same reason AddContinuous bumps: the boundary rewrite emits no
		// event and moves no log head, but active() caches on
		// continuousVersion.
		e.continuousVersion++
	}
}

func (e *Engine) nextTurnFor(p state.PlayerID) int32 {
	// Pending extra turns are taken before ordinary rotation, most recently
	// created first. Entries for eliminated players are consumed without a
	// turn, just as advanceStep does, so they must not advance the boundary.
	// The same is true of an entry whose R:Event$ BeginTurn | ExtraTurn$ True
	// | Skip$ True replacement makes the granted seat skip the turn
	// (rules/turn.go's advanceStep consumer emits the -1 consumption but never
	// calls beginTurn); nextTurnFor must apply the SAME skip decision the
	// consumer does, or a skipped grant for the controller expires the effect
	// one cleanup too early and a skipped opponent grant one cleanup too
	// late. An ordinary SkipTurn grant also consumes a slot without a
	// TurnChange, in BOTH the queued and normal-turn consumers. Simulate its
	// remaining counts locally so one skipped slot cannot erase every later
	// turn of that seat. Check it before extraTurnSkipped, matching advanceStep.
	t := e.G.Turn
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost {
		return 0
	}
	skips := make([]int, len(e.G.Players))
	for seat := range e.G.Players {
		skips[seat] = e.G.SkipTurns[state.PlayerID(seat)]
	}
	for i := len(e.G.ExtraTurnQueue) - 1; i >= 0; i-- {
		seat := e.G.ExtraTurnQueue[i].Player
		if e.G.Players[seat].Lost {
			continue
		}
		if skips[seat] > 0 {
			skips[seat]--
			continue
		}
		if skip, _ := e.extraTurnSkipped(seat); skip {
			continue
		}
		t++
		if seat == p {
			return t
		}
	}

	// Once the pending queue drains, ordinary rotation resumes after the
	// latest normal turn, not after the active extra turn. A full cycle
	// without consuming a skip or reaching p proves p cannot be reached.
	alive := e.G.AliveCount()
	q := e.rotationBase()
	for missed := 0; missed < alive; {
		q = e.G.NextAlive(q)
		if skips[q] > 0 {
			skips[q]--
			missed = 0
			continue
		}
		t++
		if q == p {
			return t
		}
		missed++
	}
	return 0
}

// EndOfTurnCleanup drops every "until end of turn" effect (CR 514.2), and
// every turn-boundary effect whose expiry turn is the one now ending. Called
// from rules/combat.go's cleanupStep, which runs it on entry to the cleanup
// step.
func (e *Engine) EndOfTurnCleanup() {
	// Re-derive the next-turn boundaries before the expiry walk below: a +1
	// ExtraTurn grant emitted after a next-turn effect was registered moves
	// the controller's next actual turn, and the frozen registration-time
	// value must not decide the expiry (see rescheduleNextTurnBoundaries).
	e.rescheduleNextTurnBoundaries()
	e.expireControl(controlAtCleanup)
	e.reconcileControlStatics()
	n0 := len(e.continuous)
	kept := e.continuous[:0]
	// expiredClones collects the clone UNITS whose LCopy marker this cleanup
	// drops, so the object's CopyFace basis can be settled after the kept
	// list is rewritten (task api-clone).
	//
	// A unit is keyed by (become object, expiry moment) rather than by the
	// become object alone: several clone units may be live on ONE permanent
	// (Mirage Mirror activated twice, a permanent copy plus a temporary one),
	// and dropping them all because one expired both wipes a still-live
	// unit's modifiers and destroys its copy. Two units that share the whole
	// key expire at the same instant by construction, so grouping by it can
	// never separate a marker from its own siblings nor merge two units whose
	// lifetimes differ.
	var expiredClones []cloneExpiry
	dropClone := func(ce ContinuousEffect) {
		k := cloneExpiryOf(&ce)
		for _, seen := range expiredClones {
			if seen == k {
				return
			}
		}
		expiredClones = append(expiredClones, k)
	}
	for _, ce := range e.continuous {
		// A Permanent one-shot survives cleanup (CR 611.2a). An LCopy
		// marker, however, is never left to the generic rules alone: an
		// UntilUnattached copy has no ordinary expiry field and must be
		// tested live.
		if ce.Layer == LCopy && ce.CloneTarget != 0 &&
			strings.EqualFold(strings.TrimSpace(ce.Duration), "untilunattached") {
			if o := e.G.Obj(ce.CloneTarget); o == nil || o.AttachedTo == 0 {
				dropClone(ce)
				continue
			}
		}
		if ce.Permanent {
			kept = append(kept, ce)
			continue
		}
		if ce.UntilEOT {
			if ce.Layer == LCopy && ce.CloneTarget != 0 {
				dropClone(ce)
			}
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ce.Duration), "untilendofcombat") {
			// CR 511.2: until-end-of-combat is an expired lifetime by the time
			// this turn's cleanup runs, so it is reclaimed here rather than
			// lingering in e.continuous forever.
			if ce.Layer == LCopy && ce.CloneTarget != 0 {
				dropClone(ce)
			}
			continue
		}
		if ce.UntilTurn != 0 && ce.UntilTurn == e.G.Turn {
			if ce.Layer == LCopy && ce.CloneTarget != 0 {
				dropClone(ce)
			}
			continue
		}
		kept = append(kept, ce)
	}
	if len(expiredClones) > 0 {
		// Drop the whole clone unit: the marker's sibling modifier effects
		// go with it. The match is the FULL unit key, not the become object,
		// so a second clone unit still live on the same permanent keeps its
		// own modifiers (the two-overlapping-clones defect).
		surviving := kept[:0]
		for _, ce := range kept {
			if ce.CloneTarget != 0 && cloneExpiryIn(expiredClones, cloneExpiryOf(&ce)) {
				continue
			}
			surviving = append(surviving, ce)
		}
		kept = surviving
	}
	e.continuous = kept
	// Settle each affected object's CopyFace basis AFTER e.continuous is
	// rewritten so the emitted ClonePermanent events cannot re-enter this
	// cleanup's list state (the emit below applies to G.Objs only). Each
	// settle is one event, so the hash chain records the expiry exactly as it
	// records the copy.
	e.settleExpiredClones(expiredClones)
	// Bump the version for the same reason AddContinuous does: the cache is
	// keyed on continuousVersion, and this in-place rewrite (which emits no
	// event and moves no log head) drops every UntilEOT pump and every
	// expired UntilTurn effect. Without the bump, a stale active() cache
	// would keep reporting a dead pump's P/T. kept is a subsequence of the
	// old list written over it in place, so an unchanged length is an
	// unchanged registry: nothing to invalidate (most turns end with no
	// effect to drop).
	if len(e.continuous) != n0 {
		e.continuousChanged()
	}
}

// cloneExpiry identifies ONE clone unit (task api-clone): the permanent that
// became a copy, together with the moment that copy's lifetime ends. A single
// permanent may carry several live clone units at once -- Mirage Mirror
// activated twice in a turn, or a permanent copy under a temporary one -- and
// every effect a unit registers (the layer-1 LCopy marker and its layer-4/5/6/7
// modifier siblings) is registered with the SAME lifetime fields, so this key
// separates the units without any per-unit identifier riding the effects.
//
// Two units that share the whole key expire at the same instant, so treating
// them as one is behaviourally identical; two units whose lifetimes differ
// differ in at least one field, so one can never drop the other.
type cloneExpiry struct {
	Target         state.ObjID
	Duration       string
	UntilEOT       bool
	UntilTurn      int32
	DurationTarget state.ObjID
}

func cloneExpiryOf(ce *ContinuousEffect) cloneExpiry {
	return cloneExpiry{Target: ce.CloneTarget,
		Duration:  strings.ToLower(strings.TrimSpace(ce.Duration)),
		UntilEOT:  ce.UntilEOT,
		UntilTurn: ce.UntilTurn, DurationTarget: ce.CloneDurationTarget}
}

func cloneExpiryIn(keys []cloneExpiry, k cloneExpiry) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}

// expireClonesOnEvent ends copy effects at their event-driven boundaries.
// The marker owns expiry; its sibling modifiers carry the same unit key.
// Run after Apply so the replay-visible re-base follows the untap/turn-down.
func (e *Engine) expireClonesOnEvent(ev events.Event, wasTapped bool) {
	var expired []cloneExpiry
	for _, ce := range e.continuous {
		if ce.Layer != LCopy || ce.CloneTarget == 0 {
			continue
		}
		dur := strings.ToLower(strings.TrimSpace(ce.Duration))
		match := dur == "untilfacedown" && ev.Kind == events.TurnFaceDown && ev.Obj == ce.CloneTarget ||
			dur == "untiltargeteduntaps" && ev.Obj == ce.CloneDurationTarget &&
				(ev.Kind == events.Untap && wasTapped || ev.Kind == events.MoveZone && ev.From == state.ZBattlefield)
		if match && !cloneExpiryIn(expired, cloneExpiryOf(&ce)) {
			expired = append(expired, cloneExpiryOf(&ce))
		}
	}
	if len(expired) == 0 {
		return
	}
	kept := e.continuous[:0]
	for _, ce := range e.continuous {
		if ce.CloneTarget != 0 && cloneExpiryIn(expired, cloneExpiryOf(&ce)) {
			continue
		}
		kept = append(kept, ce)
	}
	e.continuous = kept
	e.continuousChanged()
	e.settleExpiredClones(expired)
}

// settleExpiredClones rewrites the CopyFace basis of every permanent whose
// clone units this cleanup just dropped from e.continuous.
//
// The basis is a SINGLE field on the object (state.Object.CopyFace) while a
// permanent may carry several clone units, so an expiry cannot simply clear
// it: the object must be re-based onto whichever unit is still live. CR
// 613.1a applies copy effects in timestamp order, so the survivor that wins
// is the highest-timestamp LCopy marker left for that object; with none left
// the basis is cleared, which is the single-unit case and therefore emits
// exactly the event stream this cleanup emitted before overlapping units were
// modelled (heads unmoved for every game with at most one copy per object).
//
// The re-base is one ClonePermanent naming the survivor's own source, name
// and GainThisAbility$ rider, so it goes through events.Apply like every
// other state change and a replay derives the identical face.
func (e *Engine) settleExpiredClones(expired []cloneExpiry) {
	if len(expired) == 0 {
		return
	}
	// Deterministic order: the expiry keys are collected in e.continuous scan
	// order, and each object is settled once, on its first appearance.
	var done []state.ObjID
	for _, k := range expired {
		if k.Target == 0 || objIDIn(done, k.Target) {
			continue
		}
		done = append(done, k.Target)
		var survivor *ContinuousEffect
		for i := range e.continuous {
			ce := &e.continuous[i]
			if ce.Layer != LCopy || ce.CloneTarget != k.Target {
				continue
			}
			if survivor == nil || ce.Timestamp > survivor.Timestamp {
				survivor = ce
			}
		}
		// Clear first, unconditionally. With no survivor that is the whole
		// settle (the single-unit case, byte-identical to the pre-overlap
		// build). With one, it puts the object back on its PRINTED face
		// before the re-base, which is what the survivor's copy is taken
		// against -- notably GainThisAbility$, whose fold appends the become
		// object's own current face abilities and would otherwise append the
		// EXPIRING copy's.
		e.emit(events.Event{Kind: events.ClonePermanent, Obj: k.Target})
		if survivor == nil {
			continue
		}
		ev := events.Event{Kind: events.ClonePermanent, Obj: k.Target,
			IDs: []state.ObjID{survivor.CloneSource}, Player: survivor.Controller,
			Text: survivor.CloneName}
		if survivor.CloneChosenName != "" {
			ev.Counter = "chosen-name"
			ev.Text = survivor.CloneChosenName
		} else if survivor.CloneGainThisAbility {
			if survivor.CloneTriggerIndex > 0 {
				ev.Counter = "gain-this-trigger"
				ev.Amount = survivor.CloneTriggerIndex
			} else {
				ev.Counter = "gain-this-ability"
				ev.Amount = survivor.CloneAbilityIndex
			}
		}
		e.emit(ev)
		for _, raw := range survivor.CloneStaticBodies {
			e.emit(events.Event{Kind: events.CloneStatic, Obj: k.Target, Text: raw})
		}
	}
}

// effectMoveSweep is the move-driven lifetime of Effect-created continuous
// effects, run from Engine.emit after every MoveZone has been applied:
//
//   - ForgetOnMoved$ <zone> (Atsushi's, Rakdos's, Opposition Agent's may-play
//     effects, Incinerate's CantRegenerate): a remembered card that moved
//     FROM that zone leaves the effect's Remembered set — "you may play
//     those cards for as long as they remain exiled" ends the moment the
//     played card leaves exile — so the grant's Affected$ Card.IsRemembered
//     list follows what the effect actually holds.
//   - ExileOnMoved$ <zone> (Vines of Vastwood's blinked target, Party
//     Thrasher's chosen card): a remembered card that moved FROM that zone
//     ENDS the whole effect — Forge's "the effect is exiled".
//
// Zone names parse through the shared effects.ParseZone; an unparseable name
// can never match, so the effect simply never sweeps — the honest no-op for
// a value this build cannot read. Like EndOfTurnCleanup this is an in-place
// rewrite of e.continuous that emits no event and moves no log head; a
// replay rebuilds it by re-executing the same registrations against the same
// moves, so it reproduces byte-identically.
func (e *Engine) effectMoveSweep(ev events.Event) {
	if len(e.continuous) == 0 {
		return
	}
	kept := e.continuous[:0]
	changed := false
	for _, ce := range e.continuous {
		// A clone unit -- the layer-1 LCopy marker and every sibling modifier
		// effect, all carrying CloneTarget -- ends the instant the become
		// object leaves the battlefield (CR 400.7: it is a new object and its
		// CopyFace basis has already been cleared by Move). Dropping the whole
		// unit here is the structural owner of a clone's source-leaves
		// lifetime: without it a permanent copy's modifiers would keep applying
		// to a re-entered object and e.continuous would grow unbounded. The
		// expiring durations (UntilEOT / UntilTurn / until-combat /
		// until-unattached) are still handled by EndOfTurnCleanup; this sweep
		// adds the leave-the-battlefield case those branches cannot see.
		if ce.CloneTarget != 0 {
			if o := e.G.Obj(ce.CloneTarget); o == nil || o.Zone != state.ZBattlefield {
				changed = true
				continue
			}
		}
		forget, exile := ce.ForgetOnMoved, ce.ExileOnMoved
		exileAlso := ce.ExileOnMovedAlso
		if forget != "" && effects.ParseZone(forget) == ev.From && objIDIn(ce.Remembered, ev.Obj) {
			ce.Remembered = objIDWithout(ce.Remembered, ev.Obj)
			changed = true
		}
		if (exile != "" && effects.ParseZone(exile) == ev.From ||
			exileAlso != "" && effects.ParseZone(exileAlso) == ev.From) && objIDIn(ce.Remembered, ev.Obj) {
			changed = true
			continue // the effect ends: not kept
		}
		if ce.ChosenBound && mayPlayChosenLeftZone(&ce, ev) {
			ce.Chosen = objIDWithout(ce.Chosen, ev.Obj)
			changed = true
		}
		if forget == "" && exile == "" && mayPlayRememberedLeftZone(&ce, ev) {
			ce.Remembered = objIDWithout(ce.Remembered, ev.Obj)
			changed = true
		}
		kept = append(kept, ce)
	}
	if !changed {
		return
	}
	e.continuous = kept
	e.continuousChanged()
}

// mayPlayRememberedLeftZone reports whether ev moves one of a may-play
// grant's REMEMBERED cards out of a zone the grant names (AffectedZone$),
// for a grant that spells no move-driven lifetime of its own. CR 400.7: a
// card that leaves the zone is a new object with no memory of the old one,
// so "you may cast THAT card this turn" (Reezug, the Bonecobbler's graveyard
// grant) cannot follow it back once it has been cast, resolved and put into
// the graveyard again -- without this the one-shot permission recast the
// same Blood Pet for {B} forever within one turn (cardfuzz b12). Only an
// Affected$ spec that reads the remembered set (Card.IsRemembered) and an
// explicit zone list qualify: a grant naming Any/All zones, or one whose
// remembered set parameterises something else, keeps its old behaviour.
func mayPlayRememberedLeftZone(ce *state.ContinuousEffect, ev events.Event) bool {
	if !ce.MayPlay || ce.AffectedZone == "" || !strings.Contains(ce.Affects, "IsRemembered") ||
		!objIDIn(ce.Remembered, ev.Obj) {
		return false
	}
	zones, all, _ := effects.ParseZones(ce.AffectedZone)
	if all {
		return false
	}
	for _, z := range zones {
		if z == ev.From {
			return true
		}
	}
	return false
}

// mayPlayChosenLeftZone is mayPlayRememberedLeftZone for the chosen-card
// snapshot a Card.ChosenCard may-play grant carries (state.ContinuousEffect.
// Chosen): CR 400.7, the chosen card that leaves the grant's zone is a new
// object the permission no longer names, so a Strongbox Raider card cast from
// exile and later exiled again is not playable a second time.
func mayPlayChosenLeftZone(ce *state.ContinuousEffect, ev events.Event) bool {
	if !ce.MayPlay || ce.AffectedZone == "" || !objIDIn(ce.Chosen, ev.Obj) {
		return false
	}
	zones, all, _ := effects.ParseZones(ce.AffectedZone)
	if all {
		return ev.From != ev.To
	}
	return slices.Contains(zones, ev.From)
}

// effectCastSweep is the cast-driven lifetime of Effect-created continuous
// effects carrying ForgetOnCast$ (task param:api:Effect.ForgetOnCast): the
// first qualifying spell cast ENDS the whole effect -- Marshland
// Bloodcaster's "Rather than pay the mana cost of the NEXT spell you cast
// this turn", the one-cast cascade grants (Dark Apostle, Bigger on the
// Inside, Sloppity Bilepiper, World War Hulk), Kaza/Maelstrom Muse/
// Elminster's one-shot reduction. The spec is a card spec over the cast
// spell, You-relative to the effect's controller (the oracle's "spell YOU
// cast"), evaluated with the same machinery the other Effect specs use
// (MatchesSpecCtx against the effect's own source/controller context and
// remembered set). A cast that only targets nothing (a CastInfo-less land
// play never reaches this path: lands are never put on the stack) and a
// spell the spec does not name (an opponent's cast, a creature spell under
// a noncreature-only rider) leave the grant standing.
//
// Run from payCast's fireDeferredCastTrigger -- the deferred re-walk of the
// cast's held PutOnStack, AFTER payment -- so the sweep's timing is the
// completed cast: an ABORTED proposal (one reversed before payment, CR
// 733.1) never consumes the grant, while a completed cast -- even one later
// countered, which CR 601.2i still counts as cast -- does. Like
// effectMoveSweep this is an in-place rewrite of e.continuous that emits no
// event and moves no log head; a replay rebuilds it by re-executing the
// same registrations against the same casts, so it reproduces
// byte-identically.
func (e *Engine) effectCastSweep(ev events.Event) {
	if len(e.continuous) == 0 {
		return
	}
	kept := e.continuous[:0]
	changed := false
	for _, ce := range e.continuous {
		spec := strings.TrimSpace(ce.ForgetOnCast)
		if spec == "" {
			kept = append(kept, ce)
			continue
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		if e.matchesSpec(spec, ev.Obj, sc) {
			changed = true
			continue // the effect ends: not kept
		}
		kept = append(kept, ce)
	}
	if !changed {
		return
	}
	e.continuous = kept
	e.continuousChanged()
}

// effectCounterSweep is the counter-driven lifetime of Effect-created
// continuous effects (task vow1; ForgetCounter$), run from Engine.emit after
// every CounterChange has been applied: a remembered card whose count of the
// named kind reached zero after that removal leaves the effect's Remembered
// set -- Promise of Loyalty's "for as long as it has a vow counter on it",
// Quicksilver Fountain's FLOOD, Obsidian Fireheart's BLAZE (18 corpus
// carriers). The measured semantics this build pins: a count that DROPS
// without reaching zero keeps the card (a multi-countered card loses the
// restriction only when its LAST such counter goes), and an ADDITION never
// forgets anything. Like effectMoveSweep this is an in-place rewrite of
// e.continuous that emits no event and moves no log head; a replay rebuilds
// it by re-executing the same registrations against the same counter events,
// so it reproduces byte-identically.
func (e *Engine) effectCounterSweep(ev events.Event) {
	if ev.Amount >= 0 || len(e.continuous) == 0 {
		return
	}
	kept := e.continuous[:0]
	changed := false
	for _, ce := range e.continuous {
		if ce.ForgetCounter != "" && ce.ForgetCounter == ev.Counter && objIDIn(ce.Remembered, ev.Obj) {
			if o := e.G.Obj(ev.Obj); o == nil || o.Counter(ev.Counter) == 0 {
				ce.Remembered = objIDWithout(ce.Remembered, ev.Obj)
				changed = true
			}
		}
		kept = append(kept, ce)
	}
	if !changed {
		return
	}
	e.continuous = kept
	e.continuousChanged()
}

// objIDIn reports whether ids holds id.
func objIDIn(ids []state.ObjID, id state.ObjID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// objIDWithout returns ids without the first occurrence of id.
func objIDWithout(ids []state.ObjID, id state.ObjID) []state.ObjID {
	for i, x := range ids {
		if x == id {
			out := make([]state.ObjID, 0, len(ids)-1)
			out = append(out, ids[:i]...)
			out = append(out, ids[i+1:]...)
			return out
		}
	}
	return ids
}

// isCombatStep reports whether s is one of the combat phase's five steps
// (begin-combat through end-combat). UntilEndOfCombat effects (CR 511.2) are
// active for exactly this span and expire the moment play leaves end combat.
func isCombatStep(s state.Step) bool {
	return s >= state.StepBeginCombat && s <= state.StepEndCombat
}

// continuousLive is active()'s duration-honouring filter over one registered
// effect (see active()): whether it still exists right now. It is the one
// predicate active() and anyLayer4Active's registered-effect check share, so
// the two cannot disagree about which registered effects are live.
func (e *Engine) continuousLive(ce *ContinuousEffect) bool {
	if ce.Permanent {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(ce.Duration), "untilendofcombat") {
		return isCombatStep(e.G.Step)
	}
	if ce.UntilEOT {
		if ce.SourceIncarnation == 0 {
			return true
		}
		source := e.G.Obj(ce.Source)
		return source != nil && source.Zone == state.ZBattlefield && source.Incarnation == ce.SourceIncarnation
	}
	if ce.UntilTurn != 0 {
		// A turn-boundary effect outlives its source (a one-shot spell is
		// already gone) and is active through its own expiry turn, dropped
		// only by EndOfTurnCleanup when e.G.Turn reaches UntilTurn. Keep it
		// while the current turn is at or before that boundary; the
		// `<=` is the guard that keeps an effect from lingering if a
		// cleanup were ever skipped.
		return e.G.Turn <= ce.UntilTurn
	}
	o := e.G.Obj(ce.Source)
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	if ce.SourceIncarnation != 0 && o.Incarnation != ce.SourceIncarnation {
		return false
	}
	if ce.DurationSource != 0 {
		durationSource := e.G.Obj(ce.DurationSource)
		return durationSource != nil && durationSource.Zone == state.ZBattlefield
	}
	return true
}
