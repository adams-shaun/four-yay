package rules

// The granted spell-cast copy trigger (abcopy). A Mode$ SpellCast trigger that
// carries a Cost$ and/or an OptionalDecider$ must pay and ask exactly like a
// printed one, whether it is printed on the face or GRANTED. Two grant routes
// carry the trigger as an SVar-named body that never appears in Face.Triggers,
// so the printed-face pointer scan (findTriggerForAbilityFace) cannot see it:
//
//   - a static AddTrigger$ grant (Mendicant Core, Guidelight's Max-speed line),
//     queued as a GrantTriggerPush;
//   - an event-matched DB$ Effect | Triggers$ registration (Rowan, Scholar of
//     Sparks' emblem line), queued as a DelayedPush.
//
// Before the fix both bodies resolved with no trigger line attached, so the
// findTriggerForAbility gate returned false: the OptionalDecider$ ask never
// posed, the Cost$ window never armed, and the copy resolved for FREE.
// Engine.triggerLines (recorded by pushTrigger, resolved by
// triggerForAbilityObject) carries the line from the push to resolution.
//
// The fixtures carry the corpus cards' lines VERBATIM (inline per the licensing
// rule); the carriers are plain cards so the leaf under test is the grant
// route, not the carrier.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// mendicantCoreSrc carries mendicant_core_guidelight's Max-speed AddTrigger$
// line and both SVars VERBATIM (Condition$ MaxSpeed dropped: the leaf under
// test is the GRANT's visibility, not the speed condition).
const mendicantCoreSrc = "Name:Test Guidelight\nManaCost:2\nTypes:Artifact Creature\nPT:2/2\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | AddTrigger$ CastTrig | Description$ Max speed — Whenever you cast an artifact spell, you may pay {1}. If you do, copy it.\n" +
	"SVar:CastTrig:Mode$ SpellCast | ValidCard$ Artifact | ValidActivatingPlayer$ You | Execute$ TrigCopy | OptionalDecider$ You | TriggerZones$ Battlefield | Secondary$ True | TriggerDescription$ Max speed — Whenever you cast an artifact spell, you may pay {1}. If you do, copy it.\n" +
	"SVar:TrigCopy:AB$ CopySpellAbility | Cost$ 1 | Defined$ TriggeredSpellAbility | Amount$ 1\n" +
	"Oracle:x\n"

const artifactSpellSrc = "Name:Test Trinket Spell\nManaCost:1\nTypes:Artifact\n" +
	"A:SP$ Draw | NumCards$ 1\nOracle:x\n"

// rowanEmblemSrc carries rowan_scholar_of_sparks' emblem ultimate's Triggers$
// name and both SVars VERBATIM on a sorcery carrier.
const rowanEmblemSrc = "Name:Test Rowan Emblem\nManaCost:2 R\nTypes:Sorcery\n" +
	"A:SP$ Effect | Triggers$ TRCast | SpellDescription$ You get an emblem with \"Whenever you cast an instant or sorcery spell, you may pay {2}. If you do, copy that spell.\"\n" +
	"SVar:TRCast:Mode$ SpellCast | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | TriggerZones$ Command | Execute$ TrigCopy | TriggerDescription$ Whenever you cast an instant or sorcery spell, you may pay {2}. If you do, copy that spell. You may choose new targets for the copy.\n" +
	"SVar:TrigCopy:AB$ CopySpellAbility | Cost$ 2 | Defined$ TriggeredSpellAbility | AILogic$ Always | MayChooseTarget$ True\n" +
	"Oracle:x\n"

// eldraziEchoTwoSrc is a SECOND, distinct mandatory Eldrazi-cast trigger so the
// plural ValidStack leaf has two DIFFERENT ability wrappers to admit (the
// printed-ability family exclusion drops every instance of one ability).
const eldraziEchoTwoSrc = "Name:Test Eldrazi Echo Two\nManaCost:2\nTypes:Creature Eldrazi\nPT:1/1\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.Eldrazi | TriggerZones$ Battlefield | Execute$ TrigDrawTwo | " +
	"TriggerDescription$ Whenever a player casts an Eldrazi spell, draw a card.\n" +
	"SVar:TrigDrawTwo:DB$ Draw | NumCards$ 1\nOracle:x\n"

// castCardNow submits the priority decision's "cast" option for the named card.
func castCardNow(t *testing.T, e *Engine, name string) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no pending decision for cast of %q", name)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Label == "Cast "+name {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("cast %q: %v", name, err)
			}
			return
		}
	}
	t.Fatalf("no cast option for %q in %+v", name, d.Options)
}

// spellOnStack returns the id of the named card sitting on the stack, failing
// if it is not there (a lookup, never a cast).
func spellOnStack(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for i := len(e.G.Stack) - 1; i >= 0; i-- {
		o := e.G.Obj(e.G.Stack[i])
		if o == nil || o.Card == nil || o.Face() == nil {
			continue
		}
		if o.Face().Name == name {
			return o.ID
		}
	}
	t.Fatalf("%q not on the stack: %v", name, e.G.Stack)
	return 0
}

// drainCopyGrants drives the stack to empty, recording whether an optional
// trigger ask and a trigger-cost pay ask were posed and answering them per the
// policy. A pay ask with no answerable pay option is answered with its lone
// decline regardless of pay.
func drainCopyGrants(t *testing.T, e *Engine, limit int, acceptOptional, pay bool) (sawOptional, sawPay bool) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (depth %d)", len(e.G.Stack))
		}
		switch {
		case d.Kind == decision.KTriggerOptional:
			sawOptional = true
			if acceptOptional {
				submitChoices(t, e, 0)
			} else {
				submitChoices(t, e, 1)
			}
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_pay":
			sawPay = true
			if pay && len(d.Options) == 2 {
				submitChoices(t, e, 0)
			} else {
				submitChoices(t, e, 1)
			}
		case d.Kind == decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					goto next
				}
			}
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		case d.Kind == decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		default:
			submitChoices(t, e, 0)
		}
	next:
	}
	if !e.G.Over && len(e.G.Stack) > 0 {
		t.Fatalf("stack never emptied (depth %d after %d passes)", len(e.G.Stack), limit)
	}
	return sawOptional, sawPay
}

// TestGrantedStaticSpellCopyPaysCost is the static AddTrigger$ route on
// Mendicant Core's verbatim lines: the granted OptionalDecider$ ask must pose
// (the trigger IS a trigger), the {1} pay ask must pose, and paying must charge
// the pool and copy. Pre-fix neither ask posed and the copy resolved for free.
func TestGrantedStaticSpellCopyPaysCost(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 902, mendicantCoreSrc, artifactSpellSrc, plainSifterSrc)
	moveSeeded(t, e, 0, mendicantCoreSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, artifactSpellSrc, state.ZHand)
	addMana(t, e, 0, "UUU")
	castCardNow(t, e, "Test Trinket Spell")
	spell := spellOnStack(t, e, "Test Trinket Spell")
	// Precondition the copy depends on: the cast spell is on the stack (the
	// zone effCopySpellAbility's guard reads).
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("cast spell zone = %v, want the stack", e.G.Obj(spell).Zone)
	}
	poolBefore := poolTotal(e.G.Players[0].Pool)
	sawOptional, sawPay := drainCopyGrants(t, e, 40, true, true)
	if !sawOptional {
		t.Fatalf("granted trigger's OptionalDecider$ ask never posed -- the AddTrigger$ grant is invisible to findTriggerForAbility")
	}
	if !sawPay {
		t.Fatalf("granted trigger's {{1}} Cost$ pay ask never posed")
	}
	if got := copyCount(e, spell); got != 1 {
		t.Fatalf("StackCopy of the artifact spell = %d, want exactly one (got %v)", got, allStackCopies(e))
	}
	if got := poolTotal(e.G.Players[0].Pool); got != poolBefore-1 {
		t.Fatalf("pool %d, want %d -- the granted {{1}} must be charged", got, poolBefore-1)
	}
	replayCheck(t, e, cfg)
}

// TestGrantedStaticSpellCopyDeclinesOptional leaves the optional ask a NO: no
// copy, no charge -- the control that makes the paid leaf meaningful.
func TestGrantedStaticSpellCopyDeclinesOptional(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 903, mendicantCoreSrc, artifactSpellSrc, plainSifterSrc)
	moveSeeded(t, e, 0, mendicantCoreSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, artifactSpellSrc, state.ZHand)
	addMana(t, e, 0, "UUU")
	castCardNow(t, e, "Test Trinket Spell")
	spell := spellOnStack(t, e, "Test Trinket Spell")
	poolBefore := poolTotal(e.G.Players[0].Pool)
	sawOptional, sawPay := drainCopyGrants(t, e, 40, false, false)
	if !sawOptional {
		t.Fatalf("granted trigger's OptionalDecider$ ask never posed")
	}
	if sawPay {
		t.Fatalf("a declined optional trigger still posed its {{1}} pay ask")
	}
	if got := copyCount(e, spell); got != 0 {
		t.Fatalf("declined optional still copied: %d events %v", got, allStackCopies(e))
	}
	if got := poolTotal(e.G.Players[0].Pool); got != poolBefore {
		t.Fatalf("pool %d, want %d -- a decline must not charge", got, poolBefore)
	}
	replayCheck(t, e, cfg)
}

// TestEffectGrantedSpellCopyPaysCost is the DB$ Effect | Triggers$ route on
// Rowan's verbatim lines: cast the effect, then cast an instant -- the
// registration's {2} ask must pose, and paying must charge the pool and copy.
func TestEffectGrantedSpellCopyPaysCost(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 904, rowanEmblemSrc, spellInsightSrc, plainSifterSrc)
	moveSeeded(t, e, 0, rowanEmblemSrc, state.ZHand)
	moveSeeded(t, e, 0, spellInsightSrc, state.ZHand)
	addMana(t, e, 0, "RRRRUUUU")
	castCardNow(t, e, "Test Rowan Emblem")
	drainTriggerAsks(t, e, 20)
	if len(e.G.Delayed) == 0 {
		t.Fatalf("the Effect registered no delayed SpellCast trigger")
	}
	e.pending = nil
	e.priorityRound()
	castCardNow(t, e, "Test Blue Insight")
	spell := spellOnStack(t, e, "Test Blue Insight")
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("cast spell zone = %v, want the stack", e.G.Obj(spell).Zone)
	}
	poolBefore := poolTotal(e.G.Players[0].Pool)
	_, sawPay := drainCopyGrants(t, e, 40, false, true)
	if !sawPay {
		t.Fatalf("Effect-granted trigger's {{2}} Cost$ pay ask never posed -- the delayed body is invisible to findTriggerForAbility")
	}
	if got := copyCount(e, spell); got != 1 {
		t.Fatalf("StackCopy of the instant = %d, want exactly one (got %v)", got, allStackCopies(e))
	}
	if got := poolTotal(e.G.Players[0].Pool); got != poolBefore-2 {
		t.Fatalf("pool %d, want %d -- the granted {{2}} must be charged", got, poolBefore-2)
	}
	replayCheck(t, e, cfg)
}

// TestEffectGrantedSpellCopyDeclineNeverCopies is the Effect route's decline
// leaf: the {2} ask is answered "do not pay" -- no copy, pool untouched.
func TestEffectGrantedSpellCopyDeclineNeverCopies(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 905, rowanEmblemSrc, spellInsightSrc, plainSifterSrc)
	moveSeeded(t, e, 0, rowanEmblemSrc, state.ZHand)
	moveSeeded(t, e, 0, spellInsightSrc, state.ZHand)
	addMana(t, e, 0, "RRRRUUUU")
	castCardNow(t, e, "Test Rowan Emblem")
	drainTriggerAsks(t, e, 20)
	if len(e.G.Delayed) == 0 {
		t.Fatalf("the Effect registered no delayed SpellCast trigger")
	}
	e.pending = nil
	e.priorityRound()
	castCardNow(t, e, "Test Blue Insight")
	spell := spellOnStack(t, e, "Test Blue Insight")
	poolBefore := poolTotal(e.G.Players[0].Pool)
	_, sawPay := drainCopyGrants(t, e, 40, false, false)
	if !sawPay {
		t.Fatalf("Effect-granted trigger's {{2}} Cost$ pay ask never posed")
	}
	if got := copyCount(e, spell); got != 0 {
		t.Fatalf("declined {{2}} pay still copied: %d events %v", got, allStackCopies(e))
	}
	if got := poolTotal(e.G.Players[0].Pool); got != poolBefore {
		t.Fatalf("pool %d, want %d -- a decline must not charge", got, poolBefore)
	}
	replayCheck(t, e, cfg)
}

// drainUlalekPluralAsks orders Ulalek's trigger last, pays {C}{C} and captures
// EVERY echo ability wrapper on the stack at the pay ask (keyed by Source).
func drainUlalekPluralAsks(t *testing.T, e *Engine, ulalek, echo1, echo2 state.ObjID, limit int) map[state.ObjID]struct{} {
	t.Helper()
	var echoWrappers map[state.ObjID]struct{}
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (depth %d)", len(e.G.Stack))
		}
		switch {
		case d.Kind == decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit pass: %v", err)
					}
					goto next
				}
			}
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		case d.Kind == decision.KTriggerOrder:
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				if o.Obj != ulalek {
					choices = append(choices, o.Index)
				}
			}
			for _, o := range d.Options {
				if o.Obj == ulalek {
					choices = append(choices, o.Index)
				}
			}
			if len(choices) != len(d.Options) {
				t.Fatalf("trigger order options %v lost Ulalek's entry", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_pay":
			echoWrappers = map[state.ObjID]struct{}{}
			for _, sid := range e.G.Stack {
				o := e.G.Obj(sid)
				if o == nil || o.Ability == nil {
					continue
				}
				if o.Source == echo1 || o.Source == echo2 {
					echoWrappers[sid] = struct{}{}
				}
			}
			submitChoices(t, e, 0)
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
	return echoWrappers
}

// TestUlalekValidStackCopyIsPlural pins CR 707.10a's "copy each" for the
// Defined$ ValidStack arm: Ulalek's verbatim sub-copy body
// (Ability.YouCtrl+otherAbility) with TWO distinct echo ability wrappers on the
// stack must copy BOTH, not just the first in stack-arena order. The resolving
// wrapper is still excluded (the family anchor).
func TestUlalekValidStackCopyIsPlural(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 906, ulalekSpellCopySrc, eldraziInsightSrc, eldraziEchoSrc, eldraziEchoTwoSrc)
	ulalek := moveSeeded(t, e, 0, ulalekSpellCopySrc, state.ZBattlefield)
	echo1 := moveSeeded(t, e, 0, eldraziEchoSrc, state.ZBattlefield)
	echo2 := moveSeeded(t, e, 0, eldraziEchoTwoSrc, state.ZBattlefield)
	spell := moveSeeded(t, e, 0, eldraziInsightSrc, state.ZHand)
	addMana(t, e, 0, "CCC")
	castCardNow(t, e, "Test Eldrazi Insight")
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("cast spell not on the stack")
	}
	if spell == 0 || echo1 == 0 || echo2 == 0 || spell == echo1 || echo1 == echo2 {
		t.Fatalf("fixture objects not distinct: spell=%d echo1=%d echo2=%d", spell, echo1, echo2)
	}
	echoWrappers := drainUlalekPluralAsks(t, e, ulalek, echo1, echo2, 60)
	if len(echoWrappers) != 2 {
		t.Fatalf("echo wrappers on the stack at the pay ask = %d, want 2 (the plural arm needs two matches)", len(echoWrappers))
	}
	copies := allStackCopies(e)
	if len(copies) != 3 {
		t.Fatalf("StackCopy events %v, want exactly three (spell + both echo wrappers); the ValidStack arm copied only the first match", copies)
	}
	seenSpell, seenEcho := false, 0
	srcOf := func(id state.ObjID) state.ObjID {
		if o := e.G.Obj(id); o != nil {
			return o.Source
		}
		return 0
	}
	for _, c := range copies {
		switch {
		case c == spell:
			seenSpell = true
		case srcOf(c) == echo1 || srcOf(c) == echo2:
			seenEcho++
		default:
			t.Fatalf("unexpected StackCopy target %d (source %d)", c, srcOf(c))
		}
	}
	if !seenSpell || seenEcho != 2 {
		t.Fatalf("copies %v: spell=%v echoCopies=%d, want the spell and BOTH echo wrappers", copies, seenSpell, seenEcho)
	}
	replayCheck(t, e, cfg)
}
