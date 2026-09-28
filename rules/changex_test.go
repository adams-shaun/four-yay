package rules

// DB$ ChangeX (effects/changex.go's effChangeX + events.XChange): a
// mid-resolution rewrite of the {X} a stack object was cast or activated
// with. The two real corpus carriers:
//
//   - Unbound Flourishing (Mode$ SpellCast | ValidCard$ Permanent |
//     HasXManaCost$ True, Value$ TriggeredSpellAbility>Count$xPaid/Twice):
//     the cast trigger resolves above the spell (CR 601.2c ordering), reads
//     the PAID X off the stack object through the new <Ref>>Count$...
//     indirection in effects/count.go, and doubles it before the spell
//     resolves -- so the ETB counter keyword (which reads the moving spell's
//     X off the preserved o.X) sees the doubled value.
//   - Glava, Five-Advents Mage (Mode$ SpellAbilityCast ... Value$ 5): the
//     trigger's OptionalDecider$ ask is answered by drainTriggerAsks (yes) or
//     the local decline drain (no); on a yes the ability object's X becomes 5
//     and the ability's own resolution reads the rewritten value. The
//     activation arm's Remembered is the SOURCE PERMANENT (an AbilityPush's
//     Obj -- the ability stack object is minted inside events.Apply, off the
//     event), so effChangeX re-derives the ability wrapper (measured premise
//     correction vs the brief, which claimed triggerRemembered captures the
//     stack object on both arms).
//
// The Glava lines are copied verbatim from
// .cards/cardsfolder/g/glava_five_advents_mage.txt (the glavaSrc fixture in
// hasxmanacost_test.go already carries them); the UF trigger/SVar lines are
// the unboundFlourishingSrc fixture, same file. Inline fixtures per the
// licensing rule (never a .cards/ .txt). Engines come from newFixtureDeck,
// whose seatZeroStart pins the toss on seat 0 the way every seat-indexed
// fixture here assumes.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// changeXSifterSrc is xSifterSrc with the ability body reading the X it was
// activated at: NumCards$ X draws the announced-then-rewritten value, so the
// ability's own resolution is observable without the trigger in the way.
const changeXSifterSrc = "Name:X Sifter\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ X T | NumCards$ X | SpellDescription$ Draw X cards.\n" +
	"Oracle:Draw X cards.\n"

// changeXHydraSrc is xCostHydraSrc with an ETB counter rider: the keyword
// reads the moving spell's X off the preserved o.X (rules/replacement.go's
// ETB replacement ctx), so the doubled value is observable on the permanent
// after the spell resolves.
const changeXHydraSrc = "Name:Test X Hydra\nManaCost:X G\nTypes:Creature Beast\nPT:0/0\n" +
	"K:etbCounter:P1P1:X\n" +
	"SVar:X:Count$xPaid\n" +
	"DeckHas:Ability$Counters\n" +
	"Oracle:x\n"

// toHand bridges one fixture card to seat 0's hand: it is already there for
// newFixtureDeck's own fixture; an extra is bridged out of the library the
// same way moveSeeded does, clearing the stale priority ask afterwards.
func toHand(t *testing.T, e *Engine, src string) state.ObjID {
	t.Helper()
	name := card(t, src).Faces[0].Name
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
			e.pending = nil
			e.Advance()
			return id
		}
	}
	t.Fatalf("fixture card %q not found in seat 0's library or hand", name)
	return 0
}

// xChangeCount counts XChange events naming id and returns their amounts.
func xChangeCount(e *Engine, id state.ObjID) (int, []int32) {
	n := 0
	var amounts []int32
	for _, ev := range e.L.Events {
		if ev.Kind == events.XChange && ev.Obj == id {
			n++
			amounts = append(amounts, ev.Amount)
		}
	}
	return n, amounts
}

// drainTriggerAsksDecline is drainTriggerAsks with Glava's optional placement
// ask answered "no" (option index 1): the trigger never reaches the stack.
func drainTriggerAsksDecline(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		switch d.Kind {
		case decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					goto next
				}
			}
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		case decision.KTriggerOrder: // the answer is a full permutation
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTriggerOptional:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
				t.Fatalf("submit decline: %v", err)
			}
		default:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
				t.Fatalf("submit %v: %v", d.Kind, err)
			}
		}
	next:
	}
	if !e.G.Over && len(e.G.Stack) > 0 {
		t.Fatalf("stack never emptied (depth %d after %d passes)", len(e.G.Stack), limit)
	}
}

// glavaAbilityObject finds the activated-ability stack object of permanent xs
// on the stack, the mirror of effChangeX's own activation-arm derivation.
func glavaAbilityObject(t *testing.T, e *Engine, xs state.ObjID) state.ObjID {
	t.Helper()
	for i := len(e.G.Stack) - 1; i >= 0; i-- {
		o := e.G.Obj(e.G.Stack[i])
		if o == nil || o.Card != nil || o.Ability == nil || o.Source != xs {
			continue
		}
		if _, isTrig := state.TriggerOf(e.G, o); isTrig {
			continue
		}
		return o.ID
	}
	t.Fatalf("X Sifter ability object not on the stack: %v", e.G.Stack)
	return 0
}

// glavaAnnounce activates X Sifter's X-cost ability at X and answers the X
// ask, leaving the paid ability on the stack with Glava's optional trigger
// queued behind it.
func glavaAnnounce(t *testing.T, e *Engine, x int) state.ObjID {
	t.Helper()
	addMana(t, e, 0, "CC")
	castFirst(t, e, "ability")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, x)
	return glavaAbilityObject(t, e, func() state.ObjID {
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "X Sifter" {
				return id
			}
		}
		t.Fatalf("X Sifter permanent not on the battlefield")
		return 0
	}())
}

// TestChangeXGlavaActivationSetsXToFive is the literal-Value$ leaf on the
// verbatim corpus lines: activate the X-cost ability at X=1, accept Glava's
// optional trigger, and the ability object's X is rewritten to 5 BEFORE the
// ability resolves -- the ability's own NumCards$ X then draws 5.
func TestChangeXGlavaActivationSetsXToFive(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 132, glavaSrc, changeXSifterSrc, plainSifterSrc)
	moveSeeded(t, e, 0, glavaSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, changeXSifterSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, plainSifterSrc, state.ZBattlefield)
	abil := glavaAnnounce(t, e, 1)
	if o := e.G.Obj(abil); o.X != 1 {
		t.Fatalf("paid X = %d, want 1 before the trigger resolves", o.X)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	drainTriggerAsks(t, e, 30) // answers the optional placement ask with yes
	n, amounts := xChangeCount(e, abil)
	if n != 1 || len(amounts) != 1 || amounts[0] != 5 {
		t.Fatalf("XChange on the ability = %d events %v, want exactly one amount 5", n, amounts)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+5 {
		t.Fatalf("hand %d, want %d (the ability's NumCards$ X read the rewritten 5)",
			got, handBefore+5)
	}
	replayCheck(t, e, cfg)
}

// TestChangeXGlavaDeclineLeavesXAlone is the decline leaf: the same
// activation, but the optional trigger's ask is answered "no" -- the trigger
// never reaches the stack, no XChange is emitted, and the ability resolves at
// its paid X=1 (one draw).
func TestChangeXGlavaDeclineLeavesXAlone(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 133, glavaSrc, changeXSifterSrc, plainSifterSrc)
	moveSeeded(t, e, 0, glavaSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, changeXSifterSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, plainSifterSrc, state.ZBattlefield)
	glavaAnnounce(t, e, 1)
	handBefore := len(e.G.Zone(state.ZHand, 0))
	drainTriggerAsksDecline(t, e, 30)
	for _, ev := range e.L.Events {
		if ev.Kind == events.XChange {
			t.Fatalf("XChange emitted on a declined optional trigger: %+v", ev)
		}
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand %d, want %d (the ability's NumCards$ X read the paid 1)",
			got, handBefore+1)
	}
	replayCheck(t, e, cfg)
}

// TestChangeXUnboundFlourishingDoublesXPaid is the Ref>Count$ indirection
// leaf on the verbatim corpus lines: cast the X-cost creature at X=2, UF's
// SpellCast trigger resolves above the spell, reads the PAID 2 through
// TriggeredSpellAbility>Count$xPaid/Twice, and rewrites the spell's X to 4
// before the spell resolves -- the ETB counter keyword then enters it with 4
// +1/+1 counters and Move preserves the rewritten X on the permanent.
func TestChangeXUnboundFlourishingDoublesXPaid(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 134, unboundFlourishingSrc, changeXHydraSrc)
	uf := moveSeeded(t, e, 0, unboundFlourishingSrc, state.ZBattlefield)
	hydra := toHand(t, e, changeXHydraSrc)
	addMana(t, e, 0, "GGG")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	if o := e.G.Obj(hydra); o.Zone != state.ZStack || o.X != 2 {
		t.Fatalf("spell zone=%s X=%d, want stack/2 before the trigger resolves", o.Zone, o.X)
	}
	if got := pushCount(e, uf); got != 1 {
		t.Fatalf("UF TriggerPush count after the X-cost cast = %d, want 1", got)
	}
	drainTriggerAsks(t, e, 30)
	n, amounts := xChangeCount(e, hydra)
	if n != 1 || len(amounts) != 1 || amounts[0] != 4 {
		t.Fatalf("XChange on the spell = %d events %v, want exactly one amount 4", n, amounts)
	}
	o := e.G.Obj(hydra)
	if o.Zone != state.ZBattlefield || o.X != 4 {
		t.Fatalf("permanent zone=%s X=%d, want battlefield/4 (the rewritten value preserved through resolution)", o.Zone, o.X)
	}
	if got := o.Counter("P1P1"); got != 4 {
		t.Fatalf("ETB +1/+1 counters = %d, want 4 (the ETB replacement read the DOUBLED X)", got)
	}
	replayCheck(t, e, cfg)
}

// TestChangeXUnresolvableValueStaysLoud pins the degraded shape: a Value$
// the grammar cannot resolve (unknown ref, the CastSA adamant family's shape)
// is NOT evaluated -- no XChange is emitted and the loud Note records it,
// rather than a silent rewrite to 0.
func TestChangeXUnresolvableValueStaysLoud(t *testing.T) {
	t.Parallel()
	src := "Name:Test Doubler\nManaCost:1 U\nTypes:Enchantment\n" +
		"T:Mode$ SpellCast | ValidSA$ Spell | ValidActivatingPlayer$ You | HasXManaCost$ True | Execute$ TrigX | TriggerZones$ Battlefield | TriggerDescription$ x\n" +
		"SVar:TrigX:DB$ ChangeX | Defined$ TriggeredSpellAbility | Value$ CastSA>Count$xPaid/Twice\n" +
		"Oracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 135, src, changeXHydraSrc)
	moveSeeded(t, e, 0, src, state.ZBattlefield)
	toHand(t, e, changeXHydraSrc)
	addMana(t, e, 0, "GGG")
	castFirst(t, e, "cast")
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X decision %+v", d)
	}
	submitChoices(t, e, 2)
	drainTriggerAsks(t, e, 30)
	for _, ev := range e.L.Events {
		if ev.Kind == events.XChange {
			t.Fatalf("XChange emitted from an unresolvable Value$: %+v", ev)
		}
	}
	notes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "ChangeX value unresolvable" {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("unresolvable-Value$ notes = %d, want exactly one", notes)
	}
	replayCheck(t, e, cfg)
}
