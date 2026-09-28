package payexec

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// board is a corpus fixture: seat 0 controls the named permanents (and
// tokens), holds spell in hand, and the engine is parked at seat 0's first
// main-phase priority decision where the planner offers a plan for spell.
type board struct {
	e     *rules.Engine
	spell state.ObjID
	objs  map[string][]state.ObjID // seat-0 battlefield object ids by name
}

func lookup(t *testing.T, reg *cards.Registry, name string) *cards.Card {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus card %q not found", name)
	}
	return c
}

// newBoard builds the fixture. battlefield names seat-0 permanents (by card
// name), tokens names token scripts created for seat 0.
func newBoard(t *testing.T, spell string, battlefield []string, tokens []string) *board {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	wastes := lookup(t, reg, "Wastes")
	deck0 := []*cards.Card{lookup(t, reg, spell)}
	for _, n := range battlefield {
		deck0 = append(deck0, lookup(t, reg, n))
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, wastes)
	}
	deck1 := make([]*cards.Card, 40)
	for i := range deck1 {
		deck1[i] = wastes
	}
	e := rules.New(rules.Config{Seed: 7, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	b := &board{e: e, objs: map[string][]state.ObjID{}}
	used := map[state.ObjID]bool{}
	take := func(name string) (state.ObjID, state.Zone) {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Card != nil && o.Owner == 0 && !used[o.ID] && o.Face().Name == name {
				used[o.ID] = true
				return o.ID, o.Zone
			}
		}
		t.Fatalf("%q not found in seat 0's genesis", name)
		return 0, 0
	}
	id, from := take(spell)
	b.spell = id
	if from != state.ZHand {
		e.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZHand})
	}
	for _, n := range battlefield {
		id, from := take(n)
		e.Emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield})
		b.objs[n] = append(b.objs[n], id)
	}
	for _, tok := range tokens {
		before := len(e.G.Objs)
		e.Emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: tok})
		for i := before; i < len(e.G.Objs); i++ {
			b.objs[tok] = append(b.objs[tok], e.G.Objs[i].ID)
		}
	}
	e.Advance()
	for i := 0; i < 2000; i++ {
		if e.G.Over {
			t.Fatal("fixture game ended")
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		if d.Kind == decision.KPriority && d.Player == 0 && e.G.Stack == nil {
			if _, ok := ActionFor(e, b.spell); ok {
				return b
			}
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player}
		switch d.Kind {
		case decision.KPriority:
			for _, o := range d.Options {
				if o.Kind == "pass" {
					in.Choices = []int{o.Index}
				}
			}
		case decision.KAttackers, decision.KBlockers:
		default:
			// discard to hand size and similar: the first Min options.
			for j := 0; j < d.Min && j < len(d.Options); j++ {
				in.Choices = append(in.Choices, d.Options[j].Index)
			}
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("driving fixture: %v (%+v)", err, d)
		}
	}
	t.Fatalf("no plan for %s was offered within the drive budget", spell)
	return nil
}

func (b *board) action(t *testing.T) decision.PaymentAction {
	t.Helper()
	a, ok := ActionFor(b.e, b.spell)
	if !ok {
		t.Fatal("no payment action")
	}
	return a
}

func sources(p decision.PaymentPlan) []state.ObjID {
	var out []state.ObjID
	for _, a := range p.Activations {
		out = append(out, a.Source)
	}
	return out
}

// onStack reports whether the planned spell was cast (is on the stack).
func (b *board) onStack() bool {
	o := b.e.G.Obj(b.spell)
	return o != nil && o.Zone == state.ZStack
}

func (b *board) tapped(id state.ObjID) bool {
	o := b.e.G.Obj(id)
	return o != nil && o.Tapped
}

func (b *board) run(t *testing.T) *Execution {
	t.Helper()
	x, err := Run(b.e, b.action(t))
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// TestTwoColourCastWithBasics: Watchwolf ({G}{W}) over Forest, Plains and
// Mountain taps exactly the Forest and the Plains and casts.
func TestTwoColourCastWithBasics(t *testing.T) {
	b := newBoard(t, "Watchwolf", []string{"Forest", "Plains", "Mountain"}, nil)
	x := b.run(t)
	if x.Status() != Done {
		t.Fatalf("status %v reason %s %s", x.Status(), x.Reason, x.Detail)
	}
	if !b.onStack() {
		t.Fatal("Watchwolf was not cast")
	}
	if x.Taps != 2 || !b.tapped(b.objs["Forest"][0]) || !b.tapped(b.objs["Plains"][0]) || b.tapped(b.objs["Mountain"][0]) {
		t.Fatalf("taps %d: forest %v plains %v mountain %v", x.Taps,
			b.tapped(b.objs["Forest"][0]), b.tapped(b.objs["Plains"][0]), b.tapped(b.objs["Mountain"][0]))
	}
	if got := PoolOf(b.e, 0); got != (decision.ManaAmount{}) {
		t.Fatalf("pool after cast %v, want empty", got)
	}
}

// TestDualSourceNamesTheWitnessColour: Watchwolf over Forest and Selesnya
// Guildgate ("Add {G} or {W}"): the gate must make W, which the lowering
// answers on the gate's colour ask.
func TestDualSourceNamesTheWitnessColour(t *testing.T) {
	b := newBoard(t, "Watchwolf", []string{"Forest", "Selesnya Guildgate"}, nil)
	a := b.action(t)
	plan, _ := SelectPlan(&a)
	gate := b.objs["Selesnya Guildgate"][0]
	var gateStep *decision.PaymentActivation
	for i := range plan.Activations {
		if plan.Activations[i].Source == gate {
			gateStep = &plan.Activations[i]
		}
	}
	if gateStep == nil || gateStep.Produces != (decision.ManaAmount{1, 0, 0, 0, 0, 0}) {
		t.Fatalf("plan should take W from the gate: %+v", plan.Activations)
	}
	x := b.run(t)
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s, on stack %v", x.Status(), x.Reason, x.Detail, b.onStack())
	}
	if x.Asks < 1 {
		t.Fatalf("the gate's colour ask was not answered (asks %d)", x.Asks)
	}
}

// TestAnyColourSacrificeSourceOnlyWhenNeeded: with Forest, Plains and a
// Treasure the plan leaves the Treasure alone; with only Forest and a
// Treasure it sacrifices the Treasure for W (a disclosed Consequence).
func TestAnyColourSacrificeSourceOnlyWhenNeeded(t *testing.T) {
	const treasure = "c_a_treasure_sac"
	b := newBoard(t, "Watchwolf", []string{"Forest", "Plains"}, []string{treasure})
	tr := b.objs[treasure][0]
	a := b.action(t)
	plan, _ := SelectPlan(&a)
	for _, s := range sources(plan) {
		if s == tr {
			t.Fatalf("plan spends the Treasure although basics pay: %+v", plan.Activations)
		}
	}
	x := b.run(t)
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s", x.Status(), x.Reason, x.Detail)
	}
	if o := b.e.G.Obj(tr); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("the Treasure left the battlefield")
	}

	b = newBoard(t, "Watchwolf", []string{"Forest"}, []string{treasure})
	tr = b.objs[treasure][0]
	a = b.action(t)
	plan, _ = SelectPlan(&a)
	var step *decision.PaymentActivation
	for i := range plan.Activations {
		if plan.Activations[i].Source == tr {
			step = &plan.Activations[i]
		}
	}
	if step == nil || step.Consequence == nil || !step.Consequence.Sacrifice || step.Produces != (decision.ManaAmount{1, 0, 0, 0, 0, 0}) {
		t.Fatalf("plan should sacrifice the Treasure for W: %+v", plan.Activations)
	}
	x = b.run(t)
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s", x.Status(), x.Reason, x.Detail)
	}
	if o := b.e.G.Obj(tr); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the Treasure was not sacrificed")
	}
}

// TestPetalAnyColour: Lotus Petal ("Add one mana of any color", sacrifice)
// supplies the W a Forest cannot.
func TestPetalAnyColour(t *testing.T) {
	b := newBoard(t, "Watchwolf", []string{"Forest", "Lotus Petal"}, nil)
	x := b.run(t)
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s", x.Status(), x.Reason, x.Detail)
	}
}

// TestDivergenceAbortsCleanly: the surface diverging from the plan stops
// the lowering without an answer -- a source tapped behind its back, a
// pool that is not the predicted one, an unexpected decision -- and a
// finished Execution never answers again.
func TestDivergenceAbortsCleanly(t *testing.T) {
	b := newBoard(t, "Watchwolf", []string{"Forest", "Plains", "Mountain"}, nil)
	a := b.action(t)
	x := Start(0, &a, PoolOf(b.e, 0))
	if len(x.Plan.Activations) != 2 {
		t.Fatalf("plan %+v", x.Plan.Activations)
	}
	// The second planned source is tapped behind the plan's back, then the
	// first step runs: the fresh priority offer no longer carries the
	// second source's activate option.
	second := x.Plan.Activations[1].Source
	b.e.Emit(events.Event{Kind: events.Tap, Obj: second})
	d := b.e.Pending()
	in, st := x.Step(d, PoolOf(b.e, 0))
	if st != InProgress {
		t.Fatalf("first step %v %s", st, x.Reason)
	}
	if err := b.e.Submit(in); err != nil {
		t.Fatal(err)
	}
	d = b.e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected priority after the first tap, got %+v", d)
	}
	if in, st = x.Step(d, PoolOf(b.e, 0)); st != Aborted || x.Reason != ReasonActivateNotOffered || in.Choices != nil {
		t.Fatalf("step %v reason %q intent %+v", st, x.Reason, in)
	}
	if _, st = x.Step(d, PoolOf(b.e, 0)); st != Aborted {
		t.Fatal("an aborted execution answered again")
	}

	// Pool mismatch: the plan predicts an empty pool at Start.
	b = newBoard(t, "Watchwolf", []string{"Forest", "Plains"}, nil)
	a = b.action(t)
	x = Start(0, &a, decision.ManaAmount{})
	if _, st = x.Step(b.e.Pending(), decision.ManaAmount{0, 0, 0, 1, 0, 0}); st != Aborted || x.Reason != ReasonPoolMismatch {
		t.Fatalf("pool mismatch: %v %q", st, x.Reason)
	}

	// A decision that is not the lowering's yields (the caller answers it)
	// and the lowering continues; another seat's decision aborts.
	x = Start(0, &a, decision.ManaAmount{})
	if in, st = x.Step(&decision.Decision{Player: 0, Kind: decision.KTarget}, decision.ManaAmount{}); st != Yield || in.Choices != nil || x.Status() != InProgress || x.Yields != 1 {
		t.Fatalf("foreign decision: %v %q status %v", st, x.Reason, x.Status())
	}
	x = Start(0, &a, decision.ManaAmount{})
	if _, st = x.Step(&decision.Decision{Player: 1, Kind: decision.KPriority}, decision.ManaAmount{}); st != Aborted || x.Reason != ReasonWrongPlayer {
		t.Fatalf("wrong player: %v %q", st, x.Reason)
	}
	// No plan at all.
	if x = Start(0, &decision.PaymentAction{}, decision.ManaAmount{}); x.Status() != Aborted || x.Reason != ReasonNoPlan {
		t.Fatalf("no plan: %v %q", x.Status(), x.Reason)
	}
}

// TestSelectPlanPrefersNoConsequence pins the plan choice on a synthetic
// action whose first plan is last-resort.
func TestSelectPlanPrefersNoConsequence(t *testing.T) {
	last := decision.PaymentPlan{ID: "last", Activations: []decision.PaymentActivation{{Source: 1, Consequence: &decision.PaymentConsequence{Sacrifice: true}}}}
	plain := decision.PaymentPlan{ID: "plain", Activations: []decision.PaymentActivation{{Source: 2}}}
	if p, _ := SelectPlan(&decision.PaymentAction{Plans: []decision.PaymentPlan{last, plain}}); p.ID != "plain" {
		t.Fatalf("chose %s", p.ID)
	}
	if p, _ := SelectPlan(&decision.PaymentAction{Plans: []decision.PaymentPlan{last}}); p.ID != "last" {
		t.Fatalf("chose %s", p.ID)
	}
}

func TestMatchManaLabels(t *testing.T) {
	w := decision.ManaAmount{1, 0, 0, 0, 0, 0}
	wheel := &decision.Decision{Kind: decision.KChoose, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "mana", Obj: 5, Label: "Add C"},
		{Index: 1, Kind: "mana", Obj: 5, Label: "Pay 1 life: Add W"},
		{Index: 2, Kind: "mana", Obj: 5, Label: "Add any color"},
	}}
	if c, ok := matchMana(wheel, decision.PaymentActivation{Source: 5, Produces: w, Consequence: &decision.PaymentConsequence{Life: 1}}); !ok || c[0] != 1 {
		t.Fatalf("painful W: %v %v", c, ok)
	}
	if c, ok := matchMana(wheel, decision.PaymentActivation{Source: 5, Produces: decision.ManaAmount{0, 0, 0, 0, 0, 1}}); !ok || c[0] != 0 {
		t.Fatalf("C: %v %v", c, ok)
	}
	if c, ok := matchMana(wheel, decision.PaymentActivation{Source: 5, Produces: decision.ManaAmount{0, 0, 0, 0, 1, 0}}); !ok || c[0] != 2 {
		t.Fatalf("G via any: %v %v", c, ok)
	}
	three := &decision.Decision{Kind: decision.KChoose, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "mana", Obj: 5, Label: "Add three mana of any one color"},
	}}
	if _, ok := matchMana(three, decision.PaymentActivation{Source: 5, Produces: decision.ManaAmount{0, 0, 0, 0, 2, 0}}); ok {
		t.Fatal("GG matched a three-mana ability")
	}
	if c, ok := matchMana(three, decision.PaymentActivation{Source: 5, Produces: decision.ManaAmount{0, 0, 0, 0, 3, 0}}); !ok || c[0] != 0 {
		t.Fatalf("GGG: %v %v", c, ok)
	}
	alloc := &decision.Decision{Kind: decision.KChoose, Min: 2, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "mana", Obj: 5, ManaSymbol: "R"}, {Index: 1, Kind: "mana", Obj: 5, ManaSymbol: "G"},
		{Index: 2, Kind: "mana", Obj: 5, ManaSymbol: "R"}, {Index: 3, Kind: "mana", Obj: 5, ManaSymbol: "G"},
	}}
	if c, ok := matchMana(alloc, decision.PaymentActivation{Source: 5, Produces: decision.ManaAmount{0, 0, 0, 1, 1, 0}}); !ok || len(c) != 2 || c[0] != 0 || c[1] != 3 {
		t.Fatalf("RG allocation: %v %v", c, ok)
	}
}

// TestLastResortStepsRunLast: a plan listing a sacrifice before a plain tap
// taps the plain source first.
func TestLastResortStepsRunLast(t *testing.T) {
	c := decision.ManaAmount{0, 0, 0, 0, 0, 1}
	g := decision.ManaAmount{0, 0, 0, 0, 1, 0}
	plan := decision.PaymentPlan{Activations: []decision.PaymentActivation{
		{Source: 7, Produces: c, Consequence: &decision.PaymentConsequence{Sacrifice: true}},
		{Source: 8, Produces: g},
	}}
	x := StartPlay(0, Play{Kind: "cast", Obj: 3}, plan, decision.ManaAmount{})
	if got := sources(decision.PaymentPlan{Activations: x.Steps()}); len(got) != 2 || got[0] != 8 || got[1] != 7 {
		t.Fatalf("execution order %v, want [8 7]", got)
	}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{
		{Index: 0, Kind: "activate", Obj: 7}, {Index: 1, Kind: "activate", Obj: 8}, {Index: 2, Kind: "pass"},
	}}
	if in, st := x.Step(d, decision.ManaAmount{}); st != InProgress || in.Choices[0] != 1 {
		t.Fatalf("first step %v %+v, want the plain source (option 1)", st, in)
	}
}

// TestWaitForTheStack: every source ran but the play is not offered while
// the stack holds something (a trigger the sacrifice caused): the lowering
// passes, keeps the pool, and casts once the play is offered; with an empty
// stack the same surface aborts.
func TestWaitForTheStack(t *testing.T) {
	c := decision.ManaAmount{0, 0, 0, 0, 0, 1}
	plan := decision.PaymentPlan{Activations: []decision.PaymentActivation{{Source: 7, Produces: c}}}
	tap := &decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{{Index: 0, Kind: "activate", Obj: 7}, {Index: 1, Kind: "pass"}}}
	blocked := &decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{{Index: 0, Kind: "pass"}}}
	open := &decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{{Index: 0, Kind: "pass"}, {Index: 1, Kind: "cast", Obj: 3}}}
	x := StartPlay(0, Play{Kind: "cast", Obj: 3}, plan, decision.ManaAmount{})
	x.StepOn(tap, Surface{})
	if in, st := x.StepOn(blocked, Surface{Pool: c, Stack: 1}); st != InProgress || in.Choices[0] != 0 || x.Waits != 1 {
		t.Fatalf("wait: %v %+v waits %d", st, in, x.Waits)
	}
	if in, st := x.StepOn(open, Surface{Pool: c}); st != Done || in.Choices[0] != 1 {
		t.Fatalf("after the stack resolved: %v %+v", st, in)
	}
	x = StartPlay(0, Play{Kind: "cast", Obj: 3}, plan, decision.ManaAmount{})
	x.StepOn(tap, Surface{})
	if _, st := x.StepOn(blocked, Surface{Pool: c}); st != Aborted || x.Reason != ReasonCastNotOffered {
		t.Fatalf("empty stack: %v %q", st, x.Reason)
	}
}

// TestAbilityPlayMatchesOnlyThePrintedOption: an ability lowering ends on
// the printed ability's own option, never a granted one with the same index.
func TestAbilityPlayMatchesOnlyThePrintedOption(t *testing.T) {
	p := Play{Kind: "ability", Obj: 5, Ability: 1}
	for _, c := range []struct {
		o    decision.Option
		want bool
	}{
		{decision.Option{Kind: "ability", Obj: 5, Ability: 1}, true},
		{decision.Option{Kind: "ability", Obj: 5, Ability: 0}, false},
		{decision.Option{Kind: "ability", Obj: 5, Ability: 1, SVar: "Grant"}, false},
		{decision.Option{Kind: "ability", Obj: 5, Ability: 1, AltCostIndex: 1}, false},
		{decision.Option{Kind: "cast", Obj: 5}, false},
	} {
		if got := p.Matches(&c.o); got != c.want {
			t.Errorf("%+v: %v, want %v", c.o, got, c.want)
		}
	}
}

// TestSacrificeTriggerWaitsThenCasts: Grizzly Bears paid by a Forest and an
// Eldrazi Spawn next to Writhing Chrysalis. The Spawn's sacrifice triggers
// the Chrysalis, whose trigger holds the sorcery-speed cast off the stack;
// the lowering waits (passes, the {C} floating), the trigger resolves, and
// the Bears are cast. Before, this aborted cast_not_offered.
func TestSacrificeTriggerWaitsThenCasts(t *testing.T) {
	const spawn = "c_0_1_eldrazi_spawn_sac"
	b := newBoard(t, "Grizzly Bears", []string{"Forest", "Writhing Chrysalis"}, []string{spawn})
	x, err := Drive(b.e, Start(0, ptr(b.action(t)), PoolOf(b.e, 0)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s", x.Status(), x.Reason, x.Detail)
	}
	if x.Waits < 1 {
		t.Fatalf("the Chrysalis trigger never held the cast (waits %d): fixture no longer exercises the wait", x.Waits)
	}
	if o := b.e.G.Obj(b.objs[spawn][0]); o != nil && o.Zone == state.ZBattlefield {
		t.Fatal("the Spawn was not sacrificed")
	}
}

// TestTwoTriggersYieldThenCast: with two Chrysalises the sacrifice asks
// for a trigger order mid-lowering; the lowering yields it, waits out both
// triggers and casts.
func TestTwoTriggersYieldThenCast(t *testing.T) {
	const spawn = "c_0_1_eldrazi_spawn_sac"
	b := newBoard(t, "Grizzly Bears", []string{"Forest", "Writhing Chrysalis", "Writhing Chrysalis"}, []string{spawn})
	x, err := Drive(b.e, Start(0, ptr(b.action(t)), PoolOf(b.e, 0)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s", x.Status(), x.Reason, x.Detail)
	}
	if x.Yields < 1 || x.Waits < 1 {
		t.Fatalf("yields %d waits %d, want both", x.Yields, x.Waits)
	}
}

// TestSacrificeLastKeepsTheDefenderCount: Juggernaut ({4}) from Overgrown
// Battlement (G per defender) and Tinder Wall (sacrifice: RR). Tapping the
// Battlement before the Wall is sacrificed counts both defenders; the
// reverse order (the planner's witness order) makes one G too few.
func TestSacrificeLastKeepsTheDefenderCount(t *testing.T) {
	b := newBoard(t, "Juggernaut", []string{"Overgrown Battlement", "Tinder Wall"}, nil)
	a := b.action(t)
	plan, _ := SelectPlan(&a)
	if len(plan.Activations) != 2 || plan.Activations[0].Source != b.objs["Tinder Wall"][0] {
		t.Logf("witness order %v no longer puts the Wall first; the reorder is then untested here", sources(plan))
	}
	x, err := Drive(b.e, Start(0, ptr(b.action(t)), PoolOf(b.e, 0)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if x.Status() != Done || !b.onStack() {
		t.Fatalf("status %v reason %s %s (plan %v)", x.Status(), x.Reason, x.Detail, sources(x.Plan))
	}
}

func ptr[T any](v T) *T { return &v }
