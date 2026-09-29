package rules

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The per-turn ledgers the "this turn" count heads read that the event log
// cannot answer after the fact: which activated abilities were activated
// (with the targets they chose -- the ability object's own Targets do not
// survive its resolution) and which seats committed a crime (CR 700.13 is
// judged at the choice; a later move of the target cannot un-commit it).
// Like combatHitsThisTurn they are engine-side NO-EVENT state: every rebuild
// (replay, undo, DVR, restart) re-executes emit and re-derives them, emit
// clears them on TurnChange, and Clone copies them.

// activationThisTurn is one activated ability put on the stack this turn.
type activationThisTurn struct {
	stack     state.ObjID
	source    state.ObjID
	activator state.PlayerID
	ab        *cards.SA
	targets   []state.Target
}

func cloneActivationsThisTurn(in []activationThisTurn) []activationThisTurn {
	if in == nil {
		return nil
	}
	out := make([]activationThisTurn, len(in))
	for i, v := range in {
		out[i] = v
		out[i].targets = slices.Clone(v.targets)
	}
	return out
}

// recordTurnLedgers folds one applied event into the per-turn ledgers.
// abilityMintWant is the object id an activation push was about to mint
// (zero for any other event).
func (e *Engine) recordTurnLedgers(stored events.Event, abilityMintWant state.ObjID) {
	if stored.Kind == events.ElementalBend && stored.Player >= 0 && stored.Player < 64 {
		bit := map[string]uint8{"water": 1, "earth": 2, "fire": 4, "air": 8}[strings.ToLower(stored.Text)]
		e.bendSeatsThisTurn[stored.Player] |= bit
	}
	if abilityMintWant != 0 {
		if o := e.G.Obj(abilityMintWant); o != nil && o.Zone == state.ZStack && o.Ability != nil {
			e.activationsThisTurn = append(e.activationsThisTurn, activationThisTurn{
				stack: o.ID, source: o.Source, activator: stored.Player, ab: o.Ability})
		}
	}
	if stored.Kind != events.TargetsChosen {
		return
	}
	o := e.G.Obj(stored.Obj)
	if o == nil {
		return
	}
	for i := range e.activationsThisTurn {
		if e.activationsThisTurn[i].stack == stored.Obj {
			e.activationsThisTurn[i].targets = slices.Clone(o.Targets)
		}
	}
	actor := e.controllerOf(stored.Obj)
	if actor >= 0 && actor < 64 && e.targetEventCommitsCrime(stored, actor) {
		e.crimeSeatsThisTurn |= 1 << uint(actor)
	}
}

// AllFourBendThisTurn reports whether p performed each of the four bends
// during the current turn.
func (e *Engine) AllFourBendThisTurn(p state.PlayerID) bool {
	return p >= 0 && p < 64 && e.bendSeatsThisTurn[p] == 15
}

// CommittedCrimeThisTurn reports whether p committed a crime this turn
// (Count$CommittedCrimeThisTurn, Seize the Secrets' cost reduction).
func (e *Engine) CommittedCrimeThisTurn(p state.PlayerID) bool {
	return p >= 0 && p < 64 && e.crimeSeatsThisTurn&(1<<uint(p)) != 0
}

// OpponentsAttackedThisTurn is the number of distinct opponents p declared
// an attack on this turn (PlayerCountPropertyYou$OpponentsAttackedThisTurn,
// Fast Forward). Only the active player attacks, so another seat reads 0.
// A DeclareAttackers event names the defending seat in Player and a battle
// or planeswalker defender in Obj; attacking a permanent is not attacking
// its controller. Derived from the event log like AttackersDeclaredThisTurn.
func (e *Engine) OpponentsAttackedThisTurn(p state.PlayerID) int32 {
	if e.G.Active != p {
		return 0
	}
	var seen uint64
	var n int32
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind != events.DeclareAttackers || ev.Obj != 0 || len(ev.IDs) == 0 ||
			ev.Player == p || ev.Player < 0 || ev.Player >= 64 {
			continue
		}
		if seen&(1<<uint(ev.Player)) == 0 {
			seen |= 1 << uint(ev.Player)
			n++
		}
	}
	return n
}

// AbilitiesActivatedThisTurnMatching answers Count$ThisTurnActivated_<spec>:
// this turn's activated abilities matching an `Activated.<props>` spec
// (comma alternatives, `+`-joined properties), plus the in-flight
// activation a cost static is pricing (inFlight on affected -- the Ctx's
// AffectedAbility/AffectedObj -- with inFlightTargets as its targets) -- Forge counts an activation before its
// cost is adjusted, which is what the corpus' LE1 "first activated ability
// each turn" gates read. ok=false for a spec this grammar cannot read.
func (e *Engine) AbilitiesActivatedThisTurnMatching(you state.PlayerID, affected state.ObjID, inFlight *cards.SA, inFlightTargets []state.Target, spec string) (int32, bool) {
	type act struct {
		source    state.ObjID
		activator state.PlayerID
		ab        *cards.SA
		targets   []state.Target
	}
	acts := make([]act, 0, len(e.activationsThisTurn)+1)
	pushed := false
	for _, r := range e.activationsThisTurn {
		acts = append(acts, act{r.source, r.activator, r.ab, r.targets})
		if e.cast != nil && e.cast.isAbility() && e.cast.stackObj == r.stack {
			pushed = true
		}
	}
	if inFlight != nil && !pushed {
		if o := e.G.Obj(affected); o != nil {
			acts = append(acts, act{affected, o.Controller, inFlight, inFlightTargets})
		}
	}
	var n int32
	for _, a := range acts {
		hit := false
		for alt := range strings.SplitSeq(spec, ",") {
			m, ok := e.activationMatches(strings.TrimSpace(alt), a.source, a.activator, a.ab, a.targets, you)
			if !ok {
				return 0, false
			}
			if m {
				hit = true
			}
		}
		if hit {
			n++
		}
	}
	return n, true
}

// activationMatches evaluates one `Activated[.<p>+<p>...]` alternative
// against an activation. The properties: YouCtrl/OppCtrl (the activator),
// IsTargeting Valid <spec> (`~` standing for `+` inside the nested spec), a
// card type word or a card predicate the filter grammar knows (read off the
// ability's SOURCE -- Tezzeret's Artifact+inZoneBattlefield), and otherwise
// an ability property abilityConstraintMatches reads (Equip/Cycling/...
// Keyword$ tags, Loyalty, ManaAbility, the param-backed flags).
func (e *Engine) activationMatches(alt string, source state.ObjID, activator state.PlayerID, ab *cards.SA, targets []state.Target, you state.PlayerID) (bool, bool) {
	kind, props, _ := strings.Cut(alt, ".")
	if kind != "Activated" {
		return false, false
	}
	if ab == nil {
		return false, true
	}
	sc := e.specCtx(source, you)
	for prop := range strings.SplitSeq(props, "+") {
		prop = strings.TrimSpace(prop)
		switch {
		case prop == "":
			continue
		case prop == "YouCtrl":
			if activator != you {
				return false, true
			}
		case prop == "OppCtrl" || prop == "YouDontCtrl":
			if activator == you {
				return false, true
			}
		case strings.HasPrefix(prop, "IsTargeting"):
			rest := strings.TrimSpace(strings.TrimPrefix(prop, "IsTargeting"))
			inner, ok := strings.CutPrefix(rest, "Valid ")
			if !ok {
				return false, false
			}
			inner = strings.ReplaceAll(strings.TrimSpace(inner), "~", "+")
			if len(effects.UnknownPredicates(inner)) > 0 {
				return false, false
			}
			found := false
			for _, t := range targets {
				if t.IsPlayer {
					if effects.MatchesPlayerSpec(e.G, inner, t.Player, you) {
						found = true
					}
				} else if e.matchesSpec(inner, t.Obj, sc) {
					found = true
				}
			}
			if !found {
				return false, true
			}
		case len(effects.UnknownPredicates("Card."+prop)) == 0 || effects.CardTypeWord(prop):
			spec := "Card." + prop
			if effects.CardTypeWord(prop) {
				spec = prop
			}
			if !e.matchesSpec(spec, source, sc) {
				return false, true
			}
		default:
			if !e.abilityConstraintMatches(abilityScope(ab), you, source, prop) {
				return false, true
			}
		}
	}
	return true, true
}
