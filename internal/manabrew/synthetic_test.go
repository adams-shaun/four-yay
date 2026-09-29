package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/protocol"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// noteEvent builds a raw protocol.EventBody Note the way host/viewat.go's
// EventsSeat would hand one to a translator: only the fields Synthetic reads
// are set.
func noteEvent(seq uint64, player uint8, obj uint32, text string, ids []uint32, secret bool, pairs [][2]uint32, amount int32) protocol.EventBody {
	return protocol.EventBody{Event: protocol.Event{
		Seq: seq, Kind: "note", Player: player, Obj: obj, Text: text, IDs: ids, Secret: secret, Pairs: pairs, Amount: amount,
	}}
}

// TestSyntheticReveal covers the public-reveal Note shape (effReveal's own
// convention): a card in an always-public zone (graveyard here) resolves to
// a real CardDto and the zone/owner the reveal named; the ack round-trips
// through AcknowledgeSynthetic and never produces an intent-shaped anything.
func TestSyntheticReveal(t *testing.T) {
	tr := New("table", 2, nil)
	v := view.View{Viewer: 0, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Graveyard: []view.CardView{
			{ID: 42, Name: "Bear", Printing: view.Printing{Name: "Bear"}, Types: "Creature — Bear"},
		}},
		{ID: 1, Name: "Bob"},
	}}
	evs := []protocol.EventBody{noteEvent(101, 0, 0, "", []uint32{42}, false, nil, 0)}

	msgs := tr.Synthetic(evs, &v)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	pm, ok := msgs[0].Value.(mb.PromptMessage)
	if !ok {
		t.Fatalf("message type = %T, want PromptMessage", msgs[0].Value)
	}
	in, ok := pm.Input.Value.(mb.RevealCardsInput)
	if !ok {
		t.Fatalf("input type = %s, want revealCards", pm.Input.Value.PromptType())
	}
	if in.Zone != mb.ZoneGraveyard {
		t.Fatalf("zone = %s, want graveyard", in.Zone)
	}
	if in.OwnerPlayerID != "player-0" {
		t.Fatalf("ownerPlayerId = %s, want player-0", in.OwnerPlayerID)
	}
	if len(in.Cards) != 1 || in.Cards[0].Identity.Name != "Bear" {
		t.Fatalf("cards = %#v, want [Bear]", in.Cards)
	}
	if pm.DecidingPlayerID != "player-0" {
		t.Fatalf("decidingPlayerId = %s, want player-0 (the connected viewer)", pm.DecidingPlayerID)
	}
	if pm.PromptID != syntheticPromptID(101, 0) {
		t.Fatalf("promptId = %d, want %d", pm.PromptID, syntheticPromptID(101, 0))
	}

	ack := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: "revealCards", Output: mb.PromptOutputData{Value: mb.RevealCardsAcknowledged{}}}}}
	if err := AcknowledgeSynthetic(ack, pm); err != nil {
		t.Fatalf("AcknowledgeSynthetic(correct ack) = %v, want nil", err)
	}
	wrongType := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: "diceRolled", Output: mb.PromptOutputData{Value: mb.DiceRolledAcknowledged{}}}}}
	if err := AcknowledgeSynthetic(wrongType, pm); err == nil || err.Code != mb.CodeWrongPromptType {
		t.Fatalf("wrong ack type: want wrongPromptType, got %v", err)
	}
	stale := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID + 1,
		Action: mb.PromptOutput{Type: "revealCards", Output: mb.PromptOutputData{Value: mb.RevealCardsAcknowledged{}}}}}
	if err := AcknowledgeSynthetic(stale, pm); err == nil || err.Code != mb.CodeStalePrompt {
		t.Fatalf("stale promptId: want stalePrompt, got %v", err)
	}
}

// TestSyntheticRevealDegradesUnresolvedCard covers the documented gap: an id
// the connected seat's own view cannot resolve (an opponent's hand, a
// library card not on top) still mints a prompt, with a blank-identity
// CardDto carrying only the id -- never a fabricated name.
func TestSyntheticRevealDegradesUnresolvedCard(t *testing.T) {
	tr := New("table", 2, nil)
	v := view.View{Viewer: 0, Players: []view.PlayerView{{ID: 0}, {ID: 1}}}
	evs := []protocol.EventBody{noteEvent(5, 1, 0, "", []uint32{999}, false, nil, 0)}

	msgs := tr.Synthetic(evs, &v)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	in := msgs[0].Value.(mb.PromptMessage).Input.Value.(mb.RevealCardsInput)
	if len(in.Cards) != 1 || in.Cards[0].ID != "o999" || in.Cards[0].Identity.Name != "" {
		t.Fatalf("unresolved card = %#v, want a blank identity carrying id o999", in.Cards)
	}
	if in.Zone != mb.ZoneLibrary {
		t.Fatalf("zone default = %s, want library", in.Zone)
	}
	if in.OwnerPlayerID != "player-1" {
		t.Fatalf("ownerPlayerId = %s, want player-1", in.OwnerPlayerID)
	}
}

// TestSyntheticSecretNoteIsNotAReveal pins the Secret-bit boundary: a private
// look (effects' emitLook) shares every other field of the public-reveal
// shape and must never become a synthetic prompt every seat would see.
func TestSyntheticSecretNoteIsNotAReveal(t *testing.T) {
	tr := New("table", 2, nil)
	v := view.View{Viewer: 0}
	evs := []protocol.EventBody{noteEvent(5, 0, 0, "", []uint32{1}, true, nil, 0)}
	if msgs := tr.Synthetic(evs, &v); len(msgs) != 0 {
		t.Fatalf("a Secret Note must never become a public reveal prompt, got %#v", msgs)
	}
}

// TestSyntheticDice covers effects/dice.go's per-die-then-batch Note pair:
// the per-die Notes fold into one diceRolled prompt's single round, keyed
// off the closing batch Note's own Seq.
func TestSyntheticDice(t *testing.T) {
	tr := New("table", 2, nil)
	v := view.View{Viewer: 0}
	evs := []protocol.EventBody{
		noteEvent(10, 1, 7, "rolls a d6: 3", nil, false, [][2]uint32{{6, 3}}, 3),
		noteEvent(11, 1, 7, "rolls a d6: 5", nil, false, [][2]uint32{{6, 5}}, 5),
		noteEvent(12, 1, 7, "rolls dice: 2 -> 5", nil, false, [][2]uint32{{2, 5}}, 5),
	}

	msgs := tr.Synthetic(evs, &v)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	pm := msgs[0].Value.(mb.PromptMessage)
	in, ok := pm.Input.Value.(mb.DiceRolledInput)
	if !ok {
		t.Fatalf("input type = %s, want diceRolled", pm.Input.Value.PromptType())
	}
	if in.Sides != 6 {
		t.Fatalf("sides = %d, want 6", in.Sides)
	}
	if len(in.Rolls) != 1 {
		t.Fatalf("rolls = %d, want 1", len(in.Rolls))
	}
	roll := in.Rolls[0]
	if len(roll.NaturalResults) != 2 || roll.NaturalResults[0] != 3 || roll.NaturalResults[1] != 5 {
		t.Fatalf("natural results = %v, want [3 5]", roll.NaturalResults)
	}
	if len(roll.FinalResults) != 2 || roll.FinalResults[0] != 3 || roll.FinalResults[1] != 5 {
		t.Fatalf("final results = %v, want [3 5]", roll.FinalResults)
	}
	if roll.PlayerID != "player-1" {
		t.Fatalf("playerId = %s, want player-1", roll.PlayerID)
	}
	if pm.PromptID != syntheticPromptID(12, 0) {
		t.Fatalf("promptId = %d, want %d (the closing batch Note's Seq)", pm.PromptID, syntheticPromptID(12, 0))
	}

	ack := mb.ClientMessage{Value: mb.ClientResponse{Kind: "response", PromptID: pm.PromptID,
		Action: mb.PromptOutput{Type: "diceRolled", Output: mb.PromptOutputData{Value: mb.DiceRolledAcknowledged{}}}}}
	if err := AcknowledgeSynthetic(ack, pm); err != nil {
		t.Fatalf("AcknowledgeSynthetic(correct ack) = %v, want nil", err)
	}
}

// TestSyntheticDiceDropsAnInterruptedRun: effRollDice never emits anything
// between a roll's per-die Notes and its closing batch Note, so a run that
// does not end in a matching batch Note is dropped rather than guessed at.
func TestSyntheticDiceDropsAnInterruptedRun(t *testing.T) {
	tr := New("table", 2, nil)
	v := view.View{Viewer: 0}
	evs := []protocol.EventBody{
		noteEvent(10, 1, 7, "rolls a d6: 3", nil, false, [][2]uint32{{6, 3}}, 3),
		noteEvent(11, 1, 7, "some unrelated note", nil, false, nil, 0),
		noteEvent(12, 1, 7, "rolls dice: 1 -> 3", nil, false, [][2]uint32{{1, 3}}, 3),
	}
	if msgs := tr.Synthetic(evs, &v); len(msgs) != 0 {
		t.Fatalf("an interrupted roll run must mint nothing, got %#v", msgs)
	}
}

// TestSyntheticNilViewFallsBackToActor covers the v==nil path (a caller that
// has not built a view yet): DecidingPlayerID falls back to the acting
// player the Note itself names.
func TestSyntheticNilViewFallsBackToActor(t *testing.T) {
	tr := New("table", 2, nil)
	evs := []protocol.EventBody{noteEvent(1, 1, 0, "", []uint32{7}, false, nil, 0)}
	msgs := tr.Synthetic(evs, nil)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	pm := msgs[0].Value.(mb.PromptMessage)
	if pm.DecidingPlayerID != "player-1" {
		t.Fatalf("decidingPlayerId = %s, want player-1 (nil view falls back to the actor)", pm.DecidingPlayerID)
	}
}
