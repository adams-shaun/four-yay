package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attachmentSBAs implements CR 704.5m/n, the state-based actions that keep
// attachments legal. This build's attachments are Auras and Equipment; a
// permanent is "attached" exactly when its state.Object.AttachedTo is
// non-zero. Four of the five rules are the same "it became illegal later"
// shape that lands here rather than being checked only at attach time:
//
//   - CR 704.5m, "an Aura is attached to something it can no longer legally
//     be attached to": the bearer is gone (moved zones -- its state-based
//     action below), the bearer no longer matches the Aura's Enchant spec,
//     or the bearer gained protection from the Aura. This is the "becomes
//     illegal later" half of CR 704.5m. Either way the Aura goes to its
//     owner's graveyard.
//   - CR 704.5m, "an Aura attached to nothing": an Aura on the battlefield
//     with AttachedTo == 0 goes to the graveyard. (An Equipment is allowed to
//     sit unattached, so the "attached to nothing" wording is Aura-only.)
//   - CR 704.5n, "an Equipment or Fortification attached to something
//     illegal": an Equipment whose bearer is now a non-creature is detached
//     (an Attach with no IDs), and so is one whose bearer left the
//     battlefield entirely; a Fortification whose bearer is no longer a land
//     detaches the same way (CR 702.67b's "attached to a nonland permanent
//     becomes unattached"). The land test reads the CURRENT derived type
//     list, not the printed face: a Darksteel Mutation-shaped layer-4 static
//     that strips the Land card type ends the attachment just as a printed
//     nonland bearer would.
//   - "anything attached to an object that left the battlefield": the same
//     detached-for-Equipment, destroyed-Aura handling CR 704.5m's own
//     "attached to nothing" already gives the Aura (once its bearer left,
//     AttachedTo points at no battlefield permanent), so the Aura half does
//     not need a separate clause -- but the case is listed here so the
//     reader finds all five rules in one place.
//
// Reports whether anything changed, so it plugs into checkStateBased's own
// pass loop (a graveyard-bound Aura can itself trigger on its zone change,
// so "changed" keeps the loop going until the board is stable).
func (e *Engine) attachmentSBAs() bool {
	changed := false
	// The battlefield snapshot the walk ranges (emit moves objects out
	// of the live zone) lives in the Engine's SBA scratch, taken for the
	// walk so a re-entrant pass allocates its own (sbaIDBuf).
	ids := e.sbaIDBuf
	e.sbaIDBuf = nil
	// The board's layer-4 presence for sbaIsAura, re-read whenever an emit
	// below has moved the log (a departure can change the active list).
	ltAt, anyLType := -1, false
	for _, p := range e.G.AliveFrom(0) {
		ids = append(ids[:0], e.G.Zone(state.ZBattlefield, p)...)
		for _, id := range ids {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			if o.PhasedOut {
				// CR 702.25b/d: a phased-out permanent is treated as though it
				// does not exist, so attachment SBAs do not fire on it (it and
				// anything attached to it phase out together).
				continue
			}
			if n := len(e.L.Events); n != ltAt {
				ltAt, anyLType = n, e.activeHasLType()
			}
			if o.HasAttachedPlayer {
				// Enchant:Player Auras have no object bearer. A lost seat
				// cannot remain enchanted (CR 800.4), and an illegal
				// attachment takes the same graveyard SBA as an object Aura.
				p := o.AttachedPlayer
				if !e.isAura(o) || int(p) >= len(e.G.Players) || e.G.Players[p].Lost ||
					!e.playerAuraStillMatchesEnchant(o, p) || e.playerProtectedFrom(p, o.ID) {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id,
						From: state.ZBattlefield, To: state.ZGraveyard, Text: "illegal player attachment"})
					changed = true
				}
				continue
			}
			if o.AttachedTo == 0 {
				// A detached Aura has nothing legal to do on the battlefield.
				if e.sbaIsAura(o, anyLType) {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id,
						From: state.ZBattlefield, To: state.ZGraveyard, Text: "Aura attached to nothing"})
					changed = true
				}
				continue
			}
			bearer := e.G.Obj(o.AttachedTo)
			if o.BestowedAttached() {
				// CR 702.114b: a bestowed permanent that is no longer attached
				// to a legal creature becomes a creature again -- it DETACHES
				// and stays on the battlefield, never taking the "Aura attached
				// to nothing / bearer left / illegal bearer" graveyard arms
				// below. The illegal-bearer reading chosen here: the bearer
				// left the battlefield, is no longer a creature, or gained
				// protection from the bestowed card's colours -- any of the
				// three emits one detach Attach (no IDs) and the card is a
				// creature again from the derived type switch. (The Aura arm
				// binning it to the graveyard would contradict 702.114b's own
				// "becomes a creature again if it's not attached".)
				if bearer == nil || bearer.Zone != state.ZBattlefield ||
					!e.IsCreature(bearer.ID) || e.protectedFrom(bearer.ID, o.ID) {
					e.emit(events.Event{Kind: events.Unattached, Obj: id,
						IDs:  []state.ObjID{o.AttachedTo},
						Text: "bestowed attachment ended"})
					changed = true
				}
				continue
			}
			if bearer == nil || bearer.Zone != state.ZBattlefield {
				// The bearer left the battlefield: an Equipment detaches
				// (CR 704.5n), an Aura goes to the graveyard (CR 704.5m).
				//
				// The graveyard-enchant family (Animate Dead, Dance of the
				// Dead) is the measured exception: between the Aura entering
				// (attached at resolution, while the spell was still on the
				// stack) and its reanimate trigger resolving, the bearer is
				// LEGALLY a creature card in a graveyard -- CR 704.5m sweeps
				// an attachment that is no longer legal, and the Aura's
				// current enchant spec still admits exactly this bearer here.
				// The test is deliberately zone-POSITIVE (the spec must match
				// the bearer AND carry an inZone<X> word whose X names the
				// bearer's current zone), because MatchesSpecFrom's bare type
				// words are zone-blind (spec `Creature` matches a graveyard
				// bear too) and the exemption must never save an ordinary Aura
				// whose bearer died.
				if e.isAura(o) && bearer != nil && e.auraEnchantZoneAdmits(o, bearer) {
					continue
				}
				if e.isAura(o) {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id,
						From: state.ZBattlefield, To: state.ZGraveyard, Text: "attached to an object that left the battlefield"})
				} else {
					e.emit(events.Event{Kind: events.Unattached, Obj: id,
						IDs:  []state.ObjID{o.AttachedTo},
						Text: "bearer left the battlefield"})
				}
				changed = true
				continue
			}
			if e.isAura(o) && (!e.auraStillMatchesEnchant(o, bearer) || e.protectedFrom(bearer.ID, o.ID)) {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id,
					From: state.ZBattlefield, To: state.ZGraveyard,
					Text: "attached to something it can no longer legally enchant"})
				changed = true
				continue
			}
			if e.isEquipment(o) && (bearer.ReconfiguredAttached() || !bearer.EffectiveIsCreature()) {
				// CR 702.150c: an attached Reconfigure card is not a creature,
				// so another Equipment riding it detaches like from any other
				// non-creature bearer (CR 301.5c / 704.5n).
				e.emit(events.Event{Kind: events.Unattached, Obj: id,
					IDs:  []state.ObjID{o.AttachedTo},
					Text: "Equipment bearer is no longer a creature"})
				changed = true
				continue
			}
			if e.isFortification(o) && !e.IsLand(bearer.ID) {
				// CR 704.5n's Fortification half (CR 702.67b): the bearer is no
				// longer a land, so the Fortification detaches and stays on the
				// battlefield. The read is the DERIVED type list -- a bearer that
				// lost its Land type to a layer-4 static is as illegal as one
				// printed without it.
				e.emit(events.Event{Kind: events.Unattached, Obj: id,
					IDs:  []state.ObjID{o.AttachedTo},
					Text: "Fortification bearer is no longer a land"})
				changed = true
			}
		}
	}
	e.sbaIDBuf = ids[:0]
	return changed
}

// auraStillMatchesEnchant reports whether bearer still satisfies the Aura's
// Enchant keyword's restriction (the "creature" of K:Enchant:Creature, or the
// more specific "Creature.YouCtrl" a real card spells; the first field after
// the colon is the spec, any later fields are the human prompt and dropped).
// The read is the DERIVED enchant (derivedKeywordParam: printed plus layer-6
// granted, minus layer-6 removed), not the printed face -- Animate Dead's
// RemoveKeywords$/Keywords$ pair rewrites the Enchant the SBA must honour, and
// a printed-only read would sweep the reanimated pair at the very next
// checkpoint. An Aura with no derived Enchant keyword is treated as
// still-matching so it is never spuriously destroyed; the intended side effect
// is that an Aura under a RemoveAbilities effect (Humility) -- which has no
// derived Enchant at all -- falls into that same still-matching arm, not a
// sweep. Nothing in the corpus prints an Aura without an Enchant keyword, so
// the arm only ever covers granted-absent shapes.
func (e *Engine) auraStillMatchesEnchant(o, bearer *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	param, ok := e.derivedKeywordParamH(o.ID, kwhEnchant)
	if !ok || strings.TrimSpace(param) == "" {
		return true
	}
	spec, _, _ := strings.Cut(param, ":")
	return e.matchesSpecFrom(strings.TrimSpace(spec), bearer.ID, o.Controller, o.ID)
}

// auraEnchantZoneAdmits is attachmentSBAs' off-battlefield-bearer exemption
// test: the Aura's CURRENT (derived) enchant spec matches the bearer AND
// carries an inZone<X> word whose X names the bearer's current zone. See
// attachmentSBAs for why the zone word is mandatory (a bare spec is
// zone-blind and would exempt ordinary dead-bearer Auras too).
func (e *Engine) auraEnchantZoneAdmits(o, bearer *state.Object) bool {
	param, ok := e.derivedKeywordParamH(o.ID, kwhEnchant)
	if !ok || strings.TrimSpace(param) == "" {
		return false
	}
	spec, _, _ := strings.Cut(param, ":")
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false
	}
	zoneNamed := false
	for word := range strings.SplitSeq(spec, ".") {
		z, has := strings.CutPrefix(word, "inZone")
		if !has {
			continue
		}
		if zn, known := effects.ParseZoneWord(z); known && zn == bearer.Zone {
			zoneNamed = true
		}
	}
	if !zoneNamed {
		return false
	}
	return e.matchesSpecFrom(spec, bearer.ID, o.Controller, o.ID)
}

func (e *Engine) playerAuraStillMatchesEnchant(o *state.Object, p state.PlayerID) bool {
	if o.Face() == nil {
		return false
	}
	param, ok := o.Face().KeywordParam("Enchant")
	if !ok {
		return false
	}
	spec, _, _ := strings.Cut(param, ":")
	return effects.MatchesPlayerSpecFrom(e.G, strings.TrimSpace(spec), p, o.Controller, o.ID)
}

// isAura reports whether a permanent has the Aura subtype.
func (e *Engine) isAura(o *state.Object) bool { return e.hasType(o, "Aura") }

// isEquipment reports whether a permanent has the Equipment subtype.
func (e *Engine) isEquipment(o *state.Object) bool { return e.hasType(o, "Equipment") }

// isFortification reports whether a permanent has the Fortification subtype.
func (e *Engine) isFortification(o *state.Object) bool { return e.hasType(o, "Fortification") }

// isRole reports whether a permanent has the Role subtype. The nine
// `.cards/tokenscripts/role_*.txt` scripts each print
// `Types:Enchantment Aura Role`, so a minted Role token carries the word;
// real printed Role cards do NOT (Forge omits the subtype there), which is
// recorded as an open issue -- the exclusivity sweep below keys on this
// predicate, so it only ever fires for minted Role tokens.
func (e *Engine) isRole(o *state.Object) bool { return e.hasType(o, "Role") }

// hasType is the rules-package subtype read, mirroring the effects-package
// hasType (effects/filter.go) off the battlefield: it scans the printed face.
// On the battlefield the read is typeCharacteristics -- the layer-4 walk's
// BASE plus changes -- because a permanent's current characteristics, not its
// printed face, are what CR 704.5m classifies. The measured case (CR 708.5
// with CR 704.5m): a face-down Gift of Doom is a vanilla 2/2 Creature while
// face down -- its printed Aura subtype does not exist -- so the unattached
// arm above must NOT sweep it to the graveyard as "Aura attached to
// nothing"; the printed read did exactly that, killing it at the next SBA
// checkpoint before its Morph turn-up cost could ever be paid. The same walk
// keeps every face-up read the printed face already answered (the fast path
// returns the base list when no layer-4 effect is active) and correctly
// follows a layer-4 type rewrite on a face-up permanent too. Faced-less
// objects have no types.
func (e *Engine) hasType(o *state.Object, t string) bool {
	if o == nil {
		return false
	}
	if o.Zone == state.ZBattlefield {
		for _, x := range e.typeCharacteristics(o.ID, 0) {
			if strings.EqualFold(x, t) {
				return true
			}
		}
		return false
	}
	f := o.Face()
	if f == nil {
		return false
	}
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}
