package rules

import (
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// sharedCorpus loads the compiled corpus once per test binary. Every test in
// this file used to pay a full testutil.CorpusRegistry load -- a git
// rev-parse subprocess plus a gunzip and gob decode of the whole IR cache,
// ~0.25s apiece, which across this file's corpus-driven tests was the single
// largest share of the rules package's wall time and what held it at its
// test-time budget boundary. The loaded corpus is immutable: cards.Registry
// is written only by compile-time Add, no engine path writes through a card
// face (state lives on state.Object), and the one Tokens handout below
// (e.G.Tokens = reg.Tokens) is only ever read in this file -- each game
// still gets the registry's own token map because no test here mints into
// it. If the corpus is absent the first caller Skips without populating the
// memo, so every test still skips on its own.
var (
	sharedCorpusMu  sync.Mutex
	sharedCorpusReg *cards.Registry
)

func sharedCorpus(t *testing.T) *cards.Registry {
	t.Helper()
	sharedCorpusMu.Lock()
	defer sharedCorpusMu.Unlock()
	if sharedCorpusReg == nil {
		sharedCorpusReg = testutil.CorpusRegistry(t)
	}
	return sharedCorpusReg
}

// TestBloodOfTheMartyrEffectCreatedOptionalReplacement uses the unmodified
// compiled corpus replacement: ReplaceEffect changes the damage's affected
// player before DamageDone is logged. The Optional$ "may" belongs to the
// replacement's OptionalDecider$ -- "You", the source's controller -- not to
// the damaged creature's controller (the Battletide Alchemist round-2
// finding; both corpus Optional$ DamageDone lines name "You").
func TestBloodOfTheMartyrEffectCreatedOptionalReplacement(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	blood := e.G.AddObject(mustCorpusCard(t, reg, "Blood of the Martyr"), 0)
	blood.Zone = state.ZStack
	// Resolve the real Effect head exactly as the spell does; its SVar carries
	// the Optional$ DamageDone replacement that must survive the source moving.
	effects.Resolve(e, &effects.Ctx{Source: blood.ID, Controller: 0, SVars: blood.Face().SVars}, blood.Face().SpellAbility())
	target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 2})
	e.damaging = 0
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
		t.Fatalf("pending = %+v, want OptionalDecider$ You's ask (the source's controller, seat 0)", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Obj(target).Damage; got != 0 {
		t.Fatalf("target damage = %d, want 0 after optional redirection", got)
	}
	if got := e.G.Players[0].Life; got != 18 {
		t.Fatalf("replacement controller life = %d, want 18 after accepting redirection", got)
	}
}

// TestBattletideAlchemistAsksItsControllerAndPreventsClerics is the real
// round-2 finding's probe: Battletide Alchemist's Optional$ True |
// OptionalDecider$ You | ReplaceWith$ DB$ ReplaceDamage body. The choice is
// Battletide's controller's, not the damaged player's; accepting prevents
// exactly X of the damage, where X is Count$Valid Cleric.YouCtrl (Battletide
// itself is a Cleric, so X is 1 here), and declining prevents nothing --
// the unimplemented-body defect this closes made accepting erase ALL of it.
func TestBattletideAlchemistAsksItsControllerAndPreventsClerics(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	t.Run("decline prevents nothing", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Battletide Alchemist"))
		source := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:3/3\nOracle:x\n")
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
		e.damaging = 0
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
			t.Fatalf("pending = %+v, want the decider's (seat 0) KReplacement ask, not the damaged player's", d)
		}
		// The skip option sits at index len(cands) == 1.
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{1}}); err != nil {
			t.Fatal(err)
		}
		if got := e.G.Players[1].Life; got != 17 {
			t.Fatalf("damaged player life = %d, want 17 after declining the may", got)
		}
		if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
			t.Fatalf("pending after decline = %+v, want only the ordinary priority ask", d)
		}
	})

	t.Run("accept prevents one Cleric's worth", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Battletide Alchemist"))
		source := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:3/3\nOracle:x\n")
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
		e.damaging = 0
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
			t.Fatalf("pending = %+v, want the decider's (seat 0) KReplacement ask", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		// X is 1 (Battletide itself): 3 - 1 = 2 damage lands.
		if got := e.G.Players[1].Life; got != 18 {
			t.Fatalf("damaged player life = %d, want 18 after accepting one Cleric's prevention", got)
		}
	})

	t.Run("accept fully prevents when X covers the damage", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Battletide Alchemist"))
		onBoard(t, e, 0, "Name:Second Cleric\nTypes:Creature Cleric\nPT:1/1\nOracle:x\n")
		source := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:3/3\nOracle:x\n")
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
		e.damaging = 0
		d := e.Pending()
		if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
			t.Fatalf("pending = %+v, want the decider's (seat 0) KReplacement ask", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		// X is 2 (Battletide plus the other Cleric): nothing lands, and no
		// reduced Damage event is emitted.
		if got := e.G.Players[1].Life; got != 20 {
			t.Fatalf("damaged player life = %d, want 20 after full prevention", got)
		}
	})
}

// TestThunderstaffPreventsExactlyItsAmount pins the single-match
// ReplaceDamage body: Thunderstaff's "prevent 1 of that damage" must leave
// the rest standing, not erase the whole event (the pre-fix defect applied
// every DB$ ReplaceDamage body as a silent full prevention).
func TestThunderstaffPreventsExactlyItsAmount(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Thunderstaff"))
	source := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.damaging, e.combatDamaging = source, true
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging, e.combatDamaging = 0, false
	if got := e.G.Players[0].Life; got != 19 {
		t.Fatalf("life = %d, want 19: Thunderstaff prevents exactly 1 of 2", got)
	}

	// Noncombat damage does not match IsCombat$ True at all.
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != 17 {
		t.Fatalf("life = %d, want 17: noncombat damage bypasses Thunderstaff", got)
	}
}

// TestUnpriceableReplaceDamageBodyDoesNotMatch pins the fail-closed gate: a
// ReplaceDamage body whose Amount$ names a value frame this build does not
// resolve (here an undefined SVar name, the same class as Power Leak's
// PaidAmount) is not a match, so the damage lands untouched instead of being
// silently erased.
func TestUnpriceableReplaceDamageBodyDoesNotMatch(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	onBoard(t, e, 0, "Name:Unpriceable Shield\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidTarget$ You | ReplaceWith$ R\n"+
		"SVar:R:DB$ ReplaceDamage | Amount$ NeverCounted\nOracle:x\n")
	source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging = 0
	if got := e.G.Players[0].Life; got != 18 {
		t.Fatalf("life = %d, want 18: an unpriceable prevention body must not erase damage", got)
	}
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("pending = %+v, want only the ordinary priority ask", d)
	}
}

// TestDamageAllParksEveryRecipientBeforeTheChainResumes pins the
// multi-recipient settlement order (CR 616.1): a DamageAll over two creatures
// with two competing amount modifiers parks one order choice per recipient
// event, and the spell's chained rider (and its departure from the stack)
// waits for the LAST answer. Resuming after the first answer would let the
// rider fire and the spell leave the stack while the second recipient's
// damage is still awaiting its order choice.
func TestDamageAllParksEveryRecipientBeforeTheChainResumes(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	fiery := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Creature | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
		"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
	t1 := onBoard(t, e, 1, "Name:Victim One\nTypes:Creature\nPT:2/2\nOracle:x\n")
	t2 := onBoard(t, e, 1, "Name:Victim Two\nTypes:Creature\nPT:2/2\nOracle:x\n")
	spell := e.G.AddObject(card(t, "Name:Damage All\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DamageAll | ValidCards$ Creature | NumDmg$ 2 | SubAbility$ Gain\n"+
		"SVar:Gain:DB$ GainLife | Defined$ You | LifeAmount$ 5\nOracle:x\n"), 0)
	spell.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
	e.G.Stack = []state.ObjID{spell.ID}
	e.resolveTop()
	answers := 0
	for {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		if d.Kind != decision.KReplacement {
			t.Fatalf("answer %d: pending kind %v, want KReplacement (%+v)", answers, d.Kind, d)
		}
		answers++
		// After the first answer the rider must NOT have run and the spell
		// must still be on the stack: the second recipient's choice is owed.
		if answers == 2 {
			if got := e.G.Players[0].Life; got != 20 {
				t.Fatalf("rider ran before the second recipient's answer: life = %d, want 20", got)
			}
			if got := e.G.Obj(spell.ID).Zone; got != state.ZStack {
				t.Fatalf("spell zone after first answer = %s, want stack", got)
			}
		}
		if answers > 4 {
			t.Fatalf("too many replacement asks: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	if answers != 2 {
		t.Fatalf("replacement asks = %d, want one per recipient (2)", answers)
	}
	// Both recipients' damage settled before the rider: each victim took the
	// answered (2+2)*3 = 12 damage and died from it, and the rider ran once.
	for _, id := range []state.ObjID{t1, t2} {
		if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
			t.Fatalf("victim zone = %s, want graveyard after lethal settled damage", got)
		}
	}
	if got := e.G.Players[0].Life; got != 25 {
		t.Fatalf("rider life = %d, want 25 from exactly one GainLife after both answers", got)
	}
	if got := e.G.Obj(spell.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("spell zone = %s, want graveyard after the chain resumed", got)
	}
	_ = fiery
}

// TestRecomputedDamageOrderChoiceGoesToTheNewAffectedPlayer pins CR 616.1e's
// affected-player recompute: a redirect replacement chosen first changes the
// damage's recipient, and the recomputed order choice over the remaining
// modifiers must be asked of the NEW recipient's controller, not the original
// one.
func TestRecomputedDamageOrderChoiceGoesToTheNewAffectedPlayer(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	// Player 1 controls the optional redirector (the Blood of the Martyr
	// shape): damage to a player can be redirected to its controller.
	_ = onBoard(t, e, 1, "Name:Redirector\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidTarget$ Player | Optional$ True | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ Affected | VarValue$ You\nOracle:x\n")
	_ = onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Player | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
		"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging = 0
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
		t.Fatalf("pending = %+v, want the original recipient's (player 0's) order choice", d)
	}
	// Choose the redirector: the damage becomes player 1's to take, and the
	// recomputed competition over the remaining modifiers belongs to player 1.
	redir := -1
	for _, opt := range d.Options {
		if opt.Label == "Apply Redirector's replacement" {
			redir = opt.Index
		}
	}
	if redir < 0 {
		t.Fatalf("redirector missing from options: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{redir}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("after redirection pending = %+v, want a recomputed order choice", d)
	}
	if d.Player != 1 {
		t.Fatalf("recomputed order choice asked player %d, want the new recipient's controller 1", d.Player)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	// Redirected 2 damage, then Fiery's thrice and Plus.2: (2*3)+2 = 8 to
	// player 1; player 0 untouched.
	if got := e.G.Players[1].Life; got != 12 {
		t.Fatalf("player 1 life = %d, want 12 after the redirected, tripled, doubled damage", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("player 0 life = %d, want 20", got)
	}
}

func TestFieryEmancipationTriplesDamage(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	source := onBoard(t, e, 0, "Name:Red Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")

	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.damaging = 0
	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("life = %d, want 14 after Fiery Emancipation tripled 2 damage", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 1 && ev.Amount != 6 {
			t.Fatalf("logged damage = %d, want rewritten amount 6", ev.Amount)
		}
	}
}

// TestChandraCannotBeCountered uses Chandra's real R:Event$ Counter line and
// Counterspell's real effect: the Counter primitive must ask the replacement
// before it moves the target off the stack.
func TestChandraCannotBeCountered(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	chandra := e.G.AddObject(mustCorpusCard(t, reg, "Chandra, Awakened Inferno"), 0)
	chandra.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{chandra.ID})
	e.G.Stack = []state.ObjID{chandra.ID}

	counterspell := mustCorpusCard(t, reg, "Counterspell").Faces[0].SpellAbility()
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: chandra.ID}}}, counterspell)
	if got := e.G.Obj(chandra.ID).Zone; got != state.ZStack {
		t.Fatalf("Chandra zone = %s, want stack after its counter replacement", got)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != chandra.ID {
		t.Fatalf("stack = %v, want Chandra still present", e.G.Stack)
	}
}

// TestSpiderPunkStopsProtectionPrevention uses Spider-Punk's real global
// CantPreventDamage static. Protection is the engine's current prevention
// path, so the protected creature must still take the blue source's damage.
func TestSpiderPunkStopsProtectionPrevention(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Spider-Punk"))
	target := onBoard(t, e, 1, "Name:Protected\nTypes:Creature\nPT:1/3\nK:Protection from blue\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Blue Source\nManaCost:U\nTypes:Creature\nPT:1/1\nOracle:x\n")

	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 2})
	e.damaging = 0
	if got := e.G.Obj(target).Damage; got != 2 {
		t.Fatalf("damage = %d, want 2; Spider-Punk forbids protection prevention", got)
	}
}

// TestSpiderPunkStopsReplaceDamagePreventionBodies closes the round-4
// finding: stat:CantPreventDamage used to exclude only the legacy
// Prevent$ True shape, so the DB$ ReplaceDamage prevention-body family
// (Thunderstaff, Battletide Alchemist) still reduced damage Spider-Punk
// forbids preventing. Every damage-replacement selection and application
// path now classifies prevention through the one damageReplacementPrevents
// predicate, so both shapes are excluded everywhere.
func TestSpiderPunkStopsReplaceDamagePreventionBodies(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	t.Run("single-match path: Thunderstaff", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Spider-Punk"))
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Thunderstaff"))
		source := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.damaging, e.combatDamaging = source, true
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
		e.damaging, e.combatDamaging = 0, false
		if got := e.G.Players[0].Life; got != 18 {
			t.Fatalf("life = %d, want 18: Spider-Punk forbids Thunderstaff's ReplaceDamage prevention", got)
		}
	})

	t.Run("ordered optional path: Battletide Alchemist is filtered before the ask", func(t *testing.T) {
		e := newSeats(t, 2)
		e.pending = nil
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Spider-Punk"))
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Battletide Alchemist"))
		source := onBoard(t, e, 1, "Name:Attacker\nTypes:Creature\nPT:3/3\nOracle:x\n")
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
		e.damaging = 0
		// The prevention body is excluded by the CantPreventDamage filter
		// before the order choice is computed, so no replacement ask may be
		// posed at all -- and none of the 3 damage may be prevented.
		if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
			t.Fatalf("pending = %+v, want only the ordinary priority ask: unpreventable damage must not ask about a prevention body", d)
		}
		if got := e.G.Players[0].Life; got != 17 {
			t.Fatalf("life = %d, want 17: none of the 3 damage may be prevented", got)
		}
	})
}

// TestOjerAxonilRaisesSmallNoncombatRedDamage uses the unmodified compiled
// corpus replacement: DamageAmount$ LTX (less than Ojer's power),
// ValidSource$ Card.RedSource+YouCtrl (the <Colour>Source predicate family),
// IsCombat$ False, and a VarValue$ X whose SVar is Count$CardPower. Noncombat
// red damage below the power is raised to the power; combat damage is not.
func TestOjerAxonilRaisesSmallNoncombatRedDamage(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Ojer Axonil, Deepest Might"))
	source := onBoard(t, e, 0, "Name:Red Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")

	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("life = %d, want 16: 2 noncombat red damage below power 4 becomes 4", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == 1 && ev.Amount != 4 {
			t.Fatalf("logged damage = %d, want rewritten amount 4", ev.Amount)
		}
	}

	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("life = %d, want 14: combat damage is not Ojer Axonil's replacement", got)
	}
}

// TestTheSoundOfDrumsDoublesCombatDamage uses the DIRECT ReplaceCount$ form
// (VarValue$ ReplaceCount$DamageAmount/Twice, no SVar indirection) on the
// enchanted creature's combat damage.
func TestTheSoundOfDrumsDoublesCombatDamage(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	aura := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "The Sound of Drums"))
	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:G\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})

	e.damaging = bear
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	e.damaging = 0
	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("life = %d, want 16: the enchanted creature's 2 combat damage doubled", got)
	}
}

// TestHexingSquelcherProtectsYourSpells uses the battlefield-arm counter
// replacement (ValidSA$ Spell.YouCtrl): a counterspell from the opponent
// cannot counter a spell you control, while your own copy of the same
// replacement does not shield the opponent's spells.
func TestPalisadeGiantRedirectsDamageToItself(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	giant := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Palisade Giant"))
	source := onBoard(t, e, 1, "Name:Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.damaging = source
	applied := e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
	e.damaging = 0
	if applied.Kind != events.Damage || applied.Obj != giant {
		t.Fatalf("applied damage = %+v, want damage redirected to Palisade Giant %d", applied, giant)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("player life = %d, want 20 after redirection", got)
	}
	if got := e.G.Obj(giant).Damage; got != 2 {
		t.Fatalf("Palisade Giant damage = %d, want 2", got)
	}
}

func TestDamageReplacementPropagatesAppliedAmountToLifelink(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	source := onBoard(t, e, 0, "Name:Lifelink Source\nManaCost:R\nTypes:Creature\nPT:1/1\nK:Lifelink\nOracle:x\n")
	sa := &cards.SA{API: "DealDamage", Params: map[string]string{"Defined": "Opponent", "NumDmg": "2"}}
	e.damaging = source
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0}, sa)
	e.damaging = 0
	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("target life = %d, want 14 after tripled damage", got)
	}
	if got := e.G.Players[0].Life; got != 26 {
		t.Fatalf("lifelink controller life = %d, want 26 from applied damage", got)
	}
}

func TestPreventReplacementSuppressesLifelink(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Blessed Sanctuary"))
	source := onBoard(t, e, 0, "Name:Lifelink Source\nManaCost:R\nTypes:Creature\nPT:1/1\nK:Lifelink\nOracle:x\n")
	sa := &cards.SA{API: "DealDamage", Params: map[string]string{"Defined": "Opponent", "NumDmg": "2"}}
	e.damaging = source
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0}, sa)
	e.damaging = 0
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("protected life = %d, want 20", got)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("lifelink controller life = %d, want 20 when damage was prevented", got)
	}
}

func TestDamageReplacementPropagatesAppliedCommanderDamage(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e, _ := commanderGame(t, commanderDamageSeed, FormatCommander, 40,
		[][]string{{cmdCreature(7)}, {}})
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	cmd := fieldCommander(t, e, 0, 0)
	swing(t, e, cmd)
	if got := e.G.Players[1].CmdDamage[0]; got != 21 {
		t.Fatalf("commander damage = %d, want applied 21", got)
	}
	if !e.G.Players[1].Lost {
		t.Fatal("defender survived 21 applied commander damage")
	}
}

func TestConditionalCounterReplacementUsesPaidX(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	for _, tc := range []struct {
		x             int32
		wantCountered bool
	}{{0, true}, {5, false}} {
		t.Run(string(rune('0'+tc.x)), func(t *testing.T) {
			e := newSeats(t, 2)
			banefire := e.G.AddObject(mustCorpusCard(t, reg, "Banefire"), 0)
			banefire.Zone = state.ZStack
			banefire.X = tc.x
			e.G.SetZone(state.ZStack, 0, []state.ObjID{banefire.ID})
			e.G.Stack = []state.ObjID{banefire.ID}
			cs := mustCorpusCard(t, reg, "Counterspell").Faces[0].SpellAbility()
			effects.Resolve(e, &effects.Ctx{Source: onBoard(t, e, 1, "Name:Counter Source\nTypes:Creature\nPT:1/1\nOracle:x\n"), Controller: 1,
				Targets: []state.Target{{Obj: banefire.ID}}, TargetsOffered: true}, cs)
			countered := e.G.Obj(banefire.ID).Zone != state.ZStack
			if countered != tc.wantCountered {
				t.Fatalf("Banefire X=%d countered=%v, want %v", tc.x, countered, tc.wantCountered)
			}
		})
	}
}

func TestGuileReplacesCounterWithExile(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Guile"))
	target := e.G.AddObject(mustCorpusCard(t, reg, "Banefire"), 1)
	target.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 1, []state.ObjID{target.ID})
	e.G.Stack = []state.ObjID{target.ID}
	counterSource := e.G.AddObject(mustCorpusCard(t, reg, "Counterspell"), 0)
	counterSource.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{counterSource.ID})
	e.G.Stack = append(e.G.Stack, counterSource.ID)
	effects.Resolve(e, &effects.Ctx{Source: counterSource.ID, Controller: 0,
		Targets: []state.Target{{Obj: target.ID}}}, counterSource.Face().SpellAbility())
	if got := e.G.Obj(target.ID).Zone; got != state.ZExile {
		t.Fatalf("Guile replacement put countered spell in %s, want exile", got)
	}
}

func TestInactiveDemonfireDoesNotForbidPrevention(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	demonfire := e.G.AddObject(mustCorpusCard(t, reg, "Demonfire"), 0)
	demonfire.Zone = state.ZStack
	demonfire.X = 2
	e.G.SetZone(state.ZStack, 0, []state.ObjID{demonfire.ID})
	e.G.Stack = []state.ObjID{demonfire.ID}
	// Hellbent is false: its controller has a card in hand.
	hand := e.G.AddObject(card(t, "Name:Card in Hand\nTypes:Sorcery\nOracle:x\n"), 0)
	hand.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{hand.ID})
	target := onBoard(t, e, 1, "Name:Protected\nTypes:Creature\nPT:1/3\nK:Protection from red\nOracle:x\n")
	e.damaging = demonfire.ID
	e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 2})
	e.damaging = 0
	if got := e.G.Obj(target).Damage; got != 0 {
		t.Fatalf("damage = %d, want 0: inactive Demonfire hellbent must not bypass prevention", got)
	}
}

func TestEffectCreatedCantPreventDamage(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	skullcrack := e.G.AddObject(mustCorpusCard(t, reg, "Skullcrack"), 0)
	skullcrack.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{skullcrack.ID})
	e.G.Stack = []state.ObjID{skullcrack.ID}
	sa := skullcrack.Face().SpellAbility()
	// Resolve only the Effect head; its chained damage is unrelated here.
	head := *sa
	head.Sub = nil
	effects.Resolve(e, &effects.Ctx{Source: skullcrack.ID, Controller: 0, SVars: skullcrack.Face().SVars}, &head)
	target := onBoard(t, e, 1, "Name:Protected\nTypes:Creature\nPT:1/3\nK:Protection from red\nOracle:x\n")
	source := onBoard(t, e, 0, "Name:Red Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 2})
	e.damaging = 0
	if got := e.G.Obj(target).Damage; got != 2 {
		t.Fatalf("damage = %d, want 2 while Skullcrack's Effect forbids prevention", got)
	}
}

func TestCompetingDamageReplacementsAskAndRecompute(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	for _, tc := range []struct {
		name        string
		chooseFiery bool
		want        int32
	}{{"triple then plus", true, 8}, {"plus then triple", false, 12}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSeats(t, 2)
			// Drive this event outside an existing priority window so the
			// replacement decision can become the pending decision immediately.
			e.pending = nil
			fiery := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
			plus := onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
				"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Player | ReplaceWith$ D\n"+
				"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
				"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
			source := onBoard(t, e, 0, "Name:Red Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")
			e.damaging = source
			got := e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
			e.damaging = 0
			if got.Kind == events.Damage || e.Pending() == nil || e.Pending().Kind != decision.KReplacement {
				t.Fatalf("damage was not parked for a replacement choice: event=%v pending=%v", got.Kind, e.Pending())
			}
			wantObj := plus
			if tc.chooseFiery {
				wantObj = fiery
			}
			d := e.Pending()
			idx := -1
			for _, o := range d.Options {
				if o.Obj == wantObj {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("replacement source %d absent from options %+v", wantObj, d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatal(err)
			}
			if got := e.G.Players[1].Life; got != 20-tc.want {
				t.Fatalf("life = %d, want %d after %d damage", got, 20-tc.want, tc.want)
			}
		})
	}
}

func TestVigorUsesReplacedDamageAmountAndTarget(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Vigor"))
	target := onBoard(t, e, 0, "Name:Other Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	source := onBoard(t, e, 1, "Name:Damage Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 3})
	e.damaging = 0
	if got := e.G.Obj(target).Damage; got != 0 {
		t.Fatalf("marked damage = %d, want 0 after Vigor replaces it", got)
	}
	if got := e.G.Obj(target).Counter("P1P1"); got != 3 {
		t.Fatalf("+1/+1 counters = %d, want 3 from replaced damage amount", got)
	}
}

func TestDamageReplacementSupportedBodyFamilies(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)

	t.Run("ChangeZone Weeping Angel", func(t *testing.T) {
		e := newSeats(t, 2)
		angel := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Weeping Angel"))
		target := onBoard(t, e, 1, "Name:Target\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.damaging, e.combatDamaging = angel, true
		e.emit(events.Event{Kind: events.Damage, Obj: target, Amount: 2})
		e.damaging, e.combatDamaging = 0, false
		if got := e.G.Obj(target).Zone; got != state.ZLibrary {
			t.Fatalf("target zone = %s, want library", got)
		}
	})

	t.Run("Dig Crumbling Sanctuary", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Crumbling Sanctuary"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
		before := len(e.G.Zone(state.ZLibrary, 0))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 2})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZLibrary, 0)); got != before-2 {
			t.Fatalf("library size = %d, want %d", got, before-2)
		}
		if got := len(e.G.Zone(state.ZExile, 0)); got != 2 {
			t.Fatalf("exile size = %d, want 2", got)
		}
	})

	t.Run("Draw Swans of Bryn Argoll", func(t *testing.T) {
		e := newSeats(t, 2)
		swans := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swans of Bryn Argoll"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
		before := len(e.G.Zone(state.ZHand, 1))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Obj: swans, Amount: 3})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZHand, 1)); got != before+3 {
			t.Fatalf("source controller hand = %d, want %d", got, before+3)
		}
	})

	t.Run("GainLife Purity", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Purity"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
		e.damaging = 0
		if got := e.G.Players[0].Life; got != 23 {
			t.Fatalf("life = %d, want 23", got)
		}
	})

	t.Run("Mill Angel of Suffering", func(t *testing.T) {
		e := newSeats(t, 2)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Angel of Suffering"))
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:3/3\nOracle:x\n")
		before := len(e.G.Zone(state.ZLibrary, 0))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 3})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZLibrary, 0)); got != before-6 {
			t.Fatalf("library size = %d, want %d", got, before-6)
		}
	})

	t.Run("Sacrifice Dralnu", func(t *testing.T) {
		e := newSeats(t, 2)
		dralnu := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Dralnu, Lich Lord"))
		one := onBoard(t, e, 0, "Name:One\nTypes:Creature\nPT:1/1\nOracle:x\n")
		two := onBoard(t, e, 0, "Name:Two\nTypes:Creature\nPT:1/1\nOracle:x\n")
		source := onBoard(t, e, 1, "Name:Source\nTypes:Creature\nPT:2/2\nOracle:x\n")
		before := len(e.G.Zone(state.ZGraveyard, 0))
		e.damaging = source
		e.emit(events.Event{Kind: events.Damage, Obj: dralnu, Amount: 2})
		e.damaging = 0
		// The merge's approved sacrifice semantics ask the player to choose
		// the exact batch whenever more eligible permanents exist than the
		// amount (three creatures, sacrifice two), so the replacement body
		// suspends on the real KChoose and the answer applies the batch.
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 || len(d.Options) != 3 {
			t.Fatalf("Dralnu did not ask its controller to choose two sacrifices: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1, 2}}); err != nil {
			t.Fatal(err)
		}
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != before+2 {
			t.Fatalf("graveyard size = %d, want %d after sacrificing two permanents", got, before+2)
		}
		if e.G.Obj(one).Zone != state.ZGraveyard || e.G.Obj(two).Zone != state.ZGraveyard {
			t.Fatalf("the chosen permanents did not move: %s %s", e.G.Obj(one).Zone, e.G.Obj(two).Zone)
		}
	})

	t.Run("Token Hostility", func(t *testing.T) {
		e := newSeats(t, 2)
		e.G.Tokens = reg.Tokens
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Hostility"))
		source := e.G.AddObject(mustCorpusCard(t, reg, "Lightning Bolt"), 0)
		source.Zone = state.ZStack
		before := len(e.G.Zone(state.ZBattlefield, 0))
		e.damaging = source.ID
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
		e.damaging = 0
		if got := len(e.G.Zone(state.ZBattlefield, 0)); got != before+2 {
			t.Fatalf("battlefield size = %d, want %d after two Hostility tokens", got, before+2)
		}
	})
}

func TestFieryEmancipationModifiesPlaneswalkerDamageBeforeLoyaltyExchange(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	walker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Jace, the Mind Sculptor"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: walker, Counter: "LOYALTY", Amount: 10})
	source := onBoard(t, e, 0, "Name:Red Source\nManaCost:R\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.damaging = source
	e.emit(events.Event{Kind: events.Damage, Obj: walker, Amount: 2})
	e.damaging = 0
	if got := e.G.Obj(walker).Counter("LOYALTY"); got != 4 {
		t.Fatalf("loyalty = %d, want 4 after Fiery triples 2 damage before the 10-6 exchange", got)
	}
	if got := e.G.Obj(walker).Damage; got != 0 {
		t.Fatalf("walker has %d marked damage, want 0", got)
	}
}

func TestDamageReplacementChoiceSuspendsRemainingAbilityChain(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	fiery := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fiery Emancipation"))
	onBoard(t, e, 0, "Name:Plus Two\nTypes:Enchantment\n"+
		"R:Event$ DamageDone | ActiveZones$ Battlefield | ValidSource$ Card.YouCtrl | ValidTarget$ Player | ReplaceWith$ D\n"+
		"SVar:D:DB$ ReplaceEffect | VarName$ DamageAmount | VarValue$ X\n"+
		"SVar:X:ReplaceCount$DamageAmount/Plus.2\nOracle:x\n")
	spell := e.G.AddObject(card(t, "Name:Damage with rider\nManaCost:R\nTypes:Instant\n"+
		"A:SP$ DealDamage | Defined$ Opponent | NumDmg$ 2 | SubAbility$ Gain\n"+
		"SVar:Gain:DB$ GainLife | Defined$ You | LifeAmount$ 5\nOracle:x\n"), 0)
	spell.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
	e.G.Stack = []state.ObjID{spell.ID}
	e.resolveTop()
	if d := e.Pending(); d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("pending = %+v, want replacement order", d)
	}
	if got := e.G.Players[0].Life; got != 20 {
		t.Fatalf("rider ran before replacement answer: life = %d, want 20", got)
	}
	d := e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == fiery {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Fiery Emancipation missing from options: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Players[0].Life; got != 25 {
		t.Fatalf("life after answer = %d, want 25 from exactly one rider", got)
	}
	if got := e.G.Players[1].Life; got != 12 {
		t.Fatalf("opponent life = %d, want 12 after triple-then-plus replacement order", got)
	}
}

func TestCounterReplacementCompetitionLetsAffectedPlayerChoose(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	guile := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Guile"))
	banefire := e.G.AddObject(mustCorpusCard(t, reg, "Banefire"), 1)
	banefire.Zone = state.ZStack
	banefire.X = 5
	counterspell := e.G.AddObject(mustCorpusCard(t, reg, "Counterspell"), 0)
	counterspell.Zone = state.ZStack
	counterspell.Targets = []state.Target{{Obj: banefire.ID}}
	e.G.SetZone(state.ZStack, 1, []state.ObjID{banefire.ID})
	e.G.SetZone(state.ZStack, 0, []state.ObjID{counterspell.ID})
	e.G.Stack = []state.ObjID{banefire.ID, counterspell.ID}
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 1 {
		t.Fatalf("pending = %+v, want Banefire controller's replacement-order choice", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == banefire.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Banefire replacement missing; Guile=%d options=%+v", guile, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Obj(banefire.ID).Zone; got != state.ZStack {
		t.Fatalf("Banefire zone = %s, want stack after its cannot-be-countered replacement applies first", got)
	}
}

func TestHexingSquelcherProtectsYourSpells(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Hexing Squelcher"))
	mine := e.G.AddObject(mustCorpusCard(t, reg, "Counterspell"), 0)
	mine.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{mine.ID})
	e.G.Stack = []state.ObjID{mine.ID}

	theirs := e.G.AddObject(mustCorpusCard(t, reg, "Counterspell"), 1)
	theirs.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 1, []state.ObjID{theirs.ID})
	e.G.Stack = append(e.G.Stack, theirs.ID)

	cs := mustCorpusCard(t, reg, "Counterspell").Faces[0].SpellAbility()
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: mine.ID}}, TargetsOffered: true}, cs)
	if got := e.G.Obj(mine.ID).Zone; got != state.ZStack {
		t.Fatalf("your spell left the stack (zone %s): Hexing Squelcher forbids countering it", got)
	}
	effects.Resolve(e, &effects.Ctx{Controller: 0, Targets: []state.Target{{Obj: theirs.ID}}, TargetsOffered: true}, cs)
	if got := e.G.Obj(theirs.ID).Zone; got == state.ZStack {
		t.Fatalf("the opponent's spell stayed on the stack: the replacement only shields YOUR spells")
	}
}
