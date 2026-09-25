package rules

import (
	"maps"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestEntryCounterStageTokenMints(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind events.Kind
		plan bool
	}{
		{"token-create-direct", events.TokenCreate, false},
		{"token-create-plan", events.TokenCreate, true},
		{"card-token", events.CardToken, false},
	} {
		for _, answer := range []struct {
			pick int
			want int32
		}{{0, 4}, {1, 3}} {
			t.Run(tc.name+map[int]string{0: "/scales-first", 1: "/evolution-first"}[answer.pick], func(t *testing.T) {
				scales := tokenReplCorpusCard(t, "Hardened Scales")
				evolution := tokenReplCorpusCard(t, "Branching Evolution")
				entrant := card(t, "Name:Staging Entry\nTypes:Creature Vampire\nPT:1/1\nR:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBEntry | ReplacementResult$ Updated | Description$ entry counter\nSVar:DBEntry:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
				e, cfg := tokenReplGame(t, 901, scales, evolution, entrant)
				cfg.Tokens = maps.Clone(cfg.Tokens)
				cfg.Tokens["staging_riot"] = entrant
				e = New(cfg)
				e.Advance()
				for _, c := range []*cards.Card{scales, evolution} {
					id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
					if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
						t.Fatal("precondition: counter modifier absent")
					}
				}
				e.SetCounterAdder(0)
				var source state.ObjID
				mint := events.Event{Kind: tc.kind, Player: 0, Text: "staging_riot"}
				if tc.kind == events.CardToken {
					source = seededEntryID(t, e, entrant)
					if o := e.G.Obj(source); o == nil || o.Zone == state.ZBattlefield {
						t.Fatal("precondition: copied card must be outside battlefield")
					}
					mint.Obj = source
				}
				before := e.G.NextID
				if tc.plan {
					e.emitTokenPlanMints(events.Event{Kind: events.TokenCreate, Player: 0}, []tokenPlanMint{{script: "staging_riot"}})
				} else {
					e.emit(mint)
				}
				d := e.Pending()
				if d == nil || d.Kind != decision.KReplacement {
					t.Fatalf("expected staged counter-order ask, got %+v", d)
				}
				for id := before; id < e.G.NextID; id++ {
					if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield && o.IsToken {
						t.Fatalf("mint became visible before answer: %+v", o)
					}
				}
				for _, ev := range e.L.Events {
					if ev.Kind == tc.kind && (tc.kind != events.CardToken || ev.Obj == source) {
						t.Fatalf("mint event logged before answer: %+v", ev)
					}
				}
				if answer.pick != 0 { // choose Evolution first
					// Option ordering is deterministic; index one selects Evolution.
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{answer.pick}}); err != nil {
					t.Fatal(err)
				}
				if tc.plan {
					for id := before; id < e.G.NextID; id++ {
						o := e.G.Obj(id)
						if o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != answer.want {
							t.Fatalf("planned token %d = %+v, want %d counters", id, o, answer.want)
						}
					}
				} else {
					o := e.G.Obj(before)
					if o == nil || o.Zone != state.ZBattlefield || o.Counter("P1P1") != answer.want {
						t.Fatalf("minted object = %+v, want %d counters", o, answer.want)
					}
					foundNotice := false
					for _, ev := range e.L.Events {
						if ev.Kind == events.CounterChange && ev.Text == events.EntryCounterNotice && ev.Counter == "P1P1" {
							if ev.Obj != before {
								t.Fatalf("notice Obj %d, want minted id %d", ev.Obj, before)
							}
							foundNotice = true
						}
					}
					if !foundNotice {
						t.Fatal("missing entry counter notice for minted object")
					}
				}
				entryLogged := false
				for _, logged := range e.L.Events {
					matches := logged.Kind == tc.kind && logged.Kind != events.CardToken && logged.Text == "staging_riot"
					matches = matches || logged.Kind == events.CardToken && logged.Obj == source
					if matches {
						entryLogged = true
						if len(logged.Pairs) == 0 {
							t.Fatalf("entry event lacks atomic counter pairs: %+v", logged)
						}
					}
				}
				if !entryLogged {
					t.Fatal("precondition: finalized token entry event missing")
				}
				replayCheck(t, e, cfg)
			})
		}
	}
}
