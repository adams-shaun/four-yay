package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The alltargeted1 tests drive the three real corpus carriers of the
// AllTargeted$ count ref (Wayta, Trainer Prodigy; Raft Security Officer;
// Urgent Necropsy) through the cast-time sub-ability target pre-ask and the
// CollectEvidence payment. Every test asserts its own preconditions (the
// board the rule reads, the asks' order and shape) so a vacuous setup fails
// loudly, and each new assertion fails with the fix reverted.

func alltargetedCorpusText(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".cards", "cardsfolder", rel))
	if err != nil {
		t.Fatalf("read corpus card %s: %v", rel, err)
	}
	return string(b)
}

// drainNoPostAsk passes priority until the stack is empty, failing if any
// mid-resolution targeting ask appears: after the cast-time pre-ask the
// resolution must consume the recorded answers, not re-pose the asks (the
// exact re-ask would surface as a KChoose "tgts" or a second KTarget after
// payment).
func drainNoPostAsk(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected mid-resolution ask after the cast-time pre-asks: kind=%s resume=%q options=%+v", d.Kind, d.ResumeKind, d.Options)
		}
		submitChoices(t, e, 0)
	}
	t.Fatal("the stack never drained")
}

const atBearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// newFixtureDeckWithOpponentCard is newFixtureDeck with ONE extra card in
// seat 1's deck (all Mountains there), so the tests can put a real, replay-
// traceable creature under the opponent's control with moveSeeded.
func newFixtureDeckWithOpponentCard(t *testing.T, seed uint64, fixtureSrc, extra0, oppSrc string) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, fixtureSrc)
	name := fixture.Faces[0].Name
	extra := card(t, extra0)
	opp := card(t, oppSrc)
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{fixture, extra}, mountainDeck(t, 38)...),
				append([]*cards.Card{opp}, mountainDeck(t, 39)...),
			},
			Tokens: map[string]*cards.Card{},
		}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == name {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == name {
				id = cand
			}
		}
		if id == 0 {
			t.Fatalf("fixture %q not found in seat 0's hand or library", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	return e, cfg, id
}

// TestWaytaPreasksFightTargetAndReduces: Wayta's ability targets a fighter
// (root ask) and its DBFight sub targets the victim; Forge asks BOTH before
// payment (CR 601.2c), so the victim ask must arrive while nothing is paid
// (Wayta still untapped, pool intact, no ability object on the stack), the
// AllTargeted$ union counts both creatures you control, and the {2}
// reduction applies exactly when the union carries two you-control
// creatures.
func TestWaytaPreasksFightTargetAndReduces(t *testing.T) {
	t.Parallel()
	waytaSrc := alltargetedCorpusText(t, "w/wayta_trainer_prodigy.txt")

	t.Run("two_creatures_you_control_reduces_two", func(t *testing.T) {
		e, cfg, _ := newFixtureDeck(t, 71, waytaSrc, atBearSrc)
		wayta := moveSeeded(t, e, 0, waytaSrc, state.ZBattlefield)
		bear := putCreature(t, e, 0, atBearSrc)
		addMana(t, e, 0, "GGG")
		if e.G.Obj(wayta).Tapped {
			t.Fatal("Wayta entered tapped; the {T} cost cannot be tested")
		}
		e.Advance()
		opt := abilityOption(t, e, wayta, 0)
		submitChoices(t, e, opt.Index)

		// The root ask: the fighter (a creature you control).
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget || d.ResumeKind == "cast_sub" {
			t.Fatalf("root fighter ask = %+v", d)
		}
		fighter := -1
		for _, o := range d.Options {
			if o.Obj == bear {
				fighter = o.Index
			}
		}
		if fighter < 0 {
			t.Fatalf("the fighter is not offered as a root target: %+v", d.Options)
		}
		submitChoices(t, e, fighter)

		// THE fix: the DBFight victim ask is posed at CAST time, before any
		// payment -- Wayta still untapped, the pool still holds both G, and
		// no ability object is on the stack yet.
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
			t.Fatalf("the chained Fight victim was not pre-asked at cast time: %+v", d)
		}
		if e.G.Obj(wayta).Tapped || e.G.Players[0].Pool.Total() != 3 || len(e.G.Stack) != 0 {
			t.Fatalf("payment ran before the cast_sub ask: tapped=%v pool=%d stack=%v",
				e.G.Obj(wayta).Tapped, e.G.Players[0].Pool.Total(), e.G.Stack)
		}
		// TargetUnique$ True: the fighter cannot be its own victim.
		for _, o := range d.Options {
			if o.Obj == bear {
				t.Fatalf("the already-chosen fighter is offered as the Fight victim: %+v", d.Options)
			}
		}
		victim := -1
		for _, o := range d.Options {
			if o.Obj == wayta {
				victim = o.Index
			}
		}
		if victim < 0 {
			t.Fatalf("Wayta is not offered as the victim: %+v", d.Options)
		}
		submitChoices(t, e, victim)

		// Paid exactly the reduced cost ({G} + {T}: the {2} folded to 0); the
		// full price would have drained all three G.
		if !e.G.Obj(wayta).Tapped || e.G.Players[0].Pool.Total() != 2 {
			t.Fatalf("post-payment tapped=%v pool=%d, want tapped with 2 G left", e.G.Obj(wayta).Tapped, e.G.Players[0].Pool.Total())
		}
		if len(e.G.Stack) != 1 {
			t.Fatalf("ability stack = %v", e.G.Stack)
		}
		// The resolution consumes the recorded victim answer: the fight
		// resolves with BOTH targets and no re-ask.
		drainNoPostAsk(t, e)
		if e.G.Obj(bear).Damage != 1 || e.G.Obj(wayta).Damage != 2 {
			t.Fatalf("fight damage bear=%d wayta=%d, want 1 and 2 (Bear fought Wayta with the pre-asked victim)",
				e.G.Obj(bear).Damage, e.G.Obj(wayta).Damage)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("foreign_victim_keeps_full_price", func(t *testing.T) {
		e, cfg, _ := newFixtureDeckWithOpponentCard(t, 72, waytaSrc, atBearSrc, atBearSrc)
		wayta := moveSeeded(t, e, 0, waytaSrc, state.ZBattlefield)
		bear := putCreature(t, e, 0, atBearSrc)
		opp := putCreature(t, e, 1, atBearSrc)
		addMana(t, e, 0, "GGG")
		e.Advance()
		opt := abilityOption(t, e, wayta, 0)
		submitChoices(t, e, opt.Index)
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("root fighter ask = %+v", d)
		}
		fighter := -1
		for _, o := range d.Options {
			if o.Obj == bear {
				fighter = o.Index
			}
		}
		if fighter < 0 {
			t.Fatalf("the fighter is not offered: %+v", d.Options)
		}
		submitChoices(t, e, fighter)
		d = e.Pending()
		if d == nil || d.ResumeKind != "cast_sub" {
			t.Fatalf("victim ask = %+v", d)
		}
		victim := -1
		for _, o := range d.Options {
			if o.Obj == opp {
				victim = o.Index
			}
		}
		if victim < 0 {
			t.Fatalf("the opponent's creature is not offered as the victim: %+v", d.Options)
		}
		submitChoices(t, e, victim)
		// The union counts one you-control creature, so EQ2 fails and the
		// full {2}{G} is charged -- both G gone.
		if !e.G.Obj(wayta).Tapped || e.G.Players[0].Pool.Total() != 0 {
			t.Fatalf("full-price leg tapped=%v pool=%d, want tapped with an empty pool", e.G.Obj(wayta).Tapped, e.G.Players[0].Pool.Total())
		}
		drainNoPostAsk(t, e)
		// The two 2/2 Bears killed each other: the fight resolved against the
		// foreign victim with the recorded answer (each took the other's
		// power, 2, and both left the battlefield; Wayta was never a target).
		if e.G.Obj(bear).Zone != state.ZGraveyard || e.G.Obj(opp).Zone != state.ZGraveyard {
			t.Fatalf("fight result bear=%s opp=%s, want both in the graveyard", e.G.Obj(bear).Zone, e.G.Obj(opp).Zone)
		}
		if e.G.Obj(wayta).Damage != 0 || e.G.Obj(wayta).Zone != state.ZBattlefield {
			t.Fatalf("Wayta damage=%d zone=%s, want 0 and Battlefield", e.G.Obj(wayta).Damage, e.G.Obj(wayta).Zone)
		}
		replayCheck(t, e, cfg)
	})
}

// TestRaftSecurityOfficerReduction: the chain-less carrier keeps its own-target
// reduction and asks nothing beyond the ordinary single target ask. Retained
// as a regression test; the Wayta and Urgent Necropsy cases prove this change.
func TestRaftSecurityOfficerReduction(t *testing.T) {
	t.Parallel()
	raftSrc := alltargetedCorpusText(t, "r/raft_security_officer.txt")
	e, cfg, _ := newFixtureDeck(t, 73, raftSrc, atBearSrc)
	raft := moveSeeded(t, e, 0, raftSrc, state.ZBattlefield)
	bear := putCreature(t, e, 0, atBearSrc)
	// Raft's cost carries {T} and a summoning-sick creature cannot pay a tap
	// cost (CR 302.6): rotate to seat 0's next Main1, whose untap cleared it.
	// Precondition: the sickness really is gone, so the gate below tests the
	// reduction, not the sickness.
	e.Advance()
	passAll(t, e, 1)
	for i := 0; i < 200; i++ {
		if e.G.Turn >= 2 && e.G.Active == 0 && e.G.Step == state.StepMain1 && !e.G.Obj(raft).SummonSick {
			break
		}
		passAll(t, e, 1)
	}
	if e.G.Turn < 2 || e.G.Obj(raft).SummonSick {
		t.Fatal("could not reach a Main1 with an unsick Raft; the reduction cannot be tested")
	}
	addMana(t, e, 0, "CC")
	e.Advance()
	opt := abilityOption(t, e, raft, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask = %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("the Bear is not offered: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	// The powerLE3 reduction ({1}) folded: one generic paid, one left. A
	// second ask (a chain sub) must NOT exist -- this root has no sub chain.
	if d := e.Pending(); d != nil && d.ResumeKind == "cast_sub" {
		t.Fatalf("a chain-less root posed a sub ask: %+v", d)
	}
	if e.G.Players[0].Pool.Total() != 1 || !e.G.Obj(raft).Tapped {
		t.Fatalf("pool=%d tapped=%v, want 1 and tapped (the {1} reduction applied)",
			e.G.Players[0].Pool.Total(), e.G.Obj(raft).Tapped)
	}
	drainNoPostAsk(t, e)
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the targeted Bear was not tapped")
	}
	replayCheck(t, e, cfg)
}

// TestUrgentNecropsyCollectsEvidenceOnTheTargetUnion: the CollectEvidence<X>
// additional cost resolves X = the total mana value of the whole target
// union (the artifact root target plus the pre-asked chain subs' answers),
// the evidence ask is posed before payment, and the chosen cards are exiled
// as the payment. With every target elected zero, X = 0 and no evidence is
// owed or asked.
func TestUrgentNecropsyCollectsEvidenceOnTheTargetUnion(t *testing.T) {
	t.Parallel()
	necropsySrc := alltargetedCorpusText(t, "u/urgent_necropsy.txt")
	artifactSrc := "Name:Gold Myr\nManaCost:2\nTypes:Artifact Creature Myr\nPT:1/1\nOracle:x\n"
	grave3Src := "Name:Big Bones\nManaCost:3\nTypes:Artifact\nOracle:x\n"
	grave2Src := "Name:Small Bones\nManaCost:2\nTypes:Artifact\nOracle:x\n"

	t.Run("one_artifact_target_costs_its_mana_value_in_evidence", func(t *testing.T) {
		e, cfg, necro := newFixtureDeck(t, 74, necropsySrc, artifactSrc, grave3Src, grave2Src)
		artifact := putCreature(t, e, 0, artifactSrc)
		mv3 := moveSeeded(t, e, 0, grave3Src, state.ZGraveyard)
		moveSeeded(t, e, 0, grave2Src, state.ZGraveyard)
		addMana(t, e, 0, "BBBG")
		e.Advance()
		idx := -1
		for _, o := range e.Pending().Options {
			if o.Kind == "cast" && o.Obj == necro {
				idx = o.Index
			}
		}
		if idx < 0 {
			// Precondition: the pool (2 generic + B + G) can pay the printed
			// cost only when CollectEvidence<X> parses as a real component --
			// the old unmodelled-token fallback priced one phantom generic
			// more and the cast was never offered.
			t.Fatalf("Urgent Necropsy not offered with its exact printed mana in the pool: %+v", e.Pending().Options)
		}
		submitChoices(t, e, idx)
		// The root ask: the artifact.
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget || d.ResumeKind == "cast_sub" {
			t.Fatalf("root artifact ask = %+v", d)
		}
		tgt := -1
		for _, o := range d.Options {
			if o.Obj == artifact {
				tgt = o.Index
			}
		}
		if tgt < 0 {
			t.Fatalf("the artifact is not offered: %+v", d.Options)
		}
		submitChoices(t, e, tgt)
		// The chain's sub asks at cast time: the creature sub (Min 0) is
		// asked (the Myr is a candidate); elect zero. The enchantment and
		// planeswalker subs have no candidates and are recorded as
		// answered-empty without an ask.
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "cast_sub" {
			t.Fatalf("the chain's creature sub was not pre-asked at cast time: %+v", d)
		}
		submitChoices(t, e) // elect zero of one (Min 0)
		// The evidence ask: X = 3 (the artifact's mana value, the whole
		// union: the Gold Myr\x27s mana value 2), the greedy minimum is one card, ordered by mana value
		// descending (the 3 first).
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "evidence" {
			t.Fatalf("evidence ask = %+v", d)
		}
		if d.Min != 1 || len(d.Options) != 2 || d.Options[0].Obj != mv3 {
			t.Fatalf("evidence ask shape min=%d options=%+v, want min 1 with the MV-3 card first", d.Min, d.Options)
		}
		if len(e.G.Zone(state.ZExile, 0)) != 0 {
			t.Fatalf("evidence exiled before the ask was answered: %+v", e.G.Zone(state.ZExile, 0))
		}
		submitChoices(t, e, d.Options[0].Index)
		if e.G.Obj(mv3).Zone != state.ZExile {
			t.Fatalf("evidence card zone = %s, want Exile", e.G.Obj(mv3).Zone)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool after payment = %d, want 0 (the exact 2 B G)", got)
		}
		drainNoPostAsk(t, e)
		replayCheck(t, e, cfg)
	})

	t.Run("zero_targets_owe_no_evidence", func(t *testing.T) {
		e, cfg, necro := newFixtureDeck(t, 75, necropsySrc, artifactSrc, grave3Src)
		moveSeeded(t, e, 0, grave3Src, state.ZGraveyard)
		addMana(t, e, 0, "BBBG")
		e.Advance()
		idx := -1
		for _, o := range e.Pending().Options {
			if o.Kind == "cast" && o.Obj == necro {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("cast not offered: %+v", e.Pending().Options)
		}
		submitChoices(t, e, idx)
		// No artifact is on the battlefield and the Myr stays in the deck, so
		// the root ask (Min 0), every chain sub (Min 0, no candidates) and the
		// evidence (X = 0) all settle without a single ask: the cast paid
		// straight through and the caster holds priority again.
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("a zero-target cast left an ask outstanding: %+v", d)
		}
		if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 1 {
			t.Fatalf("graveyard size = %d, want 1 (no evidence exiled)", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool = %d, want 0 (the cast paid)", got)
		}
		drainNoPostAsk(t, e)
		replayCheck(t, e, cfg)
	})
}

// TestBodyReadsAllTargetedScopeGate pins the alltargeted1 SCOPE GATE: the
// cast flow pre-asks a chain's sub-ability targets only when the cast's own
// cost head reaches the AllTargeted$ ref, so every other chain keeps its
// mid-resolution ask (the general CR 601.2c pre-announcement is a separate
// ticket). The bodies are the real corpus spellings.
func TestBodyReadsAllTargetedScopeGate(t *testing.T) {
	t.Parallel()
	// Wayta's ReduceCost$ X -> Count$Compare Y EQ2.2.0 -> Y:AllTargeted$...
	wayta := map[string]string{
		"X": "Count$Compare Y EQ2.2.0",
		"Y": "AllTargeted$Valid Creature.YouCtrl",
	}
	if !bodyReadsAllTargeted("X", wayta, 0) {
		t.Fatal("Wayta's ReduceCost$ X must reach AllTargeted$ through its Count$Compare operand")
	}
	// Urgent Necropsy's CollectEvidence<X> names the ref directly.
	if !bodyReadsAllTargeted("X", map[string]string{"X": "AllTargeted$CardManaCost"}, 0) {
		t.Fatal("a direct AllTargeted$ SVar body must be seen")
	}
	// A reduction with no AllTargeted$ anywhere leaves the chain alone.
	if bodyReadsAllTargeted("X", map[string]string{
		"X": "Count$Compare Y EQ2.2.0",
		"Y": "Count$Valid Creature.YouCtrl",
	}, 0) {
		t.Fatal("a reduction that never names AllTargeted$ must not arm the pre-ask")
	}
	if bodyReadsAllTargeted("", map[string]string{"X": "AllTargeted$CardManaCost"}, 0) {
		t.Fatal("an empty cost head must not arm the pre-ask")
	}
	// A cyclic SVar table terminates rather than spinning.
	if bodyReadsAllTargeted("X", map[string]string{"X": "Count$Compare Y EQ1.1.0", "Y": "X"}, 0) {
		t.Fatal("a cyclic table must terminate false, not report a read")
	}
}
