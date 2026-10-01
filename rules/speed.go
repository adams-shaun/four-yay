package rules

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// speed.go implements Start your engines! (CR 702.179). The static ability
// sets speed to 1 (702.179a); a player with speed has a source-less inherent
// trigger on opponents' life loss DURING THEIR TURN (702.179d). The trigger
// uses the ordinary APNAP queue and stack, and can trigger only once a turn.
// Speed 4 is max speed (702.179e).

const speedTrigger = "__speed_increase"

// maxSpeed is CR 702.179e.
const maxSpeed = 4

// checkSpeedGain queues the inherent trigger after a folded opponent life
// loss. Both the pending queue and the logged push count as having triggered:
// countering the stack ability cannot permit another trigger this turn.
func (e *Engine) checkSpeedGain(ev events.Event) {
	if e.G.Over {
		return
	}
	p := e.G.Active
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost || p == ev.Player ||
		e.G.Players[p].Speed == 0 || e.G.Players[p].Speed >= maxSpeed ||
		e.speedTriggeredThisTurn(p) {
		return
	}
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{Controller: p, SpeedIncrease: true})
}

// checkSpeedStart is the permanent half of the Start your engines! grant:
// called from Engine.emit's post-fold hook on every battlefield entry
// (MoveZone), token mint (TokenCreate, CardToken) and control transfer
// (ControlChange) -- the same hook shape checkBlessingGrants uses. For each
// still-alive speed-less seat it checks CR 702.179a's gate directly off the
// FOLDED state: the seat controls at least one permanent whose derived
// keywords include Start your engines!. It then emits the one-way
// SpeedChange latch to 1. Running after the fold means a permanent's own
// arrival grants its controller, and the recursive emit the scan makes sees
// a seat that now has speed, so the scan terminates. A control transfer of a
// permanent already on the battlefield is exactly the case a battlefield-
// entry-only hook cannot see, which is why ControlChange is in the set.
func (e *Engine) checkSpeedStart() {
	if e.G.Over {
		return
	}
	for _, p := range e.G.AliveFrom(0) {
		if int(p) >= len(e.G.Players) || e.G.Players[p].Lost || e.G.Players[p].Speed != 0 {
			continue
		}
		started := false
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
				continue
			}
			if e.hasKeywordH(id, kwhStartYourEngines) {
				started = true
				break
			}
		}
		if started {
			e.emit(events.Event{Kind: events.SpeedChange, Player: p, Amount: 1,
				Text: "start your engines"})
		}
	}
}

// speedTriggeredThisTurn counts the trigger, not its resolution. A queued
// trigger has not yet been logged; once pushed its DelayedPush records it even
// if countered. TurnChange bounds the scan without replay-only live counters.
func (e *Engine) speedTriggeredThisTurn(p state.PlayerID) bool {
	for _, pt := range e.pendingTriggers {
		if pt.SpeedIncrease && pt.Controller == p {
			return true
		}
	}
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.DelayedPush && ev.Player == p && ev.Counter == speedTrigger {
			return true
		}
	}
	return false
}

// maxSpeedAbilities collects the abilities a max-speed static (CR 702.179e,
// "Max speed — ...") grants a permanent whose controller HAS max speed. The
// shape is `S:Mode$ Continuous | Affected$ Card.Self | Condition$ MaxSpeed |
// AddAbility$ <svar>` (Amonkhet Raceway): the granted ability is the SVar the
// AddAbility$ names, an ordinary AB$ line read off the face's SVar table. Any
// other Condition$ value (or none) contributes nothing: a condition this
// build cannot evaluate is not silently treated as always-on.
func (e *Engine) maxSpeedAbilities(p state.PlayerID, id state.ObjID) []*cards.SA {
	if e.G.Players[p].Speed < maxSpeed {
		return nil
	}
	o := e.G.Obj(id)
	if o == nil {
		return nil
	}
	f := o.Face()
	if f == nil {
		return nil
	}
	var out []*cards.SA
	for _, st := range f.Statics {
		if st.Mode != "Continuous" || st.Params["AddAbility"] == "" {
			continue
		}
		if st.Params["Condition"] != "MaxSpeed" {
			continue
		}
		if ab := cards.ResolveSVar(f.SVars, st.Params["AddAbility"]); ab != nil && ab.Kind == "AB" {
			out = append(out, ab)
		}
	}
	return out
}

// beginGainedActivation starts a has-all-abilities-of activation (Forge's
// GainsAbilitiesOf$, rules/legal.go's gained offer loop): the body is a
// compiled AB$ SA on the foreign card's own face, named by
// (opt.GainedSource, opt.GainedIdx), so it resolves directly off that face
// instead of the SVar anchor beginGrantedActivation uses. Every later stage
// is the shared activation flow -- cost parse, ReduceCost fold, targeting,
// payment, then events.GainedAbilityPush mints the same SA on the stack. A
// foreign card that left the scoped zone, or a stale index, degrades to a
// no-op (a stale option always has).
func (e *Engine) beginGainedActivation(p state.PlayerID, opt decision.Option) {
	o := e.G.Obj(opt.Obj)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return
	}
	fo := e.G.Obj(opt.GainedSource)
	if fo == nil || fo.Face() == nil {
		return
	}
	abilities := fo.Face().Abilities
	if opt.GainedIdx < 0 || opt.GainedIdx >= len(abilities) {
		return
	}
	ab := abilities[opt.GainedIdx]
	if ab == nil {
		return
	}
	cost, ok := e.fixLifeXCost(p, opt.Obj, e.parseCost(ab.Params["Cost"]))
	if !ok {
		return
	}
	// The gained twin of the printed loop's own ReduceCost$ fold: targets do
	// not exist yet (CR 601.2c runs later), so a target-dependent body reads
	// 0 here and repriceForTargets re-runs the evaluation.
	own := e.ownReduceCost(p, opt.Obj, ab, nil, nil, 0)
	if own > 0 {
		if cost.Generic >= own {
			cost.Generic -= own
		} else {
			cost.Generic = 0
		}
	}
	mods := e.costModifiers(p, opt.Obj, abilityScope(ab))
	e.cast = e.newCast(p, opt.Obj, o.Zone, "", -1)
	e.cast.gainedFrom, e.cast.gainedIdx, e.cast.cost, e.cast.mods, e.cast.ownReduce = opt.GainedSource, opt.GainedIdx, cost, mods, own
	e.continueCast()
}

// beginGrantedActivation activates an SVar-anchored granted ability: the
// max-speed static's "granted" option (rules/speed.go's own offer) and the
// AddAbilities grant's "ability" option (rules/legal.go, beginActivation's
// SVar branch) both route here. Task grantcost1: the activation is routed
// through the SAME cast flow a printed activated ability uses
// (pendingCast/continueCast/payCast) instead of a bespoke mana-only payment,
// so every non-mana cost part (Sac, Discard, SubCounter, AddCounter, Exile,
// Draw, Return, PayEnergy, Behold, Blight, Forage, ...) is asked and paid
// exactly the way a printed ability's is (CR 602.2b -> 601.2h), with the
// targets chosen before anything is paid (601.2c) and the CR 601.2g mana
// window opening when the floating pool alone cannot pay. The pendingCast
// carries the grant anchor (grantSource/grantSVar; ability stays -1), and
// payCast's ability branch mints through the same two events this function
// always minted: DelayedPush for a self-grant (Counter carries the SVar),
// GrantAbilityPush for a cross-object grant (IDs[0] carries the grantor; the
// minted ability's Source is the recipient -- so `Defined$ Self`/`CARDNAME`
// in the body names the recipient, correctly, in the ~35% of carriers that
// read it. Never DelayedPush for a cross-grant: its Apply case resolves from
// e.Obj, the recipient's face, which has no such SVar). A stale option
// degrades to a no-op.
func (e *Engine) beginGrantedActivation(p state.PlayerID, opt decision.Option) {
	o := e.G.Obj(opt.Obj)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return
	}
	// The body resolves from the GRANTOR (a printed static's own permanent),
	// never from the recipient: Opt.GrantSource is set by the granted-ability
	// offer loop, and a zero value means a self-grant (the max-speed static,
	// an Animate) where the two objects are the same. Falling back to opt.Obj
	// keeps every existing self-grant path byte-identical.
	grantor := opt.GrantSource
	if grantor == 0 {
		grantor = opt.Obj
	}
	// The body is resolved through grantedSAFrom, which walks the grantor's
	// whole pile top-first (CR 702.140d): a granting static may sit on a
	// mutated pile's UNDER-CARD, and legal.go's grantedAbilities already
	// offers such a grant off that face's own SVar table, so resolving only
	// the grantor's active face here would no-op an option the offer loop
	// legally produced. A non-mutated grantor resolves exactly as before.
	// Note this is a NAME anchor, not the flat pile-ability index: a granted
	// activation carries ability == -1 and decodes no index at all.
	ab := e.grantedSAFrom(grantor, opt.Obj, opt.SVar)
	if ab == nil {
		return
	}
	cost, ok := e.fixLifeXCost(p, opt.Obj, e.parseCost(ab.Params["Cost"]))
	if !ok {
		return
	}
	bindGrantedCostReferents(&cost, grantor)
	// The granted twin of the printed loop's own ReduceCost$ fold (the offer
	// gate composed the same reduction): Targets do not exist yet (CR 601.2c
	// runs later), so a target-dependent body reads 0 here and
	// repriceForTargets re-runs the evaluation with the answered targets.
	// merged 0: the ReduceCost$ SVar body is read off the RECIPIENT's table,
	// and the granted offer gate (legal.go's granted arm) prices it against
	// the recipient's top face too -- offer and activation must charge the
	// same reduction.
	own := e.ownReduceCost(p, opt.Obj, ab, nil, nil, 0)
	if own > 0 {
		if cost.Generic >= own {
			cost.Generic -= own
		} else {
			cost.Generic = 0
		}
	}
	mods := e.costModifiers(p, opt.Obj, abilityScope(ab))
	e.cast = e.newCast(p, opt.Obj, o.Zone, "", -1)
	e.cast.grantSVar, e.cast.grantSource, e.cast.cost, e.cast.mods, e.cast.ownReduce = opt.SVar, grantor, cost, mods, own
	e.continueCast()
}

// abSVarName returns the SVar table key whose raw body is exactly the line

// beginKeywordGrantedActivation activates a keyword-GRANTED ability (CR
// 613.1f): the layer-6 AddKeyword$ option (rules/legal.go's granted-keyword
// offer) anchors the DERIVED keyword line ("Cycling:1 U",
// "TypeCycling:Sliver:3", "Saddle:2", "Crew:1") rather than a face index or
// SVar name -- a granted keyword lives in no face's SVar table. The body is
// synthesized from the line (cards.GrantedKeywordAbility, the same synthesis
// the offer gate priced), the pendingCast carries the line as its
// grantKeyword anchor, and payCast's ability branch mints through
// events.KeywordAbilityPush, whose Counter carries the same line -- so the
// resolution, the replay and the cycling-provenance tag
// (rules/cast.go cyclingKeyword over pcAbility) all re-derive the identical
// body. A stale option (a line no synthesizer can model) degrades to a
// no-op. The grant itself is NOT re-checked here: the synthesized body is a
// pure function of the line the option was offered with, the same way an
// SVar-granted body survives a grantor's exit through grantedSAFrom's
// fallback, and the CR 601.2e recheck still gates the payment.
func (e *Engine) beginKeywordGrantedActivation(p state.PlayerID, opt decision.Option) {
	o := e.G.Obj(opt.Obj)
	if o == nil || o.Face() == nil {
		return
	}
	ab := cards.GrantedKeywordAbility(opt.Keyword)
	if ab == nil {
		return
	}
	cost, ok := e.fixLifeXCost(p, opt.Obj, e.parseCost(ab.Params["Cost"]))
	if !ok {
		return
	}
	// The granted twin of the printed loop's own ReduceCost$ fold: targets do
	// not exist yet (CR 601.2c runs later), so a target-dependent body reads
	// 0 here and repriceForTargets re-runs the evaluation. merged 0: the
	// synthesized body carries no target-dependent SVar of its own -- the
	// fold is structurally zero for every cycling body and kept only so the
	// offer gate and this charge share one composition.
	own := e.ownReduceCost(p, opt.Obj, ab, nil, nil, 0)
	if own > 0 {
		if cost.Generic >= own {
			cost.Generic -= own
		} else {
			cost.Generic = 0
		}
	}
	mods := e.costModifiers(p, opt.Obj, abilityScope(ab))
	e.cast = e.newCast(p, opt.Obj, o.Zone, "", -1)
	e.cast.grantKeyword, e.cast.cost, e.cast.mods, e.cast.ownReduce = opt.Keyword, cost, mods, own
	e.continueCast()
}

// abSVarName returns the SVar table key whose raw body is exactly the line
// ab was parsed from, in first-match order over the face's SVar table. The
// table is a Go map, so this walk sorts the keys first (determinism rule: no
// map range may reach an option list) -- and the corpus is the guarantee
// there IS an answer: every AddAbility$ value names a key whose body is
// exactly that AB.
func abSVarName(f *cards.Face, ab *cards.SA) string {
	keys := make([]string, 0, len(f.SVars))
	for k := range f.SVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if f.SVars[k] == ab.Line {
			return k
		}
	}
	return ""
}
