package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Shire Shirriff is the ticket's own example of the "you may <pay>. When you
// do, <effect>" idiom: its ETB trigger's Execute is
// `AB$ ImmediateTrigger | Cost$ Sac<1/Card.token/token> | Execute$ TrigExile`,
// and the chained DB$ ChangeZone carries Duration$ UntilHostLeavesPlay. The
// api:ImmediateTrigger primitive and the triggered-cost pay/decline window
// were merged in 453f516d; these tests pin the reported card end to end on
// the REAL corpus card (the licensing rule: no Forge script text is
// committed here) -- the same harness shape rules/forum_filibuster_test.go
// uses for its own when-you-do carriers.

// shireEngine seats seat 0 a deck whose first card is Shire Shirriff followed
// by basics/bears, moves it onto the battlefield (the ETB trigger queues on
// the placement), mints a fresh fixture token beside it with putToken, and
// moves an opponent Grizzly Bears out of seat 1's library onto the
// battlefield with a raw MoveZone (the probe shape). The toss is forced to
// seat 0 (seatZeroStart). Returns the engine parked at seat 0's Main 1 with
// priority freshly asked, plus the three object ids.
func shireEngine(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Shire Shirriff")}
	for len(deck) < 40 {
		deck = append(deck, forest, bear)
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 20; i++ {
		opp = append(opp, forest, bear)
	}
	cfg := seatZeroStart(Config{Seed: 9223, Names: []string{"shire", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	shire := searchMoveByName(t, e, "Shire Shirriff", state.ZBattlefield)
	token := putToken(t, e, 0, "Name:Food\nTypes:Artifact Food\nOracle:x\n", state.ZBattlefield)
	var bearID state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			bearID = id
			break
		}
	}
	if bearID == 0 {
		t.Fatal("no Grizzly Bears in the opponent's library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: state.ZLibrary, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	return e, shire, token, bearID
}

// shirePayThrough drives the ETB's pay/decline window: it asserts the window
// ask's shape, answers PAY, asserts the sacrifice settled, answers the
// body's mid-resolution target ask with the opponent creature, and leaves
// the engine just after the exile resolved.
func shirePayThrough(t *testing.T, e *Engine, token, bear state.ObjID) {
	t.Helper()
	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("cost window = %+v, want the two-option pay/decline KChoose", d)
	}
	if d.Options[0].Kind != "trigger_cost_pay" || d.Options[1].Kind != "trigger_cost_decline" {
		t.Fatalf("cost window options = %+v", d.Options)
	}
	if !strings.Contains(d.Prompt, "sacrifice token") {
		t.Fatalf("cost window prompt %q, want it to name the sacrifice in prose", d.Prompt)
	}
	if strings.ContainsAny(d.Prompt, "<>") || strings.Contains(d.Prompt, "Sac<") {
		t.Fatalf("cost window prompt %q leaks raw cost syntax", d.Prompt)
	}
	submitChoices(t, e, 0)

	// The answered "pay" sacrificed the token: one real MoveZone
	// battlefield->graveyard (the token then ceases to exist per CR 111.7,
	// which is why the assertion is on the event, not the final zone).
	sac := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == token &&
			ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			sac = true
		}
	}
	if !sac {
		t.Fatalf("the paid cost never moved the token to the graveyard (tail %+v)", tailEmit(e, 8))
	}

	// CR 603.12: the "when you do" half is a reflexive triggered ability, so
	// its target is a real placement target ask (KTarget), addressed to the
	// controller as the ability is put on the stack.
	dt := passUntilAsk(t, e)
	if dt == nil || dt.Kind != decision.KTarget || dt.Player != 0 {
		t.Fatalf("post-pay ask = %+v, want the reflexive ability's KTarget for seat 0", dt)
	}
	idx := -1
	for _, o := range dt.Options {
		if o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the opponent's bear was not offered as the exile target: %+v", dt.Options)
	}
	submitChoices(t, e, idx)
	if got := e.G.Obj(bear); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("the bear left before the reflexive ability resolved (obj %+v)", got)
	}
	passUntilStackEmpty(t, e, 20)
}

// TestShireShirriffWhenYouDoPayExilesUntilItLeaves is the accepted-and-paid
// leg: the window asks pay/decline, pay sacrifices the token, the reflexive
// ability's target ask appears as it goes on the stack, and it exiles the
// opponent's creature with a Duration$ UntilHostLeavesPlay marker on the
// exiler.
func TestShireShirriffWhenYouDoPayExilesUntilItLeaves(t *testing.T) {
	reg := searchTestRegistry(t)
	e, shire, token, bear := shireEngine(t, reg)
	shirePayThrough(t, e, token, bear)

	if got := e.G.Obj(bear); got == nil || got.Zone != state.ZExile {
		t.Fatalf("the answered target never reached exile (obj %+v)", got)
	}
	so := e.G.Obj(shire)
	if so == nil || len(so.ExileReturn) != 1 || so.ExileReturn[0].Obj != bear ||
		so.ExileReturn[0].From != state.ZBattlefield {
		t.Fatalf("expected one ExileReturn entry naming the bear from the battlefield, got %+v", so)
	}
}

// TestShireShirriffWhenYouDoDeclineExecutesNothing pins CR 603.5's decline
// half: answering "Do not pay" sacrifices nothing, exiles nothing, and no
// follow-up ask appears -- the trigger's cost was optional ("you may").
func TestShireShirriffWhenYouDoDeclineExecutesNothing(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _, token, bear := shireEngine(t, reg)

	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("cost window = %+v, want the two-option pay/decline KChoose", d)
	}
	submitChoices(t, e, 1) // decline

	if next := e.Pending(); next != nil && next.Kind != decision.KPriority {
		t.Fatalf("a follow-up decision followed the decline: %+v (a declined cost executes no body, CR 603.5)", next)
	}
	if got := e.G.Obj(bear); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("the decline still exiled the bear (obj %+v)", got)
	}
	if got := e.G.Obj(token); got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("the decline still sacrificed the token (obj %+v)", got)
	}
}

// TestShireShirriffExiledCreatureReturnsWhenItLeaves is the return leg: the
// Duration$ UntilHostLeavesPlay sweep returns the exiled bear to the
// battlefield under its owner's control when Shire Shirriff leaves, and the
// sweep prunes the entries it served (the TestChangeZoneDurationUntilHostLeavesPlay
// pattern -- a raw battlefield-departure MoveZone reaches the same sweep the
// destroy path does).
func TestShireShirriffExiledCreatureReturnsWhenItLeaves(t *testing.T) {
	reg := searchTestRegistry(t)
	e, shire, token, bear := shireEngine(t, reg)
	shirePayThrough(t, e, token, bear)

	e.emit(events.Event{Kind: events.MoveZone, Obj: shire, From: state.ZBattlefield, To: state.ZGraveyard})

	got := e.G.Obj(bear)
	if got == nil || got.Zone != state.ZBattlefield {
		t.Fatalf("the bear should have returned to the battlefield when Shire left (obj %+v)", got)
	}
	if got.Controller != 1 {
		t.Fatalf("returned bear must be under its owner's control, controller=%v", got.Controller)
	}
	if n := len(e.G.Obj(shire).ExileReturn); n != 0 {
		t.Fatalf("sweep must prune the entries it returned, %d left", n)
	}
}
