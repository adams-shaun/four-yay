// stack_helpers.go holds the Engine accessor surface (Game, Emit, Rand,
// Object*), the per-turn history counters a card's cost/effect reads from the
// log (life lost/gained, cards drawn/discarded, spells cast, attackers,
// counters added/removed, combat damage), the life-exchange transaction, the
// planar/zoneswalk and library-shuffle helpers, and payUnlessCost.
// Split out of stack.go by a pure move (no rename, no behaviour change).
package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) Game() *state.Game                   { return e.G }
func (e *Engine) ObjectColors(o *state.Object) string { return e.objColors(o) }

func (e *Engine) Emit(ev events.Event) { e.emit(ev) }

// CastProhibited is effects' optional castProhibitedHost read (task
// play-prohibited-election): the same CantBeCast gate beginPlay enforces
// (rules/statics.go castRestricted), exposed so effPlay's Play election never
// OFFERS a cast that CR 601.3 would refuse. It is a pure read -- no event,
// no state change -- and the beginPlay recheck stays the enforcement site.
func (e *Engine) CastProhibited(p state.PlayerID, id state.ObjID) bool {
	return e.castRestricted(p, id)
}

// EmitScryRecord is effects.Host's completed-scry-record emit (task
// scrybottom): the stand-in completion in effects' effLookAndArrange goes
// through emitScryRecord, the exact site handleArrange uses, so a scry that
// finishes without an answerable KArrange (an empty library, ScryNum$ 0, a
// no-host run) still records its zero-card bottom pile outside the
// replacement pass.
func (e *Engine) EmitScryRecord(ev events.Event) { e.emitScryRecord(ev) }

// EmitTokenCreate emits a token-creation event (TokenCreate, Encore's
// CardToken copy, or the battlefield MoveZone of a CopyToken mint) and
// returns every token whose battlefield entry the emit actually completed --
// the ids publishTokenEntry (rules/token_rest.go) published, never a mint
// still parked behind an entry-counter order. A CreateToken replacement may rewrite one would-be token
// into several mints (Divine Visitation, Doubling Season, Xorn);
// effects/token.go consults this return so its per-token riders land on
// EVERY mint, not just the first. The sink is a stack: a nested token
// creation during this emit saves and restores it, so the outer call returns
// only its own plan's mints.
func (e *Engine) EmitTokenCreate(ev events.Event) []state.ObjID {
	var ids []state.ObjID
	saved := e.tokenMintSink
	e.tokenMintSink = &ids
	savedID := e.tokenMintSinkID
	e.tokenMintSinkID = 0 // the local buffer is never a named collector
	electionsBefore := [4]bool{e.etbMove != nil, e.riotMove != nil, e.unleashMove != nil, e.siegeMove != nil}
	queued, resumeBefore, choiceBefore := len(e.replChoices), e.resume, e.tokenChoice
	e.mintParkFrom = 0
	e.mintParkElection = false
	e.emit(ev)
	e.tokenMintSink, e.tokenMintSinkID = saved, savedID
	if e.resume != nil && e.resume != resumeBefore &&
		(len(e.replChoices) > queued || (e.tokenChoice != nil && e.tokenChoice != choiceBefore)) {
		// The mint (or a later mint of its plan) parked this resolution
		// behind a replacement-order ask or a CreateToken election: report
		// it so the caller can record its continuation (SuspendTokenRest).
		e.mintParkFrom = queued + 1
	}
	if e.resume != nil && e.resume != resumeBefore && e.etbElectionParked(electionsBefore) {
		// The mint's own entry parked on an as-enters election (a Riot,
		// Unleash, Siege or generic as-enter choice posed from inside the
		// emit): the election is the continuation the collector rides, not a
		// queued competition. Report it too, with the election marker
		// SuspendTokenRest consumes (it tags the election through
		// e.pendingMintSink instead of the queue).
		e.mintParkFrom = queued + 1
		e.mintParkElection = true
	}
	return ids
}

// etbElectionParked reports whether an as-enters election (etbMove, riotMove,
// unleashMove or siegeMove) parked during the emit just finished: its parked
// move went nil→non-nil, so the entry the emit was folding is held on the
// election's ask rather than folded.
func (e *Engine) etbElectionParked(before [4]bool) bool {
	return (e.etbMove != nil && !before[0]) || (e.riotMove != nil && !before[1]) ||
		(e.unleashMove != nil && !before[2]) || (e.siegeMove != nil && !before[3])
}

// EmitStackCopy emits a StackCopy event and returns the object it actually
// minted. The copy object is created inside events.Apply's StackCopy fold, so
// effects/copy.go's RememberCopies$ rider (Forge's card.addRemembered) cannot
// see its id from Emit; this method's sink records the pre-fold NextID the
// fold's AddObject assigns. The empty return is the fold's early breaks (no
// source, source already left the stack) -- a proposed copy that minted
// nothing, which must not be remembered. Same stack discipline as
// EmitTokenCreate, so a nested stack copy cannot leak its mint to the outer
// caller.
func (e *Engine) EmitStackCopy(ev events.Event) []state.ObjID {
	var ids []state.ObjID
	saved := e.stackCopyMintSink
	e.stackCopyMintSink = &ids
	e.emit(ev)
	e.stackCopyMintSink = saved
	return ids
}
func (e *Engine) EmitDamage(ev events.Event) events.Event { return e.emit(ev) }

// EmitLifeChange reports whether the exact proposed life change was applied.
// Kept for non-exchange callers; ExchangeLifeVariant uses the transactional
// method below so transformed and parked events can complete coherently.
func (e *Engine) EmitLifeChange(ev events.Event) bool {
	queued := len(e.replChoices)
	stored := e.emit(ev)
	return e.pending == nil && len(e.replChoices) == queued && stored.Kind == events.LifeChange && stored.Player == ev.Player && stored.Amount == ev.Amount
}

// ExchangeLifeVariant carries the exchange across a CR 616 choice. The life
// change is resolved first; the source's selected characteristic is set to
// its former total only if the player's life actually changed.
func (e *Engine) ExchangeLifeVariant(ev events.Event, source state.ObjID, controller state.PlayerID, oldLife int32, setPower, setToughness bool) {
	tx := &lifeExchangeTransaction{source: source, controller: controller, oldLife: oldLife,
		player: ev.Player, lifeBefore: e.G.Players[ev.Player].Life, setPower: setPower, setToughness: setToughness}
	e.lifeExchange = tx
	e.emit(ev)
	e.lifeExchange = nil
	if e.pending == nil && len(e.replChoices) == 0 {
		e.finishLifeExchange(tx)
	} else {
		// The life change suspended (a consumed GainLife→Draw body that itself
		// parked a Dredge ask): park the transaction so a later drain can
		// finish it. Nothing else references tx once this call returns.
		e.pendingLifeExchange = tx
	}
}

// settlePendingLifeExchange re-drives a parked exchange transaction once the
// engine is idle again. It is the ONE home every post-suspension drain calls
// (askNextReplacementChoice, and through it Submit's tail): a suspension that
// is not a replacement-order ask leaves the transaction referenced by nothing
// else, so without this the exchange silently half-applies. A second
// suspension inside finishLifeExchange re-parks it.
func (e *Engine) settlePendingLifeExchange() {
	tx := e.pendingLifeExchange
	if tx == nil || e.pending != nil || len(e.replChoices) > 0 {
		return
	}
	e.pendingLifeExchange = nil
	e.finishLifeExchange(tx)
}

// finishLifeExchange completes an exchange transaction. It resumes from the
// stage the transaction stopped at, so a re-drive after a suspension (the
// dredge arm, Submit's tail) settles exactly the sides not yet applied. It
// re-parks the transaction on the engine whenever a replacement path
// suspends again, so the transaction is never orphaned mid-flight.
func (e *Engine) finishLifeExchange(tx *lifeExchangeTransaction) {
	if tx == nil {
		return
	}
	if e.pendingLifeExchange == tx {
		e.pendingLifeExchange = nil
	}
	if e.pending != nil || len(e.replChoices) > 0 {
		e.pendingLifeExchange = tx
		return
	}
	if tx.second.Kind != 0 && tx.stage == 0 {
		tx.stage = 1
		prior := e.lifeExchange
		e.lifeExchange = tx
		e.emit(tx.second)
		e.lifeExchange = prior
		if e.pending != nil || len(e.replChoices) > 0 {
			e.pendingLifeExchange = tx
			return
		}
		e.finishLifeExchange(tx)
		return
	}
	if tx.second.Kind != 0 {
		if len(tx.staged) != 2 {
			e.emit(events.Event{Kind: events.Note, Obj: tx.source, Player: tx.controller,
				Text: "ExchangeLife settled with a replaced life-change side"})
		}
		prior, applying := e.lifeExchange, e.applyingReplacement
		e.lifeExchange, e.applyingReplacement = nil, true
		for _, ev := range tx.staged {
			e.emit(ev)
		}
		e.lifeExchange, e.applyingReplacement = prior, applying
		if tx.rememberLoss && tx.rememberMemory != nil &&
			int(tx.controller) < len(e.G.Players) {
			if loss := tx.controllerLife - e.G.Players[tx.controller].Life; loss > 0 {
				// The memory is the chain's SHARED pointer (Ctx.ExchangeMemory),
				// so this write is visible to every Ctx a suspension rebuilt:
				// the SubAbility$ continuation that reads Count$RememberedNumber
				// holds the same memory no matter which frame it resumed from.
				tx.rememberMemory.Number = loss
			}
		}
		return
	}
	if int(tx.player) >= len(e.G.Players) || e.G.Players[tx.player].Life == tx.lifeBefore {
		e.emit(events.Event{Kind: events.Note, Obj: tx.source, Player: tx.player, Text: "ExchangeLifeVariant abandoned: life did not change"})
		return
	}
	ce := state.ContinuousEffect{Source: tx.source, Controller: tx.controller, Affects: "Card.Self",
		Layer: state.LPT, Sub: state.SubSet, HasSet: true, SetPower: tx.oldLife, SetToughness: tx.oldLife,
		SetPowerPresent: tx.setPower, SetToughnessPresent: tx.setToughness, StaticSet: true}
	e.AddContinuous(ce)
}

func (e *Engine) stageExchangeLife(ev events.Event) events.Event {
	tx := e.lifeExchange
	if tx != nil {
		tx.staged = append(tx.staged, ev)
	}
	return ev
}

func (e *Engine) consumeExchangeLifeSide(ev events.Event) {
	if e.lifeExchange == nil || e.lifeExchange.second.Kind == 0 {
		return
	}
	e.emit(events.Event{Kind: events.Note, Obj: e.lifeExchange.source, Player: ev.Player,
		Text: "ExchangeLife settled with a replaced life-change side"})
	e.lifeExchange.staged = append(e.lifeExchange.staged, events.Event{Kind: events.LifeChange, Player: ev.Player})
}

// ExchangeLife carries both life changes through the replacement machinery as
// one continuation. Neither event is applied until both replacement paths,
// including any suspended CR 616 choices, have settled.
func (e *Engine) ExchangeLife(first, second events.Event, controller state.PlayerID, oldControllerLife int32, ctx *effects.Ctx, rememberLoss bool) {
	tx := &lifeExchangeTransaction{source: first.Obj, controller: controller, player: first.Player,
		lifeBefore: e.G.Players[first.Player].Life, second: second, rememberLoss: rememberLoss,
		controllerLife: oldControllerLife}
	if ctx != nil {
		tx.rememberMemory = ctx.ExchangeMemory
	}
	e.lifeExchange = tx
	e.emit(first)
	e.lifeExchange = nil
	if e.pending == nil && len(e.replChoices) == 0 {
		e.finishLifeExchange(tx)
	} else {
		// The FIRST side suspended (a replacement body that parked a decision):
		// park the transaction so a later drain emits the second side and
		// settles both. Without this, neither side is referenced again.
		e.pendingLifeExchange = tx
	}
}
func (e *Engine) Rand(n int) int { return e.rng.IntN(n) }

// shufflePlanarDeck uses the match RNG; the emitted event, rather than this
// temporary order, is the replayable state change.
func (e *Engine) shufflePlanarDeck(player state.PlayerID, order []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), order...)
	e.rng.Shuffle(out)
	return out
}

// Planeswalk rotates this seat's current plane to the bottom and reveals the
// next plane (CR 901.8). With fewer than two cards there is no next plane to
// reveal. The event names the plane being walked away from (Obj) so the away
// trigger has a source; the walk itself is the PlanarWalk fold.
func (e *Engine) Planeswalk(player state.PlayerID) {
	if int(player) >= len(e.G.Players) || len(e.G.Zone(state.ZPlanarDeck, player)) == 0 {
		return
	}
	var away state.ObjID
	if p := e.currentPlane(player); p != nil {
		away = p.ID
	}
	e.emit(events.Event{Kind: events.PlanarWalk, Player: player, Obj: away})
}

// PlaneswalkTo walks this seat to the named destination plane(s) instead of
// rotating the deck (CR 901.8's Defined$ planeswalk; Norn's Seedcore,
// Spatial Merging). dontPlaneswalkAway suppresses the PlaneswalkedFrom
// ability of the plane being left. A game with no planar deck, or with no
// destination still in the zone, records nothing: there is no plane to walk
// to. The event's Obj names the departing plane and IDs the destinations, so
// the fold in events/apply.go moves exactly those planes to the current
// position.
func (e *Engine) PlaneswalkTo(player state.PlayerID, dests []state.ObjID, dontPlaneswalkAway bool) {
	if int(player) >= len(e.G.Players) || len(e.G.Zone(state.ZPlanarDeck, player)) == 0 {
		return
	}
	zone := e.G.Zone(state.ZPlanarDeck, player)
	kept := make([]state.ObjID, 0, len(dests))
	for _, d := range dests {
		for _, id := range zone {
			if id == d {
				kept = append(kept, d)
				break
			}
		}
	}
	if len(kept) == 0 {
		return
	}
	var away state.ObjID
	if p := e.currentPlane(player); p != nil {
		away = p.ID
	}
	var amount int32
	if dontPlaneswalkAway {
		amount = events.PlanarWalkDontPlaneswalkAway
	}
	e.emit(events.Event{Kind: events.PlanarWalk, Player: player, Obj: away, Amount: amount, IDs: kept})
}

// ShuffleLibrary is the single library-shuffle path used by rules and effects.
// State changes only when the caller emits the resulting Shuffle event.
func (e *Engine) ShuffleLibrary(player state.PlayerID, order []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), order...)
	s := e.rng.chance
	if s == nil || s.planner == nil {
		e.rng.Shuffle(out)
		return out
	}
	ordinal := s.shuffleOrdinals[player]
	s.shuffleOrdinals[player] = ordinal + 1
	ctx := ShuffleContext{Player: player, Ordinal: ordinal, Library: e.shuffleCards(out), Hand: e.shuffleCards(e.G.Zone(state.ZHand, player))}
	desired, err := s.planner(ctx)
	if err != nil {
		s.fail(fmt.Errorf("hypothetical shuffle planner: %w", err))
	}
	if desired == nil {
		e.rng.Shuffle(out)
		return out
	}
	if err := e.rng.forcePermutation(out, desired); err != nil {
		s.fail(err)
	}
	return out
}

// EmitTap satisfies effects.Host's EmitTap: see emitTap.
func (e *Engine) EmitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	e.emitTap(obj, tapper, entering)
}

// emitTap emits the plain Tap event for obj while its provenance -- who tapped
// it, and whether it is only being given its entry state -- is visible to the
// Taps/TapsForMana matcher and to trigger referents. The event payload is the
// same one every Tap producer emitted before, so no chain head moves for a
// game without such a trigger. The previous context is restored rather than
// zeroed, so a Tap emitted from inside another Tap's trigger matching cannot
// clobber the outer one.
func (e *Engine) emitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	savedObj, savedPlayer, savedEntering := e.tapObj, e.tapPlayer, e.tapEntering
	e.tapObj, e.tapPlayer, e.tapEntering = obj, tapper, entering
	e.emit(events.Event{Kind: events.Tap, Obj: obj})
	e.tapObj, e.tapPlayer, e.tapEntering = savedObj, savedPlayer, savedEntering
}

// LegalTargets satisfies effects.Host for target-changing effects. It exposes
// the same census used by cast and trigger target decisions, so a redirect
// cannot bypass protection, CantTarget, stack-kind, zone, or filter legality.
//
// The census runs with the RESOLVING stack object as both source and
// excludeSelf whenever one exists -- exactly the placement ask's own call
// (pushTrigger -> askTarget passes the stack object id): CR 115.5 withholds
// the ability on the stack from targeting itself, never its source permanent,
// so a trigger whose source is a legal target may target it (Kor Outfitter's
// Attach sub attaches to Kor Outfitter). A direct, off-stack resolution (no
// resolving object) keeps the caller's source.
func (e *Engine) LegalTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA) []state.Target {
	if e.resolvingObj != 0 {
		source = e.resolvingObj
	}
	cs := e.legalTargetCandidates(chooser, source, source, sa)
	out := make([]state.Target, 0, len(cs))
	for _, c := range cs {
		if c.kind == "player" {
			out = append(out, state.Target{Player: c.player, IsPlayer: true})
		} else {
			out = append(out, state.Target{Obj: c.obj})
		}
	}
	return out
}

// CastThisTurn satisfies effects.Host's CastThisTurn for Count$ThisTurnCast
// (Task 17/Storm): the spells cast this turn by ANY player, counted from
// the same event log spellsCastThisTurn reads, so a replay that rebuilds the
// game derives the identical number -- never a live-only engine counter.
// Storm subtracts one (Count$ThisTurnCast/Minus1) because the resolving
// spell's own PutOnStack is already in the log by the time its trigger
// effect runs, which would otherwise overcount by exactly one.
func (e *Engine) CastThisTurn() int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack {
			n++
		}
	}
	return n
}

// SpellsCastThisTurnMatching satisfies effects.Host's
// SpellsCastThisTurnMatching for Count$ThisTurnCast_<spec> (the
// "first/second spell you cast" cost modifiers and triggers): spells put on
// the stack this turn whose object matches the Forge spec. When the spec
// carries a You* qualifier the count scopes to YOU's casts; otherwise it
// counts everyone's. Derived from the event log like CastThisTurn.
func (e *Engine) SpellsCastThisTurnMatching(you state.PlayerID, spec string) int {
	return len(e.spellsCastThisTurnMatching(you, spec, 0))
}

// SpellsCastThisTurnMatchingExcluding is SpellsCastThisTurnMatching with one
// object's own cast excluded -- the bare !CastSaSource qualifier's engine
// reading (the count's "other than the spell being cast" device; effects
// stripBareCastSaSource strips the token and routes here with the ctx
// source). Derived from the event log like CastThisTurn.
func (e *Engine) SpellsCastThisTurnMatchingExcluding(you state.PlayerID, spec string, exclude state.ObjID) int {
	return len(e.spellsCastThisTurnMatching(you, spec, exclude))
}

// EachSpellCastThisTurnMatching satisfies effects.Host's method of the same
// name: the matching casts' OBJECT IDS (the ARGUMENTED !CastSaSource$<Prop>
// aggregate forms' engine side; effects' aggregateCastProperty sums the
// property over them). Derived from the event log like the count forms.
func (e *Engine) EachSpellCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID {
	return e.spellsCastThisTurnMatching(you, spec, exclude)
}

func (e *Engine) spellsCastThisTurnMatching(you state.PlayerID, spec string, exclude state.ObjID) []state.ObjID {
	youScoped := strings.Contains(spec, "You")
	// The CastSa count specs (Rain of Riches' gate) are evaluated per cast
	// event against THAT cast's spend window, not the object's latest one —
	// a re-cast object's older cast must not inherit the newer cast's spend
	// — so the backward walk carries a per-caster spend bucket: a negative
	// ManaAdd (a spend event carries no Obj) belongs to the NEXT PutOnStack
	// the walk reaches for its player — the cast it sits above in the log —
	// exactly the window manaSpentForCast reads for the SA-level ValidSA$
	// family. Specs without a CastSa token take the unchanged per-event
	// chain call (their castProvenanceAdmits strip is event-local and
	// stateless).
	saTokens := castSaTokensIn(spec)
	// Flag tokens (CastSa Spell.Mayhem, Spell.MayPlaySource, Spell.Warp)
	// read the cast's pay-time CastInfo
	// flags rather than a spend bucket: the backward walk records each
	// object's most recent CastInfo flags (latest-first, first write wins)
	// and the push consumes its own cast's entry, so a re-cast object's
	// older cast never inherits the newer cast's flags — the per-event
	// mirror of castSaAdmits' latest-cast read. A plain cast emits no
	// pay-time CastInfo at all, so a missing entry reads as no flags.
	wantFlags := false
	for _, tok := range saTokens {
		if tok.flag != 0 {
			wantFlags = true
		}
	}
	var castFlags map[state.ObjID]uint64
	if wantFlags {
		castFlags = make(map[state.ObjID]uint64)
	}
	// The in-flight cast's own grant walk (queueCascadeTriggers' scratch,
	// rules/cascade.go) counts PRIOR casts only: the Affected$ half of the
	// same static evaluates the current cast's own qualification, and the
	// gate's EQ0 is Forge's "the first" idiom — the twelve AffectedZone$
	// Stack SVarCompare$ gates in the corpus are all EQ0. Outside the walk
	// the count is inclusive (Vengevine's "the second creature spell" EQ2
	// gate is evaluated with the triggering cast in the log and must count
	// it).
	var buckets [8]castSpendFacts
	useAcc := len(saTokens) > 0
	var out []state.ObjID
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		switch ev.Kind {
		case events.CastInfo:
			if wantFlags {
				if _, seen := castFlags[ev.Obj]; !seen {
					castFlags[ev.Obj] = events.FlagsFrom(ev.Counter)
				}
			}
			continue
		case events.ManaAdd:
			if useAcc && ev.Amount < 0 && int(ev.Player) < len(buckets) {
				buckets[ev.Player].spent += -ev.Amount
				if tag, _, ok := state.TypedManaCounter(ev.Counter); ok {
					base, artifact := state.ManaUnitTypes(tag)
					buckets[ev.Player].tagged[base] += -ev.Amount
					if artifact && base != state.TypedArtifact {
						buckets[ev.Player].tagged[state.TypedArtifact] += -ev.Amount
					}
				}
			}
			continue
		case events.PutOnStack:
		default:
			continue
		}
		// This push closes the spend window of the cast it announces: the
		// caster's bucket holds exactly the spends since the walk start,
		// which are this cast's own (plus the caster's own post-payment
		// floating — the manaSpentForCast convention). Take the facts and
		// reset, so an older cast of the same object (a hand cast before a
		// flashback) does not inherit them and the in-flight cast's window
		// belongs to no counted cast.
		var facts castSpendFacts
		if useAcc && int(ev.Player) < len(buckets) {
			facts = buckets[ev.Player]
			buckets[ev.Player] = castSpendFacts{}
		}
		// The push itself proves a cast exists: the window's ok read.
		facts.ok = true
		// This cast's own pay-time CastInfo flags (see wantFlags above): the
		// entry recorded at the CastInfo the backward walk already passed —
		// the payment runs after the push, so its CastInfo sits BELOW the
		// push in log order — is exactly this cast's.
		var evFlags uint64
		if wantFlags {
			evFlags = castFlags[ev.Obj]
			delete(castFlags, ev.Obj)
		}
		if (e.stackGrantCast != 0 && ev.Obj == e.stackGrantCast) ||
			(e.costCompositionEvent != 0 && i+1 == e.costCompositionEvent) {
			continue
		}
		if exclude != 0 && ev.Obj == exclude {
			continue
		}
		if youScoped && ev.Player != you {
			continue
		}
		matchSpec := spec
		alive := true
		for _, tok := range saTokens {
			var held bool
			if matchSpec, held = admitProvenanceAlternatives(matchSpec, tok.token, castSaTokenHolds(tok, facts, evFlags)); !held {
				alive = false
				break
			}
		}
		if !alive {
			continue
		}
		// The bare wasCastFromYourHandByYou qualifier (the 5 end-step "if you
		// haven't cast a spell from your hand this turn" carriers'
		// Count$ThisTurnCast_Card.wasCastFromYourHandByYou bodies) is
		// evaluated per cast event against the log (task castprov1); the
		// wasCastByYou sibling (task castprov2) rides the same combined read.
		matchSpec, ok := e.castProvenanceAdmits(matchSpec, ev.Obj, you)
		if !ok {
			continue
		}
		if e.matchesSpecFrom(matchSpec, ev.Obj, you, ev.Obj) {
			out = append(out, ev.Obj)
		}
	}
	return out
}

// WasCastFromHandByYou satisfies effects.Host's WasCastFromHandByYou for the
// Count$wasCastFromYourHandByYou branch head (the Myojin cycle's etbCounter
// CheckSVar$ gate) and the Card.wasCastFromYourHandByYou filter predicate:
// obj's latest PutOnStack event names the cast that put it on the stack —
// From is the zone the cast came from, Player the caster. Provenance is
// game-long, so the scan is not bounded by the turn; if the card was later
// cast again from another zone, the latest cast wins. Derived from the event
// log like SpellsCastThisTurnMatching, so a replay derives the same answer.
func (e *Engine) WasCastFromHandByYou(obj state.ObjID, p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return ev.From == state.ZHand && ev.Player == p
		}
	}
	return false
}

// WasCastFromHand satisfies effects.Host's WasCastFromHand for the BARE
// wasCastFromYourHand filter family (task castprov3 — the "from anywhere
// other than your hand" carriers whose scripts spell the predicate without
// the ByYou suffix: Vega the Watcher, Bilbo Thief in the Night, Mm'menon's
// RestrictValid$): obj's latest PutOnStack event names the cast that put it
// on the stack, and From is the zone that cast came from — ANY caster. Every
// carrier that needs player scoping supplies it elsewhere (measured over the
// 46 raw carrier files: ValidActivatingPlayer$ You on the trigger lines,
// YouCtrl or wasCastByYou in the same Affected$/Count spec). A copy was
// never cast (the rules-side split, castProvenanceAdmits, applies the same
// IsCopy guard; this read answers the log question alone); a card never put
// on the stack (cheated into play) reads false. Derived from the event log
// like WasCastFromHandByYou, so a replay derives the same answer;
// latest-cast-wins.
func (e *Engine) WasCastFromHand(obj state.ObjID) bool {
	from, _, ok := e.latestCastOrigin(obj)
	return ok && from == state.ZHand
}

// WasCastFromExile satisfies effects.Host's WasCastFromExile for the
// Count$wasCastFromExile branch head (task wascastfrom): obj's LATEST
// PutOnStack cast came from EXILE (foretell, warp, may-play — no CastFlags
// bit carries an exile origin). Derived from the event log the way
// WasCastFromHand is, so a replay derives the same answer; a copy was never
// cast (the rules-side split applies that guard, this read answers the log
// question alone); a card never put on the stack reads false.
func (e *Engine) WasCastFromExile(obj state.ObjID) bool {
	from, _, ok := e.latestCastOrigin(obj)
	return ok && from == state.ZExile
}

// DiscardedInWindow satisfies effects.Host's DiscardedInWindow for the
// ConditionDefined$ Discarded group's cost-discard channel (task
// mordorparams1, Moria Scavenger's "If the discarded card was a creature
// card"): the events.DiscardCost records of obj's own activation. The walk
// itself (and the activation-window boundary rule) is costMovesInWindow's.
func (e *Engine) DiscardedInWindow(obj state.ObjID) []state.ObjID {
	return e.costMovesInWindow(obj, events.IsDiscardCost)
}

// ReturnedInWindow satisfies effects.Host's ReturnedInWindow: the
// Return<N/Spec> cost parts obj's own activation paid (events.IsReturnCost),
// enumerated over the same activation window DiscardedInWindow scans. It is
// the ONE other user of costMovesInWindow, so a third cost-provenance window
// (a new cost action marker) reuses the walk rather than copying it.
func (e *Engine) ReturnedInWindow(obj state.ObjID) []state.ObjID {
	return e.costMovesInWindow(obj, events.IsReturnCost)
}

// costMovesInWindow walks obj's activation window backward over the event log
// and returns (in log order) every MoveZone event match admits — the cost
// acts obj's OWN activation paid, which is what the ConditionDefined$
// Discarded/Returned groups enumerate. The scan starts at obj's resolving
// wrapper and stops at the first unrelated stack push, step/turn change, pool
// clear or player loss — while crossing obj's OWN push events, because the
// two cost orderings share the one rule: an ability's cost parts are paid
// BEFORE its AbilityPush mints the wrapper (rules/cast.go's activation
// branch), a spell's AFTER its PutOnStack (the spell branch), and no other
// wrapper's push can sit between a cost payment and the resolution that
// follows it. Priority passes are deliberately NOT a boundary: an activated
// ability can sit on the stack across any number of passes before it
// resolves, and the cost it paid belongs to exactly that resolution. Derived
// from the log the way WasCastFromHandByYou is, so a replay derives the same
// answer.
func (e *Engine) costMovesInWindow(obj state.ObjID, match func(events.Event) bool) []state.ObjID {
	if obj == 0 {
		return nil
	}
	// An ability wrapper's AbilityPush carries the SOURCE permanent's id
	// (events.Apply mints the wrapper; Event.Obj names its source), so the
	// scan crosses its own push by wrapper id or source id alike.
	var src state.ObjID
	if o := e.G.Obj(obj); o != nil {
		src = o.Source
	}
	var out []state.ObjID
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		switch ev.Kind {
		case events.MoveZone:
			if match(ev) {
				out = append(out, ev.Obj)
				continue
			}
			// An ordinary move inside the window is not a boundary — an
			// ability's payment can move several cards (exile parts, tapped
			// entries) between its cost payment and its push.
			continue
		case events.PutOnStack, events.AbilityPush, events.TriggerPush,
			events.DelayedPush, events.GrantTriggerPush:
			if ev.Obj == obj || (src != 0 && ev.Obj == src) {
				continue // the resolving object's own push: cross it
			}
			return reverseIDs(out)
		case events.StepChange, events.TurnChange, events.ManaClear, events.PlayerLost:
			return reverseIDs(out)
		}
	}
	return reverseIDs(out)
}

// reverseIDs restores log order to a backward scan's collection.
func reverseIDs(in []state.ObjID) []state.ObjID {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	return in
}

// WasCast satisfies effects.Host's WasCast (Forge Card.wasCast():
// castFrom != null), the Count$IfCastInOwnMainPhase third conjunct (task
// ifcastmain1). The pending CR 601.2c announcement ask is a cast in progress:
// pushCast runs AFTER targetAsk, so the log scan alone would misread Return
// to Dust's own TargetMax$ X bound as uncast; the live pending cast closes
// that window (Forge sets castFrom before setupTargets). !e.cast.isAbility()
// excludes an ACTIVATED-ABILITY activation (printed or granted, task
// grantcost1), which Forge never treats as a cast. A copy was never cast
// (IsCopy), and a card never put on the stack (cheated into play) reads
// false. Derived from the event log plus the live pending cast, so a replay
// derives the same answer.
func (e *Engine) WasCast(obj state.ObjID) bool {
	if e.cast != nil && e.cast.card == obj && !e.cast.isAbility() {
		return true
	}
	if o := e.G.Obj(obj); o == nil || o.IsCopy {
		return false
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == obj {
			return true
		}
	}
	return false
}

// WasCastByYou reports whether card obj was CAST AT ALL by player p — the
// bare wasCastByYou qualifier's engine read (task castprov2: the "When
// CARDNAME enters, if you cast it" ETB family — Zacama, Marina Vendrell's
// Grimoire — and Nine-Lives Familiar's etbCounter gate field): the LATEST
// PutOnStack event for this object names you as caster, whatever zone the
// cast came from (a normal hand cast, a flashback, any origin — the oracle's
// "if you cast it" does not care where from). LATEST-cast, not exists-anywhere:
// the battlefield entry this gate answers for followed the latest cast, so
// that cast is the provenance the oracle means; the corner this leaves is
// you cast it, it left the battlefield again, and an OPPONENT later cast the
// same object — the gate then reads false even though you did cast it
// (measured: no corpus carrier exercises the corner; an exists-scan would
// instead answer true for a card whose latest cast was an opponent's, the
// wider wrong). Copies were never cast; the rules-side split
// (castProvenanceAdmits) applies that guard, this read answers the log
// question alone. Derived from the event log like WasCastFromHandByYou, so
// a replay derives the same answer; a card never put on the stack (cheated
// into play) reads false, and so does a card that left the battlefield after
// that cast and came back without one (reanimated, blinked: CR 400.7).
func (e *Engine) WasCastByYou(obj state.ObjID, p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Obj != obj {
			continue
		}
		if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield {
			// It left the battlefield after its latest cast: whatever is asked
			// about now is a new object that was never cast (CR 400.7) --
			// Nine-Lives Familiar's own return must not re-enter with eight.
			return false
		}
		if ev.Kind == events.PutOnStack {
			return ev.Player == p
		}
	}
	return false
}

// DepartureCounters answers the counters obj carried when it last left the
// battlefield (CR 608.2h's last-known information for a reader that has no
// trigger snapshot -- a delayed trigger's remembered card). Derived from the
// event log: the counter changes between its latest battlefield entry and its
// latest departure, folded in order. ok is false
// when the log holds no departure.
func (e *Engine) DepartureCounters(obj state.ObjID) ([]state.Counter, bool) {
	left := -1
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := &e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == obj && ev.From == state.ZBattlefield {
			left = i
			break
		}
	}
	if left < 0 {
		return nil, false
	}
	entered := 0
	for i := left - 1; i >= 0; i-- {
		ev := &e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == obj && ev.To == state.ZBattlefield {
			entered = i + 1
			break
		}
	}
	var scratch state.Object
	for i := entered; i < left; i++ {
		ev := &e.L.Events[i]
		// An EntryCounterNotice counts too: its placement was folded inside the
		// entry MoveZone, and the notice is the log's only record of it.
		if ev.Kind == events.CounterChange && ev.Obj == obj {
			scratch.AddCounter(ev.Counter, ev.Amount)
		}
	}
	return scratch.Counters, true
}

// combatHit snapshots one landed combat-damage-to-player instance for the
// per-turn ledger. The dealing object is read through g.Obj at damage time
// (it is still on the battlefield then); its *cards.Card face pointer and
// controller are copied into the hit so a later reader can match the spec
// after the source has died, left the battlefield or been turned face down.
func (e *Engine) combatHit(player state.PlayerID, source state.ObjID, amount int32) effects.CombatDamageHit {
	hit := effects.CombatDamageHit{Player: player, Source: source, Amount: amount}
	if o := e.G.Obj(source); o != nil {
		hit.Card = o.Card
		hit.FaceIdx = o.FaceIdx
		hit.Controller = o.Controller
	}
	return hit
}

// CombatDamageToPlayersThisTurn satisfies effects.Host's
// CombatDamageToPlayersThisTurn: every combat-damage instance dealt to a
// player so far this turn, in assignment order, as captured at the combat
// damage site (runCombatAssignments). Engine-side, NO-EVENT state that every
// rebuild re-derives; emit clears it on TurnChange.
func (e *Engine) CombatDamageToPlayersThisTurn() []effects.CombatDamageHit {
	return e.combatHitsThisTurn
}

// LifeLostThisTurn satisfies effects.Host's LifeLostThisTurn for
// Count$LifeOppsLostThisTurn (Rakdos, Lord of Riots' cost reduction): the
// total life p lost this turn, summed since the last TurnChange over every
// event lifeLoss classifies as a loss -- a LifeChange below zero AND a
// player Damage event. Damage dealt to a player causes that much life loss
// (CR 120.3a, 119.3) but folds straight to the life total with no
// LifeChange (events.Apply's Damage case), so a LifeChange-only fold
// missed every point of combat and burn damage (Stromkirk Bloodthief).
// Infect-marked player damage is poison, not life loss (CR 702.90b), and
// lifeLoss already excludes it. Using the one classifier the LifeLost
// triggers and the speed check read keeps the count and those triggers
// agreeing. Derived from the event log like CastThisTurn, so a replay that
// rebuilds the game arrives at the same number. Life GAINED is not folded
// in — "lost life" is a loss even if the player ended the turn higher than
// they started (CR 118.3's distinction, and the reading Forge's own head
// takes).
func (e *Engine) LifeLostThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if q, amount, ok := lifeLoss(ev); ok && q == p {
			n += amount
		}
	}
	return n
}

// CountersRemovedThisTurn satisfies effects.Host's CountersRemovedThisTurn
// for Count$CountersRemovedThisTurn <KIND> <player> (Blaster Hulk's per-{E}
// cast discount and Izzet Generatorium's paid-or-lost-four-or-more {E}
// activation gate): the TOTAL of player counters of kind p paid or lost this
// turn, summed from every negative-Amount PlayerCounterChange naming the kind
// (case-insensitively — the grants and the pays write the same kind text a
// card's script uses, e.g. "ENERGY") since the last TurnChange. Derived from
// the event log like LifeLostThisTurn, so a replay derives the same number.
// A payment and a loss are the same event shape — rules/mana.go's PayEnergy
// settle emits exactly this fold's input — and an object-counter removal
// (Kind CounterChange, a permanent losing counters) is deliberately NOT
// folded: the head's player form counts the PLAYER's pool only.
func (e *Engine) CountersRemovedThisTurn(p state.PlayerID, kind string) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PlayerCounterChange && ev.Player == p && ev.Amount < 0 &&
			strings.EqualFold(ev.Counter, kind) {
			n += -ev.Amount
		}
	}
	return n
}

// CountersAddedThisTurn is the rules-side backing for the three-part
// Count$CountersAddedThisTurn head. It deliberately uses the pre-event
// snapshot retained by emit rather than the live object.
func (e *Engine) CountersAddedThisTurn(kind, actorSpec, objectSpec string, sc effects.SpecContext) int32 {
	var n int32
	for _, add := range e.counterAddsThisTurn {
		if !strings.EqualFold(kind, "Any") && !strings.EqualFold(add.kind, kind) ||
			!effects.MatchesPlayerSpec(e.G, actorSpec, add.actor, sc.You) ||
			!effects.MatchesObjectCtx(e.G, objectSpec, &add.object, sc) {
			continue
		}
		n += add.amount
	}
	return n
}

// DamageTakenThisTurn satisfies effects.Host's DamageTakenThisTurn for the
// TargetedPlayer$DamageThisTurn count head (Knollspine Dragon's "draw cards
// equal to the damage dealt to target opponent this turn"): the total damage
// p was dealt this turn, summed from every player-targeted Damage event
// since the last TurnChange. A player hit is Kind Damage with Player set
// and Obj 0 — an object hit sets Obj and leaves Player 0 (seat 0 is a real
// player, so the discriminator is Obj == 0, never Player != 0); a
// replacement-rewritten Note never reaches this fold, and a redirect that
// moved a hit onto a permanent reads there instead. Derived from the event
// log like LifeLostThisTurn, so a replay derives the same number.
func (e *Engine) DamageTakenThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == p && ev.Amount > 0 {
			n += ev.Amount
		}
	}
	return n
}

// LifeGainedThisTurn satisfies effects.Host's LifeGainedThisTurn for
// Count$LifeYouGainedThisTurn (the "At the beginning of each end step, if you
// gained 4 or more life this turn" family's CheckSVar$ gate — Angelic Accord,
// Resplendent Angel, Valkyrie Harbinger): the total life p gained this turn,
// summed from every LifeChange above zero since the last TurnChange. Derived
// from the event log like LifeLostThisTurn, so a replay that rebuilds the
// game arrives at the same number. Life LOST is not folded in — "gained
// life" counts only positive LifeChanges (CR 118.3's distinction, the same
// one-sided read LifeLostThisTurn takes in the other direction).
func (e *Engine) LifeGainedThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.LifeChange && ev.Player == p && ev.Amount > 0 {
			n += ev.Amount
		}
	}
	return n
}

// CardsDiscardedThisTurn satisfies effects.Host's CardsDiscardedThisTurn for
// PlayerCountPropertyYou$CardsDiscardedThisTurn (Ambergris Citadel Agent's
// "X = cards you discarded this turn"): every events.IsDiscard move since
// the last TurnChange naming p — the ordinary Discard form by its Player
// field, the cost form (events.DiscardCost, which carries no Player — every
// emitter constructs it without one, so the Player field is seat 0 regardless
// of who paid) by the discarded object's owner alone, since a cost discard is
// paid from the payer's own hand (CR 118.2a). Classifying by the marker and
// not by "Player == 0 as a fallback" is what keeps seat 0's tally from
// counting every other seat's cost discard. Derived from the event log like
// LifeLostThisTurn, so a replay derives the same number.
func (e *Engine) CardsDiscardedThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if !events.IsDiscard(ev) {
			continue
		}
		if events.IsDiscardCost(ev) {
			if o := e.G.Obj(ev.Obj); o != nil && o.Owner == p {
				n++
			}
			continue
		}
		if ev.Player == p {
			n++
		}
	}
	return n
}

// CardsDrawnThisTurn satisfies effects.Host's CardsDrawnThisTurn for the
// PlayerCount<group>$Condition<N> CardsDrawn property (Smuggler's Share's
// "draw a card for each opponent who drew two or more cards this turn"):
// every events.Draw naming p since the last TurnChange. Derived from the
// event log like CardsDiscardedThisTurn, so a replay derives the same
// number. Every draw emitter — the draw step, an effect's Draw and the
// opening hand — emits the same event kind with Player set, so the fold
// counts them all, exactly as Forge's per-turn cardsDrawn list does.
func (e *Engine) CardsDrawnThisTurn(p state.PlayerID) int32 {
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// SpellsCastThisTurnBy satisfies effects.Host's SpellsCastThisTurnBy for the
// PlayerCount<group>$Condition<N> SpellsCastThisTurn property (Ertai's
// Scorn / Mindbreak Trap / Whiplash Trap: "for each opponent who cast two
// or more spells this turn"): every PutOnStack naming p since the last
// TurnChange. Derived from the event log like CastThisTurn, so a replay
// derives the same number.
func (e *Engine) SpellsCastThisTurnBy(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p {
			n++
		}
	}
	return n
}

// StartingLife satisfies effects.Host's StartingLife: the opening life total
// genesis resolved (Config.StartingLife's 0-means-20 convention already
// applied in newWithRNG). Captured at construction and copied by Clone, so a
// replay derives the same value.
func (e *Engine) StartingLife() int32 { return e.startingLife }

// TurnsTaken satisfies effects.Host's TurnsTaken for Count$YourTurns (Serra
// Avenger's "your first, second, or third turns of the game"): the number of
// turns that have BEGUN with p as the active player, current turn included.
// Derived from the event log like LifeLostThisTurn — every turn p begins
// emits exactly one TurnChange naming p (events.Apply's TurnChange case),
// extra turns included, so a replay that rebuilds the log arrives at the
// same count. The whole-log walk (not a TurnChange-bounded scan) is the
// point: the count spans the game, not one turn.
// CommanderCastsFromCommandZone satisfies effects.Host's method of the
// same name for Count$TotalCommanderCastFromCommandZone: how many times
// player p has cast one of THEIR OWN commanders from the command zone this
// game. It walks the whole log for PutOnStack events whose caster is p,
// origin is the command zone and object is one of p's commanders — the
// exact criteria recordCmdCast (rules/cast.go) applies when it maintains
// the parallel CmdCasts slice, and commitCast's PutOnStack emit is the ONE
// site that can produce such an event, so this head and the CR 903.8 tax
// can never disagree. Whole-game scope like TurnsTaken (the whole-log walk
// is the point); derived from the event log, so a replay derives the same
// number. A non-Commander seat carries an empty Commanders list, so the
// count is 0 there by construction.
func (e *Engine) CommanderCastsFromCommandZone(p state.PlayerID) int32 {
	if p < 0 || int(p) >= len(e.G.Players) {
		return 0
	}
	var n int32
	for _, ev := range e.L.Events {
		if ev.Kind != events.PutOnStack || ev.Player != p || ev.From != state.ZCommand {
			continue
		}
		for _, cid := range e.G.Players[p].Commanders {
			if cid == ev.Obj {
				n++
				break
			}
		}
	}
	return n
}

func (e *Engine) TurnsTaken(p state.PlayerID) int32 {
	if int(p) >= len(e.G.Players) {
		return 0
	}
	if len(e.turnsTaken) != len(e.G.Players) || e.turnsTakenEpoch != len(e.L.Events) {
		e.turnsTaken = make([]int32, len(e.G.Players))
		for _, ev := range e.L.Events {
			if ev.Kind == events.TurnChange && int(ev.Player) < len(e.turnsTaken) {
				e.turnsTaken[ev.Player]++
			}
		}
		e.turnsTakenEpoch = len(e.L.Events)
	}
	return e.turnsTaken[p]
}

// AttackersThisTurn satisfies effects.Host's AttackersThisTurn for
// Count$AttackersDeclared (the Raid family's "attacked this turn" read): the
// number of attackers declared this turn, summed from every DeclareAttackers
// event's attacker list since the last TurnChange. Derived from the event log
// like CastThisTurn, so a replay that rebuilds the game arrives at the same
// number. A DeclareAttackers event carries its declared attackers in IDs (one
// event per defender); an event with no IDs contributes nothing.
// AttackersDeclaredThisTurn satisfies effects.Host's method of the same
// name: this turn's DeclareAttackers attacker ids, de-duplicated, oldest
// first. Derived from the event log like AttackersThisTurn.
func (e *Engine) AttackersDeclaredThisTurn() []state.ObjID {
	start := len(e.L.Events)
	for start > 0 && e.L.Events[start-1].Kind != events.TurnChange {
		start--
	}
	var out []state.ObjID
	seen := map[state.ObjID]bool{}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind != events.DeclareAttackers {
			continue
		}
		for _, id := range ev.IDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// LifeLostLastTurn satisfies effects.Host's LifeLostLastTurn: the life
// losses (lifeLoss: negative LifeChanges and non-infect player damage)
// naming p between the second-to-last and the last TurnChange of the log --
// the previous turn's window, the LifeLostThisTurn fold one turn back.
func (e *Engine) LifeLostLastTurn(p state.PlayerID) int32 {
	var n int32
	boundaries := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			boundaries++
			if boundaries == 2 {
				break
			}
			continue
		}
		if boundaries != 1 {
			continue
		}
		if q, amount, ok := lifeLoss(ev); ok && q == p {
			n += amount
		}
	}
	if boundaries < 2 {
		// The window before the first TurnChange is the pregame, not a turn.
		return 0
	}
	return n
}

// AttackedDuringLastTurn satisfies effects.Host's method of the same name.
// The walk runs backwards over the log: the window after the LAST
// TurnChange is the turn in progress and is skipped; the first earlier
// TurnChange naming q opens q's most recent completed turn, whose events
// run up to the next TurnChange. A DeclareAttackers inside it whose
// Player (the defending seat) is defender and whose attacker list is
// non-empty answers true.
func (e *Engine) AttackedDuringLastTurn(q, defender state.PlayerID) bool {
	ev := e.L.Events
	end := len(ev)
	// Skip the turn in progress.
	for end > 0 && ev[end-1].Kind != events.TurnChange {
		end--
	}
	if end == 0 {
		return false
	}
	end-- // the current turn's TurnChange itself
	for end > 0 {
		start := end
		for start > 0 && ev[start-1].Kind != events.TurnChange {
			start--
		}
		if start == 0 {
			return false // the pregame window, before any turn
		}
		if ev[start-1].Player == q {
			for _, x := range ev[start:end] {
				if x.Kind == events.DeclareAttackers && x.Player == defender && len(x.IDs) > 0 {
					return true
				}
			}
			return false
		}
		end = start - 1
	}
	return false
}

func (e *Engine) AttackersThisTurn() int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.DeclareAttackers {
			n += len(ev.IDs)
		}
	}
	return n
}

// targetsPlayers and targetsPermanents read the coarse shape of a ValidTgts
// spec. The per-object predicate work is effects.MatchesSpec.

// targetsPlayers reports whether a spec can name a player as a target, by
// looking at each alternative's BASE type (the part before any "."), never
// a substring scan: "Any" targets either an object or a player, "Player"/
// "Opponent"/"You" a player, while a predicate like "Creature.YouCtrl" IS an
// object filter (its ".YouCtrl" clause scopes the creature's controller,
// not the target being a player). The old substring form matched the "You"
// inside "YouCtrl" and wrongly offered players as Equip targets (Task 14
// found it: an Equip onto Creature.YouCtrl offered both players as options,
// which effAttach then had to refuse).
func targetsPlayers(spec string) bool {
	for alt := range strings.SplitSeq(spec, ",") {
		switch base, _, _ := strings.Cut(strings.TrimSpace(alt), "."); base {
		case "Player", "Any", "Opponent", "You":
			return true
		}
	}
	return false
}

func targetsPermanents(spec string) bool {
	for _, t := range [...]string{"Creature", "Any", "Permanent", "Artifact",
		"Enchantment", "Land", "Planeswalker", "Card"} {
		if strings.Contains(spec, t) {
			return true
		}
	}
	return false
}

// payUnlessCost charges the non-choice subset of a mid-resolution
// UnlessCost$ to payer p. Sacrifice, discard, reveal, return and exile
// components are deliberately refused here: beginUnlessPayment owns every
// such component and gathers the payer's selected objects before it calls
// payMana. Keeping this guard makes a future caller unable to silently
// revive the old first-in-zone-order stand-in. Fixed mana/life, Mill,
// SubCounter and Draw
// components remain synchronous: a Draw<N/Spec> pays by drawing N cards for
// the player(s) the spec names (default the payer), resolved through the
// same Ctx roles the UnlessPayer$ grammar reads, and a Mill<N> mills from
// the top of the payer's own library through the shared payMillCost (CR
// 701.13a: every remaining card when fewer than N remain, so any library
// size is payable). The dynamic life folds
// (LifeTotalHalfUp, an announced PayLife<X>) and the energy parts (fixed and
// announced-X PayEnergy) charge here too, under the same offer gate's reads
// (unlessFoldDynamic / unlessEnergyAffordable), so the gate and the charge
// can never disagree.
func (e *Engine) payUnlessCost(p state.PlayerID, cost Cost, ctx *effects.Ctx, stackObj state.ObjID) bool {
	if len(cost.Sac) != 0 || len(cost.Discard) != 0 || len(cost.Reveal) != 0 || len(cost.Behold) != 0 || len(cost.RevealOrChoose) != 0 || len(cost.RevealChosen) != 0 || len(cost.Return) != 0 || len(cost.Exile) != 0 {
		return false
	}
	if int(p) < 0 || int(p) >= len(e.G.Players) {
		return false
	}
	folded, ok := e.unlessFoldDynamic(p, cost, ctx)
	if !ok {
		return false
	}
	cost = folded
	if !e.unlessEnergyAffordable(p, cost, ctx) {
		return false
	}
	g := e.G
	// The source the SubCounter parts drain is the activated ability's host
	// when this is an ability object, otherwise the resolving source.
	src := ctx.Source
	if o := g.Obj(stackObj); o != nil && o.Ability != nil {
		src = o.Source
	}
	type counterDrain struct {
		obj  state.ObjID
		kind string
		n    int32
	}
	var drains []counterDrain
	for _, part := range cost.SubCounter {
		o := g.Obj(src)
		if o == nil {
			return false
		}
		have := int32(0)
		for _, ct := range o.Counters {
			if ct.Kind == part.Spec {
				have += ct.N
			}
		}
		if have < part.N {
			return false
		}
		drains = append(drains, counterDrain{obj: o.ID, kind: part.Spec, n: part.N})
	}
	// Resolve every drawer before charging mana/life. A Draw component whose
	// role is unavailable makes the entire cost unpayable; validating first
	// avoids a partial payment followed by a silent omitted draw.
	drawers := make([][]state.PlayerID, len(cost.Draw))
	for i, part := range cost.Draw {
		players, ok := unlessDrawPlayers(ctx, p, part.Spec)
		if !ok {
			return false
		}
		for _, dp := range players {
			if int(dp) < 0 || int(dp) >= len(g.Players) {
				return false
			}
		}
		drawers[i] = players
	}
	// Everything is affordable: charge mana/life through ordinary events,
	// then apply the synchronous counter components, then the draws. The
	// resolving object is the payment subject, so its ManaConvert statics
	// (including EffectZone$ Command and Effect-delivered grants) apply here
	// under the same conversion read used by cast offers.
	if !e.payManaConv(p, cost, e.paymentConv(p, stackObj, false)) {
		return false
	}
	// The energy parts charge through the ONE shared site (CR 118.2d); the
	// offer gate proved the total affordable and the fold above proved every
	// dynamic part bound, so the charge cannot half-apply.
	x := int32(0)
	if ctx != nil && ctx.XAnnounced {
		x = ctx.X
	}
	e.chargeEnergyCost(p, cost, x)
	// Mill parts (Mill<N>) settle through the ONE shared mill site, after
	// every payability check above has passed and beside the other charges,
	// so the ordinary cast/activation cost and an unless cost cannot diverge.
	// CR 701.13a: the payer mills the SUM of the parts' requirements, taking
	// every remaining card when the library is short, so this never turns an
	// empty or short library into an unpayable cost.
	e.payMillCost(p, cost.Mill)
	for _, d := range drains {
		e.emit(events.Event{Kind: events.CounterChange, Obj: d.obj, Counter: d.kind, Amount: -d.n})
	}
	for i, part := range cost.Draw {
		for _, dp := range drawers[i] {
			for n := int32(0); n < part.N; n++ {
				effects.DrawFor(e, dp)
			}
		}
	}
	return true
}

// unlessDrawPlayers resolves a Draw<N/Spec> cost component's drawer(s). The
// empty spec and "You" are the payer; every other spelling is one of the
// player roles the unless-payment context carries, and an unresolvable or
// unknown spec fails closed (the cost was not paid).
func unlessDrawPlayers(ctx *effects.Ctx, payer state.PlayerID, spec string) ([]state.PlayerID, bool) {
	one := func(t state.Target) ([]state.PlayerID, bool) {
		if t.IsPlayer {
			return []state.PlayerID{t.Player}, true
		}
		return nil, false
	}
	switch spec {
	case "", "You", "Player", "Self":
		return []state.PlayerID{payer}, true
	case "Player.targetedBy", "Targeted", "TargetedPlayer":
		if len(ctx.Targets) == 0 {
			return nil, false
		}
		return one(ctx.Targets[0])
	case "Player.Activator", "TriggeredActivator":
		return one(ctx.TriggerActivator)
	case "Player.TriggeredPlayer", "TriggeredPlayer":
		return one(ctx.TriggerPlayer)
	case "Player.TriggeredTarget", "TriggeredTarget":
		return one(ctx.TriggerTarget)
	}
	return nil, false
}
