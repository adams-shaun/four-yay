package rules

import (
	"maps"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestTokenPlanMintIsObservedOnce pins the finalized-plan mint path now that
// it routes through Engine.emit: a doubled creation reports both minted ids
// to EmitTokenCreate's sink, and the ETB watcher queues exactly one trigger
// per plan (the plan's mints are observed once, not once per re-drive).
func TestTokenPlanMintIsObservedOnce(t *testing.T) {
	doubler := card(t, tokenSVarAmountReplSrc())
	token := card(t, "Name:Observed Token\nTypes:Creature\nPT:1/1\nOracle:x\n")
	watcher := card(t, watcherSrc)
	e, cfg := tokenReplGame(t, 992, doubler, watcher)
	cfg.Tokens = maps.Clone(cfg.Tokens)
	cfg.Tokens["observed_token"] = token
	e = New(cfg)
	e.Advance()
	doublerID := moveSeededCard(t, e, 0, doubler, state.ZBattlefield)
	watcherID := moveSeededCard(t, e, 0, watcher, state.ZBattlefield)
	if o := e.G.Obj(doublerID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: token doubler is not on battlefield: %+v", o)
	}
	if o := e.G.Obj(watcherID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: ETB watcher is not on battlefield: %+v", o)
	}

	ids := e.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: 0, Text: "observed_token"})
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("doubled token creation returned %d ids, want two distinct minted ids: %v", len(ids), ids)
	}
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || !o.IsToken || o.Zone != state.ZBattlefield {
			t.Fatalf("reported mint %d is not a battlefield token: %+v", id, o)
		}
	}
	if got := len(e.pendingTriggers); got != 1 {
		t.Fatalf("doubled token plan queued %d ETB triggers, want the single watcher trigger", got)
	}
	replayCheck(t, e, cfg)
}

// TestEntryCounterStageTokenMints is the brief's token-mint clause: a token
// entry whose characteristic-counter grant competes under non-commuting
// AddCounter replacements must stage its CR 616.1 order ask BEFORE the mint
// folds, for a direct TokenCreate, a CardToken copy, AND a finalized token
// plan. The plan case runs TWO same-script mints, because two TokenCreate
// events carry no identity of their own: without a per-mint continuation the
// second mint reads as the first mint's re-drive and vanishes.
//
// Both answers reorder the same non-commuting pair (Hardened Scales' +1 and
// Branching Evolution's doubling), so the final counter count (1 -> 2 -> 4
// against 1 -> 2 -> 3) is an observable function of the answer.
func TestEntryCounterStageTokenMints(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  events.Kind
		plan  bool
		mints int
	}{
		{"token-create-direct", events.TokenCreate, false, 1},
		{"token-create-plan-one", events.TokenCreate, true, 1},
		{"token-create-plan-two", events.TokenCreate, true, 2},
		{"card-token", events.CardToken, false, 1},
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
				var planMintIDs []state.ObjID
				previousSink := e.tokenMintSink
				if tc.plan {
					plan := make([]tokenPlanMint, tc.mints)
					for i := range plan {
						plan[i] = tokenPlanMint{script: "staging_riot"}
					}
					e.tokenMintSink = &planMintIDs
					e.emitTokenPlanMints(events.Event{Kind: events.TokenCreate, Player: 0}, plan)
				} else {
					e.emit(mint)
				}

				// Each mint's own order ask: the first mint stages, and for a
				// two-mint plan the second stages only after the first folded.
				for n := 0; n < tc.mints; n++ {
					d := e.Pending()
					if d == nil || d.Kind != decision.KReplacement {
						t.Fatalf("mint %d: expected staged counter-order ask, got %+v", n, d)
					}
					// No minted token may be visible before its own answer.
					for id := before + state.ObjID(n); id < e.G.NextID; id++ {
						if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield && o.IsToken {
							t.Fatalf("mint %d became visible before its answer: %+v", n, o)
						}
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{answer.pick}}); err != nil {
						t.Fatal(err)
					}
					// Everything already answered is finalized; the next mint
					// is either staged (still pending) or all done.
					first := e.G.Obj(before + state.ObjID(n))
					if first == nil || first.Zone != state.ZBattlefield || first.Counter("P1P1") != answer.want {
						t.Fatalf("mint %d = %+v, want battlefield with %d counters", n, first, answer.want)
					}
				}
				d := e.Pending()
				if d != nil && d.Kind == decision.KReplacement {
					t.Fatalf("unanswered staged ask after %d mints: %+v", tc.mints, d)
				}
				if tc.plan {
					e.tokenMintSink = previousSink
					if len(planMintIDs) != tc.mints {
						t.Fatalf("token plan reported %d minted ids, want exactly %d: %v", len(planMintIDs), tc.mints, planMintIDs)
					}
					for i, id := range planMintIDs {
						if id != before+state.ObjID(i) {
							t.Fatalf("token plan minted ids = %v, want ordered unique ids beginning at %d", planMintIDs, before)
						}
					}
				}

				// Every minted token is on the battlefield with its finalized
				// counters, and each mint carries its own ledger provenance and
				// notice for the object it actually minted.
				for n := 0; n < tc.mints; n++ {
					id := before + state.ObjID(n)
					o := e.G.Obj(id)
					if o == nil || o.Zone != state.ZBattlefield || !o.IsToken || o.Counter("P1P1") != answer.want {
						t.Fatalf("token %d = %+v, want battlefield token with %d counters", id, o, answer.want)
					}
					assertEntryCounterLedger(t, e, id, "P1P1", answer.want, 0)
					notice := false
					for _, ev := range e.L.Events {
						if ev.Kind == events.CounterChange && ev.Text == events.EntryCounterNotice &&
							ev.Counter == "P1P1" && ev.Obj == id {
							notice = true
						}
					}
					if !notice {
						t.Fatalf("token %d: missing entry counter notice for the minted object", id)
					}
				}

				// Atomicity: each entry event carries its finalized counters.
				logged := 0
				for _, ev := range e.L.Events {
					matches := ev.Kind == tc.kind && tc.kind != events.CardToken && ev.Text == "staging_riot"
					matches = matches || ev.Kind == events.CardToken && ev.Obj == source
					if matches {
						logged++
						if len(ev.Pairs) == 0 {
							t.Fatalf("entry event lacks atomic counter pairs: %+v", ev)
						}
					}
				}
				if logged != tc.mints {
					t.Fatalf("logged %d token entry events, want %d (a mint was swallowed)", logged, tc.mints)
				}
				replayCheck(t, e, cfg)
			})
		}
	}
}
