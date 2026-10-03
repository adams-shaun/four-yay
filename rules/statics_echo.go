package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// panharmoniconEchoes reports how many ADDITIONAL trigger placements the
// battlefield's stat:Panharmonicon statics demand for the trigger the EVENT
// ev just caused on source object src (CR 702.109: "that ability triggers an
// additional time"). One echo per matching static, in activeStatics'
// deterministic scan order (which never matters here -- the echoes are summed
// -- but keeps the call off any map range).
//
// A static must match the triggering CAUSE, not just the triggering
// permanent, before it doubles anything. The gate reads the corpus's own
// parameter set, each optional, every one present needing to match:
//
//   - ValidMode$: the comma-separated Forge event-mode family the causing
//     event belongs to (panharmoniconModes); a static naming modes the event
//     is not in contributes nothing.
//   - ValidCause$: the event's causing OBJECT -- the moved/cast card for a
//     zone change or cast, the single attacker of a one-attacker declare
//     (a multi-attacker event has no singular cause and fails closed, the
//     same convention triggerReferents' Attacks case applies) -- matched as
//     a filter spec. Origin$/Destination$ scope a ChangesZone cause to the
//     move's own zones exactly as the trigger grammar does.
//   - ValidSource$: the in-flight damage source (e.damaging -- combat
//     damage's dealing creature; absent for any other event), a filter spec.
//   - ValidTarget$: the event's target -- the damage recipient (object or
//     player, whichever the event carries) or the BecomesTarget-ed object
//     (the trigger's own source), a filter spec or player spec.
//   - ValidActivator$: the cast's player, a player spec.
//   - ValidPlayer$: the event's player (the life gainer, the drawing
//     player), a player spec.
//   - CombatDamage$ True: the in-flight damage must be the combat damage
//     step's own assignment (the combatDamaging flag); False is the default
//     and matches either.
//
// ValidZone$ (Echoes of Eternity's "Battlefield,Stack") is honoured as a
// filter on the triggering object's own zone; a static naming zones the
// object is not in contributes nothing. A static with no ValidCard$
// contributes nothing rather than matching everything -- a Panharmonicon
// static that does not say WHAT it doubles is a script defect this build
// will not paper over by doubling every trigger on the board. ValidTurned$
// (Panoptic Projektor's "if turning a face-down permanent face up ...", the
// corpus's one carrier) scopes the permanent that was turned up, matched
// against the turn-up event's own object. Params this build cannot evaluate
// (IsPresent/PresentCompare, Condition) fail closed: the static does not
// double, never over-applies.
func (e *Engine) panharmoniconEchoes(observer *Engine, src state.ObjID, ev events.Event) int {
	g := observer.G
	o := g.Obj(src)
	if o == nil {
		return 0
	}
	modes := panharmoniconModes(ev)
	n := 0
	for _, sv := range e.activeStatics("Panharmonicon") {
		if vm := sv.ParamStr(cards.PKValidMode); vm != "" {
			ok := false
			for want := range strings.SplitSeq(vm, ",") {
				want = strings.TrimSpace(want)
				for _, have := range modes {
					if want == have {
						ok = true
						break
					}
				}
				if ok {
					break
				}
			}
			if !ok {
				continue
			}
		}
		if vz := sv.ParamStr(cards.PKValidZone); vz != "" {
			zones, all, valid := effects.ParseZones(vz)
			ok := false
			if valid || all {
				for _, z := range zones {
					if z == o.Zone {
						ok = true
						break
					}
				}
			}
			if !ok {
				continue
			}
		}
		// ValidCause$: the causing object per event family. A zone change or
		// cast's cause is the moved/cast card; an attack's is the single
		// declared attacker (a multi-attacker event has no singular cause);
		// a damage event's cause is not modelled -- fail closed.
		if spec := sv.ParamStr(cards.PKValidCause); spec != "" {
			cause := state.ObjID(0)
			switch ev.Kind {
			case events.MoveZone, events.Draw, events.PutOnStack:
				cause = ev.Obj
			case events.DeclareAttackers:
				if len(ev.IDs) == 1 {
					cause = ev.IDs[0]
				}
			}
			if cause == 0 || !observer.matchesSpecFrom(spec, cause, sv.Controller, sv.Source) {
				continue
			}
		}
		// Origin$ is a zone SET (single name or comma list); Any/All is a
		// wildcard and an unknown token fails closed.
		if o := sv.ParamStr(cards.PKOrigin); o != "" {
			zones, all, listOK := effects.ParseZones(o)
			if !listOK || (!all && !zoneIn(ev.From, zones)) {
				continue
			}
		}
		if sv.ParamStr(cards.PKDestination) != "" && effects.ParseZone(sv.ParamStr(cards.PKDestination)) != ev.To {
			continue
		}
		if spec := sv.ParamStr(cards.PKValidSource); spec != "" {
			if ev.Kind != events.Damage || e.damaging == 0 ||
				!observer.matchesSpecFrom(spec, e.damaging, sv.Controller, sv.Source) {
				continue
			}
		}
		if spec := sv.ParamStr(cards.PKValidTarget); spec != "" {
			switch {
			case ev.Kind == events.Damage && ev.Obj != 0:
				if !observer.matchesSpecFrom(spec, ev.Obj, sv.Controller, sv.Source) {
					continue
				}
			case ev.Kind == events.Damage:
				if !effects.MatchesPlayerSpec(g, spec, ev.Player, sv.Controller) {
					continue
				}
			case ev.Kind == events.TargetsChosen:
				// The BecomesTarget-ed object is the trigger's own source.
				if !observer.matchesSpecFrom(spec, src, sv.Controller, sv.Source) {
					continue
				}
			default:
				continue
			}
		}
		if spec := sv.ParamStr(cards.PKValidActivator); spec != "" {
			if ev.Kind != events.PutOnStack || !effects.MatchesPlayerSpec(g, spec, ev.Player, sv.Controller) {
				continue
			}
		}
		if spec := sv.ParamStr(cards.PKValidPlayer); spec != "" {
			if (ev.Kind != events.LifeChange && ev.Kind != events.Draw) ||
				!effects.MatchesPlayerSpec(g, spec, ev.Player, sv.Controller) {
				continue
			}
		}
		if sv.ParamStr(cards.PKCombatDamage) == "True" && !(ev.Kind == events.Damage && e.combatDamaging) {
			continue
		}
		if spec := sv.ParamStr(cards.PKValidTurned); spec != "" {
			// ValidTurned$ scopes the permanent that was turned face up
			// (Panoptic Projektor: "if turning a face-down permanent face up
			// causes ..."). It is a PAIR with ValidMode$ TurnFaceUp, and the
			// turned object is the turn-up event's own Obj -- distinct from
			// ValidCard$, which scopes the trigger's source. An event with no
			// turned permanent carries nothing the clause can match, so a
			// non-turn-up event fails closed rather than doubling.
			if ev.Kind != events.TurnFaceUp || ev.Obj == 0 ||
				!observer.matchesSpecFrom(spec, ev.Obj, sv.Controller, sv.Source) {
				continue
			}
		}
		spec := sv.ParamStr(cards.PKValidCard)
		if spec == "" {
			continue
		}
		if observer.matchesSpecFrom(spec, src, sv.Controller, sv.Source) {
			n++
		}
	}
	return n
}

// panharmoniconModes maps the folded causing event to the Forge trigger-mode
// family it can serve as a triggering event for -- the same event-to-mode
// correspondence trigger_match.go's per-mode matchers test one mode at a
// time, stated once here for a ValidMode$ list to check against. A MoveZone
// with the card played from hand also serves LandPlayed (trigmatch.landPlayedMatches
// matches the same event), the way the corpus spells multi-family statics.
func panharmoniconModes(ev events.Event) []string {
	switch ev.Kind {
	case events.MoveZone:
		out := []string{"ChangesZone", "ChangesZoneAll"}
		if ev.From == state.ZHand && ev.To == state.ZBattlefield {
			out = append(out, "LandPlayed")
		}
		return out
	case events.Draw:
		return []string{"Drawn"}
	case events.PutOnStack:
		return []string{"SpellCast", "SpellCastOrCopy"}
	case events.StackCopy:
		return []string{"SpellCopy", "SpellCastOrCopy"}
	case events.DeclareAttackers:
		return []string{"Attacks", "AttackersDeclared", "AttackersDeclaredOneTarget"}
	case events.Damage:
		return []string{"DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll"}
	case events.TargetsChosen:
		return []string{"BecomesTarget", "BecomesTargetOnce"}
	case events.LifeChange:
		if ev.Amount > 0 {
			return []string{"LifeGained"}
		}
		return []string{"LifeLost"}
	case events.TurnFaceUp:
		// The turn-up marker (CR 708.6/702.36e). Only ValidMode$
		// TurnFaceUp statics -- Panoptic Projektor's "if turning a face-down
		// permanent face up causes a triggered ability ... to trigger" --
		// serve this family.
		return []string{"TurnFaceUp"}
	default:
		return nil
	}
}

// unspentManaKeep renders the pool slots whose unspent mana the CR 500.4
// boundary ManaClear must NOT empty for player p — the stat:UnspentMana
// continuous static ("You don't lose unspent <colour> mana as steps and
// phases end", Leyline Tyrant / Omnath, Locus of Mana / Upwelling). The
// answer is one letter per protected slot in fixed WUBRGC slot order
// ("R", "WU", ...); "" means every slot empties, so a game without a live
// carrier emits the historical Text-less ManaClear byte-identically.
//
// Both delivery routes are read, the same pair SacrificeBlocked walks:
// the printed S: face statics (activeStatics) and the Effect-delivered
// registration (effEffect's UnspentMana arm — The Last Agni Kai's
// `DB$ Effect | StaticAbilities$ Unspent`), the latter through the ordinary
// active() lifetime machinery so the grant ends with its source or its
// duration. A face static's "as long as" gates run through the shared
// continuousGateHolds grammar; ValidPlayer$ goes through the shared player
// spec matcher (You scopes to the static's controller, absent — Upwelling —
// protects every seat). ManaType$ is a comma-separated colour-word list
// parsed by effects.ColorLetters; absent (or an unparseable "Colorless"
// word set, the C slot) protects every slot only when the parameter is
// wholly absent. An unresolvable ManaType$ value fails closed and protects
// nothing — the shipped-statics convention.
func (e *Engine) unspentManaKeep(p state.PlayerID) string {
	keep := make([]bool, len(e.G.Players[p].Pool))
	apply := func(params map[string]string, you state.PlayerID) {
		if spec := params["ValidPlayer"]; spec != "" &&
			!effects.MatchesPlayerSpec(e.G, spec, p, you) {
			return
		}
		mt := strings.TrimSpace(params["ManaType"])
		if mt == "" {
			for i := range keep {
				keep[i] = true
			}
			return
		}
		letters, ok := effects.ColorLetters(mt)
		if !ok {
			return
		}
		if len(letters) == 0 {
			// "Colorless" — the C slot alone.
			keep[len(keep)-1] = true
			return
		}
		for _, l := range letters {
			keep[state.ManaIndex(l[0])] = true
		}
	}
	for _, sv := range e.activeStatics("UnspentMana") {
		if !e.continuousGateHolds(sv) {
			continue
		}
		apply(sv.Params, sv.Controller)
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "UnspentMana" {
			continue
		}
		apply(ce.RestrictParams, ce.Controller)
	}
	var out []byte
	for i, k := range keep {
		if k {
			out = append(out, manaSlotSymbols[i])
		}
	}
	return string(out)
}
