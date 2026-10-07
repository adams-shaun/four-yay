package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// nameCardEngine builds a two-seat engine whose Config carries the compiled
// corpus as NameUniverse — the production wiring cmd/gorged and mtgsim now
// supply — and seats the named corpus cards in seat 0's hand. Every card is
// the real corpus card (never a synthetic fixture), so a filter or offer the
// corpus card does not actually carry cannot pass.
func nameCardEngine(t *testing.T, hand ...string) (*Engine, *cards.Registry) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus precondition: Mountain missing")
	}
	if _, ok := reg.Lookup("Wasteland"); !ok {
		t.Fatal("corpus precondition: Wasteland missing")
	}
	deck := make([]*cards.Card, 40)
	for i := range deck {
		deck[i] = mountain
	}
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, deck}, NameUniverse: reg.AllCards()})
	// Precondition: the universe must actually be wired, or every assertion
	// below would pass vacuously against the no-universe fallback.
	if len(e.G.NameUniverse) == 0 {
		t.Fatal("precondition: NameUniverse not wired from Config")
	}
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	var ids []state.ObjID
	for _, name := range hand {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing corpus card %q", name)
		}
		o := e.G.AddObject(c, 0)
		o.Zone = state.ZHand
		ids = append(ids, o.ID)
	}
	e.G.SetZone(state.ZHand, 0, ids)
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	return e, reg
}

// pendingNameAsk returns the pending cast/ETB name decision, failing loudly
// if the engine is not asking for a name. The check that it IS an ask is
// what makes the "silently defaults" regression fail this test.
func pendingNameAsk(t *testing.T, e *Engine, where string) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "name" {
		t.Fatalf("%s: expected a name ask, got %+v", where, d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("%s: name ask Min/Max = %d/%d, want 1/1", where, d.Min, d.Max)
	}
	if d.Player != 0 {
		t.Fatalf("%s: name ask went to player %d, want the controller 0", where, d.Player)
	}
	return d
}

func labelIndex(d *decision.Decision, want string) int {
	for _, o := range d.Options {
		if o.Label == want {
			return o.Index
		}
	}
	return -1
}

// TestPithingNeedleNamesAnUnseenLand pins the entry-boundary "as this enters"
// path on the real corpus card: Pithing Needle carries no ValidCards$, so
// its name is ANY card — a Wasteland that is nowhere on the board included.
// Before the fix the arm defaulted the filter to Card.nonLand and only
// offered names visible in the caster's own hand/battlefield/graveyard, so
// the land was both filtered out and unseen.
func TestPithingNeedleNamesAnUnseenLand(t *testing.T) {
	t.Parallel()
	e, _ := nameCardEngine(t, "Pithing Needle")
	needle := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 1
	e.beginCast(0, decision.Option{Kind: "cast", Obj: needle})
	if got := e.G.Obj(needle).Zone; got != state.ZStack {
		t.Fatalf("Pithing Needle precondition: zone = %s, want stack", got)
	}
	e.resolveTop()
	d := pendingNameAsk(t, e, "Pithing Needle entry")
	if d.ResumeKind != "etb" {
		t.Fatalf("Pithing Needle name choice ResumeKind = %q, want etb", d.ResumeKind)
	}
	if len(d.Options) < 1000 {
		t.Fatalf("Needle offered only %d names; the full corpus universe is not wired", len(d.Options))
	}
	idx := labelIndex(d, "Wasteland")
	if idx < 0 {
		t.Fatal("Pithing Needle did not offer the unseen land Wasteland")
	}
	submitChoices(t, e, idx)
	if got := e.G.Obj(needle).ChosenName; got != "Wasteland" {
		t.Fatalf("Pithing Needle ChosenName = %q, want Wasteland", got)
	}
}

// TestNameAskBotAnswerValidates pins the legal-answer rule for the new ask
// end to end: the bot's own policy answer (botpolicy.Decide over the real
// BoardFromGame, the seat.Bot path) must pass Decision.Validate on the real
// corpus option list, so the deterministic bot can never re-submit a
// rejected answer and livelock the name window. The bare validate of the
// chosen index is the constraint this test binds.
func TestNameAskBotAnswerValidates(t *testing.T) {
	t.Parallel()
	e, _ := nameCardEngine(t, "Pithing Needle")
	needle := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 1
	e.beginCast(0, decision.Option{Kind: "cast", Obj: needle})
	if got := e.G.Obj(needle).Zone; got != state.ZStack {
		t.Fatalf("Pithing Needle precondition: zone = %s, want stack", got)
	}
	e.resolveTop()
	d := pendingNameAsk(t, e, "Pithing Needle entry")
	if d.ResumeKind != "etb" {
		t.Fatalf("Pithing Needle name choice ResumeKind = %q, want etb", d.ResumeKind)
	}
	in := newTestBot(3).answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %+v failed Decision.Validate: %v", in, err)
	}
	if len(in.Choices) != 1 {
		t.Fatalf("bot name answer = %+v, want exactly one choice", in)
	}
}

// TestPhyrexianRevokerNamesANonland pins the SA's own ValidCards$ filter on
// the real corpus card: Revoker carries `ValidCards$ Card.nonLand`, so the
// ask must exclude every land while still offering the full unseen universe
// of nonlands.
func TestPhyrexianRevokerNamesANonland(t *testing.T) {
	t.Parallel()
	e, _ := nameCardEngine(t, "Phyrexian Revoker")
	revoker := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	e.beginCast(0, decision.Option{Kind: "cast", Obj: revoker})
	if got := e.G.Obj(revoker).Zone; got != state.ZStack {
		t.Fatalf("Phyrexian Revoker precondition: zone = %s, want stack", got)
	}
	e.resolveTop()
	d := pendingNameAsk(t, e, "Phyrexian Revoker entry")
	if d.ResumeKind != "etb" {
		t.Fatalf("Phyrexian Revoker name choice ResumeKind = %q, want etb", d.ResumeKind)
	}
	if labelIndex(d, "Wasteland") >= 0 || labelIndex(d, "Forest") >= 0 || labelIndex(d, "Mountain") >= 0 {
		t.Fatal("Revoker (Card.nonLand) offered a land name")
	}
	idx := labelIndex(d, "Grizzly Bears")
	if idx < 0 {
		t.Fatal("Revoker did not offer the unseen nonland Grizzly Bears")
	}
	submitChoices(t, e, idx)
	if got := e.G.Obj(revoker).ChosenName; got != "Grizzly Bears" {
		t.Fatalf("Phyrexian Revoker ChosenName = %q, want Grizzly Bears", got)
	}
}
