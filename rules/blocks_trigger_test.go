package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The trig:Blocks ticket (agent-20260918T202223Z-efca418d): Forge's
// "whenever [this creature] blocks" trigger mode, pinned end to end on real
// corpus cards in the msh_commander_trigger_test.go style -- the real
// decision chain (askAttackers / submitAttackersOnly / drainCombatPriority /
// submitBlockersOnly), resolveTop, and the clone-replay check. The mode's
// matching object is the pair's BLOCKER and its referents split between the
// blocker and the attacker, which is why it rides the dedicated per-pair
// hook beside checkAttackerBlockedTriggers.

// blocksCombatEngine is combatEngine with the corpus token registry (Savvy
// Hunter's Food mint needs it -- the plain combat engine reports "unknown
// token script") and a chosen active seat, since a block trigger's fixture
// sometimes needs the OTHER seat to be the attacker (the trigger's source
// sits on the defending side).
func blocksCombatEngine(t *testing.T, active state.PlayerID) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := New(seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens}))
	e.G.Active = active
	e.G.Priority = active
	e.G.Step = state.StepDeclareAttackers
	return e
}

// TestSavvyHunterBlocksCreatesFood is the ticket's carrier: Savvy Hunter's
// "Whenever Savvy Hunter attacks or blocks, create a Food token." The attack
// half (trig:Attacks) worked before this ticket; the block half was dead.
// One combat: the Hunter attacks, a Memnite blocks it -- the drain resolves
// the Attacks trigger (pushed at the attacker submission) and the blocker
// declaration queues the Blocks trigger on top, so each half creates one
// Food, the attack half first.
func TestSavvyHunterBlocksCreatesFood(t *testing.T) {
	t.Parallel()
	hunter := mshCorpusCard(t, "Savvy Hunter")
	// The ATTACK half (the control arm, trig:Attacks): the Hunter attacks, a
	// Memnite blocks it; the drain resolves the Attacks trigger pushed at
	// the attacker submission.
	e := blocksCombatEngine(t, 0)
	h := onBoardCard(t, e, 0, hunter)
	e.G.Obj(h).SummonSick = false
	b := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")

	e.askAttackers()
	submitAttackersOnly(t, e, h)
	drainCombatPriority(t, e)
	if n := countTokensNamedOnSeat(t, e, 0, "Food Token"); n != 1 {
		t.Fatalf("Food tokens after the ATTACK half = %d, want 1 (the control arm)", n)
	}
	_ = b
	// The BLOCK half (the ticket's subject): the sides flip -- the Memnite
	// attacks, the Hunter blocks, the pair (Memnite, Hunter) admits the
	// Blocks line's ValidCard$ Card.Self, and the trigger resolves to a
	// Food.
	e2 := blocksCombatEngine(t, 1)
	h2 := onBoardCard(t, e2, 0, hunter)
	m2 := onBoard(t, e2, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e2.G.Obj(m2).SummonSick = false
	e2.askAttackers()
	submitAttackersOnly(t, e2, m2)
	drainCombatPriority(t, e2)
	d := e2.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e2, h2)
	e2.resolveTop()
	if n := countTokensNamedOnSeat(t, e2, 0, "Food Token"); n != 1 {
		t.Fatalf("Food tokens after the BLOCK half = %d, want 1 (the block half was the dead one)", n)
	}
}

// TestBlocksTriggerFiresPerPair pins the per-pair firing and the global-
// enchantment shape (ValidCard$ Creature, the blocker NOT the source): Heat
// of Battle, "Whenever a creature blocks, CARDNAME deals 1 damage to that
// creature's controller." Two attackers each blocked by a distinct creature
// are two trigger instances in one declaration, and the damage lands on the
// BLOCKER's controller, never the attacker's.
func TestBlocksTriggerFiresPerPair(t *testing.T) {
	t.Parallel()
	hob := mshCorpusCard(t, "Heat of Battle")
	e := blocksCombatEngine(t, 0)
	onBoardCard(t, e, 0, hob)
	a1 := onBoardReady(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	a2 := onBoardReady(t, e, 0, "Name:Runeclaw Bear Two\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	b1 := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	b2 := onBoard(t, e, 1, "Name:Memnite Two\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	p0, p1 := e.G.Players[0].Life, e.G.Players[1].Life

	e.askAttackers()
	submitAttackersOnly(t, e, a1, a2)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	b1Idx, b2Idx := -1, -1
	for _, o := range d.Options {
		switch {
		case o.Obj == b1 && o.Attacker == a1:
			b1Idx = o.Index
		case o.Obj == b2 && o.Attacker == a2:
			b2Idx = o.Index
		}
	}
	if b1Idx < 0 || b2Idx < 0 {
		t.Fatalf("block options missing: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 1, Choices: []int{b1Idx, b2Idx}}); err != nil {
		t.Fatal(err)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		// Two instances from one controller in one declaration: CR 603.3b
		// asks that controller to order them.
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
			t.Fatal(err)
		}
	}
	e.resolveTop()
	e.resolveTop()
	if got := e.G.Players[1].Life; got != p1-2 {
		t.Fatalf("blocker controller's life = %d, want %d (one damage per pair)", got, p1-2)
	}
	if got := e.G.Players[0].Life; got != p0 {
		t.Fatalf("attacker controller's life = %d, want %d (the referent is the blocker's controller)", got, p0)
	}
}

// TestBlocksTriggerBlockerReferents pins the two blocker referent roles on
// real cards: Wand of Orcus' Blocks half pumps TriggeredBlockerLKICopy --
// ONLY the blocking bearer gains deathtouch, never the attacker -- and Heat
// of Battle's TriggeredBlockerController damages the blocker's controller,
// never the attacker's.
func TestBlocksTriggerBlockerReferents(t *testing.T) {
	t.Parallel()
	wand := mshCorpusCardPath(t, "Wand of Orcus", "w/wand_of_orcus.txt")
	e := blocksCombatEngine(t, 1)
	h := onBoard(t, e, 0, "Name:Kor Outfitter\nManaCost:1 W\nTypes:Creature Kor Cleric\nPT:2/2\nOracle:x\n")
	w := onBoardCard(t, e, 0, wand)
	e.G.Obj(w).AttachedTo = h // the direct-attach setup event_trigger_test.go uses
	mem := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.G.Obj(mem).SummonSick = false

	e.askAttackers()
	submitAttackersOnly(t, e, mem)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, h)
	e.resolveTop()
	if !e.HasKeyword(h, "Deathtouch") {
		t.Fatal("the BLOCKER (the bearer) did not gain deathtouch from TriggeredBlockerLKICopy")
	}
	if e.HasKeyword(mem, "Deathtouch") {
		t.Fatal("the ATTACKER gained deathtouch -- the role resolved the wrong pair side")
	}

	// Heat of Battle, flipped sides: the enchantment is seat 1's, the
	// attacker seat 0's, the blocker seat 1's -- the 1 damage lands on the
	// blocker's controller (seat 1) and seat 0 is untouched.
	e2 := blocksCombatEngine(t, 0)
	onBoardCard(t, e2, 1, mshCorpusCard(t, "Heat of Battle"))
	atk := onBoardReady(t, e2, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	blk := onBoard(t, e2, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	q0, q1 := e2.G.Players[0].Life, e2.G.Players[1].Life
	e2.askAttackers()
	submitAttackersOnly(t, e2, atk)
	drainCombatPriority(t, e2)
	submitBlockersOnly(t, e2, blk)
	e2.resolveTop()
	if got := e2.G.Players[1].Life; got != q1-1 {
		t.Fatalf("blocker controller's life = %d, want %d", got, q1-1)
	}
	if got := e2.G.Players[0].Life; got != q0 {
		t.Fatalf("attacker controller's life = %d, want %d", got, q0)
	}
}

// TestBlocksTriggerAttachedBearer pins the Card.AttachedBy spelling with a
// real Aura (Contaminated Bond: "Whenever enchanted creature blocks or
// becomes blocked, its controller loses 3 life"): the Blocks half fires when
// the BEARER blocks -- once, for the bearer's pair -- not for the other
// declaration pair a non-bearer blocks, and a declaration where the bearer
// does not block queues nothing.
func TestBlocksTriggerAttachedBearer(t *testing.T) {
	t.Parallel()
	bond := mshCorpusCardPath(t, "Contaminated Bond", "c/contaminated_bond.txt")
	e := blocksCombatEngine(t, 1)
	h := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	oo := onBoard(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bw := onBoardCard(t, e, 0, bond)
	e.G.Obj(bw).AttachedTo = h // the direct-attach setup event_trigger_test.go uses
	aa1 := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	aa2 := onBoard(t, e, 1, "Name:Memnite Two\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.G.Obj(aa1).SummonSick = false
	e.G.Obj(aa2).SummonSick = false
	q0 := e.G.Players[0].Life

	// The bearer is seat 0's, seat 0 is the defender: seat 1 declares both
	// attackers and seat 0 blocks -- the bearer (h) blocks aa1 and the
	// non-bearer (oo) blocks aa2. Only the bearer's pair fires: seat 0
	// loses 3, once.
	e.askAttackers()
	submitAttackersOnly(t, e, aa1, aa2)
	drainCombatPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	hIdx, oIdx := -1, -1
	for _, o := range d.Options {
		switch {
		case o.Obj == h && o.Attacker == aa1:
			hIdx = o.Index
		case o.Obj == oo && o.Attacker == aa2:
			oIdx = o.Index
		}
	}
	if hIdx < 0 || oIdx < 0 {
		t.Fatalf("block options missing: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{hIdx, oIdx}}); err != nil {
		t.Fatal(err)
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != q0-3 {
		t.Fatalf("bearer's controller's life = %d, want %d (exactly the bearer's pair fired)", got, q0-3)
	}

	// The negative half: a declaration where the bearer does NOT block
	// queues nothing.
	e3 := blocksCombatEngine(t, 1)
	h3 := onBoard(t, e3, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bw3 := onBoardCard(t, e3, 0, bond)
	e3.G.Obj(bw3).AttachedTo = h3
	a3 := onBoard(t, e3, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e3.G.Obj(a3).SummonSick = false
	e3.askAttackers()
	submitAttackersOnly(t, e3, a3)
	drainCombatPriority(t, e3)
	if d := e3.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	if err := e3.Submit(decision.Intent{Seq: e3.Pending().Seq, Player: 0, Choices: nil}); err != nil {
		t.Fatal(err)
	}
	if got := len(e3.G.Stack); got != 0 {
		t.Fatalf("a no-bearer-block declaration left %d stack entries, want 0", got)
	}
}

// TestBlocksTriggerValidBlockedGatesAttacker pins ValidBlocked$ -- the param
// that filters the pair's ATTACKER. Rashka the Slayer ("Whenever CARDNAME
// blocks one or more black creatures, CARDNAME gets +1/+2 until end of
// turn") is the working-body pin: the pump lands when the attacker is black
// and never when it is not. Goblin Cadets' two spellings on ONE face sharing
// one Execute$ SVar pin the gate's exclusion side: the blocks half
// (ValidCard$ Card.Self | ValidBlocked$ Creature) fires when Cadets BLOCKS
// and the becomes-blocked half (ValidCard$ Creature | ValidBlocked$
// Card.Self, Secondary$ True) fires when Cadets BECOMES blocked -- each
// fixture yields exactly ONE trigger, so the attacker filter is what
// excludes the sibling spelling in both directions. (The bodies'
// control-change outcome is not asserted: DB$ GainControl's player-target
// spelling -- ValidTgts$ Opponent with no NewController$ -- is a separate,
// pre-existing engine gap; see this round's report.)
func TestBlocksTriggerValidBlockedGatesAttacker(t *testing.T) {
	t.Parallel()
	rashka := mshCorpusCard(t, "Rashka the Slayer")
	black := "Name:Black Rat\nManaCost:B\nTypes:Creature Rat\nPT:1/1\nOracle:x\n"
	mem := "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n"

	// The admitting shape: Rashka blocks a BLACK attacker -> +1/+2.
	e := blocksCombatEngine(t, 1)
	r := onBoardCard(t, e, 0, rashka)
	bat := onBoard(t, e, 1, black)
	e.G.Obj(bat).SummonSick = false
	e.askAttackers()
	submitAttackersOnly(t, e, bat)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, r)
	e.resolveTop()
	if got := e.Derived(r).Power; got != 4 {
		t.Fatalf("Rashka power after blocking a black creature = %d, want 4 (3+1)", got)
	}

	// The excluding shape: Rashka blocks a NON-black attacker -> no pump.
	e2 := blocksCombatEngine(t, 1)
	r2 := onBoardCard(t, e2, 0, rashka)
	m0 := onBoard(t, e2, 1, mem)
	e2.G.Obj(m0).SummonSick = false
	e2.askAttackers()
	submitAttackersOnly(t, e2, m0)
	drainCombatPriority(t, e2)
	if d := e2.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e2, r2)
	if got := len(e2.G.Stack); got != 0 {
		t.Fatalf("a non-black attacker's block queued %d triggers, want 0 (ValidBlocked$ excluded)", got)
	}

	// Cadets' becomes-blocked spelling: Cadets attacks, a Memnite blocks it
	// -- exactly one trigger, admitted through ValidBlocked$ Card.Self while
	// the blocks spelling's ValidCard$ Card.Self excludes itself (the
	// blocker is the Memnite).
	cadets := mshCorpusCard(t, "Goblin Cadets")
	e3 := blocksCombatEngine(t, 0)
	c3 := onBoardCard(t, e3, 0, cadets)
	e3.G.Obj(c3).SummonSick = false
	blk := onBoard(t, e3, 1, mem)
	e3.askAttackers()
	submitAttackersOnly(t, e3, c3)
	drainCombatPriority(t, e3)
	if d := e3.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e3, blk)
	if got := len(e3.G.Stack); got != 1 {
		t.Fatalf("become-blocked stack depth = %d, want 1 (the attacker gate admitted the pair)", got)
	}

	// And the mirror: Cadets BLOCKS (a Memnite attacks it) -- exactly one
	// trigger again, the becomes-blocked spelling now gated out by
	// ValidBlocked$ Card.Self (the attacker is the Memnite, not Cadets).
	e4 := blocksCombatEngine(t, 1)
	c4 := onBoardCard(t, e4, 0, cadets)
	atk4 := onBoard(t, e4, 1, mem)
	e4.G.Obj(atk4).SummonSick = false
	e4.askAttackers()
	submitAttackersOnly(t, e4, atk4)
	drainCombatPriority(t, e4)
	if d := e4.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e4, c4)
	if got := len(e4.G.Stack); got != 1 {
		t.Fatalf("blocks-half stack depth = %d, want 1 (ValidBlocked$ Card.Self gated the second spelling)", got)
	}
}

// TestGodsendBlocksOffersTheAttacker pins the Remembered-is-the-ATTACKER
// mapping through Godsend's Blocks half (DB$ ChooseCard |
// DefinedCards$ TriggeredAttackers): "one of those creatures" for the blocks
// half is the attacker the bearer blocked, so the choice pool holds exactly
// the attacker and the answer exiles it.
func TestGodsendBlocksOffersTheAttacker(t *testing.T) {
	t.Parallel()
	godsend := mshCorpusCardPath(t, "Godsend", "g/godsend.txt")
	e := blocksCombatEngine(t, 1)
	h := onBoard(t, e, 0, "Name:Kor Outfitter\nManaCost:1 W\nTypes:Creature Kor Cleric\nPT:2/2\nOracle:x\n")
	gw := onBoardCard(t, e, 0, godsend)
	e.G.Obj(gw).AttachedTo = h // the direct-attach setup event_trigger_test.go uses
	mem := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.G.Obj(mem).SummonSick = false

	e.askAttackers()
	submitAttackersOnly(t, e, mem)
	drainCombatPriority(t, e)
	if d := e.Pending(); d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("expected a blockers decision, got %+v", d)
	}
	submitBlockersOnly(t, e, h)
	e.resolveTop() // the queued Blocks trigger is on the stack; resolution poses the OptionalDecider ask
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the OptionalDecider ask, got %+v", d)
	}
	submitChoices(t, e, 0) // yes, exile one of those creatures
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the ChooseCard ask, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != mem {
		t.Fatalf("choice pool = %+v, want exactly the ATTACKER %d", d.Options, mem)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Obj(mem).Zone; got != state.ZExile {
		t.Fatalf("attacker zone = %s, want exile", got)
	}
}

// TestBlocksTriggerCloneReplaysExactly is the msh-style determinism pin: the
// whole block flow driven through the same intents on a Clone produces the
// identical event log.
func TestBlocksTriggerCloneReplaysExactly(t *testing.T) {
	t.Parallel()
	hunter := mshCorpusCard(t, "Savvy Hunter")
	e := blocksCombatEngine(t, 1)
	h := onBoardCard(t, e, 0, hunter)
	b := onBoard(t, e, 1, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	e.G.Obj(b).SummonSick = false

	e.askAttackers()
	submitAttackersOnly(t, e, b)
	drainCombatPriority(t, e)
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		submitBlockersOnly(t, eng, h)
		eng.resolveTop()
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the Blocks resolution")
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Food Token"); n != 1 {
		t.Fatalf("Food tokens = %d, want 1", n)
	}
}

// TestBlocksModeEligibilityMask pins triggerModeEvents' Blocks row and the
// support declaration (the Exerted pins' shape): the mode is eligible on
// DeclareBlockers only, and cmd/forgec's report reads effects.Supported.
func TestBlocksModeEligibilityMask(t *testing.T) {
	t.Parallel()
	m := triggerModeEvents("Blocks")
	if !m.allows(events.DeclareBlockers) {
		t.Fatal("triggerModeEvents(Blocks) does not allow events.DeclareBlockers")
	}
	for _, k := range []events.Kind{events.DeclareAttackers, events.Damage, events.MoveZone, events.Tap, events.PutOnStack, events.StepChange} {
		if m.allows(k) {
			t.Fatalf("triggerModeEvents(Blocks) unexpectedly allows kind %d", k)
		}
	}
	if !effects.Supported()["trig:Blocks"] {
		t.Fatal("trig:Blocks is not declared supported (effects.RegisterNonAPI)")
	}
}
