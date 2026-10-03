package searchprobe

// The map- and fmt.Sprint-keyed AttackCandidates and BlockCandidates as they
// stood before the map-free rewrite -- plus, in AttackCandidates, the
// one-defender-per-attacker rule both now share (a declaration naming a
// creature twice is never offered; toggling on a creature's option for
// another defender moves it there) -- the reference the equivalence tests
// hold the live enumerators to.

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func attackCandidatesRef(d *decision.Decision, bot decision.Intent, limit int) []decision.Intent {
	if d == nil || d.Kind != decision.KAttackers || limit < 2 {
		return nil
	}
	chosen := make(map[int]bool, len(bot.Choices))
	for _, c := range bot.Choices {
		chosen[c] = true
	}
	var out []decision.Intent
	seen := make(map[string]bool)
	add := func(set map[int]bool) {
		if len(out) >= limit {
			return
		}
		var choices []int
		attackers := make(map[state.ObjID]bool)
		for _, o := range d.Options {
			if set[o.Index] {
				if attackers[o.Obj] {
					return
				}
				attackers[o.Obj] = true
				choices = append(choices, o.Index)
			}
		}
		// Decision.Validate does not enforce Required; the engine does
		// (CR 508.1d), through the shared quota rule
		// (decision.RequiredQuota), so never offer a declaration short of it.
		if d.RequiredChosen(choices) < d.RequiredQuota() {
			return
		}
		key := fmt.Sprint(choices)
		if seen[key] {
			return
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
		if d.Validate(in) != nil {
			return
		}
		seen[key] = true
		out = append(out, in)
	}
	add(chosen)
	if len(out) == 0 {
		return nil
	}
	// "No attack" is the least declaration the requirement allows: the
	// shared required core (decision.FitRequired over an empty preference).
	none := make(map[int]bool)
	for _, c := range d.FitRequired(nil) {
		none[c] = true
	}
	add(none)
	all := make(map[int]bool)
	attacking := make(map[state.ObjID]bool)
	for _, o := range d.Options {
		if !attacking[o.Obj] {
			attacking[o.Obj] = true
			all[o.Index] = true
		}
	}
	add(all)
	for _, o := range d.Options {
		t := make(map[int]bool, len(chosen)+1)
		for k := range chosen {
			t[k] = true
		}
		if t[o.Index] {
			delete(t, o.Index)
		} else {
			for _, other := range d.Options {
				if other.Obj == o.Obj && other.Index != o.Index {
					delete(t, other.Index)
				}
			}
			t[o.Index] = true
		}
		add(t)
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func blockCandidatesRef(d *decision.Decision, bot decision.Intent, limit int, legal func([]int) []int) []decision.Intent {
	if d == nil || d.Kind != decision.KBlockers || limit < 2 {
		return nil
	}
	var out []decision.Intent
	seen := make(map[string]bool)
	add := func(choices []int) bool {
		if len(out) >= limit {
			return false
		}
		sorted := append([]int(nil), choices...)
		sort.Ints(sorted)
		key := fmt.Sprint(sorted)
		if seen[key] {
			return false
		}
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}
		if d.Validate(in) != nil {
			return false
		}
		if d.RequiredChosen(choices) < d.RequiredQuota() {
			return false
		}
		if !sameChoices(d.FitRequired(choices), choices) {
			return false
		}
		if legal != nil && !sameChoices(legal(append([]int(nil), choices...)), choices) {
			return false
		}
		seen[key] = true
		out = append(out, in)
		return true
	}
	base := append([]int(nil), bot.Choices...)
	if !add(base) {
		return nil
	}
	chosen := make(map[int]bool, len(base)) // membership only -- never ranged.
	for _, c := range base {
		chosen[c] = true
	}
	add(append([]int(nil), d.FitRequired(nil)...))
	for _, o := range d.Options {
		if chosen[o.Index] {
			continue
		}
		next := make([]int, 0, len(base)+1)
		for _, c := range base {
			if o.Group != "" && c >= 0 && c < len(d.Options) && d.Options[c].Group == o.Group {
				continue
			}
			next = append(next, c)
		}
		add(append(next, o.Index))
	}
	for i := range base {
		next := make([]int, 0, len(base)-1)
		next = append(next, base[:i]...)
		next = append(next, base[i+1:]...)
		add(next)
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// recordingLegal wraps a block guard so the equivalence test can prove the
// rewrite consults it with exactly the same declarations, in the same order.
func recordingLegal(legal func([]int) []int, log *[][]int) func([]int) []int {
	if legal == nil {
		return nil
	}
	return func(c []int) []int {
		*log = append(*log, append([]int(nil), c...))
		return legal(c)
	}
}

// checkCandidatesEquivalent holds AttackCandidates/BlockCandidates to the
// reference for one decision and bot answer: identical intents (nil-ness of
// an empty declaration included) and identical guard consultations.
func checkCandidatesEquivalent(t *testing.T, d *decision.Decision, bot decision.Intent, limit int, legal func([]int) []int) {
	t.Helper()
	switch d.Kind {
	case decision.KAttackers:
		got, want := AttackCandidates(d, bot, limit), attackCandidatesRef(d, bot, limit)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AttackCandidates(%+v, %v, %d):\n got %#v\nwant %#v", d.Options, bot.Choices, limit, got, want)
		}
	case decision.KBlockers:
		var gl, wl [][]int
		got := BlockCandidates(d, bot, limit, recordingLegal(legal, &gl))
		want := blockCandidatesRef(d, bot, limit, recordingLegal(legal, &wl))
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gl, wl) {
			t.Fatalf("BlockCandidates(%+v, %v, %d):\n got %#v (guard %v)\nwant %#v (guard %v)", d.Options, bot.Choices, limit, got, gl, want, wl)
		}
	}
}

// botVariants is the bot's answer plus mutations of it: a dropped choice,
// an added one, a duplicate, an out-of-range index, an empty answer.
func botVariants(d *decision.Decision, bot decision.Intent, r *rand.Rand) []decision.Intent {
	out := []decision.Intent{bot, {Seq: d.Seq, Player: d.Player}}
	n := len(d.Options)
	for k := 0; k < 5; k++ {
		in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: append([]int(nil), bot.Choices...)}
		switch k {
		case 0:
			if len(in.Choices) > 0 {
				in.Choices = in.Choices[1:]
			}
		case 1:
			if n > 0 {
				in.Choices = append(in.Choices, r.IntN(n))
			}
		case 2:
			if len(in.Choices) > 0 {
				in.Choices = append(in.Choices, in.Choices[0])
			}
		case 3:
			in.Choices = append(in.Choices, n+r.IntN(3))
		case 4:
			if n > 0 {
				in.Choices = r.Perm(n)[:r.IntN(n+1)]
			}
		}
		out = append(out, in)
	}
	return out
}

func randomCombatDecision(r *rand.Rand) *decision.Decision {
	kind := decision.KAttackers
	if r.IntN(2) == 0 {
		kind = decision.KBlockers
	}
	d := &decision.Decision{Seq: uint64(r.IntN(9)), Player: state.PlayerID(r.IntN(2)), Kind: kind}
	objs := 1 + r.IntN(6)
	targets := 1 + r.IntN(3)
	for o := 0; o < objs; o++ {
		for a := 0; a < targets; a++ {
			if r.IntN(4) == 0 {
				continue
			}
			opt := decision.Option{Index: len(d.Options), Obj: state.ObjID(10 + o), Attacker: state.ObjID(50 + a),
				Value: r.IntN(3), Required: r.IntN(8) == 0}
			if kind == decision.KBlockers {
				opt.Group = fmt.Sprint("b", o)
				opt.BlockMust = r.IntN(10) == 0
				opt.AttackMust = r.IntN(10) == 0
				if r.IntN(6) == 0 {
					opt.MinBlockers = 2
				}
				if r.IntN(6) == 0 {
					opt.MaxBlockers = 1
				}
			}
			d.Options = append(d.Options, opt)
		}
	}
	// A shuffled Index permutation, and now and then a broken one.
	if r.IntN(4) == 0 {
		p := r.Perm(len(d.Options))
		for i := range d.Options {
			d.Options[i].Index = p[i]
		}
	}
	if r.IntN(30) == 0 && len(d.Options) > 0 {
		d.Options[r.IntN(len(d.Options))].Index = len(d.Options) + 1
	}
	d.Min = 0
	d.Max = len(d.Options)
	if r.IntN(4) == 0 {
		d.Max = r.IntN(len(d.Options) + 1)
	}
	if r.IntN(4) == 0 {
		d.MaxSum = 1 + r.IntN(5)
	}
	if r.IntN(5) == 0 {
		d.GroupLimit = 2
	}
	return d
}

// TestCombatCandidatesMatchMapReference: the map-free enumerators agree with
// the map- and fmt.Sprint-keyed reference over randomized attack and block
// decisions and bot answers, with and without a block guard.
func TestCombatCandidatesMatchMapReference(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 5))
	guard := func(c []int) []int { // a deterministic, choosy stand-in guard
		if len(c)%3 == 2 {
			return c[:len(c)-1]
		}
		return c
	}
	for i := 0; i < 20000; i++ {
		d := randomCombatDecision(r)
		k := r.IntN(len(d.Options) + 1)
		bot := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: r.Perm(len(d.Options))[:k]}
		for _, in := range botVariants(d, bot, r) {
			limit := 2 + r.IntN(12)
			checkCandidatesEquivalent(t, d, in, limit, nil)
			checkCandidatesEquivalent(t, d, in, limit, guard)
		}
	}
}

// TestCombatCandidatesMatchMapReferenceOnRealDecisions does the same over
// every attack and block decision of bot-played Legacy-deck games, at 2 and
// 4 seats, with the bot's own block guard bound to the deciding seat's
// board, exactly as azmcts binds it.
func TestCombatCandidatesMatchMapReferenceOnRealDecisions(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := testutil.LegacyDeckNames()
	r := rand.New(rand.NewPCG(9, 13))
	var attacks, blocks int
	for g := 0; g < 10; g++ {
		seats := 2 + 2*(g%2)
		cfg := rules.Config{Seed: uint64(5200 + g), Tokens: reg.Tokens}
		for s := 0; s < seats; s++ {
			name := names[(g*5+s)%len(names)]
			cfg.Names = append(cfg.Names, name)
			cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, name))
		}
		e := rules.New(cfg)
		e.Advance()
		rngs := make([]*rand.Rand, seats)
		for s := range rngs {
			rngs[s] = rand.New(rand.NewPCG(uint64(g), uint64(s)))
		}
		for step := 0; step < 2500 && !e.G.Over && e.Pending() != nil; step++ {
			d := e.Pending()
			b := botpolicy.BoardFromGame(e.G, e, d.Player)
			in := botpolicy.Decide(b, d, rngs[d.Player])
			if d.Kind == decision.KAttackers || d.Kind == decision.KBlockers {
				legal := func(c []int) []int { return botpolicy.LegalBlockChoices(b, d, c) }
				for _, v := range botVariants(d, in, r) {
					for _, limit := range []int{2, 4, 8, 64} {
						checkCandidatesEquivalent(t, d, v, limit, legal)
					}
				}
				if d.Kind == decision.KAttackers {
					attacks++
				} else {
					blocks++
				}
			}
			if err := e.Submit(in); err != nil {
				t.Fatalf("game %d step %d: %v", g, step, err)
			}
		}
	}
	if attacks == 0 || blocks == 0 {
		t.Fatalf("real games posed %d attack and %d block decisions", attacks, blocks)
	}
	t.Logf("checked %d attack and %d block decisions", attacks, blocks)
}
