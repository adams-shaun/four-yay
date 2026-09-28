package rules

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task prevent-keyword-expansion: the corpus's printed `K:Prevent <english
// sentence>` keyword (9 card files, three sentence shapes, plus one token
// script) expands into the bodyless DamageDone `Prevent$ True` printed
// replacement (cards/kw_prevent.go), which rules' damage dispatch matches
// (ValidTarget$/ValidSource$ Card.Self, IsCombat$) and applies as a full
// prevention. These proofs run the REAL corpus scripts (the relink pipeline
// is what adds the R: lines, so the cache route is exercised too) and pin
// each sentence shape: combat-to, combat-to-and-by, and all-damage-to.

// preventCorpusCard parses the live GPL corpus script for a named card, so
// the expansion under test runs on the actual K:Prevent line (the checked-in
// IR cache is intentionally stale-proof via relink, but the live parse is
// what corpusKeywordCard does for its own carriers).
func preventCorpusCard(t *testing.T, name, rel string) *cards.Card {
	t.Helper()
	c, ds := cards.Parse(filepath.Join("..", ".cards", "cardsfolder", rel))
	if len(ds) != 0 {
		t.Fatalf("parsing %s: %v", rel, ds)
	}
	if c.Faces[0].Name != name {
		t.Fatalf("corpus script %s is %q, want %q", rel, c.Faces[0].Name, name)
	}
	if ds := c.Link(); len(ds) != 0 {
		t.Fatalf("link %s: %v", rel, ds)
	}
	return c
}

// attackerBlockedBy wires seat 0's attacker id against the blocker it is
// blocked by, exactly the state the damage step's own flow leaves after a
// blockers decision (combat_test.go's direct-declaration device).
func attackerBlockedBy(t *testing.T, e *Engine, atk, blocker state.ObjID) {
	t.Helper()
	if e.G.Obj(atk) == nil || e.G.Obj(atk).Zone != state.ZBattlefield {
		t.Fatal("PRECONDITION: attacker not on the battlefield")
	}
	if e.G.Obj(blocker) == nil || e.G.Obj(blocker).Zone != state.ZBattlefield {
		t.Fatal("PRECONDITION: blocker not on the battlefield")
	}
	o := e.G.Obj(atk)
	o.IsAttacking = true
	o.Attacking = 1
	o.SummonSick = false
	o.BlockedBy = append(o.BlockedBy, blocker)
	e.G.NoteBlockers() // written directly, not by DeclareBlockers
}

// TestFogBankKeywordPreventsCombatDamageToIt: the real Fog Bank script
// (`K:Prevent all combat damage that would be dealt to and dealt by
// CARDNAME.`) — a 5/5 attacker blocked by the Fog Bank deals it nothing.
func TestFogBankKeywordPreventsCombatDamageToIt(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardCard(t, e, 0, card(t, "Name:Bludgeon\nManaCost:4 R\nTypes:Creature Ogre\nPT:5/5\nOracle:x\n"))
	fog := onBoardCard(t, e, 1, preventCorpusCard(t, "Fog Bank", "f/fog_bank.txt"))
	attackerBlockedBy(t, e, atk, fog)

	e.dealCombatDamage()

	if got := e.G.Obj(fog).Damage; got != 0 {
		t.Fatalf("Fog Bank damage = %d, want 0: the K:Prevent expansion never prevented", got)
	}
	if got := e.G.Obj(atk).Damage; got != 0 {
		t.Fatalf("attacker damage = %d, want 0 (Fog Bank deals none back)", got)
	}
	if c := countPreventionNotes(e, 5); c != 1 {
		t.Fatalf("logged %d prevention Note(s) with amount 5, want 1", c)
	}
}

// TestFogBankKeywordDoesNotPreventNonCombatDamage pins the IsCombat$ gate the
// "combat damage" wording expands to: a NON-combat hit lands on Fog Bank in
// full. Without this half a wrongly-broad expansion (IsCombat omitted) would
// pass the combat test above just the same.
func TestFogBankKeywordDoesNotPreventNonCombatDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card(t, "Name:Zap\nManaCost:1 R\nTypes:Creature Ogre\nPT:1/1\nOracle:x\n"))
	fog := onBoardCard(t, e, 1, preventCorpusCard(t, "Fog Bank", "f/fog_bank.txt"))

	e.damaging, e.combatDamaging = src, false
	e.emit(events.Event{Kind: events.Damage, Obj: fog, Amount: 3})
	e.damaging, e.combatDamaging = 0, false

	if got := e.G.Obj(fog).Damage; got != 3 {
		t.Fatalf("Fog Bank damage = %d, want 3 (the sentence scopes the prevention to COMBAT damage)", got)
	}
	if c := countPreventionNotes(e, 3); c != 0 {
		t.Fatalf("logged %d prevention Note(s) with amount 3, want 0", c)
	}
}

// TestPreventKeywordDealsByDirectionPrevented: the "and dealt by CARDNAME."
// half. An inline carrier prints the exact Fog Bank sentence but without
// Defender, so it can attack: blocked, its own damage AND the blocker's
// hit-back are both prevented. Also pins the by-direction's own combat
// scoping: a non-combat hit the carrier deals lands in full.
func TestPreventKeywordDealsByDirectionPrevented(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	carrier := onBoardCard(t, e, 0, card(t, "Name:Pacifist Oaf\nManaCost:2 W\nTypes:Creature Giant\nPT:3/3\n"+
		"K:Prevent all combat damage that would be dealt to and dealt by CARDNAME.\nOracle:x\n"))
	guard := onBoardCard(t, e, 1, card(t, "Name:Guard\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n"))
	attackerBlockedBy(t, e, carrier, guard)

	e.dealCombatDamage()

	if got := e.G.Obj(guard).Damage; got != 0 {
		t.Fatalf("blocker damage = %d, want 0: the carrier's damage was not prevented (by-direction)", got)
	}
	if got := e.G.Obj(carrier).Damage; got != 0 {
		t.Fatalf("carrier damage = %d, want 0: the hit-back was not prevented (to-direction)", got)
	}
	if c := countPreventionNotes(e, 3); c != 1 {
		t.Fatalf("by-direction: logged %d prevention Note(s) with amount 3, want 1", c)
	}
	if c := countPreventionNotes(e, 2); c != 1 {
		t.Fatalf("to-direction: logged %d prevention Note(s) with amount 2, want 1", c)
	}

	// The by-direction is combat-scoped too: the same carrier's NON-combat
	// damage lands.
	bystander := onBoardCard(t, e, 1, card(t, "Name:Victim\nManaCost:1 W\nTypes:Creature Soldier\nPT:2/2\nOracle:x\n"))
	e.damaging, e.combatDamaging = carrier, false
	e.emit(events.Event{Kind: events.Damage, Obj: bystander, Amount: 3})
	e.damaging, e.combatDamaging = 0, false
	if got := e.G.Obj(bystander).Damage; got != 3 {
		t.Fatalf("bystander damage = %d, want 3 (the by-direction prevents COMBAT damage only)", got)
	}
}

// TestGuardGomazoaKeywordPreventsCombatDamageToIt: the second corpus carrier
// the brief names, the to-only combat sentence (no "dealt by" half), on the
// real script.
func TestGuardGomazoaKeywordPreventsCombatDamageToIt(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardCard(t, e, 0, card(t, "Name:Bludgeon\nManaCost:4 R\nTypes:Creature Ogre\nPT:5/5\nOracle:x\n"))
	gom := onBoardCard(t, e, 1, preventCorpusCard(t, "Guard Gomazoa", "g/guard_gomazoa.txt"))
	attackerBlockedBy(t, e, atk, gom)

	e.dealCombatDamage()

	if got := e.G.Obj(gom).Damage; got != 0 {
		t.Fatalf("Guard Gomazoa damage = %d, want 0", got)
	}
	if c := countPreventionNotes(e, 5); c != 1 {
		t.Fatalf("logged %d prevention Note(s) with amount 5, want 1", c)
	}
}

// TestChoMannoKeywordPreventsNonCombatDamage: the third sentence shape —
// "Prevent all damage that would be dealt to CARDNAME." (no "combat") —
// prevents a NON-combat hit, which the two combat-scoped shapes above
// deliberately do not.
func TestChoMannoKeywordPreventsNonCombatDamage(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card(t, "Name:Burn\nManaCost:1 R\nTypes:Creature Ogre\nPT:1/1\nOracle:x\n"))
	cho := onBoardCard(t, e, 1, preventCorpusCard(t, "Cho-Manno, Revolutionary", "c/cho_manno_revolutionary.txt"))

	e.damaging, e.combatDamaging = src, false
	e.emit(events.Event{Kind: events.Damage, Obj: cho, Amount: 3})
	e.damaging, e.combatDamaging = 0, false

	if got := e.G.Obj(cho).Damage; got != 0 {
		t.Fatalf("Cho-Manno damage = %d, want 0 (the sentence prevents ALL damage, combat or not)", got)
	}
	if c := countPreventionNotes(e, 3); c != 1 {
		t.Fatalf("logged %d prevention Note(s) with amount 3, want 1", c)
	}
}
