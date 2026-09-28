package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFatefulTempestStoresEachOptionVoteCount pins the real-corpus
// StoreVoteNum$ shape: each option's SVar body consumes VoteNum<choice>, and
// both outcomes resolve according to their own tally rather than only the
// most-voted option resolving.
func TestFatefulTempestStoresEachOptionVoteCount(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	fateful := searchCorpusCard(t, reg, "Fateful Tempest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := make([]*cards.Card, 40)
	deck[0] = fateful
	for i := 1; i < len(deck); i++ {
		deck[i] = mountain
	}
	decks := make([][]*cards.Card, 4)
	for p := range decks {
		decks[p] = make([]*cards.Card, 40)
		for i := range decks[p] {
			decks[p][i] = mountain
		}
	}
	decks[0] = deck
	cfg := seatZeroStart(Config{Seed: 48321, Names: []string{"a", "b", "c", "d"}, Decks: decks, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	var spell state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Fateful Tempest" {
				spell = id
				if z != state.ZHand {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZHand})
				}
				break
			}
		}
		if spell != 0 {
			break
		}
	}
	if spell == 0 {
		t.Fatal("precondition: Fateful Tempest is in seat 0's hand or library")
	}
	addMana(t, e, 0, "RRCCCC")
	d := castFixture(t, e, spell, -1)
	wantPicks := []string{"DBVotePast", "DBVotePresent", "DBVotePast", "DBVotePresent"}
	for i, want := range wantPicks {
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "vote" {
			t.Fatalf("voter %d pending = %+v, want Fateful Tempest vote decision", i, d)
		}
		index := -1
		for _, option := range d.Options {
			if option.Label == want {
				index = option.Index
				break
			}
		}
		if index < 0 {
			t.Fatalf("voter %d option %q missing from %+v", i, want, d.Options)
		}
		submitChoices(t, e, index)
		d = e.Pending()
	}
	if d != nil && d.Kind != decision.KPriority {
		t.Fatalf("after last vote pending = %+v, want priority", d)
	}
	countNamed := func(zone state.Zone, owner state.PlayerID, name string) int {
		n := 0
		for _, id := range e.G.Zone(zone, owner) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				n++
			}
		}
		return n
	}
	if got := countNamed(state.ZGraveyard, 0, "Mountain"); got != 2 {
		t.Fatalf("precondition/result: two past votes must mill two Mountains, got %d", got)
	}
	if got := countNamed(state.ZExile, 0, "Mountain"); got != 2 {
		t.Fatalf("two present votes must exile two Mountains, got %d", got)
	}
	replayCheck(t, e, cfg)
}
