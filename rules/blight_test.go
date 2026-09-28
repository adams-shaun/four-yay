package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Blight (CR 701.60) primitive pinned end to end on the REAL corpus
// cards. Pre-fix, every one of these bodies hit the generic "unimplemented
// API Blight" Note and did nothing. The helpers come from
// search_library_test.go (searchTestRegistry, searchCorpusCard,
// searchMoveByName), cast_test.go (submitChoices, toMain1, addMana),
// draw_step_test.go (driveToStep), replacement_updated_test.go
// (passUntilStackEmpty) and activate_test.go (abilityOption) — all the same
// package. The decks are built from compiled corpus cards only, so no Forge
// script text is committed. None of the six carriers is in any repo deck or
// legacy golden deck, so no chain head depends on these cards (measured: the
// heads gate in the report).

// blightEngine deals seat 0 a 40-card deck whose first cards are the named
// fixtures, then eight Forests and eight Mountains and Grizzly Bears; the
// other seats' decks are all Mountains (plus any fixture named for them via
// blightSeatDeck). seatZeroStart keeps seat 0 the starting seat.
func blightEngine(t *testing.T, reg *cards.Registry, seats int, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	decks := [][]*cards.Card{deck}
	for i := 1; i < seats; i++ {
		// Each opponent's deck opens with two Grizzly Bears (in hand after the
		// deal, where blightMove reaches them) over Mountains.
		opp := []*cards.Card{bear, bear}
		for len(opp) < 40 {
			opp = append(opp, mountain)
		}
		decks = append(decks, opp)
	}
	cfg := seatZeroStart(Config{Seed: 9204,
		Names: []string{"blighter", "opponent", "third"}[:seats],
		Decks: decks, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// blightMove moves the named corpus card from seat's hand/library to zone and
// returns its id — searchMoveByName generalised past seat 0, for the
// opponents' creatures the Morcant test needs.
func blightMove(t *testing.T, e *Engine, seat state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, seat) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from seat %d hand/library", name, seat)
	return 0
}

// blightCounters collects the log's canonical M1M1 CounterChange events as
// id→total amount.
func blightCounters(t *testing.T, e *Engine) map[state.ObjID]int32 {
	t.Helper()
	out := map[state.ObjID]int32{}
	for _, ev := range e.L.Events {
		if ev.Kind != events.CounterChange || ev.Counter != "M1M1" {
			continue
		}
		out[ev.Obj] += ev.Amount
	}
	return out
}

// blightSVar resolves a face's named SVar into its SA (the trigger bodies
// this task's carriers execute).
func blightSVar(t *testing.T, reg *cards.Registry, cardName, svar string) *cards.SA {
	t.Helper()
	c := searchCorpusCard(t, reg, cardName)
	sa := cards.ResolveSVar(c.Faces[0].SVars, svar)
	if sa == nil {
		t.Fatalf("%s: SVar %s unresolved", cardName, svar)
	}
	return sa
}

// blightAnswerBlight answers a pending blight KChoose with its first option
// (botpolicy's own clamp answer) and returns the decision that was answered.
func blightAnswerBlight(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "blight" {
		t.Fatalf("pending %+v, want a blight KChoose", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	return d
}

// pendingBlightAsk returns the pending decision when it is a blight KChoose
// (a mid-resolution ask), and nil otherwise — a direct effects.Resolve that
// completes leaves the ordinary priority decision pending, which is not an
// ask.
func pendingBlightAsk(e *Engine) *decision.Decision {
	d := e.Pending()
	if d != nil && d.Kind == decision.KChoose && d.ResumeKind == "blight" {
		return d
	}
	return nil
}

// passToSeat passes priority (whoever holds it) until the named seat holds
// the pending decision — how the opponent's turn lets the instant-speed
// Dose of Dawnglow reach its caster.
func passToSeat(t *testing.T, e *Engine, seat state.PlayerID) {
	t.Helper()
	for i := 0; i < 50; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision while passing priority")
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while passing", d)
		}
		if d.Player == seat {
			return
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		submitChoices(t, e, idx)
	}
	t.Fatalf("seat %d never received priority", seat)
}

// drainStackEmpty passes priority until the stack is empty, answering any
// mid-resolution blight KChoose with its first option (botpolicy's own clamp
// answer). Returns the number of blight asks it answered — a caller that
// expects none (the Dose main-phase gate) asserts on it.
func drainStackEmpty(t *testing.T, e *Engine, limit int) int {
	t.Helper()
	answered := 0
	for n := 0; n < limit && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "blight" {
			submitChoices(t, e, d.Options[0].Index)
			answered++
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while draining the stack", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	return answered
}

// castDose funds, casts, targets and resolves a Dose of Dawnglow; target is
// the given graveyard bear id. Returns the number of blight asks the drain
// answered.
func castDose(t *testing.T, e *Engine, target state.ObjID) int {
	t.Helper()
	dose := blightMove(t, e, 0, "Dose of Dawnglow", state.ZHand)
	addMana(t, e, 0, "BBBBB")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == dose {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Dose of Dawnglow: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after cast %+v, want the reanimate target ask", d)
	}
	tidx := -1
	for _, o := range d.Options {
		if o.Obj == target {
			tidx = o.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("target %d not offered: %+v", target, d.Options)
	}
	submitChoices(t, e, tidx)
	return drainStackEmpty(t, e, 30)
}

// TestBlightDoseOfDawnglowGateHoldsInOwnMainPhase is the gate-0 branch: cast
// in the caster's own main phase, the Count$InOwnMainPhase gate short-circuits
// the chained DBBlight — the reanimate works, nothing is blighted, no ask.
func TestBlightDoseOfDawnglowGateHoldsInOwnMainPhase(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 2, "Dose of Dawnglow")
	bearA := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bearB := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	grave := blightMove(t, e, 0, "Grizzly Bears", state.ZGraveyard)
	if answered := castDose(t, e, grave); answered != 0 {
		t.Fatalf("%d blight asks posed in the caster's own main phase, want 0", answered)
	}
	// The reanimated bear is on the battlefield…
	if o := e.G.Obj(grave); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("reanimated bear zone = %v, want battlefield", o)
	}
	// …and nothing was blighted (the gate held).
	if got := blightCounters(t, e); len(got) != 0 {
		t.Fatalf("counters = %v, want none in the caster's own main phase", got)
	}
	if bearA == 0 || bearB == 0 {
		t.Fatal("board fixtures missing")
	}
}

// TestBlightDoseOfDawnglowBlightsOutsideMainPhase is the gate-1 branch: cast
// on the OPPONENT's turn (an instant), the gate opens, the controller is asked
// which of their creatures takes the two −1/−1 counters, and the answer lands.
func TestBlightDoseOfDawnglowBlightsOutsideMainPhase(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 2, "Dose of Dawnglow")
	bearA := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bearB := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	grave := blightMove(t, e, 0, "Grizzly Bears", state.ZGraveyard)
	// Seat 1's turn (an instant is castable there); seat 1 passes priority so
	// seat 0 can cast.
	driveToStep(t, e, 2, 1, state.StepMain1)
	passToSeat(t, e, 0)
	// Resolution: the reanimate, then the gate-1 blight 2 — a real ask to
	// seat 0 (three creatures are controlled), answered first-option.
	if answered := castDose(t, e, grave); answered != 1 {
		t.Fatalf("%d blight asks posed outside the main phase, want exactly 1", answered)
	}
	got := blightCounters(t, e)
	var hit, miss int
	for id, n := range got {
		if id == bearA {
			hit += int(n)
		} else if id == bearB {
			miss += int(n)
		}
	}
	if hit+miss != 2 || hit == 0 && miss == 0 || hit != 0 && miss != 0 {
		t.Fatalf("counters = %v, want exactly 2 M1M1 on ONE of bearA(%d)/bearB(%d)",
			got, bearA, bearB)
	}
	// The reanimated bear is on the battlefield too.
	if o := e.G.Obj(grave); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("reanimated bear zone = %v, want battlefield", o)
	}
}

// TestBlightShadowUchinAsksOnlyWithTwoCreatures pins the strict-supersets
// leaf on the real trigger body: Defined$ You / Num$ 1 — with two controlled
// creatures a real KChoose is posed and the answer lands; with exactly one,
// the counter is placed silently with no decision.
func TestBlightShadowUchinAsksOnlyWithTwoCreatures(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	blight := blightSVar(t, reg, "Shadow Urchin", "DBBlight")

	// One creature: silent.
	e, _ := blightEngine(t, reg, 2, "Shadow Urchin")
	urchin := blightMove(t, e, 0, "Shadow Urchin", state.ZBattlefield)
	effects.Resolve(e, &effects.Ctx{Source: urchin, Controller: 0,
		SVars: searchCorpusCard(t, reg, "Shadow Urchin").Faces[0].SVars}, blight)
	if d := pendingBlightAsk(e); d != nil {
		t.Fatalf("ask posed %+v with a single controlled creature", d)
	}
	if got := blightCounters(t, e); got[urchin] != 1 {
		t.Fatalf("counters = %v, want 1 silent M1M1 on the urchin (%d)", got, urchin)
	}

	// Two creatures: the ask, the answer, the counter.
	e2, _ := blightEngine(t, reg, 2, "Shadow Urchin")
	urchin2 := blightMove(t, e2, 0, "Shadow Urchin", state.ZBattlefield)
	bear2 := blightMove(t, e2, 0, "Grizzly Bears", state.ZBattlefield)
	effects.Resolve(e2, &effects.Ctx{Source: urchin2, Controller: 0,
		SVars: searchCorpusCard(t, reg, "Shadow Urchin").Faces[0].SVars}, blight)
	d := blightAnswerBlight(t, e2)
	got := blightCounters(t, e2)
	want := d.Options[0].Obj
	if got[want] != 1 || (want != urchin2 && want != bear2) {
		t.Fatalf("counters = %v, want 1 on the answered %d (urchin %d, bear %d)",
			got, want, urchin2, bear2)
	}
}

// TestBlightHighPerfectMorcantAsksEachOpponent pins the multi-player
// continuation: Defined$ Opponent asks each opponent that controls two or
// more creatures in turn (the first ask suspends; the re-entry applies its
// answer and continues to the later targets), silently blights an opponent
// with exactly one creature, and never touches the Elf's own controller's
// creatures.
func TestBlightHighPerfectMorcantAsksEachOpponent(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 3, "High Perfect Morcant")
	morcant := blightMove(t, e, 0, "High Perfect Morcant", state.ZBattlefield)
	opp1a := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	opp1b := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	// Seat 2's two bears are moved to the graveyard first, so its leg pins
	// the CR 701.60 no-creature arm: unharmed, never failing open onto
	// someone else's creature (the silent single-creature leg is the Urchin
	// test's).
	blightMove(t, e, 2, "Grizzly Bears", state.ZGraveyard)
	blightMove(t, e, 2, "Grizzly Bears", state.ZGraveyard)
	effects.Resolve(e, &effects.Ctx{Source: morcant, Controller: 0,
		SVars: searchCorpusCard(t, reg, "High Perfect Morcant").Faces[0].SVars},
		blightSVar(t, reg, "High Perfect Morcant", "TrigBlight"))
	// Seat 1 controls two creatures: the ask is theirs (target index 0 in
	// AliveFrom order).
	d := e.Pending()
	if d == nil || d.ResumeKind != "blight" || d.Player != 1 {
		t.Fatalf("pending %+v, want seat 1's blight ask", d)
	}
	blightAnswerBlight(t, e)
	got := blightCounters(t, e)
	if len(got) != 1 || got[opp1a]+got[opp1b] != 1 {
		t.Fatalf("counters = %v, want exactly 1 on one of seat 1's bears (%d/%d)",
			got, opp1a, opp1b)
	}
	if got[morcant] != 0 {
		t.Fatalf("the Elf's controller was blighted: %v", got)
	}
	if d := pendingBlightAsk(e); d != nil {
		t.Fatalf("a second blight ask was left pending: %+v", d)
	}
}

// TestBlightChaosSpewerUnlessGate pins the shared unless gate around the
// blight body: paying {2} spares the blight entirely; declining lets the body
// run and blight 2. The gate itself is effects.Resolve's unlessProceed — the
// body never reads UnlessCost$.
func TestBlightChaosSpewerUnlessGate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	trig := blightSVar(t, reg, "Chaos Spewer", "TrigBlight")

	// Pay {2}: no blight, pool drained by 2.
	e, _ := blightEngine(t, reg, 2, "Chaos Spewer")
	spewer := blightMove(t, e, 0, "Chaos Spewer", state.ZBattlefield)
	blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e, 0, "CC")
	effects.Resolve(e, &effects.Ctx{Source: spewer, Controller: 0,
		SVars: searchCorpusCard(t, reg, "Chaos Spewer").Faces[0].SVars}, trig)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending %+v, want the unless-pay offer", d)
	}
	before := lifeOf(t, e, 0)
	submitChoices(t, e, d.Options[0].Index) // pay
	if got := blightCounters(t, e); len(got) != 0 {
		t.Fatalf("counters = %v after paying, want none", got)
	}
	if lifeOf(t, e, 0) != before {
		t.Fatal("life changed on a mana payment")
	}

	// Decline: blight 2 over the controlled creatures (Spewer + Bear is two:
	// the ask is posed, answered below). The payer floats {2} so BOTH
	// branches are offered -- the unless gate suppresses an unreachable pay,
	// and this seat's pool alone covers {2}.
	e2, _ := blightEngine(t, reg, 2, "Chaos Spewer")
	spewer2 := blightMove(t, e2, 0, "Chaos Spewer", state.ZBattlefield)
	bear2 := blightMove(t, e2, 0, "Grizzly Bears", state.ZBattlefield)
	addMana(t, e2, 0, "CC")
	effects.Resolve(e2, &effects.Ctx{Source: spewer2, Controller: 0,
		SVars: searchCorpusCard(t, reg, "Chaos Spewer").Faces[0].SVars}, trig)
	d2 := e2.Pending()
	if d2 == nil || d2.ResumeKind != "unless_pay" {
		t.Fatalf("pending %+v, want the unless-pay offer", d2)
	}
	if len(d2.Options) != 2 {
		t.Fatalf("payable {2} should offer pay and decline: %+v", d2.Options)
	}
	submitChoices(t, e2, d2.Options[1].Index) // decline
	// The body runs on a decline: blight 2 over the controlled creatures —
	// Spewer + Bear is two, so the ask is posed; answer it (first option).
	if d3 := pendingBlightAsk(e2); d3 != nil {
		submitChoices(t, e2, d3.Options[0].Index)
	}
	got := blightCounters(t, e2)
	total := got[spewer2] + got[bear2]
	if total != 2 || len(got) != 1 {
		t.Fatalf("counters = %v, want 2 M1M1 on ONE of the declined blight's creatures (spewer %d, bear %d)",
			got, spewer2, bear2)
	}
}

// TestBlightChampionOfTheWeirdActivationEndToEnd activates the real corpus
// ability: the cost half (PayLife<1> Blight<2>) pays one life and blights the
// activator's own chosen creature, and the effect body makes the TARGETED
// OPPONENT (not the activator) blight 2. The cost half working here is also
// the regression guard the brief asks for.
func TestBlightChampionOfTheWeirdActivationEndToEnd(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 2, "Champion of the Weird")
	champ := blightMove(t, e, 0, "Champion of the Weird", state.ZBattlefield)
	bear := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// The targeted opponent keeps exactly ONE creature (its second deck bear
	// goes to the graveyard), so the body's blight 2 lands silently on it.
	oppBear := blightMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	blightMove(t, e, 1, "Grizzly Bears", state.ZGraveyard)
	o := e.G.Obj(champ)
	abIdx := -1
	for i, sa := range o.Face().Abilities {
		if sa.Kind == "AB" && sa.API == "Blight" {
			abIdx = i
		}
	}
	if abIdx < 0 {
		t.Fatalf("no Blight ability on Champion of the Weird: %+v", o.Face().Abilities)
	}
	opt := abilityOption(t, e, champ, abIdx)
	submitChoices(t, e, opt.Index)
	// The activation's own decision sequence in the engine's order: the
	// Blight<2> cost ask (two controlled creatures → a real KChoose) and the
	// CR 601.2c target-opponent ask.
	costPick := state.ObjID(0)
	for i := 0; i < 5; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		switch {
		case d.Kind == decision.KChoose && (d.ResumeKind == "blightcost" ||
			(len(d.Options) > 0 && d.Options[0].Kind == "blightcost")):
			costPick = d.Options[0].Obj
			submitChoices(t, e, d.Options[0].Index)
		case d.Kind == decision.KTarget:
			tidx := -1
			for _, o := range d.Options {
				if o.Player == 1 {
					tidx = o.Index
				}
			}
			if tidx < 0 {
				t.Fatalf("opponent not offered as a target: %+v", d.Options)
			}
			submitChoices(t, e, tidx)
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
	}
	passUntilStackEmpty(t, e, 30)
	got := blightCounters(t, e)
	if got[costPick] != 2 || (costPick != champ && costPick != bear) {
		t.Fatalf("cost blight = %v, want 2 on the activator's chosen %d (champ %d, bear %d)",
			got, costPick, champ, bear)
	}
	if got[oppBear] != 2 {
		t.Fatalf("counters = %v, want 2 on the TARGETED OPPONENT's bear (%d)", got, oppBear)
	}
	if lifeOf(t, e, 0) != 19 {
		t.Fatalf("life = %d, want 19 after PayLife<1>", lifeOf(t, e, 0))
	}
}

// lifeOf reads a seat's life total.
func lifeOf(t *testing.T, e *Engine, p state.PlayerID) int32 {
	t.Helper()
	if int(p) >= len(e.G.Players) {
		t.Fatalf("seat %d out of range", p)
	}
	return e.G.Players[p].Life
}

// TestBlightSinisterGnarlbarkChainsAfterTheDraw pins the chained-sub shape:
// the end-step trigger resolves DB$ Draw (NumCards$ 1, Defined$ You) and its
// SubAbility$ DBBlight runs after it — the controller draws, then blights 1
// (one controlled creature: silent, the strict-supersets arm again).
func TestBlightSinisterGnarlbarkChainsAfterTheDraw(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 2, "Sinister Gnarlbark")
	gnarlbark := blightMove(t, e, 0, "Sinister Gnarlbark", state.ZBattlefield)
	before := len(e.G.Zone(state.ZHand, 0))
	effects.Resolve(e, &effects.Ctx{Source: gnarlbark, Controller: 0,
		SVars: searchCorpusCard(t, reg, "Sinister Gnarlbark").Faces[0].SVars},
		blightSVar(t, reg, "Sinister Gnarlbark", "TrigDraw"))
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("hand = %d, want %d after the chained draw", got, before+1)
	}
	if got := blightCounters(t, e); got[gnarlbark] != 1 {
		t.Fatalf("counters = %v, want 1 silent M1M1 on the gnarlbark (%d)", got, gnarlbark)
	}
}
