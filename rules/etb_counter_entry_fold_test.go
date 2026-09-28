package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 614.12: a permanent's "enters the battlefield with N counters" ability
// (K:etbCounter, and every other bare DB$ PutCounter | ETB$ True Updated
// replacement body) is a replacement effect whose counter placement is part
// of the ENTRY. Before this ticket rules/replacement.go folded the entry
// MoveZone first and ran the PutCounter body after it, so the counters were
// absent from the entry's own fold: a log-only replay, an ETB trigger and
// the state-based actions saw the permanent enter without them, and a
// body placement whose AddCounter competition needed CR 616.1's order answer
// was not even placed until after the entry had happened. rules/
// entry_counters.go now preflights those bodies with the move (the same
// continuation the intrinsic loyalty/Riot/Unleash/lore/defense grants use):
// the placement is settled through the AddCounter replacement class against
// the post-entry preview, the move folds with the finalized amounts in its
// Pairs payload, and the body itself is not run a second time.
//
// Every card source here is authored inline (no corpus .txt is committed,
// per the licensing rule) and carries a REAL K:etbCounter line, so the real
// cards/kw_etbcounter.go expansion produces the body under test.

// etbCounterEntryReader is the brief's observer: a creature that enters with
// two +1/+1 counters (K:etbCounter:P1P1:2) and whose ETB trigger reads its own
// +1/+1 counters, drawing one card per counter. The log then records exactly
// what an observer saw at entry, and the assertion is on the log, not on a
// re-read of the board.
func etbCounterEntryReader(t testing.TB) *cards.Card {
	return card(t, "Name:Entry Reader\nTypes:Creature Bear\nPT:2/2\n"+
		"K:etbCounter:P1P1:2:no Condition:CARDNAME enters with two +1/+1 counters on it.\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When CARDNAME enters, draw a card for each +1/+1 counter on it.\n"+
		"SVar:TrigDraw:DB$ Draw | NumCards$ Count$CardCounters.P1P1\n"+
		"Oracle:x\n")
}

// etbCounterEntryDraws counts the draw events seat p logged.
func etbCounterEntryDraws(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// etbCounterEntryMove returns the object's entry MoveZone from the log.
func etbCounterEntryMove(e *Engine, id state.ObjID) (events.Event, bool) {
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
			return ev, true
		}
	}
	return events.Event{}, false
}

// etbCounterEntryTriggered reports whether the object's ETB trigger was
// pushed onto the stack (so a "drew nothing" assertion cannot pass with the
// whole trigger unregistered).
func etbCounterEntryTriggered(e *Engine, id state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == id {
			return true
		}
	}
	return false
}

// etbCounterEntryAnswerOrder submits the pending CR 616.1 AddCounter order
// ask, choosing the option that names want. It also asserts the ENTRY has not
// folded while the ask is outstanding -- the atomicity this ticket exists for.
func etbCounterEntryAnswerOrder(t *testing.T, e *Engine, id state.ObjID, want string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement {
		t.Fatalf("order ask = %+v, want KReplacement", d)
	}
	if _, folded := etbCounterEntryMove(e, id); folded {
		t.Fatal("entry folded before the CR 616.1 order answer")
	}
	idx := -1
	for _, o := range d.Options {
		if len(o.Label) >= len(want) && entryLabelHas(o.Label, want) {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no order option naming %q among %+v", want, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit order answer: %v", err)
	}
}

func entryLabelHas(label, want string) bool {
	for i := 0; i+len(want) <= len(label); i++ {
		if label[i:i+len(want)] == want {
			return true
		}
	}
	return false
}

// etbCounterEntryChargeReader is a real K:etbCounter carrier whose kind
// (CHARGE) is not one of the fixed-index table kinds, plus the brief's ETB
// observer: it draws one card per charge counter on it, so the log records
// exactly what an ETB observer saw at entry. The historic Pairs tag could
// not encode CHARGE, so this entry used to stay on the body path. The general
// payload form (events.EntryCounterPairs) folds it like any P1P1 grant.
func etbCounterEntryChargeReader(t testing.TB) *cards.Card {
	return card(t, "Name:Entry Charge\nTypes:Artifact\n"+
		"K:etbCounter:CHARGE:2:no Condition:CARDNAME enters with two charge counters on it.\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ When CARDNAME enters, draw a card for each charge counter on it.\n"+
		"SVar:TrigDraw:DB$ Draw | NumCards$ Count$CardCounters.CHARGE\n"+
		"Oracle:x\n")
}

// TestEtbCounterNonP1P1KindFoldsIntoMove is the consolidated acceptance's
// non-P1P1 half: a real K:etbCounter:CHARGE carrier's counters are present in
// the entry MoveZone fold, so its ETB observer sees them and the log replays.
// The competing pair (Winding Constrictor, Doubling Season) replaces ANY
// counter kind on an artifact or permanent, so unlike the P1P1 +1/+1 pair it
// actually contests a CHARGE placement and parks the CR 616.1 order ask: the
// entry must not fold, and the counters must still be in the move once the
// answer finalizes the amount.
func TestEtbCounterNonP1P1KindFoldsIntoMove(t *testing.T) {
	t.Parallel()
	constrictor := tokenReplCorpusCard(t, "Winding Constrictor")
	season := tokenReplCorpusCard(t, "Doubling Season")
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")

	type tc struct {
		name  string
		board []*cards.Card
		pick  string // CR 616.1 order option to submit ("" = none expected)
		want  int32  // final CHARGE counters, and what the ETB observer must draw
	}
	for _, c := range []tc{
		{name: "plain", want: 2},
		{name: "hardened-scales-and-branching-evolution", board: []*cards.Card{scales, evolution}, want: 2},
		{name: "winding-first", board: []*cards.Card{constrictor, season}, pick: "Winding Constrictor", want: 6},
		{name: "season-first", board: []*cards.Card{constrictor, season}, pick: "Doubling Season", want: 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			cre := etbCounterEntryChargeReader(t)
			deck := append(append([]*cards.Card{}, c.board...), cre)
			e, cfg := tokenReplGame(t, 431, deck...)
			for _, b := range c.board {
				moveSeededCard(t, e, 0, b, state.ZBattlefield)
			}
			for _, b := range c.board {
				name := b.Faces[0].Name
				found := false
				for _, id := range e.G.Zone(state.ZBattlefield, 0) {
					if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
						found = true
					}
				}
				if !found {
					t.Fatalf("precondition: %s not on the battlefield", name)
				}
			}
			e.SetCounterAdder(0)
			cid := moveSeededCard(t, e, 0, cre, state.ZHand)
			if o := e.G.Obj(cid); o == nil || o.Zone != state.ZHand {
				t.Fatal("precondition: artifact not in hand")
			}
			before := etbCounterEntryDraws(e, 0)
			e.emit(events.Event{Kind: events.MoveZone, Obj: cid, From: state.ZHand, To: state.ZBattlefield})
			if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
				if c.pick == "" {
					t.Fatalf("unexpected order ask: %+v", d)
				}
				etbCounterEntryAnswerOrder(t, e, cid, c.pick)
			} else if c.pick != "" {
				t.Fatalf("expected a CR 616.1 order ask naming %q, got %+v", c.pick, d)
			}
			e.Advance()
			passUntilStackEmpty(t, e, 20)

			o := e.G.Obj(cid)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: artifact not on the battlefield: %+v", o)
			}
			mv, ok := etbCounterEntryMove(e, cid)
			if !ok {
				t.Fatal("precondition: no entry MoveZone in the log")
			}
			if got := o.Counter("CHARGE"); got != c.want {
				t.Fatalf("entry CHARGE counters = %d, want %d", got, c.want)
			}
			// The counters must be INSTALLED by the entry move's Pairs payload,
			// not by a later real CounterChange.
			if c.want > 0 && len(mv.Pairs) == 0 {
				t.Fatalf("entry move carries no CHARGE counter grant: %+v", mv)
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.CounterChange && ev.Obj == cid && ev.Counter == "CHARGE" &&
					ev.Amount > 0 && ev.Text != events.EntryCounterNotice {
					t.Fatalf("CHARGE entry counter placed as a real event after the move: %+v", ev)
				}
			}
			if !etbCounterEntryTriggered(e, cid) {
				t.Fatal("precondition: the ETB trigger never fired")
			}
			if got := etbCounterEntryDraws(e, 0) - before; got != int(c.want) {
				t.Fatalf("ETB trigger drew %d, want %d (it must see the entry CHARGE counters)", got, c.want)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// An object enters with counters from a real K:etbCounter line; those counters
// are present in the
// entry MoveZone fold, so an ETB trigger reading them sees them and the log
// replays. It runs the plain case, the commuting-modifier case, the
// non-commuting CR 616.1 order choice (the body suspended behind the ask,
// both answers), and the CantPutCounter (Solemnity) case.
func TestEtbCounterEntryFoldsIntoMove(t *testing.T) {
	t.Parallel()
	scales := tokenReplCorpusCard(t, "Hardened Scales")
	evolution := tokenReplCorpusCard(t, "Branching Evolution")
	solemnity := tokenReplCorpusCard(t, "Solemnity")

	type tc struct {
		name      string
		board     []*cards.Card // permanents moved to the battlefield first
		pick      string        // order option to submit ("" = none expected)
		want      int32
		triggered bool
	}
	for _, c := range []tc{
		{name: "plain", board: nil, want: 2, triggered: true},
		{name: "hardened-scales", board: []*cards.Card{scales}, want: 3, triggered: true},
		{name: "scales-first", board: []*cards.Card{scales, evolution}, pick: "Hardened Scales", want: 6, triggered: true},
		{name: "evolution-first", board: []*cards.Card{scales, evolution}, pick: "Branching Evolution", want: 5, triggered: true},
		{name: "solemnity-blocks", board: []*cards.Card{solemnity}, want: 0, triggered: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			cre := etbCounterEntryReader(t)
			deck := append(append([]*cards.Card{}, c.board...), cre)
			e, cfg := tokenReplGame(t, 411, deck...)
			for _, b := range c.board {
				moveSeededCard(t, e, 0, b, state.ZBattlefield)
			}
			for _, b := range c.board {
				name := b.Faces[0].Name
				found := false
				for _, id := range e.G.Zone(state.ZBattlefield, 0) {
					if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
						found = true
					}
				}
				if !found {
					t.Fatalf("precondition: %s not on the battlefield", name)
				}
			}
			e.SetCounterAdder(0)
			cid := moveSeededCard(t, e, 0, cre, state.ZHand)
			if o := e.G.Obj(cid); o == nil || o.Zone != state.ZHand {
				t.Fatal("precondition: creature not in hand")
			}
			before := etbCounterEntryDraws(e, 0)
			e.emit(events.Event{Kind: events.MoveZone, Obj: cid, From: state.ZHand, To: state.ZBattlefield})
			if d := e.Pending(); d != nil && d.Kind == decision.KReplacement {
				if c.pick == "" {
					t.Fatalf("unexpected order ask: %+v", d)
				}
				etbCounterEntryAnswerOrder(t, e, cid, c.pick)
			} else if c.pick != "" {
				t.Fatalf("expected a CR 616.1 order ask naming %q, got %+v", c.pick, d)
			}
			e.Advance()
			passUntilStackEmpty(t, e, 20)

			o := e.G.Obj(cid)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: creature not on the battlefield: %+v", o)
			}
			mv, ok := etbCounterEntryMove(e, cid)
			if !ok {
				t.Fatal("precondition: no entry MoveZone in the log")
			}
			if got := o.Counter("P1P1"); got != c.want {
				t.Fatalf("entry counters = %d, want %d", got, c.want)
			}
			// The counter that the observer must see is folded into the entry
			// move's Pairs payload -- except a fully blocked grant (Solemnity),
			// which places nothing and so carries no pair.
			if c.want > 0 && len(mv.Pairs) == 0 {
				t.Fatalf("entry move carries no counter grant: %+v", mv)
			}
			// No REAL CounterChange may place the entry counter after the
			// move: the only allowed record is the notification-only marker.
			for _, ev := range e.L.Events {
				if ev.Kind == events.CounterChange && ev.Obj == cid && ev.Amount > 0 &&
					ev.Text != events.EntryCounterNotice {
					t.Fatalf("entry counter placed as a real event after the move: %+v", ev)
				}
			}
			if !etbCounterEntryTriggered(e, cid) {
				t.Fatal("precondition: the ETB trigger never fired")
			}
			if got := etbCounterEntryDraws(e, 0) - before; got != int(c.want) {
				t.Fatalf("ETB trigger drew %d, want %d (it must see the entry counters)", got, c.want)
			}
			replayCheck(t, e, cfg)
		})
	}
}
