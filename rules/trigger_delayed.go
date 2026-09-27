// Delayed triggers: the ones armed now to fire later.
//
// Both the turn-scoped delayed triggers and the event-scoped ones, which carry
// their own cast matcher because a delayed SpellCast is scoped to the delay, not
// to the face.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TriggerModeSupported reports the SAME dispatch table the live trigger scan
// uses. Effect registration refuses unknown modes rather than storing an inert
// registration that would make the card appear supported.
func (e *Engine) TriggerModeSupported(mode string) bool {
	return trigMatchers[mode] != nil
}

// checkTriggers is called from emit after every event. It walks every
// object once (forEachObject) and, for each cards.Trigger on that object's
// face, asks triggerMatches whether this event satisfies it. A match
// appends to e.pendingTriggers with a context whose Remembered holds the
// triggering object; putTriggersOnStack later drains that queue onto the
// stack in APNAP order.
//
// lki is the object a MoveZone/Draw/PutOnStack event's own Obj was, just
// before emit applied the event (nil for every other event kind, or if that
// object could not be found -- see emit). It describes ev.Obj, not id (the
// object whose T: line is being checked in a given iteration below), so it
// is handed to EVERY trigger this event fires, not only a Card.Self trigger
// on ev.Obj itself: a bystander's "whenever a creature you control dies"
// wants the LKI of the creature that died, exactly the same as that
// creature's own dies trigger would. Fix round 1, Important 2: an earlier
// version of this doc claimed LKI was withheld from every trigger but the
// matching object's own, which was never what the code below does (see
// objLKI's guard) -- lki.ID == ev.Obj is already guaranteed by construction
// in emit (lki, when non-nil, is always built from e.G.Obj(ev.Obj)), so
// there is no per-trigger gate here at all, only a defensive belt-and-
// braces check against a future emit change that might one day pass a
// mismatched lki.
// collectExpiredDelayedTriggers removes promises that cannot fire after a
// new turn begins, even if no matching phase or event occurs again. A source,
// controller or Execute that disappeared is likewise collected. Removal is
// logged so replay folds the same registration set.
func (e *Engine) collectExpiredDelayedTriggers() {
	var remove []uint32
	for _, dt := range e.G.Delayed {
		if (dt.MaxTurn > 0 && e.G.Turn > dt.MaxTurn) || !e.delayedRegistrationLive(&dt) {
			remove = append(remove, dt.ID)
		}
	}
	e.clearEffectMatchScope()
	for _, id := range remove {
		e.emit(events.Event{Kind: events.DelayedRemove, Amount: int32(id)})
	}
}

func (e *Engine) delayedRegistrationLive(dt *state.DelayedTrigger) bool {
	if int(dt.Controller) >= len(e.G.Players) || e.G.Players[dt.Controller].Lost {
		return false
	}
	src := e.G.Obj(dt.Source)
	if src == nil || src.Face() == nil || events.ResolveSVarAcrossFaces(src, dt.Execute) == nil {
		return false
	}
	// The sole lifetime predicate is shared by collection and both fire-time
	// scans. In particular a permanent Effect grant ends with its source's
	// battlefield incarnation -- but ONLY when the source was a battlefield
	// permanent at registration (CR 611.2). An opening-hand Effect
	// (Chancellor of the Annex, source in hand) or an emblem/command-zone
	// source has no battlefield incarnation to lose, so its Permanent
	// promise is unbounded (SourceBattlefield false).
	switch dt.EffectDuration {
	case "permanent":
		if dt.SourceBattlefield &&
			(src.Zone != state.ZBattlefield || src.Incarnation != dt.SourceIncarnation) {
			return false
		}
	case "untilendofcombat":
		if !isCombatStep(e.G.Step) {
			return false
		}
	case "untilyournextturn", "untiltheendofyournextturn":
		// Read the folded turn history through the shared turn-start cache
		// (nextTurnFor/rescheduleNextTurnBoundaries' own source of truth),
		// not a frozen absolute turn: late extra-turn grants and skipped
		// turns move the next-turn boundary. The cache is rebuilt once per
		// log length, never per registration per event.
		starts := e.turnStartsFor(dt.Controller)
		first, ok := firstTurnStartAfter(starts, dt.BirthTurn)
		if !ok {
			break
		}
		if dt.EffectDuration == "untilyournextturn" {
			return false
		}
		// UntilTheEndOfYourNextTurn lasts through that turn's cleanup: it is
		// over once a LATER turn has begun, or at the cleanup of the boundary
		// turn. A second turn start after the boundary proves the first has
		// ended even if the intervening cleanup event was never presented.
		_, second := firstTurnStartAfter(starts, first)
		if second || e.G.Turn > first || (e.G.Turn == first && e.G.Step == state.StepCleanup) {
			return false
		}
	}
	if dt.EventMode != "" {
		if dt.Trigger == "" {
			return false
		}
		raw := dt.Trigger
		if !strings.HasPrefix(raw, "Mode$") {
			raw = events.SVarAcrossFaces(src, raw)
		}
		t, ok := cards.ParseTriggerLine(raw)
		return ok && t.Mode == dt.EventMode
	}
	return true
}

// turnStartsFor returns the sorted turn numbers at which p's turn began,
// folded from the event log when the cache is stale. It is the ONE home for
// the delayed next-turn boundary, so every registration shares one walk per
// log length rather than one walk per registration per event (the collector
// and both fire-time scans all call delayedRegistrationLive). Matches the
// turnsTaken cache's lazy epoch pattern.
func (e *Engine) turnStartsFor(p state.PlayerID) []int32 {
	if len(e.turnStartTurns) != len(e.G.Players) || e.turnStartEpoch != len(e.L.Events) {
		starts := make([][]int32, len(e.G.Players))
		for _, ev := range e.L.Events {
			if ev.Kind == events.TurnChange && int(ev.Player) < len(starts) {
				starts[ev.Player] = append(starts[ev.Player], ev.Amount)
			}
		}
		e.turnStartTurns = starts
		e.turnStartEpoch = len(e.L.Events)
	}
	if int(p) >= len(e.turnStartTurns) {
		return nil
	}
	return e.turnStartTurns[p]
}

// firstTurnStartAfter returns the first turn number in the sorted starts
// slice strictly greater than turn.
func firstTurnStartAfter(starts []int32, turn int32) (int32, bool) {
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > turn })
	if i < len(starts) {
		return starts[i], true
	}
	return 0, false
}

// checkDelayedTriggers queues a pending trigger for every delayed-trigger
// registration (state.Game.Delayed) registered to fire on the step just
// entered. It runs from checkTriggers after a StepChange event, alongside
// the ordinary face-trigger walk, so a delayed trigger reaches the stack
// through exactly the same putTriggersOnStack drain as any T: line trigger.
// The registration carries everything the drain needs: the source permanent
// (whose SVar table holds the Execute$ sub-ability), the controller, the
// Execute$ SVar name and the Remembered captured at registration.
//
// CR 603.7: a delayed trigger fires even when its source has left the
// battlefield (the registration is independent of it once created), so the
// source is read only to resolve the Execute$ SVar table -- an object that
// has moved zones (in a graveyard, exiled) still has a Face and SVars, so
// the trigger still fires; a source that has ceased to exist entirely (a
// token or copy gone from the board) has nothing to read and degrades to a
// no-op. The one-shot removal happens in events.Apply's DelayedPush case, so
// the drain never re-fires the same registration on a later occurrence of
// the phase.
func (e *Engine) checkDelayedTriggers(ev events.Event) {
	var remove []uint32
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		if dt.EventMode != "" {
			continue // an event-matched registration fires on its event, never a step
		}
		if (dt.MaxTurn > 0 && e.G.Turn > dt.MaxTurn) || !e.delayedRegistrationLive(dt) {
			remove = append(remove, dt.ID)
			continue
		}
		if dt.Phase != ev.Step {
			continue
		}
		// CR 603.7 + CR 500.7 (rules/saga.go, events.ExtraTurn): a delayed
		// trigger an extra-turn grant registered carries the granted turn's
		// number as MinTurn, so the granting turn's own occurrence of the
		// phase (Final Fortune's end step) does not consume the one-shot
		// registration -- the trigger fires exactly once, in the granted
		// turn, and the registration stays pending until then.
		if dt.MinTurn > 0 && e.G.Turn < dt.MinTurn {
			continue
		}
		// Expiry is collected on TurnChange, including when this phase is
		// skipped entirely; see collectExpiredDelayedTriggers.
		// ValidPlayer$ (Necropotence's "at the beginning of YOUR next end
		// step"): the registering DelayedTrigger SA's ValidPlayer$ filter,
		// carried on the registration and evaluated at the phase occurrence
		// the way phaseMatches evaluates a Mode$ Phase T: line's own
		// ValidPlayer$ -- the step just entered always belongs to the current
		// active player, so the gate asks MatchesPlayerSpec about e.G.Active
		// against the REGISTRATION's controller. A gate the step fails leaves
		// the one-shot registration pending (the DelayedPush that would
		// consume it never mints), so it fires at the first later occurrence
		// of the phase that does match. The corpus's DelayedTrigger
		// ValidPlayer$ values are Player 135, You 35, Opponent 2 plus a
		// handful of qualified forms; bare Player matches every seat (the
		// ungated behaviour those registrations already had), and the
		// qualified ones fail closed inside MatchesPlayerSpec (the fx20
		// convention: an unmodellable qualifier fires for nobody, never for
		// everybody).
		pc := e.playerSpecCtx(dt.Source)
		pc.DelayedRemembered = dt.Remembered
		if dt.ValidPlayer != "" && !effects.MatchesPlayerSpecCtx(e.G, dt.ValidPlayer, e.G.Active, dt.Controller, pc) {
			continue
		}
		// IsPresent$ / PresentZone$ / PresentCompare$ (Bank Job's "at the
		// beginning of the next end step, if that card is still exiled"): the
		// registering DelayedTrigger SA's presence condition, carried on the
		// registration and evaluated at the phase occurrence exactly like a
		// static's own present gate. Card.IsTriggerRemembered binds against
		// THIS registration's remembered capture, so the count asks whether a
		// still-remembered card is in the named zone. A gate the step fails
		// leaves the one-shot registration pending for the first later
		// occurrence that matches, the same direction ValidPlayer takes; an
		// absent spec (every earlier registration) skips the gate whole.
		if dt.PresentSpec != "" && !e.delayedPresentGateHolds(dt) {
			continue
		}
		if int(dt.Controller) >= len(e.G.Players) || e.G.Players[dt.Controller].Lost {
			remove = append(remove, dt.ID)
			continue
		}
		src := e.G.Obj(dt.Source)
		if src == nil {
			remove = append(remove, dt.ID)
			continue
		}
		f := src.Face()
		if f == nil {
			remove = append(remove, dt.ID)
			continue
		}
		sa := cards.ResolveSVar(f.SVars, dt.Execute)
		if sa == nil {
			remove = append(remove, dt.ID)
			continue
		}
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     dt.Source,
			Controller: dt.Controller,
			Delayed:    true,
			DelayedID:  dt.ID,
			Execute:    dt.Execute,
			SA:         sa,
			Ctx: effects.Ctx{
				Source:     dt.Source,
				Controller: dt.Controller,
				Remembered: append([]state.Target(nil), dt.Remembered...),
				Captured:   append([]state.Target(nil), dt.Remembered...),
				// A phase registration has no firing event, so its capture
				// is both Remembered and the DelayTriggerRemembered referent.
				TriggerContext: effects.TriggerContext{
					DelayedRemembered: append([]state.Target(nil), dt.Remembered...),
					OptionalSpec:      dt.OptionalSpec,
				},
			},
		})
	}
	for _, id := range remove {
		e.emit(events.Event{Kind: events.DelayedRemove, Amount: int32(id)})
	}
}

// delayedPresentGateHolds evaluates a Phase delayed registration's
// IsPresent$/PresentZone$/PresentCompare$ condition at the phase occurrence.
// The count scans PresentZone (absent = Battlefield) for objects matching
// PresentSpec with the registration's capture bound as the IsTriggerRemembered
// referent -- the same SpecContext binding effects.MatchesPlayerSpecCtx gets
// for ValidPlayer, so a capture-aware filter answers identically at both
// gates. The compare defaults to GE1 ("at least one present"), exactly the
// static presentGate default; an unknown PresentZone$ fails closed (count 0),
// the same direction countStaticPresent takes.
func (e *Engine) delayedPresentGateHolds(dt *state.DelayedTrigger) bool {
	zone, ok := presentZoneFromParam(dt.PresentZone)
	if !ok {
		return false
	}
	sc := e.specCtx(dt.Source, dt.Controller)
	sc.DelayedRemembered = dt.Remembered
	n := 0
	e.forEachObject(func(id state.ObjID) {
		o := e.G.Obj(id)
		if o == nil || o.Zone != zone {
			return
		}
		if e.matchesSpec(dt.PresentSpec, id, sc) {
			n++
		}
	})
	cmp := dt.PresentCompare
	if cmp == "" {
		cmp = "GE1"
	}
	return comparePresent(n, e.presentCompareFor(cmp, dt.Source, dt.Controller))
}

// checkEventDelayedTriggers queues a pending trigger for every event-matched
// delayed registration (state.Game.Delayed entries with EventMode set) that
// the event just folded satisfies. A Mode$ Phase registration fires on the
// step it names (checkDelayedTriggers, above); the opening-hand Effect shape
// (Chancellor of the Annex) registers Mode$ SpellCast one-off triggers, which
// fire on a spell's PutOnStack exactly like a face SpellCast trigger. The
// registration's STORED trigger body (dt.Trigger) is re-parsed at fire time
// so its validity clauses are evaluated against the actual cast -- the
// registration is the game state a replay rebuilds, the body is not.
//
// "You" inside that body is the REGISTRATION's controller -- the effect owner
// the registration was minted for, not the source card's controller: the
// Chancellor's Effect is EffectOwner$ Opponent, so each per-opponent
// registration fires on that opponent's first cast, which is the oracle's
// "when each opponent casts their first spell". TriggerZones$ is deliberately
// not consulted: Forge parks the trigger on a command-zone Effect, while the
// engine's registration itself is that presence -- the source Chancellor card
// stays in its hand.
// delayedSpellCastFire is one event-matched SpellCast registration the scan
// pass of checkEventDelayedTriggers collected, with everything its firing
// pass needs. The scan is deliberately PURE — it must not emit — because the
// static firing arm's synchronous DelayedPush consumes its registration via
// events.Apply, which splices state.Game.Delayed mid-iteration: firing inside
// the live-slice range skipped or crashed on the registrations after the
// first matched one (two "the next spell you cast can't be countered"
// promises live at once, so this is the ordinary shape, not an edge).
type delayedSpellCastFire struct {
	dt         state.DelayedTrigger
	sa         *cards.SA
	trigger    cards.Trigger
	remembered []state.Target
	referents  effects.TriggerContext
	svars      map[string]string
	static     bool
}

func (e *Engine) checkEventDelayedTriggers(ev events.Event, lki *state.Object) {
	var fires []delayedSpellCastFire
	var remove []uint32
	for i := range e.G.Delayed {
		dt := &e.G.Delayed[i]
		// The event-matched modes: SpellCast (a spell's PutOnStack) and
		// ChangesZone (a move, the Earthbend return promise). A Mode$ Phase
		// registration carries no EventMode at all and is owned by
		// checkDelayedTriggers at its phase occurrence.
		if !dt.EffectRepeat && dt.EventMode != "SpellCast" && dt.EventMode != "ChangesZone" &&
			dt.EventMode != "ChangesController" && dt.EventMode != "DamageDone" &&
			dt.EventMode != "AttackersDeclared" && dt.EventMode != "BecomeMonarch" {
			continue
		}
		// TurnChange collects expired registrations before another event can
		// match; keep this guard for callers presenting a later event directly.
		if dt.MaxTurn > 0 && e.G.Turn > dt.MaxTurn {
			remove = append(remove, dt.ID)
			continue
		}
		if !e.delayedRegistrationLive(dt) {
			remove = append(remove, dt.ID)
			continue
		}
		src := e.G.Obj(dt.Source)
		if dt.Trigger == "" {
			remove = append(remove, dt.ID)
			continue
		}
		// The stored trigger body: a SVar NAME (the keyword-expansion shape
		// rules.registerOpeningEffectTriggers mints) resolves against the
		// source's own table; an inline body (effDelayedTrigger's Mode$
		// SpellCast branch, a face Ability's DelayedTrigger with no SVar name
		// of its own — Mistrise Village — or an Earthbend registration's
		// stored "Mode$ ChangesZone | ..." body) is stored raw and parses
		// directly. The two carriers cannot collide: no SVar name starts
		// "Mode$".
		raw := dt.Trigger
		if !strings.HasPrefix(raw, "Mode$") {
			raw = events.SVarAcrossFaces(src, raw)
		}
		t, ok := cards.ParseTriggerLine(raw)
		if !ok || t.Mode != dt.EventMode {
			remove = append(remove, dt.ID)
			continue
		}
		sa := events.ResolveSVarAcrossFaces(src, dt.Execute)
		if sa == nil {
			remove = append(remove, dt.ID)
			continue
		}
		// Match a recurring Effect as its registration owner, not as the
		// controller of the card that created it. The scope is a read-only
		// matching overlay -- controller-relative predicates (including
		// nested spec matching and referent capture) resolve against this
		// registration -- and touches no object and no state.Game field, so
		// nothing here reaches an event. It is cleared immediately before
		// each registration's own matching (so a `continue` cannot leak one
		// registration's scope into the next) and once more after the
		// matching loop, before the removal and firing passes below, which
		// resolve with no scope at all.
		e.clearEffectMatchScope()
		if dt.EffectRepeat {
			e.effectMatchSource = dt.Source
			e.effectMatchController = dt.Controller
			e.effectMatchRemembered = dt.Remembered
			e.effectMatchOverride = true
		}
		// referentsArg is the LKI snapshot handed to triggerReferents. The
		// SpellCast arm deliberately passes nil (the spell object itself is
		// the referent source), exactly as it did before the ChangesZone arm
		// existed; the ChangesZone arm threads the leaving object's snapshot
		// so a departing source's last battlefield characteristics are read
		// (CR 603.10a), the same lki the face-trigger walk uses.
		var referentsArg *state.Object
		if dt.EventMode == "BecomeMonarch" {
			// Palace Jailer's command-zone trigger body reads
			// `ValidPlayer$ Player.OpponentOf Remembered` — "until an
			// OPPONENT becomes the monarch". The opponent relation is
			// against the EFFECT's controller (dt.Controller), the player
			// Forge's `RememberObjects$ You & Targeted` anchors. In a
			// multiplayer game that is NOT the exiled creature's own
			// controller: seat 0's Jailer exiles seat 1's creature and seat
			// 2 takes the crown, so the creature must return even though
			// seat 2 does not control it.
			if v := strings.TrimSpace(t.Params["ValidPlayer"]); strings.EqualFold(v, "Player.OpponentOf Remembered") {
				if int(ev.Player) >= len(e.G.Players) || e.G.Players[ev.Player].Lost ||
					!effects.MatchesPlayerSpec(e.G, "Opponent", ev.Player, dt.Controller) {
					continue
				}
				delete(t.Params, "ValidPlayer")
			}
			if !e.becomeMonarchMatches(t, dt.Source, ev) {
				continue
			}
			referentsArg = nil
		} else if dt.EventMode == "ChangesZone" {
			// The Earthbend return promise. destinationAdmits handles the
			// comma-separated Destination$ list (Graveyard,Exile) that
			// zoneChangeMatches reads with the single-word effects.ParseZone
			// -- the engine-wide comma-Destination$ defect, ledgered
			// separately. The special case is local to delayed
			// registrations, so a face trigger keeps the existing single-word
			// reading; a single-zone string is not touched at all.
			if d, ok := t.Params["Destination"]; ok && strings.Contains(d, ",") {
				if !zoneDelayedDestinationAdmits(d, ev.To) {
					continue
				}
				delete(t.Params, "Destination")
			}
			if !e.zoneChangeMatchesWithCapture(t, dt.Source, ev, lki, dt.Remembered) {
				continue
			}
			if vp := strings.TrimSpace(t.Params["ValidPlayer"]); vp != "" {
				p, ok := e.delayedEventPlayer(t, ev, lki)
				pc := e.playerSpecCtx(dt.Source)
				pc.DelayedRemembered = dt.Remembered
				if !ok || !effects.MatchesPlayerSpecCtx(e.G, vp, p, dt.Controller, pc) {
					continue
				}
			}
			referentsArg = lki
		} else if delayedEventModeHandled(t.Mode) {
			// The modes with a delayed-registration matcher of their own.
			// An EffectRepeat registration runs them through the scoped
			// matching overlay, so the "you" every controller-relative clause reads
			// is the Effect's owner, not the creating card's controller.
			if vp := strings.TrimSpace(t.Params["ValidPlayer"]); vp != "" {
				p, ok := e.delayedEventPlayer(t, ev, lki)
				pc := e.playerSpecCtx(dt.Source)
				pc.DelayedRemembered = dt.Remembered
				if !ok || !effects.MatchesPlayerSpecCtx(e.G, vp, p, dt.Controller, pc) {
					continue
				}
			}
			if !e.delayedEventMatches(t, dt, ev, lki) {
				continue
			}
		} else if dt.EffectRepeat {
			// The Effect object is represented by its registration, not by a
			// battlefield face. Dispatch through the ordinary mode matcher but
			// skip the printed-face zone gate (the creating spell may already
			// be in the graveyard). The event mask prevents a mismatched event
			// from reaching a matcher that assumes its own event shape.
			fn := trigMatchers[t.Mode]
			if fn == nil || !triggerModeEvents(t.Mode).allows(ev.Kind) ||
				!fn(e, t, dt.Source, ev, lki) {
				continue
			}
		} else {
			continue
		}
		if dt.EventMode != "BecomeMonarch" {
			// The registration's OWN capture binds Card.IsTriggerRemembered
			// inside the body's IsPresent$/IsPresent2$ clauses (Stolen
			// Uniform's "if it's attached to a creature you control"): the
			// unbound generic walk answers the capture-aware spec unknown
			// and fails closed, so a qualifying control change would never
			// fire. The binding rides the caller's TriggerContext, so every
			// other condition clause keeps its single evaluation with
			// unchanged semantics (Fight for the Throne's non-capture
			// IsPresent$ Card.IsCommander+... is untouched). A failed
			// condition does not spend the registration: the fire is simply
			// not collected, so a later matching event, once the condition
			// holds, may fire -- the same pending direction ValidPlayer$
			// and the Phase arm's own present gate take.
			if !e.triggerConditionHoldsAsWithDelayedRemembered(t, dt.Source, dt.Controller, dt.Remembered) {
				continue
			}
		}
		refs := e.triggerReferents(t, dt.Source, ev, referentsArg)
		refs.DelayedObject = ev.Obj
		// The REGISTRATION's capture rides its own referent field, never
		// Ctx.Remembered: an event-matched registration's Remembered is the
		// firing event's object, exactly what triggerRemembered seeds a
		// printed trigger of the same mode with, because the Execute bodies
		// read it through the ordinary Triggered* spellings (Chancellor of
		// the Annex's Defined$ TriggeredSpellAbility is the cast spell, not
		// whatever the Effect captured when it registered). Only
		// DelayTriggerRemembered names the registration's own set.
		remembered := triggerRemembered(ev, dt.Source)
		if dt.EventMode == "BecomeMonarch" {
			// Palace Jailer's exile promise: nothing on a MonarchChange
			// names the exiled card, so the registration's capture IS the
			// referent this body resolves.
			remembered = append([]state.Target(nil), dt.Remembered...)
		} else if dt.EffectRepeat && dt.EventMode == "ChangesZone" {
			// An Effect-owned ChangesZone trigger's `Defined$ Remembered`
			// names the Effect's OWN capture (Forge's created-effect
			// Remembered list), not the zone-changing card: Out of Time's
			// comeback body is `DB$ Phases | Defined$ Remembered` and must
			// phase the creatures the Effect remembered, not the host that
			// just left. The firing event's object rides the ordinary
			// Triggered* referents (refs.DelayedObject), so a body naming
			// the trigger source still resolves it.
			remembered = append([]state.Target(nil), dt.Remembered...)
		}
		refs.DelayedRemembered = append([]state.Target(nil), dt.Remembered...)
		// The registration's OptionalDecider$ election rides the referent
		// context to the minted stack object, where resolveTop's CR 603.5
		// gate poses it. It cannot be recovered from the trigger body for a
		// Mode$ Phase registration (never re-parsed), so it is carried from
		// state rather than read from t.Params here.
		refs.OptionalSpec = dt.OptionalSpec
		fires = append(fires, delayedSpellCastFire{
			dt:         *dt,
			sa:         sa,
			trigger:    t,
			remembered: remembered,
			referents:  refs,
			svars:      src.Face().SVars,
			static:     strings.TrimSpace(t.Params["Static"]) != "",
		})
	}
	// The LAST registration the loop processed leaves its scope armed
	// (clearEffectMatchScope runs before each registration's own matching, so
	// nothing clears the final one -- and a registration that hit one of the
	// early `continue`s above never reached it at all). Left armed, the scope
	// leaks past this function: the NEXT matching walk's controllerOf and the
	// trigmatch_zone look-back guard consult effectMatchControllerFor precisely
	// because it is only true inside a registration's matching, so a printed
	// controller-relative trigger would read this registration's virtual
	// controller instead of its own source's state. Clear here, so emit
	// returns with no scope and both passes below resolve with no scope at all.
	e.clearEffectMatchScope()
	for _, id := range remove {
		e.emit(events.Event{Kind: events.DelayedRemove, Amount: int32(id)})
	}
	// The firing pass runs over the collected copies, never the live slice:
	// a static fire's synchronous emit consumes its registration (splice) and
	// the body's resolution may register or consume further registrations.
	for i := range fires {
		f := &fires[i]
		dt := &f.dt
		// Forge's STATIC delayed trigger (TriggerHandler's isStatic arm): the
		// Execute body resolves IMMEDIATELY at fire time — never pushed on
		// the stack — so the promise it creates (Mistrise Village's
		// "the next spell you cast this turn can't be countered") is
		// active before any player can respond to the cast. The static-
		// marked DelayedPush consumes the one-shot registration without
		// minting a stack object (events.Apply), then the body resolves
		// inline; an ask the body poses parks through the ordinary direct
		// resume machinery.
		if f.static {
			// Fail-closed guard (cli-20260923T060218Z round 2): a static
			// delayed registration carrying an OptionalDecider$ spec has no
			// election channel on this path -- the body resolves inline,
			// never minting a stack object, so resolveTop's CR 603.5 gate
			// cannot pose the yes/no -- and running it would execute the
			// "you may" mandatorily. effEffect withholds that shape at
			// registration; this guard keeps any future "|OD=" minter from
			// misexecuting here. Loud Note, registration consumed without
			// execution (the same one-shot spend a declined placement is).
			if dt.OptionalSpec != "" {
				e.emit(events.Event{Kind: events.Note, Obj: dt.Source,
					Player: dt.Controller,
					Text:   "optional static delayed trigger has no election channel (not executed)"})
				e.emit(events.Event{Kind: events.DelayedPush, Obj: dt.Source,
					Player: dt.Controller, Amount: int32(dt.ID), Counter: dt.Execute,
					Text: "static"})
				continue
			}
			e.emit(events.Event{Kind: events.DelayedPush, Obj: dt.Source,
				Player: dt.Controller, Amount: int32(dt.ID), Counter: dt.Execute,
				Text: "static"})
			ctx := effects.Ctx{Source: dt.Source, Controller: dt.Controller,
				Remembered:     f.remembered,
				Captured:       f.remembered,
				SVars:          f.svars,
				EffectFrame:    effectDelayedFrame(dt),
				TriggerContext: f.referents,
			}
			effects.Resolve(e, &ctx, f.sa)
			continue
		}
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:       dt.Source,
			Controller:   dt.Controller,
			Delayed:      true,
			DelayedID:    dt.ID,
			Execute:      dt.Execute,
			SA:           f.sa,
			Trigger:      f.trigger,
			TriggerSVars: f.svars,
			Ctx: effects.Ctx{
				Source:     dt.Source,
				Controller: dt.Controller,
				Remembered: f.remembered,
				Captured:   f.remembered,
				// An Effect-created trigger body resolves under the Effect's OWN
				// source-scoped frame, so the one-shot self-exile idiom reached
				// from it (Kor Dirge's `Triggers$ OutOfSight` ->
				// `DB$ ChangeZone | Defined$ Self | Origin$ Command |
				// Destination$ Exile`) ends the registration instead of
				// lingering for the Effect's whole duration. A plain CR 603.7
				// promise leaves the frame zero (nothing to end).
				EffectFrame: effectDelayedFrame(dt),
				// The same event-provenance capture the ordinary face
				// SpellCast path takes (triggerReferents' SpellCast case),
				// so the fired ability resolves TriggeredActivator/
				// TriggeredSource exactly as a face trigger would.
				TriggerContext: f.referents,
			},
		})
	}
}

// effectDelayedFrame is the source-scoped Effect frame an Effect-created
// delayed trigger body resolves under: a live registration's source with a
// zero stamp, meaning "every Effect-created registration from this source"
// (Host.EndEffectSource). A plain CR 603.7 promise (dt.EffectRepeat false)
// carries no frame, so the self-exile idiom stays inert for it exactly as it
// always has.
func effectDelayedFrame(dt *state.DelayedTrigger) effects.EffectFrame {
	if dt == nil || !dt.EffectRepeat {
		return effects.EffectFrame{}
	}
	return effects.EffectFrame{Source: dt.Source}
}

// delayedEventModeHandled names the modes delayedEventMatches evaluates with
// a delayed-registration matcher of its own. Every other mode an Effect
// registration may name falls through to the generic trigMatchers dispatch.
func delayedEventModeHandled(mode string) bool {
	switch mode {
	case "SpellCast", "ChangesController", "DamageDone", "AttackersDeclared":
		return true
	}
	return false
}

// delayedSpecCtx binds a registration's capture only for its trigger match.
func delayedSpecCtx(sc effects.SpecContext, remembered []state.Target) effects.SpecContext {
	sc.DelayedRemembered = remembered
	return sc
}

// delayedEventMatches dispatches the existing trigger matchers for an event
// delayed registration. Keeping this on the ordinary matcher helpers makes a
// delayed body and a printed T: line agree on zone, damage and attack filters.
func (e *Engine) delayedEventMatches(t cards.Trigger, dt *state.DelayedTrigger, ev events.Event, lki *state.Object) bool {
	switch t.Mode {
	case "SpellCast":
		return e.eventDelayedSpellCastMatches(t, dt, ev)
	case "ChangesController":
		return e.delayedChangesControllerMatches(t, dt, ev, lki)
	case "DamageDone":
		return e.damageMatchesWithCapture(t, dt.Source, ev, dt.Remembered)
	case "AttackersDeclared":
		return e.attackersDeclaredOneTargetMatches(t, dt.Source, ev, dt.Remembered)
	default:
		return false
	}
}

// delayedEventPlayer supplies the player named by a delayed ValidPlayer$ gate.
// Event modes use the event's natural actor/recipient, while a zone or control
// change uses the pre-event controller captured in LKI.
func (e *Engine) delayedEventPlayer(t cards.Trigger, ev events.Event, lki *state.Object) (state.PlayerID, bool) {
	switch t.Mode {
	case "ChangesZone", "ChangesController":
		if lki != nil {
			return lki.Controller, true
		}
		if o := e.G.Obj(ev.Obj); o != nil {
			return o.Controller, true
		}
	case "SpellCast":
		return ev.Player, int(ev.Player) < len(e.G.Players)
	case "DamageDone":
		if ev.Obj == 0 {
			return ev.Player, int(ev.Player) < len(e.G.Players)
		}
		if src := e.damageEventSource(); src != 0 {
			return e.controllerOf(src), true
		}
	case "AttackersDeclared":
		if len(ev.IDs) > 0 {
			return e.controllerOf(ev.IDs[0]), true
		}
	}
	return 0, false
}

func (e *Engine) delayedChangesControllerMatches(t cards.Trigger, dt *state.DelayedTrigger, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.ControlChange || lki == nil || lki.Controller == ev.Player {
		return false
	}
	ctrl := dt.Controller
	if v := t.Params["ValidCard"]; v != "" && !effects.MatchesObjectCtx(e.G, v, lki, delayedSpecCtx(e.specCtx(dt.Source, ctrl), dt.Remembered)) {
		return false
	}
	if v := t.Params["ValidOriginalController"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, lki.Controller, ctrl) {
		return false
	}
	if v := t.Params["ValidNewController"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	return true
}

// eventDelayedSpellCastMatches is the event-matched registration's validity
// evaluation, mirroring spellCastMatches' clause grammar (ValidCard$,
// ValidActivatingPlayer$, PlayerTurn$) with one deliberate difference: the
// "you" every player clause is measured against is dt.Controller, the
// registration's effect owner, not the source card's controller (see
// checkEventDelayedTriggers above).
func (e *Engine) eventDelayedSpellCastMatches(t cards.Trigger, dt *state.DelayedTrigger, ev events.Event) bool {
	if ev.Kind != events.PutOnStack {
		return false
	}
	// Casting a spell means an actual card entering the stack (Ruling F3,
	// the same guard spellCastMatches carries).
	obj := e.G.Obj(ev.Obj)
	if obj == nil || obj.Face() == nil {
		return false
	}
	if actionTriggerModes[t.Mode] && strings.EqualFold(t.Params["PlayerTurn"], "True") &&
		e.G.Active != dt.Controller {
		return false
	}
	if v, ok := t.Params["ValidCard"]; ok {
		// The cast-provenance qualifiers (castprov1/2/3 — narset's
		// `ValidCard$ Instant.wasCastFromYourHand,Sorcery.wasCastFromYourHand`)
		// split out BEFORE spellCastPermanentSpec rewrites the base: the strip
		// helpers match the raw Forge spec's predicate chain. The spell is on
		// the stack (this is the PutOnStack event), so the log read is honest.
		v, ok := e.castProvenanceAdmits(v, ev.Obj, dt.Controller)
		if !ok {
			return false
		}
		if !e.matchesSpec(spellCastPermanentSpec(v), ev.Obj, delayedSpecCtx(e.specCtx(dt.Source, dt.Controller), dt.Remembered)) {
			return false
		}
	}
	if v, ok := t.Params["ValidActivatingPlayer"]; ok {
		if !effects.MatchesPlayerSpec(e.G, v, ev.Player, dt.Controller) {
			return false
		}
	}
	// The same cast-condition clauses spellCastMatches evaluates, mirrored
	// so a stored body carrying either stays fire-time-correct (the "you"
	// the activator clauses measure is the event's caster either way).
	if v, ok := t.Params["ActivatorThisTurnCast"]; ok {
		if !compareIntCount(int32(e.spellsCastThisTurn(ev.Player)), v) {
			return false
		}
	}
	if v, ok := t.Params["ValidSA"]; ok {
		if !e.validSAMatches(dt.Source, ev, dt.Controller, v) {
			return false
		}
	}
	// The target-shape params (targetsvalid1), the delayed mirror: no corpus
	// carrier combines a delayed SpellCast registration with either param
	// (measured: the two files carrying both a DelayedTrigger and a
	// target-shape param carry only Mode$ Phase bodies), but the stored
	// grammar mirrors spellCastMatches' clauses one for one and the read is
	// honest here -- the spell is already on the stack with its recorded
	// targets, so a future registration cannot widen silently.
	if !e.targetShapeMatches(t, obj.Targets, dt.Source, dt.Controller) {
		return false
	}
	return true
}

// hasEffectRepeatDelayed reports whether any live registration is a recurring
// Effect-delivered trigger. Those may name any registered mode, so the event
// gate on checkEventDelayedTriggers cannot be a fixed kind list while one is
// outstanding.
func (e *Engine) hasEffectRepeatDelayed() bool {
	for i := range e.G.Delayed {
		if e.G.Delayed[i].EffectRepeat {
			return true
		}
	}
	return false
}

// clearEffectMatchScope drops the recurring-Effect matching overlay. It is a
// matcher-local read scope only: no state.Game field and no object is touched,
// so it can never reach an event or the replay.
func (e *Engine) clearEffectMatchScope() {
	e.effectMatchSource = 0
	e.effectMatchController = 0
	e.effectMatchRemembered = nil
	e.effectMatchOverride = false
}
