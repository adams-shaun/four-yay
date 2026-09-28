package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// castTargetsFor chooses the cast option for id out of the current priority
// decision and advances the cast through its put-on-stack, returning the
// target decision's options WITHOUT answering them (an empty slice when the
// cast completed with no target decision, which is what a no-legal-target
// fizzle -- a target-hungry spell whose only legal target is protected --
// does).
func castTargetsFor(t *testing.T, e *Engine, id state.ObjID) []decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		return d.Options
	}
	return nil
}

// TestProtectionFromBlueBlocksTargetingBlockingAndDamage is Task 15's end-to-
// end cycle on one protected permanent (Goblin Piledriver's shape, in
// miniature): a blue source's damage to it is prevented while a red one's
// lands; a blue creature cannot block it; a blue spell offers it no target
// while a red one does; and the game still replays exactly. The direct
// e.damaging/e.canBlock reads mirror the engine-internal hooks the brief
// exposes; the cast path goes through real decisions so the protection
// check in askTarget is exercised through play, not just unit-called.
func TestProtectionFromBlueBlocksTargetingBlockingAndDamage(t *testing.T) {
	pile := "Name:Piledriver\nManaCost:1 R\nTypes:Creature Goblin Warrior\nPT:1/2\nK:Protection from blue\nOracle:x\n"
	shock := "Name:BlueShock\nManaCost:U\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n"
	redShock := "Name:RedShock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 2\nOracle:x\n"
	e, cfg, pd := newFixtureDeck(t, 71, pile, shock, redShock)
	e.emit(events.Event{Kind: events.MoveZone, Obj: pd, From: state.ZHand, To: state.ZBattlefield})

	// Damage: a blue source's damage is prevented; a red source's is not.
	blueGuy := putToken(t, e, 1, "Name:Merfolk\nManaCost:U\nTypes:Creature Merfolk\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	e.damaging = blueGuy
	e.emit(events.Event{Kind: events.Damage, Obj: pd, Amount: 2})
	e.damaging = 0
	if e.G.Obj(pd).Damage != 0 {
		t.Fatal("blue damage was not prevented")
	}
	redGuy := putToken(t, e, 1, "Name:Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	e.damaging = redGuy
	e.emit(events.Event{Kind: events.Damage, Obj: pd, Amount: 1})
	e.damaging = 0
	if e.G.Obj(pd).Damage != 1 {
		t.Fatalf("red damage was prevented too: Damage=%d, want 1", e.G.Obj(pd).Damage)
	}

	// Blocking: the blue creature cannot block it; a red one can.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{pd}})
	if e.canBlock(blueGuy, pd) {
		t.Fatal("a blue creature blocked a creature with protection from blue")
	}
	if !e.canBlock(redGuy, pd) {
		t.Fatal("a red creature should be able to block")
	}

	// Targeting: a blue spell cannot target it, a red one can.
	shockID := addToHand(t, e, 0, shock)
	redShockID := addToHand(t, e, 0, redShock)
	addMana(t, e, 0, "UR")
	blueOpts := castTargetsFor(t, e, shockID)
	// The blue spell offers the OTHER (unprotected) creatures on the
	// battlefield, but the protection-from-blue piledriver must never be one.
	for _, o := range blueOpts {
		if o.Obj == pd {
			t.Fatalf("blue spell offered the protection-from-blue creature as a target: %+v", blueOpts)
		}
	}
	// Answer blueShock's pending target decision (to a non-protected
	// creature -- blueOpts never contained pd) so priority returns and the
	// red spell below can be cast on a clear stack.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, 0)
	}
	redOpts := castTargetsFor(t, e, redShockID)
	var sawPD bool
	for _, o := range redOpts {
		if o.Obj == pd {
			sawPD = true
		}
	}
	if !sawPD {
		t.Fatalf("red spell omitted the piledriver: %+v", redOpts)
	}
	replayCheck(t, e, cfg)
}

// TestProtectedBlockerTakesNoDeathtouchAndGivesNoLifelink is Task 15 fix
// round 1's Critical C1 regression pin. Protection stops a creature from
// being blocked BY a blue source, not from blocking one, so a creature with
// protection from blue blocking a blue Lifelink+Deathtouch attacker is a
// normal board state. The damage is prevented, and the riders that used to
// ride along regardless -- the Deathtouched counter (which under CR 704.5g
// is lethal to the very permanent protection exists to save) and the lifelink
// life gain (CR 702.15a: lifelink triggers only on damage actually dealt) --
// must be skipped too.
func TestProtectedBlockerTakesNoDeathtouchAndGivesNoLifelink(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	atk := onBoardReady(t, e, 0, "Name:Merfolk\nManaCost:1 U\nTypes:Creature Merfolk\nPT:2/2\nK:Lifelink\nK:Deathtouch\nOracle:x\n")
	blk := onBoard(t, e, 1, "Name:Piledriver\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/2\nK:Protection from blue\nOracle:x\n")

	start := e.G.Players[0].Life
	e.askAttackers()
	submitAttackers(t, e, atk)
	submitBlockers(t, e, blk)

	bo := e.G.Obj(blk)
	if bo.Zone != state.ZBattlefield {
		t.Fatalf("blocker zone = %s, want battlefield (prevented damage killed nothing)", bo.Zone)
	}
	if bo.Damage != 0 {
		t.Fatalf("blocker damage = %d, want 0 (the damage was prevented)", bo.Damage)
	}
	if n := bo.Counter("Deathtouched"); n != 0 {
		t.Fatalf("blocker Deathtouched counter = %d, want 0 (deathtouch marker must not ride prevented damage)", n)
	}
	if e.G.Players[0].Life != start {
		t.Fatalf("attacker controller life = %d, want %d (lifelink must not gain on prevented damage)",
			e.G.Players[0].Life, start)
	}
}

// TestAbilityFizzlesWhenTargetGainsProtectionBeforeResolving is Task 15 fix
// round 1's Important I1 pin for the legalTargets filter (stack.go's CR
// 608.2b recheck at resolution). legalTargets was mutation-tested to be
// uncovered -- no test exercised dropping a target that became PROTECTED
// between being chosen and the ability resolving (the existing pin covered
// only a target that died). Here Reflex Sentinel's ValidTgts$ trigger takes
// the alive rat as its target during the drain; the drain then grants seat 0
// priority while the ability sits on the stack, and seat 0 casts Ward
// Blessing -- an instant that resolves of its own (it entered above the
// trigger) and grants the rat "Protection from artifacts" until end of turn.
// When the sentinel ability's turn comes, its only target is now protected
// from the sentinel (an artifact), legalTargets finds zero legal targets, and
// the ability fizzles to exile (CR 608.2b, CR 702.16c) without ever running
// its damage script.
func TestAbilityFizzlesWhenTargetGainsProtectionBeforeResolving(t *testing.T) {
	t.Parallel()
	mountain := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	blessing := card(t, "Name:Ward Blessing\nManaCost:1 W\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | KW$ Protection from artifacts\nOracle:x\n")
	e := handEngine(t, mountain, blessing)
	sentinel := onBoard(t, e, 0, damageWatcherSrc)
	rat := onBoard(t, e, 1, fieldRatSrc)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 2})
	e.askPriority(0)

	// Play the Mountain -- Reflex Sentinel's ChangesZone trigger fires and,
	// in its drain, asks seat 0 for a target: the alive, still-unprotected rat.
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority before playing the land, got %+v", d)
	}
	playLand := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" {
			playLand = opt.Index
		}
	}
	if playLand < 0 {
		t.Fatalf("no play_land option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{playLand}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the sentinel trigger's target decision, got %+v", d)
	}
	ratChoice := -1
	for _, opt := range d.Options {
		if opt.Kind == "permanent" && opt.Obj == rat {
			ratChoice = opt.Index
		}
	}
	if ratChoice < 0 {
		t.Fatalf("no target option for the rat at ask time: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ratChoice}}); err != nil {
		t.Fatalf("submit sentinel target: %v", err)
	}
	abilityID := e.G.Stack[len(e.G.Stack)-1]
	if len(e.G.Stack) != 1 || e.G.Obj(abilityID).Ability == nil {
		t.Fatalf("stack = %v, want the sentinel's targeted ability on it", e.G.Stack)
	}

	// Seat 0 casts Ward Blessing at the SAME rat -- an instant, so it enters
	// the stack above the trigger, resolves first, and grants the rat
	// "Protection from artifacts" until end of turn.
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the blessing's target decision, got %+v", d)
	}
	ratChoice = -1
	for _, opt := range d.Options {
		if opt.Kind == "permanent" && opt.Obj == rat {
			ratChoice = opt.Index
		}
	}
	if ratChoice < 0 {
		t.Fatalf("no target option for the blessing's rat: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ratChoice}}); err != nil {
		t.Fatalf("submit blessing target: %v", err)
	}
	// The blessing now sits on top of the sentinel ability. Answering a
	// spell's target leaves the engine holding priority for BOTH seats in
	// pass-around -- only once every player passes does the top object
	// resolve -- so the single bounded drain below passes through both
	// seats, resolves the blessing (arming the rat with protection from the
	// artifact sentinel) and then lets the sentinel ability resolve (and
	// fizzle).
	passUntilStackEmpty(t, e, 60)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want empty -- the sentinel ability did not leave it", e.G.Stack)
	}
	if got := e.G.Obj(rat).Zone; got != state.ZBattlefield {
		t.Fatalf("rat zone = %s, want battlefield (the blessing only grants protection; it does not hurt the rat)", got)
	}
	// The grant landed: the rat is protected from the artifact sentinel while
	// the blessing's until-end-of-turn effect is still live, which is exactly
	// the protection that made the sentinel's target illegal at resolution.
	if !e.protectedFrom(rat, sentinel) {
		t.Fatalf("rat not protected from the sentinel after the blessing resolved")
	}
	// The sentinel ability fizzled at RESOLUTION time: its target became
	// protected before its turn to resolve. It moves to exile, never resolving
	// and never dealing its damage.
	if got := e.G.Obj(abilityID).Zone; got != state.ZExile {
		t.Fatalf("ability zone = %s, want exile -- the resolution-time fizzle must land it there", got)
	}
	fizzleMove := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == abilityID && strings.Contains(ev.Text, "fizzled: no legal targets") {
			fizzleMove = true
		}
	}
	if !fizzleMove {
		t.Fatal("no fizzled: no legal targets move records the sentinel ability being countered at resolution")
	}
	if n := countKind(e.L.Events, events.Resolve, abilityID); n != 0 {
		t.Fatalf("logged %d Resolve events for the fizzled ability, want 0 -- a fizzle never "+
			"runs its script", n)
	}
	if got := e.G.Obj(rat).Damage; got != 0 {
		t.Fatalf("rat damage = %d, want 0 (a resolving sentinel hit would have marked it)", got)
	}
}

// TestAbilityTargetingWithheldFromProtectedPermanent is Task 15 fix round
// 1's Critical C2 pin. askTarget used to filter protection against the
// ability STACK OBJECT (minted with no Face by events.Apply's AbilityPush),
// so effects.ColorsOf saw "" and the filter never fired for an ability: a
// blue Zapper with a targeted activated ability happily offered its
// protection-from-blue Piledriver as a target, then legalTargets fizzled it
// at resolution -- the player paid mana + tap for an option the engine then
// rejected, exiling the ability for nothing. protectionSource resolves the
// stack object to its Source permanent, so both call sites now agree on
// "the source" and the protected permanent is withheld at ask time.
func TestAbilityTargetingWithheldFromProtectedPermanent(t *testing.T) {
	src := "Name:Zapper\nManaCost:U\nTypes:Creature Merfolk Wizard\nPT:1/1\n" +
		"A:AB$ DealDamage | Cost$ U | ValidTgts$ Creature | NumDmg$ 2 | SpellDescription$ deals 2.\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 79, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	pile := putToken(t, e, 1, "Name:Piledriver\nManaCost:1 R\nTypes:Creature Goblin\nPT:1/2\nK:Protection from blue\nOracle:x\n", state.ZBattlefield)
	other := putToken(t, e, 1, "Name:Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "U")
	e.Advance()
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the ability's target decision, got %+v", d)
	}
	sawOther := false
	for _, o := range d.Options {
		if o.Obj == pile {
			t.Fatalf("a blue ability offered the protection-from-blue permanent as a target: %+v", d.Options)
		}
		if o.Obj == other {
			sawOther = true
		}
	}
	if !sawOther {
		t.Fatalf("the unprotected creature should be offered but was not: %+v", d.Options)
	}
}

// of the protectedFrom predicate: protection GRANTED by a resolved effect
// counts the same as printed (a transient DB$ Pump grant, not just a printed
// K: line), and a Devoid permanent is colourless, so "Protection from red"
// does not protect from a colourless Devoid source even though protection
// from red normally protects from anything red.
func TestGrantedProtectionCountsAndDevoidIsColourless(t *testing.T) {
	e, _, bear := newFixtureDeck(t, 72, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	effects.Resolve(e, &effects.Ctx{Source: bear, Controller: 0, Targets: []state.Target{{Obj: bear}}},
		&cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{"KW": "Protection from red"}})
	red := putToken(t, e, 1, "Name:Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	devoid := putToken(t, e, 1, "Name:Breaker\nManaCost:6 G\nTypes:Creature Eldrazi\nPT:5/7\nK:Devoid\nOracle:x\n", state.ZBattlefield)
	if !e.protectedFrom(bear, red) || e.protectedFrom(bear, devoid) {
		t.Fatal("granted protection from red must apply to a red source and not to a devoid one")
	}
}

// TestGrantedProtectionFromEachColor protects against every coloured source,
// but not a colourless artifact source (CR 702.16c). The grant path is used
// deliberately: this catches a quality matcher that only handles printed K:
// keywords while still proving the bearer is on the battlefield.
func TestGrantedProtectionFromEachColor(t *testing.T) {
	e, _, bear := newFixtureDeck(t, 73, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	effects.Resolve(e, &effects.Ctx{Source: bear, Controller: 0, Targets: []state.Target{{Obj: bear}}},
		&cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{"KW": "Protection from each color"}})
	bo := e.G.Obj(bear)
	if bo == nil || bo.Zone != state.ZBattlefield {
		t.Fatalf("bearer is not on the battlefield after grant: %+v", bo)
	}
	if !e.protectedFrom(bear, bear) {
		t.Fatal("bearer did not receive the each-color protection keyword")
	}

	red := putToken(t, e, 1, "Name:RedSource\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	if !e.protectedFrom(bear, red) {
		t.Fatal("each-color protection did not match the red source")
	}
	e.damaging = red
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 1})
	e.damaging = 0
	if got := e.G.Obj(bear).Damage; got != 0 {
		t.Fatalf("red damage was not prevented: Damage=%d, want 0", got)
	}

	artifact := putToken(t, e, 1, "Name:ArtifactSource\nTypes:Artifact\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	if e.protectedFrom(bear, artifact) {
		t.Fatal("each-color protection incorrectly matched a colourless artifact source")
	}
	e.damaging = artifact
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 1})
	e.damaging = 0
	if got := e.G.Obj(bear).Damage; got != 1 {
		t.Fatalf("artifact damage was prevented: Damage=%d, want 1", got)
	}
}
