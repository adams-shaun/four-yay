package rules

// Grant-delivered cost-modifier statics (the census's "delivery" class): a
// Mode$ ReduceCost/RaiseCost/SetCost body an object GAINS -- through an
// Animate/AnimateAll staticAbilities$, a Mode$ Continuous AddStaticAbility$
// or a CopyPermanent AddStaticAbilities$ -- must price the real offer and
// the real payment exactly as a printed static on that object would, for as
// long as the grant lives, and not a moment longer.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cgPlace puts c onto p's battlefield through a real MoveZone (so every
// log-head-keyed cache sees it) and settles whatever the entry queues.
func cgPlace(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	cgSettle(t, e)
	return o.ID
}

func cgSettle(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) == 0 {
			return
		}
		e.resolveTop()
	}
	t.Fatalf("board did not settle: %d pending, %d on the stack", len(e.pendingTriggers), len(e.G.Stack))
}

// cgResolveSVar resolves the named SVar body off src's face as src's
// controller would, with the given Remembered set.
func cgResolveSVar(t *testing.T, e *Engine, src state.ObjID, name string, remembered ...state.ObjID) {
	t.Helper()
	sa := resolveSourceFaceSA(t, e, src, name)
	ctx := &effects.Ctx{Source: src, Controller: e.G.Obj(src).Controller, SVars: e.G.Obj(src).Face().SVars}
	for _, id := range remembered {
		ctx.Remembered = append(ctx.Remembered, state.Target{Obj: id})
	}
	effects.Resolve(e, ctx, sa)
}

// cgCastTargeting casts spell (seat 0) at target through the real cast flow
// and returns how much of the pool the cast left.
func cgCastTargeting(t *testing.T, e *Engine, spell, target state.ObjID) int32 {
	t.Helper()
	castMode(t, e, spell, "")
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("cast stalled with no decision; spell in %s", e.G.Obj(spell).Zone)
		}
		if d.Kind == decision.KPriority {
			break
		}
		pick := -1
		for _, opt := range d.Options {
			if opt.Obj == target {
				pick = opt.Index
			}
		}
		if pick < 0 {
			pick = d.Options[0].Index
		}
		submitChoices(t, e, pick)
	}
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("spell in %s after cast, want stack; pending %+v", z, e.Pending())
	}
	return e.G.Players[0].Pool.Total()
}

const cgBear = "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const cgSorcery = "Name:Test Growth\nManaCost:1 G\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"

// Chronicler of Worship: "It perpetually gains 'This spell costs {1} less to
// cast.'" -- an api:Animate staticAbilities$ grant on a card in HAND. With
// only {G} floating, the {1}{G} card must be offered and cast for {G}; the
// grant is perpetual, so it survives the cleanup step.
func TestCostGrantAnimatePerpetualReducesHandCard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, cgBear))
	bear := e.G.Zone(state.ZHand, 0)[0]
	chron := cgPlace(t, e, 0, corpusAlternativeCard(t, "Chronicler of Worship"))
	e.G.Players[0].Pool[state.MG] = 1
	if hasCastOption(e.legalActions(0), bear) {
		t.Fatalf("precondition: {1}{G} bear offered with only {G} before any grant")
	}
	cgResolveSVar(t, e, chron, "DBAnimate", bear)
	if !hasCastOption(e.legalActions(0), bear) {
		t.Fatalf("bear not offered with {G} after Chronicler's perpetual {1} reduction")
	}
	e.EndOfTurnCleanup()
	if !hasCastOption(e.legalActions(0), bear) {
		t.Fatalf("perpetual reduction lost at cleanup")
	}
	castMode(t, e, bear, "")
	if z := e.G.Obj(bear).Zone; z != state.ZStack {
		t.Fatalf("bear in %s after cast, want stack; pending %+v", z, e.Pending())
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0 (charged {G})", left)
	}
}

// An api:Animate staticAbilities$ grant with no Duration$ is this-turn: the
// reduction prices the offer until cleanup and not after it.
func TestCostGrantAnimateUntilEOTEndsAtCleanup(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, cgBear))
	bear := e.G.Zone(state.ZHand, 0)[0]
	src := cgPlace(t, e, 0, card(t, "Name:Test Discounter\nTypes:Artifact\n"+
		"SVar:DBAnimate:DB$ Animate | Defined$ Remembered | staticAbilities$ ReduceCost\n"+
		"SVar:ReduceCost:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ 1 | EffectZone$ All\nOracle:x\n"))
	e.G.Players[0].Pool[state.MG] = 1
	cgResolveSVar(t, e, src, "DBAnimate", bear)
	if !hasCastOption(e.legalActions(0), bear) {
		t.Fatalf("bear not offered with {G} while the this-turn reduction is live")
	}
	e.EndOfTurnCleanup()
	if hasCastOption(e.legalActions(0), bear) {
		t.Fatalf("this-turn reduction still prices the bear after cleanup")
	}
}

// Absorb Energy: "Cards in your hand that share a card type with that spell
// perpetually gain 'This spell costs {1} less to cast.'" -- an api:AnimateAll
// staticAbilities$ grant over Zone$ Hand. The creature card shares a type
// with the remembered creature; the sorcery does not.
func TestCostGrantAnimateAllReducesMatchingHandCards(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, cgBear), card(t, cgSorcery))
	hand := e.G.Zone(state.ZHand, 0)
	bear, growth := hand[0], hand[1]
	absorb := cgPlace(t, e, 0, corpusAlternativeCard(t, "Absorb Energy"))
	countered := cgPlace(t, e, 1, card(t, "Name:Test Spell Creature\nManaCost:2\nTypes:Creature Golem\nPT:2/2\nOracle:x\n"))
	e.G.Players[0].Pool[state.MG] = 1
	cgResolveSVar(t, e, absorb, "DBAnimate", countered)
	opts := e.legalActions(0)
	if !hasCastOption(opts, bear) {
		t.Fatalf("creature card not offered with {G} after Absorb Energy's reduction")
	}
	if hasCastOption(opts, growth) {
		t.Fatalf("sorcery (shares no type with the countered creature) was reduced")
	}
}

// Jubilant Skybonder: "Creatures you control with flying have 'Spells your
// opponents cast that target this creature cost {2} more to cast.'" -- a
// stat:Continuous AddStaticAbility$ grant. The raise binds to each HOST: a
// spell at the flyer pays {2} more while Skybonder is on the battlefield,
// and the plain price once it has left.
func TestCostGrantContinuousAddStaticAbilityRaisesWhileLive(t *testing.T) {
	t.Parallel()
	zap := "Name:Test Zap\nManaCost:U\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1\nOracle:x\n"
	e := handEngine(t, card(t, zap), card(t, zap))
	hand := e.G.Zone(state.ZHand, 0)
	zap1, zap2 := hand[0], hand[1]
	sky := cgPlace(t, e, 1, corpusAlternativeCard(t, "Jubilant Skybonder"))
	flyer := cgPlace(t, e, 1, card(t, "Name:Test Flyer\nManaCost:1 U\nTypes:Creature Bird\nPT:1/3\nK:Flying\nOracle:x\n"))
	e.G.Players[0].Pool[state.MU] = 1
	e.G.Players[0].Pool[state.MC] = 2
	if left := cgCastTargeting(t, e, zap1, flyer); left != 0 {
		t.Fatalf("pool after a zap at the flyer = %d, want 0 (charged {2}{U})", left)
	}
	e.resolveTop()
	e.emit(events.Event{Kind: events.MoveZone, Obj: sky, From: state.ZBattlefield, To: state.ZGraveyard})
	cgSettle(t, e)
	e.G.Players[0].Pool = state.Mana{}
	e.G.Players[0].Pool[state.MU] = 1
	e.G.Players[0].Pool[state.MC] = 2
	if left := cgCastTargeting(t, e, zap2, flyer); left != 2 {
		t.Fatalf("pool after a zap at the flyer with Skybonder gone = %d, want 2 (charged {U})", left)
	}
}

// Firion, Wild Rose Warrior: the token copy has "This Equipment's equip
// abilities cost {2} less to activate." -- an api:CopyPermanent
// AddStaticAbilities$ grant. With {1} floating the COPY's equip {3} is
// offered; the original Equipment gained nothing and is not.
func TestCostGrantCopyPermanentReducesTokenEquip(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	cgPlace(t, e, 0, corpusAlternativeCard(t, "Firion, Wild Rose Warrior"))
	cgPlace(t, e, 0, card(t, cgBear))
	blade := cgPlace(t, e, 0, card(t, "Name:Test Blade\nManaCost:1\nTypes:Artifact Equipment\nK:Equip:3\nS:Mode$ Continuous | Affected$ Creature.EquippedBy | AddPower$ 1\nOracle:x\n"))
	var token state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); id != blade && o.Face() != nil && o.Face().Name == "Test Blade" {
			token = id
		}
	}
	if token == 0 {
		t.Fatalf("Firion minted no copy of the entering Equipment")
	}
	e.G.Players[0].Pool[state.MC] = 1
	opts := e.legalActions(0)
	offered := func(obj state.ObjID) bool {
		for _, o := range opts {
			if o.Kind == "ability" && o.Obj == obj {
				return true
			}
		}
		return false
	}
	if !offered(token) {
		t.Fatalf("token copy's equip not offered with {1} floating (Firion's {2} reduction)")
	}
	if offered(blade) {
		t.Fatalf("original Equipment's equip offered with {1}: the grant leaked off the copy")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: token, From: state.ZBattlefield, To: state.ZGraveyard})
	cgSettle(t, e)
	for _, v := range e.collectCostStatics().reduce {
		if v.Source == token {
			t.Fatalf("the departed copy's granted reduction is still collected: %+v", v)
		}
	}
}
