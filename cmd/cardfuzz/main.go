// Command cardfuzz plays bot-vs-bot games between randomly generated
// mono-colour decks drawn from the whole supported corpus, to surface engine
// panics, livelocks, submit errors and replay divergences that the curated
// repo decks never reach.
//
// Each deck is 60 cards: 20 lands (basics of its colour, up to 3 of them
// swapped for sampled non-basic lands: the ~1/3 mana share), and 40 distinct non-land cards
// sampled from the supported cards whose colour identity is that colour or
// colourless. Sampling is weighted toward cards the accumulated coverage
// state has seen least, so repeated runs walk the whole pool.
//
// Runs go in batches: every deck of a batch is generated sequentially from
// (-seed, game index) and the coverage snapshot taken at the batch's start,
// then the batch plays on -workers goroutines.
//
// A game whose live object count passes -max-objects (a runaway token
// engine) is ended by the harness and recorded as kind "bigboard", distinct
// from an engine "hang" or "livelock". Every failure is appended to
// -failures as one JSON line carrying both full deck lists, the game seed
// and the diagnostic, and `cardfuzz -repro <file> -line N` replays it.
//
// -autopay off|all|mixed arms production-bot seats with the hosted bot's
// payment-plan wrapper (seat.Bot.EnableAutoPayMana), so they cast through the
// engine's offered PaymentActions instead of floating mana by hand; off is
// byte-identical to the fuzzer before the flag. The failure record carries the
// mode, and the run summary (and -stats) counts planned casts, reversed
// planned casts, PaymentFallback windows, priority decisions with a plan and
// the casts an auto-pay seat still paid by hand. See autopay.go.
//
// -measure-manual-seat-plans is a diagnostic-only opt-in for -autopay off: an
// off run has no payment-plan consumer, so no seat's extension is built and
// the manual_seat_priority_with_plan counter stays zero. Setting the flag
// forces the planner at every priority window to restore that counter at the
// cost of runtime; it changes no game, intent, replay signature or failure
// record. See autoPay.measureManualSeatPlans.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/host"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/paymirror"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

var colourNames = []string{"W", "U", "B", "R", "G"}
var basicFor = []string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

// excludedTypes are card types that never go in a 60-card main deck.
var excludedTypes = []string{"Conspiracy", "Scheme", "Plane", "Phenomenon", "Vanguard", "Dungeon", "Attraction", "Contraption", "Emblem", "Token"}

type poolCard struct {
	card *cards.Card
	name string
	land bool
	// keys is the card's usable-ability inventory (abilityInventory), so the
	// sampling weight can ask whether every one has been used.
	keys []string
}

// pool holds, per colour index 0..4, the eligible non-land and land cards
// (colourless cards appear under every colour).
type pool struct {
	spells [5][]poolCard
	lands  [5][]poolCard
	all    map[string]bool
	cards  map[string]*cards.Card
	keys   map[string][]string // name -> abilityKeys, computed once
	basics [5]*cards.Card
}

func cardName(c *cards.Card) string { return c.Faces[0].Name }

func eligible(c *cards.Card) bool {
	if len(c.Faces) == 0 || c.Faces[0].Name == "" {
		return false
	}
	f := c.Faces[0]
	for _, t := range f.Types {
		for _, x := range excludedTypes {
			if t == x {
				return false
			}
		}
	}
	if f.IsBasic() && f.IsLand() {
		return false
	}
	if strings.Contains(strings.ToLower(f.Oracle), "playing for ante") {
		return false
	}
	// Alchemy rebalances and other digital variants duplicate a printed card.
	if strings.HasPrefix(f.Name, "A-") {
		return false
	}
	return true
}

func identity(c *cards.Card) uint8 {
	var id uint8
	for _, f := range c.Faces {
		id |= f.ColourIdentity()
	}
	return id
}

func buildPool(reg *cards.Registry) (*pool, error) {
	p := &pool{all: map[string]bool{}, cards: map[string]*cards.Card{}, keys: map[string][]string{}}
	sup := effects.Supported()
	for _, c := range reg.Cards {
		if !eligible(c) || len(reg.Unsupported(c, sup)) > 0 {
			continue
		}
		id := identity(c)
		pc := poolCard{card: c, name: cardName(c), land: c.Faces[0].IsLand(), keys: abilityKeys(c)}
		added := false
		for i := 0; i < 5; i++ {
			if id != 0 && id != 1<<i {
				continue
			}
			if pc.land {
				p.lands[i] = append(p.lands[i], pc)
			} else {
				p.spells[i] = append(p.spells[i], pc)
			}
			added = true
		}
		if added {
			p.all[pc.name] = true
			p.cards[pc.name] = c
			p.keys[pc.name] = pc.keys
		}
	}
	for i, b := range basicFor {
		c, ok := reg.Lookup(b)
		if !ok {
			return nil, fmt.Errorf("basic %s not in corpus", b)
		}
		p.basics[i] = c
	}
	for i := 0; i < 5; i++ {
		sort.Slice(p.spells[i], func(a, b int) bool { return p.spells[i][a].name < p.spells[i][b].name })
		sort.Slice(p.lands[i], func(a, b int) bool { return p.lands[i][a].name < p.lands[i][b].name })
	}
	return p, nil
}

// cov is the persistent per-card coverage state.
type cov struct {
	Games    int64            `json:"games"`
	Included map[string]int64 `json:"included"`
	Cast     map[string]int64 `json:"cast"`
	Ability  map[string]int64 `json:"ability"`
	Fails    map[string]int64 `json:"fails"`
	// Used counts, per card, the games in which each of its own abilities
	// (abilityInventory keys) was used. Absent from state files written
	// before it existed; those load with it empty.
	Used map[string]map[string]int64 `json:"used,omitempty"`
	// Offered counts, per card, the games in which the engine offered each
	// of its own activated abilities (inventory keys) to its controller as a
	// priority action (useProbe.observe). With Used it splits a never-used
	// activated ability into "offered, never chosen" (a seat-policy gap) and
	// "never offered" (an engine offer gap, or a board never reached). Absent
	// from older state files; those load with it empty.
	Offered map[string]map[string]int64 `json:"offered,omitempty"`
}

func loadCov(path string) (*cov, error) {
	c := &cov{Included: map[string]int64{}, Cast: map[string]int64{}, Ability: map[string]int64{}, Fails: map[string]int64{}, Used: map[string]map[string]int64{}, Offered: map[string]map[string]int64{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	for _, m := range []*map[string]int64{&c.Included, &c.Cast, &c.Ability, &c.Fails} {
		if *m == nil {
			*m = map[string]int64{}
		}
	}
	if c.Used == nil {
		c.Used = map[string]map[string]int64{}
	}
	if c.Offered == nil {
		c.Offered = map[string]map[string]int64{}
	}
	return c, nil
}

func (c *cov) save(path string) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// missing returns the keys of name's inventory never used yet, in keys'
// (sorted) order.
func (c *cov) missing(name string, keys []string) []string {
	var out []string
	u := c.Used[name]
	for _, k := range keys {
		if u[k] == 0 {
			out = append(out, k)
		}
	}
	return out
}

// full reports whether name was cast/played and every ability in keys used.
func (c *cov) full(name string, keys []string) bool {
	return c.Cast[name] > 0 && len(c.missing(name, keys)) == 0
}

// weight favours cards never cast (x8), cards cast but with some ability
// never used (x4), and seldom-included cards. It reads only the coverage
// snapshot, so it is deterministic for a given state.
func (c *cov) weight(pc poolCard) float64 {
	w := 1.0 / float64(1+c.Included[pc.name])
	switch {
	case c.Cast[pc.name] == 0:
		w *= 8
	case len(c.missing(pc.name, pc.keys)) > 0:
		w *= 4
	}
	return w
}

// sampleDistinct draws k distinct cards from cands by weight (Efraimidis-Spirakis).
func sampleDistinct(r *rand.Rand, cands []poolCard, k int, c *cov) []poolCard {
	if k >= len(cands) {
		return append([]poolCard(nil), cands...)
	}
	type kv struct {
		key float64
		i   int
	}
	keys := make([]kv, len(cands))
	for i, pc := range cands {
		u := r.Float64()
		if u == 0 {
			u = 1e-12
		}
		// key = u^(1/w); compare in log space.
		keys[i] = kv{key: math.Log(u) / c.weight(pc), i: i}
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].key != keys[b].key {
			return keys[a].key > keys[b].key
		}
		return keys[a].i < keys[b].i
	})
	out := make([]poolCard, k)
	for j := 0; j < k; j++ {
		out[j] = cands[keys[j].i]
	}
	return out
}

type genDeck struct {
	Colour string   `json:"colour"`
	Cards  []string `json:"cards"`
}

func generate(r *rand.Rand, p *pool, c *cov) genDeck {
	ci := r.IntN(5)
	nonbasic := sampleDistinct(r, p.lands[ci], 3, c)
	spells := sampleDistinct(r, p.spells[ci], 40, c)
	d := genDeck{Colour: colourNames[ci]}
	for i := 0; i < 20-len(nonbasic); i++ {
		d.Cards = append(d.Cards, basicFor[ci])
	}
	for _, pc := range nonbasic {
		d.Cards = append(d.Cards, pc.name)
	}
	for _, pc := range spells {
		d.Cards = append(d.Cards, pc.name)
	}
	return d
}

func resolveDeck(reg *cards.Registry, d genDeck) ([]*cards.Card, error) {
	out := make([]*cards.Card, 0, len(d.Cards))
	for _, n := range d.Cards {
		c, ok := reg.Lookup(n)
		if !ok {
			return nil, fmt.Errorf("card %q not found", n)
		}
		out = append(out, c)
	}
	return out, nil
}

// failure is one JSONL record.
type failure struct {
	Kind  string    `json:"kind"`
	Seed  uint64    `json:"seed"`
	Decks []genDeck `json:"decks"`
	// Explore records that seat exploreSeat(Seed) played the exploration
	// policy, so -repro rebuilds the same seats.
	Explore bool `json:"explore,omitempty"`
	// AutoPay is the run's -autopay mode ("" for off, so an off record is
	// byte-identical to one written before the flag), ExploreAutoPay its
	// -explore-autopay, and AutoPaySeats the seats that auto-paid (derived
	// from Seed; informational -- -repro rebuilds them from the mode).
	AutoPay        string `json:"autopay,omitempty"`
	ExploreAutoPay bool   `json:"explore_autopay,omitempty"`
	AutoPaySeats   []int  `json:"autopay_seats,omitempty"`
	Turns          int32  `json:"turns"`
	Intents        int    `json:"intents"`
	Diag           string `json:"diag"`
	Sig            string `json:"sig"`
}

// stamp records the run's auto-pay configuration on a failure record.
func (f *failure) stamp(a autoPay, ap []bool) *failure {
	if f != nil && a.on() {
		f.AutoPay, f.ExploreAutoPay, f.AutoPaySeats = a.mode, a.explore, seatList(ap)
	}
	return f
}

// signature reduces a diagnostic to a dedupe key: for a panic, the panic
// value plus the first engine frame; otherwise the first line.
func signature(kind, diag string) string {
	lines := strings.Split(diag, "\n")
	first := lines[0]
	if i := strings.Index(first, "): "); i >= 0 && strings.HasPrefix(first, "engine panic") {
		first = first[i+3:]
	}
	if kind == "panic" {
		for i, l := range lines {
			if strings.Contains(l, "github.com/adams-shaun/gorge/") && !strings.Contains(l, "internal/bench") && !strings.Contains(l, "runtime/") && i+1 < len(lines) && !strings.Contains(l, "panic(") {
				fr := strings.TrimSpace(l)
				if j := strings.LastIndex(fr, "("); j > 0 {
					fr = fr[:j]
				}
				return kind + ": " + trunc(first, 120) + " @ " + fr
			}
		}
	}
	if kind == "livelock" {
		// Keep only the cycle's event kinds and quoted texts, digits
		// stripped, so the same cycle on different seeds collapses.
		if i := strings.Index(first, "cycle: ["); i >= 0 {
			first = first[i:]
		}
		return kind + ": " + trunc(stripDigits(first), 200)
	}
	return kind + ": " + trunc(stripDigits(first), 160)
}

func stripDigits(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !prev {
				b.WriteByte('#')
			}
			prev = true
			continue
		}
		prev = false
		b.WriteRune(r)
	}
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type gameResult struct {
	idx      int
	fail     *failure
	included []string
	gc       *gameCov
	// secs is the game's harness wall-clock time (0 for a hang or a
	// -skip record): -stats sums it so throughput excludes hang budgets.
	secs float64
}

// gameCov is one game's coverage: cards cast/played, cards with any ability
// pushed, and per card the inventory keys used (abilitiesUsed) and offered
// (abilitiesOffered).
type gameCov struct {
	cast, ability map[string]bool
	used, offered map[string]map[string]bool
	// ap is the game's auto-pay counters (collected in every mode).
	ap             *apStats
	mirrorFailures []failure
	mirrorVerdicts map[string]int
}

// autopayMirror is set once from the opt-in CLI flag before worker games start.
var autopayMirror bool
var autopayMirrorOptions = paymirror.Options{Control: true}

// botSeat is the production hosted bot, or with explore the opt-in
// coverage-exploration policy (seat.NewExploreBot, botpolicy.ExploreDecide);
// autoPay arms either with the payment-plan wrapper (seat.Bot.
// EnableAutoPayMana), exactly as host.NewBotPolicySeatWithAutoPayMana arms a
// hosted bot. With autoPay false both are the pre-flag seats.
func botSeat(seed uint64, explore, autoPay bool) seat.Seat {
	if explore {
		b := seat.NewExploreBot(seed)
		if autoPay {
			b.EnableAutoPayMana()
		}
		return b
	}
	s, err := host.NewBotPolicySeatWithAutoPayMana(host.BotPolicy, seed, autoPay)
	if err != nil {
		panic(err)
	}
	return s
}

// played walks the finished game for which deck cards were cast/land-played,
// which had an ability put on the stack, and which of their own abilities
// were used (abilitiesUsed) and offered (abilitiesOffered).
func played(e *rules.Engine, decks [][]*cards.Card, probe *useProbe) *gameCov {
	gc := &gameCov{cast: map[string]bool{}, ability: map[string]bool{}}
	name := func(id state.ObjID) string { return realCardName(e, id) }
	evs := e.L.Events
	for i, ev := range evs {
		switch ev.Kind {
		case events.PutOnStack:
			if ev.To == state.ZStack {
				if n := name(ev.Obj); n != "" {
					gc.cast[n] = true
				}
			}
		case events.LandPlayed:
			if n := name(playedLand(evs, i)); n != "" {
				gc.cast[n] = true
			}
		case events.AbilityPush, events.TriggerPush:
			if n := name(ev.Obj); n != "" {
				gc.ability[n] = true
			}
		}
	}
	gc.used = abilitiesUsed(e, decks, probe)
	gc.offered = abilitiesOffered(e, decks, probe)
	return gc
}

// playedLand is the object a LandPlayed event (at index i) played. The event
// carries only the player (rules/cast.go's settleLandPlay: it closes the
// land-play accounting AFTER the land's entry boundary), so the land is the
// latest MoveZone before it in the same action -- the scan stops at the
// Priority event the play_land answer emits before moving the card. The land's
// own move is normally hand->battlefield; a replacement that fully replaces
// the entry (CR 305.1: the land was still played) moves it elsewhere, and
// that move is still the played card's. Returns 0 when none is found.
func playedLand(evs []events.Event, i int) state.ObjID {
	for j := i - 1; j >= 0; j-- {
		switch evs[j].Kind {
		case events.MoveZone:
			return evs[j].Obj
		case events.Priority:
			return 0
		}
	}
	return 0
}

// exploreSeat is the seat index that plays the exploration policy in an
// -explore game: the seed's low bit, so across games every generated deck
// is piloted by each policy about half the time.
func exploreSeat(seed uint64) int { return int(seed & 1) }

// playOne plays one game with every seat paying mana by hand (-autopay off).
func playOne(reg *cards.Registry, decks []genDeck, seed uint64, maxTurns, maxIntents, maxObjects int, verify, explore bool) (fail *failure, gc *gameCov) {
	return playGame(reg, decks, seed, maxTurns, maxIntents, maxObjects, verify, explore, autoPay{mode: "off"})
}

// exploreIndex is the game's explore seat, or -1 when explore is off.
func exploreIndex(seed uint64, explore bool) int {
	if !explore {
		return -1
	}
	return exploreSeat(seed)
}

// playGame is playOne under an auto-pay configuration (-autopay).
func playGame(reg *cards.Registry, decks []genDeck, seed uint64, maxTurns, maxIntents, maxObjects int, verify, explore bool, apc autoPay) (fail *failure, gc *gameCov) {
	exploreIdx := exploreIndex(seed, explore)
	ap := apc.seats(seed, len(decks), exploreIdx)
	mk := func(kind, diag string, o gbench.Outcome) *failure {
		return (&failure{Kind: kind, Seed: seed, Decks: decks, Explore: explore, Turns: o.Turns, Intents: o.Intents, Diag: diag, Sig: signature(kind, diag)}).stamp(apc, ap)
	}
	var dk [][]*cards.Card
	for _, d := range decks {
		cs, err := resolveDeck(reg, d)
		if err != nil {
			return mk("setup", err.Error(), gbench.Outcome{}), nil
		}
		dk = append(dk, cs)
	}
	names := make([]string, len(decks))
	seats := make([]seat.Seat, len(decks))
	for i := range decks {
		names[i] = fmt.Sprintf("%s-%d", decks[i].Colour, i)
		seats[i] = botSeat(seed^(0x9e3779b97f4a7c15*uint64(i+1)), i == exploreIdx, ap[i])
	}
	// The aborting game's own cfg arms the mid-resolution object cap (below).
	// Only a COMPLETED game reaches the replay verification (every stall kind
	// returns early), and a completed game never crossed the cap, so the cap
	// is inert on the replay; a cap-aborted game is never verified.
	cfg := rules.Config{Names: names, Decks: dk, Tokens: reg.Tokens, Seed: seed, NameUniverse: reg.Cards,
		LoopGuard: objectCapGuard(maxObjects)}
	var o gbench.Outcome
	var e *rules.Engine
	var err error
	probe := &useProbe{}
	app := &apProbe{ap: ap}
	var mirrorFailures []failure
	mirrorVerdicts := map[string]int{}
	offerSeen := map[probeRef]bool{}
	board := boardGuard(maxObjects)
	dumped := false
	guard := func(e *rules.Engine) (string, string) {
		probe.observe(e, offerSeen)
		if d := e.Pending(); d != nil {
			// apProbe reads d.PaymentActions for EVERY deciding seat -- its
			// manual_seat_priority_with_plan counter measures plans offered
			// to seats that ignore the extension. So an auto-pay run (any
			// mode but off) must build every seat's extension to keep those
			// counters, exactly as the eager publisher did; off runs build
			// none and stay cheap. A consumer seat always opts in. The
			// off-mode diagnostic opt-in (-measure-manual-seat-plans) also
			// builds every seat's extension, restoring the manual-seat
			// counters at the cost of the planner on every priority window.
			if apc.measurePlans() {
				e.EnsurePaymentActions()
			} else if consumer, ok := seats[d.Player].(seat.PaymentPlanConsumer); ok && consumer.WantsPaymentActions() {
				e.EnsurePaymentActions()
			}
		}
		if dumpAt > 0 && !dumped {
			if d := e.Pending(); d != nil && d.Seq >= dumpAt {
				dumped = true
				dumpDecision(e, d)
			}
		}
		if board == nil {
			return "", ""
		}
		return board(e)
	}
	setup := func(e *rules.Engine) {
		probe.install(e)
		app.install(e)
	}
	var capAbort *rules.LivelockError
	func() {
		defer func() {
			if r := recover(); r != nil {
				if l, ok := r.(*rules.LivelockError); ok && l.Reason == objectCapReason {
					// The mid-resolution object cap fired outside the drive
					// loop (a genesis burst that already overflows the
					// budget). gbench's own recover never ran, so the abort
					// arrives here as the raw panic value; e/o are untouched.
					capAbort = l
					return
				}
				err = fmt.Errorf("panic outside drive loop: %v", r)
			}
		}()
		o, e, err = gbench.PlayGame(cfg, seats, maxTurns, maxIntents, gbench.Hooks{
			Guard: guard, Setup: setup, Decision: app.decision,
			Submit: func(e *rules.Engine, seatIdx int, d *decision.Decision, in decision.Intent) (bool, error) {
				if !autopayMirror || seatIdx < 0 || seatIdx >= len(ap) || !ap[seatIdx] || in.Payment == nil {
					return false, nil
				}
				answer := func(engine *rules.Engine, follow *decision.Decision) (decision.Intent, error) {
					v := view.Project(engine.G, engine, follow.Player, follow)
					return seats[follow.Player].Decide(context.Background(), v, *follow)
				}
				report := paymirror.CheckLive(e, in, answer, autopayMirrorOptions)
				status, key := report.Verdict()
				verdict := string(status)
				if key != "" {
					verdict += " " + key
				}
				mirrorVerdicts[verdict]++
				if f := mirrorFailureRecord(report, seed, decks, explore, apc, ap, e.G.Turn); f != nil {
					mirrorFailures = append(mirrorFailures, *f)
				}
				if report.AError != "" {
					return true, fmt.Errorf("paymirror run A: %s", report.AError)
				}
				return true, nil
			},
		})
	}()
	if capAbort != nil {
		// The engine never returned from gbench (e is nil here), so the record
		// carries the abort's own counts and no battlefield census.
		return mk("bigboard", objectCapDiag(nil, maxObjects, capAbort.Count, capAbort.Error()), o),
			&gameCov{ap: app.finish(), mirrorFailures: mirrorFailures, mirrorVerdicts: mirrorVerdicts}
	}
	if err != nil {
		return mk("error", err.Error(), o), &gameCov{ap: app.finish(), mirrorFailures: mirrorFailures, mirrorVerdicts: mirrorVerdicts}
	}
	gc = played(e, dk, probe)
	gc.ap = app.finish()
	gc.mirrorFailures = mirrorFailures
	gc.mirrorVerdicts = mirrorVerdicts
	withCtx := func(kind, diag string) *failure {
		ctx, involved := tailContext(e, 24)
		f := mk(kind, diag+"\n-- last events --\n"+ctx, o)
		if kind != "panic" && len(involved) > 0 {
			f.Sig += " {" + strings.Join(involved, ", ") + "}"
		}
		return f
	}
	switch {
	case o.StallOn == "livelock" && strings.HasPrefix(o.Livelock, objectCapAbortPrefix):
		// The engine's own per-event watchdog fired the mid-resolution object
		// cap: the object arena crossed -max-objects during a resolution. The
		// game is a bigboard record, not an engine-bug livelock, and it keeps
		// the boundary record's kind and diagnostic vocabulary so triage
		// reads one kind either way; the tail context survives, because the
		// resolution the abort fired inside is what a triager wants to see.
		return withCtx("bigboard", objectCapDiag(e, maxObjects, boardCount(e), o.Livelock)), gc
	case gbench.IsAbort(o.StallOn):
		return withCtx(o.StallOn, o.Livelock), gc
	case o.StallOn == "intents":
		return withCtx("intents", fmt.Sprintf("intent cap %d hit at turn %d", maxIntents, o.Turns)), gc
	case o.StallOn == "bigboard":
		// Not an engine bug: the game grew a board past the harness's
		// budget. Its own kind lets triage separate it from hangs, and the
		// signature names the card with the most battlefield copies (the
		// usual token engine) rather than the log tail.
		return mk("bigboard", o.Livelock, o), gc
	}
	// The inert backstop (rules/priority_guard.go) keeps a game from spinning
	// on a priority option whose handler changed nothing, but the option was
	// still an offer/handler disagreement: report the game.
	for i, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, rules.InertPriorityNotePrefix) {
			return mk("inert", fmt.Sprintf("%s (player %d, event %d)", ev.Text, ev.Player, i), o), gc
		}
	}
	if verify {
		var rerr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					rerr = fmt.Errorf("replay panic: %v", r)
				}
			}()
			_, rerr = replay.Replay(e.L, cfg)
		}()
		if rerr != nil {
			return mk("replay", rerr.Error(), o), gc
		}
	}
	// A plan-contract violation (a reversed planned cast, a fallback window)
	// is recorded only for an otherwise clean game, so it never masks an
	// engine failure; -plan-failures=false keeps counting without recording.
	if planFailures {
		if kind, diag, sig, ok := app.contractFailure(); ok {
			f := mk(kind, diag, o)
			f.Sig = sig
			return f, gc
		}
	}
	return nil, gc
}

func mirrorFailureRecord(report *paymirror.Report, seed uint64, decks []genDeck, explore bool, apc autoPay, ap []bool, turn int32) *failure {
	status, key := report.Verdict()
	if status == paymirror.Equivalent || report.ExpectedUnmirrorable() {
		// An expected unmirrorable cast (paymirror.RouteResult.Expected) is a
		// known limit of the manual route, not a finding; it is still counted
		// in the run's mirror_verdicts under its "expected:" key.
		return nil
	}
	b, _ := json.Marshal(report)
	return &failure{Kind: "mirror", Seed: seed, Decks: decks, Explore: explore,
		AutoPay: apc.mode, ExploreAutoPay: apc.explore, AutoPaySeats: seatList(ap),
		Turns: turn, Diag: string(b), Sig: "mirror: " + key}
}

// planFailures is -plan-failures: record plan-contract violations.
var planFailures = true

// boardGuard is the harness-side board-size watchdog at decision
// boundaries: once the live (non-ceased) object count exceeds max, the game
// ends as a "bigboard" stall. The arena length bounds the live count from
// above, so a game that never grows past max never pays the scan.
// max <= 0 disables it -- and then no mid-resolution cap is armed either
// (objectCapGuard), so "0 disables" means the whole budget is off.
func boardGuard(max int) func(*rules.Engine) (string, string) {
	if max <= 0 {
		return nil
	}
	return func(e *rules.Engine) (string, string) {
		if len(e.G.Objs) <= max {
			return "", ""
		}
		live := boardCount(e)
		if live <= max {
			return "", ""
		}
		return "bigboard", bigboardDiag(e, max, fmt.Sprintf("live object count %d", live))
	}
}

// objectCapReason is rules.LivelockError's Reason for the mid-resolution
// object-cap abort (rules/livelock.go). It is the string both the abort's
// own rendering and the re-classification below key on, so the two cannot
// drift apart silently: rules/livelock_objectcap_test.go pins the rendering
// and cmd/cardfuzz's boardcap test pins the classification.
const objectCapReason = "object cap"

// objectCapAbortPrefix is the head of the rendered abort diagnostic for the
// mid-resolution object cap. It is what survives into an Outcome (the
// LivelockError itself is recovered inside internal/bench), so it is all the
// re-classification can key on there.
const objectCapAbortPrefix = "livelock detected (object cap"

// objectCapGuard arms the engine's own per-event watchdog (rules/livelock.go)
// with the mid-resolution object cap. Between two decisions one resolution
// can mint thousands of objects -- one Krenko, Mob Boss activation doubles
// the Goblin count -- and a decision-boundary check cannot see inside it, so
// a storm that crosses the budget mid-resolution used to run unchecked until
// the next decision, burning the whole wall-clock budget and getting the
// game recorded as a hang. The watcher's per-event path reads the arena
// length with one O(1) comparison, so the abort fires within one event of
// the crossing. The game is then classified as its own "bigboard" stall --
// never as an engine-bug livelock -- by the prefix check in playGame.
// max <= 0 arms nothing (the zero value is "off", never a default).
func objectCapGuard(max int) *rules.LoopGuard {
	if max <= 0 {
		return nil
	}
	return &rules.LoopGuard{MaxObjs: max}
}

// boardCount is the live (non-ceased) object count, the O(n) scan the
// boundary guard pays only when the arena length says the budget could be
// crossed.
func boardCount(e *rules.Engine) int {
	live := 0
	for i := range e.G.Objs {
		if e.G.Objs[i].Zone != state.ZCeased {
			live++
		}
	}
	return live
}

// bigboardDiag renders a bigboard record's diagnostic: a header naming the
// cap crossing and the turn, then the battlefield census. label names the
// count that crossed ("live object count 120"); boundary records keep their
// existing header, the mid-resolution record says which population crossed.
func bigboardDiag(e *rules.Engine, max int, label string) string {
	most, table := battlefieldCensus(e)
	var b strings.Builder
	fmt.Fprintf(&b, "%s exceeds -max-objects %d at turn %d", label, max, e.G.Turn)
	if most != "" {
		fmt.Fprintf(&b, " (most on battlefield: %s)", most)
	}
	b.WriteString(table)
	return b.String()
}

// objectCapDiag renders the bigboard diagnostic for the mid-resolution
// object-cap abort. count is the arena population the watcher aborted on; e
// is nil when the abort fired before the drive loop ever ran (a genesis
// burst that already overflows the budget, recovered in playGame's own
// recover), in which case there is no engine to census.
func objectCapDiag(e *rules.Engine, max, count int, abort string) string {
	var b strings.Builder
	turn := int32(0)
	if e != nil {
		turn = e.G.Turn
	}
	fmt.Fprintf(&b, "arena object count %d exceeds -max-objects %d at turn %d", count, max, turn)
	if e != nil {
		fmt.Fprintf(&b, " (live %d)", boardCount(e))
		b.WriteString(battlefieldCensusTable(e))
	}
	fmt.Fprintf(&b, "\n-- mid-resolution object cap --\n%s", abort)
	return b.String()
}

// battlefieldCensus renders the sorted battlefield census trailing a
// bigboard diagnostic. The string is the card with the most battlefield
// copies (the usual token engine, "" for an empty battlefield); the second
// return is the "-- battlefield --" table block, top ten rows.
func battlefieldCensus(e *rules.Engine) (string, string) {
	counts := map[string]int{}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Card != nil {
			counts[cardName(o.Card)]++
		}
	}
	type nc struct {
		n string
		c int
	}
	var top []nc
	for n, c := range counts {
		top = append(top, nc{n, c})
	}
	sort.Slice(top, func(a, b int) bool { return top[a].c > top[b].c || (top[a].c == top[b].c && top[a].n < top[b].n) })
	most := ""
	if len(top) > 0 {
		most = top[0].n
	}
	var b strings.Builder
	b.WriteString("\n-- battlefield --\n")
	for i, t := range top {
		if i == 10 {
			break
		}
		fmt.Fprintf(&b, "%6d %s\n", t.c, t.n)
	}
	return most, b.String()
}

// battlefieldCensusTable renders only the table block of the census.
func battlefieldCensusTable(e *rules.Engine) string {
	_, table := battlefieldCensus(e)
	return table
}

// tailContext renders the last n log events with object names, and returns
// the sorted distinct non-basic card names they reference (the likely
// culprits of a cycle).
func tailContext(e *rules.Engine, n int) (string, []string) {
	evs := e.L.Events
	if len(evs) > n {
		evs = evs[len(evs)-n:]
	}
	objName := func(id state.ObjID) string {
		o := e.G.Obj(id)
		if o == nil || o.Card == nil {
			return ""
		}
		nm := cardName(o.Card)
		if o.Source != 0 && o.Source != id {
			if src := e.G.Obj(o.Source); src != nil && src.Card != nil && cardName(src.Card) != nm {
				nm += "<-" + cardName(src.Card)
			}
		}
		return nm
	}
	seen := map[string]bool{}
	var b strings.Builder
	for _, ev := range evs {
		nm := ""
		if ev.Obj != 0 {
			nm = objName(ev.Obj)
			for _, part := range strings.Split(nm, "<-") {
				if part != "" && !isBasicName(part) {
					seen[part] = true
				}
			}
		}
		fmt.Fprintf(&b, "%d %s p=%d obj=%d(%s) %s->%s amt=%d %q\n", ev.Seq, ev.Kind, ev.Player, ev.Obj, nm, ev.From, ev.To, ev.Amount, ev.Text)
	}
	var names []string
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	return b.String(), names
}

func isBasicName(n string) bool {
	for _, b := range basicFor {
		if b == n {
			return true
		}
	}
	return false
}

var hang *time.Duration

// playWatched runs playOne on its own goroutine under a wall-clock budget.
// The budget is harness-only (the engine never sees the clock): a game that
// overruns is recorded as a "hang" carrying its goroutine's stack, and the
// goroutine is abandoned since Go cannot kill it.
func playWatched(reg *cards.Registry, decks []genDeck, seed uint64, maxTurns, maxIntents, maxObjects int, verify, explore bool, apc autoPay, budget time.Duration) (*failure, *gameCov, bool) {
	type res struct {
		f  *failure
		gc *gameCov
	}
	done := make(chan res, 1)
	gid := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		buf = buf[:runtime.Stack(buf, false)]
		fields := strings.Fields(string(buf))
		id := ""
		if len(fields) > 1 {
			id = fields[1]
		}
		gid <- id
		crashLog.start(id, seed)
		f, gc := playGame(reg, decks, seed, maxTurns, maxIntents, maxObjects, verify, explore, apc)
		crashLog.end(seed)
		done <- res{f, gc}
	}()
	id := <-gid
	t := time.NewTimer(budget)
	defer t.Stop()
	select {
	case r := <-done:
		return r.f, r.gc, false
	case <-t.C:
	}
	buf := make([]byte, 64<<20)
	buf = buf[:runtime.Stack(buf, true)]
	stack := ""
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.HasPrefix(g, "goroutine "+id+" ") {
			stack = g
			break
		}
	}
	sig := "hang"
	for _, l := range strings.Split(stack, "\n") {
		if strings.HasPrefix(l, "github.com/adams-shaun/gorge/") {
			fr := l
			if j := strings.LastIndex(fr, "("); j > 0 {
				fr = fr[:j]
			}
			sig = "hang: @ " + fr
			break
		}
	}
	f := &failure{Kind: "hang", Seed: seed, Decks: decks, Explore: explore, Diag: fmt.Sprintf("game exceeded %s wall clock\n%s", budget, stack), Sig: sig}
	return f.stamp(apc, apc.seats(seed, len(decks), exploreIndex(seed, explore))), nil, true
}

func main() {
	dir := flag.String("dir", ".cards", "corpus directory")
	games := flag.Int("games", 1000, "games to play this run")
	batch := flag.Int("batch", 400, "games per batch (coverage snapshot granularity)")
	seed := flag.Uint64("seed", 1, "base seed; game i of the run plays at hash(seed, games-so-far + i)")
	workers := flag.Int("workers", 8, "parallel games")
	statePath := flag.String("state", "cardfuzz-state.json", "persistent coverage state")
	failPath := flag.String("failures", "cardfuzz-failures.jsonl", "append failure records here")
	maxTurns := flag.Int("max-turns", 100, "turn cap (a stall, not a failure)")
	maxIntents := flag.Int("max-intents", 20000, "intent cap (recorded as an 'intents' failure)")
	maxObjects := flag.Int("max-objects", 20000, "live object cap (recorded as a 'bigboard' failure; 0 disables)")
	verify := flag.Bool("verify", true, "replay every finished game and compare")
	explore := flag.Bool("explore", true, "one seat per game (by seed parity) plays the coverage-exploration policy (seat.NewExploreBot) instead of the production bot")
	repro := flag.String("repro", "", "replay a failure record from this JSONL file (with -line)")
	line := flag.Int("line", 1, "1-based line of -repro to replay")
	report := flag.Bool("report", false, "print coverage summary from -state and exit")
	missing := flag.Bool("missing", false, "with -report: also list cards cast/played but with abilities never used, and which")
	hang = flag.Duration("hang", 90*time.Second, "wall-clock budget per game before it is recorded as a 'hang' (its goroutine is abandoned)")
	maxHangs := flag.Int("max-hangs", 6, "stop the run once this many hung games are leaked (each burns a core)")
	cpuProfile := flag.String("cpuprofile", "", "with -repro: write a CPU profile of the replay here")
	autoPayMode := flag.String("autopay", "off", "off|all|mixed: which production-bot seats cast through offered payment plans (seat.Bot.EnableAutoPayMana); mixed picks per game and seat from the seed")
	mirror := flag.Bool("autopay-mirror", false, "mirror each auto-pay planned cast against float-then-cast (opt-in)")
	exploreAutoPay := flag.Bool("explore-autopay", false, "the -explore seat auto-pays too wherever -autopay would select its seat (off by default: the wrapper hides priority mana activations, cutting explore coverage)")
	statsPath := flag.String("stats", "", "write the run's failure counts and auto-pay counters here as JSON")
	measureManualSeatPlans := flag.Bool("measure-manual-seat-plans", false, "diagnostic: in -autopay off mode, force the payment planner on every priority window so manual_seat_priority_with_plan (the plans the eager publisher would have offered to manual seats) is populated. Rebuilds every seat's payment extension, so it costs runtime; it never changes game outcomes, intents, replay signatures or failure records, and it is ignored in all|mixed (which already measure)")
	journalPath := flag.String("journal", "", "append 'start <goroutine> <seed>' / 'end <seed>' around every game: a fatal runtime error (a stack overflow) kills the whole process past any recover, and the journal names the game the crashing goroutine was playing")
	skipPath := flag.String("skip", "", "JSONL of games not to play ({seed, sig, diag} per line, from a -journal crash): each is recorded as a 'fatal' failure instead")
	flag.Uint64Var(&dumpAt, "dump-at", 0, "with -repro: print the first pending decision whose Seq is at least this log index (options, payment actions, pool, battlefield)")
	flag.BoolVar(&planFailures, "plan-failures", true, "record a game whose planned cast was reversed (kind planrev) or fell back to the manual window (planfb) as a failure")
	maxStack := flag.Int("max-stack", 256<<20, "per-goroutine stack limit in bytes (runtime/debug.SetMaxStack): an unbounded recursion dies here instead of at Go's 1 GB default")
	flag.Parse()
	debug.SetMaxStack(*maxStack)
	autopayMirror = *mirror
	apc, err := parseAutoPay(*autoPayMode, *exploreAutoPay)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cardfuzz:", err)
		os.Exit(1)
	}
	// Measurement-only opt-in: threaded into the guard, never stamped on a
	// failure record nor rebuilt by autoPayOf, so -repro and replay are
	// unaffected.
	apc.measureManualSeatPlans = *measureManualSeatPlans

	reg, err := cards.OpenCorpus(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cardfuzz:", err)
		os.Exit(1)
	}
	p, err := buildPool(reg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cardfuzz:", err)
		os.Exit(1)
	}
	if *repro != "" {
		if *cpuProfile != "" {
			pf, err := os.Create(*cpuProfile)
			if err != nil {
				fmt.Fprintln(os.Stderr, "cardfuzz:", err)
				os.Exit(1)
			}
			if err := pprof.StartCPUProfile(pf); err != nil {
				fmt.Fprintln(os.Stderr, "cardfuzz:", err)
				os.Exit(1)
			}
			code := runRepro(reg, *repro, *line, *maxTurns, *maxIntents, *maxObjects)
			pprof.StopCPUProfile()
			pf.Close()
			os.Exit(code)
		}
		os.Exit(runRepro(reg, *repro, *line, *maxTurns, *maxIntents, *maxObjects))
	}
	c, err := loadCov(*statePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cardfuzz:", err)
		os.Exit(1)
	}
	if *report {
		printReport(p, c, true)
		if *missing {
			printMissing(p, c)
		}
		return
	}
	skip, err := loadSkip(*skipPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cardfuzz:", err)
		os.Exit(1)
	}
	if *journalPath != "" {
		jf, err := os.OpenFile(*journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cardfuzz:", err)
			os.Exit(1)
		}
		defer jf.Close()
		crashLog = &journal{f: jf}
	}
	ff, err := os.OpenFile(*failPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cardfuzz:", err)
		os.Exit(1)
	}
	defer ff.Close()
	fw := bufio.NewWriter(ff)

	var stop atomic.Bool
	var hangs atomic.Int64
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; stop.Store(true) }()

	start := time.Now()
	runFails := map[string]int{}
	runKinds := map[string]int{}
	var runAP apStats
	runMirrorVerdicts := map[string]int{}
	var gameSecs float64
	played := 0
	for played < *games && !stop.Load() {
		n := min(*batch, *games-played)
		// Generate the whole batch against one coverage snapshot.
		type job struct {
			idx   int
			seed  uint64
			decks []genDeck
		}
		jobs := make([]job, n)
		for i := 0; i < n; i++ {
			gi := uint64(c.Games) + uint64(i)
			gs := mix(*seed, gi)
			r := rand.New(rand.NewPCG(gs, gs^0xdeadbeefcafef00d))
			jobs[i] = job{idx: i, seed: gs, decks: []genDeck{generate(r, p, c), generate(r, p, c)}}
		}
		results := make([]gameResult, n)
		var wg sync.WaitGroup
		ch := make(chan job)
		for w := 0; w < *workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range ch {
					if stop.Load() {
						results[j.idx] = gameResult{idx: -1}
						continue
					}
					if sk, ok := skip[j.seed]; ok {
						// A game that killed an earlier process: record it,
						// never replay it here.
						f := (&failure{Kind: "fatal", Seed: j.seed, Decks: j.decks, Explore: *explore, Diag: sk.Diag, Sig: sk.Sig}).
							stamp(apc, apc.seats(j.seed, len(j.decks), exploreIndex(j.seed, *explore)))
						gr := gameResult{idx: j.idx, fail: f}
						for _, d := range j.decks {
							gr.included = append(gr.included, d.Cards...)
						}
						results[j.idx] = gr
						continue
					}
					t0 := time.Now()
					f, gc, hung := playWatched(reg, j.decks, j.seed, *maxTurns, *maxIntents, *maxObjects, *verify, *explore, apc, *hang)
					secs := time.Since(t0).Seconds()
					if hung {
						secs = 0
					}
					if hung {
						if hangs.Add(1) > int64(*maxHangs) {
							fmt.Fprintln(os.Stderr, "cardfuzz: too many leaked hung games; stopping")
							stop.Store(true)
						}
					}
					gr := gameResult{idx: j.idx, fail: f, gc: gc, secs: secs}
					for _, d := range j.decks {
						gr.included = append(gr.included, d.Cards...)
					}
					results[j.idx] = gr
				}
			}()
		}
		for _, j := range jobs {
			ch <- j
		}
		close(ch)
		wg.Wait()
		for _, gr := range results {
			if gr.idx < 0 {
				continue
			}
			c.Games++
			played++
			gameSecs += gr.secs
			seen := map[string]bool{}
			for _, nme := range gr.included {
				if !seen[nme] {
					seen[nme] = true
					c.Included[nme]++
				}
			}
			if gr.gc != nil {
				runAP.add(gr.gc.ap)
				if *mirror {
					for verdict, count := range gr.gc.mirrorVerdicts {
						runMirrorVerdicts[verdict] += count
					}
				}
				for nme := range gr.gc.cast {
					c.Cast[nme]++
				}
				for nme := range gr.gc.ability {
					c.Ability[nme]++
				}
				addKeys(c.Used, gr.gc.used)
				addKeys(c.Offered, gr.gc.offered)
			}
			failures := make([]*failure, 0, 1)
			if gr.gc != nil {
				failures = make([]*failure, 0, 1+len(gr.gc.mirrorFailures))
			}
			if gr.fail != nil {
				failures = append(failures, gr.fail)
			}
			if gr.gc != nil {
				for i := range gr.gc.mirrorFailures {
					failures = append(failures, &gr.gc.mirrorFailures[i])
				}
			}
			for _, fail := range failures {
				runFails[fail.Sig]++
				runKinds[fail.Kind]++
				for nme := range seen {
					if !p.isBasic(nme) {
						c.Fails[nme]++
					}
				}
				b, _ := json.Marshal(fail)
				fw.Write(b)
				fw.WriteByte('\n')
			}

		}
		fw.Flush()
		if err := c.save(*statePath); err != nil {
			fmt.Fprintln(os.Stderr, "cardfuzz: save:", err)
		}
		nf := 0
		for _, v := range runFails {
			nf += v
		}
		fmt.Fprintf(os.Stderr, "cardfuzz: %d/%d games, %d failures (%d sigs), %.1f games/s\n", played, *games, nf, len(runFails), float64(played)/time.Since(start).Seconds())
		if *mirror {
			fmt.Fprintf(os.Stderr, "cardfuzz: autopay mirror verdicts: %s\n", formatMirrorVerdicts(runMirrorVerdicts))
		}
		if apc.on() {
			fmt.Fprintf(os.Stderr, "cardfuzz: autopay %s (explore-autopay %v): %s\n", apc.mode, apc.explore, runAP.String())
		} else if apc.measureManualSeatPlans {
			fmt.Fprintf(os.Stderr, "cardfuzz: autopay off (measure-manual-seat-plans: planner forced on every priority): %s\n", runAP.String())
		}
		printReport(p, c, false)
		if *statsPath != "" {
			rs := runStats{AutoPay: apc.mode, ExploreAutoPay: apc.explore, Explore: *explore, Seed: *seed, Games: played,
				Failures: nf, Kinds: runKinds, Sigs: runFails, Stats: runAP, MirrorVerdicts: optionalMirrorVerdicts(*mirror, runMirrorVerdicts), Seconds: time.Since(start).Seconds(),
				GameSeconds: gameSecs, Workers: *workers}
			if err := rs.save(*statsPath); err != nil {
				fmt.Fprintln(os.Stderr, "cardfuzz: stats:", err)
			}
		}
	}
	fmt.Println("== failure signatures this run ==")
	keys := make([]string, 0, len(runFails))
	for k := range runFails {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		return runFails[keys[a]] > runFails[keys[b]] || (runFails[keys[a]] == runFails[keys[b]] && keys[a] < keys[b])
	})
	for _, k := range keys {
		fmt.Printf("%6d  %s\n", runFails[k], k)
	}
}

// addKeys adds one game's per-card key set into a persistent count table.
func addKeys(dst map[string]map[string]int64, game map[string]map[string]bool) {
	for nme, keys := range game {
		u := dst[nme]
		if u == nil {
			u = map[string]int64{}
			dst[nme] = u
		}
		for k := range keys {
			u[k]++
		}
	}
}

func (p *pool) isBasic(n string) bool {
	for _, b := range basicFor {
		if b == n {
			return true
		}
	}
	return false
}

func printReport(p *pool, c *cov, detail bool) {
	total, inc, cast, abil, full := 0, 0, 0, 0, 0
	for n := range p.all {
		total++
		if c.full(n, p.keys[n]) {
			full++
		}
		if c.Included[n] > 0 {
			inc++
		}
		if c.Cast[n] > 0 {
			cast++
		}
		if c.Cast[n] > 0 || c.Ability[n] > 0 {
			abil++
		}
	}
	fmt.Fprintf(os.Stderr, "cardfuzz: pool %d cards | included %d (%.1f%%) | cast/played %d (%.1f%%) | cast-or-ability %d (%.1f%%) | full %d (%.1f%%) | games %d\n",
		total, inc, pct(inc, total), cast, pct(cast, total), abil, pct(abil, total), full, pct(full, total), c.Games)
	if !detail {
		return
	}
	var never []string
	for n := range p.all {
		if c.Cast[n] == 0 && c.Ability[n] == 0 && c.Included[n] > 0 {
			never = append(never, fmt.Sprintf("%d\t%s", c.Included[n], n))
		}
	}
	sort.Strings(never)
	fmt.Printf("# included but never cast/activated: %d\n", len(never))
	for _, l := range never {
		fmt.Println(l)
	}
}

// printMissing lists every pool card cast/played at least once but with some
// ability in its inventory never used: "<times cast>\t<name>\t<key(desc)> ...".
// A key the engine offered as a priority action in some game but the seats
// never chose is marked "<key(desc)>offered:<games>" -- a seat-policy gap,
// not an engine one (see cov.Offered).
func printMissing(p *pool, c *cov) {
	var lines []string
	for n := range p.all {
		if c.Cast[n] == 0 {
			continue
		}
		cd := p.cards[n]
		miss := c.missing(n, p.keys[n])
		if len(miss) == 0 {
			continue
		}
		desc := abilityDescs(cd)
		parts := make([]string, len(miss))
		for i, k := range miss {
			parts[i] = k + "(" + desc[k] + ")"
			if g := c.Offered[n][k]; g > 0 {
				parts[i] += fmt.Sprintf("offered:%d", g)
			}
		}
		lines = append(lines, fmt.Sprintf("%d\t%s\t%s", c.Cast[n], n, strings.Join(parts, " ")))
	}
	sort.Strings(lines)
	fmt.Printf("# cast/played but with abilities never used: %d\n", len(lines))
	for _, l := range lines {
		fmt.Println(l)
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}

func mix(a, b uint64) uint64 {
	x := a*0x9e3779b97f4a7c15 ^ b
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func runRepro(reg *cards.Registry, path string, line, maxTurns, maxIntents, maxObjects int) int {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for i := 1; sc.Scan(); i++ {
		if i != line {
			continue
		}
		var rec failure
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		apc, err := autoPayOf(rec)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if got := seatList(apc.seats(rec.Seed, len(rec.Decks), exploreIndex(rec.Seed, rec.Explore))); fmt.Sprint(got) != fmt.Sprint(rec.AutoPaySeats) {
			fmt.Fprintf(os.Stderr, "cardfuzz: record's autopay_seats %v disagree with the seats -autopay %s derives (%v)\n", rec.AutoPaySeats, apc.mode, got)
			return 1
		}
		previousMirror := autopayMirror
		if rec.Kind == "mirror" {
			autopayMirror = true
		}
		fl, gc := playGame(reg, rec.Decks, rec.Seed, maxTurns, maxIntents, maxObjects, true, rec.Explore, apc)
		autopayMirror = previousMirror
		if gc != nil && gc.ap != nil && apc.on() {
			fmt.Printf("REPRO autopay %s seats %v: %s\n", apc.mode, rec.AutoPaySeats, gc.ap.String())
		}
		if gc != nil {
			for _, mf := range gc.mirrorFailures {
				if rec.Kind == "mirror" && mf.Sig == rec.Sig {
					fmt.Printf("REPRO mirror seed=%d sig=%s\n%s\n", mf.Seed, mf.Sig, mf.Diag)
					return 2
				}
			}
		}
		if fl == nil {
			fmt.Println("REPRO: game completed cleanly (not reproduced)")
			return 0
		}
		fmt.Printf("REPRO %s seed=%d turns=%d intents=%d\n%s\n", fl.Kind, fl.Seed, fl.Turns, fl.Intents, fl.Diag)
		return 2
	}
	fmt.Fprintln(os.Stderr, "line not found")
	return 1
}
