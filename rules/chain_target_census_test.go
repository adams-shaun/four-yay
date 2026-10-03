package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// knownChainTargetOfferGaps names the corpus spells the chain-target census
// below still finds offered on a board where the cast flow then reverses the
// announcement for want of a legal target. It is a ratchet: it may only
// shrink, and a spell that newly reverses fails the census by name.
var knownChainTargetOfferGaps = map[string]bool{}

// chainTargetCensusSubject reports whether a corpus card's front-face spell
// announces chained targets on cast (CR 601.2c): a Charm (a mode is the
// cast's target root, its chain announced with it) or a SubAbility$ chain
// with a targeting link the cast flow pre-asks (collectSubTargetPreAsks: every
// ValidTgts$ link but the ChangeZone family).
func chainTargetCensusSubject(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return false
	}
	f := c.Faces[0]
	sa := f.SpellAbility()
	if sa == nil {
		return false
	}
	if sa.API == "Charm" {
		return true
	}
	if sa.API == "CopySpellAbility" {
		return false
	}
	for sub := sa.Sub; sub != nil; sub = sub.Sub {
		if strings.TrimSpace(sub.ParamStr(cards.PKValidTgts)) != "" &&
			sub.CompiledAPI() != cards.APIChangeZone && sub.API != "ChangeZone" {
			return true
		}
	}
	return false
}

// TestChainTargetOfferCensusAgreesWithCastFlow is the class census behind
// cardfuzz fuzz-1003's planrev "no legal target for a chained ability"
// (Stand Together, Rookie Mistake, Incremental Growth, Swift Kick, Compel
// Brutality): every corpus spell that announces chained targets on cast is
// dealt onto small boards (no creature, one on either side, one on each, two
// of the caster's), and wherever the offer census offers its plain cast, the
// cast is submitted and its announcements answered with the first legal
// options. A cast the offer admitted must never reverse for want of a legal
// target (CR 601.2c/733.1): the offer census, the payment planner's candidate
// walk (which reads it) and the cast flow's subTargetAsk must agree.
func TestChainTargetOfferCensusAgreesWithCastFlow(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	var subjects []*cards.Card
	for _, c := range reg.Cards {
		if chainTargetCensusSubject(c) {
			subjects = append(subjects, c)
		}
	}
	slices.SortFunc(subjects, func(a, b *cards.Card) int { return strings.Compare(a.Faces[0].Name, b.Faces[0].Name) })
	if len(subjects) < 500 {
		t.Fatalf("census found %d chained-target spells, want the corpus's several hundred", len(subjects))
	}
	boards := []struct{ mine, theirs int }{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {2, 0}}
	var gaps []string
	offered := 0
	for ci, c := range subjects {
		name := c.Faces[0].Name
		for bi, b := range boards {
			if reason, cast := chainCensusCast(t, c, bears, b.mine, b.theirs, uint64(70000+ci*len(boards)+bi)); cast {
				offered++
				if reason != "" && !knownChainTargetOfferGaps[name] {
					gaps = append(gaps, name+": "+reason)
				}
			}
		}
	}
	t.Logf("chain-target census: %d spells, %d offered casts", len(subjects), offered)
	if len(gaps) != 0 {
		t.Fatalf("%d offered casts reversed for want of a legal target:\n%s", len(gaps), strings.Join(gaps, "\n"))
	}
}

// chainCensusCast deals c into seat 0's hand over the given creature board,
// funds the pool and, when the plain cast is offered, casts it with the first
// legal answers. cast reports whether it was offered; reason is the abort
// note when the announcement reversed for want of a legal target.
func chainCensusCast(t *testing.T, c, bears *cards.Card, mine, theirs int, seed uint64) (reason string, cast bool) {
	t.Helper()
	deck0 := []*cards.Card{c}
	for i := 0; i < mine; i++ {
		deck0 = append(deck0, bears)
	}
	deck1 := []*cards.Card{}
	for i := 0; i < theirs; i++ {
		deck1 = append(deck1, bears)
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck0, mountainDeck(t, 40-len(deck0))...), append(deck1, mountainDeck(t, 40-len(deck1))...)}}
	e := New(cfg)
	id := moveByName(t, e, 0, c.Faces[0].Name, state.ZHand)
	for i := 0; i < mine; i++ {
		moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	}
	for i := 0; i < theirs; i++ {
		moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.Advance()
	edrSeatZeroPriority(t, e)
	for _, r := range "WWWWWUUUUUBBBBBRRRRRGGGGGCCCCC" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.pending = nil
	e.priorityRound()
	edrSeatZeroPriority(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		return "", false
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == "" && o.AltCostIndex == 0 {
			idx = o.Index
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	from := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("%s: submit cast: %v", c.Faces[0].Name, err)
	}
	for step := 0; step < 40; step++ {
		d = e.Pending()
		if d == nil || d.Kind == decision.KPriority || d.Player != 0 {
			break
		}
		if !chainCensusAnswer(e, d) {
			break
		}
	}
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "no legal target") {
			return ev.Text, true
		}
	}
	return "", true
}

// chainCensusAnswer answers one cast-flow decision with the first legal
// options: a target or mode ask its minimum (at least one when one exists),
// anything else the first answer Validate accepts among none, the first
// option, or the first two.
func chainCensusAnswer(e *Engine, d *decision.Decision) bool {
	try := func(choices []int) bool {
		return e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}) == nil
	}
	first := func(n int) []int {
		out := make([]int, 0, n)
		for i := 0; i < n && i < len(d.Options); i++ {
			out = append(out, d.Options[i].Index)
		}
		return out
	}
	if d.Kind == decision.KTarget || d.Kind == decision.KModes {
		n := d.Min
		if n < 1 && len(d.Options) > 0 {
			n = 1
		}
		if try(first(n)) {
			return true
		}
	}
	for _, n := range []int{0, 1, 2} {
		if try(first(n)) {
			return true
		}
	}
	return false
}
