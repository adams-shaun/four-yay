// Command corpuscov generates a bot-vs-bot corpus over the 29 repo decks and
// prints its coverage census (internal/corpuscov): for every (card, ability)
// slot the IN_DECK -> OFFERED -> CHOSEN funnel, the structural and
// policy-avoidance gaps, and decision.Kind coverage.
//
// With -explore P > 0 each seat is wrapped in corpuscov.ExploreSeat
// (coverage-directed exploration, off by default); -record writes one JSONL
// line per decision carrying "explore": true on a forced answer, so an
// imitation learner can drop those rows.
//
// Games are 2-seat. Deck i = game mod 29 plays a deterministic opponent from
// its own pool: the 14 constructed decks play constructed (20 life), the 15
// commander decks play FormatCommander (40 life, command-zone commanders),
// as cmd/botbench's commander mode seats them.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/all"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/corpuscov"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

const maxIntents = 200000

type config struct {
	games   int
	seed    uint64
	policy  string
	autopay bool
	explore float64
	dir     string
	jsonOut string
	record  string
	top     int
	decks   string
}

func main() {
	var c config
	flag.IntVar(&c.games, "games", 29, "games to play (29 = every deck seated once as seat 0)")
	flag.Uint64Var(&c.seed, "seed", 1, "base seed; game g uses seed+g")
	flag.StringVar(&c.policy, "policy", bots.Default, "hosted bot policy for both seats (bots registry; Env/search policies are refused)")
	flag.BoolVar(&c.autopay, "autopay", true, "bots cast through offered payment plans (gorged's -bot-auto-mana default)")
	flag.Float64Var(&c.explore, "explore", 0, "coverage-directed exploration probability per priority decision (0 = off)")
	flag.StringVar(&c.dir, "dir", ".cards", "corpus directory")
	flag.StringVar(&c.jsonOut, "json", "", "write the full census (summary + every row) as JSON here")
	flag.StringVar(&c.record, "record", "", "write one JSONL decision record per decision here (explore-flagged)")
	flag.IntVar(&c.top, "top", 40, "rows per gap list in the text report")
	flag.StringVar(&c.decks, "decks", "", "optional comma-free single deck name to restrict seat 0 to (smoke)")
	flag.Parse()
	if err := run(c, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "corpuscov:", err)
		os.Exit(1)
	}
}

type deckInfo struct {
	name       string
	cards      []*cards.Card
	commanders []int
}

// Record is one decision line of -record.
type Record struct {
	Game    int    `json:"game"`
	Seq     uint64 `json:"seq"`
	Player  int    `json:"player"`
	Kind    string `json:"kind"`
	Options int    `json:"options"`
	Choices []int  `json:"choices"`
	Payment string `json:"payment,omitempty"`
	Explore bool   `json:"explore,omitempty"`
}

func run(c config, out io.Writer) error {
	entry, ok := bots.Lookup(c.policy)
	if c.policy == explorePolicy {
		entry, ok = bots.Entry{}, true
	}
	if !ok {
		return fmt.Errorf("unknown -policy %q (have %v)", c.policy, bots.Names())
	}
	if entry.Env {
		return fmt.Errorf("-policy %q needs a host-built Env (search); not supported by this driver", c.policy)
	}
	reg, err := testutil.OpenCorpusRegistry(c.dir)
	if err != nil {
		return fmt.Errorf("opening corpus at %s: %w", c.dir, err)
	}
	var constructed, commander []deckInfo
	for _, n := range testutil.RepoDeckNames() {
		cs, err := testutil.LoadRepoDeck(reg, n)
		if err != nil {
			return err
		}
		f, err := testutil.LoadRepoDeckFile(n)
		if err != nil {
			return err
		}
		di := deckInfo{name: n, cards: cs}
		if len(f.CommanderNames()) > 0 {
			di.commanders = f.CommanderIndices()
			commander = append(commander, di)
		} else {
			constructed = append(constructed, di)
		}
	}
	all := append(append([]deckInfo(nil), constructed...), commander...)

	var rec *bufio.Writer
	if c.record != "" {
		f, err := os.Create(c.record)
		if err != nil {
			return err
		}
		defer f.Close()
		rec = bufio.NewWriter(f)
		defer rec.Flush()
	}

	cen := corpuscov.New()
	failed := 0
	for g := 0; g < c.games; g++ {
		i := g % len(all)
		a := all[i]
		if c.decks != "" {
			found := false
			for _, d := range all {
				if d.name == c.decks {
					a, found = d, true
				}
			}
			if !found {
				return fmt.Errorf("-decks %q is not a repo deck", c.decks)
			}
		}
		pool := constructed
		if a.commanders != nil {
			pool = commander
		}
		pos := 0
		for k, d := range pool {
			if d.name == a.name {
				pos = k
			}
		}
		b := pool[(pos+1+(g/len(all))%(len(pool)-1))%len(pool)]
		pair := []deckInfo{a, b}
		if err := playOne(c, g, c.seed+uint64(g), pair, reg, cen, rec); err != nil {
			failed++
			fmt.Fprintf(out, "game %d (%s vs %s): %v\n", g, a.name, b.name, err)
		}
	}
	fmt.Fprintf(out, "policy=%s autopay=%v explore=%g seed=%d failed=%d\n", c.policy, c.autopay, c.explore, c.seed, failed)
	cen.WriteText(out, c.top)
	if c.jsonOut != "" {
		f, err := os.Create(c.jsonOut)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := cen.WriteJSON(f); err != nil {
			return err
		}
	}
	return nil
}

func playOne(c config, gi int, seed uint64, pair []deckInfo, reg *cards.Registry, cen *corpuscov.Census, rec *bufio.Writer) (err error) {
	names := []string{pair[0].name, pair[1].name}
	decks := [][]*cards.Card{pair[0].cards, pair[1].cards}
	cfg := rules.Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
	if pair[0].commanders != nil {
		cfg.Format = rules.FormatCommander
		cfg.StartingLife = 40
		cfg.Commanders = [][]int{pair[0].commanders, pair[1].commanders}
	}
	e := rules.NewStartingPlayerChoice(cfg)
	game := func() *state.Game { return e.G }
	seats := make([]seat.Seat, 2)
	explorers := make([]*corpuscov.ExploreSeat, 2)
	for p := range seats {
		s, err := newSeat(c, seed^uint64(p+1))
		if err != nil {
			return err
		}
		if c.explore > 0 {
			explorers[p] = corpuscov.NewExploreSeat(s, c.explore, seed*0x9e3779b97f4a7c15^uint64(p+1), cen, game)
			s = explorers[p]
		}
		seats[p] = s
	}
	cen.BeginGame(names, decks)
	defer cen.EndGame()
	defer func() {
		if r := recover(); r != nil {
			if lle, ok := r.(*rules.LivelockError); ok {
				err = lle
				return
			}
			panic(r)
		}
	}()
	e.AskStartingPlayer()
	e.Advance()
	ctx := context.Background()
	for n := 0; !e.G.Over && e.Pending() != nil; n++ {
		if n >= maxIntents {
			return fmt.Errorf("no termination after %d intents", n)
		}
		d := e.Pending()
		s := seats[d.Player]
		plans := false
		if pc, ok := s.(seat.PaymentPlanConsumer); ok && pc.WantsPaymentActions() {
			e.EnsurePaymentActions()
			plans = true
		}
		cen.ObserveState(e.G)
		cen.ObserveDecision(e.G, d, plans)
		v := view.Project(e.G, e, d.Player, d)
		v.Round = view.RoundOf(e.G, e.L.Events)
		for _, pv := range v.Players {
			if pv.ID == d.Player {
				if dbg := os.Getenv("CORPUSCOV_DEBUG"); dbg != "" && d.Kind == decision.KPriority {
					for _, a := range pv.PotentialActions {
						if o := e.G.Obj(a.Obj); o != nil && o.Card != nil && corpuscov.CardName(o.Card) == dbg {
							pay := "nil"
							if a.Payable != nil {
								pay = fmt.Sprint(*a.Payable)
							}
							fmt.Fprintf(os.Stderr, "DBG turn %d step %v kind=%s ab=%d mode=%q payable=%s cost=%s pool=%v opts=%d\n", e.G.Turn, e.G.Step, a.Kind, a.Ability, a.Mode, pay, o.Face().ManaCost, e.G.Players[d.Player].Pool, len(d.Options))
							for _, pa := range d.PaymentActions {
								if pa.Cast.Object == a.Obj {
									fmt.Fprintf(os.Stderr, "   payaction base=%v plans=%d\n", pa.BaseOptionIndex, len(pa.Plans))
								}
							}
						}
					}
				}
				cen.ObservePotential(e.G, d, pv.PotentialActions)
			}
		}
		in, err := s.Decide(ctx, v, *d)
		if err != nil {
			return fmt.Errorf("seat %d error at intent %d: %w", d.Player, n, err)
		}
		explored := explorers[d.Player] != nil && explorers[d.Player].Explored()
		cen.ObserveChoice(e.G, d, in, explored)
		if rec != nil {
			r := Record{Game: gi, Seq: d.Seq, Player: int(d.Player), Kind: string(d.Kind), Options: len(d.Options), Choices: in.Choices, Explore: explored}
			if in.Payment != nil {
				r.Payment = in.Payment.ActionID
			}
			line, _ := json.Marshal(r)
			rec.Write(append(line, '\n'))
		}
		if err := e.Submit(in); err != nil {
			return fmt.Errorf("intent %d rejected: %w", n, err)
		}
	}
	return nil
}

var _ = decision.KPriority

// explorePolicy is cmd/cardfuzz's coverage policy (seat.NewExploreBot,
// botpolicy.ExploreDecide), absent from the hosted registry. The census uses
// it as a diagnostic: its X6 speculative float is the control for the
// "abilities are priced against the floating pool" structural cause.
const explorePolicy = "cardfuzz-explore"

func newSeat(c config, seed uint64) (seat.Seat, error) {
	if c.policy == explorePolicy {
		b := seat.NewExploreBot(seed)
		if c.autopay {
			b.EnableAutoPayMana()
		}
		return b, nil
	}
	return bots.New(c.policy, bots.Options{Seed: seed, AutoPayMana: c.autopay})
}
