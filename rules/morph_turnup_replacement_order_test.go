package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Skirk Volcanist's Morph cost sacrifices two Mountains. The Mountains are
// the opponent's cards under the turn-up player's control, so under Rest in
// Peace ("a card or token", exile instead) and Leyline of the Void ("a card
// an opponent owns", exile instead) each sacrifice has two replacement
// effects competing for the same move. Each cost event therefore parks on a
// CR 616.1 KReplacement order choice -- not the CR 903.9 commander-zone
// choice the commander test covers. The turn-up must wait for BOTH answers,
// then turn face up exactly once, after both replaced moves, and the game
// must replay from the log.
func TestMorphTurnFaceUpWaitsForSacrificeReplacementOrderChoice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		picks [2]int // the KReplacement option index answered for each sacrifice
	}{
		{"first-option-both", [2]int{0, 0}},
		{"second-option-both", [2]int{1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := searchTestRegistry(t)
			volcanist := searchCorpusCard(t, reg, "Skirk Volcanist")
			rip := searchCorpusCard(t, reg, "Rest in Peace")
			leyline := searchCorpusCard(t, reg, "Leyline of the Void")
			mountain := searchCorpusCard(t, reg, "Mountain")
			forest := searchCorpusCard(t, reg, "Forest")
			decks := [][]*cards.Card{{volcanist, rip, leyline}, {}}
			for len(decks[0]) < 40 {
				decks[0] = append(decks[0], forest)
			}
			for len(decks[1]) < 40 {
				decks[1] = append(decks[1], mountain)
			}
			cfg := seatZeroStart(Config{Seed: 7083, Names: []string{"turn-up", "owner"},
				Decks: decks, Tokens: reg.Tokens})
			e := New(cfg)
			e.Advance()
			toMain1(t, e)
			ripID := searchMoveByNameSeat(t, e, 0, "Rest in Peace", state.ZBattlefield)
			leyID := searchMoveByNameSeat(t, e, 0, "Leyline of the Void", state.ZBattlefield)
			var mountains []state.ObjID
			for _, oid := range e.G.Zone(state.ZLibrary, 1) {
				if len(mountains) < 2 && e.G.Obj(oid).Face().Name == "Mountain" {
					mountains = append(mountains, oid)
				}
			}
			if len(mountains) != 2 {
				t.Fatal("precondition: seat 1's library has fewer than two Mountains")
			}
			for _, m := range mountains {
				e.emit(events.Event{Kind: events.MoveZone, Obj: m, From: state.ZLibrary, To: state.ZBattlefield})
				e.emit(events.Event{Kind: events.ControlChange, Obj: m, Player: 0})
				if o := e.G.Obj(m); o.Zone != state.ZBattlefield || o.Controller != 0 || o.Owner != 1 {
					t.Fatalf("precondition: Mountain %d = %+v, want seat 1's card controlled by seat 0", m, o)
				}
			}
			id := searchMoveByNameSeat(t, e, 0, "Skirk Volcanist", state.ZBattlefield)
			e.emit(events.Event{Kind: events.TurnFaceDown, Obj: id})
			e.emit(events.Event{Kind: events.CastInfo, Obj: id, Counter: events.FlagsString(state.FlagMorphed)})
			mf, ok := morphFaceUpCost(e.G.Obj(id))
			if !ok || len(mf.cost.Sac) != 1 || mf.cost.Sac[0].N != 2 {
				t.Fatalf("precondition: Skirk Volcanist turn-up cost = %+v, want Sac<2/Mountain>", mf.cost)
			}
			for _, m := range mountains {
				if e.commanderZoneReplacementApplies(events.Sacrifice(m)) {
					t.Fatal("precondition: this fixture must not be a commander-zone park")
				}
			}
			e.pending = nil
			e.priorityRound()
			mark := len(e.L.Events)
			submit(t, e, turnFaceUpIndex(t, e, id))
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 || d.Min != 2 {
				t.Fatalf("pending = %+v, want the choose-two-Mountains sacrifice ask", d)
			}
			submit(t, e, 0, 1)
			for i, pick := range tc.picks {
				d := e.Pending()
				if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) < 2 {
					t.Fatalf("answer %d: pending = %+v, want seat 0's replacement order choice", i, d)
				}
				if !e.G.Obj(id).FaceDown || e.turnUp == nil {
					t.Fatalf("answer %d: Skirk Volcanist turned face up (or dropped its payment) before the replacement order answer", i)
				}
				for _, ev := range e.L.Events[mark:] {
					if ev.Kind == events.TurnFaceUp && ev.Obj == id {
						t.Fatalf("answer %d: TurnFaceUp logged before the replacement order answer", i)
					}
				}
				parked := e.replChoices[0].ev.Obj
				if parked != mountains[i] || e.G.Obj(parked).Zone != state.ZBattlefield {
					t.Fatalf("answer %d: parked object %d (zone %v), want Mountain %d still on the battlefield",
						i, parked, e.G.Obj(parked).Zone, mountains[i])
				}
				submit(t, e, pick)
				if got := e.G.Obj(parked).Zone; got != state.ZExile {
					t.Fatalf("answer %d: sacrificed Mountain zone = %v, want exile", i, got)
				}
			}
			if e.G.Obj(id).FaceDown {
				t.Fatal("Skirk Volcanist remained face down after both replacement order answers")
			}
			if e.turnUp != nil {
				t.Fatal("turn-up payment still parked after it completed")
			}
			if e.G.Obj(ripID).Zone != state.ZBattlefield || e.G.Obj(leyID).Zone != state.ZBattlefield {
				t.Fatal("a replacement source left the battlefield")
			}
			lastMove, flipAt, flips, moves := -1, -1, 0, 0
			for i, ev := range e.L.Events[mark:] {
				if ev.Kind == events.MoveZone && ev.From == state.ZBattlefield &&
					(ev.Obj == mountains[0] || ev.Obj == mountains[1]) {
					lastMove = i
					moves++
				}
				if ev.Kind == events.TurnFaceUp && ev.Obj == id {
					flipAt = i
					flips++
				}
			}
			if moves != 2 || flips != 1 {
				t.Fatalf("Mountain battlefield moves = %d, TurnFaceUp events = %d; want 2 and 1", moves, flips)
			}
			if flipAt <= lastMove {
				t.Fatalf("last replaced move index=%d TurnFaceUp index=%d; want both moves first", lastMove, flipAt)
			}
			// The turned-up Volcanist's own TurnFaceUp trigger is the next
			// decision, which proves the flip happened through the real event.
			if d := e.Pending(); d == nil || d.Kind != decision.KTarget || d.Prompt != "Choose a target for Skirk Volcanist" {
				t.Fatalf("pending after the flip = %+v, want Skirk Volcanist's turned-face-up trigger target", d)
			}
			replayCheck(t, e, cfg)
		})
	}
}
