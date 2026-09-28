// CR 601.2b flexible-pip announcement for the two upkeep payment windows:
// Echo (kw:Echo, rules/echo.go) and cumulative upkeep (K:Cumulative upkeep,
// rules/cumulative.go). Before the fix both windows gated on
// Cost.Priceable() against the UNANNOUNCED cost, and Priceable() rejects
// hybrid, monocolour-hybrid, Phyrexian and hybrid-Phyrexian pips -- so a
// flexible upkeep cost was treated as unpayable and the pay option was
// omitted (sacrifice only). No corpus card carries such a cost (measured
// zero), so the fixtures here are inline synthetic cards; the engine under
// test is the real one, driven through the real resolution window. Kept in
// its own file so the ticket cannot conflict on a shared test file.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// flexUpkeepEngine deals seat 0 (the starting seat) an inline fixture card
// plus inline basic lands, puts the fixture on the battlefield with a logged
// MoveZone (which stamps the control-acquisition tuple Echo's gate reads),
// and returns the engine, the replayable config and the permanent's id. The
// lands are named per colour so a test can tap the exact producer it needs.
func flexUpkeepEngine(t *testing.T, fixtureSrc string, seat0Lands []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, fixtureSrc)
	name := fixture.Faces[0].Name
	var deck []*cards.Card
	deck = append(deck, fixture)
	for _, src := range seat0Lands {
		deck = append(deck, card(t, src))
	}
	for len(deck) < 40 {
		deck = append(deck, card(t, "Name:Filler\nTypes:Basic Land\nOracle:x\n"))
	}
	cfg := seatZeroStart(Config{Seed: 8573, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := searchMoveByName(t, e, name, state.ZBattlefield)
	for _, src := range seat0Lands {
		searchMoveByName(t, e, card(t, src).Faces[0].Name, state.ZBattlefield)
	}
	return e, cfg, id
}

// flexPipDecision fills the pending decision into the fixture's pip ask: it
// asserts the decision is a KChoose for the fixture whose options are the
// pay_* family, so a test that never reaches the announcement fails loudly
// rather than passing vacuously. It returns the offered face kinds in order.
func flexPipDecision(t *testing.T, e *Engine, id state.ObjID) []string {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != id || !isPipAsk(d.Options) {
		t.Fatalf("expected the flexible-pip announcement for %d, got %+v", id, d)
	}
	var faces []string
	for _, o := range d.Options {
		faces = append(faces, o.Kind)
	}
	return faces
}

// TestEchoFlexiblePipPayment is the Echo regression: an {W/U} echo cost is
// announced (both faces offered and distinct), the elected blue face is paid
// from an Island, the permanent survives, and only the elected resource is
// consumed.
func TestEchoFlexiblePipPayment(t *testing.T) {
	t.Parallel()
	const echoHybrid = "Name:Test Echo Hybrid\nManaCost:2\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Echo:W/U\nOracle:test\n"
	e, cfg, bear := flexUpkeepEngine(t, echoHybrid, []string{
		"Name:Island\nTypes:Basic Land Island\nOracle:x\n",
		"Name:Forest\nTypes:Basic Land Forest\nOracle:x\n",
	})
	// Precondition the rule actually reads: the permanent is on the
	// battlefield, it prints Echo, and its keyword parsed to a real hybrid
	// pip rather than a generic substitute.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the echo permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if !e.G.Obj(bear).Face().HasKeyword("Echo") {
		t.Fatal("precondition: the fixture does not print Echo")
	}
	if c := ParseCost("W/U"); len(c.Hybrid) != 1 || len(c.Phyrexian) != 0 {
		t.Fatalf("precondition: ParseCost(W/U) = %+v, want one hybrid pip", c)
	}

	d := driveEchoQuiet(t, e, bear, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election at the first upkeep after the entry")
	}
	// The announcement comes FIRST, before the mana window (CR 601.2b).
	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 || faces[0] != "pay_W" || faces[1] != "pay_U" {
		t.Fatalf("hybrid {W/U} faces = %v, want the distinct pay_W and pay_U", faces)
	}
	submitChoices(t, e, 1) // elect blue

	// The mana window can now float blue; tap the Island and re-open the
	// election. A Forest is on the board too, so electing blue and paying
	// from blue is a genuine colour choice, not the only producer available.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear ||
		len(d.Options) == 0 || d.Options[0].Kind != "activate" {
		t.Fatalf("expected the mana-ability window after the announcement, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the echo election, got %+v", d)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "echo_pay" {
		t.Fatalf("hybrid echo election = %+v, want a pay option once blue is in the pool", d.Options)
	}
	poolBefore := e.G.Players[0].Pool
	if poolBefore[state.MU] != 1 || poolBefore[state.MW] != 0 {
		t.Fatalf("precondition: pool before payment = %+v, want exactly one blue", poolBefore)
	}
	submitChoices(t, e, 0) // pay

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid the hybrid echo but the permanent is %+v", o)
	}
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("hybrid echo left %d mana in the pool, want the elected blue consumed", total)
	}
	replayCheck(t, e, cfg)
}

// TestEchoPhyrexianPipPaidWithLife is the Phyrexian face: a {U/P} echo cost
// offers both the blue face and the two-life face, and paying life keeps the
// permanent at no mana cost.
func TestEchoPhyrexianPipPaidWithLife(t *testing.T) {
	t.Parallel()
	const echoPhyrexian = "Name:Test Echo Phyrexian\nManaCost:2\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Echo:UP\nOracle:test\n"
	e, cfg, bear := flexUpkeepEngine(t, echoPhyrexian, []string{
		"Name:Island\nTypes:Basic Land Island\nOracle:x\n",
	})
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the echo permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if c := ParseCost("UP"); len(c.Phyrexian) != 1 || len(c.Hybrid) != 0 {
		t.Fatalf("precondition: ParseCost(UP) = %+v, want one Phyrexian pip", c)
	}

	d := driveEchoQuiet(t, e, bear, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election at the first upkeep after the entry")
	}
	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 || faces[0] != "pay_U" || faces[1] != "pay_life" {
		t.Fatalf("Phyrexian {U/P} faces = %v, want pay_U and pay_life", faces)
	}
	submitChoices(t, e, 1) // pay 2 life

	lifeBefore := e.G.Players[0].Life
	if lifeBefore != 20 {
		t.Fatalf("precondition: seat 0 life = %d, want 20", lifeBefore)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear ||
		len(d.Options) < 2 || d.Options[0].Kind != "echo_pay" {
		t.Fatalf("Phyrexian echo election = %+v, want a pay option after electing life", d)
	}
	submitChoices(t, e, 0) // pay

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid life for the echo but the permanent is %+v", o)
	}
	if life := e.G.Players[0].Life; life != lifeBefore-2 {
		t.Fatalf("life after the Phyrexian echo = %d, want %d (two paid)", life, lifeBefore-2)
	}
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("Phyrexian echo consumed %d mana, want none", total)
	}
	replayCheck(t, e, cfg)
}

// TestEchoUnpayableElectionStillSacrifices is the decline half: with no mana
// source the announcement is still posed (a CR 601.2b choice), but the pay
// option is absent (the announced face cannot be covered) and the sacrifice
// path is retained.
func TestEchoUnpayableElectionStillSacrifices(t *testing.T) {
	t.Parallel()
	const echoHybrid = "Name:Test Echo Hybrid\nManaCost:2\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Echo:W/U\nOracle:test\n"
	e, cfg, bear := flexUpkeepEngine(t, echoHybrid, nil)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the echo permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if n := len(untappedManaSourceIDs(e, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 has %d untapped mana sources, want 0", n)
	}

	d := driveEchoQuiet(t, e, bear, 3, 0, state.StepDraw)
	if d == nil {
		t.Fatal("no echo election at the first upkeep after the entry")
	}
	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 {
		t.Fatalf("hybrid {W/U} faces = %v, want both announcement faces", faces)
	}
	submitChoices(t, e, 0) // elect white, then find no mana
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the echo election, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "echo_sac" {
		t.Fatalf("unpayable hybrid echo election = %+v, want sacrifice-only", d.Options)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("declined echo but the permanent is %+v", o)
	}
	if text := lastMoveZoneText(e, bear); text != "sacrificed for echo" {
		t.Fatalf("sacrifice move text = %q", text)
	}
	replayCheck(t, e, cfg)
}

// flexCumulativeEngine seeds seat 0 with an inline cumulative-upkeep fixture
// and lands, places it on the battlefield and resolves one upkeep trigger so
// the payment window is live (one age counter accrued).
func flexCumulativeEngine(t *testing.T, fixtureSrc string, seat0Lands []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, id := flexUpkeepEngine(t, fixtureSrc, seat0Lands)
	resolveUpkeepCumulative(t, e)
	return e, cfg, id
}

// TestCumulativeUpkeepFlexiblePipPayment is the cumulative-upkeep regression:
// a {W/U} upkeep cost is announced, the elected green face is paid from a
// Forest, the permanent survives and only the elected resource is consumed.
func TestCumulativeUpkeepFlexiblePipPayment(t *testing.T) {
	t.Parallel()
	const cumulativeHybrid = "Name:Test Cumulative Hybrid\nManaCost:1\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Cumulative upkeep:W/U\nOracle:test\n"
	e, cfg, bear := flexCumulativeEngine(t, cumulativeHybrid, []string{
		"Name:Island\nTypes:Basic Land Island\nOracle:x\n",
		"Name:Forest\nTypes:Basic Land Forest\nOracle:x\n",
	})
	// Precondition: the permanent is on the battlefield and the age counter
	// the window reads exists.
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the cumulative permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if !e.G.Obj(bear).Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: the fixture does not print Cumulative upkeep")
	}
	if got := e.G.Obj(bear).Counter("AGE"); got != 1 {
		t.Fatalf("precondition: age counters = %d, want 1", got)
	}
	if c := ParseCost("W/U"); len(c.Hybrid) != 1 {
		t.Fatalf("precondition: ParseCost(W/U) = %+v, want one hybrid pip", c)
	}

	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 || faces[0] != "pay_W" || faces[1] != "pay_U" {
		t.Fatalf("hybrid {W/U} faces = %v, want the distinct pay_W and pay_U", faces)
	}
	submitChoices(t, e, 1) // elect blue

	// Drain the mana window: tap the Island, then re-open to the election.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear ||
		len(d.Options) == 0 || d.Options[0].Kind != "activate" {
		t.Fatalf("expected the mana-ability window after the announcement, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the cumulative election, got %+v", d)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("hybrid cumulative election = %+v, want a pay option once blue is in the pool", d.Options)
	}
	poolBefore := e.G.Players[0].Pool
	if poolBefore[state.MU] != 1 || poolBefore[state.MW] != 0 {
		t.Fatalf("precondition: pool before payment = %+v, want exactly one blue", poolBefore)
	}
	submitChoices(t, e, 0) // pay

	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid the hybrid upkeep but the permanent is %+v", o)
	}
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("hybrid upkeep left %d mana in the pool, want the elected blue consumed", total)
	}
	replayCheck(t, e, cfg)
}

// TestCumulativeUpkeepUnpayableElectionStillSacrifices is the cumulative
// decline half: no mana source means the announcement is posed but the pay
// option is absent and the sacrifice path is retained.
func TestCumulativeUpkeepUnpayableElectionStillSacrifices(t *testing.T) {
	t.Parallel()
	const cumulativeHybrid = "Name:Test Cumulative Hybrid\nManaCost:1\nTypes:Creature Bear\nPT:2/2\n" +
		"K:Cumulative upkeep:W/U\nOracle:test\n"
	e, cfg, bear := flexCumulativeEngine(t, cumulativeHybrid, nil)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: the cumulative permanent is not on the battlefield: %+v", e.G.Obj(bear))
	}
	if n := len(untappedManaSourceIDs(e, 0)); n != 0 {
		t.Fatalf("precondition: seat 0 has %d untapped mana sources, want 0", n)
	}

	faces := flexPipDecision(t, e, bear)
	if len(faces) != 2 {
		t.Fatalf("hybrid {W/U} faces = %v, want both announcement faces", faces)
	}
	submitChoices(t, e, 0) // elect white, then find no mana
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != bear {
		t.Fatalf("expected the cumulative election, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "cumulative_sac" {
		t.Fatalf("unpayable hybrid cumulative election = %+v, want sacrifice-only", d.Options)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("declined upkeep but the permanent is %+v", o)
	}
	if text := lastMoveZoneText(e, bear); text != "sacrificed for cumulative upkeep" {
		t.Fatalf("sacrifice move text = %q", text)
	}
	replayCheck(t, e, cfg)
}
