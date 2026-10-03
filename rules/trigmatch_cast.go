// Cast, copy and ability-cast trigger modes.
//
// Mode$ SpellCast, SpellCastOrCopy, SpellCopy, AbilityCast and SpellAbilityCast,
// with the cast evaluation and the mana/alternative-cost gates they share.
//
// Split out of trigger_match.go so tickets touching different modes stop
// colliding on one file. Registration is at the bottom; a duplicate mode
// panics (registerTrigMatcher).

package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// spellCastMatches implements Mode$ SpellCast: ValidCard$ and
// ValidActivatingPlayer$ against a PutOnStack event, plus the two cast-
// condition clauses the measured cards carry -- ActivatorThisTurnCast$
// (The Lord of Pain, Vial Smasher the Fierce: <OP><N> over the spells the
// ACTIVATOR has cast this turn, the current cast included -- its PutOnStack
// is already in the log when the deferred trigger fires) and ValidSA$
// (Roiling Vortex: the Spell.ManaSpent <OP><value> comparison family).
// ValidSAonCard$ carries the SAME mana comparison over the cast spell
// (Ancient Cellarspawn's Spell.ManaSpent LTX); see
// spellValidSAonCardMatches.
func (e *Engine) spellCastMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.PutOnStack {
		return false
	}
	return e.spellCastEval(t, source, ev)
}

// spellCopyMatches is the copy half of the spell-cast family: Mode$
// SpellCastOrCopy delegates a StackCopy event here, and Mode$ SpellCopy
// ("Whenever you copy a spell, ...", the_parnesse_the_subtle_brush shape)
// is its only mode. Copies do not re-enter the stack as a PutOnStack --
// effects/copy.go emits events.StackCopy naming the ORIGINAL spell (ev.Obj,
// still on the stack; events.Apply's StackCopy case rejects anything else)
// with ev.Player the copy's controller -- and the copy object shares the
// original's card, face and CastFlags, so the shared evaluation below reads
// the copied spell identically. A plain SpellCast trigger stays silent here
// (a copy is not a cast) and a SpellCastOrCopy/SpellCopy trigger does not
// fire on the cast of the spell itself -- that half of the division is
// spellCastMatches'.
func (e *Engine) spellCopyMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.StackCopy {
		return false
	}
	return e.spellCastEval(t, source, ev)
}

// spellCastEval is the spell evaluation spellCastMatches and
// spellCopyMatches share, minus the entering-the-stack guard each mode owns:
// ValidCard$ (through the trigger-side cast alternatives and the
// cast-provenance qualifiers), ValidActivatingPlayer$, the
// ActivatorThisTurnCast[Each] counts, ValidSA$ and HasXManaCost$ -- read off
// ev.Obj (the spell being cast, or the original being copied) and ev.Player
// (the cast's or the copy's controller) exactly alike.
func (e *Engine) spellCastEval(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	// Casting a spell means an actual card entering the stack (a copy
	// evaluates the ORIGINAL, which Apply's StackCopy case guarantees is on
	// the stack). This build also uses PutOnStack-shaped Move()s for nothing else today (triggered
	// abilities go on the stack via a dedicated TriggerPush event --
	// putTriggersOnStack, above, and events.Apply's TriggerPush case), but a
	// Face()-less object could otherwise satisfy a bare "Any"/"Spell"
	// ValidCard$ regardless (matchesBase's Spell/Any cases don't consult
	// Face()), so this guard holds regardless of how a future ability-object
	// path might reach here. Ruling F3.
	obj := e.G.Obj(ev.Obj)
	if obj == nil || obj.Face() == nil {
		return false
	}
	ctrl := e.controllerOf(source)
	// The ValidCard alternatives (post trigger-side self-cast exclusion) are
	// computed ONCE: the cast's own match below and the
	// ActivatorThisTurnCastEach$ tally must read the same surviving set, so
	// the two can never disagree (the Each arm below reads only the alts this
	// block computed).
	var castAlts []triggerCastAlt
	if v, ok := t.Params["ValidCard"]; ok {
		alts, ok2 := e.triggerCastAlternatives(v, source, ev.Obj)
		if !ok2 {
			return false
		}
		spec := ""
		for i, alt := range alts {
			if i > 0 {
				spec += ","
			}
			spec += alt.spec
		}
		// The bare wasCastFromYourHandByYou / wasCastByYou qualifiers (the
		// cast-provenance families, tasks castprov1/castprov2) are split out
		// and evaluated against the log here; the remainder matches as before.
		spec, ok3 := e.castProvenanceAdmits(spec, ev.Obj, ctrl)
		if !ok3 || !e.matchesSpec(spec, ev.Obj, e.specCtx(source, ctrl)) {
			return false
		}
		castAlts = alts
	}
	if v, ok := t.Params["ValidActivatingPlayer"]; ok {
		if !effects.MatchesPlayerSpecCtx(e.G, v, ev.Player, ctrl, e.playerSpecCtx(source)) {
			return false
		}
	}
	if v, ok := t.Params["ActivatorThisTurnCast"]; ok {
		if !compareIntCount(int32(e.spellsCastThisTurn(ev.Player)), v) {
			return false
		}
	}
	if v, ok := t.Params["ActivatorThisTurnCastEach"]; ok {
		// The PER-ALTERNATIVE first-cast read (task castprov2, Alania,
		// Divergent Storm — the corpus's one carrier): the trigger fires when
		// the activator's cast is the FIRST this turn of at least ONE
		// ValidCard$ alternative — a disjunction of firsts, so casting an
		// instant and then a sorcery fires on the sorcery. The tally is the
		// activator's casts this turn matching that alternative, the current
		// cast INCLUDED (it is already in the log when the deferred trigger
		// fires; EQ1 means this cast is the first).
		//
		// Only alternatives the CURRENT CAST MATCHES are evaluated (round-2
		// review): the oracle reads "if it's the first instant ... you've cast
		// this turn" — the firstness must attach to the spell being cast, so
		// a second instant after instant→sorcery does NOT fire on the
		// sorcery alternative's earlier tally (that sorcery was the first
		// sorcery, but this cast is not one). An alternative whose tally is
		// met while the current cast matches a DIFFERENT alternative is
		// skipped; without the guard the trigger false-fires on any
		// two-cast-then-repeat turn. castAlts comes from the same surviving
		// set the cast's own match above used, so the two cannot disagree.
		//
		// A trigger carrying the Each param with NO ValidCard$ has no
		// alternatives to attach the firstness to: UNSUPPORTED, fail closed
		// (castAlts stays nil; the corpus's one carrier, Alania, always has
		// a ValidCard$).
		fired := false
		for _, alt := range castAlts {
			if !e.matchesSpec(alt.spec, ev.Obj, e.specCtx(source, ctrl)) {
				continue
			}
			if compareIntCount(int32(e.spellsCastThisTurnByMatching(ev.Player, alt.spec, alt.exclSelf, source)), v) {
				fired = true
				break
			}
		}
		if !fired {
			return false
		}
	}
	if v, ok := t.Params["ValidSA"]; ok {
		if !e.validSAMatches(source, ev, ctrl, v) {
			return false
		}
	}
	// ValidSAonCard$ on the SpellCast arm (trig:SpellCast.ValidSAonCard): a
	// filter over the CAST SPELL's own characteristics, here the mana-spent
	// comparison family Ancient Cellarspawn and Tokka & Rahzar carry as
	// `Spell.ManaSpent LTX` ("less than its mana value") and Blazing Bomb /
	// Ultros / Prompto as literal GE4/GE8 shapes. The AbilityCast arm owns
	// the same parameter for ACTIVATED abilities (validSAonCard); the two
	// arms share the parameter name, not the grammar, and this arm fails
	// closed on every shape it does not model (an unsupported head stays
	// silent rather than firing wide).
	if v, ok := t.Params["ValidSAonCard"]; ok {
		if !e.spellValidSAonCardMatches(obj, ev, v) {
			return false
		}
	}
	// Increment (CR 702.XX, task kw:Increment): the keyword expansion's
	// SpellCast marker (cards/kw_increment.go). "Whenever you cast a spell, if
	// the amount of mana you spent is greater than this creature's power or
	// toughness" is an event-relative comparison -- the mana paid for THIS
	// cast against the trigger SOURCE's current power/toughness -- so it cannot
	// ride the CheckSVar$/SVarCompare$ gate (literal RHS only) or ValidSA$ (a
	// filter over the cast spell). It lives here beside the other event-relative
	// SpellCast gates, the Dethrone/Training precedent read by their own
	// matchers.
	if v, ok := t.Params["Increment"]; ok && strings.EqualFold(strings.TrimSpace(v), "True") {
		if !e.incrementAdmits(source, ev) {
			return false
		}
	}
	// The target-shape params (targetsvalid1): TargetsValid$ narrows the cast
	// to spells whose targets all match the spec, IsSingleTarget$ to spells
	// with exactly one target. A SpellCast trigger "that targets CARDNAME"
	// (the whole Heroic family) must not fire on a cast that targets
	// something else. The spell's targets were recorded onto the stack object
	// by handleTarget BEFORE payCast emitted this PutOnStack (CR 601.2c:
	// targets are chosen before costs), so obj.Targets is the completed list.
	if !e.targetShapeMatches(t, obj.Targets, source, ctrl) {
		return false
	}
	if !hasXManaCostGate(t.Params, obj.Face().ManaCost, nil) {
		return false
	}
	return true
}

// targetShapeMatches implements the two target-shape parameters the
// cast/activation trigger family carries (task targetsvalid1): TargetsValid$
// and IsSingleTarget$. Both are read at every trigger arm the family has --
// the SpellCast/SpellCastOrCopy/SpellCopy arm (spellCastEval), the
// AbilityCast/SpellAbilityCast activation arm (abilityCastMatches) and the
// SpellAbilityCast spell arm (spellAbilityCastSpellMatches) -- through this
// one helper, so the grammar cannot drift between the arms. targets is the
// triggering spell's or activation's chosen target list, in each arm's own
// completion state (a spell's recorded stack-object Targets; an activation's
// pending-cast targets -- see abilityCastMatches for the timing).
//
// TargetsValid$ <spec>: EVERY target of the triggering spell or ability must
// match one of the comma alternatives (Forge's own all-targets reading of
// the parameter -- the corpus never carries the singular TargetValid$). The
// reading is what "targets only CARDNAME" needs together with
// IsSingleTarget$ (exactly one target, and it is CARDNAME). Object targets
// resolve through the ordinary object filter with the trigger source bound
// (so Card.Self is the trigger's own permanent) and player targets through
// the player filter (so Opponent matches the target player). A spell or
// ability with NO targets never matches -- "targets X" presupposes targets.
//
// IsSingleTarget$ True: the triggering spell or ability carries exactly one
// target. A present param with any other value fails closed (the trigger
// stays silent), per the repo's unreadable-condition convention; a param
// absent leaves the trigger's behaviour unchanged.
func (e *Engine) targetShapeMatches(t cards.Trigger, targets []state.Target, source state.ObjID, ctrl state.PlayerID) bool {
	if v, ok := t.Params["IsSingleTarget"]; ok {
		if !strings.EqualFold(strings.TrimSpace(v), "True") {
			return false
		}
		if len(targets) != 1 {
			return false
		}
	}
	if v, ok := t.Params["TargetsValid"]; ok {
		if len(targets) == 0 {
			return false
		}
		for _, tgt := range targets {
			if !e.targetMatchesTargetsValid(v, tgt, source, ctrl) {
				return false
			}
		}
	}
	return true
}

// targetMatchesTargetsValid reports whether ONE chosen target matches a
// TargetsValid$ spec. Comma alternatives are split with the shared
// filterAlternatives splitter (a named<Name, Name> argument's printed comma
// is not an alternative boundary); the target's own shape picks the filter:
// a player target through the player filter, an object target through the
// object filter with the trigger source bound so Card.Self and the other
// source-relative predicates read the trigger's own permanent. An object
// target never matches a player-base alternative and vice versa -- each
// filter simply answers false for the other's bases.
func (e *Engine) targetMatchesTargetsValid(spec string, tgt state.Target, source state.ObjID, ctrl state.PlayerID) bool {
	for alt := range effects.FilterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		if tgt.IsPlayer {
			if effects.MatchesPlayerSpecFrom(e.G, alt, tgt.Player, ctrl, source) {
				return true
			}
			continue
		}
		if tgt.Obj != 0 && e.matchesSpec(alt, tgt.Obj, e.specCtx(source, ctrl)) {
			return true
		}
	}
	return false
}

// spellAbilityCastSpellMatches is the SPELL half of Mode$ SpellAbilityCast
// ("whenever you cast a spell or activate an ability ..."): a PutOnStack
// event, evaluated with the spell-side parameters the corpus carriers
// write. Mode$ AbilityCast stays activation-only -- its oracle text is
// "whenever you activate an ability" -- so the mode split at the bottom of
// this file routes the two events by mode, not by event kind.
//
// The arm is deliberately narrow: ValidActivatingPlayer$, ValidSA$ (the
// spell-side kind grammar, spellAbilityCastValidSA), the shared
// target-shape params and HasXManaCost$ -- the same four the activation arm
// reads, plus the ValidCard$ this build now reads (the delayed-registration
// mirror's grammar: castProvenanceAdmits, then the ordinary filter) so a
// future carrier's card restriction cannot silently widen. The cast-count
// clauses (ActivatorThisTurnCast*) remain SpellCast-mode parameters no
// SpellAbilityCast carrier carries.
func (e *Engine) spellAbilityCastSpellMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	obj := e.G.Obj(ev.Obj)
	if obj == nil || obj.Face() == nil {
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidActivatingPlayer"]; ok {
		// ev.Player is the player who cast the spell; MatchesPlayerSpecCtx
		// resolves "You" as the trigger's controller and
		// Player.EnchantedController against the trigger's source permanent.
		if !effects.MatchesPlayerSpecCtx(e.G, v, ev.Player, ctrl, e.playerSpecCtx(source)) {
			return false
		}
	}
	if v, ok := t.Params["ValidCard"]; ok {
		// The delayed-registration mirror's ValidCard$ grammar
		// (eventDelayedSpellCastMatches): the provenance strip, then the
		// ordinary object filter over the cast spell on the stack. The
		// corpus's one SpellAbilityCast carrier with the param is the trivial
		// `Card` (vazi_keen_negotiator), so no live behaviour changes; the
		// read keeps the first future carrier from widening silently.
		v, ok := e.castProvenanceAdmits(v, ev.Obj, ctrl)
		if !ok {
			return false
		}
		if !e.matchesSpec(spellCastPermanentSpec(v), ev.Obj, e.specCtx(source, ctrl)) {
			return false
		}
	}
	if v, ok := t.Params["ValidSA"]; ok {
		if !e.spellAbilityCastSpellValidSA(obj, v, ctrl, e.specCtx(source, ctrl)) {
			return false
		}
	}
	if !e.targetShapeMatches(t, obj.Targets, source, ctrl) {
		return false
	}
	if !hasXManaCostGate(t.Params, obj.Face().ManaCost, nil) {
		return false
	}
	return true
}

// spellAbilityCastSpellValidSA evaluates a SpellAbilityCast trigger's
// ValidSA$ clause on the SPELL half. The grammar is Forge's comma-separated
// OR list of "<kind>.<constraint>" values where a kind may name a spell, an
// ability, or the union:
//
//   - Spell / Instant / Sorcery / Card / Permanent (or no kind): the whole
//     alternative is the ordinary object filter over the cast spell --
//     Spell.nonCreature (Feather, Radiant Arbiter) and Instant.YouCtrl /
//     Sorcery.YouCtrl (Bill Potts) resolve through the existing bases and
//     predicates, including nonCreature and YouCtrl.
//   - SpellAbility (the spell-or-ability union kind, Grip of Chaos's
//     SpellAbility.!ManaAbility): any spell matches it -- a spell is never
//     a mana ability, so !ManaAbility holds and a bare SpellAbility holds;
//     YouCtrl checks the spell's controller; ManaAbility never holds.
//   - Activated / Triggered name ability kinds only and never match a
//     spell (the activation arm's abilityCastValidSA owns them there).
//
// An alternative this reading cannot resolve is skipped; the trigger fires
// only when at least one alternative matches (an unresolvable clause fails
// closed, the repo's convention).
func (e *Engine) spellAbilityCastSpellValidSA(obj *state.Object, validSA string, ctrl state.PlayerID, sc effects.SpecContext) bool {
	v := strings.TrimSpace(validSA)
	if v == "" {
		return true
	}
	for alt := range effects.FilterAlternatives(v) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		kind, constraint, _ := strings.Cut(alt, ".")
		switch kind {
		case "Activated", "Triggered":
			continue
		case "SpellAbility":
			switch constraint {
			case "", "!ManaAbility":
				return true
			case "ManaAbility":
				continue
			case "YouCtrl":
				if obj.Controller == ctrl {
					return true
				}
				continue
			}
			continue
		default:
			// Spell / Instant / Sorcery / Card / Permanent / no kind -- the
			// ordinary object filter over the cast spell.
			if e.matchesSpec(alt, obj.ID, sc) {
				return true
			}
		}
	}
	return false
}

// spellAbilityCastMatches is Mode$ SpellAbilityCast's dispatcher (the
// spell-or-activate union): an AbilityPush or KeywordAbilityPush event routes
// to the activation arm, a PutOnStack to the spell arm. A named
// method, not an inline func literal, so the param census (the rot guard)
// attributes both arms' trigger-param reads to this mode through the one
// dispatch function.
func (e *Engine) spellAbilityCastMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	switch ev.Kind {
	case events.AbilityPush, events.KeywordAbilityPush:
		return e.abilityCastMatches(t, source, ev)
	case events.PutOnStack:
		return e.spellAbilityCastSpellMatches(t, source, ev)
	}
	return false
}

// abilityCastMatches implements Mode$ AbilityCast and Mode$ SpellAbilityCast
// against an AbilityPush or KeywordAbilityPush event -- the moment an
// activated ability is put on the stack. This is the COMPLETED boundary:
// the push is emitted only once the activation's cost is fully paid and
// the ability object is minted, so a trigger firing here is never observing a
// provisional or abandoned activation. (F15: there was no such arm at all, so
// Rings of Brighthearth's "Whenever you activate an ability, if it isn't a
// mana ability..." trigger never fired.)
//
// ValidActivatingPlayer$ and ValidSA$ narrow the activation the trigger
// observes, in the same two params the corpus spells them with. A source
// card whose face has no Abilities at the recorded index (stale data) is
// a no-op, never a panic. A keyword line without a synthesizable ability
// also fails closed.
func (e *Engine) abilityCastMatches(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	if ev.Kind != events.AbilityPush && ev.Kind != events.KeywordAbilityPush {
		return false
	}
	obj := e.G.Obj(ev.Obj)
	if obj == nil || obj.Face() == nil {
		return false
	}
	ctrl := e.controllerOf(source)
	if v, ok := t.Params["ValidActivatingPlayer"]; ok {
		// ev.Player is the player who activated the ability;
		// MatchesPlayerSpecCtx resolves "You" as the trigger's controller.
		if !effects.MatchesPlayerSpecCtx(e.G, v, ev.Player, ctrl, e.playerSpecCtx(source)) {
			return false
		}
	}
	// CR 702.140d: an AbilityPush's Amount is the FLAT pile-ability index
	// events.Apply decoded the mint from, so an under-card activation sits
	// past the top face's list. Resolve through the same pile view the
	// emitter, the offer loop and events.Apply share -- reading
	// obj.Face().Abilities here would make a ValidSA$ narrowing fail closed
	// on every under-card activation and degrade a HasXManaCost$ gate to the
	// pile TOP's printed cost (a wrong-wide pass whenever the top card's cost
	// carried {X}). A plain permanent's flat index is unchanged.
	pa, havePa := obj.PileAbilityAt(int(ev.Amount))
	var ab *cards.SA
	if ev.Kind == events.KeywordAbilityPush {
		// Counter is the replayable derived keyword line used by Apply to
		// mint the stack body's SA. Amount is not a printed-ability index.
		ab = cards.GrantedKeywordAbility(ev.Counter)
		if ab == nil {
			return false
		}
	} else if havePa {
		ab = pa.SA
	}
	// ValidCard$ scopes AbilityCast to the permanent whose ability was
	// activated. It is distinct from the trigger source: Avalanche of Sector
	// 7's Artifact restriction must reject an ability from a non-artifact.
	if v, ok := t.Params["ValidCard"]; ok {
		if !e.matchesSpec(v, obj.ID, e.specCtx(source, ctrl)) {
			return false
		}
	}
	removed := int32(-1)
	if strings.Contains(t.Params["ValidSA"], "CountersRemovedToPay") || strings.Contains(t.Params["ValidSAonCard"], "CountersRemovedToPay") {
		removed = e.activationCountersRemoved(ev, ab)
	}
	if v, ok := t.Params["ValidSA"]; ok {
		if ab == nil || !abilityCastValidSA(ab, v, obj.Controller, ctrl, removed) {
			return false
		}
	}
	if v, ok := t.Params["ValidSAonCard"]; ok {
		if ab == nil || !abilityCastValidSA(ab, v, ev.Player, obj.Controller, removed) {
			return false
		}
	}
	// The target-shape params (targetsvalid1), the activation arm: ertha_jo's
	// "Whenever you activate an ability that targets a creature or player".
	//
	// AbilityPush's ordinary trigger scan runs before handleTarget records
	// targets, so while the cast flow is live use its answered target list.
	// TriggersWhenSpent dispatches after finishTargetedCast records those
	// targets; at that point read the minted ability stack object's event-backed
	// Targets rather than the source permanent's (unrelated) target list.
	tgts := obj.Targets
	if pc := e.cast; pc != nil && pc.isAbility() && pc.card == ev.Obj {
		tgts = pc.targets
	} else if stack := e.abilityCastStackObject(ev.Obj); stack != 0 {
		if stackObj := e.G.Obj(stack); stackObj != nil {
			tgts = stackObj.Targets
		}
	}
	if !e.targetShapeMatches(t, tgts, source, ctrl) {
		return false
	}
	// HasXManaCost$ reads the same body as ValidSA$: a printed pile index
	// or the keyword line that Apply used to mint the granted ability.
	if !hasXManaCostGate(t.Params, "", ab) {
		return false
	}
	return true
}

// abilityCastValidSA reports whether an activated ability matches a ValidSA$
// narrowing on an AbilityCast/SpellAbilityCast trigger. The grammar is Forge's
// comma-separated OR list of "<kind>.<constraint>" values; a value whose kind
// names a spell (Spell/Instant/Sorcery) describes a cast, not an activation,
// so it never matches an activated ability and is simply skipped. An absent
// or unqualified value matches every activated ability. abCtrl is the
// activated ability's controller (the activator) and ctrl the trigger
// source's controller: a YouCtrl constraint holds when the two agree
// (bill_potts' Activated.YouCtrl; the spell half resolves its own YouCtrl
// through the ordinary filter).
func abilityCastValidSA(ab *cards.SA, validSA string, abCtrl, ctrl state.PlayerID, removed int32) bool {
	v := strings.TrimSpace(validSA)
	if v == "" {
		return true
	}
	// The SAME alternative splitter the spell half (spellAbilityCastSpellValidSA)
	// uses: comma-aware of a named<Name, Name> argument's printed comma, so
	// the two arms of one mode cannot disagree on where one alternative ends.
	for alt := range effects.FilterAlternatives(v) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		kind, constraint := alt, ""
		if i := strings.IndexByte(alt, '.'); i >= 0 {
			kind, constraint = alt[:i], alt[i+1:]
		}
		switch kind {
		case "SpellAbility", "Activated", "":
			if abilityCastConstraintHolds(ab, constraint, abCtrl, ctrl, removed) {
				return true
			}
		}
	}
	return false
}

// abilityCastConstraintHolds evaluates one alternative's constraint: a
// `+`-joined list of terms that must ALL hold (Forge's conjunction, as in
// Activated.Loyalty+OppCtrl). An empty constraint matches every activated
// ability; an unknown term fails the whole alternative closed.
//
// Loyalty / !Loyalty classify the activated ability as a planeswalker
// loyalty ability (isLoyaltyAbility: the Planeswalker$ marker or a LOYALTY
// counter cost part). CountersRemovedToPay<OP><N> (Way of the Mind
// Sculptor's "if you removed two or more loyalty counters to activate it")
// compares the counters the activation removed as its cost; removed < 0
// means the caller could not establish that count, and the term fails closed.
func abilityCastConstraintHolds(ab *cards.SA, constraint string, abCtrl, ctrl state.PlayerID, removed int32) bool {
	if constraint == "" {
		return true
	}
	for term := range strings.SplitSeq(constraint, "+") {
		ok := false
		switch term {
		case "!ManaAbility":
			ok = !isManaAbilityAPI(ab.API)
		case "ManaAbility":
			ok = isManaAbilityAPI(ab.API)
		case "YouCtrl":
			ok = abCtrl == ctrl
		case "OppCtrl":
			ok = abCtrl != ctrl
		case "Loyalty":
			ok = isLoyaltyAbility(ab)
		case "!Loyalty":
			ok = !isLoyaltyAbility(ab)
		default:
			if cmp, found := strings.CutPrefix(term, "CountersRemovedToPay"); found && removed >= 0 {
				if op, n, valid := splitCompare(cmp); valid {
					ok = applyCompare(int(removed), op, n)
				}
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// activationCountersRemoved is the number of counters the activation ev
// pushed removed as its cost (its SubCounter parts), for the
// CountersRemovedToPay ValidSA$ term. The AbilityPush is emitted only once
// the cost is paid, while the activation's pendingCast is still live, so the
// paid parts and the announced X are read from it: a fixed part counts its
// N, an announced (SubCounter<X/...>) part counts the announced X. With no
// live activation for ev the printed cost answers when every part is fixed;
// an announced part then makes the count unknown (-1, failing closed).
func (e *Engine) activationCountersRemoved(ev events.Event, ab *cards.SA) int32 {
	if pc := e.cast; pc != nil && pc.isAbility() && pc.card == ev.Obj {
		var n int32
		for _, part := range pc.cost.SubCounter {
			if part.Announced {
				n += pc.x
			} else {
				n += part.N
			}
		}
		return n
	}
	if ab == nil {
		return -1
	}
	var n int32
	for _, part := range ParseCost(ab.Params["Cost"]).SubCounter {
		if part.Announced {
			return -1
		}
		n += part.N
	}
	return n
}

// hasXManaCostGate implements HasXManaCost$ True on cast/activation trigger
// modes (Mode$ SpellCast and Mode$ AbilityCast/SpellAbilityCast): "whenever
// you cast a permanent spell with a mana cost that contains {X}" / "...or
// activate an ability ... if that ability's activation cost contains {X}"
// (Unbound Flourishing, Glava Five-Advents Mage, Brass Infiniscope, Magus
// Lucea Kane). The gate reads the PRINTED cost -- the spell's ManaCost face
// field, or the ability's Cost$ param -- through ParseCost, which counts only
// the mana {X} tokens (c.X); a non-mana component token such as
// SubCounter<X/CHARGE> or PayEnergy<X> is matched by its own grammar and does
// NOT count, which is exactly the card text's "mana cost/activation cost
// contains {X}". For an activation the printed Cost$ still carries the X at
// AbilityPush time (the announced value folds into the provisional payment
// cost, rules/cast.go's pc.cost.WithX, never into the SA params), so the
// gate is about the printed shape, never the announced value.
//
// A param present with a value other than True FAILS CLOSED (the trigger
// stays silent), per the repo's unreadable-condition convention; a param
// absent leaves the trigger's behaviour unchanged. ab is the activated
// ability on the AbilityCast arm (nil on the SpellCast arm, where faceCost is
// the spell's printed face cost); a nil ab with an empty faceCost fails
// closed rather than firing wide.
func hasXManaCostGate(params map[string]string, faceCost string, ab *cards.SA) bool {
	v, ok := params["HasXManaCost"]
	if !ok {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(v), "True") {
		return false
	}
	if ab != nil {
		return ParseCost(ab.Params["Cost"]).X > 0
	}
	return ParseCost(faceCost).X > 0
}

// manaSpentForCast sums the mana the player spent casting the spell ev put
// on the stack: every negative ManaAdd for that player since the spell's
// own PutOnStack. Between CR 601.2a's push and the deferred cast trigger
// the only mana leaving a pool is this cast's payment (a mana window adds
// mana, it never spends), and the scan reads the log, so a replay derives
// the identical number.
func (e *Engine) manaSpentForCast(p state.PlayerID, id state.ObjID) int32 {
	var spent int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.PutOnStack && ev.Obj == id && ev.Player == p {
			return spent
		}
		if ev.Kind == events.ManaAdd && ev.Player == p && ev.Amount < 0 {
			spent += -ev.Amount
		}
	}
	return spent
}

// incrementAdmits implements the Increment keyword's event-relative condition:
// the mana actually spent to cast the triggering spell is greater than the
// trigger SOURCE's current power OR greater than its current toughness. The
// oracle reminder's "power or toughness" is a disjunction (CR 702.XX), so one
// of the two comparisons sufficing is the firing condition. Power and
// toughness are the LAYER OUTPUT (Engine.Power/Engine.Toughness, the same
// derived values Training reads for its power comparison), so a lord, a
// counter or a pump moves the comparison exactly as it moves the creature.
//
// The spend comes from the SHARED manaSpentForCast log scan validSAMatches and
// spellValidSAonCardMatches read, so the three SpellCast spend readers can
// never disagree. A source that is no longer a creature on the battlefield
// when the trigger fires (it left, or became a noncreature) has no P/T to
// compare and fails closed, as every unreadable gate in this file does.
func (e *Engine) incrementAdmits(source state.ObjID, ev events.Event) bool {
	src := e.G.Obj(source)
	if src == nil || src.Face() == nil {
		return false
	}
	spent := e.manaSpentForCast(ev.Player, ev.Obj)
	return spent > e.Power(source) || spent > e.Toughness(source)
}

// validSAMatches evaluates a SpellCast trigger's ValidSA$ clause. The
// measured grammar is the mana comparison family, "Spell.ManaSpent <OP><N>"
// (Roiling Vortex's EQ0 -- no mana was spent to cast that spell; Raggadragga
// and the emperor's GE7/EQ0 shapes read the same head): the value is the
// mana the ACTIVATOR paid for the cast, so GTX/other dynamic values fail
// closed. A clause naming an unmodelled property (ManaSpentBy, MayPlaySource,
// Self, YouCtrl) or carrying no comparison is evaluated as a plain spec
// filter over the cast spell when it is a single field, and fails closed
// otherwise.
func (e *Engine) validSAMatches(source state.ObjID, ev events.Event, ctrl state.PlayerID, clause string) bool {
	fields := strings.Fields(strings.TrimSpace(clause))
	switch len(fields) {
	case 1:
		return e.matchesSpec(fields[0], ev.Obj, e.specCtx(source, ctrl))
	case 2:
		if fields[0] == "Spell.ManaSpent" {
			return compareIntCount(e.manaSpentForCast(ev.Player, ev.Obj), fields[1])
		}
		return false
	}
	return false
}

// spellValidSAonCardMatches evaluates a SpellCast trigger's ValidSAonCard$
// clause: a filter over the CAST SPELL's own ability/characteristics. The
// one measured SpellCast shape (the corpus's five `Spell.ManaSpent` carriers)
// is the same mana-comparison grammar validSAMatches reads for ValidSA$, with
// one addition -- the value may be spelled `X`, the CAST SPELL's mana value
// (the trigger-side `SVar:X:Count$CardManaCost` both LTX carriers define:
// Ancient Cellarspawn and Tokka & Rahzar's "the amount of mana spent to cast
// it was less than its mana value"). `Spell.ManaSpent GE4` (Blazing Bomb,
// Ultros, Prompto) keeps the literal reading. The mana actually spent comes
// from the SHARED manaSpentForCast log scan, the identical provenance
// validSAMatches reads -- so the two comparison params of one SpellCast
// trigger can never disagree on the spend.
//
// Every other head (Dragonlord Kolaghan's
// `Spell.Creature+sharesNameWith YourGraveyard`, Gonti's `Spell.YouDontOwn`)
// is an unmodelled shape and FAILS CLOSED (the trigger stays silent), per
// the repo's unreadable-condition convention; broadening the grammar is out
// of this fix's scope. A copy reads the ORIGINAL spell's spend through
// manaSpentForCast, the same reading validSAMatches gives a copy today.
func (e *Engine) spellValidSAonCardMatches(obj *state.Object, ev events.Event, clause string) bool {
	fields := strings.Fields(strings.TrimSpace(clause))
	if len(fields) != 2 || fields[0] != "Spell.ManaSpent" {
		return false
	}
	expr := strings.TrimSpace(fields[1])
	for _, op := range []string{"EQ", "NE", "GE", "LE", "GT", "LT"} {
		rest := strings.TrimPrefix(expr, op)
		if rest == expr {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rest), "X") {
			// X is the cast spell's mana value (CR 202.3). A face-less object
			// cannot name one -- fail closed rather than read 0.
			if obj == nil || obj.Face() == nil {
				return false
			}
			return compareIntCount(e.manaSpentForCast(ev.Player, ev.Obj),
				op+strconv.Itoa(int(obj.Face().ManaValue())))
		}
		// A literal (or any non-X value) goes through the shared comparison
		// grammar, which fails closed on a value it cannot parse.
		return compareIntCount(e.manaSpentForCast(ev.Player, ev.Obj), expr)
	}
	return false
}

// spellCastPermanentSpec rewrites the leading `Permanent` base token of
// every comma-alternative in a SpellCast trigger's ValidCard$ spec to
// `PermanentCard`, so the "whenever you cast a permanent spell" family
// (Unbound Flourishing, the Defiler cycle, Archmage of Echoes) reads the
// base as a permanent card at PutOnStack -- the evaluated object there is
// always the cast spell, and CR 109.2 makes an artifact/creature/
// enchantment/planeswalker/battle spell a permanent spell. matchesBase's
// bare `Permanent` keeps the on-the-battlefield reading every other filter
// depends on and is deliberately untouched. rules/stack.go's
// targetSpecForZone and effects/cardflow.go's permanentCardSpec carry the
// same leading-token rule with a zone gate (both leave ZStack unchanged);
// this third copy applies it WITHOUT a zone gate and ACROSS alternatives,
// because a comma-separated spec names each alternative's own base
// (Archmage of Echoes' `Permanent.Faerie,Permanent.Wizard`). effects must
// not import rules and vice versa, so the copies cannot share code. The
// rewritten text is not the original spec, so the compiled sidecar's
// byText lookup misses and the textual oracle answers it -- which after the
// matchesBase/matchesCompiledBase relaxation is correct.
func spellCastPermanentSpec(spec string) string {
	// Comma-split at angle-bracket depth 0: a named<X, Y> name argument's
	// printed comma is not an alternative boundary (the same distinction
	// effects.filterAlternatives draws).
	depth, start := 0, 0
	var b strings.Builder
	for i := 0; i < len(spec); i++ {
		switch spec[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if start > 0 {
					b.WriteByte(',')
				}
				b.WriteString(spellCastLeadingPermanentToCard(spec[start:i]))
				start = i + 1
			}
		}
	}
	if start == 0 {
		return spellCastLeadingPermanentToCard(spec)
	}
	b.WriteByte(',')
	b.WriteString(spellCastLeadingPermanentToCard(spec[start:]))
	return b.String()
}

// spellCastLeadingPermanentToCard is the leading-token rule targetSpecForZone
// and permanentCardSpec share: the whole spec is `Permanent`, or its leading
// token is `Permanent` followed by '.', '+' or ',' -- only that token is
// rewritten, every qualifier rides along.
func spellCastLeadingPermanentToCard(alt string) string {
	if alt == "Permanent" {
		return "PermanentCard"
	}
	if len(alt) > len("Permanent") && alt[:len("Permanent")] == "Permanent" {
		switch alt[len("Permanent")] {
		case '.', '+':
			return "PermanentCard" + alt[len("Permanent"):]
		}
	}
	return alt
}

// triggerSnapshot is immutable look-back state. Parked replacement choices
// may retain it across intent/Clone boundaries; each matching walk constructs
// its own Engine scratch caches, never mutating or sharing the snapshot's.
type triggerSnapshot struct {
	game       *state.Game
	continuous []ContinuousEffect
	// retained marks a snapshot a parked record holds (retainTriggerBefore):
	// it outlives the window that took it, so its window never recycles its
	// arena (trigger_snapshot_pool.go). Set only while the snapshot is still
	// private to the engine that took it, so a snapshot Clone shares is
	// never written.
	retained bool
	// noLookBack: the board was proven inert for the look-back walk when the
	// window opened (lookBackNoopBoard); see noLookBackSnapshot.
	noLookBack bool
	// staticOwner/staticSeq record the taking engine's static memo when it
	// was current and quiet for exactly this board (lookback_static.go):
	// the observer adopts that engine's memo while it is still that build.
	staticOwner *Engine
	staticSeq   uint64
}

// triggerCastAlternatives splits a Mode$ SpellCast trigger's ValidCard$ into
// its comma alternatives and applies the trigger-side bare !CastSaSource
// reading (task castprov2, Alania, Divergent Storm — measured: exactly 1
// trigger-side line in the corpus): an alternative carrying the token names
// "a spell whose PRINTED NAME is not the trigger SOURCE's printed name" (the
// oracle's "other than NICKNAME"; Forge has no trigger-side CastSaSource
// grammar precedent in this corpus, so this reading is a documented
// decision, not a discovered fact). The cast card's printed name equal to
// the source's drops the alternative (a second Alania cast fails the Otter
// alternative and does not trigger); the token is otherwise stripped and the
// alternative kept for the ordinary filter. ok is false when no alternative
// survives.
func (e *Engine) triggerCastAlternatives(rawSpec string, source, castObj state.ObjID) ([]triggerCastAlt, bool) {
	spec := spellCastPermanentSpec(rawSpec)
	sourceName, castName := "", ""
	if o := e.G.Obj(source); o != nil && o.Face() != nil {
		sourceName = o.Face().Name
	}
	if o := e.G.Obj(castObj); o != nil && o.Face() != nil {
		castName = o.Face().Name
	}
	isSelf := sourceName != "" && castName == sourceName
	var alts []triggerCastAlt
	for alt := range effects.FilterAlternatives(spec) {
		s, had := effects.StripPredicateToken(alt, "!CastSaSource")
		if had && isSelf {
			continue
		}
		alts = append(alts, triggerCastAlt{spec: s, exclSelf: had})
	}
	if len(alts) == 0 {
		return nil, false
	}
	return alts, true
}

// spellsCastThisTurnByMatching counts player p's spells cast this turn whose
// object matches ONE alternative spec, with the trigger-side NICKNAME
// exclusion applied to the tally when that alternative carried the
// !CastSaSource token (Alania's "the first Otter spell other than Alania":
// the source's own casts do not count toward that alternative's first).
// The current cast is INCLUDED — it is already in the log when the deferred
// trigger fires, and the oracle's "first ... you've cast this turn" counts
// it (EQ1 = this cast is the first).
func (e *Engine) spellsCastThisTurnByMatching(p state.PlayerID, spec string, exclSelf bool, source state.ObjID) int {
	selfName := ""
	if exclSelf {
		if o := e.G.Obj(source); o != nil && o.Face() != nil {
			selfName = o.Face().Name
		}
	}
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind != events.PutOnStack || ev.Player != p {
			continue
		}
		o := e.G.Obj(ev.Obj)
		if o == nil || o.Face() == nil {
			continue
		}
		if selfName != "" && o.Face().Name == selfName {
			continue
		}
		if e.matchesSpecFrom(spec, ev.Obj, p, ev.Obj) {
			n++
		}
	}
	return n
}

func init() {
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.spellCastMatches(t, source, ev)
	}, "SpellCast")
	// The magecraft family: the cast half is the ordinary SpellCast evaluation
	// on the PutOnStack event; the copy half delegates a StackCopy event to
	// spellCopyMatches. A copy never re-enters the stack as a PutOnStack --
	// effects/copy.go emits events.StackCopy naming the original -- so
	// spellCastMatches' entering-the-stack guard would keep the copy half dead
	// if the whole mode fell through to it.
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		if ev.Kind == events.StackCopy {
			return e.spellCopyMatches(t, source, ev)
		}
		return e.spellCastMatches(t, source, ev)
	}, "SpellCastOrCopy")
	// The copy-only mode ("Whenever you copy a spell, ..."): plain casts are not
	// copies, so a PutOnStack event must not fire it.
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.spellCopyMatches(t, source, ev)
	}, "SpellCopy")
	// The activation-cast family, split (targetsvalid1): Mode$ AbilityCast is
	// "whenever you activate an ability" -- AbilityPush or KeywordAbilityPush,
	// never a spell cast -- while Mode$ SpellAbilityCast is Forge's "spell or
	// activate an ability" union: both push kinds take the activation arm, and
	// PutOnStack takes the spell arm
	// (spellAbilityCastSpellMatches). triggerModeEvents and the compiled
	// triggerInterestForMode mirror this split.
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
		return e.abilityCastMatches(t, source, ev)
	}, "AbilityCast")
	registerTrigMatcher((*Engine).spellAbilityCastMatches, "SpellAbilityCast")
	// ManaExpend (CR-expend, Bloomburrow Commander): "Whenever you expend N
	// ..." fires when its controller's per-turn cast-spend tally CROSSES the
	// trigger's Amount$ N. payCast (rules/cast.go) folds every paid cast into
	// the per-turn engine tally (manaExpended) and, when a carrier is out,
	// emits a pay-time FlagManaExpendCast CastInfo whose Amount is that cast's
	// spend; this matcher reads the tally for `total` and `total - Amount` for
	// the pre-payment base, so prev < N <= total. A cast that starts
	// at-or-above N fires nothing (no second crossing), and a later threshold
	// (Muerra's expend 8 beside its expend 4) crosses independently in the
	// same payment. Player$ You is the only selector the corpus writes (13
	// lines), but the matcher reads the shared player-spec grammar so an
	// explicit Opponent/Player selector is honoured; an ABSENT Player$ keeps
	// the historical You-only reading (the trigger's controller must be the
	// payer), and an unresolvable selector fails closed.
	registerTrigMatcher((*Engine).manaExpendMatches, "ManaExpend")
}

// manaExpendMatches implements Mode$ ManaExpend. See the registration above
// for the crossing contract; manaExpendReaderOut (rules/cast.go) is the
// heads-safety gate that makes the matched event exist at all.
//
// The crossing base is e.manaExpendTotal -- the engine's per-turn tally,
// which payCast updates on EVERY paid cast (manaExpendAdd), not just casts
// made while a carrier was out. Reading it, rather than a gated event fold,
// is what makes a carrier that entered mid-turn see the casts made before it
// entered: the pre-entry spend is in `total` but not in `ev.Amount`, so
// `prev = total - ev.Amount` is the true pre-payment tally for this cast and
// the crossing test is exact in both directions.
func (e *Engine) manaExpendMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.CastInfo || events.FlagsFrom(ev.Counter)&state.FlagManaExpendCast == 0 {
		return false
	}
	ctrl := e.controllerOf(source)
	if int(ev.Player) >= len(e.G.Players) {
		return false
	}
	// An omitted Player$ means the trigger's own controller (Forge's default
	// for the "whenever YOU expend" family), not "any player". Match the
	// selector through the shared player-spec grammar either way: explicit
	// Player$ Opponent/Player forms resolve here, and an absent one is spelled
	// as You so the controller check is not skipped.
	player := strings.TrimSpace(t.Params["Player"])
	if player == "" {
		player = "You"
	}
	if !effects.MatchesPlayerSpecCtx(e.G, player, ev.Player, ctrl, e.playerSpecCtx(source)) {
		return false
	}
	n, ok := e.manaExpendAmount(source, t.Params["Amount"], ctrl)
	if !ok || n <= 0 {
		// An unreadable or non-positive Amount$ is a threshold this engine
		// cannot evaluate: fail closed, never fire wide.
		return false
	}
	total := e.manaExpendTotal(ev.Player)
	prev := total - ev.Amount
	return prev < n && total >= n
}

// manaExpendAmount resolves a literal, source SVar, or inline Count$ amount.
// EvalCountOK preserves the distinction between an understood zero and an
// expression this engine cannot read; both remain no-fire thresholds here.
func (e *Engine) manaExpendAmount(source state.ObjID, raw string, controller state.PlayerID) (int32, bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.ParseInt(raw, 10, 32); err == nil {
		return int32(n), true
	}
	obj := e.G.Obj(source)
	if obj == nil || obj.Face() == nil {
		return 0, false
	}
	svars := obj.Face().SVars
	if body, ok := svars[raw]; ok {
		raw = body
	}
	ctx := &effects.Ctx{Source: source, Controller: controller, SVars: svars}
	return effects.EvalCountOK(e, ctx, raw)
}
