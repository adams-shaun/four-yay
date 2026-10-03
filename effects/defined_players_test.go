package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// definedrem3: `Defined$ Remembered` must contribute remembered PLAYERS only;
// a remembered CARD contributes a seat solely for the
// RememberedController/RememberedOwner spellings. These leaves pin the shared
// helper and each remaining player-selection site against a mixed Remembered
// set -- the RepeatEach shape Summon: Valefor's Sonic Wings chapter produces,
// where iteration 2's Remembered still holds iteration 1's RememberChosen$
// card beside the current opponent.

// mixedRememberedHost builds a 3-seat host whose Ctx.Remembered holds a card
// controlled by seat 1 AND the remembered player seat 2, and returns the Ctx
// plus the card id. It asserts the precondition the whole file relies on: the
// two remembered entries map to DIFFERENT seats, and the card's controller
// (1) is neither the remembered player (2) nor the resolving controller (0).
func mixedRememberedHost(t *testing.T) (*fakeHost, *Ctx, state.ObjID) {
	t.Helper()
	h := newHost(t, 3)
	card := h.g.AddObject(mkCard(t, "Name:Remembered Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	card.Controller = 1
	c := &Ctx{
		Source:     card.ID,
		Controller: 0,
		Remembered: []state.Target{
			{Obj: card.ID},
			{Player: 2, IsPlayer: true},
		},
	}
	if h.g.Obj(c.Remembered[0].Obj) == nil {
		t.Fatal("precondition: remembered card is not in the game")
	}
	if got := PlayerOf(h, c, c.Remembered[0]); got != 1 {
		t.Fatalf("precondition: remembered card maps to seat %d, want 1", got)
	}
	if got := c.Remembered[1].Player; got != 2 {
		t.Fatalf("precondition: remembered player = %d, want 2", got)
	}
	if c.Controller == 1 || c.Controller == 2 || 1 == 2 {
		t.Fatalf("precondition: seats collide (controller %d, card %d, player %d)", c.Controller, 1, 2)
	}
	return h, c, card.ID
}

func wantPlayers(t *testing.T, got, want []state.PlayerID, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, got, want)
		}
	}
}

// TestDefinedPlayersRememberedFamilyIsPlayersOnly pins the helper contract at
// its one structural home: the plain Remembered family is players-only, while
// the RememberedController/Owner spellings keep the remembered card's
// controller/owner.
func TestDefinedPlayersRememberedFamilyIsPlayersOnly(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)

	wantPlayers(t, definedPlayerIDs(h, c, "Remembered"), []state.PlayerID{2},
		"definedPlayerIDs(Remembered)")
	// The card is controlled by 1 and owned by 1; its spelling keeps the
	// card's seat, and the remembered player 2 stays.
	wantPlayers(t, definedPlayerIDs(h, c, "RememberedController"), []state.PlayerID{1, 2},
		"definedPlayerIDs(RememberedController)")
	wantPlayers(t, definedPlayerIDs(h, c, "RememberedOwner"), []state.PlayerID{1, 2},
		"definedPlayerIDs(RememberedOwner)")

	// definedPlayers (the SA-level spelling) agrees with the selector-level one.
	sa := &cards.SA{Params: map[string]string{"Defined": "Remembered"}}
	wantPlayers(t, definedPlayers(h, c, sa), []state.PlayerID{2}, "definedPlayers(Remembered)")
}

// TestSearchPlayersPlainRememberedExcludesCardControllers pins the hidden-zone
// search scope: `Defined$ Remembered` names the remembered player, never the
// remembered card's controller (the leak the scratch probe at c5669fdf showed
// returning [1 2] for the mixed set).
func TestSearchPlayersPlainRememberedExcludesCardControllers(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	sa := &cards.SA{Params: map[string]string{"Defined": "Remembered"}}
	wantPlayers(t, searchPlayers(h, c, sa), []state.PlayerID{2}, "searchPlayers(Remembered)")

	// The ...Controller spelling still reaches the card's controller.
	saCtrl := &cards.SA{Params: map[string]string{"Defined": "RememberedController"}}
	wantPlayers(t, searchPlayers(h, c, saCtrl), []state.PlayerID{1, 2}, "searchPlayers(RememberedController)")
}

// TestActingPlayersPlainRememberedExcludesCardControllers pins the Draw /
// Discard / Mill / Scry / Surveil / Rearrange player walk.
func TestActingPlayersPlainRememberedExcludesCardControllers(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	sa := &cards.SA{Params: map[string]string{"Defined": "Remembered"}}
	wantPlayers(t, actingPlayers(h, c, sa), []state.PlayerID{2}, "actingPlayers(Remembered)")
}

// TestVoteVotersPlainRememberedExcludesCardControllers pins both vote voter
// walks: the fixed-list/card-ballot shape (effVote) and the player-ballot
// shape (effPlayerVote). Each emits exactly one note for the single remembered
// player; before the fix the remembered card's controller joined as a second
// voter.
func TestVoteVotersPlainRememberedExcludesCardControllers(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	effVote(h, c, &cards.SA{Params: map[string]string{
		"Defined": "Remembered", "Choices": "Alpha,Beta",
	}})
	if n := countNotes(h, "votes for"); n != 1 {
		t.Fatalf("effVote voters = %d notes, want 1 (only remembered player 2)", n)
	}

	h2, c2, _ := mixedRememberedHost(t)
	pv := &cards.SA{Params: map[string]string{
		"Defined": "Remembered", "VotePlayer": "Player",
	}}
	effPlayerVote(h2, c2, pv, VoteOf(pv))
	if n := countNotes(h2, "player vote resolved"); n != 1 {
		t.Fatalf("effPlayerVote voters = %d notes, want 1 (only remembered player 2)", n)
	}
}

// countNotes counts emitted Notes whose text has the given prefix.
func countNotes(h *fakeHost, prefix string) int {
	n := 0
	for _, e := range h.log {
		if e.Kind == events.Note && strings.HasPrefix(e.Text, prefix) {
			n++
		}
	}
	return n
}

// TestChangeTargetChooserPlainRememberedExcludesCardControllers pins
// `Chooser$ <sel>`: the redirect chooser must be the remembered player, not
// the remembered card's controller.
func TestChangeTargetChooserPlainRememberedExcludesCardControllers(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	sa := &cards.SA{Params: map[string]string{"Chooser": "Remembered"}}
	if got := changeTargetChooser(h, c, sa); got != 2 {
		t.Fatalf("changeTargetChooser(Chooser$ Remembered) = %d, want 2", got)
	}
	saCtrl := &cards.SA{Params: map[string]string{"Chooser": "RememberedController"}}
	if got := changeTargetChooser(h, c, saCtrl); got != 1 {
		t.Fatalf("changeTargetChooser(Chooser$ RememberedController) = %d, want 1", got)
	}
}

// TestManaRecipientsPlainRememberedExcludesCardControllers pins the Mana SA's
// recipient walk.
func TestManaRecipientsPlainRememberedExcludesCardControllers(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	sa := &cards.SA{Params: map[string]string{"Defined": "Remembered"}}
	wantPlayers(t, ManaRecipients(h, c, sa), []state.PlayerID{2}, "ManaRecipients(Remembered)")
}

// TestHandMoveOwnersPlainRememberedKeepsRememberedPlayer pins the hidden-hand
// owner walk: a remembered PLAYER is a legitimate owner and must survive the
// set even when a remembered card coexists (previously the fail-closed guard
// dropped it).
func TestHandMoveOwnersPlainRememberedKeepsRememberedPlayer(t *testing.T) {
	h, c, _ := mixedRememberedHost(t)
	sa := &cards.SA{Params: map[string]string{"Defined": "Remembered"}}
	owners, ok := handMoveOwners(h, c, sa, ChangeZoneOf(sa))
	if !ok {
		t.Fatal("handMoveOwners(Remembered) failed closed, want the remembered player")
	}
	wantPlayers(t, owners, []state.PlayerID{2}, "handMoveOwners(Remembered)")
}

// TestRememberedControllerOwnerControlReferents pins the two control referents
// definedrem3 added (craterous_stomp / public_execution / winnowing carry
// `ControlledBy RememberedController`): the remembered card's controller's
// creature matches and nothing else, resolution-only.
func TestRememberedControllerOwnerControlReferents(t *testing.T) {
	h := newHost(t, 3)
	g := h.g
	remembered := g.AddObject(mkCard(t, "Name:Remembered Relic\nTypes:Artifact\nOracle:x\n"), 2)
	remembered.Controller = 1 // owned by 2, controlled by 1
	g.SetZone(state.ZBattlefield, 1, []state.ObjID{remembered.ID})

	// mine is controlled by 1 (the remembered card's controller); theirs by 2.
	mine := g.AddObject(mkCard(t, "Name:Mine\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	theirs := g.AddObject(mkCard(t, "Name:Theirs\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 2)
	g.SetZone(state.ZBattlefield, 1, []state.ObjID{remembered.ID, mine.ID})
	g.SetZone(state.ZBattlefield, 2, []state.ObjID{theirs.ID})

	if g.Obj(mine.ID).Controller == g.Obj(theirs.ID).Controller {
		t.Fatalf("precondition: the two creatures share a controller (%d)", g.Obj(mine.ID).Controller)
	}
	if g.Obj(remembered.ID).Controller != g.Obj(mine.ID).Controller {
		t.Fatalf("precondition: remembered card's controller %d != mine's %d",
			g.Obj(remembered.ID).Controller, g.Obj(mine.ID).Controller)
	}

	sc := SpecContext{You: 0, Remembered: []state.Target{{Obj: remembered.ID}}, Resolving: true}
	if !MatchesSpecCtx(g, "Creature.ControlledBy RememberedController", mine.ID, sc) {
		t.Fatal("the remembered card's controller's creature did not match ControlledBy RememberedController")
	}
	if MatchesSpecCtx(g, "Creature.ControlledBy RememberedController", theirs.ID, sc) {
		t.Fatal("another controller's creature matched ControlledBy RememberedController")
	}
	// Offer-time (Resolving false) fails closed.
	if MatchesSpecCtx(g, "Creature.ControlledBy RememberedController", mine.ID,
		SpecContext{You: 0, Remembered: []state.Target{{Obj: remembered.ID}}}) {
		t.Fatal("an offer-time (unresolved) ControlledBy RememberedController matched")
	}

	// OwnedBy RememberedOwner: the remembered card's OWNER (2) names its
	// permanents -- theirs, not mine.
	scOwner := SpecContext{You: 0, Remembered: []state.Target{{Obj: remembered.ID}}, Resolving: true}
	if !MatchesSpecCtx(g, "Creature.OwnedBy RememberedOwner", theirs.ID, scOwner) {
		t.Fatal("the remembered card's owner's creature did not match OwnedBy RememberedOwner")
	}
	if MatchesSpecCtx(g, "Creature.OwnedBy RememberedOwner", mine.ID, scOwner) {
		t.Fatal("another owner's creature matched OwnedBy RememberedOwner")
	}
	if MatchesSpecCtx(g, "Creature.OwnedBy RememberedOwner", theirs.ID,
		SpecContext{You: 0, Remembered: []state.Target{{Obj: remembered.ID}}}) {
		t.Fatal("an offer-time (unresolved) OwnedBy RememberedOwner matched")
	}

	// The referent is recognised by the grammar census for both spellings.
	for _, spec := range []string{"Creature.ControlledBy RememberedController", "Creature.OwnedBy RememberedOwner"} {
		if u := UnknownPredicates(spec); len(u) != 0 {
			t.Fatalf("UnknownPredicates(%q) = %v, want none", spec, u)
		}
	}
	// The bare Remembered referent stays players-only.
	scPlayer := SpecContext{You: 0, Remembered: []state.Target{
		{Obj: remembered.ID}, {Player: 1, IsPlayer: true},
	}, Resolving: true}
	if !MatchesSpecCtx(g, "Creature.ControlledBy Remembered", mine.ID, scPlayer) {
		t.Fatal("the remembered PLAYER's creature did not match the bare ControlledBy Remembered")
	}
	if MatchesSpecCtx(g, "Creature.ControlledBy Remembered", theirs.ID, scPlayer) {
		t.Fatal("a non-remembered player's creature matched ControlledBy Remembered")
	}
}
