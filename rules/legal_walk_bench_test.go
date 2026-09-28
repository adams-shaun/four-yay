package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// legalWalkBenchPairs are the botbench pairs the priority offer walk's cost
// was measured on (uw-tempo against five mono-colour decks).
var legalWalkBenchPairs = [][2]string{
	{"uw-tempo", "mono-white-equipment"},
	{"uw-tempo", "mono-blue-tempo"},
	{"uw-tempo", "mono-black-aggro"},
	{"uw-tempo", "mono-red-prowess"},
	{"uw-tempo", "mono-green-stompy"},
}

// legalWalkPositions plays one bot-vs-bot game per bench pair and snapshots
// (Clone) every third priority decision: realistic mid-game positions for
// the offer walk, from opening hands to late boards.
func legalWalkPositions(tb testing.TB) []*Engine {
	tb.Helper()
	reg := testutil.CorpusRegistry(tb)
	var out []*Engine
	for i, pr := range legalWalkBenchPairs {
		cfg := Config{Seed: 90000000 + uint64(i), Names: pr[:],
			Decks:  [][]*cards.Card{testutil.RepoDeck(tb, reg, pr[0]), testutil.RepoDeck(tb, reg, pr[1])},
			Tokens: reg.Tokens}
		e := New(cfg)
		b := newTestBot(uint64(7000 + i))
		e.Advance()
		k := 0
		for n := 0; !e.G.Over && e.Pending() != nil && n < 3000; n++ {
			d := e.Pending()
			if d.Kind == decision.KPriority {
				if k%3 == 0 {
					out = append(out, e.Clone())
				}
				k++
			}
			if err := e.Submit(b.answer(e, d)); err != nil {
				tb.Fatalf("%v intent %d: %v", pr, n, err)
			}
		}
	}
	if len(out) < 100 {
		tb.Fatalf("precondition: %d bench positions, want >= 100", len(out))
	}
	return out
}

// legalWalkVerifyOff turns the rules test binary's verify modes off (each
// recomputes a cache hit, which a timing would charge to the production
// path) and returns the restore.
func legalWalkVerifyOff() func() {
	flags := []*bool{&derivedMemoVerify, &sacrificeCardnameVerify, &pricedCandidatesVerify,
		&castsOnlyWalkVerify, &potentialMembersVerify, &walkCacheVerify, &trigZoneSkipVerify,
		&manaPayFastVerify, &activeSummaryVerify, &manaSAFactsVerify, &faceScanVerify,
		&layer4PrecheckVerify, &layerInertVerify, &livelockCandVerify, &priorityFlowVerify,
		&replZoneSkipVerify, &sbaQuietVerify, &provenanceGateVerify, &specDerivedVerify, &staticZoneSkipVerify}
	prev := make([]bool, len(flags))
	for i, f := range flags {
		prev[i], *f = *f, false
	}
	return func() {
		for i, f := range flags {
			*f = prev[i]
		}
	}
}

// BenchmarkLegalActionsRealGames times one priority offer walk per snapshot
// position (b.N counts full sweeps over every position). "warm" repeats the
// walk at an unchanged board, so the cross-walk Derived memo serves (the
// shape of a pass answered by the next seat); "cold" retires that memo
// first, the shape of the first walk after a state change; "clone" walks a
// fresh Clone of the position (Clone itself untimed), the search shape.
func BenchmarkLegalActionsRealGames(b *testing.B) {
	pos := legalWalkPositions(b)
	defer legalWalkVerifyOff()()
	for _, mode := range []string{"warm", "cold", "clone"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, e := range pos {
					switch mode {
					case "cold":
						e.retireCrossWalkMemo()
					case "clone":
						// The search shape: a fresh Clone's first walk (its memo
						// tables and walk caches start empty).
						b.StopTimer()
						e = e.Clone()
						b.StartTimer()
					}
					e.legalActions(e.Pending().Player)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(pos)), "ns/walk")
		})
	}
}

// TestLegalWalkDigest is an opt-in equivalence harness for performance work
// on the offer walk: with LEGAL_WALK_DIGEST=<file> it plays bot games over
// every repo deck (two-seat round robin plus four-seat and Commander games)
// and writes, per game, a digest of every priority decision's Options, the
// seat's PotentialActions and (on a clone) its PaymentActions. Two builds
// whose files are byte-identical offered the same surfaces at every stop.
func TestLegalWalkDigest(t *testing.T) {
	t.Parallel()
	path := os.Getenv("LEGAL_WALK_DIGEST")
	if path == "" {
		t.Skip("set LEGAL_WALK_DIGEST=<file> to write the offer-walk digest")
	}
	reg := testutil.CorpusRegistry(t)
	names, err := os.ReadDir("../internal/testutil/decks")
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, n := range names {
		if s := n.Name(); len(s) > 5 && s[len(s)-5:] == ".json" {
			all = append(all, s[:len(s)-5])
		}
	}
	type game struct {
		cfg Config
		bot uint64
	}
	var games []game
	for i := range all {
		seat := []string{all[i], all[(i+7)%len(all)]}
		games = append(games, game{Config{Seed: 81000 + uint64(i), Names: seat,
			Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, seat[0]), testutil.RepoDeck(t, reg, seat[1])},
			Tokens: reg.Tokens, NameUniverse: reg.Cards}, uint64(900 + i)})
	}
	legacy := testutil.LegacyDeckNames()
	for i := 0; i < 4; i++ {
		var seat []string
		var decks [][]*cards.Card
		for s := 0; s < 4; s++ {
			n := legacy[(i*3+s*5)%len(legacy)]
			seat = append(seat, n)
			decks = append(decks, testutil.RepoDeck(t, reg, n))
		}
		games = append(games, game{Config{Seed: 82000 + uint64(i), Names: seat, Decks: decks,
			Tokens: reg.Tokens, NameUniverse: reg.Cards}, uint64(950 + i)})
	}
	for _, g := range repoCommanderGames {
		games = append(games, game{Config{Seed: g.seed, Names: []string{g.file, g.opp},
			Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, g.file), testutil.RepoDeck(t, reg, g.opp)},
			Tokens: reg.Tokens, Format: FormatCommander, StartingLife: 40,
			Commanders: [][]int{{testutil.RepoDeckFile(t, g.file).CommanderIndex()}, {testutil.RepoDeckFile(t, g.opp).CommanderIndex()}},
		}, g.bot})
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	total := 0
	for gi, g := range games {
		e := New(g.cfg)
		b := newTestBot(g.bot)
		e.Advance()
		h := sha256.New()
		enc := json.NewEncoder(h)
		prio := 0
		for n := 0; !e.G.Over && e.Pending() != nil && n < 4000; n++ {
			d := e.Pending()
			if d.Kind == decision.KPriority {
				prio++
				if err := enc.Encode(d.Options); err != nil {
					t.Fatal(err)
				}
				if err := enc.Encode(e.PotentialActions(d.Player)); err != nil {
					t.Fatal(err)
				}
				if prio%4 == 0 {
					c := e.Clone()
					if err := enc.Encode(c.EnsurePaymentActions()); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := e.Submit(b.answer(e, d)); err != nil {
				t.Fatalf("game %d %v intent %d: %v", gi, g.cfg.Names, n, err)
			}
		}
		total += prio
		fmt.Fprintf(f, "%d %v prio=%d events=%d head=%s digest=%s\n", gi, g.cfg.Names, prio,
			len(e.L.Events), e.L.Head(), hex.EncodeToString(h.Sum(nil)))
	}
	t.Logf("%d games, %d priority decisions", len(games), total)
}
