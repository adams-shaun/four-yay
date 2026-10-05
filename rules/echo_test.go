package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kw:Echo (CR 702.35a) pinned end to end on the REAL corpus cards — Karmic
// Guide is the issue's carrier. The helpers come from search_library_test.go
// (searchTestRegistry, searchCorpusCard, searchMoveByName), cast_test.go
// (submitChoices), draw_step_test.go (driveToStep, answerIfDiscard) and
// delayed_test.go (submitPass) — all the same package. The deck is built from
// compiled corpus cards only, so no Forge script text is committed. Karmic
// Guide is in no legacy golden deck and no repo deck carries any of the 52
// echo cards, so no chain head or ratchet entry depends on any of this.

// echoEntryEngine builds a two-seat engine whose seat-0 deck leads with the
// named echo permanent, moves it from hand to the battlefield (the
// reanimation-shaped entry: a real events.MoveZone, which is what stamps the
// control-acquisition tuple), then places `plains` Plains beside it and
// returns the permanent's id.
func echoEntryEngine(t *testing.T, fixture string, plains int) (*Engine, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	card := searchCorpusCard(t, reg, fixture)
	forest := searchCorpusCard(t, reg, "Forest")
	plainsCard := searchCorpusCard(t, reg, "Plains")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{card}
	for i := 0; i < 8; i++ {
		deck = append(deck, plainsCard, forest)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 9304, Names: []string{"echoer", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := searchMoveByName(t, e, fixture, state.ZBattlefield)
	for i := 0; i < plains; i++ {
		searchMoveByName(t, e, "Plains", state.ZBattlefield)
	}
	return e, id
}

// driveEchoQuiet passes priority (answering any cleanup discard, attacker or
// blocker declaration with the empty/default answer) until (turn, active,
// step) is reached or the echo election for `id` is pending — the drain: the
// upkeep trigger queues on the StepChange, is placed on the stack, everyone
// passes, resolveTop runs startEcho and the election (or its payment window)
// is asked. Returns nil when the destination was reached without an election.
func driveEchoQuiet(t *testing.T, e *Engine, id state.ObjID, turn int32, active state.PlayerID, step state.Step) *decision.Decision {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return nil
		}
		if e.G.Over {
			t.Fatalf("game ended at turn %d seat %d step %s before reaching turn %d seat %d step %s",
				e.G.Turn, e.G.Active, e.G.Step, turn, active, step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		if d.Kind == decision.KChoose && d.Source == id {
			return d
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		default:
			if answerIfDiscard(t, e) {
				continue
			}
			t.Fatalf("unexpected decision %+v while waiting for the echo election", d)
		}
	}
	t.Fatal("destination never reached")
	return nil
}

// tapUntilElection drains the payment window an openable echo cost poses
// first (the shared paymentManaAsk window): tap the first offered source
// until the window stops reopening, then return the election.
func tapUntilElection(t *testing.T, e *Engine, id state.ObjID, taps int) *decision.Decision {
	t.Helper()
	d := e.Pending()
	for i := 0; i < taps; i++ {
		if d == nil || d.Kind != decision.KChoose || d.Source != id ||
			len(d.Options) == 0 || d.Options[0].Kind != "activate" {
			break
		}
		submitChoices(t, e, 0)
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KChoose || d.Source != id {
		t.Fatalf("expected the echo election, got %+v", d)
	}
	return d
}

func lastMoveZoneText(e *Engine, obj state.ObjID) string {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == obj {
			return ev.Text
		}
	}
	return ""
}

// TestEchoKarmicGuidePaysEchoOrIsSacrificed is the registered kw:Echo proof:
// (a) the echo trigger fires at the controller's first upkeep after the
// battlefield entry, (b) the election is posed with a payable pay option
// first (through the shared payment window), and (c) paying keeps the Angel.
func TestEchoKarmicGuidePaysEchoOrIsSacrificed(t *testing.T) {
	t.Parallel()
	e, guide := echoEntryEngine(t, "Karmic Guide", 5)
	// (a) drive through seat 0's next upkeep (turn 3): the echo election is
	// asked mid-upkeep, before the drive can reach the draw step.
	d := driveEchoQuiet(t, e, guide, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election at the first upkeep after the entry")
	}
	// (b) the shared payment window opens first (pool empty, untapped
	// sources); tap five Plains, then the election shows pay FIRST (option 0
	// is the bot clamp fallback's sane default) with the sacrifice below it.
	d = tapUntilElection(t, e, guide, 8)
	if len(d.Options) != 2 || d.Options[0].Kind != "echo_pay" || d.Options[1].Kind != "echo_sac" {
		t.Fatalf("echo election = %+v", d.Options)
	}
	// (c) paying keeps the Angel, empties the pool and the stack.
	submitChoices(t, e, 0)
	if e.G.Obj(guide).Zone != state.ZBattlefield {
		t.Fatalf("paid echo but the guide is in %s", e.G.Obj(guide).Zone)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("echo trigger still on the stack: %v", e.G.Stack)
	}
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("echo payment left %d mana in the pool", total)
	}
}

// TestEchoKarmicGuideDeclineSacrificesWithEchoText covers (d): declining (or
// not paying) sacrifices the guide, and the move carries the echo text.
func TestEchoKarmicGuideDeclineSacrificesWithEchoText(t *testing.T) {
	t.Parallel()
	e, guide := echoEntryEngine(t, "Karmic Guide", 2)
	// Two Plains only: the payment window can never float {3}{W}{W}, so
	// "done" closes it and the election has NO pay option — sacrifice only.
	d := driveEchoQuiet(t, e, guide, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election at the first upkeep after the entry")
	}
	if len(d.Options) == 0 || d.Options[0].Kind != "activate" {
		t.Fatalf("expected the payment window first, got %+v", d.Options)
	}
	done := d.Options[len(d.Options)-1].Index
	submitChoices(t, e, done)
	d = e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "echo_sac" {
		t.Fatalf("unpayable echo election = %+v, want sacrifice-only", d)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(guide); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("declined echo but the guide is in %v", o)
	}
	if text := lastMoveZoneText(e, guide); text != "sacrificed for echo" {
		t.Fatalf("sacrifice move text = %q", text)
	}
}

// TestEchoKarmicGuideGateSuppressesLaterUpkeeps covers (e): the guide alive
// after paying upkeep N owes nothing at N+1 — the intervening-if suppresses
// the trigger before it stacks, so the election never appears again.
func TestEchoKarmicGuideGateSuppressesLaterUpkeeps(t *testing.T) {
	t.Parallel()
	e, guide := echoEntryEngine(t, "Karmic Guide", 5)
	driveEchoQuiet(t, e, guide, 3, 0, state.StepDraw)
	d := tapUntilElection(t, e, guide, 8)
	if d.Options[0].Kind != "echo_pay" {
		t.Fatalf("election before paying = %+v", d.Options)
	}
	submitChoices(t, e, 0) // pay; the guide stays
	// Driving to seat 0's turn-5 draw crosses the turn-5 upkeep: the gate
	// must suppress the trigger, so the drive arrives WITHOUT an election.
	if d := driveEchoQuiet(t, e, guide, 5, 0, state.StepDraw); d != nil {
		t.Fatalf("echo election wrongly posed at the later upkeep: %+v", d.Options)
	}
	if e.G.Obj(guide).Zone != state.ZBattlefield {
		t.Fatalf("guide moved after the paid upkeep: %s", e.G.Obj(guide).Zone)
	}
}

// TestEchoKarmicGuideControlChangeRearms covers (f): a battlefield control
// change re-arms the election — the round trip lands on seat 0 during their
// own turn 3 (after the draw step has recorded its upkeep), so seat 1 never
// holds the guide across their own upkeep, and at turn 5's upkeep the guide
// is owed again.
func TestEchoKarmicGuideControlChangeRearms(t *testing.T) {
	t.Parallel()
	e, guide := echoEntryEngine(t, "Karmic Guide", 5)
	d := driveEchoQuiet(t, e, guide, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election at the first upkeep after the entry")
	}
	d = tapUntilElection(t, e, guide, 8)
	submitChoices(t, e, 0) // pay at turn 3's upkeep
	// Cross the draw step (it records seat 0's upkeep) into main 1.
	driveEchoQuiet(t, e, guide, 3, 0, state.StepMain1)
	// CR 400.7: the round trip is a fresh control acquisition for seat 0.
	e.emit(events.Event{Kind: events.ControlChange, Obj: guide, Player: state.PlayerID(1)})
	e.emit(events.Event{Kind: events.ControlChange, Obj: guide, Player: state.PlayerID(0)})
	e.pending = nil
	e.priorityRound()
	d = driveEchoQuiet(t, e, guide, 5, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no re-armed echo election at turn 5's upkeep")
	}
	// No floating mana: close the window, take the sacrifice-only election.
	done := d.Options[len(d.Options)-1].Index
	submitChoices(t, e, done)
	d = e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "echo_sac" {
		t.Fatalf("re-armed echo election = %+v", d)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(guide); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("re-armed echo answered with sacrifice but the guide is in %v", o)
	}
}

// TestEchoShahOfNaarIsleFreePay pins the plain-mana `0` shape: the election
// IS posed with a free pay arm (option 0), and paying it keeps the Shah.
func TestEchoShahOfNaarIsleFreePay(t *testing.T) {
	t.Parallel()
	e, shah := echoEntryEngine(t, "Shah of Naar Isle", 0)
	d := driveEchoQuiet(t, e, shah, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election for the free-pay Shah")
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "echo_pay" || d.Options[1].Kind != "echo_sac" {
		t.Fatalf("free-pay echo election = %+v", d.Options)
	}
	submitChoices(t, e, 0)
	if e.G.Obj(shah).Zone != state.ZBattlefield {
		t.Fatalf("paid the free echo but the Shah is in %s", e.G.Obj(shah).Zone)
	}
}

// TestEchoUnresolvableCostStaysLoud pins the Q3 answer: Volcano Hellion's
// K:Echo:X (X priced by an SVar at the controller's life total) skips the
// election with ONE loud Note and the Hellion stays — never a silent
// sacrifice, never a silent keep.
func TestEchoUnresolvableCostStaysLoud(t *testing.T) {
	t.Parallel()
	e, hellion := echoEntryEngine(t, "Volcano Hellion", 0)
	// The ETB trigger's chain (ChooseNumber -> DB$ DealDamage | ValidTgts$
	// Creature) now announces the DealDamage target at placement (CR 603.3d,
	// ResumeKind trig_sub) before anyone gets priority; the ChooseNumber
	// choice then happens at resolution. Answer the target first, then 0 (the
	// former deterministic fallback) to keep this test about echo costs.
	target := passUntilAsk(t, e)
	if target == nil || target.Kind != decision.KTarget || target.ResumeKind != "trig_sub" {
		t.Fatalf("ETB ask = %+v, want the Hellion's placement target ask", target)
	}
	submitChoices(t, e, 0)
	number := passUntilAsk(t, e)
	if number == nil || number.Kind != decision.KChoose || number.ResumeKind != "choosenumber" || number.Source != hellion {
		t.Fatalf("ETB ask = %+v, want the Hellion's ChooseNumber ask", number)
	}
	if len(number.Options) == 0 || number.Options[0].Kind != "number" || number.Options[0].Amount != 0 {
		t.Fatalf("Hellion's number ask does not offer 0 first: %+v", number.Options)
	}
	submitChoices(t, e, number.Options[0].Index)
	// Drive through the turn-3 upkeep: no election may ever be asked, one
	// loud note names the unresolved cost, the Hellion stays.
	driveEchoQuiet(t, e, hellion, 3, 0, state.StepDraw)
	loud := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "echo cost unresolvable") {
			loud++
		}
	}
	if loud != 1 {
		t.Fatalf("loud unresolvable-echo notes = %d, want exactly 1", loud)
	}
	if o := e.G.Obj(hellion); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("hellion = %v, want on the battlefield", o)
	}
}

// TestEchoDeepcavernImpPaysByDiscarding pins the Q2 answer for the
// Discard<1/Card> shape (2 corpus files): the election is posed, the pay arm
// reuses the cumulative action vocabulary (parseCumulativeAction +
// cumulativeObjects), and the answered discard keeps the Imp.
func TestEchoDeepcavernImpPaysByDiscarding(t *testing.T) {
	t.Parallel()
	e, imp := echoEntryEngine(t, "Deepcavern Imp", 0)
	d := driveEchoQuiet(t, e, imp, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election for Deepcavern Imp")
	}
	if d.Options[0].Kind != "echo_pay" {
		t.Fatalf("imp election = %+v, want pay first (a hand always exists)", d.Options)
	}
	before := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, 0) // pay
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("echo discard ask = %+v", d)
	}
	submitChoices(t, e, 0)
	if e.G.Obj(imp).Zone != state.ZBattlefield {
		t.Fatalf("paid echo but the imp is in %s", e.G.Obj(imp).Zone)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != before-1 {
		t.Fatalf("hand = %d, want %d after the echo discard", got, before-1)
	}
}

// TestEchoSkizzikSurgerPaysBySacrificingLands pins the Sac<2/Land> shape
// (1 corpus file): two Plains on the battlefield make the pay arm payable and
// the answered sacrifice of both keeps the Surger.
func TestEchoSkizzikSurgerPaysBySacrificingLands(t *testing.T) {
	t.Parallel()
	e, surger := echoEntryEngine(t, "Skizzik Surger", 2)
	d := driveEchoQuiet(t, e, surger, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election for Skizzik Surger")
	}
	if d.Options[0].Kind != "echo_pay" {
		t.Fatalf("surger election = %+v, want pay first (two lands on board)", d.Options)
	}
	submitChoices(t, e, 0) // pay
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("echo sacrifice ask = %+v, want both Plains", d)
	}
	submitChoices(t, e, 0, 1)
	if e.G.Obj(surger).Zone != state.ZBattlefield {
		t.Fatalf("paid echo but the surger is in %s", e.G.Obj(surger).Zone)
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got != 1 {
		t.Fatalf("battlefield = %d permanents, want the surger alone", got)
	}
}
