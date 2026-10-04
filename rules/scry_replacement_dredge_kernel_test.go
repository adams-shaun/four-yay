package rules

// Kernel-era restoration of scry_replacement_dredge_test.go: a Draw-instead
// Scry replacement (Eligeth after Kenessos) whose first draw is itself
// replaced by Dredge. The answered draw must complete the remaining replaced
// draws AND the Scry spell's next ability, all derived from the log.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A Draw-instead result can itself suspend on Dredge. The answered draw must
// complete the remaining replaced draws AND the Scry spell's next ability.
func TestKr8ScryReplacementOrderDrawDredgeResumesSpell(t *testing.T) {
	t.Parallel()
	const spell = "Name:ScryThenLife\nManaCost:U\nTypes:Sorcery\n" +
		"A:SP$ Scry | Defined$ You | ScryNum$ 3 | SubAbility$ DBLife\n" +
		"SVar:DBLife:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n"
	scry := card(t, spell)
	thug := corpusCard(t, "Golgari Thug")
	kenessos := corpusCard(t, "Kenessos, Priest of Thassa")
	eligeth := corpusCard(t, "Eligeth, Crossroads Augur")
	cfg := seatZeroStart(Config{Seed: 9243, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{scry, thug, kenessos, eligeth}, mountainDeck(t, 36)...),
			mountainDeck(t, 40),
		}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	var spellID, thugID, k, el state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			switch e.G.Obj(id).Face().Name {
			case "ScryThenLife":
				spellID = id
			case "Golgari Thug":
				thugID = id
			case "Kenessos, Priest of Thassa":
				k = id
			case "Eligeth, Crossroads Augur":
				el = id
			}
		}
	}
	if spellID == 0 || thugID == 0 || k == 0 || el == 0 || spellID == thugID || k == el {
		t.Fatalf("precondition: spell %d, dredger %d and replacement sources %d/%d must be distinct in the deck", spellID, thugID, k, el)
	}
	if from := e.G.Obj(spellID).Zone; from != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spellID, From: from, To: state.ZHand})
	}
	for _, move := range []struct {
		id state.ObjID
		to state.Zone
	}{
		{thugID, state.ZGraveyard}, {k, state.ZBattlefield}, {el, state.ZBattlefield},
	} {
		if from := e.G.Obj(move.id).Zone; from != move.to {
			e.emit(events.Event{Kind: events.MoveZone, Obj: move.id, From: from, To: move.to})
		}
	}
	// Moving the spell after Advance invalidates the pending priority offer.
	e.pending = nil
	e.Advance()
	addMana(t, e, 0, "U")
	for _, id := range []state.ObjID{k, el} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || len(o.Face().Repls) == 0 || o.Face().Repls[0].Event != "Scry" {
			t.Fatalf("precondition: Scry replacement source %d not active: %+v", id, o)
		}
	}
	if o := e.G.Obj(thugID); o.Zone != state.ZGraveyard || len(e.G.Zone(state.ZLibrary, 0)) < 10 {
		t.Fatalf("precondition: Dredge 4 needs thug in graveyard and at least ten cards in library: %+v", o)
	}
	life := e.G.Players[0].Life
	hand, lib := len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZLibrary, 0))
	since := len(e.L.Events)
	d := castFixture(t, e, spellID, -1)
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 || e.G.Obj(spellID).Zone != state.ZStack {
		t.Fatalf("precondition: Scry spell must be parked on affected player's order choice: %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == k {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Kenessos order option missing: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "dredge" || d.Player != 0 || e.G.Obj(spellID).Zone != state.ZStack {
		t.Fatalf("Kenessos first should offer Dredge on Eligeth's first of four draws: %+v", d)
	}
	dredgeIdx := -1
	for _, opt := range d.Options {
		if opt.Kind == "dredge" && opt.Obj == thugID {
			dredgeIdx = opt.Index
		}
	}
	if dredgeIdx < 0 {
		t.Fatalf("precondition: Thug's Dredge 4 is not offered: %+v", d.Options)
	}
	// Accept the first Dredge, leaving the remaining three draws ordinary.
	submitChoices(t, e, dredgeIdx)
	if got := e.Pending(); got != nil && (got.Kind == decision.KArrange || got.Kind == decision.KReplacement || got.ResumeKind == "dredge") {
		t.Fatalf("nested answer left the Scry or draw replacement parked: %+v", got)
	}
	if o := e.G.Obj(spellID); o.Zone != state.ZGraveyard {
		t.Fatalf("Scry spell did not finish resolving: zone = %v", o.Zone)
	}
	if o := e.G.Obj(thugID); o.Zone != state.ZHand {
		t.Fatalf("accepted Dredge did not return Thug to hand: zone = %v", o.Zone)
	}
	if got := life + 2; e.G.Players[0].Life != got {
		t.Fatalf("Scry spell's post-Scry ability lost: life = %d, want %d", e.G.Players[0].Life, got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != 3 { // cast -1, dredge +1, draw +3
		t.Fatalf("net hand delta = %d, want 3 (cast, dredge, remaining draws)", got)
	}
	if got := lib - len(e.G.Zone(state.ZLibrary, 0)); got != 7 { // mill 4, draw 3
		t.Fatalf("library delta = %d, want 7 (four milled, three drawn)", got)
	}
	if got := drawEvents(t, e, since); got != 3 {
		t.Fatalf("Draw events = %d, want 3", got)
	}
	if got := millEvents(t, e, since); got != 4 {
		t.Fatalf("mill events = %d, want 4", got)
	}
	if marks := scryMarkers(e); len(marks) != 0 {
		t.Fatalf("replaced Scry logged a marker: %+v", marks)
	}
	if notes := scryUnimplementedNotes(e); len(notes) != 0 {
		t.Fatalf("unimplemented Scry replacement: %+v", notes)
	}
	// The entire suspended chain must be derived from the same event log.
	replayCheck(t, e, cfg)
}
