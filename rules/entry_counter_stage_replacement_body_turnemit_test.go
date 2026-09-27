package rules

import (
	"maps"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEntryCounterStageReplacementBodyTurnEmit is the out-of-resolution half
// of TestEntryCounterStageReplacementBody: the exact same ReplaceWith$ body
// (a nested token entry carrying an absorbable PutCounter|ETB$ True grant,
// followed by a SubAbility$ rider) driven by a DIRECT e.emit(MoveZone) -- the
// turn-structure / non-resolution emit path -- rather than by casting the
// host. The nested entry still parks its CR 616.1 order ask, but no
// resolution pass owns e.contChain, so the body's rider must be linked onto
// the stage's resume instead of being discarded when resolveReplacementBody
// restores the chain. Before the fix the token enters with the right
// counters and the controller's life stays 20 instead of 22.
func TestEntryCounterStageReplacementBodyTurnEmit(t *testing.T) {
	for _, tc := range []struct {
		name string
		pick int
		want int32
	}{{"scales-first", 0, 4}, {"evolution-first", 1, 3}} {
		t.Run(tc.name, func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			evolution := tokenReplCorpusCard(t, "Branching Evolution")
			spell := card(t, "Name:Body Entry Spell\nManaCost:0\nTypes:Creature\nPT:1/1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ MakeToken | ReplacementResult$ Updated | Description$ make token\n"+
				"SVar:MakeToken:DB$ Token | TokenScript$ body_entry | TokenTapped$ True | SubAbility$ Rider\n"+
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
			// The defect branch: emit the host's entry directly, outside any
			// resolution pass, so resolveReplacementBody's non-resolution arm
			// runs and owns the body's continuation linkage.
			if e.contChainOwners != 0 {
				t.Fatalf("precondition: contChainOwners = %d, want 0 at the direct-emit boundary", e.contChainOwners)
			}
			mintID := e.G.NextID
			e.emit(events.Event{Kind: events.MoveZone, Obj: hostID, From: state.ZHand, To: state.ZBattlefield})

			// Preconditions before any answer: the body fired, the nested
			// mint has NOT been logged, and the rider has not run.
			for _, ev := range e.L.Events {
				if ev.Kind == events.TokenCreate && ev.Text == "body_entry" {
					t.Fatalf("nested entry minted before the order answer: %+v", ev)
				}
			}
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement {
				t.Fatalf("nested replacement-body entry did not park for the order choice: %+v", d)
			}
			if o := e.G.Obj(mintID); o != nil && o.Zone == state.ZBattlefield {
				t.Fatalf("precondition: predicted nested entry already on battlefield: %+v", o)
			}
			if got := e.G.Players[0].Life; got != 20 {
				t.Fatalf("precondition: body rider ran before the answer: life = %d, want 20", got)
			}

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
			if !minted.Tapped || countKind(e.L.Events, events.Tap, mintID) != 1 {
				t.Fatalf("replacement-body token tapped=%v (%d Tap events): the body's DB$ Token rider must land exactly once on the token its answer minted",
					minted.Tapped, countKind(e.L.Events, events.Tap, mintID))
			}
			if got := minted.Counter("P1P1"); got != tc.want {
				t.Fatalf("replacement-body token counters = %d, want %d (the answer must decide 1->2->4 vs 1->2->3)", got, tc.want)
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
