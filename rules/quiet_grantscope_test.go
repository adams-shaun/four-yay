package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// quietGrantBase is quietBaseLandWith with the extras seated in BOTH decks,
// so a fixture can put a grant source on either seat's battlefield.
func quietGrantBase(t *testing.T, reg *cards.Registry, extras []*cards.Card) *Engine {
	t.Helper()
	mountain := lookup(t, reg, "Mountain")
	plains := lookup(t, reg, "Plains")
	mk := func(first *cards.Card) []*cards.Card {
		d := append([]*cards.Card{first}, extras...)
		for len(d) < 40 {
			d = append(d, mountain)
		}
		return d
	}
	e := New(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{mk(plains), mk(mountain)}})
	e.Advance()
	toMain1(t, e)
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZHand, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
	pid := pullByName(t, e, 0, "Plains")
	e.emit(events.Event{Kind: events.MoveZone, Obj: pid, From: state.ZLibrary, To: state.ZBattlefield})
	return e
}

// TestQuietGrantScopePerSeat is the Q3c table: the three board flags that used
// to fire for any grant anywhere (§2.2 flags 1, 3, 5) are per seat. Every row
// builds the SAME board twice, once with the grant source under the opponent
// (seat 1) and once under p (seat 0); the opponent's copy must not block p and
// p's own copy must, so a row passes vacuously only if the flag never fires at
// all -- which the own-seat arm and the board precondition rule out.
func TestQuietGrantScopePerSeat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	rows := []struct {
		name string
		card string
		// own is the blocker when p controls the grant source.
		own quietBlockerID
		// board asserts the grant is live on the board (not a vacuous row).
		board func(t *testing.T, e *Engine)
	}{
		{
			// Flag 1: a tap ability granted to a seat's creatures.
			name: "AddAbility anthem (Enduring Vitality)", card: "Enduring Vitality", own: qbBoardGrant,
			board: func(t *testing.T, e *Engine) {
				if !e.activeSummaryOf(e.active()).hasGrants {
					t.Fatal("precondition: no ability grant is active")
				}
			},
		},
		{
			// Flag 5: a granted Crew (an expanded granted-keyword head).
			name: "AddKeyword Crew grant (Kotori)", card: "Kotori, Pilot Prodigy", own: qbBoardKeyword,
			board: func(t *testing.T, e *Engine) {
				found := false
				for _, h := range e.activeKWHeads {
					if h == "Crew" {
						found = true
					}
				}
				if !found {
					t.Fatalf("precondition: no Crew grant is active (heads %v)", e.activeKWHeads)
				}
			},
		},
		{
			// Flag 3: a MayPlay$ True static is its controller's alone.
			name: "MayPlay static (Omniscience)", card: "Omniscience", own: qbBoardMayPlay,
			board: func(t *testing.T, e *Engine) {
				if e.quietBoardStaticScan(0, true)&qbhMayPlay == 0 && e.quietBoardStaticScan(1, true)&qbhMayPlay == 0 {
					t.Fatal("precondition: no MayPlay$ static is on the board")
				}
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			c := lookup(t, reg, row.card)
			for _, seat := range []state.PlayerID{1, 0} {
				e := quietGrantBase(t, reg, []*cards.Card{c})
				addZone(t, e, seat, c, state.ZBattlefield)
				row.board(t, e)
				got := e.quietBlocker(0)
				want := qbNone
				if seat == 0 {
					want = row.own
				}
				if got != want {
					t.Fatalf("grant under seat %d: quietBlocker(0) = %s, want %s", seat, quietBlockerNames[got], quietBlockerNames[want])
				}
				// The contract: whatever the proof says, the walk agrees.
				if _, quiet, _ := quietContract(t, e, 0); quiet != (want == qbNone) {
					t.Fatalf("grant under seat %d: quiet = %v, want %v", seat, quiet, want == qbNone)
				}
			}
		})
	}
}

// TestQuietSpecClearsSeat pins quietSpecClearsSeat's scope arms, including the
// attached-to arm no corpus fixture reaches cheaply: an Equipment's
// "Creature.EquippedBy" grant reaches only the creature it is attached to.
func TestQuietSpecClearsSeat(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bears := lookup(t, reg, "Grizzly Bears")
	gear := lookup(t, reg, "Bonesplitter")
	e := quietGrantBase(t, reg, []*cards.Card{bears, gear})
	mine := addZone(t, e, 0, bears, state.ZBattlefield)
	theirs := addZone(t, e, 1, bears, state.ZBattlefield)
	equip := addZone(t, e, 0, gear, state.ZBattlefield)
	clears := func(spec string, you state.PlayerID, src state.ObjID, p state.PlayerID) bool {
		return e.quietSpecClearsSeat(spec, you, src, p)
	}
	if !clears("Creature.YouCtrl", 1, equip, 0) || clears("Creature.YouCtrl", 0, equip, 0) {
		t.Error("YouCtrl: only a grantor other than p clears p")
	}
	if clears("Creature.OppCtrl", 1, equip, 0) || !clears("Creature.OppCtrl", 0, equip, 0) {
		t.Error("OppCtrl: only p's own grant clears p")
	}
	if clears("Creature", 1, equip, 0) {
		t.Error("a spec with no control term must not clear")
	}
	if !clears("Card.Self", 0, theirs, 0) || clears("Card.Self", 0, mine, 0) {
		t.Error("Self: clears exactly when the source is not p's")
	}
	// Unattached equipment matches nothing; attached to the opponent's
	// creature it clears p; attached to p's own creature it must not.
	if !clears("Creature.EquippedBy", 0, equip, 0) {
		t.Error("an unattached Equipment's EquippedBy grant matches nothing")
	}
	e.G.Obj(equip).AttachedTo = theirs
	if !clears("Creature.EquippedBy", 0, equip, 0) {
		t.Error("EquippedBy on the opponent's creature must clear p")
	}
	e.G.Obj(equip).AttachedTo = mine
	if clears("Creature.EquippedBy", 0, equip, 0) {
		t.Error("EquippedBy on p's own creature must not clear p")
	}
}
