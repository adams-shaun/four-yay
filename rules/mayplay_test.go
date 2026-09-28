package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMayPlayStaticGrantsLandFromGraveyard drives Conduit of Worlds' real
// static -- S:Mode$ Continuous | Affected$ Land.YouOwn | MayPlay$ True |
// AffectedZone$ Graveyard -- through the priority walk: a basic land in its
// controller's graveyard must be offered as an ordinary play_land (spending
// the turn's land drop), and committing it must move it from the graveyard
// onto the battlefield. A land played this way is an ordinary land play, not
// an additional drop, so a second land may not be played the same turn.
func TestMayPlayStaticGrantsLandFromGraveyard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	conduit, ok := reg.Lookup("Conduit of Worlds")
	if !ok {
		t.Fatal("Conduit of Worlds missing from corpus")
	}
	if d := conduit.Link(); len(d) != 0 {
		t.Fatalf("link Conduit of Worlds: %v", d)
	}
	e, _, _ := newFixtureDeck(t, 211, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	co := e.G.AddObject(conduit, 0)
	co.Zone = state.ZBattlefield
	lo := e.G.AddObject(mountainCard(t), 0)
	lo.Zone = state.ZGraveyard
	// An opponent's land in THEIR graveyard: the grant is the static
	// controller's, so seat 1 walking its own graveyard gets nothing.
	fo := e.G.AddObject(mountainCard(t), 1)
	fo.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), co.ID))
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), lo.ID))
	e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), fo.ID))
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	var playIdx int = -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == lo.ID {
			playIdx = o.Index
		}
	}
	if playIdx < 0 {
		t.Fatalf("granted graveyard land not offered as play_land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{playIdx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if lo.Zone != state.ZBattlefield {
		t.Fatalf("land zone %v, want battlefield", lo.Zone)
	}
	if e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("LandsPlayed = %d, want 1 (the granted play is the land drop)", e.G.Players[0].LandsPlayed)
	}
	// The land drop is now spent: the opponent's land in their graveyard and
	// any second seat-0 land must not be offered this turn.
	d = e.Pending()
	if d == nil {
		t.Fatal("no pending decision after the land play")
	}
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			t.Fatalf("play_land still offered after the drop: %+v", o)
		}
	}
}

// TestMayPlayGrantStaysWithItsController verifies the battlefield grant's
// scope the negative way: with Conduit of Worlds under seat 0's control,
// seat 1's priority must not offer a play_land for a card in seat 1's own
// graveyard (Affected$ Land.YouOwn resolves against the static's controller).
func TestMayPlayGrantStaysWithItsController(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	conduit, ok := reg.Lookup("Conduit of Worlds")
	if !ok {
		t.Fatal("Conduit of Worlds missing from corpus")
	}
	e, _, _ := newFixtureDeck(t, 212, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	co := e.G.AddObject(conduit, 0)
	co.Zone = state.ZBattlefield
	fo := e.G.AddObject(mountainCard(t), 1)
	fo.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), co.ID))
	e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), fo.ID))
	// Pass priority to seat 1 in seat 0's main phase (seat 1 may act there at
	// instant speed; a land play needs its own turn, so nothing may be
	// offered either way -- this asserts the grant does not leak).
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 1, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("pending = %+v, want priority for seat 1", d)
	}
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == fo.ID {
			t.Fatalf("opponent's graveyard land offered a grant meant for seat 0: %+v", o)
		}
	}
}

// TestMosswortBridgeAmountAllPlaysEveryExiledCard drives Mosswort Bridge's
// real Play SA -- AB$ Play | Defined$ ExiledWith | Amount$ All | Optional$ True
// | WithoutManaCost$ True -- with TWO cards carrying the bridge's ExiledWith
// provenance in exile: the ask offers both, an answer may select several, and
// every selected card is played onto the battlefield free of its mana cost.
func TestMosswortBridgeAmountAllPlaysEveryExiledCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bridge, ok := reg.Lookup("Mosswort Bridge")
	if !ok {
		t.Fatal("Mosswort Bridge missing from corpus")
	}
	if d := bridge.Link(); len(d) != 0 {
		t.Fatalf("link Mosswort Bridge: %v", d)
	}
	e, _, _ := newFixtureDeck(t, 213, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	bo := e.G.AddObject(bridge, 0)
	bo.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), bo.ID))
	// Two creatures with 10 total power gate the condition (X >= 10).
	for i := 0; i < 2; i++ {
		c := e.G.AddObject(card(t, "Name:Power\nManaCost:3 G\nTypes:Creature\nPT:5/5\nOracle:x\n"), 0)
		c.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), c.ID))
	}
	// Two exiled cards with the bridge's provenance, plus one WITHOUT it
	// (the grant population is Defined$ ExiledWith, so that one is not
	// offered).
	var exiled [2]state.ObjID
	for i := 0; i < 2; i++ {
		o := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
		o.Zone = state.ZExile
		o.ExiledWith = bo.ID
		e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), o.ID))
		exiled[i] = o.ID
	}
	stray := e.G.AddObject(card(t, "Name:Stray\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	stray.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), stray.ID))
	// Fund {G} for the activation cost and untap the bridge (hideaway lands
	// enter tapped).
	e.G.Obj(bo.ID).Tapped = false
	e.G.Players[0].Pool[state.MG] = 1
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	ability := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == bo.ID {
			ability = o.Index
		}
	}
	if ability < 0 {
		t.Fatalf("bridge ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{ability}}); err != nil {
		t.Fatalf("activate bridge: %v", err)
	}
	for d = e.Pending(); d != nil && d.Kind != decision.KModes; d = e.Pending() {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("Play ask = %+v, want KModes", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("Play ask bounds Min=%d Max=%d, want Min 0 (Optional) Max 2 (Amount$ All over two candidates)", d.Min, d.Max)
	}
	idx := map[state.ObjID]int{}
	for _, o := range d.Options {
		if o.Obj == stray.ID {
			t.Fatalf("card without ExiledWith provenance offered: %+v", d.Options)
		}
		if o.Obj == exiled[0] || o.Obj == exiled[1] {
			idx[o.Obj] = o.Index
		}
	}
	if len(idx) != 2 {
		t.Fatalf("not both exiled cards offered: %+v", d.Options)
	}
	// Select BOTH cards (Amount$ All semantics: several in one answer).
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Choices: []int{idx[exiled[1]], idx[exiled[0]]}}); err != nil {
		t.Fatalf("submit two-card play: %v", err)
	}
	passUntilStackEmpty(t, e, 60)
	for i, id := range exiled {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("exiled card %d zone %v, want battlefield (Amount$ All plays every answer)", i, e.G.Obj(id).Zone)
		}
		if e.G.Obj(id).Tapped {
			t.Fatalf("exiled card %d entered tapped", i)
		}
	}
	if stray.Zone != state.ZExile {
		t.Fatalf("unprovenanced card moved: zone %v", stray.Zone)
	}
}

// TestMosswortBridgePlayMayDecline pins the Optional$ True decline: the same
// ask answered EMPTY plays nothing -- both exiled cards stay in exile, the
// activation still resolves (it is consumed), and the game continues.
func TestMosswortBridgePlayMayDecline(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bridge, ok := reg.Lookup("Mosswort Bridge")
	if !ok {
		t.Fatal("Mosswort Bridge missing from corpus")
	}
	e, _, _ := newFixtureDeck(t, 214, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	bo := e.G.AddObject(bridge, 0)
	bo.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), bo.ID))
	for i := 0; i < 2; i++ {
		c := e.G.AddObject(card(t, "Name:Power\nManaCost:3 G\nTypes:Creature\nPT:5/5\nOracle:x\n"), 0)
		c.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), c.ID))
	}
	eo := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	eo.Zone = state.ZExile
	eo.ExiledWith = bo.ID
	e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), eo.ID))
	e.G.Obj(bo.ID).Tapped = false
	e.G.Players[0].Pool[state.MG] = 1
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	ability := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == bo.ID {
			ability = o.Index
		}
	}
	if ability < 0 {
		t.Fatalf("bridge ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{ability}}); err != nil {
		t.Fatalf("activate bridge: %v", err)
	}
	for d = e.Pending(); d != nil && d.Kind != decision.KModes; d = e.Pending() {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("Play ask = %+v, want KModes", d)
	}
	// The empty answer is a legal decline of an Optional$ Play.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
		t.Fatalf("submit decline: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if eo.Zone != state.ZExile {
		t.Fatalf("declined card still left exile: zone %v", eo.Zone)
	}
	d = e.Pending()
	if d == nil {
		t.Fatal("engine wedged after declining the Play ask")
	}
}

func mountainCard(t *testing.T) *cards.Card {
	t.Helper()
	return card(t, "Name:Mountain\nTypes:Land Mountain\nOracle:x\n")
}

// TestKaZarGrantsPlayLandFromTopOfLibrary drives Ka-Zar of the Savage Land's
// real static -- S:Mode$ Continuous | Affected$ Land.TopLibrary+YouCtrl |
// AffectedZone$ Library | MayPlay$ True -- through the priority walk: the top
// card of its controller's library, when a land, must be offered as an
// ordinary play_land that consumes the turn's land drop and moves from the
// library onto the battlefield. The card BENEATH the top is never offered --
// a MayPlay grant exposes exactly the one card it lets you play, and walking
// only the top card keeps deeper library identities out of the option list.
func TestKaZarGrantsPlayLandFromTopOfLibrary(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kazar, ok := reg.Lookup("Ka-Zar of the Savage Land")
	if !ok {
		t.Fatal("Ka-Zar of the Savage Land missing from corpus")
	}
	if d := kazar.Link(); len(d) != 0 {
		t.Fatalf("link Ka-Zar: %v", d)
	}
	e, _, _ := newFixtureDeck(t, 215, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ko := e.G.AddObject(kazar, 0)
	ko.Zone = state.ZBattlefield
	top := e.G.AddObject(mountainCard(t), 0)
	beneath := e.G.AddObject(card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n"), 0)
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ko.ID))
	// The library's fixture cards sit beneath the two lands; the TOP of the
	// library is index 0 (drawFor draws lib[0]).
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{top.ID, beneath.ID},
		e.G.Zone(state.ZLibrary, 0)...))
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	var playIdx int = -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == top.ID {
			playIdx = o.Index
		}
		if o.Kind == "play_land" && o.Obj == beneath.ID {
			t.Fatalf("non-top library card offered: %+v", o)
		}
	}
	if playIdx < 0 {
		t.Fatalf("top-of-library land not offered as play_land: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{playIdx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if top.Zone != state.ZBattlefield {
		t.Fatalf("land zone %v, want battlefield (played from the library)", top.Zone)
	}
	if e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("LandsPlayed = %d, want 1 (the granted play is the land drop)", e.G.Players[0].LandsPlayed)
	}
	d = e.Pending()
	if d == nil {
		t.Fatal("no pending decision after the land play")
	}
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			t.Fatalf("play_land still offered after the drop: %+v", o)
		}
	}
}

// TestVergeRangersOffersTopLibraryLandWhenOpponentAhead drives Verge Rangers'
// real static -- S:Mode$ Continuous | Affected$ Land.TopLibrary+YouCtrl |
// AffectedZone$ Library | MayPlay$ True | CheckSVar$ X | SVarCompare$ GTY,
// X = PlayerCountOpponents$HighestValid Land.YouCtrl, Y = Count$Valid
// Land.YouCtrl -- through the priority walk. The static's SVar gate is an
// intervening-if the may-play path must EVALUATE, not withhold: with an
// opponent controlling more lands the top-of-library land is offered, played
// and consumes the land drop; with no opponent ahead the same board offers
// nothing. Before the fix mayPlayGateRejected rejected the whole static on
// CheckSVar$/SVarCompare$ and the top card was never offered.
func TestVergeRangersOffersTopLibraryLandWhenOpponentAhead(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	rangers, ok := reg.Lookup("Verge Rangers")
	if !ok {
		t.Fatal("Verge Rangers missing from corpus")
	}
	if d := rangers.Link(); len(d) != 0 {
		t.Fatalf("link Verge Rangers: %v", d)
	}

	// Positive arm: seat 1 controls more lands than seat 0, so the gate holds
	// and the top library land is offered.
	e, _, _ := newFixtureDeck(t, 217, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ro := e.G.AddObject(rangers, 0)
	ro.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ro.ID))
	// Seat 1 is ahead 2-0 on lands (two Mountains to none).
	for i := 0; i < 2; i++ {
		m := e.G.AddObject(mountainCard(t), 1)
		m.Zone = state.ZBattlefield
		e.G.SetZone(state.ZBattlefield, 1, append(e.G.Zone(state.ZBattlefield, 1), m.ID))
	}
	top := e.G.AddObject(mountainCard(t), 0)
	beneath := e.G.AddObject(card(t, "Name:Forest\nTypes:Land Forest\nOracle:x\n"), 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{top.ID, beneath.ID},
		e.G.Zone(state.ZLibrary, 0)...))
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	playIdx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == top.ID {
			playIdx = o.Index
		}
		if o.Kind == "play_land" && o.Obj == beneath.ID {
			t.Fatalf("non-top library card offered: %+v", o)
		}
	}
	if playIdx < 0 {
		t.Fatalf("opponent ahead 2-0: top-of-library land not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{playIdx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	if top.Zone != state.ZBattlefield {
		t.Fatalf("land zone %v, want battlefield (played from the library)", top.Zone)
	}
	if e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("LandsPlayed = %d, want 1 (the granted play is the land drop)", e.G.Players[0].LandsPlayed)
	}
	// The land drop is spent: no further play_land is offered.
	d = e.Pending()
	if d == nil {
		t.Fatal("no pending decision after the land play")
	}
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			t.Fatalf("play_land still offered after the drop: %+v", o)
		}
	}

	// Negative arm: seat 0 now controls as many lands as seat 1, so the gate
	// fails and the top library land must NOT be offered.
	e2, _, _ := newFixtureDeck(t, 217, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ro2 := e2.G.AddObject(rangers, 0)
	ro2.Zone = state.ZBattlefield
	e2.G.SetZone(state.ZBattlefield, 0, append(e2.G.Zone(state.ZBattlefield, 0), ro2.ID))
	for i := 0; i < 2; i++ {
		m := e2.G.AddObject(mountainCard(t), 1)
		m.Zone = state.ZBattlefield
		e2.G.SetZone(state.ZBattlefield, 1, append(e2.G.Zone(state.ZBattlefield, 1), m.ID))
	}
	for i := 0; i < 2; i++ {
		m := e2.G.AddObject(mountainCard(t), 0)
		m.Zone = state.ZBattlefield
		e2.G.SetZone(state.ZBattlefield, 0, append(e2.G.Zone(state.ZBattlefield, 0), m.ID))
	}
	top2 := e2.G.AddObject(mountainCard(t), 0)
	e2.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{top2.ID}, e2.G.Zone(state.ZLibrary, 0)...))
	e2.G.Step, e2.G.Active, e2.G.Priority, e2.G.Turn = state.StepMain1, 0, 0, 1
	e2.pending = nil
	e2.Advance()
	d2 := e2.Pending()
	if d2 == nil {
		t.Fatal("no pending decision for the negative arm")
	}
	for _, o := range d2.Options {
		if o.Kind == "play_land" && o.Obj == top2.ID {
			t.Fatalf("lands tied 2-2: top-of-library land offered but the SVar gate is false: %+v", o)
		}
	}
}

// TestVergeRangersUnreadableSVarGateStaysClosed pins the fail-closed direction
// of the new CheckSVar evaluation: a may-play static whose named SVar has no
// body (or an unmodelled count head) must stay withheld, so the fix cannot
// widen a gate this build cannot read.
func TestVergeRangersUnreadableSVarGateStaysClosed(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 219, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	// A synthetic static with the same shape as Verge Rangers' but a CheckSVar$
	// naming an SVar the face does not define.
	src := "Name:Gate Probe\nTypes:Creature Human\nPT:2/2\n" +
		"S:Mode$ Continuous | Affected$ Land.TopLibrary+YouCtrl | AffectedZone$ Library | MayPlay$ True | CheckSVar$ Q | SVarCompare$ GT0\n" +
		"Oracle:x\n"
	probe := e.G.AddObject(card(t, src), 0)
	probe.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), probe.ID))
	top := e.G.AddObject(mountainCard(t), 0)
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{top.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == top.ID {
			t.Fatalf("unreadable CheckSVar$ body widened the grant: %+v", o)
		}
	}
}

// TestKorlessaCastsDragonFromTopOfLibrary drives Korlessa, Scale Singer's
// real static -- S:Mode$ Continuous | Affected$ Dragon.TopLibrary+YouCtrl+
// nonLand | AffectedZone$ Library | MayPlay$ True -- end to end: a Dragon on
// top of the library is offered as a cast paying its printed mana cost, and
// resolving it moves it from the library through the stack onto the
// battlefield. A non-Dragon sitting beneath the Dragon is never offered.
func TestKorlessaCastsDragonFromTopOfLibrary(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	korlessa, ok := reg.Lookup("Korlessa, Scale Singer")
	if !ok {
		t.Fatal("Korlessa, Scale Singer missing from corpus")
	}
	if d := korlessa.Link(); len(d) != 0 {
		t.Fatalf("link Korlessa: %v", d)
	}
	e, _, _ := newFixtureDeck(t, 216, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ko := e.G.AddObject(korlessa, 0)
	ko.Zone = state.ZBattlefield
	dragon := e.G.AddObject(card(t, "Name:Fire Dragon\nManaCost:2 R\nTypes:Creature Dragon\nPT:2/2\nOracle:x\n"), 0)
	bear := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ko.ID))
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{dragon.ID, bear.ID},
		e.G.Zone(state.ZLibrary, 0)...))
	// Fund Korlessa's {G}{U} grant... no, the DRAGON's printed cost is paid;
	// {2}{R} needs R. Fund the dragon's {2}{R}.
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MG] = 2
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	var castIdx int = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == dragon.ID {
			castIdx = o.Index
			if o.Mode != "mayplay" {
				t.Fatalf("cast option mode %q, want mayplay", o.Mode)
			}
		}
		if (o.Kind == "cast" || o.Kind == "play_land") && o.Obj == bear.ID {
			t.Fatalf("non-top library card offered: %+v", o)
		}
	}
	if castIdx < 0 {
		t.Fatalf("top-of-library Dragon not offered as a cast: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{castIdx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if dragon.Zone != state.ZStack {
		t.Fatalf("dragon zone %v, want stack after the cast", dragon.Zone)
	}
	passUntilStackEmpty(t, e, 40)
	if dragon.Zone != state.ZBattlefield {
		t.Fatalf("dragon zone %v, want battlefield after resolution", dragon.Zone)
	}
}

// TestKessGraveyardInstantCastsOnItsControllerTurn drives Kess, Dissident
// Mage's real static -- Condition$ PlayerTurn -- through its POSITIVE side:
// on its controller's turn the graveyard instant is offered, casts for its
// printed cost, and MayPlayLimit$ 1 stops a second offer in the same turn.
func TestKessGraveyardInstantCastsOnItsControllerTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kess, ok := reg.Lookup("Kess, Dissident Mage")
	if !ok {
		t.Fatal("Kess, Dissident Mage missing from corpus")
	}
	if d := kess.Link(); len(d) != 0 {
		t.Fatalf("link Kess: %v", d)
	}
	e, _, _ := newFixtureDeck(t, 217, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ko := e.G.AddObject(kess, 0)
	ko.Zone = state.ZBattlefield
	bolt := e.G.AddObject(card(t, "Name:Bolt\nManaCost:U\nTypes:Instant\nOracle:x\n"), 0)
	bolt.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ko.ID))
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), bolt.ID))
	e.G.Players[0].Pool[state.MU] = 2
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want priority for seat 0", d)
	}
	var castIdx int = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt.ID {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("graveyard instant not offered on its controller's turn: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{castIdx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if bolt.Zone != state.ZStack {
		t.Fatalf("bolt zone %v, want stack after the cast", bolt.Zone)
	}
	passUntilStackEmpty(t, e, 40)
	d = e.Pending()
	if d == nil {
		t.Fatal("no pending decision after the instant resolved")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt.ID {
			t.Fatalf("MayPlayLimit$ 1 not enforced -- instant offered twice in one turn: %+v", o)
		}
	}
}

// TestKessGraveyardInstantNotOfferedOnOpponentTurn is the opponent-turn
// regression for the same Condition$ PlayerTurn gate: with the identical
// board during an OPPONENT's turn (seat 0 holding priority at instant speed,
// the card an instant so only the Condition gate can withhold it), the
// graveyard instant must NOT be offered.
func TestKessGraveyardInstantNotOfferedOnOpponentTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kess, ok := reg.Lookup("Kess, Dissident Mage")
	if !ok {
		t.Fatal("Kess, Dissident Mage missing from corpus")
	}
	e, _, _ := newFixtureDeck(t, 218, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	ko := e.G.AddObject(kess, 0)
	ko.Zone = state.ZBattlefield
	bolt := e.G.AddObject(card(t, "Name:Bolt\nManaCost:U\nTypes:Instant\nOracle:x\n"), 0)
	bolt.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), ko.ID))
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), bolt.ID))
	e.G.Players[0].Pool[state.MU] = 2
	// Seat 1's turn; seat 0 holds priority (instant speed is available to
	// seat 0 here, so only the Condition$ PlayerTurn gate can withhold the
	// grant).
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 1, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("pending = %+v, want priority for seat 0", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bolt.ID {
			t.Fatalf("graveyard instant offered during the opponent's turn: %+v", o)
		}
	}
}

// TestGravecrawlerCastsFromGraveyardWithZombieOnBattlefield drives
// Gravecrawler's real IsPresent$ gate -- S:Mode$ Continuous | Affected$
// Card.Self | AffectedZone$ Graveyard | IsPresent$ Zombie.YouCtrl -- through
// both sides: with a Zombie on the battlefield the crawler in the graveyard
// is offered and casts; with no Zombie anywhere the offer is withheld.
func TestGravecrawlerCastsFromGraveyardWithZombieOnBattlefield(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	crawler, ok := reg.Lookup("Gravecrawler")
	if !ok {
		t.Fatal("Gravecrawler missing from corpus")
	}
	if d := crawler.Link(); len(d) != 0 {
		t.Fatalf("link Gravecrawler: %v", d)
	}
	zombieSrc := "Name:Patch Zombie\nManaCost:1 B\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n"
	// With a Zombie on the battlefield.
	e, _, _ := newFixtureDeck(t, 219, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	zo := e.G.AddObject(card(t, zombieSrc), 0)
	zo.Zone = state.ZBattlefield
	gc := e.G.AddObject(crawler, 0)
	gc.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), zo.ID))
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), gc.ID))
	e.G.Players[0].Pool[state.MB] = 1
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	var castIdx int = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == gc.ID {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("Gravecrawler not offered with a Zombie on the battlefield: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{castIdx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if gc.Zone != state.ZStack {
		t.Fatalf("Gravecrawler zone %v, want stack after the cast", gc.Zone)
	}
	passUntilStackEmpty(t, e, 40)

	// Without a Zombie (the crawler itself is on the battlefield now, and it
	// IS a Zombie, so remove it to exile first) the crawler in the graveyard
	// is not offered.
	e2, _, _ := newFixtureDeck(t, 220, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	gc2 := e2.G.AddObject(crawler, 0)
	gc2.Zone = state.ZGraveyard
	e2.G.SetZone(state.ZGraveyard, 0, append(e2.G.Zone(state.ZGraveyard, 0), gc2.ID))
	e2.G.Players[0].Pool[state.MB] = 1
	e2.G.Step, e2.G.Active, e2.G.Priority, e2.G.Turn = state.StepMain1, 0, 0, 1
	e2.pending = nil
	e2.Advance()

	d = e2.Pending()
	if d == nil {
		t.Fatal("no pending decision (no-zombie arm)")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == gc2.ID {
			t.Fatalf("Gravecrawler offered with no Zombie on the battlefield: %+v", o)
		}
	}
}
