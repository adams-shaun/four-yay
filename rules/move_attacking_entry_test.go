package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the Attacking$ True entry rider on the REAL corpus move
// bodies (the brief's flagship carriers), one per entry path: the hand
// movers (Paladin Elizabeth Taggerdy, Preeminent Captain), the graveyard
// object path (Thunderkin Awakener), the self-return AttackersDeclared
// trigger (Warcry Phoenix) and the Dig window (Jet, Rebel Leader). Every
// test drives REAL combat from the attackers declaration through the combat
// damage step, so the entered permanent is proven to participate in that
// combat's damage -- not merely to carry the IsAttacking fields. The entry
// fields are asserted at the stop point (the entered object on the
// battlefield, the trigger resolution just settled) because EndCombatReset
// clears IsAttacking at combat's end.

// attackingEntryMove parks a named seat-p battlefield card in to (hand,
// graveyard, library) with a logged MoveZone -- a real move the replay folds.
func attackingEntryMove(t *testing.T, e *Engine, seat state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	id := findBattlefield(t, e, seat, name, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: to})
	if o := e.G.Obj(id); o == nil || o.Zone != to {
		t.Fatalf("%q is %+v, want it parked in %v", name, o, to)
	}
	return id
}

// onBattlefield reports whether id is a battlefield object.
func onBattlefield(e *Engine, id state.ObjID) bool {
	o := e.G.Obj(id)
	return o != nil && o.Zone == state.ZBattlefield
}

// entryDrive resolves the queued attack trigger(s), handing every
// mid-resolution ask to onAsk, and returns once stop holds, combat has
// reached its second main phase, or the game ended. Priorities are passed
// and trigger-order asks answered with the offered order.
func entryDrive(t *testing.T, e *Engine, onAsk func(*decision.Decision), stop func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if e.G.Over || e.G.Step == state.StepMain2 || (stop != nil && stop()) {
			return
		}
		d := e.Pending()
		if d == nil {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass: %v", err)
			}
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		default:
			onAsk(d)
		}
	}
	t.Fatal("combat did not settle within the ask budget")
}

// chooseObjOption submits the ask's first option whose Obj is id.
func chooseObjOption(t *testing.T, e *Engine, d *decision.Decision, id state.ObjID) {
	t.Helper()
	for _, o := range d.Options {
		if o.Obj == id {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit option for obj %d: %v", id, err)
			}
			return
		}
	}
	t.Fatalf("no option for object %d in %+v", id, d.Options)
}

// chooseKindOption submits the ask's first option whose Kind is kind.
func chooseKindOption(t *testing.T, e *Engine, d *decision.Decision, kind string) {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == kind {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit %s option: %v", kind, err)
			}
			return
		}
	}
	t.Fatalf("no %q option in %+v", kind, d.Options)
}

// assertEnteredAttacking pins the entry state on an object that must now sit
// on the battlefield, tapped and attacking defender, having emitted exactly
// one TokenAttacks event (the reused entry event; DeclareAttackers must never
// be emitted for a mid-combat entry).
func assertEnteredAttacking(t *testing.T, e *Engine, id state.ObjID, defender state.PlayerID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("entered object = %+v, want it on the battlefield", o)
	}
	if !o.Tapped {
		t.Fatalf("%s entered UNTAPPED, want tapped", o.Face().Name)
	}
	if !o.IsAttacking || o.Attacking != defender {
		t.Fatalf("%s IsAttacking=%v Attacking=%d, want attacking defender %d",
			o.Face().Name, o.IsAttacking, o.Attacking, defender)
	}
	attacks := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenAttacks && ev.Obj == id {
			attacks++
		}
	}
	if attacks != 1 {
		t.Fatalf("logged %d TokenAttacks for %d, want exactly 1", attacks, id)
	}
}

// TestElizabethTaggerdyEntersCardTappedAndAttacking is the hand mover's
// flagship: the battalion trigger's DB$ ChangeZone Origin$ Hand Tapped$
// Attacking$ puts a creature card from the hand onto the battlefield TAPPED
// and ATTACKING, and the entered creature deals its damage in that combat
// (defender's life drops by all four attackers' power).
func TestElizabethTaggerdyEntersCardTappedAndAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Paladin Elizabeth Taggerdy"},
		[]string{
			"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
			"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
			"Name:HandBear\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		}, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	elizabeth := findBattlefield(t, e, 0, "Paladin Elizabeth Taggerdy", 0)
	bear1 := findBattlefield(t, e, 0, "Bear", 0)
	bear2 := findBattlefield(t, e, 0, "Bear", 1)
	handBear := attackingEntryMove(t, e, 0, "HandBear", state.ZHand)
	if e.G.Players[1].Life != 20 {
		t.Fatalf("defender life = %d, want 20 before combat", e.G.Players[1].Life)
	}

	e.askAttackers()
	submitAttackers(t, e, elizabeth, bear1, bear2)
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KChoose && d.ResumeKind == "hand_move" {
			chooseObjOption(t, e, d, handBear)
			return
		}
		t.Fatalf("unexpected ask during Elizabeth's resolution: %+v", d)
	}, func() bool { return onBattlefield(e, handBear) })
	assertEnteredAttacking(t, e, handBear, 1)

	// 3 (Elizabeth) + 2 + 2 (the bears) + 2 (the entered HandBear): the
	// entered permanent was part of the damage assignment.
	entryDrive(t, e, func(d *decision.Decision) {
		t.Fatalf("unexpected ask after the entry: %+v", d)
	}, nil)
	if got := e.G.Players[1].Life; got != 11 {
		t.Fatalf("defender life = %d, want 11 (all four attackers dealt damage)", got)
	}
	replayCheck(t, e, cfg)
}

// TestPreeminentCaptainSoldierEntersAttacking pins the ChangeType$-filtered
// hand mover: the Captain's Soldier (a real hand card matching
// Creature.Soldier+YouCtrl) enters tapped and attacking and deals its damage.
func TestPreeminentCaptainSoldierEntersAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Preeminent Captain"},
		[]string{"Name:FootSoldier\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/1\nOracle:x\n"}, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	captain := findBattlefield(t, e, 0, "Preeminent Captain", 0)
	soldier := attackingEntryMove(t, e, 0, "FootSoldier", state.ZHand)

	e.askAttackers()
	submitAttackers(t, e, captain)
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KChoose && d.ResumeKind == "hand_move" {
			chooseObjOption(t, e, d, soldier)
			return
		}
		t.Fatalf("unexpected ask during the Captain's resolution: %+v", d)
	}, func() bool { return onBattlefield(e, soldier) })
	assertEnteredAttacking(t, e, soldier, 1)

	entryDrive(t, e, func(d *decision.Decision) {
		t.Fatalf("unexpected ask after the entry: %+v", d)
	}, nil)
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("defender life = %d, want 17 (Captain 2 + entered Soldier 1)", got)
	}
	replayCheck(t, e, cfg)
}

// TestYoreTillerNephilimReturnsCreatureTappedAndAttacking is the graveyard
// object path: the attack trigger's ChangeZone returns a creature card from
// the graveyard TAPPED and ATTACKING, it is blockable, and its blocked
// damage kills it against the blocker.
//
// It stands in for the brief's named Alesha coverage. That coverage was
// unreachable through a gap since closed by agent-20260922T232740Z-cf0357bb:
// her trigger's Cost$ WB WB used to be decline-only at the triggered-cost
// window (Cost.Priceable() rejected the hybrid pips and no announcement ask
// existed). The window now poses the CR 601.2b pip election and pays the
// resolved cost; rules/alesha_hybrid_trigger_test.go drives that real card
// end to end. This test keeps the cost-free Yore-Tiller carrier because it
// isolates the graveyard object path itself from any cost window.
// Thunderkin Awakener, the other object-path carrier, was blocked by its
// ValidTgts$ ...toughnessLTX SVar-X comparison resolving no target; that was
// fixed by 7454592e ("fix(rules): resolve trigger target X from source
// SVar") and is now pinned by
// rules/thunderkin_awakener_test.go
// TestThunderkinAwakenerReturnsToughnessLessElementalTappedAndAttacking
// (agent-20260922T221917Z-aa93a144). Yore-Tiller carries the same inlined
// object-loop move with a literal spec and no cost, so the path under test is
// identical.
func TestYoreTillerNephilimReturnsCreatureTappedAndAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Yore-Tiller Nephilim"},
		[]string{"Name:GravBear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"},
		nil, []string{"Name:BlockBear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	neph := findBattlefield(t, e, 0, "Yore-Tiller Nephilim", 0)
	blocker := findBattlefield(t, e, 1, "BlockBear", 0)
	gravBear := attackingEntryMove(t, e, 0, "GravBear", state.ZGraveyard)

	e.askAttackers()
	submitAttackers(t, e, neph)
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KTarget {
			chooseObjOption(t, e, d, gravBear)
			return
		}
		t.Fatalf("unexpected ask during the Nephilim's resolution: %+v", d)
	}, func() bool { return onBattlefield(e, gravBear) })
	assertEnteredAttacking(t, e, gravBear, 1)

	// Blockable: seat 1's bear blocks the RETURNED bear; in the damage step
	// the blocked 2/2s kill each other and only the unblocked Nephilim's 2
	// reaches the defender.
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KBlockers {
			for _, o := range d.Options {
				if o.Obj == blocker && o.Attacker == gravBear {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
						t.Fatalf("submit block: %v", err)
					}
					return
				}
			}
			t.Fatalf("no block pair (%d blocks %d) in %+v", blocker, gravBear, d.Options)
		}
		t.Fatalf("unexpected ask during blockers: %+v", d)
	}, nil)
	if e.G.Players[1].Life != 18 {
		t.Fatalf("defender life = %d, want 18 (only the unblocked Nephilim's 2 got through)", e.G.Players[1].Life)
	}
	if onBattlefield(e, gravBear) {
		t.Fatal("the blocked returned bear survived its own blocked combat damage")
	}
	if onBattlefield(e, blocker) {
		t.Fatalf("blocker survived a blocked 2/2 attacker: %+v", e.G.Obj(blocker))
	}
	replayCheck(t, e, cfg)
}

// TestWarcryPhoenixReturnsSelfTappedAndAttacking is the self-return
// AttackersDeclared trigger: the Phoenix returns ITSELF from the graveyard
// tapped and attacking and deals its damage.
func TestWarcryPhoenixReturnsSelfTappedAndAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Warcry Phoenix"},
		[]string{
			"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
			"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
			"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		}, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	phoenix := attackingEntryMove(t, e, 0, "Warcry Phoenix", state.ZGraveyard)
	bears := []state.ObjID{
		findBattlefield(t, e, 0, "Bear", 0),
		findBattlefield(t, e, 0, "Bear", 1),
		findBattlefield(t, e, 0, "Bear", 2),
	}
	for _, r := range "RRR" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}

	e.askAttackers()
	submitAttackers(t, e, bears...)
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KChoose && len(d.Options) > 0 &&
			d.Options[0].Kind == "trigger_cost_pay" {
			chooseKindOption(t, e, d, "trigger_cost_pay")
			return
		}
		t.Fatalf("unexpected ask during Warcry Phoenix's resolution: %+v", d)
	}, func() bool { return onBattlefield(e, phoenix) })
	assertEnteredAttacking(t, e, phoenix, 1)

	entryDrive(t, e, func(d *decision.Decision) {
		t.Fatalf("unexpected ask after the entry: %+v", d)
	}, nil)
	if got := e.G.Players[1].Life; got != 12 {
		t.Fatalf("defender life = %d, want 12 (bears 6 + returned Phoenix 2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestJetRebelLeaderDigsACreatureTappedAndAttacking is the Dig window: the
// attack trigger's DB$ Dig puts a creature from the top five TAPPED and
// ATTACKING and it deals its damage.
func TestJetRebelLeaderDigsACreatureTappedAndAttacking(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Jet, Rebel Leader"},
		[]string{"Name:DigBear\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	jet := findBattlefield(t, e, 0, "Jet, Rebel Leader", 0)
	digBear := attackingEntryMove(t, e, 0, "DigBear", state.ZLibrary)
	// Put the creature at the top of the library so the Dig's five-card
	// window contains it (logged LibraryOrder, the cascade_test convention).
	rest := e.G.Zone(state.ZLibrary, 0)
	lib := append([]state.ObjID{digBear}, rest...)
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: lib})

	e.askAttackers()
	submitAttackers(t, e, jet)
	entryDrive(t, e, func(d *decision.Decision) {
		if d.Kind == decision.KChoose && d.ResumeKind == "dig" {
			chooseObjOption(t, e, d, digBear)
			return
		}
		t.Fatalf("unexpected ask during Jet's resolution: %+v", d)
	}, func() bool { return onBattlefield(e, digBear) })
	assertEnteredAttacking(t, e, digBear, 1)

	entryDrive(t, e, func(d *decision.Decision) {
		t.Fatalf("unexpected ask after the entry: %+v", d)
	}, nil)
	if got := e.G.Players[1].Life; got != 15 {
		t.Fatalf("defender life = %d, want 15 (Jet 3 + dug creature 2)", got)
	}
	replayCheck(t, e, cfg)
}
