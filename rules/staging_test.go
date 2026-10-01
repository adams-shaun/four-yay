package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func stageCard(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("card %q not in the corpus", name)
	}
	return c
}

func stageCards(t *testing.T, reg *cards.Registry, names ...string) []*cards.Card {
	out := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		out = append(out, stageCard(t, reg, n))
	}
	return out
}

func stagePassUntil(t *testing.T, e *Engine, stop func(*decision.Decision) bool) *decision.Decision {
	t.Helper()
	for i := 0; i < 200; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
			if d == nil {
				t.Fatalf("no decision")
			}
		}
		if stop(d) {
			return d
		}
		if d.Kind == decision.KAttackers || d.Kind == decision.KBlockers {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %s decision for %d at turn %d %v", d.Kind, d.Player, e.G.Turn, e.G.Step)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("no stop decision")
	return nil
}

// stageMain is a turn-5 position: seat 0 active in its precombat main,
// seat 0 on the play.
func stageMain(t *testing.T, reg *cards.Registry) (Config, Stage) {
	lib := stageCards(t, reg, "Plains", "Forest", "Island", "Plains", "Forest", "Island", "Plains", "Forest")
	cfg := Config{Seed: 77, Names: []string{"a", "b"}, Tokens: reg.Tokens}
	st := Stage{
		Turn: 5, Active: 0, Starting: 0, Step: state.StepMain1, Enter: StagePriorityFresh,
		Players: []StagedPlayer{
			{Life: 17, Library: lib, Hand: stageCards(t, reg, "Giant Growth", "Forest"),
				Graveyard: stageCards(t, reg, "Savannah Lions"), LandsPlayed: 0},
			{Life: 12, Library: append([]*cards.Card(nil), lib...), Hand: stageCards(t, reg, "Plains", "Plains", "Plains"),
				Exile: stageCards(t, reg, "Ajani's Pridemate")},
		},
		Permanents: []StagedPermanent{
			{Card: stageCard(t, reg, "Forest"), Owner: 0, Controller: 0},
			{Card: stageCard(t, reg, "Forest"), Owner: 0, Controller: 0, Tapped: true},
			{Card: stageCard(t, reg, "Helpful Hunter"), Owner: 0, Controller: 0, Sick: true},
			{Card: stageCard(t, reg, "Burglar Rat"), Owner: 0, Controller: 0},
			{Card: stageCard(t, reg, "Guarded Heir"), Owner: 0, Controller: 0, Counters: []StagedCounter{{"P1P1", 2}}, Damage: 1},
			{Card: stageCard(t, reg, "Ajani, Caller of the Pride"), Owner: 0, Controller: 0},
			{Card: stageCard(t, reg, "Llanowar Elves"), Owner: 0, Controller: 0, Sick: true},
			{Card: stageCard(t, reg, "Serra Angel"), Owner: 1, Controller: 1},
			{Card: stageCard(t, reg, "Pacifism"), Owner: 0, Controller: 0, AttachTo: 8},
			{Token: "w_1_1_cat", Owner: 1, Controller: 1, Sick: true},
			{Card: stageCard(t, reg, "Savannah Lions"), Owner: 1, Controller: 0},
			{Card: stageCard(t, reg, "Plains"), Owner: 1, Controller: 1, Tapped: true},
		},
	}
	return cfg, st
}

func TestStagedPositionPlacesEverythingWithoutTriggers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg, st := stageMain(t, reg)
	e, ids, err := NewStaged(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range e.L.Events {
		switch ev.Kind {
		case events.TriggerPush, events.AbilityPush, events.PutOnStack, events.Draw, events.DecisionAsk:
			t.Fatalf("staging emitted %v", ev.Kind)
		}
	}
	d := stagePassUntil(t, e, func(d *decision.Decision) bool { return true })
	if d.Kind != decision.KPriority || d.Player != 0 || e.G.Step != state.StepMain1 || e.G.Turn != 5 {
		t.Fatalf("first decision %s for %d at turn %d %v", d.Kind, d.Player, e.G.Turn, e.G.Step)
	}
	if len(e.G.Stack) != 0 || len(e.pendingTriggers) != 0 {
		t.Fatalf("stack %v pending triggers %d", e.G.Stack, len(e.pendingTriggers))
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 2 {
		t.Fatalf("seat 0 hand %d, want 2 (no ETB draw)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 3 {
		t.Fatalf("seat 1 hand %d, want 3 (no ETB discard)", got)
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)) + len(e.G.Zone(state.ZBattlefield, 1)); got != len(st.Permanents) {
		t.Fatalf("battlefield %d, want %d (no ETB tokens)", got, len(st.Permanents))
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 8 {
		t.Fatalf("library %d", got)
	}
	if e.G.Players[0].Life != 17 || e.G.Players[1].Life != 12 || e.G.StartingPlayer != 0 {
		t.Fatalf("life %d/%d starting %d", e.G.Players[0].Life, e.G.Players[1].Life, e.G.StartingPlayer)
	}
	if len(e.G.Entered) != 0 {
		t.Fatalf("entered history %v", e.G.Entered)
	}
	for i, pm := range st.Permanents {
		o := e.G.Obj(ids.Permanents[i])
		if o.Zone != state.ZBattlefield || o.Controller != pm.Controller || o.Owner != pm.Owner {
			t.Fatalf("permanent %d zone %v controller %d owner %d", i, o.Zone, o.Controller, o.Owner)
		}
		if o.SummonSick != pm.Sick {
			t.Fatalf("permanent %d (%s) sick %v, want %v", i, o.Face().Name, o.SummonSick, pm.Sick)
		}
		if o.Tapped != pm.Tapped {
			t.Fatalf("permanent %d tapped %v", i, o.Tapped)
		}
		if o.EnteredThisTurn || o.WasDealtDamageThisTurn {
			t.Fatalf("permanent %d carries this-turn history", i)
		}
		if o.IsToken != (pm.Card == nil) {
			t.Fatalf("permanent %d token %v", i, o.IsToken)
		}
	}
	heir := e.G.Obj(ids.Permanents[4])
	if heir.Counter("P1P1") != 2 || heir.Damage != 1 {
		t.Fatalf("heir counters %d damage %d", heir.Counter("P1P1"), heir.Damage)
	}
	if ajani := e.G.Obj(ids.Permanents[5]); ajani.Counter("LOYALTY") != 4 {
		t.Fatalf("ajani loyalty %d", ajani.Counter("LOYALTY"))
	}
	if paci := e.G.Obj(ids.Permanents[8]); paci.AttachedTo != ids.Permanents[7] {
		t.Fatalf("pacifism attached to %d", paci.AttachedTo)
	}
	// Llanowar Elves is sick: its mana ability is not offered.
	for _, o := range d.Options {
		if o.Obj == ids.Permanents[6] {
			t.Fatalf("sick elves offered %q", o.Label)
		}
	}
}

func TestStagedIsDeterministic(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg, st := stageMain(t, reg)
	a, _, err := NewStaged(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := NewStaged(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if a.L.Head() != b.L.Head() || len(a.L.Events) != len(b.L.Events) {
		t.Fatalf("heads %s %s", a.L.Head(), b.L.Head())
	}
	// Play both to seat 1's first priority of the next turn: identical.
	stop := func(d *decision.Decision) bool { return d.Player == 1 && d.Kind == decision.KPriority && a.G.Turn == 6 }
	stagePassUntil(t, a, stop)
	stop = func(d *decision.Decision) bool { return d.Player == 1 && d.Kind == decision.KPriority && b.G.Turn == 6 }
	stagePassUntil(t, b, stop)
	if a.L.Head() != b.L.Head() {
		t.Fatalf("played heads %s %s", a.L.Head(), b.L.Head())
	}
}

func TestStagedEndTurnRollsOverToTheKnownDraw(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg, st := stageMain(t, reg)
	// Seat 1's end step, turn 4; seat 0 draws Giant Growth next.
	st.Turn, st.Active, st.Step = 4, 1, state.StepEnd
	st.Players[0].Library = append(stageCards(t, reg, "Giant Growth"), st.Players[0].Library...)
	e, ids, err := NewStaged(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	d := stagePassUntil(t, e, func(d *decision.Decision) bool {
		return d.Player == 0 && e.G.Step == state.StepMain1
	})
	if e.G.Turn != 5 || d.Kind != decision.KPriority {
		t.Fatalf("turn %d kind %s", e.G.Turn, d.Kind)
	}
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 3 || hand[2] != ids.Library[0][0] {
		t.Fatalf("hand %v, want the known top %d drawn", hand, ids.Library[0][0])
	}
	// Seat 0's permanents untapped and unsick; seat 1's cat token (sick in
	// seat 1's own turn 4) is still sick in seat 0's turn 5.
	if o := e.G.Obj(ids.Permanents[1]); o.Tapped {
		t.Fatalf("seat 0 forest still tapped")
	}
	if o := e.G.Obj(ids.Permanents[6]); o.SummonSick {
		t.Fatalf("seat 0 elves still sick in its own turn")
	}
	if o := e.G.Obj(ids.Permanents[9]); !o.SummonSick {
		t.Fatalf("seat 1 cat token lost its sickness")
	}
}

func TestStagedHeldDeclareAttackersReachesBlockers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg, st := stageMain(t, reg)
	// Seat 1 attacks with its Serra Angel (vigilance, un-Pacified here) and
	// the Savannah Lions it does not control -- no: give it the lions.
	st.Turn, st.Active, st.Step, st.Enter = 6, 1, state.StepDeclareAttackers, StagePriorityHeld
	st.PriorityPlayer = 1
	st.Permanents[8].AttachTo = 0 // Pacifism on nothing is illegal; drop it
	st.Permanents = st.Permanents[:8]
	st.Permanents = append(st.Permanents, StagedPermanent{Card: stageCard(t, reg, "Savannah Lions"), Owner: 1, Controller: 1})
	st.Attackers = []StagedAttack{{Attacker: 7, Defender: 0}, {Attacker: 8, Defender: 0}}
	e, ids, err := NewStaged(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if o := e.G.Obj(ids.Permanents[7]); !o.IsAttacking || o.Tapped {
		t.Fatalf("angel attacking %v tapped %v (vigilance)", o.IsAttacking, o.Tapped)
	}
	if o := e.G.Obj(ids.Permanents[8]); !o.IsAttacking || !o.Tapped {
		t.Fatalf("lions attacking %v tapped %v", o.IsAttacking, o.Tapped)
	}
	d := stagePassUntil(t, e, func(d *decision.Decision) bool { return d.Kind == decision.KBlockers })
	if d.Player != 0 || e.G.Step != state.StepDeclareBlockers || e.G.Turn != 6 {
		t.Fatalf("blockers for %d at %d %v", d.Player, e.G.Turn, e.G.Step)
	}
}

func TestStagedBeginStepRunsTheStep(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cfg, st := stageMain(t, reg)
	st.Step, st.Enter = state.StepDraw, StageBeginStep
	e, ids, err := NewStaged(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	stagePassUntil(t, e, func(d *decision.Decision) bool { return true })
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 3 || hand[2] != ids.Library[0][0] {
		t.Fatalf("draw step did not draw the top card: %v", hand)
	}
}

func TestStagedRefusesWhatItCannotPlace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for name, mut := range map[string]func(*Config, *Stage){
		"decks":         func(c *Config, s *Stage) { c.Decks = [][]*cards.Card{{}, {}} },
		"parity":        func(c *Config, s *Stage) { s.Active = 1 },
		"untap":         func(c *Config, s *Stage) { s.Step = state.StepUntap },
		"cleanup":       func(c *Config, s *Stage) { s.Step = state.StepCleanup },
		"damage step":   func(c *Config, s *Stage) { s.Step = state.StepCombatDamage },
		"token":         func(c *Config, s *Stage) { s.Permanents[9].Token = "no_such_token" },
		"attack timing": func(c *Config, s *Stage) { s.Attackers = []StagedAttack{{Attacker: 3, Defender: 1}} },
		"turn one":      func(c *Config, s *Stage) { s.Turn = 1 },
	} {
		cfg, st := stageMain(t, reg)
		mut(&cfg, &st)
		if _, _, err := NewStaged(cfg, st); err == nil {
			t.Errorf("%s: staged", name)
		}
	}
}
