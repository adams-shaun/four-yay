package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The triggered Cost$ CollectEvidence<N> family: six corpus trigger bodies
// carry an evidence cost and previously settled it FOR FREE -- the window's
// payable gate did not read Cost.Evidence, so it reported the cost Priceable
// (the mana half is empty) and the pay arm ran the body without exiling
// anything. These tests pin the real-corpus Izoni, Center of the Web end to
// end: the election is offered, paying poses a real graveyard selection,
// the chosen cards move graveyard -> exile carrying events.EvidenceCost, and
// only then does the body (two Spider tokens) run. All fixtures are inline
// synthetic cards plus corpus rules text; no Forge text is committed.

// evidenceEngine is handEngine with the corpus token registry wired in, so a
// body that creates a token (Izoni's two Spiders) actually creates it; the
// plain handEngine's Config carries no Tokens and the Token effect would
// silently no-op, making a token assertion vacuous.
func evidenceEngine(t *testing.T, reg *cards.Registry) *Engine {
	t.Helper()
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens})
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	return e
}

// evidenceGraveCard places a compiled card in seat 0's graveyard (the zone
// the evidence stage reads) and returns its id.
func evidenceGraveCard(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, 0)
	o.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
	return o.ID
}

// evidenceEvents returns the events.EvidenceCost moves since mark and the
// ids they name.
func evidenceEvents(e *Engine, mark int) []state.ObjID {
	var ids []state.ObjID
	for _, ev := range e.L.Events[mark:] {
		if events.IsEvidenceCost(ev) {
			ids = append(ids, ev.Obj)
		}
	}
	return ids
}

// spiderTokens counts seat p's battlefield tokens named "Spider Token".
func spiderTokens(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Spider") {
			n++
		}
	}
	return n
}

// TestIzoniEvidenceTriggerPaysAndExiles: with two Hill Giants (mana value 4
// each) in the graveyard, Izoni's ETB trigger offers the evidence cost for
// real; paying poses the mana-value-descending evidence selection, exiles
// exactly the chosen cards with the canonical evidence marker, and only then
// creates the two Spider tokens. Before the fix the pay option settled
// nothing (0 evidence events, graveyard untouched) yet ran the body.
func TestIzoniEvidenceTriggerPaysAndExiles(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	izoni := searchCorpusCard(t, reg, "Izoni, Center of the Web")
	giant := searchCorpusCard(t, reg, "Hill Giant")

	e := evidenceEngine(t, reg)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 0 {
		t.Fatalf("fixture graveyard = %d, want empty", got)
	}
	if got := spiderTokens(e, 0); got != 0 {
		t.Fatalf("fixture already has %d Spider tokens", got)
	}
	g1 := evidenceGraveCard(t, e, giant)
	g2 := evidenceGraveCard(t, e, giant)

	d := realCardETBCost(t, e, izoni)
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("CollectEvidence<4> was not offered as payable: %+v", d.Options)
	}

	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	// The evidence ask: Min = the greedy minimum card count reaching 4 (one
	// Hill Giant), options ordered mana value DESC.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "evidence" {
		t.Fatalf("evidence ask = %+v", d)
	}
	if d.Min != 1 {
		t.Fatalf("evidence ask Min = %d, want 1 (one mana-value-4 card reaches the threshold)", d.Min)
	}
	if len(evidenceEvents(e, mark)) != 0 {
		t.Fatalf("evidence exiled before the selection was answered")
	}
	submitChoices(t, e, d.Options[0].Index)

	ids := evidenceEvents(e, mark)
	if len(ids) != 1 {
		t.Fatalf("evidence events = %d, want 1", len(ids))
	}
	exiled, inGrave := 0, 0
	for _, id := range []state.ObjID{g1, g2} {
		switch e.G.Obj(id).Zone {
		case state.ZExile:
			exiled++
		case state.ZGraveyard:
			inGrave++
		}
	}
	if exiled != 1 || inGrave != 1 {
		t.Fatalf("graveyard cards after payment: exiled=%d graveyard=%d, want exactly one of each", exiled, inGrave)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 1 {
		t.Fatalf("graveyard size after payment = %d, want 1", got)
	}
	if got := spiderTokens(e, 0); got != 2 {
		t.Fatalf("Spider tokens after payment = %d, want 2", got)
	}
}

// TestIzoniEvidenceTriggerDeclineExilesNothing: the decline moves no
// evidence and creates no tokens. It asserts the pay option WAS offered
// (the window armed and the evidence gate priced it) so the test cannot pass
// with the whole feature unregistered.
func TestIzoniEvidenceTriggerDeclineExilesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	izoni := searchCorpusCard(t, reg, "Izoni, Center of the Web")
	giant := searchCorpusCard(t, reg, "Hill Giant")

	e := evidenceEngine(t, reg)
	g1 := evidenceGraveCard(t, e, giant)
	g2 := evidenceGraveCard(t, e, giant)

	d := realCardETBCost(t, e, izoni)
	pay, decline := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("CollectEvidence<4> was not offered as payable: %+v", d.Options)
	}

	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	if ids := evidenceEvents(e, mark); len(ids) != 0 {
		t.Fatalf("decline exiled %v as evidence, want nothing", ids)
	}
	if e.G.Obj(g1).Zone != state.ZGraveyard || e.G.Obj(g2).Zone != state.ZGraveyard {
		t.Fatalf("decline moved a graveyard card: g1=%s g2=%s", e.G.Obj(g1).Zone, e.G.Obj(g2).Zone)
	}
	if got := spiderTokens(e, 0); got != 0 {
		t.Fatalf("decline created %d Spider tokens, want 0", got)
	}
}

// TestIzoniEvidenceTriggerBotAnswerAccepted: the deterministic bot's own
// first answer (the first Min options, in the ask's mana-value-descending
// order) must be accepted by the validator on a board where the threshold
// binds. The greedy Min drives the ask; the validator re-derives the total
// from the same shared helper, so a mismatch would re-pose forever.
func TestIzoniEvidenceTriggerBotAnswerAccepted(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	izoni := searchCorpusCard(t, reg, "Izoni, Center of the Web")

	e := evidenceEngine(t, reg)
	mv2 := card(t, "Name:Evidence Two\nManaCost:2\nTypes:Artifact\nOracle:x\n")
	mv1 := card(t, "Name:Evidence One\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	// 2 + 2 + 2 + 1 = 7; the threshold is 4, so the greedy Min is 2 (the top
	// two 2-mana cards) and binds.
	evidenceGraveCard(t, e, mv2)
	evidenceGraveCard(t, e, mv2)
	evidenceGraveCard(t, e, mv2)
	evidenceGraveCard(t, e, mv1)

	d := realCardETBCost(t, e, izoni)
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("CollectEvidence<4> was not offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "evidence" {
		t.Fatalf("evidence ask = %+v", d)
	}
	if d.Min != 2 || len(d.Options) != 4 {
		t.Fatalf("evidence ask Min=%d options=%d, want Min 2 of 4", d.Min, len(d.Options))
	}
	// The bot's first answer: the first Min options.
	var bot []int
	for i := 0; i < d.Min; i++ {
		bot = append(bot, d.Options[i].Index)
	}
	submitChoices(t, e, bot...)

	if ids := evidenceEvents(e, mark); len(ids) != d.Min {
		t.Fatalf("bot answer exiled %d cards, want %d (it must be accepted, not re-posed)", len(ids), d.Min)
	}
	// The bot picked the first Min options -- the greedy minimum, top mana
	// values first. Each of those must be exiled; the rest stay put.
	for i := 0; i < d.Min; i++ {
		if e.G.Obj(d.Options[i].Obj).Zone != state.ZExile {
			t.Fatalf("bot-selected card %d zone = %s, want Exile", d.Options[i].Obj, e.G.Obj(d.Options[i].Obj).Zone)
		}
	}
	for i := d.Min; i < len(d.Options); i++ {
		if e.G.Obj(d.Options[i].Obj).Zone != state.ZGraveyard {
			t.Fatalf("unselected card %d zone = %s, want Graveyard", d.Options[i].Obj, e.G.Obj(d.Options[i].Obj).Zone)
		}
	}
	if e.Pending() != nil && e.Pending().Kind == decision.KChoose && len(e.Pending().Options) > 0 &&
		e.Pending().Options[0].Kind == "evidence" {
		t.Fatalf("the evidence ask was re-posed after a legal bot answer")
	}
}

// evidenceReserveLooter mirrors Lamplight Phoenix's cost shape: an
// ExileAnyGrave part and a CollectEvidence part over the SAME graveyard, so
// the evidence candidates must exclude the card the exile part already
// reserved. The synthetic ETB trigger keeps the setup small; the cost tokens
// are the real carrier's verbatim shapes.
const evidenceReserveLooter = "Name:Evidence Looter\nManaCost:2 U\nTypes:Creature Human Wizard\nPT:1/1\n" +
	"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost\n" +
	"SVar:TrigCost:AB$ Draw | Cost$ ExileAnyGrave<1/Card> CollectEvidence<4> | NumCards$ 1\n" +
	"Oracle:x\n"

// TestEvidenceTriggerReservesAcrossParts: the ExileAnyGrave part takes one
// graveyard card and the CollectEvidence<4> part must reach 4 using DIFFERENT
// cards. With two mana-value-4 Hill Giants, the exile takes one and the
// evidence takes the other; one object paying both would leave a single
// evidence event (or a shortfall) and is rejected here.
func TestEvidenceTriggerReservesAcrossParts(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	giant := searchCorpusCard(t, reg, "Hill Giant")
	zero := card(t, "Name:Evidence Zero\nManaCost:0\nTypes:Artifact\nOracle:x\n")

	e := handEngine(t)
	// Put the evidence-enabling 4-MV card first, followed by a 0-MV card.
	// The gate must find the disjoint allocation (reserve the zero) rather
	// than blindly reserving the first candidate.
	g1 := evidenceGraveCard(t, e, giant)
	g2 := evidenceGraveCard(t, e, zero)

	d := etbCostWindow(t, e, evidenceReserveLooter)
	pay, _ := windowPayDecline(t, d)
	if pay < 0 {
		t.Fatalf("ExileAnyGrave + CollectEvidence<4> was not offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay)

	// The component choice must exclude the 4-MV card: choosing it would
	// leave only a 0-MV card and make the already-offered evidence payment
	// impossible. The sole legal choice is the zero-MV card.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "exile_cost" {
		t.Fatalf("evidence-compatible exile ask = %+v, want only zero-MV candidate", d)
	}
	if d.Options[0].Obj != g2 {
		t.Fatalf("component option = %d, want zero-MV card %d (4-MV card %d must be preserved)", d.Options[0].Obj, g2, g1)
	}
	submitChoices(t, e, d.Options[0].Index)

	// The evidence stage then needs the remaining 4-mana card; exactly two
	// distinct cards must have left the graveyard.
	evidence := evidenceEvents(e, mark)
	exiledAsCost := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.To == state.ZExile && ev.Text == "exiled as a cost" {
			exiledAsCost++
		}
	}
	if exiledAsCost != 1 {
		t.Fatalf("exile-component moves = %d, want 1", exiledAsCost)
	}
	if len(evidence) != 1 {
		t.Fatalf("evidence moves = %d, want 1", len(evidence))
	}
	if evidence[0] == exiledAsCostID(e, mark) {
		t.Fatalf("the same object paid both parts: evidence=%d", evidence[0])
	}
	if e.G.Obj(g1).Zone != state.ZExile || e.G.Obj(g2).Zone != state.ZExile {
		t.Fatalf("after both parts: g1=%s g2=%s, want both exiled", e.G.Obj(g1).Zone, e.G.Obj(g2).Zone)
	}
}

// TestDynamicEvidenceTriggerDeclinesLoudly checks the reachable trigger-window
// path for CollectEvidence<X>: it is decline-only and records why, rather
// than silently treating the unresolved amount as free.
func TestDynamicEvidenceTriggerDeclinesLoudly(t *testing.T) {
	t.Parallel()
	const dynamic = "Name:Dynamic Evidence\nManaCost:2 U\nTypes:Creature Wizard\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigCost\n" +
		"SVar:TrigCost:AB$ Draw | Cost$ CollectEvidence<X> | NumCards$ 1\nOracle:x\n"
	e := handEngine(t)
	giant := card(t, "Name:Evidence Four\nManaCost:4\nTypes:Artifact\nOracle:x\n")
	evidenceGraveCard(t, e, giant)
	mark := len(e.L.Events)
	d := etbCostWindow(t, e, dynamic)
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "trigger_cost_decline" {
		t.Fatalf("dynamic evidence ask = %+v, want decline-only", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	found := false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "collect evidence amount is dynamic") {
			found = true
		}
		if events.IsEvidenceCost(ev) {
			t.Fatalf("unresolved dynamic evidence was paid: %+v", ev)
		}
	}
	if !found {
		t.Fatal("declining dynamic evidence emitted no diagnostic Note")
	}
}

// exiledAsCostID returns the object named by the single "exiled as a cost"
// move since mark (0 if none).
func exiledAsCostID(e *Engine, mark int) state.ObjID {
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.To == state.ZExile && ev.Text == "exiled as a cost" {
			return ev.Obj
		}
	}
	return 0
}
