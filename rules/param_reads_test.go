package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the parameter reads the param-census ratchet shrank for
// (task inbox-paramcensus-planeswalker-ultimate-and-mass-move), one test per
// API touched, each end to end on the real corpus card whose label was
// removed (or, for the Effect Stackable$ dedup, on an inline fixture whose
// static actually registers — the only repo-deck carrier, Wrenn and Six,
// registers nothing but a Note for its emblem static). The helpers are the
// deck-engine fixtures the neighbouring tests share.

// censusEngine builds a two-seat engine from the given decks (any cards.Card
// source: corpus lookups or inline test fixtures), parks it at seat 0's turn
// 2 Main1 (walkerBoard's shape), and returns it with its config for
// replayCheck. Deck lists shorter than 40 are filled with Mountains.
func censusEngine(t *testing.T, seed uint64, deck0, deck1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	mtn := mustCorpusCard(t, testutil.CorpusRegistry(t), "Mountain")
	fill := func(d []*cards.Card) []*cards.Card {
		out := append([]*cards.Card(nil), d...)
		for len(out) < 40 {
			out = append(out, mtn)
		}
		return out
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"p0", "p1"},
		Decks: [][]*cards.Card{fill(deck0), fill(deck1)}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	e.priorityRound()
	return e, cfg
}

// moveOwnerCard moves one owner-copy of c to `to` through a logged MoveZone,
// wherever genesis dealt it, and returns its id.
func moveOwnerCard(t *testing.T, e *Engine, owner state.PlayerID, c *cards.Card, to state.Zone) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == owner && o.Card == c {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: to})
			return o.ID
		}
	}
	t.Fatalf("no owner-%d copy of %s in any zone", owner, c.Faces[0].Name)
	return 0
}

// TestEffectStackableFalseDoesNotStack: an AB$ Effect with Stackable$ False
// registers its continuous effect once; a second activation of the same
// named effect is declined with a "not stacked" Note (Forge's
// EffectEffect.createEffect skip) instead of stacking a second instance.
func TestEffectStackableFalseDoesNotStack(t *testing.T) {
	t.Parallel()
	src := "Name:PicEmblem\nManaCost:1\nTypes:Enchantment\n" +
		"A:AB$ Effect | Cost$ R | Name$ PicEmblem | Stackable$ False | StaticAbilities$ STpic | Duration$ Permanent | SpellDescription$ x\n" +
		"SVar:STpic:Mode$ CantTarget | ValidTarget$ Creature | Description$ x\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 97, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "R")
	for i := 0; i < 2; i++ {
		opt := abilityOption(t, e, id, 0)
		submitChoices(t, e, opt.Index)
		passUntilStackEmpty(t, e, 20)
		addMana(t, e, 0, "R")
	}
	n := 0
	for _, ce := range e.continuous {
		if ce.Name == "PicEmblem" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Stackable$ False stacked %d PicEmblem effects, want 1", n)
	}
	sawDecline := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "effect not stacked (PicEmblem)") {
			sawDecline = true
		}
	}
	if !sawDecline {
		t.Fatal("second activation was not declined with a not-stacked Note")
	}
}

// TestEffectStackableDefaultStacks: an AB$ Effect with Name$ and NO
// Stackable$ is FORGE-DEFAULT STACKABLE — the corpus carries Stackable$ only
// as "False" (38 raw lines, no "True"), so the dedup gate must fire only on
// an explicit "False". Two activations of the same named effect stack TWO
// registry instances and no not-stacked Note is emitted (the en-Kor
// "en-Kor Redirection" shape, where stacking is the card's whole point).
func TestEffectStackableDefaultStacks(t *testing.T) {
	t.Parallel()
	src := "Name:PicStackEffect\nManaCost:1\nTypes:Enchantment\n" +
		"A:AB$ Effect | Cost$ R | Name$ PicStackEffect | StaticAbilities$ STpic | Duration$ Permanent | SpellDescription$ x\n" +
		"SVar:STpic:Mode$ CantTarget | ValidTarget$ Creature | Description$ x\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 98, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.priorityRound()
	for i := 0; i < 2; i++ {
		addMana(t, e, 0, "R")
		opt := abilityOption(t, e, id, 0)
		submitChoices(t, e, opt.Index)
		passUntilStackEmpty(t, e, 20)
	}
	n := 0
	for _, ce := range e.continuous {
		if ce.Name == "PicStackEffect" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("absent Stackable$ registered %d PicStackEffect instances, want 2", n)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "effect not stacked") {
			t.Fatalf("the stackable default was declined: %+v", ev)
		}
	}
}

// TestTerminusChangeZoneAllLibraryPositionBottom: Terminus' SP$ ChangeZoneAll
// with LibraryPosition$ -1 puts every battlefield creature on the BOTTOM of
// its owner's library, in the move order, with no extra placement event.
// EVENT-NEUTRALITY PIN ONLY: "-1" is exactly the MoveZone bottom append the
// mass move already produces, so this test stays green on a tree where the
// LibraryPosition$ read is reverted — the READ itself is enforced by the
// census ratchet (TestEveryRepoDeckParamsAreRead names Terminus); the
// observable half of the read is pinned by
// TestChangeZoneAllLibraryPositionZeroPinsTopOfOwnerLibrary below.
func TestTerminusChangeZoneAllLibraryPositionBottom(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	terminus := mustCorpusCard(t, reg, "Terminus")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 4711, []*cards.Card{terminus, bear}, []*cards.Card{bear})
	cardToHand(t, e, terminus)
	bear0 := moveOwnerCard(t, e, 0, bear, state.ZBattlefield)
	bear1 := moveOwnerCard(t, e, 1, bear, state.ZBattlefield)
	addMana(t, e, 0, "WWWWWW")
	base := len(e.L.Events)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	lib0, lib1 := e.G.Zone(state.ZLibrary, 0), e.G.Zone(state.ZLibrary, 1)
	if len(lib0) == 0 || lib0[len(lib0)-1] != bear0 {
		t.Fatalf("seat 0's bear not on the library bottom: %v", lib0)
	}
	if len(lib1) == 0 || lib1[len(lib1)-1] != bear1 {
		t.Fatalf("seat 1's bear not on the library bottom: %v", lib1)
	}
	for _, ev := range e.L.Events[base:] {
		if ev.Kind == events.Shuffle || ev.Kind == events.LibraryOrder {
			t.Fatalf("Terminus emitted a placement/shuffle event: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneAllLibraryPositionZeroPinsTopOfOwnerLibrary: the OBSERVABLE
// half of ChangeZoneAll's LibraryPosition$ read — "0" pins the moved cards on
// TOP of their owners' libraries with one Secret LibraryOrder each. The
// moved battlefield creature here is controlled by seat 0 but OWNED by seat
// 1 (the Gomazoa / Vortex Elemental blocking shape), and the placement must
// act on the OWNER's library the MoveZone actually landed the card in, not
// the source-zone scan's controller.
func TestChangeZoneAllLibraryPositionZeroPinsTopOfOwnerLibrary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	pin := card(t, "Name:PicMassPin\nManaCost:2 U\nTypes:Sorcery\n"+
		"A:SP$ ChangeZoneAll | ChangeType$ Creature | Origin$ Battlefield | Destination$ Library | LibraryPosition$ 0\nOracle:x\n")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 5353, []*cards.Card{pin, bear}, []*cards.Card{bear})
	cardToHand(t, e, pin)
	bear1 := moveOwnerCard(t, e, 1, bear, state.ZBattlefield)
	// An opposing creature under seat 0's control: it scans out of seat 0's
	// battlefield zone, but its library destination is still seat 1's.
	e.emit(events.Event{Kind: events.ControlChange, Obj: bear1, Player: 0})
	addMana(t, e, 0, "UUU")
	base := len(e.L.Events)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear1); o == nil || o.Zone != state.ZLibrary || o.Owner != 1 {
		t.Fatalf("bear not in its owner's library: %+v", o)
	}
	lib1 := e.G.Zone(state.ZLibrary, 1)
	if len(lib1) == 0 || lib1[0] != bear1 {
		t.Fatalf("seat 1's bear not pinned on top of its owner's library: %v", lib1)
	}
	orders := 0
	for _, ev := range e.L.Events[base:] {
		if ev.Kind != events.LibraryOrder {
			continue
		}
		orders++
		if ev.Player != 1 {
			t.Fatalf("LibraryOrder recorded under player %d, want the owner 1", ev.Player)
		}
		if len(ev.IDs) == 0 || ev.IDs[0] != bear1 {
			t.Fatalf("LibraryOrder did not pin the bear on top: %v", ev.IDs)
		}
	}
	if orders != 1 {
		t.Fatalf("want exactly one Secret LibraryOrder placement, got %d", orders)
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneAllShufflesDestinationLibraries: ChangeZoneAll's Shuffle$ True
// shuffles every destination library that received a card, after the moves
// land (the Gomazoa "put on top ..., then those players shuffle" order).
func TestChangeZoneAllShufflesDestinationLibraries(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bounce := card(t, "Name:PicMassBounce\nManaCost:2 U\nTypes:Sorcery\n"+
		"A:SP$ ChangeZoneAll | ChangeType$ Creature | Origin$ Battlefield | Destination$ Library | Shuffle$ True\nOracle:x\n")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 4242, []*cards.Card{bounce, bear}, []*cards.Card{bear})
	cardToHand(t, e, bounce)
	bear0 := moveOwnerCard(t, e, 0, bear, state.ZBattlefield)
	bear1 := moveOwnerCard(t, e, 1, bear, state.ZBattlefield)
	addMana(t, e, 0, "UUU")
	base := len(e.L.Events)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	if lib := e.G.Zone(state.ZLibrary, 0); !containsID(lib, bear0) {
		t.Fatalf("seat 0's bear not back in its library: %v", lib)
	}
	if lib := e.G.Zone(state.ZLibrary, 1); !containsID(lib, bear1) {
		t.Fatalf("seat 1's bear not back in its library: %v", lib)
	}
	shuffles := 0
	for _, ev := range e.L.Events[base:] {
		if ev.Kind == events.Shuffle && ev.Secret {
			shuffles++
		}
	}
	if shuffles != 2 {
		t.Fatalf("want one Secret shuffle per destination library, got %d", shuffles)
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneAllGainControl: Rise of the Dark Realms puts every creature
// card in every graveyard onto the battlefield under its controller's
// control — an opponent's graveyard creature enters under seat 0's control
// (the ChangeZoneAll GainControl$ read, Karn Liberated's ReturnFromExile
// shape).
func TestChangeZoneAllGainControl(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	rise := mustCorpusCard(t, reg, "Rise of the Dark Realms")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 6161, []*cards.Card{rise}, []*cards.Card{bear})
	cardToHand(t, e, rise)
	bear1 := moveOwnerCard(t, e, 1, bear, state.ZGraveyard)
	addMana(t, e, 0, "BBBBBBBBB")
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(bear1)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the opponent's graveyard bear is not on the battlefield: %+v", o)
	}
	if o.Controller != 0 {
		t.Fatalf("GainControl$ left the creature with controller %d, want 0", o.Controller)
	}
	sawControl := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.ControlChange && ev.Obj == bear1 && ev.Player == 0 {
			sawControl = true
		}
	}
	if !sawControl {
		t.Fatal("no ControlChange event for the gained creature")
	}
	replayCheck(t, e, cfg)
}

// TestRestartGameRestrictFromNotesTheKeepSet: Karn Liberated's [-14] reads
// RestrictFromZone$/RestrictFromValid$ — a permanent exiled with Karn is in
// the complement of the restrict spec inside Exile, so the log names it as
// what the restart would keep, and the match then ends as a draw (this
// engine's documented RestartGame degradation; a real restart needs
// game-loop machinery the engine does not have).
func TestRestartGameRestrictFromNotesTheKeepSet(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	karn := mustCorpusCard(t, reg, "Karn Liberated")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 7171, []*cards.Card{karn}, []*cards.Card{bear})
	karnID := moveOwnerCard(t, e, 0, karn, state.ZBattlefield)
	bear1 := moveOwnerCard(t, e, 1, bear, state.ZBattlefield)
	e.pending = nil
	e.priorityRound()
	exileIdx, restartIdx := -1, -1
	for i, sa := range karn.Faces[0].Abilities {
		if sa.API == "RestartGame" {
			restartIdx = i
		}
		if sa.API == "ChangeZone" && strings.Contains(sa.Params["Cost"], "SubCounter<3/LOYALTY>") {
			exileIdx = i
		}
	}
	if exileIdx < 0 || restartIdx < 0 {
		t.Fatalf("Karn ability indices: exile %d restart %d", exileIdx, restartIdx)
	}
	// Turn 2: [-3] exiles the opponent's bear, recording the exiled-with
	// provenance the RestrictFromValid$ complement turns into the keep-set.
	opt := abilityOption(t, e, karnID, exileIdx)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, bear1)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear1); o.Zone != state.ZExile {
		t.Fatalf("[-3] did not exile the bear: %s", o.Zone)
	}
	// Turn 3: top Karn up to 15 loyalty and activate [-14] (the cost leaves 1,
	// so the zero-loyalty state-based action cannot sweep him mid-resolution).
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: karnID, Counter: "LOYALTY", Amount: 12})
	e.pending = nil
	e.priorityRound()
	opt = abilityOption(t, e, karnID, restartIdx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Over {
		t.Fatal("Karn's [-14] did not end the match")
	}
	sawKeep, sawDraw := false, false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "restart would keep in Exile: Grizzly Bears") {
			sawKeep = true
		}
		if ev.Kind == events.GameOver && ev.Amount == 1 {
			sawDraw = true
		}
	}
	if !sawKeep || !sawDraw {
		t.Fatalf("restart log wrong: keep-Note %v, draw GameOver %v", sawKeep, sawDraw)
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneTransformedEntersFlipped: Ojer Axonil's death trigger returns
// it to the battlefield with Transformed$ True, so it enters as its OTHER
// face (Temple of Power, FaceIdx 1) — the CR 711.10a transformed entry.
func TestChangeZoneTransformedEntersFlipped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	ojer := mustCorpusCard(t, reg, "Ojer Axonil, Deepest Might")
	e, cfg := censusEngine(t, 8181, []*cards.Card{ojer}, nil)
	ojerID := moveOwnerCard(t, e, 0, ojer, state.ZBattlefield)
	// The death: a logged battlefield-to-graveyard move fires the ChangesZone
	// trigger, whose Execute$ ChangeZone carries Transformed$ True.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ojerID, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(ojerID)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Ojer did not return to the battlefield: %+v", o)
	}
	if o.FaceIdx != 1 {
		t.Fatalf("Ojer returned on face %d, want the transformed face 1", o.FaceIdx)
	}
	sawFlip := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.FlipFace && ev.Obj == ojerID && ev.Amount == 1 {
			sawFlip = true
		}
	}
	if !sawFlip {
		t.Fatal("no FlipFace event for the transformed entry")
	}
	replayCheck(t, e, cfg)
}

// TestDealDamageReplaceDyingDefinedExilesInstead: Wilt in the Heat deals 5 to
// a 2/2 bear with ReplaceDyingDefined$ Targeted, so the lethal-damage SBA's
// battlefield-to-graveyard move is replaced: the bear is EXILED, and the
// continuous registry carries the Moved replacement that did it.
func TestDealDamageReplaceDyingDefinedExilesInstead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	wilt := mustCorpusCard(t, reg, "Wilt in the Heat")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	e, cfg := censusEngine(t, 9191, []*cards.Card{wilt, bear}, nil)
	cardToHand(t, e, wilt)
	bear0 := moveOwnerCard(t, e, 0, bear, state.ZBattlefield)
	addMana(t, e, 0, "GGRRW")
	castFirst(t, e, "cast")
	targetObject(t, e, bear0)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bear0); o.Zone != state.ZExile {
		t.Fatalf("the dealt-damage bear should be exiled, not dying: %s", o.Zone)
	}
	n := 0
	for _, ce := range e.continuous {
		if ce.ReplacementEvent == "Moved" {
			n++
			if len(ce.Remembered) != 1 || ce.Remembered[0] != bear0 {
				t.Fatalf("replacement remembered %v, want the bear %d", ce.Remembered, bear0)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one Moved replacement registered, got %d", n)
	}
	replayCheck(t, e, cfg)
}
