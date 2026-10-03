package effects

// The NoteCards$/NoteCardsFor$ CARD-notation family (task card-notation:
// the object-side sibling of notecards_notation_test.go's player half). A
// DB$ Pump body records "this card was carried by that resolution" through
// events.CardNoted (Forge's NoteCardsEffect with NoteCards$ Remembered or
// TriggeredSource), and a later resolution reads it back through the shared
// card filter's `Card.NotedFor<label>` qualifier -- ChooseCard's Choices$,
// DB$ Play's Valid$ and RepeatEach's RepeatCards$ resolve through that read.
// CopyPermanent's RevealFromExile cost is present in corpus but unsupported.
//
// The real corpus carriers pin the exact spellings. Volatile Chimera is the
// reported card:
//
//	SVar:DBPump:DB$ Pump | NoteCards$ Remembered | NoteCardsFor$ VolatileChimera
//	A:AB$ ChooseCard | Choices$ Card.YouOwn+NotedForVolatileChimera | ...
//
// so the activated ability's random pick draws from exactly the cards the
// setup pass noted. The tests below bind Ctx.Remembered (or Ctx.TriggerSource)
// to the card directly, which is exactly the binding the preceding
// ChangeZone/trigger bodies provide in the real resolution.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// exileCardAt puts a card object in its owner's exile zone (the zone the
// setup-path carriers note cards in) and returns its id.
func exileCardAt(t *testing.T, h *fakeHost, card *cards.Card, owner state.PlayerID) state.ObjID {
	t.Helper()
	src := h.g.AddObject(card, owner)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZExile})
	return src.ID
}

// TestCardNotedForSelectsTheNotedCard drives Volatile Chimera's real DBPump
// body over a resolution Remembered set and asserts the activated ability's
// ChooseCard pool spec selects precisely the noted card.
func TestCardNotedForSelectsTheNotedCard(t *testing.T) {
	card, note := corpusSA(t, "Volatile Chimera", "DBPump")
	if got := note.Params["NoteCards"]; got != "Remembered" {
		t.Fatalf("Volatile Chimera NoteCards$ = %q, want Remembered", got)
	}
	if got := note.Params["NoteCardsFor"]; got != "VolatileChimera" {
		t.Fatalf("Volatile Chimera NoteCardsFor$ = %q, want VolatileChimera", got)
	}

	h := newHost(t, 2)
	src := noteCardAt(t, h, card)
	if got := h.g.Obj(src).Zone; got != state.ZBattlefield {
		t.Fatalf("notation source zone = %v, want battlefield", got)
	}
	noted := exileCardAt(t, h, mkCard(t, "Name:Exiled Ape\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	other := exileCardAt(t, h, mkCard(t, "Name:Exiled Owl\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	if noted == other || h.g.Obj(noted).Zone != state.ZExile || h.g.Obj(other).Zone != state.ZExile {
		t.Fatalf("setup broken: noted=%d other=%d zones=%v/%v", noted, other,
			h.g.Obj(noted).Zone, h.g.Obj(other).Zone)
	}
	if un := UnknownPredicates("Card.YouOwn+NotedForVolatileChimera"); len(un) != 0 {
		t.Fatalf("corpus ChooseCard spec %q is not recognised: %v", "Card.YouOwn+NotedForVolatileChimera", un)
	}

	effPump(h, &Ctx{Source: src, Controller: 0, SVars: h.g.Obj(src).Face().SVars,
		Remembered: []state.Target{{Obj: noted}}}, note)

	if got := h.g.Players[0].Notes; len(got) != 0 {
		t.Fatalf("card notation wrote player labels = %v, want none", got)
	}
	if got := h.g.Obj(noted).Notes; len(got) != 1 || got[0] != "VolatileChimera" {
		t.Fatalf("noted card Notes = %v, want [VolatileChimera]", got)
	}
	if got := h.g.Obj(other).Notes; len(got) != 0 {
		t.Fatalf("unnoted card Notes = %v, want none", got)
	}
	if !MatchesSpecFrom(h.g, "Card.YouOwn+NotedForVolatileChimera", noted, 0, src) {
		t.Fatal("ChooseCard pool spec did not select the noted card")
	}
	if MatchesSpecFrom(h.g, "Card.YouOwn+NotedForVolatileChimera", other, 0, src) {
		t.Fatal("ChooseCard pool spec matched an unnoted card")
	}
}

// TestCardNotedFoldIsIdempotentOrderedAndReplayable pins the event fold: one
// CardNoted appends the label to the object's note set, a re-note of the SAME
// label does not duplicate it, order is first-note order, an empty label or a
// vanished object writes nothing, and a log replay plus an isolated clone
// rebuild the exact state.
func TestCardNotedFoldIsIdempotentOrderedAndReplayable(t *testing.T) {
	h := newHost(t, 2)
	ape := exileCardAt(t, h, mkCard(t, "Name:Exiled Ape\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)

	h.Emit(events.Event{Kind: events.CardNoted, Obj: ape, Text: "VolatileChimera"})
	h.Emit(events.Event{Kind: events.CardNoted, Obj: ape, Text: "ArcaneSavant"})
	h.Emit(events.Event{Kind: events.CardNoted, Obj: ape, Text: "VolatileChimera"})
	h.Emit(events.Event{Kind: events.CardNoted, Obj: ape, Text: ""})
	h.Emit(events.Event{Kind: events.CardNoted, Obj: 99999, Text: "VolatileChimera"})

	if got := h.g.Obj(ape).Notes; len(got) != 2 || got[0] != "VolatileChimera" || got[1] != "ArcaneSavant" {
		t.Fatalf("card notes = %v, want [VolatileChimera ArcaneSavant] (deduped, first-note order)", got)
	}

	replayed := state.NewGame(names(2))
	rpApe := replayed.AddObject(mkCard(t, "Name:Exiled Ape\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	if rpApe.ID != ape {
		t.Fatalf("replay board setup: fresh object id = %d, want the noted card's %d", rpApe.ID, ape)
	}
	noteEvents := 0
	for _, e := range h.log {
		events.Apply(replayed, e)
		if e.Kind == events.CardNoted {
			noteEvents++
		}
	}
	if noteEvents != 5 {
		t.Fatalf("log carried %d CardNoted events, want all 5 (including the two no-op writes)", noteEvents)
	}
	if got := replayed.Obj(ape).Notes; len(got) != 2 || got[0] != "VolatileChimera" || got[1] != "ArcaneSavant" {
		t.Fatalf("replayed notes = %v, want [VolatileChimera ArcaneSavant]", got)
	}
	if un := UnknownPredicates("Card.NotedForArcaneSavant"); len(un) != 0 {
		t.Fatalf("bare Card.NotedFor<X> not recognised: %v", un)
	}
	if !MatchesSpec(replayed, "Card.NotedForArcaneSavant", ape, 0) {
		t.Fatal("replay lost the ArcaneSavant card note")
	}

	clone := replayed.Clone()
	events.Apply(replayed, events.Event{Kind: events.CardNoted, Obj: ape, Text: "MaelstromArchangelAvatar"})
	if got := clone.Obj(ape).Notes; len(got) != 2 {
		t.Fatalf("clone aliases the original's Notes: clone=%v after a note was added to the original", got)
	}
	if got := replayed.Obj(ape).Notes; len(got) != 3 || got[2] != "MaelstromArchangelAvatar" {
		t.Fatalf("replayed notes = %v, want the third label appended", got)
	}
}

// TestCardNotationTriggeredSourceNotesTheTriggerCard drives Maelstrom
// Archangel Avatar's real TrigNote body: the DamageDone trigger executes DB$
// Pump | NoteCards$ TriggeredSource | NoteCardsFor$ MaelstromArchangelAvatar,
// and the vanguard's actual TrigRepeatEach RepeatCards$ reader
// (`Card.NotedForMaelstromArchangelAvatar`) must select exactly the creature
// that dealt the damage.
func TestCardNotationTriggeredSourceNotesTheTriggerCard(t *testing.T) {
	card, note := corpusSA(t, "Maelstrom Archangel Avatar", "TrigNote")
	if got := note.Params["NoteCards"]; got != "TriggeredSource" {
		t.Fatalf("Maelstrom NoteCards$ = %q, want TriggeredSource", got)
	}
	if got := note.Params["NoteCardsFor"]; got != "MaelstromArchangelAvatar" {
		t.Fatalf("Maelstrom NoteCardsFor$ = %q, want MaelstromArchangelAvatar", got)
	}

	h := newHost(t, 2)
	src := noteCardAt(t, h, card)
	if got := h.g.Obj(src).Zone; got != state.ZBattlefield {
		t.Fatalf("notation source zone = %v, want battlefield", got)
	}
	dealer := noteCardAt(t, h, mkCard(t, "Name:Damage Dealer\nTypes:Creature\nPT:3/3\nOracle:x\n"))
	bystander := noteCardAt(t, h, mkCard(t, "Name:Bystander\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	if dealer == bystander || h.g.Obj(dealer).Zone != state.ZBattlefield || h.g.Obj(bystander).Zone != state.ZBattlefield {
		t.Fatalf("setup broken: dealer=%d bystander=%d", dealer, bystander)
	}

	effPump(h, &Ctx{TriggerContext: TriggerContext{TriggerSource: dealer},
		Source: src, Controller: 0, SVars: h.g.Obj(src).Face().SVars}, note)

	if got := h.g.Players[0].Notes; len(got) != 0 {
		t.Fatalf("card notation wrote player labels = %v, want none", got)
	}
	if !MatchesSpec(h.g, "Card.NotedForMaelstromArchangelAvatar", dealer, 0) {
		t.Fatal("RepeatCards$ spec did not select the triggering source")
	}
	if MatchesSpec(h.g, "Card.NotedForMaelstromArchangelAvatar", bystander, 0) {
		t.Fatal("RepeatCards$ spec matched a creature the trigger did not name")
	}

	_, repeat := corpusSA(t, "Maelstrom Archangel Avatar", "TrigRepeatEach")
	if repeat.Params["RepeatCards"] != "Card.NotedForMaelstromArchangelAvatar" {
		t.Fatalf("Maelstrom TrigRepeatEach RepeatCards$ = %q", repeat.Params["RepeatCards"])
	}
	if repeat.Params["Zone"] != "Battlefield,Graveyard,Exile,Library,Hand" {
		t.Fatalf("Maelstrom RepeatEach Zone$ = %q", repeat.Params["Zone"])
	}
	got, ok := repeatedCards(h, &Ctx{Controller: 0}, RepeatEachOf(repeat))
	if !ok {
		t.Fatal("Maelstrom RepeatEach did not run its RepeatCards$ reader")
	}
	selected := make(map[state.ObjID]bool, len(got))
	for _, target := range got {
		selected[target.Obj] = true
	}
	if !selected[dealer] || selected[bystander] {
		t.Fatalf("Maelstrom RepeatEach selected %v, want dealer %d and exclude bystander %d", got, dealer, bystander)
	}
}

// TestCardNotationIsReadByCorpusChooseCardAndPlayConsumers drives the real
// consumer handlers, not only MatchesSpec: Volatile Chimera's Choices$ pool
// and Arcane Savant's Valid$ Play population must each admit the noted card
// and reject the contrasting unnoted object.
func TestCardNotationIsReadByCorpusChooseCardAndPlayConsumers(t *testing.T) {
	chimera, pump := corpusSA(t, "Volatile Chimera", "DBPump")
	var choose *cards.SA
	for _, face := range chimera.Faces {
		for _, ability := range face.Abilities {
			if ability.API == "ChooseCard" {
				choose = ability
			}
		}
	}
	if choose == nil {
		t.Fatal("Volatile Chimera ChooseCard consumer not found")
	}
	h := newHost(t, 2)
	h.askResult = true
	src := noteCardAt(t, h, chimera)
	noted := exileCardAt(t, h, mkCard(t, "Name:Exiled Ape\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	other := exileCardAt(t, h, mkCard(t, "Name:Exiled Owl\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	if noted == other || h.g.Obj(noted).Zone != state.ZExile || h.g.Obj(other).Zone != state.ZExile {
		t.Fatalf("ChooseCard fixture invalid: noted=%d other=%d zones=%v/%v", noted, other, h.g.Obj(noted).Zone, h.g.Obj(other).Zone)
	}
	effPump(h, &Ctx{Source: src, Controller: 0, SVars: h.g.Obj(src).Face().SVars, Remembered: []state.Target{{Obj: noted}}}, pump)
	choiceCtx := &Ctx{Source: src, Controller: 0}
	effChooseCard(h, choiceCtx, choose)
	assertOptions := func(where string, options []decision.Option, want, reject state.ObjID) {
		t.Helper()
		seen := map[state.ObjID]bool{}
		for _, option := range options {
			seen[option.Obj] = true
		}
		if !seen[want] || seen[reject] {
			t.Fatalf("%s options = %v, want noted %d and not unnoted %d", where, options, want, reject)
		}
	}
	// AtRandom$ True is the corpus behavior: the handler samples its filtered
	// candidate pool and records the selected target, without posing an ask.
	if len(choiceCtx.Chosen) != 1 || choiceCtx.Chosen[0].Obj != noted {
		t.Fatalf("Volatile Chimera ChooseCard selected %v, want only noted card %d (unnoted %d excluded)", choiceCtx.Chosen, noted, other)
	}

	// Arcane Savant's real DB$ Play Valid$ consumer requires playable spells;
	// use distinct instant cards in the exact Exile zone it scans.
	savant, play := corpusSA(t, "Arcane Savant", "TrigPlay")
	playSource := noteCardAt(t, h, savant)
	playNoted := exileCardAt(t, h, mkCard(t, "Name:Noted Instant\nTypes:Instant\nOracle:x\n"), 0)
	playOther := exileCardAt(t, h, mkCard(t, "Name:Other Instant\nTypes:Instant\nOracle:x\n"), 0)
	_, playPump := corpusSA(t, "Arcane Savant", "DBPump")
	if playNoted == playOther || h.g.Obj(playNoted).Zone != state.ZExile || h.g.Obj(playOther).Zone != state.ZExile {
		t.Fatalf("Play fixture invalid: noted=%d other=%d zones=%v/%v", playNoted, playOther, h.g.Obj(playNoted).Zone, h.g.Obj(playOther).Zone)
	}
	effPump(h, &Ctx{Source: playSource, Controller: 0, SVars: h.g.Obj(playSource).Face().SVars, Remembered: []state.Target{{Obj: playNoted}}}, playPump)
	h.lastAsk = nil
	effPlay(h, &Ctx{Source: playSource, Controller: 0}, play)
	if h.lastAsk == nil {
		t.Fatal("Arcane Savant Play did not ask")
	}
	assertOptions("Arcane Savant Play", h.lastAsk.Options, playNoted, playOther)
	if playNoted == playOther || h.g.Obj(playNoted).Zone != state.ZExile || h.g.Obj(playOther).Zone != state.ZExile {
		t.Fatalf("Play fixture invalid: noted=%d other=%d zones=%v/%v", playNoted, playOther, h.g.Obj(playNoted).Zone, h.g.Obj(playOther).Zone)
	}
}

// TestUnimplementedNoteCardsFormIsLoud pins the fail-loud contract for a
// NoteCards$ form this build does not model: the body records a transcript
// note and writes NO state, so a future form can never succeed silently.
func TestUnimplementedNoteCardsFormIsLoud(t *testing.T) {
	body := sa(t, "AB$ Pump | NoteCards$ SomeFutureForm | NoteCardsFor$NewLabel")
	h := newHost(t, 2)
	src := noteCardAt(t, h, mkCard(t, "Name:Noter\nTypes:Creature\nPT:1/1\nOracle:x\n"))

	effPump(h, &Ctx{Source: src, Controller: 0, SVars: h.g.Obj(src).Face().SVars}, body)

	sawNote := false
	for _, e := range h.log {
		if e.Kind == events.CardNoted || e.Kind == events.PlayerNoted {
			t.Fatalf("an unimplemented NoteCards$ form wrote state: %+v", e)
		}
		if e.Kind == events.Note && strings.Contains(e.Text, "unimplemented NoteCards$ SomeFutureForm") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatal("the unimplemented-NoteCards$ loud note was never emitted -- the handler did not run")
	}
	if got := h.g.Players[0].Notes; len(got) != 0 {
		t.Fatalf("player labels = %v, want none", got)
	}
}
