package rules

import (
	"maps"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEntryCounterStageReplacementBody is the brief's replacement-body clause:
// a real ReplaceWith$ body emits a nested battlefield ENTRY while the
// replacement is resolving (applyingReplacement), and that entry carries a
// non-commuting entry-counter grant. The CR 616.1 order ask must park BEFORE
// the nested entry folds -- no observer may see the un-replaced permanent --
// and the body's own SubAbility$ rider must resume exactly once after the
// answer. The host's entry is Updated (the corpus-dominant shape), so the
// original action still happens; it must happen exactly once and never recur.
//
// The two answers reorder the same non-commuting pair (Hardened Scales' +1
// and Branching Evolution's doubling), so the final counter count is an
// observable function of the answer: 1 -> 2 -> 4 against 1 -> 2 -> 3.
func TestEntryCounterStageReplacementBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		pick int
		want int32
	}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			evolution := tokenReplCorpusCard(t, "Branching Evolution")
			// The host permanent's Moved replacement runs a body that CREATES
			// a token -- a nested entry emitted from inside the replacement --
			// and then a SubAbility$ rider. Updated keeps the original host
			// entry, so the host really enters and the body runs on top of it.
			spell := card(t, "Name:Body Entry Spell\nManaCost:0\nTypes:Creature\nPT:1/1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ MakeToken | ReplacementResult$ Updated | Description$ make token\n"+
				"SVar:MakeToken:DB$ Token | TokenScript$ body_entry | SubAbility$ Rider\n"+
				"SVar:Rider:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
			token := card(t, "Name:Body Entry Token\nTypes:Creature\nPT:1/1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ AddEntry | ReplacementResult$ Updated | Description$ entry counter\n"+
				"SVar:AddEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
			e, cfg := tokenReplGame(t, 991, scales, evolution, spell)
			cfg.Tokens = maps.Clone(cfg.Tokens)
			cfg.Tokens["body_entry"] = token
			e = New(cfg)
			e.Advance()
			for _, c := range []*cards.Card{scales, evolution} {
				id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatal("precondition: counter modifier absent")
				}
			}
			e.SetCounterAdder(0)
			hostID := moveSeededCard(t, e, 0, spell, state.ZHand)
			if o := e.G.Obj(hostID); o == nil || o.Zone != state.ZHand {
				t.Fatalf("precondition: host not in hand: %+v", o)
			}
			toMain1(t, e)
			e.priorityRound()
			castSpellOption(t, e, "Body Entry Spell")
			// The nested token is minted at the next fresh id; the host entry
			// reuses the spell's own id (an Updated move) and nothing else
			// allocates before the nested fold.
			mintID := e.G.NextID
			// Resolve the spell. The nested replacement-body entry parks its
			// order ask mid-resolution, so pass priority until the ask -- or
			// fail if the resolution ended without one.
			for i := 0; i < 8; i++ {
				d := e.Pending()
				if d == nil {
					break
				}
				if d.Kind == decision.KReplacement {
					break
				}
				if d.Kind != decision.KPriority {
					t.Fatalf("unexpected decision while resolving the body: %+v", d)
				}
				passPriorityOnce(t, e)
			}
			if got := e.G.NextID; got != mintID {
				t.Fatalf("precondition: another object allocated before the nested entry: NextID = %d, want %d", got, mintID)
			}

			// Precondition: the body actually fired and its nested entry
			// competes, so the ask we assert below is the nested entry's
			// order, not the host's own entry.
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement {
				t.Fatalf("nested replacement-body entry did not park for the order choice: %+v", d)
			}
			if o := e.G.Obj(mintID); o != nil && o.Zone == state.ZBattlefield {
				t.Fatalf("precondition: predicted nested entry already on battlefield: %+v", o)
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.TokenCreate && ev.Text == "body_entry" {
					t.Fatalf("nested entry minted before the order answer: %+v", ev)
				}
			}
			if got := e.G.Players[0].Life; got != 20 {
				t.Fatalf("precondition: body rider ran before the answer: life = %d, want 20", got)
			}

			// Answer the nested entry's order ask (and any live remainder).
			asks := 0
			for d != nil && d.Kind == decision.KReplacement {
				asks++
				if asks > 3 {
					t.Fatalf("too many replacement asks while resuming the body: %+v", d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tc.pick}}); err != nil {
					t.Fatal(err)
				}
				d = e.Pending()
			}
			if asks == 0 {
				t.Fatal("precondition: no order ask was answered")
			}

			minted := e.G.Obj(mintID)
			if minted == nil || minted.Zone != state.ZBattlefield {
				t.Fatalf("replacement-body token = %+v, want a battlefield permanent", minted)
			}
			if got := minted.Counter("P1P1"); got != tc.want {
				t.Fatalf("replacement-body token counters = %d, want %d (the answer must decide 1->2->4 vs 1->2->3)", got, tc.want)
			}
			// Provenance: the settled entry notice names the MINTED object and
			// carries the entrant's controller as adder in the ledger.
			notice := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.CounterChange && ev.Text == events.EntryCounterNotice && ev.Counter == "P1P1" {
					if ev.Obj != mintID {
						t.Fatalf("entry notice Obj = %d, want the minted id %d", ev.Obj, mintID)
					}
					notice = true
				}
				if ev.Kind == events.CounterChange && ev.Obj == mintID && ev.Counter == "P1P1" &&
					ev.Text != events.EntryCounterNotice {
					t.Fatalf("entry counter placed as a real event after the mint: %+v", ev)
				}
			}
			if !notice {
				t.Fatal("missing entry counter notice for the replacement-body mint")
			}
			assertEntryCounterLedger(t, e, mintID, "P1P1", tc.want, 0)

			// Atomicity: the nested TokenCreate carries the finalized counters
			// in its Pairs payload.
			entry := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.TokenCreate && ev.Text == "body_entry" {
					entry = true
					if len(ev.Pairs) == 0 {
						t.Fatalf("nested token entry not atomic: %+v", ev)
					}
				}
			}
			if !entry {
				t.Fatal("replacement-body token entry was not logged")
			}

			// The body's rider fires exactly once, after the answer.
			if got := e.G.Players[0].Life; got != 22 {
				t.Fatalf("replacement body's SubAbility rider life = %d, want 22 exactly once", got)
			}
			// The original action (the host's own entry) happens exactly once
			// and never recurs.
			if o := e.G.Obj(hostID); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("original host entry = %+v, want a battlefield permanent", o)
			}
			if n := countMoves(e.L.Events, hostID, state.ZBattlefield); n != 1 {
				t.Fatalf("original host entry recurred: %d battlefield moves, want 1", n)
			}
			replayCheck(t, e, cfg)
		})
	}
}
