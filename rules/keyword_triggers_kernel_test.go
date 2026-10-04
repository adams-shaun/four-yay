package rules

// Kernel-era restorations of the keyword_triggers_test.go behaviour tests the W3
// legacy removal deleted: the same scenarios, driven through the resolution
// kernel (rules/resolve) instead of the suspend/resume protocol.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func TestRiotAndHideawayUseRealCorpusCards(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	spider, ok := reg.Lookup("Spider-Punk")
	if !ok {
		t.Fatal("Spider-Punk missing from corpus")
	}
	if d := spider.Link(); len(d) != 0 {
		t.Fatalf("link Spider-Punk: %v", d)
	}
	deck := make([]*cards.Card, 40)
	for i := range deck {
		deck[i] = spider
	}
	cfgSpider := seatZeroStart(Config{Seed: 187, Names: []string{"spider", "other"}, Decks: [][]*cards.Card{deck, deck}})
	e := New(cfgSpider)
	id := e.G.Objs[0].ID
	if got := e.G.Obj(id).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: Spider-Punk zone = %s, want library", got)
	}
	// Drive the real card through the entry-boundary as-enters selection.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	rd := e.Pending()
	if rd == nil || rd.Kind != decision.KChoose || len(rd.Options) != 2 || rd.Options[1].Kind != "riot" {
		t.Fatalf("Riot entry choice = %+v, want counter/haste choice", rd)
	}
	if err := e.Submit(decision.Intent{Seq: rd.Seq, Player: rd.Player, Choices: []int{rd.Options[1].Index}}); err != nil {
		t.Fatalf("submit Riot haste choice: %v", err)
	}
	if !e.HasKeyword(id, "Haste") || e.G.Obj(id).Counter("P1P1") != 0 {
		t.Fatal("Riot haste choice was not applied")
	}
	// Reanimation/blink does not create pendingCast. The general MoveZone
	// replacement must still offer Riot before the creature enters.
	eReanimated := New(cfgSpider)
	rid := eReanimated.G.Objs[0].ID
	eReanimated.emit(events.Event{Kind: events.MoveZone, Obj: rid, From: state.ZLibrary, To: state.ZGraveyard})
	eReanimated.emit(events.Event{Kind: events.MoveZone, Obj: rid, From: state.ZGraveyard, To: state.ZBattlefield})
	rd = eReanimated.Pending()
	if rd == nil || rd.Kind != decision.KChoose || len(rd.Options) != 2 {
		t.Fatalf("non-cast Riot choice = %+v, want counter/haste choice", rd)
	}
	if err := eReanimated.Submit(decision.Intent{Seq: rd.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit non-cast Riot choice: %v", err)
	}
	if o := eReanimated.G.Obj(rid); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 1 || eReanimated.HasKeyword(rid, "Haste") {
		t.Fatalf("non-cast Riot entry = %+v, want counter and no haste", o)
	}

	knoll, ok := reg.Lookup("Spinerock Knoll")
	if !ok {
		t.Fatal("Spinerock Knoll missing from corpus")
	}
	if d := knoll.Link(); len(d) != 0 {
		t.Fatalf("link Spinerock Knoll: %v", d)
	}
	for i := range deck {
		deck[i] = knoll
	}
	cfg := seatZeroStart(Config{Seed: 188, Names: []string{"knoll", "other"}, Decks: [][]*cards.Card{deck, deck}})
	e2 := New(cfg)
	kid := e2.G.Objs[0].ID
	// Kernel era: the Hideaway entry replacement's asks are tape asks, so
	// the raw entry runs as a kernel probe.
	kr6Probe(e2, func() {
		e2.emit(events.Event{Kind: events.MoveZone, Obj: kid, From: state.ZLibrary, To: state.ZBattlefield})
	})
	pick := e2.Pending()
	if pick == nil || pick.Kind != decision.KChoose || len(pick.Options) != 4 {
		t.Fatalf("Hideaway pick = %+v, want four-card choice", pick)
	}
	// Exile the second card, then put the other three on the bottom in the
	// answer's order. This drives Spinerock Knoll's real Hideaway replacement.
	exiledID := pick.Options[1].Obj
	if err := e2.Submit(decision.Intent{Seq: pick.Seq, Player: 0, Choices: []int{1}}); err != nil {
		t.Fatalf("submit Hideaway pick: %v", err)
	}
	bottom := e2.Pending()
	if bottom == nil || bottom.Kind != decision.KArrange || len(bottom.Options) != 3 {
		t.Fatalf("Hideaway bottom order = %+v, want three-card arrangement", bottom)
	}
	if err := e2.Submit(decision.Intent{Seq: bottom.Seq, Player: 0, Choices: []int{2, 0, 1}}); err != nil {
		t.Fatalf("submit Hideaway bottom order: %v", err)
	}
	exiled := e2.G.Zone(state.ZExile, 0)
	if len(exiled) != 1 || exiled[0] != exiledID {
		t.Fatalf("Hideaway exile = %v, want selected card %d", exiled, exiledID)
	}
	if e2.G.Obj(exiledID).ExiledWith != kid {
		t.Fatalf("Hideaway provenance = %d, want %d", e2.G.Obj(exiledID).ExiledWith, kid)
	}
	if !e2.G.Obj(exiledID).FaceDown {
		t.Fatal("Hideaway exile is not persisted face down")
	}
	owner := view.Project(e2.G, e2, 0, nil).Players[0].Exile
	opponent := view.Project(e2.G, e2, 1, nil).Players[0].Exile
	if len(owner) != 1 || !owner[0].FaceDown || owner[0].Name == "" {
		t.Fatalf("controller Hideaway view = %+v, want identifiable face-down card", owner)
	}
	if len(opponent) != 1 || !opponent[0].FaceDown || opponent[0].Name != "" || opponent[0].Types != "" || opponent[0].ManaCost != "" {
		t.Fatalf("opponent Hideaway view leaked its face: %+v", opponent)
	}
	lib := e2.G.Zone(state.ZLibrary, 0)
	wantBottom := []state.ObjID{bottom.Options[2].Obj, bottom.Options[0].Obj, bottom.Options[1].Obj}
	for i, id := range wantBottom {
		if lib[len(lib)-len(wantBottom)+i] != id {
			t.Fatalf("Hideaway bottom[%d] = %d, want %d", i, lib[len(lib)-len(wantBottom)+i], id)
		}
	}
}

// TestPlayUsesRealCorpusCard drives Spinerock Knoll's real script: its
// activated Play ability plays the card exiled by its own Hideaway (Defined$
// ExiledWith) without paying its mana cost, once an opponent has been dealt 7+
// this turn. We let Hideaway exile the top cards with provenance, swap one for
// a Bear, deal 7 to the opponent, activate the Play ability, and verify the
// Bear is played onto the battlefield from exile without a mana cost.
func TestPlayUsesRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	knoll, ok := reg.Lookup("Spinerock Knoll")
	if !ok {
		t.Fatal("Spinerock Knoll missing from corpus")
	}
	if d := knoll.Link(); len(d) != 0 {
		t.Fatalf("link Spinerock Knoll: %v", d)
	}
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	deck := []*cards.Card{knoll}
	cfg := seatZeroStart(Config{Seed: 192, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, deck...), mountainDeck(t, 39)...),
			mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	// Put Spinerock Knoll on seat 0's battlefield; its Hideaway exiles the top
	// 4 library cards with provenance ExiledWith == the Knoll.
	var kid state.ObjID
	for _, id := range append(e.G.Zone(state.ZLibrary, 0), e.G.Zone(state.ZHand, 0)...) {
		if e.G.Obj(id).Face() != nil && e.G.Obj(id).Face().Name == "Spinerock Knoll" {
			kid = id
		}
	}
	from := e.G.Obj(kid).Zone
	kr6Probe(e, func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: kid, From: from, To: state.ZBattlefield})
	})
	pick := e.Pending()
	if pick == nil || pick.Kind != decision.KChoose || len(pick.Options) != 4 {
		t.Fatalf("Hideaway pick = %+v, want four-card choice", pick)
	}
	if err := e.Submit(decision.Intent{Seq: pick.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit Hideaway pick: %v", err)
	}
	bottom := e.Pending()
	if bottom == nil || bottom.Kind != decision.KArrange || len(bottom.Options) != 3 {
		t.Fatalf("Hideaway bottom order = %+v, want three-card arrangement", bottom)
	}
	if err := e.Submit(decision.Intent{Seq: bottom.Seq, Player: 0, Choices: []int{0, 1, 2}}); err != nil {
		t.Fatalf("submit Hideaway bottom order: %v", err)
	}
	if len(e.G.Zone(state.ZExile, 0)) != 1 {
		t.Fatalf("Hideaway should exile one card, got %d", len(e.G.Zone(state.ZExile, 0)))
	}
	// Swap the exiled card for a Bear with the Knoll's provenance so the Play
	// ability has a card to play.
	er := e.G.Zone(state.ZExile, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: er, From: state.ZExile, To: state.ZGraveyard})
	bo := e.G.AddObject(bear, 0)
	bo.Zone = state.ZExile
	bo.ExiledWith = kid
	e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), bo.ID))
	// Deal 7 to the opponent so X (MaxOppDamageThisTurn) >= 7, then fund {R}
	// and untap the Knoll (a hideaway land enters tapped), in a sorcery window.
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 7})
	e.G.Obj(kid).Tapped = false
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	var ai int = -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == kid {
			ai = o.Index
		}
	}
	if ai < 0 {
		t.Fatalf("no Spinerock ability option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ai}}); err != nil {
		t.Fatalf("submit ability: %v", err)
	}
	// The Play effect poses a KModes choose; answer it (option 0 plays the
	// Bear from exile), then let the cast resolve.
	d = e.Pending()
	for d != nil && d.Kind != decision.KModes {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected a KModes for the Play choice, got %+v", d)
	}
	var pi int = -1
	for _, o := range d.Options {
		if o.Obj == bo.ID {
			pi = o.Index
		}
	}
	if pi < 0 {
		t.Fatalf("exiled Bear not offered by the Play choice: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pi}}); err != nil {
		t.Fatalf("submit play: %v", err)
	}
	passUntilStackEmpty(t, e, 60)
	// The Bear should now be on seat 0's battlefield, played from exile.
	var bearOnBF bool
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).ID == bo.ID {
			bearOnBF = true
		}
	}
	if !bearOnBF {
		t.Fatalf("Bear not played onto the battlefield from exile: zone=%v", e.G.Obj(bo.ID).Zone)
	}
}

// TestDredgeUsesRealCorpusCard drives Golgari Thug's real script: a card
// with Dredge 4 in the graveyard replaces a draw -- the controller may instead
// mill 4 and return it to hand. We put a Golgari Thug in seat 0's graveyard,
// trigger a draw, answer the dredge ask "yes", and verify the 4 cards were
// milled and the Thug returned to hand (and no card was drawn).
func TestDredgeUsesRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	thug, ok := reg.Lookup("Golgari Thug")
	if !ok {
		t.Fatal("Golgari Thug missing from corpus")
	}
	if d := thug.Link(); len(d) != 0 {
		t.Fatalf("link Golgari Thug: %v", d)
	}
	deck := []*cards.Card{thug}
	cfg := seatZeroStart(Config{Seed: 193, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, deck...), mountainDeck(t, 39)...),
			mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	// Move the Thug to seat 0's graveyard (it starts in hand).
	var tid state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face() != nil && e.G.Obj(id).Face().Name == "Golgari Thug" {
			tid = id
		}
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: state.ZHand, To: state.ZGraveyard})
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	handBefore := len(e.G.Zone(state.ZHand, 0))
	// Draw for seat 0 directly through the shared path.
	kr6Probe(e, func() { e.drawCard(0) })
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected dredge KModes ask, got %+v", d)
	}
	// Choose "dredge" (option 0).
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit dredge: %v", err)
	}
	if len(e.G.Zone(state.ZLibrary, 0)) != libBefore-4 {
		t.Fatalf("library after dredge = %d, want %d (milled 4)", len(e.G.Zone(state.ZLibrary, 0)), libBefore-4)
	}
	// The Thug leaves the graveyard for hand, so after milling the graveyard
	// holds exactly the 4 milled cards.
	if len(e.G.Zone(state.ZGraveyard, 0)) != 4 {
		t.Fatalf("graveyard after dredge = %d, want 4 (the 4 milled)", len(e.G.Zone(state.ZGraveyard, 0)))
	}
	if len(e.G.Zone(state.ZHand, 0)) != handBefore+1 {
		t.Fatalf("hand after dredge = %d, want %d (Thug returned)", len(e.G.Zone(state.ZHand, 0)), handBefore+1)
	}
	// The Thug is back in hand.
	inHand := false
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if id == tid {
			inHand = true
		}
	}
	if !inHand {
		t.Fatal("Golgari Thug not returned to hand after dredge")
	}
}

// TestDredgeResumesEveryDrawAndContinuation proves an actual Golgari Thug
// replacement cannot abandon a surrounding Draw 2 or its SubAbility$. Both
// choices are exercised: dredging the first draw, and declining both offered
// replacements to draw two cards normally.
func TestDredgeResumesEveryDrawAndContinuation(t *testing.T) {
	t.Parallel()
	thug, ok := testutil.CorpusRegistry(t).Lookup("Golgari Thug")
	if !ok {
		t.Fatal("Golgari Thug missing from corpus")
	}
	drawTwo := card(t, "Name:Draw Two\nManaCost:U\nTypes:Sorcery\nA:SP$ Draw | Defined$ You | NumCards$ 2 | SubAbility$ After\nSVar:After:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n")
	for _, tc := range []struct {
		name        string
		firstDredge bool
		wantDraws   int
	}{
		{name: "dredge first", firstDredge: true, wantDraws: 1},
		{name: "decline", firstDredge: false, wantDraws: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deck := append([]*cards.Card{thug, drawTwo}, mountainDeck(t, 38)...)
			e := New(seatZeroStart(Config{Seed: 241, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}}))
			e.Advance()
			var tid, did state.ObjID
			for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
				for _, id := range e.G.Zone(z, 0) {
					switch e.G.Obj(id).Face().Name {
					case "Golgari Thug":
						tid = id
					case "Draw Two":
						did = id
					}
				}
			}
			if tid == 0 || did == 0 {
				t.Fatalf("fixture ids thug=%d draw=%d", tid, did)
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: e.G.Obj(tid).Zone, To: state.ZGraveyard})
			start := len(e.L.Events)
			life := e.G.Players[0].Life
			e.emit(events.Event{Kind: events.PutOnStack, Obj: did, From: e.G.Obj(did).Zone, To: state.ZStack, Player: 0})
			kr6ResolveTop(e)
			first := e.Pending()
			if first == nil || first.ResumeKind != "dredge" {
				t.Fatalf("first Draw 2 replacement = %+v", first)
			}
			choice := len(first.Options) - 1 // ordinary draw
			if tc.firstDredge {
				choice = 0
			}
			if err := e.Submit(decision.Intent{Seq: first.Seq, Player: 0, Choices: []int{choice}}); err != nil {
				t.Fatal(err)
			}
			for d := e.Pending(); d != nil && d.ResumeKind == "dredge"; d = e.Pending() {
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{len(d.Options) - 1}}); err != nil {
					t.Fatal(err)
				}
			}
			draws := 0
			for _, ev := range e.L.Events[start:] {
				if ev.Kind == events.Draw && ev.Player == 0 {
					draws++
				}
			}
			if draws != tc.wantDraws || e.G.Players[0].Life != life+1 || e.G.Obj(did).Zone != state.ZGraveyard {
				t.Fatalf("draws=%d life=%d spell zone=%v, want draws=%d life=%d resolved graveyard", draws, e.G.Players[0].Life, e.G.Obj(did).Zone, tc.wantDraws, life+1)
			}
		})
	}
}
