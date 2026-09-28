package builtins

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"
)

// archSeat builds an sb-tactical-arch seat (Archetype on, everything else at
// the defaults).
func (f *tacticalFixture) archSeat() *Seat {
	w := DefaultTacticalWeights()
	w.Archetype = true
	return NewTactical(AutoPay, 1, f.lookup, w)
}

// archOf observes the current view and classifies opponent 1.
func (f *tacticalFixture) archOf(s *Seat) tArchState {
	f.t.Helper()
	f.v.Viewer = 0
	s.tac.observeArch(f.v)
	return s.tac.classify(1)
}

// TestArchetypeOffIsInert: with the group off (the sb-tactical default) a
// seat never modulates its weights, even against a fully classified
// opponent, so the plain arm's decisions are unchanged.
func TestArchetypeOffIsInert(t *testing.T) {
	if DefaultTacticalWeights().Archetype {
		t.Fatal("precondition: the default weights must leave the archetype group off")
	}
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.battlefield(1, "Mountain")
	f.battlefield(1, "Goblin Bushwhacker")
	f.hand("Llanowar Elves")
	s := f.seat()
	if s.tac.base.Archetype {
		t.Fatal("precondition: f.seat() is the plain arm")
	}
	in := decide(t, s, f.v, prio(opt(0, "pass", 0), opt(1, "cast", f.v.Players[0].Hand[0].ID)))
	_ = in
	if s.tac.w != s.tac.base {
		t.Fatalf("plain arm modulated its weights:\n got %+v\nwant %+v", s.tac.w, s.tac.base)
	}
}

// TestArchetypeColourFromLandsOnly: a mountain-only opponent has only red,
// so the prior leans aggro/burn even before any nonland card is seen.
func TestArchetypeColourFromLandsOnly(t *testing.T) {
	f := newTacticalFixture(t)
	f.battlefield(1, "Mountain")
	f.battlefield(1, "Mountain")
	s := f.archSeat()
	a := f.archOf(s)
	if !a.known {
		t.Fatal("precondition: a public mountain must be classifiable")
	}
	if got := s.tac.archObs[1].colors; !got[3] || got[1] {
		t.Fatalf("colours = %v, want red only", got)
	}
	top, _ := a.top()
	if top != "burn" && top != "aggro" {
		t.Fatalf("red-only top = %q, want burn or aggro (score %v)", top, a.score)
	}
}

// TestArchetypeColourFromLandsAndASeenSpell: an island pair plus a
// counterspell already seen is blue tempo/control with a real counter risk.
func TestArchetypeColourFromLandsAndASeenSpell(t *testing.T) {
	f := newTacticalFixture(t)
	f.battlefield(1, "Island")
	f.battlefield(1, "Island")
	counter := f.card("Counterspell", 1)
	f.v.Stack = []view.StackView{{ID: counter.ID, Kind: "spell", Name: counter.Name, Controller: 1, Card: &counter}}
	s := f.archSeat()
	a := f.archOf(s)
	if !a.known {
		t.Fatal("precondition: islands plus a seen spell must be classifiable")
	}
	if !s.tac.archObs[1].colors[1] {
		t.Fatalf("colours = %v, want blue", s.tac.archObs[1].colors)
	}
	if s.tac.archObs[1].counters == 0 {
		t.Fatal("precondition: the seen Counterspell must count as a counter")
	}
	top, _ := a.top()
	if top != "tempo" && top != "control" {
		t.Fatalf("blue + counter top = %q, want tempo or control (score %v)", top, a.score)
	}
}

// TestArchetypeAggroBurn: two cheap red creatures plus face burn is
// aggro/burn.
func TestArchetypeAggroBurn(t *testing.T) {
	f := newTacticalFixture(t)
	f.battlefield(1, "Goblin Bushwhacker")    // R, MV 2, fast body
	f.battlefield(1, "Burning-Tree Emissary") // RG, MV 2, fast body
	bolt := f.card("Lightning Bolt", 1)       // face burn
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, bolt)
	s := f.archSeat()
	a := f.archOf(s)
	o := s.tac.archObs[1]
	if o.creatures != 2 || o.burnFace == 0 {
		t.Fatalf("precondition: features = creatures %d burnFace %d", o.creatures, o.burnFace)
	}
	top, score := a.top()
	if top != "aggro" && top != "burn" {
		t.Fatalf("cheap red + burn top = %q (%.2f), want aggro or burn (score %v)", top, score, a.score)
	}
	if a.burnThreat < 0.3 {
		t.Fatalf("burnThreat = %.2f, want a real burn signal", a.burnThreat)
	}
}

// TestArchetypeSpeedFromEarlyTurn: a creature seen on turn 2 counts its
// features even though nothing else has resolved yet.
func TestArchetypeSpeedFromEarlyTurn(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Turn = 2
	f.battlefield(1, "Goblin Bushwhacker")
	s := f.archSeat()
	a := f.archOf(s)
	if !a.known {
		t.Fatal("precondition: a turn-2 creature must classify")
	}
	if s.tac.archObs[1].maxTurn != 2 {
		t.Fatalf("maxTurn = %d, want 2", s.tac.archObs[1].maxTurn)
	}
}

// TestArchetypeEngine: a mana creature plus an artifact engine is engine /
// ramp, not aggro.
func TestArchetypeEngine(t *testing.T) {
	f := newTacticalFixture(t)
	f.battlefield(1, "Llanowar Elves") // a mana creature
	f.battlefield(1, "Myr Enforcer")   // artifact creature with affinity
	s := f.archSeat()
	a := f.archOf(s)
	o := s.tac.archObs[1]
	if o.manaCre == 0 || o.affinity == 0 {
		t.Fatalf("precondition: manaCre %d affinity %d", o.manaCre, o.affinity)
	}
	top, _ := a.top()
	if top != "engine" && top != "ramp" {
		t.Fatalf("mana creature + artifact engine top = %q, want engine or ramp (score %v)", top, a.score)
	}
	if a.engineRisk < 0.3 {
		t.Fatalf("engineRisk = %.2f, want a real engine signal", a.engineRisk)
	}
}

// TestArchetypeRoleFlips: the role is beatdown when our clock is faster and
// control when it is slower.
func TestArchetypeRoleFlips(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	// Us: a 4/4 attacker. Them: nothing that attacks.
	f.battlefield(0, "Myr Enforcer")
	s := f.archSeat()
	st := s.tac.newState(&f.v, 0)
	if !(st.myClock < st.oppClock) {
		t.Fatalf("precondition: our clock %.1f must beat theirs %.1f", st.myClock, st.oppClock)
	}
	if got := beatdownFactor(st); got != 1 {
		t.Fatalf("faster deck role = %.1f, want beatdown (+1)", got)
	}
	// Flip: give them a bigger attacker and take ours away.
	f2 := newTacticalFixture(t)
	f2.v.Viewer = 0
	f2.battlefield(1, "Myr Enforcer")
	s2 := f2.archSeat()
	st2 := s2.tac.newState(&f2.v, 0)
	if !(st2.myClock > st2.oppClock) {
		t.Fatalf("precondition: our clock %.1f must be slower than theirs %.1f", st2.myClock, st2.oppClock)
	}
	if got := beatdownFactor(st2); got != -1 {
		t.Fatalf("slower deck role = %.1f, want control (-1)", got)
	}
}

// TestArchetypeNeutralWhenUnsure: with no public opponent card and no colour
// known, the modulation is exactly 1.0x the defaults.
func TestArchetypeNeutralWhenUnsure(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Players[1] = view.PlayerView{ID: 1, Life: 20, HandSize: 5} // nothing public
	s := f.archSeat()
	f.v.Viewer = 0
	s.tac.observeArch(f.v)
	st := s.tac.newState(&f.v, 0)
	if st.arch.known {
		t.Fatal("precondition: an empty opponent must not be classified")
	}
	got := s.tac.modulate(st)
	want := s.tac.base
	if got != want {
		t.Fatalf("unknown opponent modulation changed weights:\n got %+v\nwant %+v", got, want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("unknown opponent must leave every weight identical")
	}
}

// TestArchetypeBurnValuesLifeAndBlocks: against a seen burn opponent, our
// life is worth strictly more (ArchLife) and a non-lethal hit is chumped
// where the plain arm declines.
func TestArchetypeBurnValuesLifeAndBlocks(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 1, 10, "declare-blockers", "combat"
	f.v.Players[0].Life = 20
	// Their public cards: a burn signal (face burn plus cheap red bodies in
	// the graveyard) and a big attacker whose hit is non-lethal.
	atk := f.battlefield(1, "Myr Enforcer")
	f.v.Players[1].Battlefield[0].Attacking = true
	bolt := f.card("Lightning Bolt", 1)
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, bolt)
	for _, n := range []string{"Goblin Bushwhacker", "Burning-Tree Emissary"} {
		f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, f.card(n, 1))
	}
	// Our free blocker: a body the sim declines to trade for a non-lethal hit.
	blk := f.battlefield(0, "Grizzly Bears")

	d := decision.Decision{Seq: 4, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "block", Obj: blk.ID, Attacker: atk.ID}}}

	plain := f.seat()
	arch := f.archSeat()
	// Precondition: the classifier sees the burn and the arch group is on.
	arch.tac.observeArch(f.v)
	a := arch.tac.classify(1)
	if !a.known || a.burnThreat < 0.15 {
		t.Fatalf("precondition: burnThreat %.2f, known %v", a.burnThreat, a.known)
	}
	if in := decide(t, plain, f.v, d); len(in.Choices) != 0 {
		t.Fatalf("precondition: plain sb-tactical already blocks %v; pick a different board", in.Choices)
	}
	in := decide(t, arch, f.v, d)
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "block" {
		t.Fatalf("against seen burn the seat answered %v, want the chump block", in.Choices)
	}
}

// TestArchetypeHoldsBestSpellAgainstCounter: with open blue mana and a
// counter already seen, the arch arm casts the lesser spell first where the
// plain arm casts the bigger threat.
func TestArchetypeHoldsBestSpellAgainstCounter(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 0, 9, "main1", "main1"
	f.battlefield(0, "Forest")
	f.battlefield(0, "Forest")
	f.battlefield(0, "Forest")
	// Opponent: two untapped Islands and a counter already in the graveyard.
	f.battlefield(1, "Island")
	f.battlefield(1, "Island")
	cs := f.card("Counterspell", 1)
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, cs)

	small := f.hand("Llanowar Elves") // a one-drop
	big := f.hand("Myr Enforcer")     // a seven-drop threat
	d := prio(opt(0, "pass", 0), opt(1, "cast", small.ID), opt(2, "cast", big.ID))

	plain := f.seat()
	arch := f.archSeat()
	arch.tac.observeArch(f.v)
	a := arch.tac.classify(1)
	if !a.known || a.counterRisk == 0 {
		t.Fatalf("precondition: counterRisk %.2f, known %v", a.counterRisk, a.known)
	}
	if o := f.chose(decide(t, plain, f.v, d), d); o.Obj != big.ID {
		t.Fatalf("precondition: plain arm casts obj %d, want the big threat %d", o.Obj, big.ID)
	}
	if o := f.chose(decide(t, arch, f.v, d), d); o.Obj != small.ID {
		t.Fatalf("into open counter mana the arch arm cast obj %d, want the lesser spell %d", o.Obj, small.ID)
	}
}

// TestArchetypeRetargetsRemovalToEngine: removal is pointed at the opponent's
// mana creature over a bigger vanilla body once the opponent reads as an
// engine/ramp deck.
func TestArchetypeRetargetsRemovalToEngine(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 0, 9, "main1", "main1"
	elf := f.battlefield(1, "Llanowar Elves")   // 1/1 mana creature
	bear := f.battlefield(1, "Centaur Courser") // a bigger vanilla body
	// Engine evidence in the public graveyard: an artifact with affinity.
	enforcer := f.card("Myr Enforcer", 1)
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, enforcer)

	bolt := f.hand("Lightning Bolt")
	d := decision.Decision{Seq: 3, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1, Source: bolt.ID,
		TargetEffect: &decision.TargetEffect{API: "DealDamage"},
		Options: []decision.Option{
			{Index: 0, Kind: "card", Obj: bear.ID, Player: 1, Controller: 1},
			{Index: 1, Kind: "card", Obj: elf.ID, Player: 1, Controller: 1},
		}}

	plain := f.seat()
	arch := f.archSeat()
	arch.tac.observeArch(f.v)
	a := arch.tac.classify(1)
	if !a.known || a.engineRisk < 0.3 {
		t.Fatalf("precondition: engineRisk %.2f, known %v", a.engineRisk, a.known)
	}
	if o := f.chose(decide(t, plain, f.v, d), d); o.Obj != bear.ID {
		t.Fatalf("precondition: plain arm targets %d, want the bigger body %d", o.Obj, bear.ID)
	}
	if o := f.chose(decide(t, arch, f.v, d), d); o.Obj != elf.ID {
		t.Fatalf("engine opponent: removal aimed at %d, want the mana creature %d", o.Obj, elf.ID)
	}
}

// TestArchetypeDoesNotOverextendIntoControl: with three bodies already on
// the board against a control read, the arch arm holds the fourth where the
// plain arm deploys it into a sweeper.
func TestArchetypeDoesNotOverextendIntoControl(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 0, 9, "main1", "main1"
	f.battlefield(0, "Grizzly Bears")
	f.battlefield(0, "Grizzly Bears")
	f.battlefield(0, "Grizzly Bears")
	f.battlefield(0, "Forest")
	f.battlefield(0, "Forest")
	f.battlefield(0, "Forest")
	f.battlefield(0, "Forest")
	// Control evidence in the public graveyard: removal and counters, no
	// creatures.
	for i := 0; i < 3; i++ {
		c := f.card("Journey to Nowhere", 1)
		f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, c)
	}
	for i := 0; i < 2; i++ {
		c := f.card("Counterspell", 1)
		f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, c)
	}
	fourth := f.hand("Centaur Courser")
	d := prio(opt(0, "pass", 0), opt(1, "cast", fourth.ID))

	plain := f.seat()
	arch := f.archSeat()
	arch.tac.observeArch(f.v)
	a := arch.tac.classify(1)
	if !a.known || a.controlRisk < 0.3 {
		t.Fatalf("precondition: controlRisk %.2f, known %v", a.controlRisk, a.known)
	}
	if o := f.chose(decide(t, plain, f.v, d), d); o.Kind != "cast" {
		t.Fatalf("precondition: plain arm answered %q, want the cast", o.Kind)
	}
	if o := f.chose(decide(t, arch, f.v, d), d); o.Kind != "pass" {
		t.Fatalf("control opponent: arch arm answered %q, want it to hold the fourth body", o.Kind)
	}
}

// TestArchetypeBlocksIntoGoWide: against a go-wide opponent the arch arm
// chumps a non-lethal attacker, keeping life, where the plain arm declines.
func TestArchetypeBlocksIntoGoWide(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 1, 10, "declare-blockers", "combat"
	f.v.Players[0].Life = 20
	atk := f.battlefield(1, "Myr Enforcer") // the big attacker
	f.v.Players[1].Battlefield[0].Attacking = true
	// Go-wide evidence: small bodies plus a token maker in the graveyard.
	for i := 0; i < 3; i++ {
		f.battlefield(1, "Grizzly Bears")
	}
	fodder := f.card("Dragon Fodder", 1)
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, fodder)
	blk := f.battlefield(0, "Grizzly Bears")
	d := decision.Decision{Seq: 4, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "block", Obj: blk.ID, Attacker: atk.ID}}}

	plain := f.seat()
	arch := f.archSeat()
	arch.tac.observeArch(f.v)
	a := arch.tac.classify(1)
	if !a.known || a.wideRisk < 0.3 {
		t.Fatalf("precondition: wideRisk %.2f, known %v", a.wideRisk, a.known)
	}
	if in := decide(t, plain, f.v, d); len(in.Choices) != 0 {
		t.Fatalf("precondition: plain arm already blocks %v", in.Choices)
	}
	if in := decide(t, arch, f.v, d); len(in.Choices) != 1 {
		t.Fatalf("go-wide opponent: arch arm answered %v, want the chump", in.Choices)
	}
}

// TestArchetypeValuesEvasiveRemoval: against a tempo/fliers read, removal is
// pointed at the evasive threat over a bigger ground body.
func TestArchetypeValuesEvasiveRemoval(t *testing.T) {
	f := newTacticalFixture(t)
	f.v.Viewer = 0
	f.v.Active, f.v.Turn, f.v.Step, f.v.Phase = 0, 9, "main1", "main1"
	flier := f.battlefield(1, "Faerie Miscreant") // 1/1 flying
	bear := f.battlefield(1, "Grizzly Bears")     // 2/2 ground
	// Tempo evidence in the public graveyard: a counter and an evasive body.
	cs := f.card("Counterspell", 1)
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, cs)
	ev := f.card("Faerie Miscreant", 1)
	f.v.Players[1].Graveyard = append(f.v.Players[1].Graveyard, ev)

	bolt := f.hand("Lightning Bolt")
	three := 3
	d := decision.Decision{Seq: 3, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1, Source: bolt.ID,
		TargetEffect: &decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{Amount: &three}},
		Options: []decision.Option{
			{Index: 0, Kind: "card", Obj: bear.ID, Player: 1, Controller: 1},
			{Index: 1, Kind: "card", Obj: flier.ID, Player: 1, Controller: 1},
		}}

	plain := f.seat()
	arch := f.archSeat()
	arch.tac.observeArch(f.v)
	a := arch.tac.classify(1)
	if !a.known || a.tempoRisk < 0.3 {
		t.Fatalf("precondition: tempoRisk %.2f, known %v", a.tempoRisk, a.known)
	}
	if o := f.chose(decide(t, plain, f.v, d), d); o.Obj != bear.ID {
		t.Fatalf("precondition: plain arm targets %d, want the bigger ground body %d", o.Obj, bear.ID)
	}
	if o := f.chose(decide(t, arch, f.v, d), d); o.Obj != flier.ID {
		t.Fatalf("tempo opponent: removal aimed at %d, want the evasive threat %d", o.Obj, flier.ID)
	}
}
