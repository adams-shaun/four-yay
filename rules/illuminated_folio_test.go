package rules

// Illuminated Folio's {1}, {T}, Reveal two cards from your hand that share a
// color: Draw a card. (ticket agent-20260928T173851Z-05a69598). SameColor is
// a relation BETWEEN the revealed cards, not a card filter: read as one, no
// hand card ever matched, nonManaCastable returned false for every board and
// the ability was withheld forever. The fix reads the part relationally
// (isSameColorRevealSpec): the offer gate measures the largest shared-colour
// class among the eligible hand cards, and the payment ask carries
// decision.SetPropShared over DERIVED colour tokens, so Decision.Validate and
// botpolicy's Clamp enforce the same rule the offer gate measured.
//
// No Forge script text is committed here; the carrier comes from the compiled
// .cards corpus (grep -rlE 'Reveal<2/SameColor>' .cards/cardsfolder names
// exactly one file, illuminated_folio.txt).

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// folioSweepHand moves every hand card except the named keeps back to the
// library through the REAL MoveZone path, so a test's hand is deterministic:
// the padded random forests of paidCostEngine's shuffle must not be able to
// complete a colour pair by accident.
func folioSweepHand(t *testing.T, e *Engine, p state.PlayerID, keep ...state.ObjID) {
	t.Helper()
	for _, id := range slices.Clone(e.G.Zone(state.ZHand, p)) {
		if slices.Contains(keep, id) {
			continue
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
	e.pending = nil
	e.priorityRound()
}

// folioAbilityOption reports whether the pending priority decision offers the
// folio's ability, and returns the option index.
func folioAbilityOption(t *testing.T, e *Engine, folio state.ObjID) (int, bool) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == folio && (o.Kind == "activate" || o.Kind == "ability") && o.Ability == 0 {
			return o.Index, true
		}
	}
	return -1, false
}

// folioColoursOf is the derived colour read the fix uses (never Face().Colors).
func folioColoursOf(t *testing.T, e *Engine, id state.ObjID) string {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("object %d missing", id)
	}
	return effects.ColorsOf(o)
}

// TestIlluminatedFolioSameColorRevealCost pins the end-to-end behaviour: with
// two green cards in hand the ability is offered, the reveal ask is answered
// with the green pair, the {1} is paid and the ability resolves and draws a
// card. A third, red card keeps the reveal ask posed (more candidates than N).
// A non-sharing answer is rejected by Decision.Validate (the ONE rule
// botpolicy's Clamp reads) and the ask survives, so no client can livelock.
func TestIlluminatedFolioSameColorRevealCost(t *testing.T) {
	t.Parallel()
	e, cfg := paidCostEngine(t, []string{"Illuminated Folio", "Grizzly Bears", "Giant Growth", "Hill Giant"}, []string{"Ancient Brontodon"})
	folio := paidCostMoveTo(t, e, 0, "Illuminated Folio", state.ZBattlefield)
	bears := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	growth := paidCostMoveTo(t, e, 0, "Giant Growth", state.ZHand)
	hill := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZHand)
	folioSweepHand(t, e, 0, bears, growth, hill)
	if got := folioColoursOf(t, e, hill); got != "R" {
		t.Fatalf("precondition: Hill Giant colours = %q, want R", got)
	}

	if o := e.G.Obj(folio); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: folio = %+v, want an untapped battlefield permanent", o)
	}
	bc, gc := folioColoursOf(t, e, bears), folioColoursOf(t, e, growth)
	if bc == "" || gc == "" || bc != gc {
		t.Fatalf("precondition: Grizzly Bears (%q) and Giant Growth (%q) must share a derived colour", bc, gc)
	}
	if n := len(e.G.Zone(state.ZHand, 0)); n != 3 {
		t.Fatalf("precondition: hand = %d cards, want exactly the three fixtures", n)
	}
	c := ParseCost("Reveal<2/SameColor>")
	if len(c.Unknown) != 0 || len(c.Reveal) != 1 || c.Reveal[0].N != 2 || !isSameColorRevealSpec(c.Reveal[0].Spec) {
		t.Fatalf("precondition: parsed cost = %+v, want one Reveal<2/SameColor>, no Unknown", c)
	}

	// The offer gate: the cost is payable with this hand.
	if !e.nonManaCastable(0, folio, ParseCost("Reveal<2/SameColor>"), true) {
		t.Fatal("nonManaCastable refuses Reveal<2/SameColor> with two same-colour cards in hand")
	}

	// The REAL offer path: fund {1} and reach priority; the ability is offered.
	addMana(t, e, 0, "C")
	idx, offered := folioAbilityOption(t, e, folio)
	if !offered {
		t.Fatal("the folio's ability was not offered although two same-colour cards are in hand")
	}
	submitChoices(t, e, idx)

	// The payment ask: 2..2, SetPropShared, both green cards offered.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 {
		t.Fatalf("no 2..2 reveal ask pending: %+v", d)
	}
	if d.SetPropMode != decision.SetPropShared {
		t.Fatalf("reveal ask SetPropMode = %q, want shared", d.SetPropMode)
	}
	bearsIdx, growthIdx := -1, -1
	for _, o := range d.Options {
		if o.Kind != "revealcost" {
			t.Fatalf("option kind = %q, want revealcost", o.Kind)
		}
		if o.Obj == bears {
			bearsIdx = o.Index
		}
		if o.Obj == growth {
			growthIdx = o.Index
		}
	}
	if bearsIdx < 0 || growthIdx < 0 {
		t.Fatalf("reveal ask does not offer both green cards: %+v", d.Options)
	}
	if len(d.Options) != 3 {
		t.Fatalf("reveal ask offers %d options, want 3", len(d.Options))
	}

	// An illegal (single-pick) answer is rejected and the ask survives.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{bearsIdx}}); err == nil {
		t.Fatal("a one-card reveal answer was accepted, want rejection")
	}
	if d2 := e.Pending(); d2 == nil || d2.Seq != d.Seq {
		t.Fatal("the reveal ask did not survive a rejected answer")
	}
	submitChoices(t, e, bearsIdx, growthIdx)

	// A reveal does not move the cards; the ability resolves and draws.
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZHand {
		t.Fatalf("revealed card zone = %+v, want hand", o)
	}
	passUntilStackEmpty(t, e, 30)
	if n := len(e.G.Zone(state.ZHand, 0)); n != 4 {
		t.Fatalf("hand size after resolving = %d, want 4 (three fixtures + the drawn card)", n)
	}
	replayCheck(t, e, cfg)
}

// TestIlluminatedFolioNotOfferedWithoutSharedColor pins the other leg: with
// no two same-colour cards in hand the cost is not payable, so the ability is
// not offered. Two colourless cards do NOT share a colour (CR 106.1).
func TestIlluminatedFolioNotOfferedWithoutSharedColor(t *testing.T) {
	t.Parallel()
	e, _ := paidCostEngine(t, []string{"Illuminated Folio", "Grizzly Bears", "Chromatic Sphere", "Mishra's Bauble", "Giant Growth"}, []string{"Mountain"})
	folio := paidCostMoveTo(t, e, 0, "Illuminated Folio", state.ZBattlefield)
	bears := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	sphere := paidCostMoveTo(t, e, 0, "Chromatic Sphere", state.ZHand)
	folioSweepHand(t, e, 0, bears, sphere)

	if got := folioColoursOf(t, e, bears); got != "G" {
		t.Fatalf("precondition: Grizzly Bears colours = %q, want G", got)
	}
	if got := folioColoursOf(t, e, sphere); got != "" {
		t.Fatalf("precondition: Chromatic Sphere colours = %q, want empty", got)
	}
	if e.nonManaCastable(0, folio, ParseCost("Reveal<2/SameColor>"), true) {
		t.Fatal("a coloured and a colourless card were treated as sharing a colour")
	}

	// Two colourless cards still share nothing: the ability must not be
	// offered on the real priority path either.
	bauble := paidCostMoveTo(t, e, 0, "Mishra's Bauble", state.ZHand)
	if got := folioColoursOf(t, e, bauble); got != "" {
		t.Fatalf("precondition: Mishra's Bauble colours = %q, want empty", got)
	}
	addMana(t, e, 0, "C")
	if _, offered := folioAbilityOption(t, e, folio); offered {
		t.Fatal("the folio's ability was offered with no same-colour pair in hand")
	}

	// Positive control, so this test can FAIL: introduce a second green card
	// and the very same gate must flip to payable. Without this, a build that
	// simply never registers Reveal<2/SameColor> would pass the assertions
	// above vacuously.
	growth := paidCostMoveTo(t, e, 0, "Giant Growth", state.ZHand)
	if got := folioColoursOf(t, e, growth); got != "G" {
		t.Fatalf("precondition: Giant Growth colours = %q, want G", got)
	}
	if !e.nonManaCastable(0, folio, ParseCost("Reveal<2/SameColor>"), true) {
		t.Fatal("with two green cards in hand the cost must be payable again")
	}
}

// TestIlluminatedFolioRevealAskRejectsIllegalPair runs the constraint where it
// BINDS: three cards in hand (two green, one red) means three options; every
// non-sharing answer is rejected through the real Submit/Validate path and the
// legal answer still works afterwards (no livelock).
func TestIlluminatedFolioRevealAskRejectsIllegalPair(t *testing.T) {
	t.Parallel()
	e, cfg := paidCostEngine(t, []string{"Illuminated Folio", "Grizzly Bears", "Giant Growth", "Hill Giant"}, []string{"Mountain"})
	folio := paidCostMoveTo(t, e, 0, "Illuminated Folio", state.ZBattlefield)
	bears := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	growth := paidCostMoveTo(t, e, 0, "Giant Growth", state.ZHand)
	hill := paidCostMoveTo(t, e, 0, "Hill Giant", state.ZHand)
	folioSweepHand(t, e, 0, bears, growth, hill)

	if got := folioColoursOf(t, e, hill); got != "R" {
		t.Fatalf("precondition: Hill Giant colours = %q, want R", got)
	}

	addMana(t, e, 0, "C")
	idx, offered := folioAbilityOption(t, e, folio)
	if !offered {
		t.Fatal("the folio's ability was not offered")
	}
	submitChoices(t, e, idx)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.SetPropMode != decision.SetPropShared {
		t.Fatalf("the ask is not a SetPropShared KChoose: %+v", d)
	}
	if len(d.Options) != 3 {
		t.Fatalf("reveal ask offers %d options, want 3", len(d.Options))
	}
	byObj := map[state.ObjID]int{}
	for _, o := range d.Options {
		byObj[o.Obj] = o.Index
	}
	bi, gi, hi := byObj[bears], byObj[growth], byObj[hill]
	if bi < 0 || gi < 0 || hi < 0 {
		t.Fatalf("reveal ask does not offer all three cards: %+v", d.Options)
	}
	for _, illegal := range [][]int{{bi, hi}, {gi, hi}} {
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: illegal}); err == nil {
			t.Fatalf("illegal answer %v accepted", illegal)
		}
	}
	if d2 := e.Pending(); d2 == nil || d2.Seq != d.Seq {
		t.Fatal("the reveal ask did not survive the rejected answers")
	}
	submitChoices(t, e, bi, gi)
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestIlluminatedFolioRevealAskAutoSettles pins the exactly-N path: when the
// hand holds EXACTLY two cards and they share a colour, no ask is posed -- the
// reveal settles automatically (the capacity check above it proves the pair
// shares, so the auto-settle can never pay an illegal reveal).
func TestIlluminatedFolioRevealAskAutoSettles(t *testing.T) {
	t.Parallel()
	e, cfg := paidCostEngine(t, []string{"Illuminated Folio", "Grizzly Bears", "Giant Growth"}, []string{"Mountain"})
	folio := paidCostMoveTo(t, e, 0, "Illuminated Folio", state.ZBattlefield)
	bears := paidCostMoveTo(t, e, 0, "Grizzly Bears", state.ZHand)
	growth := paidCostMoveTo(t, e, 0, "Giant Growth", state.ZHand)
	folioSweepHand(t, e, 0, bears, growth)

	bc, gc := folioColoursOf(t, e, bears), folioColoursOf(t, e, growth)
	if bc == "" || gc == "" || bc != gc {
		t.Fatalf("precondition: the two cards must share a derived colour (%q vs %q)", bc, gc)
	}

	addMana(t, e, 0, "C")
	idx, offered := folioAbilityOption(t, e, folio)
	if !offered {
		t.Fatal("the folio's ability was not offered")
	}
	submitChoices(t, e, idx)

	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		for _, o := range d.Options {
			if o.Kind == "revealcost" {
				t.Fatalf("reveal ask posed although there were exactly N candidates: %+v", d.Options)
			}
		}
	}
	// The reveal was announced as a cost (the Note emitChoiceCosts emits;
	// the name order follows the hand's zone order, so match on the IDs).
	var sawReveal bool
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.HasPrefix(ev.Text, "revealed ") &&
			strings.HasSuffix(ev.Text, " as a cost") &&
			slices.Contains(ev.IDs, bears) && slices.Contains(ev.IDs, growth) {
			sawReveal = true
		}
	}
	if !sawReveal {
		t.Fatal("no public reveal Note for the green pair in the log")
	}
	passUntilStackEmpty(t, e, 30)
	if n := len(e.G.Zone(state.ZHand, 0)); n != 3 {
		t.Fatalf("hand size after resolving = %d, want 3", n)
	}
	replayCheck(t, e, cfg)
}
