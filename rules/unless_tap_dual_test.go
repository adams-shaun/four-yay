package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the unless-cost payment window against the REAL corpus cards
// the brief names (Mana Leak/Daze/Spell Pierce/Chain Lightning) and the real
// dual lands in the repo decks. The pre-fix window walked
// windowManaUnits, which dropped every permanent with more than one free mana
// ability -- so an untapped Volcanic Island (intrinsics {U} and {R}) could not
// pay even a one-pip tax, and the pay option was suppressed. The window now
// offers each production alternative as its own option, so the tap selects a
// colour with no nested ask, and the offer gate searches over those
// alternatives rather than their (wrong) sum.

// TestUnlessCostPayableRealDualLandAlternatives is the offer-gate pin: one
// untapped dual land with an EMPTY pool must make a one-colour tax payable by
// EITHER of its colours, and must NOT make a two-pip tax payable (it taps for
// one of its colours, never both).
func TestUnlessCostPayableRealDualLandAlternatives(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Volcanic Island"))
	e.G.Players[0].Pool = state.Mana{}

	dual := mustCorpusCard(t, reg, "Volcanic Island")
	id := onBoardCard(t, e, 0, dual)

	// Precondition: the dual is an untapped payment-window source AND the
	// membership walk sees it with TWO production alternatives ({U} and {R}).
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("dual land precondition failed: %+v", o)
	}
	if !e.untappedManaSource(0, id) {
		t.Fatal("Volcanic Island is not a payment-window source -- premise false")
	}
	units := e.windowManaUnits(0)
	if len(units) != 1 || len(units[0].alts) != 2 {
		t.Fatalf("Volcanic Island window membership = %+v, want one unit with two alts", units)
	}

	// The one-pip taxes are reachable through the dual's either colour.
	if !e.UnlessCostPayable(0, "U") {
		t.Fatal("an untapped Volcanic Island did not make a {U} unless tax payable")
	}
	if !e.UnlessCostPayable(0, "R") {
		t.Fatal("an untapped Volcanic Island did not make a {R} unless tax payable")
	}
	// Both colours at once is two pips from one permanent's single tap: the
	// alternatives are a choice, never a sum.
	if e.UnlessCostPayable(0, "U R") {
		t.Fatal("one Volcanic Island made {U}{R} payable; it taps for one colour")
	}
}

// TestCounterDazePaysFromRealDualLand is the end-to-end pin: a real Daze
// counters a real creature spell, the pool-empty payer controls ONE untapped
// Volcanic Island, and the payer taps it (choosing {U}) through the window to
// pay Daze's {1}. Before the fix the dual was dropped from the window, the pay
// option was suppressed, and the creature was countered.
func TestCounterDazePaysFromRealDualLand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, bearID := counterFixture(t, reg, "Daze", "Grizzly Bears")
	e.G.Players[0].Pool = state.Mana{}

	dual := mustCorpusCard(t, reg, "Volcanic Island")
	id := onBoardCard(t, e, 0, dual)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("dual land precondition failed: %+v", o)
	}
	if !e.untappedManaSource(0, id) {
		t.Fatal("Volcanic Island is not a payment-window source -- premise false")
	}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Daze")
	}
	// The {1} is reachable from the dual, so the pay branch must be offered.
	if len(pay.Options) != 2 || pay.Options[0].Index != 0 {
		t.Fatalf("Daze did not offer a payable pay/decline election: %+v", pay.Options)
	}
	submitChoices(t, e, pay.Options[0].Index)

	// The mana window must offer the dual's colour alternatives.
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_mana" {
		t.Fatalf("expected the unless_mana window after choosing pay, got %+v", d)
	}
	tapIdx := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == id {
			tapIdx = opt.Index
			break
		}
	}
	if tapIdx < 0 {
		t.Fatalf("the window omitted the untapped dual land: %+v", d.Options)
	}
	submitChoices(t, e, tapIdx)
	// Volcanic Island has two mana abilities, so choosing its source opens the
	// normal mana-ability choice. Choose its {U} alternative, then finish the
	// enclosing unless window with Done.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Source != id {
		t.Fatalf("expected Volcanic Island's mana-ability choice, got %+v", d)
	}
	blue := -1
	for _, opt := range d.Options {
		if opt.Kind == "mana" && opt.Label == "Add U" {
			blue = opt.Index
			break
		}
	}
	if blue < 0 {
		t.Fatalf("Volcanic Island did not offer its {U} ability: %+v", d.Options)
	}
	submitChoices(t, e, blue)
	d = e.Pending()
	if d == nil || d.ResumeKind != "unless_mana" || len(d.Options) != 1 || d.Options[0].Kind != "done" {
		t.Fatalf("expected final unless_mana Done window, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)

	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(bearID).Zone; z != state.ZBattlefield {
		t.Fatalf("Daze paid from the dual land but the Bear zone = %s", z)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("the dual land was not tapped to pay the unless cost")
	}
}

// TestUnlessCostSacComponentUnpayableNotOffered is the review's second finding:
// the offer gate must not offer Pay for a non-mana component the payer cannot
// satisfy. With no creatures and no mana, the real `Sac<1/Creature>` unless
// token (Kardur's Vicious Return) is unpayable; with a creature it is payable.
// Before the fix the gate returned true for every parsed non-mana cost.
func TestUnlessCostSacComponentUnpayableNotOffered(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	kardur := mustCorpusCard(t, reg, "Kardur's Vicious Return")
	raw := "Sac<1/Creature>"
	if !cardSVarCarries(kardur, "UnlessCost$ "+raw) {
		t.Fatalf("Kardur's Vicious Return does not carry %q -- premise false", "UnlessCost$ "+raw)
	}
	creature := mustCorpusCard(t, reg, "Grizzly Bears")

	e := handEngine(t, kardur, creature)
	e.G.Players[0].Pool = state.Mana{}
	if e.UnlessCostPayable(0, raw) {
		t.Fatalf("%q offered with no creatures on the battlefield", raw)
	}

	// Positive control: a real creature makes the same cost payable.
	cid := onBoardCard(t, e, 0, creature)
	if o := e.G.Obj(cid); o.Zone != state.ZBattlefield {
		t.Fatalf("creature precondition failed: %+v", o)
	}
	if !e.UnlessCostPayable(0, raw) {
		t.Fatalf("%q not offered with a creature available to sacrifice", raw)
	}
}

// cardSVarCarries reports whether any of the card's faces has an SVar body
// containing the given token, so a test can prove the raw UnlessCost$ text it
// exercises is the real corpus script.
func cardSVarCarries(c *cards.Card, token string) bool {
	for _, f := range c.Faces {
		for _, body := range f.SVars {
			if strings.Contains(body, token) {
				return true
			}
		}
	}
	return false
}

// TestUnlessWindowOptionsAreBotLegal asserts the new per-alternative window
// still offers a legal KChoose answer: every option is a valid index and the
// bot-policy-shaped answer (the first activate) passes Decision.Validate. This
// is the no-livelock guarantee for the added options.
func TestUnlessWindowOptionsAreBotLegal(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, _ := counterFixture(t, reg, "Daze", "Grizzly Bears")
	e.G.Players[0].Pool = state.Mana{}
	dual := mustCorpusCard(t, reg, "Volcanic Island")
	onBoardCard(t, e, 0, dual)

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Daze")
	}
	submitChoices(t, e, pay.Options[0].Index)
	d := e.Pending()
	if d == nil || d.ResumeKind != "unless_mana" {
		t.Fatalf("expected the unless_mana window, got %+v", d)
	}
	// Every option index is its slot (ask's own invariant), and the first
	// activate option is a legal single-choice answer.
	firstActivate := -1
	for i, opt := range d.Options {
		if opt.Index != i {
			t.Fatalf("option %d has Index %d", i, opt.Index)
		}
		if opt.Kind == "activate" && firstActivate < 0 {
			firstActivate = opt.Index
		}
	}
	if firstActivate < 0 {
		t.Fatalf("no activate option offered: %+v", d.Options)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{firstActivate}}); err != nil {
		t.Fatalf("bot-shaped activate answer rejected: %v", err)
	}
}
