package effects

import (
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// fakeHost is the smallest thing satisfying Host: a real Game plus a captured
// event list, so effect tests assert on emitted events rather than internals.
// continuous captures every AddContinuous call the same way log captures
// every Emit call -- effects package tests have no rules.Engine (and no
// layer computation) to check the resulting Power/Toughness against, so they
// can only assert on what got registered, not on its downstream effect; the
// two engine-level end-to-end tests in rules/layers_pump_test.go are what
// check the actual computed result.
// counterAdderUnsetFake mirrors rules' counterAdderUnset token for the
// effects-package double (seat 255 cannot exist).
const counterAdderUnsetFake = state.PlayerID(255)

type fakeHost struct {
	g            *state.Game
	log          []events.Event
	continuous   []state.ContinuousEffect
	controls     []ControlGrant
	n            int
	dmgSrc       state.ObjID
	counterAdder state.PlayerID
	batch        []state.ObjID
	// exploitedLKI records the snapshots effExploit publishes through
	// RememberExploitedLKI, keyed by exploited object id, so an effects-level
	// test can assert the marker publication without an engine.
	exploitedLKI map[state.ObjID]state.SacrificedInfo
	// sacrificeBlocked is the per-object SacrificeBlocked answer the double
	// reports (nil = nothing blocked, the default): the engine-side CantSacrifice
	// machinery lives in rules.Engine, so the effects-package tests configure the
	// answers they need instead of inventing a registry.
	sacrificeBlocked map[state.ObjID]bool
	// exileBlocked is the per-object ExileBlocked answer the double reports
	// (nil = nothing blocked, the default), the CantExile counterpart of
	// sacrificeBlocked above.
	exileBlocked map[state.ObjID]bool
	// castFromHand is the WasCastFromHandByYou answer the double reports;
	// the eval-level Count$wasCastFromYourHandByYou tests flip it to pin the
	// true branch (the real log-scan read is pinned in rules).
	castFromHand bool
	// wasCast is the WasCast answer the double reports (task ifcastmain1):
	// the eval-level Count$IfCastInOwnMainPhase tests flip it to pin the
	// true/false branches independently of the live main-phase read.
	wasCast bool
	// revolt is the RevoltHolds answer the double reports (the bare
	// Condition$ Revolt gate and the Count$Revolt branch head read it; the
	// real event-log scan is pinned in rules).
	revolt bool
	// delirium is the DeliriumHolds answer the double reports (the bare
	// Condition$ Delirium gate reads it; the real graveyard-census read is
	// pinned in rules).
	delirium bool
	// metalcraft is the MetalcraftHolds answer used by bare Condition$ tests.
	metalcraft bool
	// typeChoices is the TypeChoices answer the double reports (nil by
	// default): the effects-side ChooseType tests configure it to pose a
	// real option list. Nil routes ChooseType through AskEmpty — the
	// unchanged deterministic fallback.
	typeChoices []decision.Option
	// combatHits is the CombatDamageToPlayersThisTurn answer the double
	// reports; the effects-level PlayerCountDefinedRegistered tests set it to
	// drive the combat-damage property (the real engine capture is pinned in
	// rules).
	combatHits []CombatDamageHit
	// lifeLost is the LifeLostThisTurn answer the double reports, keyed by
	// player; a nil map (the default) reports zero for every player, the
	// pre-existing no-op. The effects-level PlayerCountDefinedRegistered
	// tests set it to drive the HighestLifeLostThisTurn and
	// HasPropertyLostLifeThisTurn properties (the real log-scan read is
	// pinned in rules).
	lifeLost map[state.PlayerID]int32
	// commanderCasts is the CommanderCastsFromCommandZone answer the double
	// reports, keyed by player; a nil map (the default) reports zero. The
	// effects-level Count$TotalCommanderCastFromCommandZone test sets it
	// (the real log-walk read is pinned in rules).
	commanderCasts map[state.PlayerID]int32
	// dmgTaken is the DamageTakenThisTurn answer the double reports, keyed
	// by player; a nil map (the default) reports zero for every player. The
	// effects-level TargetedPlayer$DamageThisTurn tests set it; the real
	// event-log fold is pinned in rules.
	dmgTaken map[state.PlayerID]int32
	// discarded is the CardsDiscardedThisTurn answer the double reports;
	// a nil map keeps the pre-existing constant zero.
	discarded map[state.PlayerID]int32
	// drawn is the CardsDrawnThisTurn answer the double reports; a nil map
	// keeps the pre-existing constant zero. The effects-level
	// PlayerCount$Condition CardsDrawn tests set it.
	drawn map[state.PlayerID]int32
	// castsBy is the SpellsCastThisTurnBy answer the double reports; a nil
	// map keeps the pre-existing constant zero. The effects-level
	// PlayerCount$Condition SpellsCastThisTurn tests set it.
	castsBy map[state.PlayerID]int
	// suspendAfterAsk is the opt-in suspension the double reports after its
	// Ask was called (the real engine's Suspended() reads its pending resume
	// point; the double fakes the same shape for the Repeat loop's
	// between-iteration suspension break). Zero value keeps the historical
	// constant-false read every other effects test relies on.
	suspendAfterAsk bool
	// flipRests records every SuspendFlipRest call, so the effects-level
	// FlipUntilYouLose$ resume test can assert the loop cursor was reported
	// rather than the loop being abandoned.
	flipRests            []FlipRest
	repeatOptionalNext   int32
	repeatOptionalCalled bool
	// repeatSuspensions records every SuspendRepeat call, so the effects-level
	// RepeatEach RepeatOptionalForEachPlayer$ test can assert the per-subject
	// election parked the loop cursor with Election set and the offered index.
	repeatSuspensions []RepeatSuspension
	askCount          int
	askResult         bool
	// lastAsk is the most recent decision handed to Ask, so an effects-level
	// test can assert the election's ResumeRepeatNext (the two distinct
	// RepeatOptional resume states) without an engine.
	lastAsk *decision.Decision
	// startingLife is the StartingLife answer the double reports (0 when
	// unset); the effects-level relative half-starting-life tests set it.
	startingLife int32
}

func (h *fakeHost) Game() *state.Game                   { return h.g }
func (h *fakeHost) ObjectColors(o *state.Object) string { return ColorsOf(o) }

// ObjectText mirrors rules.Engine.ObjectText's derived-text read at the only
// fidelity the effects double has: it has no layer walk, so it returns the
// object's printed Oracle (an object with no face reads "").
func (h *fakeHost) ObjectText(o *state.Object) string {
	if o == nil || o.Face() == nil {
		return ""
	}
	return o.Face().Oracle
}

// ObjectKeywords mirrors rules.Engine.ObjectKeywords at the only fidelity the
// effects double has: it has no layer walk, so it returns a copy of the
// printed face's keyword list.
func (h *fakeHost) ObjectKeywords(o *state.Object) []string {
	if o == nil || o.Face() == nil {
		return nil
	}
	return append([]string(nil), o.Face().Keywords...)
}
func (h *fakeHost) Emit(e events.Event) {
	h.log = append(h.log, e)
	events.Apply(h.g, e)
}

// EmitTokenCreate mirrors rules.Engine's: the effects double has no token
// replacement pipeline, so the emit creates at most the one token the event
// names and that id is the whole return.
func (h *fakeHost) EmitTokenCreate(e events.Event) []state.ObjID {
	want := h.g.NextID
	h.Emit(e)
	if h.g.Obj(want) == nil {
		return nil
	}
	return []state.ObjID{want}
}

// EmitStackCopy mirrors rules.Engine's: the StackCopy fold mints exactly the
// one copy object the event names (there is no CopySpell replacement in this
// engine), and that id is the whole return. The empty return is the fold's
// early break (no source, source already left the stack).
func (h *fakeHost) EmitStackCopy(e events.Event) []state.ObjID {
	want := h.g.NextID
	h.Emit(e)
	if h.g.Obj(want) == nil {
		return nil
	}
	return []state.ObjID{want}
}

// EmitTap has no trigger matcher to hand the tapper and entry provenance to,
// so the double records the same plain Tap event the engine logs.
func (h *fakeHost) EmitTap(obj state.ObjID, _ state.PlayerID, _ bool) {
	h.Emit(events.Event{Kind: events.Tap, Obj: obj})
}
func (h *fakeHost) Rand(n int) int { h.n++; return 0 }
func (h *fakeHost) ShuffleLibrary(_ state.PlayerID, order []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), order...)
	for i := len(out) - 1; i > 0; i-- {
		j := h.Rand(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}
func (h *fakeHost) AddContinuous(ce state.ContinuousEffect) {
	h.continuous = append(h.continuous, ce)
}

// EndEffect mirrors rules.Engine's implementation: drop the registration
// with exactly this (source, timestamp) identity.
func (h *fakeHost) EndEffect(source state.ObjID, stamp uint32) {
	kept := h.continuous[:0]
	for _, ce := range h.continuous {
		if ce.Source == source && ce.Timestamp == stamp {
			continue
		}
		kept = append(kept, ce)
	}
	h.continuous = kept
}

// EndImprintedEffects mirrors rules.Engine's EndImprintedEffect: drop every
// registration the ImprintOnHost$ True Effect imprinted on the host.
func (h *fakeHost) EndImprintedEffects(source state.ObjID) {
	kept := h.continuous[:0]
	for _, ce := range h.continuous {
		if ce.Source == source && ce.ImprintOnHost {
			continue
		}
		kept = append(kept, ce)
	}
	h.continuous = kept
}

// EndEffectSource mirrors rules.Engine's EndEffectSource: drop every
// Effect-created registration from the source, leaving printed statics.
func (h *fakeHost) EndEffectSource(source state.ObjID) {
	kept := h.continuous[:0]
	for _, ce := range h.continuous {
		if ce.Source == source && ce.FromEffect {
			continue
		}
		kept = append(kept, ce)
	}
	h.continuous = kept
}
func (h *fakeHost) ContinuousNamed(controller state.PlayerID, name string) bool {
	for _, ce := range h.continuous {
		if ce.Controller == controller && ce.Name == name {
			return true
		}
	}
	return false
}
func (h *fakeHost) TriggerModeSupported(mode string) bool { return false }
func (h *fakeHost) RegisterControl(gr ControlGrant) {
	h.controls = append(h.controls, gr)
}
func (h *fakeHost) LegalTargets(chooser state.PlayerID, source state.ObjID, sa *cards.SA) []state.Target {
	var out []state.Target
	spec := sa.Params["ValidTgts"]
	if strings.Contains(spec, "Player") || strings.Contains(spec, "Opponent") || strings.Contains(spec, "Any") {
		for _, p := range h.g.AliveFrom(chooser) {
			if MatchesPlayerSpec(h.g, spec, p, chooser) || spec == "Any" {
				out = append(out, state.Target{Player: p, IsPlayer: true})
			}
		}
	}
	for i := range h.g.Objs {
		o := &h.g.Objs[i]
		if o.ID != source && o.Zone == state.ZBattlefield && MatchesObjectCtx(h.g, spec, o, SpecContext{You: chooser, Source: source}) {
			out = append(out, state.Target{Obj: o.ID})
		}
	}
	return out
}

// ChooserFor has no rules-tier resolver to consult in the test double, so it
// keeps the ability controller as the chooser -- the same fail-closed default
// as an unknown TargetingPlayer$ spec. Tests that need the opponent redirect
// drive the real rules.Engine Host instead.
func (h *fakeHost) ChooserFor(c *Ctx, sa *cards.SA) state.PlayerID {
	return c.Controller
}

// RegenerationDisallowed has no registry to consult here (the engine-side
// restriction lives in rules.Engine); the effects-package tests that exercise
// ReplaceDestruction set up their own boards and never rely on an
// Effect-registered CantRegenerate. Reporting false keeps the double honest
// rather than inventing a registry it cannot answer for.
func (h *fakeHost) RegenerationDisallowed(id state.ObjID) bool { return false }

// SacrificeBlocked has no registry to consult here (the engine-side
// restriction lives in rules.Engine), the same discipline as
// RegenerationDisallowed above: the double reports the per-object answers
// sacrificeBlocked configures (nil = false everywhere -- nothing blocked)
// rather than inventing a registry it cannot answer for.
func (h *fakeHost) SacrificeBlocked(id state.ObjID, forCost bool) bool {
	return h.sacrificeBlocked[id]
}

// ExileBlocked has no registry to consult here (the engine-side CantExile
// restriction lives in rules.Engine), the same discipline as SacrificeBlocked
// above: the double reports the per-object answers exileBlocked configures
// (nil = false everywhere -- nothing blocked).
func (h *fakeHost) ExileBlocked(id state.ObjID, forCost bool) bool {
	return h.exileBlocked[id]
}

// SurveilLookExtra has no static registry to consult here (the activeStatics
// walk lives in rules.Engine), the same discipline as SacrificeBlocked above:
// the double reports zero rather than inventing a registry it cannot answer
// for, so an effects-level surveil keeps its base count.
func (h *fakeHost) SurveilLookExtra(p state.PlayerID) (int32, []int32) { return 0, nil }

// ExploreReplaced has no replacement registry to consult here (the
// replacement matching lives in rules.Engine), the same discipline as
// SacrificeBlocked above: the double reports false rather than inventing a
// registry it cannot answer for.
func (h *fakeHost) ExploreReplaced(explorer state.ObjID) bool { return false }

// Scry has no replacement registry to consult here, the same discipline as
// ExploreReplaced above: the double reports the instruction unchanged so an
// effects-level scry test keeps the base count.
func (h *fakeHost) Scry(p state.PlayerID, source state.ObjID, count int32, sa *cards.SA, target int) (int32, bool, bool) {
	return count, true, false
}

// EmitScryRecord routes through the double's ordinary Emit: it has no
// replacement pipeline to bypass, the same discipline as its Scry above.
func (h *fakeHost) EmitScryRecord(e events.Event) { h.Emit(e) }

// RollDiceProposed has no replacement registry to consult here, the same
// discipline as the double's Scry above: the proposal returns unchanged.
func (h *fakeHost) RollDiceProposed(_ state.PlayerID, _ state.ObjID, amount, ignore int32) (int32, int32) {
	return amount, ignore
}

// CascadeReplacement has no replacement registry to consult here, the same
// discipline as the double's Scry above: it reports that nothing replaced the
// cascade instruction, so the caller runs its residue inline.
func (h *fakeHost) CascadeReplacement(_ state.ObjID, _ state.PlayerID, _ []state.ObjID, _ *cards.SA) bool {
	return false
}

// RememberExploitedLKI records the snapshot so an effects-level test can see
// what effExploit published (the real engine attaches it to the trig:Exploited
// pending trigger's LKI; that half is pinned in rules).
func (h *fakeHost) RememberExploitedLKI(s state.SacrificedInfo) {
	if h.exploitedLKI == nil {
		h.exploitedLKI = make(map[state.ObjID]state.SacrificedInfo)
	}
	h.exploitedLKI[s.Obj] = s
}

// The damage-batch bracket has nothing to latch here (no trigger machinery),
// so the double reports no-ops; the dealDamage loops' bracketing still runs.
func (h *fakeHost) BeginDamageBatch() {}
func (h *fakeHost) EndDamageBatch()   {}
func (h *fakeHost) BeginZoneBatch()   {}
func (h *fakeHost) EndZoneBatch()     {}

// CastThisTurn has no real turn log to count here (Task 17); the effects
// package tests set up their own boards, so the double reports zero.
func (h *fakeHost) CastThisTurn() int { return 0 }

// commanderCasts is the CommanderCastsFromCommandZone answer the double
// reports (per player); the Count$TotalCommanderCastFromCommandZone
// eval-level test flips it to pin the head through the Host seam (the real
// log-walk read is pinned in rules).
func (h *fakeHost) CommanderCastsFromCommandZone(p state.PlayerID) int32 {
	return h.commanderCasts[p]
}

// LifeLostThisTurn reports the h.lifeLost entry the effects-level
// PlayerCountDefinedRegistered tests configure; a nil map reports zero (the
// pre-existing conservative no-op, so every other test is unchanged).
func (h *fakeHost) LifeLostThisTurn(p state.PlayerID) int32         { return h.lifeLost[p] }
func (h *fakeHost) LifeLostLastTurn(p state.PlayerID) int32         { return 0 }
func (h *fakeHost) AttackedDuringLastTurn(q, d state.PlayerID) bool { return false }
func (h *fakeHost) AttackersDeclaredThisTurn() []state.ObjID        { return nil }

func (h *fakeHost) DamageTakenThisTurn(p state.PlayerID) int32 { return h.dmgTaken[p] }

// LifeGainedThisTurn has no event log here; the double reports zero (the
// same conservative no-op as LifeLostThisTurn).
func (h *fakeHost) LifeGainedThisTurn(_ state.PlayerID) int32 { return 0 }

// CountersRemovedThisTurn has no event log here; the double reports zero
// (the same conservative no-op as LifeLostThisTurn).
func (h *fakeHost) CountersRemovedThisTurn(_ state.PlayerID, _ string) int32                { return 0 }
func (h *fakeHost) CountersAddedThisTurn(_ string, _ string, _ string, _ SpecContext) int32 { return 0 }

// CombatDamageToPlayersThisTurn reports the h.combatHits slice the
// effects-level PlayerCountDefinedRegistered tests configure.
func (h *fakeHost) CombatDamageToPlayersThisTurn() []CombatDamageHit { return h.combatHits }

// CardsDiscardedThisTurn has no event log here; the double reports zero (the
// same conservative no-op as LifeLostThisTurn).
func (h *fakeHost) CardsDiscardedThisTurn(p state.PlayerID) int32 {
	if h.discarded == nil {
		return 0
	}
	return h.discarded[p]
}

// TurnsTaken has no event log here; the double reports zero.
func (h *fakeHost) TurnsTaken(_ state.PlayerID) int32 { return 0 }

// CardsDrawnThisTurn has no event log here; the double reports the
// h.drawn map the eval-level PlayerCount condition tests configure (nil reads
// zero, the same conservative no-op as CardsDiscardedThisTurn).
func (h *fakeHost) CardsDrawnThisTurn(p state.PlayerID) int32 {
	if h.drawn == nil {
		return 0
	}
	return h.drawn[p]
}

// SpellsCastThisTurnBy has no event log here; the double reports the
// h.castsBy map the eval-level PlayerCount condition tests configure (nil
// reads zero).
func (h *fakeHost) SpellsCastThisTurnBy(p state.PlayerID) int {
	if h.castsBy == nil {
		return 0
	}
	return h.castsBy[p]
}

// StartingLife reports the h.startingLife field the eval-level relative
// half-starting-life tests configure; zero when unset.
func (h *fakeHost) StartingLife() int32 { return h.startingLife }

// RevoltHolds has no event log here; the double reports the h.revolt flag
// the eval-level tests flip (the real log-scan read is pinned in rules).
func (h *fakeHost) RevoltHolds(_ state.PlayerID) bool { return h.revolt }

// DeliriumHolds has no graveyard census here; the double reports the
// h.delirium flag the eval-level tests flip (the real census read is pinned
// in rules).
func (h *fakeHost) DeliriumHolds(_ state.PlayerID) bool { return h.delirium }

// MetalcraftHolds reports the configured census answer for effects tests.
func (h *fakeHost) MetalcraftHolds(_ state.PlayerID) bool { return h.metalcraft }

// SpellsCastThisTurnMatching has no event log here; the double reports zero.
func (h *fakeHost) SpellsCastThisTurnMatching(_ state.PlayerID, _ string) int { return 0 }

// SpellsCastThisTurnMatchingExcluding has no event log here; the double
// reports zero (the same conservative no-op as SpellsCastThisTurnMatching).
func (h *fakeHost) SpellsCastThisTurnMatchingExcluding(_ state.PlayerID, _ string, _ state.ObjID) int {
	return 0
}

// EachSpellCastThisTurnMatching has no event log here; the double reports no
// ids (the same conservative no-op as SpellsCastThisTurnMatching), so the
// argumented !CastSaSource aggregate forms' fakeHost evals read an empty
// cast set (their end-to-end pins live in rules).
func (h *fakeHost) EachSpellCastThisTurnMatching(_ state.PlayerID, _ string, _ state.ObjID) []state.ObjID {
	return nil
}

// WasCastFromHandByYou has no cast log here; the double reports false (the
// same conservative no-op as CastThisTurn), so the Count$
// wasCastFromYourHandByYou branch head's fakeHost evals take the ifFalse
// branch; the true branch is pinned end to end on the real engine in rules
// (the Myojin cycle's corpus tests).
func (h *fakeHost) WasCastFromHandByYou(_ state.ObjID, _ state.PlayerID) bool { return h.castFromHand }

// DiscardedInWindow: the fake has no event log, so the ConditionDefined$
// Discarded group's cost-discard channel is always empty (the gate stays
// unresolved, its fail-open); the channel is pinned end to end on the real
// engine in rules (Moria Scavenger's corpus test).
func (h *fakeHost) DiscardedInWindow(_ state.ObjID) []state.ObjID { return nil }

// ReturnedInWindow: the fake has no event log either, so the
// ConditionDefined$ Returned group resolves to an empty list (a definite
// zero); the channel is pinned end to end on the real engine in rules
// (Wonderscape Sage's corpus test).
func (h *fakeHost) ReturnedInWindow(_ state.ObjID) []state.ObjID { return nil }

// The bare wasCastFromYourHand family's read (castprov3): the fake has no
// cast log either, so it reports the same single flag the ByYou double reads
// — enough for the branch-head and ConditionPresent$ gate unit tests, whose
// provenance is pinned end to end on the real engine in rules.
func (h *fakeHost) WasCastFromHand(_ state.ObjID) bool { return h.castFromHand }

// WasCastFromExile (task wascastfrom): the fake has no cast log, so the
// Count$wasCastFromExile branch head's fakeHost evals take the ifFalse
// branch; the provenance is pinned end to end on the real engine in rules
// (Delayed Blast Fireball's foretell-cast corpus test).
func (h *fakeHost) WasCastFromExile(_ state.ObjID) bool { return false }

// WasCast is the Count$IfCastInOwnMainPhase third conjunct's read (task
// ifcastmain1): the fake reports the flag, so the eval-level head tests pin
// both branches by flipping it (the real engine read is pinned in rules).
func (h *fakeHost) WasCast(_ state.ObjID) bool { return h.wasCast }

// CommanderIdentityColourCount has no commander bookkeeping here; the double
// reports zero (the same replay-derivable class as TurnsTaken above).
func (h *fakeHost) CommanderIdentityColourCount(_ state.PlayerID) int { return 0 }

// AttackersThisTurn has no combat log here; the double reports zero (the same
// conservative no-op as CastThisTurn).
func (h *fakeHost) AttackersThisTurn() int { return 0 }

// HasKeyword has no layer system to consult here (see the type doc comment),
// so it reads the printed face directly -- enough for the effects-package
// tests, which set up Indestructible by mutating Card.Faces[0].Keywords.
func (h *fakeHost) HasKeyword(id state.ObjID, kw string) bool {
	o := h.g.Obj(id)
	return o != nil && o.Face() != nil && o.Face().HasKeyword(kw)
}

// UmbraArmorAura has no layer system to consult here either (Umbra Mystic's
// grant is a rules-side derived keyword); the double reports none, so the
// effects-package tests that drive ReplaceUmbraArmor directly must seed a
// printed keyword on the Aura's face.
func (h *fakeHost) UmbraArmorAura(_ state.ObjID) state.ObjID { return 0 }
func (h *fakeHost) Power(id state.ObjID) int32 {
	o := h.g.Obj(id)
	if o == nil || o.Face() == nil {
		return 0
	}
	return int32(o.Face().Power()) + o.Counter("P1P1") - o.Counter("M1M1")
}
func (h *fakeHost) BasePower(id state.ObjID) int32 {
	o := h.g.Obj(id)
	if o == nil || o.Face() == nil {
		return 0
	}
	return int32(o.Face().Power())
}
func (h *fakeHost) Toughness(id state.ObjID) int32 {
	o := h.g.Obj(id)
	if o == nil || o.Face() == nil {
		return 0
	}
	return int32(o.Face().Toughness()) + o.Counter("P1P1") - o.Counter("M1M1")
}
func (h *fakeHost) IsCreature(id state.ObjID) bool {
	o := h.g.Obj(id)
	return o != nil && o.Face() != nil && o.Face().IsCreature()
}

// Ask reports false: an effects-package test double has no engine to drive,
// so a mid-resolution ask falls back to the primitive's deterministic
// stand-in (effCharm's first mode, effCopySpellAbility's may-copy election --
// which MAKES the copy, the documented pre-ask behaviour) -- which is exactly
// today's no-ask behaviour, now with the engines it is a fallback for clearly
// named (R-9).
func (h *fakeHost) Ask(d *decision.Decision) bool { h.askCount++; h.lastAsk = d; return h.askResult }

// TypeChoices serves the double's configured typeChoices list (nil by
// default): nil routes ChooseType through AskEmpty — the unchanged
// deterministic fallback — so the existing fallback pins pass untouched.
func (h *fakeHost) TypeChoices(_ state.PlayerID, _ string) []decision.Option {
	return h.typeChoices
}

// Suspended reports false: an effects-package test double never actually
// suspends a resolution (its Ask always returns false, so the asking effect
// falls back to its deterministic stand-in and the chain — if it had a
// SubAbility — finishes in one pass). This keeps effects.Resolve's
// suspended-check from breaking the chain on a host that never asked; the
// real suspension behaviour is exercised through the rules engine, where
// Engine.Suspended reports e.resume != nil.
func (h *fakeHost) Suspended() bool { return h.suspendAfterAsk }

// SuspendContinuation is a no-op: an effects-package test double never
// suspends (its Ask returns false), so effects.Resolve never reaches the
// suspended branch that would call it. Kept to satisfy the Host interface.
func (h *fakeHost) SuspendContinuation(*cards.SA) {}

// SuspendUnless is a no-op for the same reason as SuspendContinuation.
func (h *fakeHost) SuspendUnless(*cards.SA, bool) {}

// SetResolutionTargetControllerLKI keeps the no-op shape: this double never
// suspends, so the map it publishes is never consumed. Returns nil as the
// "previous" value, which the caller restores on return.
func (h *fakeHost) SetResolutionTargetControllerLKI(map[state.ObjID]state.PlayerID) map[state.ObjID]state.PlayerID {
	return nil
}

func (h *fakeHost) ReplaceEvent(string, string, int32) {}

func (h *fakeHost) EmitDamage(e events.Event) events.Event {
	h.Emit(e)
	return e
}
func (h *fakeHost) CounterAllowed(state.ObjID, state.ObjID) bool { return true }

// SuspendRepeatOptional is a no-op for the same reason as SuspendContinuation.
func (h *fakeHost) SuspendRepeatOptional(_ *cards.SA, next int32) {
	h.repeatOptionalCalled = true
	h.repeatOptionalNext = next
}

// SuspendRepeat records the loop cursor the RepeatEach reported (the
// effects-level ReadRepeatEach tests assert the election's own cursor) and is
// otherwise inert: an effects-package test double never really resumes.
func (h *fakeHost) SuspendRepeat(s RepeatSuspension) {
	h.repeatSuspensions = append(h.repeatSuspensions, s)
}

// SuspendCharmRest is a no-op for the same reason as SuspendContinuation.
func (h *fakeHost) SuspendCharmRest(*cards.SA, []string) {}

// SuspendVillainousRest is a no-op for the same reason as
// SuspendContinuation.
func (h *fakeHost) SuspendVillainousRest(*cards.SA, VillainousRest) {}

// SuspendGenericChoiceRest is a no-op for the same reason as
// SuspendContinuation.
func (h *fakeHost) SuspendGenericChoiceRest(*cards.SA, GenericChoiceRest) {}
func (h *fakeHost) SuspendFlipRest(_ *cards.SA, rest FlipRest) {
	h.flipRests = append(h.flipRests, rest)
}

// SetDamageSource records the published damage source on the double (the
// last value wins) and returns the previous one, mirroring the engine's
// set-and-restore contract so an emitter's restore is observable.
func (h *fakeHost) SetDamageSource(id state.ObjID) state.ObjID {
	prev := h.dmgSrc
	h.dmgSrc = id
	return prev
}

// SetCounterAdder is the AddCounter counterpart of SetDamageSource; the
// effects package never publishes one (rules owns that), so the double only
// needs the round-trip contract, including the unset sentinel.
func (h *fakeHost) SetCounterAdder(p state.PlayerID) state.PlayerID {
	prev := counterAdderUnsetFake
	if h.counterAdder != 0 {
		prev = h.counterAdder - 1
	}
	if p == counterAdderUnsetFake {
		h.counterAdder = 0
	} else {
		h.counterAdder = p + 1
	}
	return prev
}

// BatchDepartures is a no-op snapshot: an effects-package double has no
// engine-side departure capture to feed, so it keeps only the fact a batch
// was declared (never asserted on today; the rules package owns the
// behaviour this method exists for).
func (h *fakeHost) BatchDepartures(ids []state.ObjID) { h.batch = ids }
func (h *fakeHost) EndBatchDepartures()               { h.batch = nil }

func newHost(t testing.TB, seats int) *fakeHost {
	t.Helper()
	return &fakeHost{g: state.NewGame(names(seats))}
}

// fixtureHost builds a 2-seat game with two objects already on it -- object 1
// (ID from state.Game.AddObject's first call) controlled by seat 0, object 2
// controlled by seat 1 -- and a Ctx sourced at object 1. context_test.go's
// and count_test.go's own tests share this instead of each hand-rolling a
// board, the same way filter_test.go's board(t) is shared across that file.
func fixtureHost(t *testing.T) (*fakeHost, *Ctx) {
	t.Helper()
	h := newHost(t, 2)
	card := mkCard(t, "Name:Fixture\nTypes:Creature\nPT:1/1\nOracle:x\n")
	src := h.g.AddObject(card, 0)
	h.g.AddObject(card, 1)
	return h, &Ctx{Source: src.ID, Controller: 0}
}

func names(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i))
	}
	return out
}

func sa(t testing.TB, line string) *cards.SA {
	t.Helper()
	src := "Name:T\nTypes:Sorcery\nA:" + line + "\nOracle:x\n"
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	return c.Faces[0].Abilities[0]
}

func TestResolveWalksTheSubAbilityChain(t *testing.T) {
	var order []string
	Register("TestA", func(h Host, c *Ctx, s *cards.SA) { order = append(order, "A") })
	Register("TestB", func(h Host, c *Ctx, s *cards.SA) { order = append(order, "B") })
	Register("TestC", func(h Host, c *Ctx, s *cards.SA) { order = append(order, "C") })
	t.Cleanup(func() { unregister("TestA", "TestB", "TestC") })

	src := "Name:T\nTypes:Sorcery\nA:SP$ TestA | SubAbility$ X\nSVar:X:DB$ TestB | SubAbility$ Y\nSVar:Y:DB$ TestC\nOracle:x\n"
	c, _ := cards.ParseBytes("t.txt", []byte(src))
	c.Link()
	Resolve(newHost(t, 2), &Ctx{}, c.Faces[0].Abilities[0])

	if len(order) != 3 || order[0] != "A" || order[1] != "B" || order[2] != "C" {
		t.Fatalf("resolution order = %v", order)
	}
}

func TestResolveNotesUnimplementedAPIsInsteadOfPanicking(t *testing.T) {
	h := newHost(t, 2)
	Resolve(h, &Ctx{Source: 0}, sa(t, "SP$ NoSuchApiExists | NumDmg$ 1"))
	if len(h.log) != 1 || h.log[0].Kind != events.Note {
		t.Fatalf("expected one Note event, got %+v", h.log)
	}
	if want := "unimplemented API NoSuchApiExists"; h.log[0].Text != want {
		t.Fatalf("Text = %q, want %q", h.log[0].Text, want)
	}
}

func TestDefinedResolvesEachForm(t *testing.T) {
	h := newHost(t, 4)
	ctx := &Ctx{Source: 7, Controller: 1,
		Targets:    []state.Target{{Player: 3, IsPlayer: true}},
		Remembered: []state.Target{{Obj: 9}}}

	check := func(line string, want []state.Target) {
		t.Helper()
		got := Defined(h, ctx, sa(t, line))
		if len(got) != len(want) {
			t.Fatalf("%s -> %v, want %v", line, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s -> %v, want %v", line, got, want)
			}
		}
	}
	check("SP$ X | Defined$ You", []state.Target{{Player: 1, IsPlayer: true}})
	check("SP$ X | Defined$ Self", []state.Target{{Obj: 7}})
	check("SP$ X | Defined$ Remembered", []state.Target{{Obj: 9}})
	check("SP$ X | Defined$ Targeted", []state.Target{{Player: 3, IsPlayer: true}})
	// No Defined$ and no ValidTgts$ falls back to the ability's own source
	// (object 7), not the chosen targets -- Forge's default (R-10).
	check("SP$ X | NumDmg$ 1", []state.Target{{Obj: 7}})
	// No Defined$ but ValidTgts$ present falls back to the chosen targets.
	check("SP$ X | ValidTgts$ Player | NumDmg$ 1", []state.Target{{Player: 3, IsPlayer: true}})
	// Opponent expands to every other living seat, in APNAP order from the
	// controller, so the resulting event order is deterministic.
	check("SP$ X | Defined$ Opponent", []state.Target{
		{Player: 2, IsPlayer: true}, {Player: 3, IsPlayer: true}, {Player: 0, IsPlayer: true}})
	// Player expands to every living seat including the controller, in APNAP
	// order from the controller.
	check("SP$ X | Defined$ Player", []state.Target{
		{Player: 1, IsPlayer: true}, {Player: 2, IsPlayer: true},
		{Player: 3, IsPlayer: true}, {Player: 0, IsPlayer: true}})
}

// TestDefinedReturnsCopiesNotAliases guards against Defined() handing back a
// slice that shares a backing array with Ctx.Targets or Ctx.Remembered.
// Ctx is threaded by pointer through Resolve, so if this aliased, the ordinary
// Go filter-in-place idiom applied to a Defined() result would corrupt state
// a later effect in the same Sub chain still relies on — silently, with no
// compiler warning. Covers every path in Defined that can return c.Targets or
// c.Remembered: the no-Defined$-but-ValidTgts$ case, Targeted, ParentTarget,
// Remembered, and the unknown-form fallback.
func TestDefinedReturnsCopiesNotAliases(t *testing.T) {
	h := newHost(t, 2)
	origTarget := state.Target{Player: 0, IsPlayer: true}
	origRemembered := state.Target{Obj: 5}

	lines := []string{
		"SP$ X | ValidTgts$ Creature | NumDmg$ 1", // no Defined$, has ValidTgts$: falls back to Targets
		"SP$ X | Defined$ Targeted",
		"SP$ X | Defined$ ParentTarget",
		"SP$ X | Defined$ SomeFormM1DoesNotModel", // unknown-form fallback
	}
	for _, line := range lines {
		ctx := &Ctx{Targets: []state.Target{origTarget}}
		got := Defined(h, ctx, sa(t, line))
		if len(got) != 1 {
			t.Fatalf("%s: got %v, want 1 target", line, got)
		}
		got[0] = state.Target{Player: 99, IsPlayer: true}
		if ctx.Targets[0] != origTarget {
			t.Fatalf("%s: mutating the Defined() result changed Ctx.Targets to %v", line, ctx.Targets)
		}
	}

	ctx := &Ctx{Remembered: []state.Target{origRemembered}}
	got := Defined(h, ctx, sa(t, "SP$ X | Defined$ Remembered"))
	if len(got) != 1 {
		t.Fatalf("Defined$ Remembered = %v, want 1 target", got)
	}
	got[0] = state.Target{Obj: 999}
	if ctx.Remembered[0] != origRemembered {
		t.Fatalf("mutating the Defined() result changed Ctx.Remembered to %v", ctx.Remembered)
	}

	// A nil Targets must still come back nil, not a spurious allocation, on
	// the no-Defined$-but-ValidTgts$ path that defers to copyTargets.
	if got := Defined(h, &Ctx{}, sa(t, "SP$ X | ValidTgts$ Creature | NumDmg$ 1")); got != nil {
		t.Fatalf("Defined() on nil Targets = %#v, want nil", got)
	}
}

func TestDefinedOpponentSkipsEliminatedSeats(t *testing.T) {
	h := newHost(t, 4)
	h.g.Players[2].Lost = true
	got := Defined(h, &Ctx{Controller: 1}, sa(t, "SP$ X | Defined$ Opponent"))
	if len(got) != 2 || got[0].Player != 3 || got[1].Player != 0 {
		t.Fatalf("Defined$ Opponent = %v", got)
	}
}

func TestDefinedDefaultsToSelfWithoutTargets(t *testing.T) {
	h, c := fixtureHost(t) // seat 0 controls object 1
	got := Defined(h, c, &cards.SA{Params: map[string]string{}})
	if len(got) != 1 || got[0].Obj != c.Source {
		t.Fatalf("no ValidTgts, no Defined: %v, want Self", got)
	}
	c.Targets = []state.Target{{Player: 1, IsPlayer: true}}
	got = Defined(h, c, &cards.SA{Params: map[string]string{"ValidTgts": "Player"}})
	if len(got) != 1 || !got[0].IsPlayer {
		t.Fatalf("ValidTgts present: %v, want the chosen targets", got)
	}
	got = Defined(h, c, &cards.SA{Params: map[string]string{}})
	if len(got) != 1 || got[0].Obj != c.Source {
		t.Fatalf("a sub-ability without ValidTgts acts on Self even when the root had targets: %v", got)
	}
}

func TestDefinedTriggeredForms(t *testing.T) {
	h, c := fixtureHost(t)
	c.Remembered = []state.Target{{Obj: 2}, {Player: 1, IsPlayer: true}}
	for _, form := range []string{"TriggeredCard", "TriggeredCardLKICopy", "TriggeredNewCardLKICopy", "TriggeredSpellAbility", "TriggeredAttacker"} {
		got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": form}})
		if len(got) != 1 || got[0].Obj != 2 {
			t.Errorf("%s: %v", form, got)
		}
	}
	for _, form := range []string{"TriggeredDefendingPlayer", "TriggeredPlayer"} {
		got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": form}})
		if len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
			t.Errorf("%s: %v", form, got)
		}
	}
	got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "TriggeredCardController"}})
	if len(got) != 1 || !got[0].IsPlayer || got[0].Player != h.Game().Obj(2).Controller {
		t.Errorf("TriggeredCardController: %v", got)
	}
	if got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "Parent"}}); len(got) != 1 || got[0].Obj != c.Source {
		t.Errorf("Parent: %v", got)
	}
	if got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "Equipped"}}); len(got) != 0 {
		t.Errorf("Equipped with nothing attached: %v", got)
	}
	h.Game().Obj(c.Source).AttachedTo = 2
	if got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "Equipped"}}); len(got) != 1 || got[0].Obj != 2 {
		t.Errorf("Equipped: %v", got)
	}
}

// TestDefinedTriggeredCardOwners pins the TriggeredCardOwner /
// NonTriggeredCardOwner selectors on a three-seat game whose trigger card is
// OWNED by seat 2 while CONTROLLED by seat 1 -- so ownership and last-known
// control can never be confused -- with the resolving source owned and
// controlled by seat 0. The trigger card's owner is distinct from both the
// source's owner and its controller, which is the precondition the ownership
// assertions depend on.
func TestDefinedTriggeredCardOwners(t *testing.T) {
	h := newHost(t, 3)
	card := mkCard(t, "Name:Fixture\nTypes:Creature\nPT:1/1\nOracle:x\n")
	src := h.g.AddObject(card, 0)  // the resolving ability's source, seat 0
	trig := h.g.AddObject(card, 2) // the triggering card: owner seat 2
	trig.Controller = 1            // ... but last controlled by seat 1
	c := &Ctx{Source: src.ID, Controller: 0, TriggerContext: TriggerContext{TriggerCard: trig.ID}}

	// Precondition: the fixture really does distinguish owner from controller
	// and from the resolving source's owner, or the assertions below prove
	// nothing.
	if trig.Owner != 2 || trig.Controller != 1 || trig.Owner == src.Owner {
		t.Fatalf("fixture precondition: trigger owner=%d controller=%d source owner=%d, want owner 2 != controller 1 != source owner 0",
			trig.Owner, trig.Controller, src.Owner)
	}

	got := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "TriggeredCardOwner"}})
	if len(got) != 1 || !got[0].IsPlayer || got[0].Player != 2 {
		t.Fatalf("TriggeredCardOwner = %v, want owner seat 2", got)
	}

	got = Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "NonTriggeredCardOwner"}})
	if len(got) != 2 || !got[0].IsPlayer || got[0].Player != 0 || !got[1].IsPlayer || got[1].Player != 1 {
		t.Fatalf("NonTriggeredCardOwner = %v, want every other living seat [0 1] in AliveFrom order", got)
	}

	// A departed non-owner drops out of the "other players" set while the
	// set stays deterministic: seat 0 loses, so only seat 1 remains.
	h.g.Players[0].Lost = true
	got = Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "NonTriggeredCardOwner"}})
	if len(got) != 1 || !got[0].IsPlayer || got[0].Player != 1 {
		t.Fatalf("NonTriggeredCardOwner with seat 0 dead = %v, want [1]", got)
	}
	h.g.Players[0].Lost = false

	// No triggering card: both forms fail CLOSED to the empty set, never to
	// the ability's source or to every player. A remembered object must not
	// substitute for the absent TriggerCard role.
	empty := &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: trig.ID}}}
	for _, form := range []string{"TriggeredCardOwner", "NonTriggeredCardOwner"} {
		if got := Defined(h, empty, &cards.SA{Params: map[string]string{"Defined": form}}); len(got) != 0 {
			t.Errorf("%s with no triggering card = %v, want the empty set", form, got)
		}
	}
}

func TestSupportedListsRegisteredAPIs(t *testing.T) {
	Register("TestZ", func(Host, *Ctx, *cards.SA) {})
	t.Cleanup(func() { unregister("TestZ") })
	if !Supported()["api:TestZ"] {
		t.Fatal("Supported did not list a registered API")
	}
	if Supported()["api:NotRegistered"] {
		t.Fatal("Supported listed an API that was never registered")
	}
}

func TestCompiledAPIRegistryDispatchParity(t *testing.T) {
	defer Register("Draw", effDraw)
	defer unregister("TestCompiledUnknown")

	bound := &cards.SA{Kind: "SP", API: "Draw"}
	r := cards.NewRegistry()
	r.Add(&cards.Card{Faces: []*cards.Face{{Abilities: []*cards.SA{bound}}}})
	if err := r.CompileMetadata(); err != nil {
		t.Fatal(err)
	}
	if bound.CompiledAPI() != cards.APIDraw {
		t.Fatalf("compiled API = %d, want Draw", bound.CompiledAPI())
	}

	var first, second, unknown int
	Register("Draw", func(Host, *Ctx, *cards.SA) { first++ })
	snapshot := registry.load()
	if snapshot.byName["Draw"] == nil || snapshot.byCode[cards.APIDraw] == nil {
		t.Fatal("Draw registration was not published in both lookup views")
	}
	Resolve(newHost(t, 2), &Ctx{}, bound)
	Resolve(newHost(t, 2), &Ctx{}, &cards.SA{Kind: "SP", API: "Draw"})
	if first != 2 {
		t.Fatalf("first Draw implementation ran %d times, want bound and textual dispatch", first)
	}

	Register("Draw", func(Host, *Ctx, *cards.SA) { second++ })
	Resolve(newHost(t, 2), &Ctx{}, bound)
	Resolve(newHost(t, 2), &Ctx{}, &cards.SA{Kind: "SP", API: "Draw"})
	if second != 2 || first != 2 {
		t.Fatalf("replacement dispatch: first=%d second=%d, want 2/2", first, second)
	}

	Register("TestCompiledUnknown", func(Host, *Ctx, *cards.SA) { unknown++ })
	Resolve(newHost(t, 2), &Ctx{}, &cards.SA{Kind: "SP", API: "TestCompiledUnknown"})
	if unknown != 1 {
		t.Fatalf("registered unknown API ran %d times, want 1", unknown)
	}

	unregister("Draw", "TestCompiledUnknown")
	snapshot = registry.load()
	if snapshot.byName["Draw"] != nil || snapshot.byCode[cards.APIDraw] != nil {
		t.Fatal("Draw unregistration was not published in both lookup views")
	}
	h := newHost(t, 2)
	Resolve(h, &Ctx{}, bound)
	if len(h.log) != 1 || h.log[0].Text != "unimplemented API Draw" {
		t.Fatalf("unregistered compiled API log = %+v", h.log)
	}
}

// TestRegistryConcurrentRegisterAndReadDoesNotRace guards the process-global
// registry against the case Register's own doc comment advertises as
// supported: registering (or re-registering, e.g. M3's plugin tier
// overriding a native primitive) after init time, concurrently with the
// engine's per-match goroutines calling Supported()/Resolve(). Run with
// `go test -race`, this failed against the plain, unsynchronized map that
// registry.go used before the copy-on-write atomicMap fix — see the task-15
// fix report for the before/after -race output.
func TestRegistryConcurrentRegisterAndReadDoesNotRace(t *testing.T) {
	Register("TestRace", func(Host, *Ctx, *cards.SA) {})
	t.Cleanup(func() { unregister("TestRace") })
	line := sa(t, "SP$ TestRace | NumDmg$ 1")

	const iterations = 500
	var wg sync.WaitGroup

	// One writer keeps re-registering the same API...
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			Register("TestRace", func(Host, *Ctx, *cards.SA) {})
		}
	}()

	// ...while several readers hammer the two read paths. Each reader gets
	// its own fakeHost so any data race the detector reports is in the
	// registry under test, not in this test's own event-log slice.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := newHost(t, 2)
			for i := 0; i < iterations; i++ {
				_ = Supported()
				Resolve(h, &Ctx{}, line)
			}
		}()
	}
	wg.Wait()
}

// TestKnownDefinedTargetsRecognisesBareValidBattlefieldForm pins the resolver
// contract behind the copyp2 fix: the bare "Defined$ Valid <filter>"
// battlefield form must be classified as KNOWN by knownDefinedTargets, not as
// an unresolvable fetch list. Before the fix definedSpec recognised only the
// zone-suffixed forms (ValidGraveyard/ValidHand/...) and ValidStack, so every
// fail-closed caller (effCopyPermanent, effChangeZone's hidden search,
// effCopySpellAbility, the DamageSource$/ValidPlayers$ resolvers) treated a
// bare Valid spec as "unknown" and acted on nothing -- Redoubled Stormsinger
// (Defined$ Valid Creature.token+YouCtrl+ThisTurnEntered) minted no copy.
//
// The sweep itself is shared with Defined's own bare-Valid branch
// (battlefieldValidTargets), so this also guards the two against drifting.
func TestKnownDefinedTargetsRecognisesBareValidBattlefieldForm(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	c := &Ctx{Source: ids["myBear"], Controller: 0}

	got, ok := knownDefinedTargets(h, c, "Valid Creature")
	if !ok {
		t.Fatal("bare Defined$ Valid form was not classified as known")
	}
	// board(t) seats two creatures for seat 0 (myBear, myFlier) and one
	// for seat 1 (theirBig): the sweep is the whole battlefield.
	if len(got) != 3 {
		t.Fatalf("Valid Creature -> %v, want all three battlefield creatures", got)
	}

	// A controller qualifier narrows to the resolving controller's seat, and
	// the fail-closed caller shares Defined's exact reading of it.
	got, ok = knownDefinedTargets(h, c, "Valid Creature.YouCtrl")
	if !ok || len(got) != 2 {
		t.Fatalf("Valid Creature.YouCtrl -> %v (ok=%v), want seat 0's two creatures", got, ok)
	}

	// An unmodelled predicate fails CLOSED inside the filter (empty set) but
	// is still a KNOWN selector -- never ok=false, which would make a
	// fail-closed caller fall through to a wrong source instead.
	got, ok = knownDefinedTargets(h, c, "Valid Creature.NotAPredicateXYZ")
	if !ok {
		t.Fatal("unmodelled predicate made the selector unknown; want ok=true, empty set")
	}
	if len(got) != 0 {
		t.Fatalf("unmodelled predicate -> %v, want empty", got)
	}

	// The zone-suffixed and ValidStack prefixes must still win over the bare
	// form (ValidGraveyard is not a prefix of "Valid " and vice versa).
	if _, ok := knownDefinedTargets(h, c, "ValidStack Spell"); !ok {
		t.Fatal("ValidStack prefix no longer recognised")
	}
	if got, ok := knownDefinedTargets(h, c, "ValidGraveyard Creature"); !ok || len(got) != 0 {
		t.Fatalf("ValidGraveyard Creature -> %v (ok=%v), want ok=true over the empty graveyard", got, ok)
	}

	// Forge's " & " joins independent selectors as a UNION, so a compound
	// "Valid Creature & Player" must be split -- NOT captured whole by the
	// bare-Valid branch, which would hand battlefieldValidTargets the mangled
	// filter "Creature & Player" and match nothing. definedSpec runs BEFORE
	// knownDefinedTargets' own " & " split, so the guard lives in definedSpec.
	// kitsune_palliator.txt:5 is the corpus carrier
	// (AB$ PreventDamage | Defined$ Valid Creature & Player).
	got, ok = knownDefinedTargets(h, c, "Valid Creature & Player")
	if !ok {
		t.Fatal("compound Defined$ Valid & selector was not classified as known")
	}
	// board(t) seats three battlefield creatures and two players: the union
	// is 3 objects + 2 players = 5, the pre-regression reading.
	if len(got) != 5 {
		t.Fatalf("Valid Creature & Player -> %v, want the 3 creatures + 2 players union", got)
	}
	var objs, players int
	for _, tt := range got {
		if tt.IsPlayer {
			players++
		} else {
			objs++
		}
	}
	if objs != 3 || players != 2 {
		t.Fatalf("compound split -> %d objects / %d players, want 3/2", objs, players)
	}
}
