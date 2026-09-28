package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// Task 16 keyword triggers: Undying, Evolve, Exalted, Prowess. Each keyword
// is expanded by cards/keywords.go into an ordinary ChangesZone / Attacks /
// SpellCast trigger routed through trigger_match.go's own modes (see
// keyword_registration_test.go's pin and the acceptance ratchet), so these
// tests drive the same engine paths a real card of each keyword exercises.
//
// The tests emit setup events directly (e.emit) and then call e.priorityRound
// -- the engine's "CR 117.5: handle state-based actions and triggered
// abilities before granting priority" entry point -- to place any queued
// trigger on the stack ahead of passUntilStackEmpty draining it. This deviates
// from the brief's bare e.Advance() calls, which are a no-op here: emitting an
// event directly leaves whatever decision was pending (genesis's first
// priority in newFixtureDeck) untouched, so e.Advance() returns immediately
// without ever running putTriggersOnStack. e.priorityRound() is the same
// refresh existing helpers (addMana, putCreature) use for this exact reason.
// See the task report.

func TestUndyingReturnsOnceWithACounter(t *testing.T) {
	t.Parallel()
	geist := "Name:Geist\nManaCost:G G\nTypes:Creature Spirit\nPT:2/1\nK:Haste\nK:Undying\nOracle:x\n"
	e, cfg, g := newFixtureDeck(t, 81, geist)
	e.emit(events.Event{Kind: events.MoveZone, Obj: g, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Damage, Obj: g, Amount: 3})
	e.checkStateBased()
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(g); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 1 || e.Power(g) != 3 {
		t.Fatalf("after first death: %s, counters %d", o.Zone, o.Counter("P1P1"))
	}
	e.emit(events.Event{Kind: events.Damage, Obj: g, Amount: 5})
	e.checkStateBased()
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(g).Zone != state.ZGraveyard {
		t.Fatal("undying returned a creature that had a +1/+1 counter")
	}
	replayCheck(t, e, cfg)
}

func TestEvolveGrowsOnlyForBiggerCreatures(t *testing.T) {
	small := "Name:Small\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n"
	big := "Name:Big\nManaCost:2\nTypes:Creature\nPT:2/2\nOracle:x\n"
	theirs := "Name:Theirs\nManaCost:5\nTypes:Creature\nPT:5/5\nOracle:x\n"
	e, cfg, one := newFixtureDeck(t, 82,
		"Name:One\nManaCost:G\nTypes:Creature Human Ooze\nPT:1/1\nK:Evolve\nOracle:x\n",
		small, big)
	e.emit(events.Event{Kind: events.MoveZone, Obj: one, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	putCreature(t, e, 0, small) // equal size: must not evolve
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(one).Counter("P1P1") != 0 {
		t.Fatal("evolved for an equal-size creature")
	}
	putCreature(t, e, 0, big) // strictly bigger: must evolve
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(one).Counter("P1P1") != 1 {
		t.Fatal("did not evolve for a bigger creature")
	}
	// An opponent's creature (seat 1) entering must not evolve seat 0's One:
	// Evolve's expansion is ValidCard$ Creature.YouCtrl+Other, so a seat on
	// the other side of the table is simply not YouCtrl. The card is minted
	// onto seat 1's battlefield via TokenCreate (newFixtureDeck's extras only
	// ever seed seat 0, so there is no real seat-1 card to move).
	e.priorityRound()
	putToken(t, e, 1, theirs, state.ZBattlefield)
	e.Advance()
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(one).Counter("P1P1") != 1 {
		t.Fatal("evolved for an opponent's creature")
	}
	replayCheck(t, e, cfg)
}

// TestDethroneCountsOnlyTheAttackedPlayersLife drives Treasonous Ogre's
// actual compiled script. Dethrone uses the defender carried by the attack
// event: a different player having more life must not make an attack at a
// lower-life opponent eligible.
func TestDethroneCountsOnlyTheAttackedPlayersLife(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ogre, ok := reg.Lookup("Treasonous Ogre")
	if !ok {
		t.Fatal("Treasonous Ogre missing from corpus")
	}
	if d := ogre.Link(); len(d) != 0 {
		t.Fatalf("link Treasonous Ogre: %v", d)
	}
	deck := make([]*cards.Card, 40)
	for i := range deck {
		deck[i] = ogre
	}
	cfg := seatZeroStart(Config{Seed: 85, Names: []string{"ogre", "other", "third"}, Decks: [][]*cards.Card{deck, deck, deck}})
	e := New(cfg)
	var id state.ObjID
	for _, candidate := range e.G.Objs {
		if candidate.Owner == 0 && candidate.Face() != nil && candidate.Face().Name == "Treasonous Ogre" {
			id = candidate.ID
			break
		}
	}
	if id == 0 {
		t.Fatal("Treasonous Ogre was not created")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, To: state.ZBattlefield})
	// Seat 1 is tied for the most life, so attacking it triggers Dethrone.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("Dethrone at tied-most-life defender gave %d counters, want 1", got)
	}
	// A third player above the defender prevents Dethrone. The condition is
	// greatest life among every player, not merely >= the attacker's life.
	e.emit(events.Event{Kind: events.LifeChange, Player: 2, Amount: 1})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("Dethrone fired while an uninvolved player had more life: counters %d", got)
	}
	replayCheck(t, e, cfg)
}

// TestGrantedDethroneTriggers uses Marchesa's real static script. A granted
// keyword must create its rules trigger too; checking HasKeyword alone would
// make the creature visibly have Dethrone while its attacks did nothing.
func TestGrantedDethroneTriggers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	marchesa, ok := reg.Lookup("Marchesa, the Black Rose")
	if !ok {
		t.Fatal("Marchesa missing from corpus")
	}
	if d := marchesa.Link(); len(d) != 0 {
		t.Fatalf("link Marchesa: %v", d)
	}
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	deck := make([]*cards.Card, 40)
	deck[0] = marchesa
	for i := 1; i < len(deck); i++ {
		deck[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: 186, Names: []string{"marchesa", "other"}, Decks: [][]*cards.Card{deck, deck}})
	e := New(cfg)
	var m, b state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || o.Face() == nil {
			continue
		}
		switch o.Face().Name {
		case "Marchesa, the Black Rose":
			m = o.ID
		case "Bear":
			b = o.ID
		}
	}
	if m == 0 || b == 0 {
		t.Fatalf("fixture ids Marchesa=%d Bear=%d", m, b)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: m, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.MoveZone, Obj: b, To: state.ZBattlefield})
	if !e.HasKeyword(b, "Dethrone") {
		t.Fatal("Marchesa did not grant Dethrone")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{b}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(b).Counter("P1P1"); got != 1 {
		t.Fatalf("granted Dethrone counters=%d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

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
	e2.emit(events.Event{Kind: events.MoveZone, Obj: kid, From: state.ZLibrary, To: state.ZBattlefield})
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

// TestExtortUsesRealCorpusCard drives Crypt Ghast's real compiled script:
// casting a spell with an Extort permanent on the battlefield fires a
// SpellCast trigger whose body poses the optional {W/B} payment, and on pay
// each opponent loses 1 life and the controller gains that much.
func TestExtortUsesRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ghast, ok := reg.Lookup("Crypt Ghast")
	if !ok {
		t.Fatal("Crypt Ghast missing from corpus")
	}
	if d := ghast.Link(); len(d) != 0 {
		t.Fatalf("link Crypt Ghast: %v", d)
	}
	bolt := card(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	deck := []*cards.Card{ghast}
	game := func() (*Engine, Config, state.ObjID) {
		cfg := seatZeroStart(Config{Seed: 190, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append(append([]*cards.Card{}, deck...), mountainDeck(t, 39)...),
				mountainDeck(t, 40)},
			Tokens: map[string]*cards.Card{}})
		e := New(cfg)
		e.Advance()
		// Put Crypt Ghast on seat 0's battlefield, and Bolt in hand.
		var g state.ObjID
		for _, id := range append(e.G.Zone(state.ZLibrary, 0), e.G.Zone(state.ZHand, 0)...) {
			if e.G.Obj(id).Face() != nil && e.G.Obj(id).Face().Name == "Crypt Ghast" {
				g = id
			}
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: g, From: e.G.Obj(g).Zone, To: state.ZBattlefield})
		bo := e.G.AddObject(bolt, 0)
		bo.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), bo.ID))
		e.G.Players[0].Pool[state.MR] = 2
		e.G.Players[0].Pool[state.MW] = 1
		e.pending = nil
		e.Advance()
		return e, cfg, g
	}

	// Cast Bolt, answer the target (seat 1), then the cast resolves. The
	// Extort trigger fires on the SpellCast; the caster must be able to say
	// "pay" and see the drain. We fund a W so the {W/B} pip is payable.
	e, _, g := game()
	if !e.HasKeyword(g, "Extort") {
		t.Fatal("Crypt Ghast does not grant Extort on the battlefield")
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	// Choose the cast option for Bolt.
	var ci int = -1
	for _, o := range d.Options {
		if o.Kind == "cast" {
			ci = o.Index
		}
	}
	if ci < 0 {
		t.Fatalf("no cast option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ci}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Bolt, got %+v", d)
	}
	var ti int = -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			ti = o.Index
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{ti}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	// The cast is paid and pushed. The Extort trigger should now be posed as
	// the next decision (an optional KModes ask), when the trigger drain runs.
	// First allow the trigger drain to reach the ask by passing any priority
	// that arrives before it. The trigger is queued by the PutOnStack that
	// payCast emits; the drain poses it as a KModes.
	d = e.Pending()
	for d != nil && d.Kind == decision.KPriority {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
		d = e.Pending()
	}
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected Extort KModes ask, got %+v", d)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	// Choose "pay" (option index 0), then let the trigger resolution (and the
	// Bolt spell) finish draining the stack so the drain's LifeChange lands.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("submit extort pay: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	// Bolt deals 1 to the opponent, and Extort drains 1 (opponent) and gains
	// it (caster), so after both resolutions the opponent is life-2 and the
	// caster is life+1.
	if e.G.Players[1].Life != life1-2 {
		t.Fatalf("opponent life %d after extort, want %d", e.G.Players[1].Life, life1-2)
	}
	if e.G.Players[0].Life != life0+1 {
		t.Fatalf("caster life %d after extort, want %d", e.G.Players[0].Life, life0+1)
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
	e.emit(events.Event{Kind: events.MoveZone, Obj: kid, From: e.G.Obj(kid).Zone, To: state.ZBattlefield})
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

// TestConduitOfWorldsPlayPaysMana drives Conduit of Worlds' real Play SA.
// Unlike Spinerock Knoll, Conduit has no WithoutManaCost$ parameter, so its
// selected graveyard card must not be cast for free.
func TestConduitOfWorldsPlayPaysMana(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	conduit, ok := reg.Lookup("Conduit of Worlds")
	if !ok {
		t.Fatal("Conduit of Worlds missing from corpus")
	}
	if d := conduit.Link(); len(d) != 0 {
		t.Fatalf("link Conduit of Worlds: %v", d)
	}
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e, _, _ := newFixtureDeck(t, 197, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	co := e.G.AddObject(conduit, 0)
	co.Zone = state.ZBattlefield
	bo := e.G.AddObject(bear, 0)
	bo.Zone = state.ZGraveyard
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), co.ID))
	e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), bo.ID))
	e.G.Step, e.G.Active, e.G.Priority, e.G.Turn = state.StepMain1, 0, 0, 1
	e.pending = nil
	e.Advance()

	d := e.Pending()
	ability := -1
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == co.ID {
			ability = o.Index
		}
	}
	if ability < 0 {
		t.Fatalf("Conduit Play ability not offered (params=%v targetable=%v): %+v", co.Face().Abilities[0].Params, e.abilityTargetsAvailable(0, co.ID, co.Face().Abilities[0]), d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{ability}}); err != nil {
		t.Fatalf("activate Conduit: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Conduit target = %+v, want graveyard target", d)
	}
	target := -1
	for _, o := range d.Options {
		if o.Obj == bo.ID {
			target = o.Index
		}
	}
	if target < 0 {
		t.Fatalf("Conduit did not offer Bear: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{target}}); err != nil {
		t.Fatalf("target Bear: %v", err)
	}
	for d = e.Pending(); d != nil && d.Kind != decision.KModes; d = e.Pending() {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("Conduit Play choice = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("choose Bear for Play: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if bo.Zone != state.ZGraveyard {
		t.Fatalf("Conduit cast Bear without its {1}{G}: zone=%v", bo.Zone)
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
	e.drawCard(0)
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

// TestDredgeCannotReplaceDrawWithInsufficientLibrary proves a Dredge
// replacement cannot offer a choice the library cannot pay for: with three
// cards left, Dredge 4 is not offered and the draw stays ordinary.
func TestDredgeCannotReplaceDrawWithInsufficientLibrary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	thug, ok := reg.Lookup("Golgari Thug")
	if !ok {
		t.Fatal("Golgari Thug missing from corpus")
	}
	cfg := seatZeroStart(Config{Seed: 194, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append([]*cards.Card{thug}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	var tid state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o.Face() != nil && o.Face().Name == "Golgari Thug" {
			tid = id
		}
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: tid, From: state.ZHand, To: state.ZGraveyard})
	for len(e.G.Zone(state.ZLibrary, 0)) > 3 {
		id := e.G.Zone(state.ZLibrary, 0)[0]
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
	}
	e.drawCard(0)
	if d := e.Pending(); d != nil && d.Kind == decision.KModes && d.ResumeKind == "dredge" {
		t.Fatalf("Dredge 4 with three library cards offered illegal choice: %+v", d)
	}
	if e.G.Obj(tid).Zone != state.ZGraveyard {
		t.Fatalf("Golgari Thug zone = %v, want graveyard after ordinary draw", e.G.Obj(tid).Zone)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != 2 {
		t.Fatalf("library after ordinary draw = %d, want 2", got)
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
			e.resolveTop()
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

func TestSoulbondUsesRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	lookout, ok := reg.Lookup("Tandem Lookout")
	if !ok {
		t.Fatal("Tandem Lookout missing from corpus")
	}
	if d := lookout.Link(); len(d) != 0 {
		t.Fatalf("link Tandem Lookout: %v", d)
	}
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	deck := []*cards.Card{lookout}
	cfg := seatZeroStart(Config{Seed: 195, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(append([]*cards.Card{}, deck...), mountainDeck(t, 39)...),
			mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var wid state.ObjID
	for _, id := range append(e.G.Zone(state.ZLibrary, 0), e.G.Zone(state.ZHand, 0)...) {
		if e.G.Obj(id).Face() != nil && e.G.Obj(id).Face().Name == "Tandem Lookout" {
			wid = id
		}
	}
	// A Bear on the battlefield for it to pair with (the Soulbond entry
	// trigger fires while Tandem Lookout resolves its MoveZone, so the Bear
	// must already be present as the Pair candidate).
	bo := e.G.AddObject(bear, 0)
	bo.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), bo.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: wid, From: e.G.Obj(wid).Zone, To: state.ZBattlefield})
	// The Soulbond entry trigger (ChangesZone to battlefield) fires Pair; drain
	// the stack so the pairing (applied by the resolving trigger) is in place
	// before we check it.
	e.priorityRound()
	d := passToDecision(t, e, 8)
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 {
		t.Fatalf("Soulbond choice = %+v, want optional partner choice", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit Soulbond partner: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(wid).Paired == 0 {
		t.Fatalf("Tandem Lookout not paired on entry: Paired=%d", e.G.Obj(wid).Paired)
	}
	if e.G.Obj(wid).Paired != bo.ID || e.G.Obj(bo.ID).Paired != wid {
		t.Fatalf("pairing not reciprocal: lookout.Paired=%d bear.Paired=%d", e.G.Obj(wid).Paired, e.G.Obj(bo.ID).Paired)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bo.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(wid).Paired != 0 {
		t.Fatalf("Soulbond partner remained paired after the other creature left: %d", e.G.Obj(wid).Paired)
	}
}

// TestSoulbondTriggersWhenAnotherCreatureEnters covers Soulbond's second CR
// 702.103 trigger case and proves a noncreature cannot be offered as partner.
func TestSoulbondTriggersWhenAnotherCreatureEnters(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lookout, ok := reg.Lookup("Tandem Lookout")
	if !ok {
		t.Fatal("Tandem Lookout missing from corpus")
	}
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	rock := card(t, "Name:Rock\nTypes:Artifact\nOracle:x\n")
	e, _, _ := newFixtureDeck(t, 196, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	lo := e.G.AddObject(lookout, 0)
	lo.Zone = state.ZBattlefield
	rockObj := e.G.AddObject(rock, 0)
	rockObj.Zone = state.ZBattlefield
	bearObj := e.G.AddObject(bear, 0)
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{lo.ID, rockObj.ID})
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{bearObj.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearObj.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.priorityRound()
	d := passToDecision(t, e, 8)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Obj != bearObj.ID {
		t.Fatalf("Soulbond second-entry choices = %+v, want only entering creature", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit second-entry Soulbond: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if lo.Paired != bearObj.ID || bearObj.Paired != lo.ID || rockObj.Paired != 0 {
		t.Fatalf("second-entry pairing = lookout:%d bear:%d rock:%d", lo.Paired, bearObj.Paired, rockObj.Paired)
	}
}

// TestSoulbondOtherEntryOffersOnlyItsOwnTriggeringCreature is the three-
// creature regression: TWO already-unpaired creatures sit on the battlefield
// alongside Tandem Lookout (also unpaired) when a THIRD creature enters.
// CR 702.103a's second trigger case ("another unpaired creature enters")
// pairs Lookout with THAT entrant specifically -- Ctx.Remembered, not a
// battlefield-wide scan -- so neither bystander unpaired creature may ever be
// offered, even though effPair's soulbondPartner predicate alone would admit
// them (unpaired, controlled, a creature). This is what the two-candidate
// TestSoulbondTriggersWhenAnotherCreatureEnters case above cannot catch: with
// only one eligible creature on the board, a broad scan and a Remembered-
// restricted scan produce the same single-option offer either way.
func TestSoulbondOtherEntryOffersOnlyItsOwnTriggeringCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lookout, ok := reg.Lookup("Tandem Lookout")
	if !ok {
		t.Fatal("Tandem Lookout missing from corpus")
	}
	bearSrc := func(n string) *cards.Card {
		return card(t, "Name:"+n+"\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	}
	e, _, _ := newFixtureDeck(t, 196, "Name:Blank\nTypes:Sorcery\nOracle:x\n")
	lo := e.G.AddObject(lookout, 0)
	lo.Zone = state.ZBattlefield
	bystander1 := e.G.AddObject(bearSrc("Bystander One"), 0)
	bystander1.Zone = state.ZBattlefield
	bystander2 := e.G.AddObject(bearSrc("Bystander Two"), 0)
	bystander2.Zone = state.ZBattlefield
	entrant := e.G.AddObject(bearSrc("Entrant"), 0)
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{lo.ID, bystander1.ID, bystander2.ID})
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{entrant.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.emit(events.Event{Kind: events.MoveZone, Obj: entrant.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.priorityRound()
	d := passToDecision(t, e, 8)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Obj != entrant.ID {
		t.Fatalf("Soulbond second-entry choices = %+v, want exactly the entering creature (neither bystander)", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("submit second-entry Soulbond: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	if lo.Paired != entrant.ID || entrant.Paired != lo.ID {
		t.Fatalf("pairing = lookout:%d entrant:%d, want reciprocal", lo.Paired, entrant.Paired)
	}
	if bystander1.Paired != 0 || bystander2.Paired != 0 {
		t.Fatalf("a bystander unpaired creature was paired instead: b1=%d b2=%d", bystander1.Paired, bystander2.Paired)
	}
}

// TestMyriadUsesRealCorpusCard drives Chittering Dispatcher's real script:
// a K:Myriad creature, when it attacks, creates a tapped attacking token
// copy for each opponent other than the defending player.
// passToDecision advances priority until an effect ask interrupts stack
// resolution. It is intentionally limited so a missing trigger fails instead
// of allowing a turn to run indefinitely.
func passToDecision(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			return d
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	return e.Pending()
}

// viewShowsObject reports whether any projected card view carries id.
func viewShowsObject(cards []view.CardView, id state.ObjID) bool {
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestMyriadUsesRealCorpusCard(t *testing.T) {
	e, cfg, _ := myriadCombat(t, 3)
	// Dispatcher attacks seat 1; in a 3-seat game there are TWO other
	// opponents (seat 1 defender, seat 2 the extra Myriad target). CR 702.109
	// makes the remaining token optional, so explicitly create it here.
	d := passToDecision(t, e, 8)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "myriad" || d.Player != 0 {
		t.Fatalf("Myriad choice = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("create Myriad copy: %v", err)
	}
	passUntilStackEmpty(t, e, 20)
	// Expect one MyriadCopy token attacking seat 2 created (the defender is
	// seat 1, excluded).
	tokens := 0
	var tokenID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).IsToken && e.G.Obj(id).IsCopy {
			tokens++
			tokenID = id
		}
	}
	if tokens != 1 {
		t.Fatalf("Myriad created %d attacker tokens, want 1", tokens)
	}
	// The mint is a real battlefield permanent, not an ephemeral copy:
	// state.Object.Ephemeral must not hide it (it is IsCopy+ZBattlefield),
	// and the projection must show it on the attacking seat's battlefield.
	if e.G.Obj(tokenID).Ephemeral() {
		t.Fatalf("Myriad token %d reports Ephemeral(); a battlefield copy is a real permanent", tokenID)
	}
	if v := view.Project(e.G, nil, 0, nil); !viewShowsObject(v.Players[0].Battlefield, tokenID) {
		t.Fatalf("Myriad token %d is missing from the attacking seat's battlefield view", tokenID)
	}
	// CR 702.109a exiles Myriad tokens as the end-of-combat step ends.
	// passUntilStackEmpty leaves the end-of-combat priority ask outstanding;
	// the merged engine defers finishStepBoundary while a decision is pending
	// (main's BeginPhase-replacement guard), so clear it the same way main's
	// own TestLeavingEndCombatRemovesAttackerBeforePostcombatMain does before
	// driving the transition by hand.
	e.pending = nil
	e.setStep(state.StepEndCombat)
	e.advanceStep()
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).IsMyriad {
			t.Fatal("Myriad token remained on battlefield after combat")
		}
	}
	exiled := false
	for _, o := range e.G.Objs {
		if o.IsToken && o.IsCopy && o.Zone == state.ZExile {
			exiled = true
		}
	}
	if !exiled {
		t.Fatal("Myriad token was not exiled at end of combat")
	}
	replayCheck(t, e, cfg)
}

// TestMyriadTokenEntryFiresOtherCreatureETBTriggers proves the Myriad copy's
// battlefield entry is a genuine ChangesZone-matchable event (Mode$
// ChangesZone requires ev.Kind == events.MoveZone), not merely a synthetic
// MyriadCopy event no trigger can see. A watcher permanent with an ordinary
// "whenever another creature enters" trigger sits on the controller's
// battlefield before Chittering Dispatcher's Myriad token is created; the
// watcher's life-gain must fire off the token's own entry exactly as it
// would for a cast or reanimated creature.
func TestMyriadTokenEntryFiresOtherCreatureETBTriggers(t *testing.T) {
	e, cfg, _ := myriadCombat(t, 3)
	watcher := card(t, "Name:Watcher\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature.Other | Execute$ Trig | TriggerDescription$ watch\n"+
		"SVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n")
	wo := e.G.AddObject(watcher, 0)
	wo.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), wo.ID))
	life := e.G.Players[0].Life

	d := passToDecision(t, e, 8)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "myriad" || d.Player != 0 {
		t.Fatalf("Myriad choice = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{0}}); err != nil {
		t.Fatalf("create Myriad copy: %v", err)
	}
	// The token's own MoveZone queues the watcher's trigger but does not
	// itself place it on the stack; priorityRound is CR 117.5's "put queued
	// triggers on the stack before priority" step.
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != life+1 {
		t.Fatalf("watcher life after Myriad token entry = %d, want %d (its ChangesZone trigger must fire on the token's real MoveZone entry)",
			e.G.Players[0].Life, life+1)
	}
	_ = cfg // the watcher is added out-of-band (not through the event log), so this test does not replayCheck.
}

// myriadCombat uses Chittering Dispatcher's actual corpus keyword expansion
// and leaves its Myriad trigger ready to resolve. Seat 1 is the defender;
// each additional opponent is an independent CR 702.109 may choice.
func myriadCombat(t *testing.T, seats int) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	disperser, ok := reg.Lookup("Chittering Dispatcher")
	if !ok {
		t.Fatal("Chittering Dispatcher missing from corpus")
	}
	if d := disperser.Link(); len(d) != 0 {
		t.Fatalf("link Chittering Dispatcher: %v", d)
	}
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := range names {
		names[i] = string(rune('a' + i))
		decks[i] = mountainDeck(t, 40)
	}
	decks[0] = append([]*cards.Card{disperser}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 196, Names: names, Decks: decks, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var did state.ObjID
	for _, id := range append(e.G.Zone(state.ZLibrary, 0), e.G.Zone(state.ZHand, 0)...) {
		if e.G.Obj(id).Face() != nil && e.G.Obj(id).Face().Name == "Chittering Dispatcher" {
			did = id
		}
	}
	if did == 0 {
		t.Fatal("Chittering Dispatcher was not dealt")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: did, From: e.G.Obj(did).Zone, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{did}})
	e.priorityRound()
	return e, cfg, did
}

// TestMyriadMayDeclineEveryOpponent proves the CR 702.109 may is not a
// mandatory token creation: in a four-player combat the controller may
// decline each non-defending opponent independently.
func TestMyriadMayDeclineEveryOpponent(t *testing.T) {
	e, cfg, _ := myriadCombat(t, 4)
	for want := 2; want <= 3; want++ {
		d := passToDecision(t, e, 8)
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "myriad" || len(d.Options) != 2 {
			t.Fatalf("Myriad choice for seat %d = %+v", want, d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{1}}); err != nil {
			t.Fatalf("decline Myriad copy for seat %d: %v", want, err)
		}
	}
	passUntilStackEmpty(t, e, 20)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).IsMyriad {
			t.Fatal("declined Myriad created a token")
		}
	}
	replayCheck(t, e, cfg)
}

// TestMyriadMayChooseEachOpponentIndependently proves a mixed answer: creating
// a copy for seat 2 does not force one for seat 3.
func TestMyriadMayChooseEachOpponentIndependently(t *testing.T) {
	e, cfg, _ := myriadCombat(t, 4)
	for want, choice := range []int{0, 1} { // create for seat 2, decline seat 3
		d := passToDecision(t, e, 8)
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "myriad" {
			t.Fatalf("Myriad choice %d = %+v", want, d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{choice}}); err != nil {
			t.Fatalf("answer Myriad choice %d: %v", want, err)
		}
	}
	passUntilStackEmpty(t, e, 20)
	copies := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if !o.IsMyriad {
			continue
		}
		copies++
		if o.Attacking != 2 {
			t.Fatalf("Myriad copy attacks seat %d, want seat 2", o.Attacking)
		}
	}
	if copies != 1 {
		t.Fatalf("mixed Myriad choice created %d copies, want 1", copies)
	}
	replayCheck(t, e, cfg)
}

func TestExaltedPumpsALoneAttackerAndProwessPumpsOnNoncreatureSpells(t *testing.T) {
	t.Parallel()
	knight := "Name:Knight\nManaCost:1 B\nTypes:Creature Human Knight\nPT:2/1\nK:Exalted\nOracle:x\n"
	other := "Name:Other\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n"
	e, cfg, k := newFixtureDeck(t, 83, knight, other)
	e.emit(events.Event{Kind: events.MoveZone, Obj: k, From: state.ZHand, To: state.ZBattlefield})
	e.priorityRound()
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{k}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if e.Power(k) != 3 {
		t.Fatalf("lone attacker power %d", e.Power(k))
	}
	// End combat so the first pump expires (until-end-of-turn) and k returns
	// to 2/1, so the two-attacker assertion below measures a fresh attack.
	e.cleanupStep()
	if e.Power(k) != 2 {
		t.Fatalf("power after cleanup %d, want 2", e.Power(k))
	}
	o := putCreature(t, e, 0, other)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{k, o}})
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if e.Power(k) != 2 {
		t.Fatal("exalted fired for two attackers")
	}
	replayCheck(t, e, cfg)

	e2, cfg2, sw := newFixtureDeck(t, 84,
		"Name:Swift\nManaCost:R\nTypes:Creature Human Monk\nPT:1/2\nK:Haste\nK:Prowess\nOracle:x\n",
		"Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	e2.emit(events.Event{Kind: events.MoveZone, Obj: sw, From: state.ZHand, To: state.ZBattlefield})
	e2.priorityRound()
	bolt := addToHand(t, e2, 0, "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n")
	addMana(t, e2, 0, "R")
	e2.Advance()
	castObj(t, e2, bolt)
	passUntilStackEmpty(t, e2, 20)
	if e2.Power(sw) != 2 || e2.Toughness(sw) != 3 {
		t.Fatalf("prowess: %d/%d", e2.Power(sw), e2.Toughness(sw))
	}
	replayCheck(t, e2, cfg2)
}

// TestPersistUsesRealCorpusCard drives Safehold Elite's real compiled script
// (K:Persist is its only non-printed line) through CR 702.77: it dies with no
// -1/-1 counter and returns under its owner's control with one; the returned
// 1/1 dies again and stays dead; and a copy that already carried a -1/-1
// counter from another source never returns at all. Persist is the mirror of
// Undying, so the dies-condition (counters_EQ0_M1M1) is read off the LKI by
// the same trigger_match.go path TestUndyingReturnsOnceWithACounter pins.
func TestPersistUsesRealCorpusCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	elite, ok := reg.Lookup("Safehold Elite")
	if !ok {
		t.Fatal("Safehold Elite missing from corpus")
	}
	if d := elite.Link(); len(d) != 0 {
		t.Fatalf("link Safehold Elite: %v", d)
	}
	deck := make([]*cards.Card, 40)
	for i := range deck {
		deck[i] = elite
	}
	newElite := func(seed uint64) (*Engine, Config, state.ObjID) {
		cfg := seatZeroStart(Config{Seed: seed, Names: []string{"elite", "other"}, Decks: [][]*cards.Card{deck, deck}})
		e := New(cfg)
		var id state.ObjID
		for _, c := range e.G.Objs {
			if c.Owner == 0 && c.Face() != nil && c.Face().Name == "Safehold Elite" {
				id = c.ID
				break
			}
		}
		if id == 0 {
			t.Fatal("Safehold Elite was not created")
		}
		return e, cfg, id
	}

	// Dies with no -1/-1 counter: returns to the battlefield with one, and
	// the printed 2/2 is a 1/1 once the counter applies.
	e, cfg, id := newElite(701)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 2})
	e.checkStateBased()
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Owner != 0 || o.Counter("M1M1") != 1 {
		t.Fatalf("after first death: zone %s owner %d counters %d, want battlefield/0/1", o.Zone, o.Owner, o.Counter("M1M1"))
	}
	if e.Power(id) != 1 || e.Toughness(id) != 1 {
		t.Fatalf("persisted Safehold Elite = %d/%d, want 1/1", e.Power(id), e.Toughness(id))
	}
	// Dies again, this time carrying the -1/-1 counter: it stays dead.
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 1})
	e.checkStateBased()
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("persist returned a creature that had a -1/-1 counter: zone %s", z)
	}
	replayCheck(t, e, cfg)

	// A -1/-1 counter from another source is the same condition: a Persist
	// creature that already had one when it died does not return.
	e2, cfg2, id2 := newElite(702)
	e2.emit(events.Event{Kind: events.MoveZone, Obj: id2, From: state.ZLibrary, To: state.ZBattlefield})
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "M1M1", Amount: 1})
	if e2.Power(id2) != 1 || e2.Toughness(id2) != 1 {
		t.Fatalf("Safehold Elite with a -1/-1 counter = %d/%d, want 1/1", e2.Power(id2), e2.Toughness(id2))
	}
	e2.emit(events.Event{Kind: events.Damage, Obj: id2, Amount: 1})
	e2.checkStateBased()
	e2.priorityRound()
	passUntilStackEmpty(t, e2, 20)
	// Counters fall off on the way to the graveyard (CR 400.7), so the zone
	// is the whole assertion here: the Persist trigger read the -1/-1 counter
	// off the LKI and did not fire.
	if z := e2.G.Obj(id2).Zone; z != state.ZGraveyard {
		t.Fatalf("persist returned a creature that already had a -1/-1 counter: zone %s", z)
	}
	replayCheck(t, e2, cfg2)
}
