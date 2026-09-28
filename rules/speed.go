package rules

import (
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// speed.go implements the "Start your engines!" mechanic (CR 702.163; 47
// corpus files carry the keyword). Speed is a per-seat count
// (state.Player.Speed) that never resets; max speed is 4. Two engine rules:
//
//   - CR 702.163a: while a player controls a permanent with "Start your
//     engines!", if they have no speed (0), it becomes 1. Emitted from the
//     one emit path when such a permanent ENTERS the battlefield under them
//     (checkSpeedStart, called from Engine.emit's MoveZone fold).
//   - CR 702.163b: a player's speed increases once each turn when an
//     opponent loses life, up to max speed -- including another player's
//     turn. A player with NO speed (0 -- no Start your engines! permanent
//     yet) does not gain: the increase rule is speed's own, and it applies
//     to players who have speed. Emitted after the folded LifeChange loss
//     (checkSpeedGain).
//
// The once-per-turn gate is derived from the event log (the SpeedChange
// events already emitted this turn), never from a live counter, so a replay
// re-executes to the same gains. Max speed is a cap: a seat at 4 gains
// nothing further, and events.Apply clamps defensively too.

// maxSpeed is CR 702.163b: speed cannot go above 4.
const maxSpeed = 4

// checkSpeedGain is called from Engine.emit after every folded LifeChange
// with a negative Amount (a loss) and after every positive player-D Damage
// event (combat and spell/ability damage fold straight to the life total,
// and a Damage event that reaches emit has already been through prevention
// -- a prevented hit is a Note, never a Damage -- so a positive player-arm
// Damage event IS a landed loss). Every living player WITH speed gains one
// speed when an opponent of theirs lost the life (any other seat -- CR
// 800.4k: in a free-for-all every other player is an opponent); at most one
// gain per turn, and never past max speed. More than one player may qualify
// for the same loss on another player's turn. The gain itself is an ordinary
// event (re-entrant emit: a SpeedChange triggers nothing).
func (e *Engine) checkSpeedGain(ev events.Event) {
	if e.G.Over {
		return
	}
	loser := ev.Player
	for _, p := range e.G.AliveFrom(0) {
		if p == loser {
			// The active player losing their own life is not "an opponent
			// loses life".
			continue
		}
		if e.G.Players[p].Speed == 0 {
			// CR 702.163a: a player with NO speed (no Start your engines!
			// permanent yet) does not take the increase; the increase rule
			// governs speed a player HAS.
			continue
		}
		if e.G.Players[p].Speed >= maxSpeed {
			continue
		}
		if e.speedGainedThisTurn(p) {
			continue
		}
		e.emit(events.Event{Kind: events.SpeedChange, Player: p, Amount: 1,
			Text: "speed"})
	}
}

// checkSpeedStart is called from Engine.emit after a battlefield-entry Move
// whose object carries kw:Start your engines: if its controller has no speed,
// it becomes 1 (CR 702.163a). A controller at speed 1..4 is untouched.
func (e *Engine) checkSpeedStart(id state.ObjID) {
	if e.G.Over {
		return
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return
	}
	if !e.hasKeywordH(id, kwhStartYourEngines) {
		return
	}
	p := o.Controller
	if int(p) >= len(e.G.Players) || e.G.Players[p].Lost || e.G.Players[p].Speed != 0 {
		return
	}
	e.emit(events.Event{Kind: events.SpeedChange, Player: p, Amount: 1,
		Text: "start your engines"})
}

// speedGainedThisTurn reports whether p already took the TURN'S INCREASE
// since the turn began: the latest TurnChange in the log bounds the scan,
// exactly the way castThisTurn and drawsThisTurn derive their per-turn
// counts. Only gains with Text "speed" count: the "start your engines"
// grant (CR 702.163a, speed starting at 1 when a first Start your engines!
// permanent enters) is a distinct rule from the turn's increase (CR
// 702.163b), so the turn the first permanent enters still earns its
// increase when an opponent loses life -- two events, both SpeedChange,
// distinguished by the Text the two emit sites write.
func (e *Engine) speedGainedThisTurn(p state.PlayerID) bool {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			return false
		}
		if ev.Kind == events.SpeedChange && ev.Player == p && ev.Amount > 0 && ev.Text == "speed" {
			return true
		}
	}
	return false
}

// maxSpeedAbilities collects the abilities a max-speed static (CR 702.163c,
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
	e.cast = &pendingCast{player: p, card: opt.Obj, from: o.Zone, ability: -1,
		gainedFrom: opt.GainedSource, gainedIdx: opt.GainedIdx, cost: cost, mods: mods, ownReduce: own}
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
	e.cast = &pendingCast{player: p, card: opt.Obj, from: o.Zone, ability: -1,
		grantSVar: opt.SVar, grantSource: grantor, cost: cost, mods: mods, ownReduce: own}
	e.continueCast()
}

// abSVarName returns the SVar table key whose raw body is exactly the line

// beginKeywordGrantedActivation activates a keyword-GRANTED ability (CR
// 613.1f): the layer-6 AddKeyword$ Cycling/TypeCycling option
// (rules/legal.go's keyword-cycling offer) anchors the DERIVED keyword line
// ("Cycling:1 U", "TypeCycling:Sliver:3") rather than a face index or SVar
// name -- a granted keyword lives in no face's SVar table. The body is
// synthesized from the line (cards.GrantedCyclingAbility, the same synthesis
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
	ab := cards.GrantedCyclingAbility(opt.Keyword)
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
	e.cast = &pendingCast{player: p, card: opt.Obj, from: o.Zone, ability: -1,
		grantKeyword: opt.Keyword, cost: cost, mods: mods, ownReduce: own}
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
