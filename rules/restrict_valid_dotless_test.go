package rules

// The dotless RestrictValid$ payment classes (the restricted-mana spend
// task): bare `Spell` (Klauth's encoding), bare `Activated` (Omen Hawker's),
// and the cost-keyed terms — CostContainsC read from a COMMA TAIL,
// CostContainsX, CantPayGenericCosts, CantCastNonArtifactSpells,
// CantCastSpellFromHand and CumulativeUpkeep. Each pin drives the REAL corpus
// producer's own mana ability through the real offer/payment wheel: the
// restricted batch is emitted by the card, never fabricated, and every
// admission/rejection is asserted both at the pool view (manaAvailableFor)
// and behaviourally at the priority decision.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Inline-authored payment fixtures (never corpus .txt — the licensing rule):
// the PRODUCERS of every restricted batch below are real corpus cards; these
// are the payers whose spell/activation/cost shape the term under test keys
// on.
const (
	dotlessCharge2     = "Name:Splash Tithe\nManaCost:2\nTypes:Instant\nOracle:x\n"
	dotlessChargeC     = "Name:Plain Charge\nManaCost:C\nTypes:Instant\nOracle:x\n"
	dotlessCharge1     = "Name:One Toll\nManaCost:1\nTypes:Instant\nOracle:x\n"
	dotlessCharge1W    = "Name:White Toll\nManaCost:1 W\nTypes:Instant\nOracle:x\n"
	dotlessChargeWU    = "Name:Duo Praise\nManaCost:W U\nTypes:Instant\nOracle:x\n"
	dotlessTwobrid     = "Name:Twobrid Toll\nManaCost:2/W\nTypes:Instant\nOracle:x\n"
	dotlessArtifactU   = "Name:Clockwork Beetle\nManaCost:U\nTypes:Artifact Creature Insect\nPT:1/1\nOracle:x\n"
	dotlessCreatureU   = "Name:Blue Whelp\nManaCost:U\nTypes:Creature Drake\nPT:1/1\nOracle:x\n"
	dotlessCBearer     = "Name:C Testbearer\nManaCost:W\nTypes:Creature Human Cleric\nPT:1/1\nA:AB$ Draw | Cost$ C | SpellDescription$ Draw a card.\nOracle:x\n"
	dotlessFlashback   = "Name:Grave Rite\nManaCost:W\nTypes:Instant\nK:Flashback:2\nA:SP$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n"
	dotlessGlareTarget = "Name:Wall Target\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	// dotlessClerk is the activation PAYER fixture: a {1}-only (tap-free)
	// counter ability is legal on a summoning-sick creature (CR 302.6 gates
	// only {T} costs), so a raw battlefield move needs no state write.
	dotlessClerk = "Name:Sunrise Clerk\nManaCost:U\nTypes:Creature Human Wizard\nPT:1/1\nA:AB$ PutCounter | Cost$ 1 | CounterType$ P1P1 | CounterNum$ 1\nOracle:x\n"
)

func dotlessDeck(t *testing.T, reg *cards.Registry, front ...*cards.Card) [][]*cards.Card {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, len(front)+88)
	deck = append(deck, front...)
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	return [][]*cards.Card{deck, opp}
}

func dotlessEngine(t *testing.T, reg *cards.Registry, front ...*cards.Card) (*Engine, Config) {
	t.Helper()
	cfg := seatZeroStart(Config{Seed: 9231, Names: []string{"restrictor", "opponent"},
		Decks: dotlessDeck(t, reg, front...), Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

func dotlessEngineE(t *testing.T, reg *cards.Registry, front ...*cards.Card) *Engine {
	t.Helper()
	cfg := seatZeroStart(Config{Seed: 9231, Names: []string{"restrictor", "opponent"},
		Decks: dotlessDeck(t, reg, front...), Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e
}

// findOption returns the index of the pending decision's option of kind kind
// carrying Obj obj, or -1 when absent.
func findOption(d *decision.Decision, kind string, obj state.ObjID) int {
	if d == nil {
		return -1
	}
	for _, o := range d.Options {
		if o.Kind == kind && o.Obj == obj {
			return o.Index
		}
	}
	return -1
}

// tapForRestrictedBatch activates src's mana ability whose RestrictValid$
// value is exactly wantValid, through the real priority and mana-wheel chain.
func tapForRestrictedBatch(t *testing.T, e *Engine, src state.ObjID, wantValid string) {
	t.Helper()
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority for %d's mana: %+v", src, d)
	}
	act := findOption(d, "activate", src)
	if act < 0 {
		t.Fatalf("no mana activation offered for %d: %+v", src, d.Options)
	}
	submitChoices(t, e, act)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "mana" {
		// A single-ability source skips the wheel entirely (the activation's
		// only ability is played outright); the caller's batch precondition
		// still proves WHICH ability produced.
		return
	}
	idx := -1
	for i, ab := range e.G.Obj(src).Face().ManaAbilities() {
		if ab.Params["RestrictValid"] == wantValid {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("%d's face carries no mana ability with RestrictValid$ %q", src, wantValid)
	}
	wheel := -1
	for _, o := range d.Options {
		if o.Ability == idx {
			wheel = o.Index
		}
	}
	if wheel < 0 {
		t.Fatalf("no wheel option for ability %d: %+v", idx, d.Options)
	}
	submitChoices(t, e, wheel)
}

// restrictedBatchesOf returns seat p's restricted batches whose Valid is
// exactly valid.
func restrictedBatchesOf(e *Engine, p state.PlayerID, valid string) []state.ManaRestriction {
	var out []state.ManaRestriction
	for _, b := range e.G.Players[p].RestrictedMana {
		if b.Valid == valid {
			out = append(out, b)
		}
	}
	return out
}

// restrictedBatchUnits sums a batch list's units.
func restrictedBatchUnits(batches []state.ManaRestriction) int32 {
	var n int32
	for _, b := range batches {
		n += b.Amount
	}
	return n
}

// leaveCombatWithNoBlockers crosses a declared attack into the following
// priority: the combat windows pass (the bare attacker deals no damage),
// combat ends, and the loop stops at seat 0's next priority decision.
func leaveCombatWithNoBlockers(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			e.priorityRound()
			d = e.Pending()
		}
		if d == nil {
			t.Fatal("no decision pending while leaving combat")
		}
		if d.Kind == decision.KPriority && d.Player == 0 && e.G.Step != state.StepDeclareAttackers && e.G.Step != state.StepDeclareBlockers {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KChoose, decision.KModes:
			// Klauth's real Combo Any trigger allocates every produced unit.
			// Pick every W option, preserving the test's all-white restricted
			// batch while submitting the exact allocation the decision requires.
			if d.Kind == decision.KChoose && d.Options[0].Kind == "mana" {
				if d.Options[0].Label != "Add W" {
					t.Fatalf("Klauth mana choice first option = %+v, want Add W", d.Options[0])
				}
				var white []int
				for _, option := range d.Options {
					if option.Label == "Add W" {
						white = append(white, option.Index)
					}
				}
				if len(white) != d.Min {
					t.Fatalf("Klauth white allocation = %+v, want %d W options", d.Options, d.Min)
				}
				submitChoices(t, e, white...)
				break
			}
			submitChoices(t, e, d.Options[0].Index)
		default:
			t.Fatalf("unexpected %s decision while leaving combat: %+v", d.Kind, d)
		}
	}
	t.Fatal("did not reach seat 0's post-combat priority")
}

// TestRestrictValidDotlessSpell pins the bare `Spell` term on its real corpus
// producer: Klauth, Unrivaled Ancient's attack trigger
// (`PersistentMana$ True | RestrictValid$ Spell`) — the persistent batch is
// admitted for a spell payment (and consumed from the restricted batch,
// shrinking it), while an activated-ability payment never sees it, before or
// after the spell spend.
func TestRestrictValidDotlessSpell(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := dotlessEngine(t, reg,
		searchCorpusCard(t, reg, "Klauth, Unrivaled Ancient"),
		card(t, dotlessClerk),
		card(t, dotlessCharge2))
	kl := searchMoveByName(t, e, "Klauth, Unrivaled Ancient", state.ZBattlefield)
	clerk := searchMoveByName(t, e, "Sunrise Clerk", state.ZBattlefield)
	charge := searchMoveByName(t, e, "Splash Tithe", state.ZHand)

	// The producer is the REAL trigger: Klauth has haste, so turn 1 runs a
	// real combat — declare the attack and let the TrigMana resolve. Its
	// Combo Any asks for a colour; this flow selects the first W option, so
	// each unit carries the bare "Spell" restriction and persistent marker.
	passToKind(t, e, decision.KAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, kl)
	drainCombatPriority(t, e)
	leaveCombatWithNoBlockers(t, e)
	batches := restrictedBatchesOf(e, 0, "Spell")
	if len(batches) != 1 || batches[0].Color != "W" || batches[0].Amount != 4 || !batches[0].Persistent {
		t.Fatalf("test precondition: Klauth's attack batch = %+v, want one persistent 4×{W} Spell batch", batches)
	}
	if per := e.G.Players[0].PersistentMana[state.MW]; per != 4 {
		t.Fatalf("test precondition: persistent W tally = %d, want 4", per)
	}

	// Rejection: an activated-ability payment never sees the batch — the
	// clerk's {1} counter ability is generic-payable from any mana, so a
	// leaked class would surface as an offered option.
	if got := e.manaAvailableFor(0, paymentFor(clerk, true, e.parseCost("1"))).pool.Total(); got != 0 {
		t.Fatalf("manaAvailableFor(clerk activation) = %d, want 0 (Spell batch hidden)", got)
	}
	e.pending = nil
	e.priorityRound()
	if findOption(e.Pending(), "ability", clerk) >= 0 || findOption(e.Pending(), "activate", clerk) >= 0 {
		t.Fatalf("the {1} activation was offered on a bare-Spell-only pool: %+v", e.Pending())
	}

	// Admission: the {2} instant's cast is offered on the batch alone and
	// paid FROM the restricted batch — the batch and the persistent tally
	// shrink by the cost, the pool with them.
	if got := e.manaAvailableFor(0, paymentFor(charge, false, e.parseCost("2"))).pool.Total(); got != 4 {
		t.Fatalf("manaAvailableFor({2} spell) = %d, want 4 (Spell batch admitted)", got)
	}
	e.pending = nil
	e.priorityRound()
	castIdx := findOption(e.Pending(), "cast", charge)
	if castIdx < 0 {
		t.Fatalf("precondition: the {2} cast was not offered on the admitted pool: %+v", e.Pending())
	}
	submitChoices(t, e, castIdx)
	if o := e.G.Obj(charge); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: the spell did not reach the stack paid: %+v", o)
	}
	passUntilStackEmpty(t, e, 30)
	batches = restrictedBatchesOf(e, 0, "Spell")
	if len(batches) != 1 || batches[0].Color != "W" || batches[0].Amount != 2 {
		t.Fatalf("after the spell payment the batch = %+v, want one 2×{W} Spell batch (consumed from the restricted batch)", batches)
	}
	if got, per := e.G.Players[0].Pool[state.MW], e.G.Players[0].PersistentMana[state.MW]; got != 2 || per != 2 {
		t.Fatalf("after the spell payment pool W=%d persistent W=%d, want 2/2", got, per)
	}

	// And the surviving batch still cannot pay the activation.
	e.pending = nil
	e.priorityRound()
	if findOption(e.Pending(), "ability", clerk) >= 0 || findOption(e.Pending(), "activate", clerk) >= 0 {
		t.Fatalf("the surviving Spell batch leaked into the {1} activation after the spell payment: %+v", e.Pending())
	}
	replayCheck(t, e, cfg)
}

// TestRestrictValidDotlessActivated pins the bare `Activated` term on its
// real corpus producer: Omen Hawker's `{T}: Add {C}{U}. Spend this mana only
// to activate abilities.` The batch pays Cryptic Trilobite's {1},{T}
// counter ability — a genuine activated-ability payment, consumed from the
// restricted batch — and never funds a spell payment of the same shape.
// TestRestrictValidSpellDoesNotPayOtherCosts pins the context-free payment
// boundary shared by ward, unless-pay, attack and triggered-cost callers: it
// has no cast descriptor, so Klauth's bare Spell batch must be hidden.
func TestRestrictValidSpellDoesNotPayOtherCosts(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Klauth, Unrivaled Ancient"))
	source := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(source); o == nil || o.Face() == nil || o.Face().Name != "Klauth, Unrivaled Ancient" {
		t.Fatalf("test precondition: source = %+v, want Klauth", e.G.Obj(source))
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1,
		Text: events.ManaRestrictionText("Spell", source)})
	if batches := restrictedBatchesOf(e, 0, "Spell"); len(batches) != 1 || batches[0].Amount != 1 {
		t.Fatalf("test precondition: restricted batch = %+v, want one bare Spell R", batches)
	}
	if e.payMana(0, ParseCost("R")) {
		t.Fatal("bare Spell restricted mana paid a context-free payment")
	}
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("after the rejected other payment pool R=%d, want 1", got)
	}
}

func TestRestrictValidDotlessActivated(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := dotlessEngine(t, reg,
		searchCorpusCard(t, reg, "Omen Hawker"),
		card(t, dotlessClerk),
		card(t, dotlessChargeC))
	hawker := searchMoveByName(t, e, "Omen Hawker", state.ZBattlefield)
	// A real logged TurnChange (not a raw SummonSick write) clears CR 302.6
	// sickness on the log the replayCheck below reads from.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	tri := searchMoveByName(t, e, "Sunrise Clerk", state.ZBattlefield)
	spell := searchMoveByName(t, e, "Plain Charge", state.ZHand)

	tapForRestrictedBatch(t, e, hawker, "Activated")
	batches := restrictedBatchesOf(e, 0, "Activated")
	if len(batches) != 2 || restrictedBatchUnits(batches) != 2 {
		t.Fatalf("test precondition: Omen Hawker's batch(es) = %+v, want two 1-unit Activated batches ({C} and {U})", batches)
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("test precondition: pool total = %d, want 2", got)
	}

	// Admission: the clerk's {1} counter ability — a real activated-ability
	// payment — is offered on the restricted pool alone and paid from a
	// restricted batch (two batches shrink to one, the pool with them).
	e.pending = nil
	e.priorityRound()
	abIdx := findOption(e.Pending(), "ability", tri)
	if abIdx < 0 {
		t.Fatalf("precondition: the {1},{T} activation was not offered on the admitted pool: %+v", e.Pending())
	}
	submitChoices(t, e, abIdx)
	passUntilStackEmpty(t, e, 30)
	batches = restrictedBatchesOf(e, 0, "Activated")
	if len(batches) != 1 || restrictedBatchUnits(batches) != 1 {
		t.Fatalf("after the activation payment the batches = %+v, want one 1-unit Activated batch (consumed from the restricted batch)", batches)
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("after the activation payment pool total = %d, want 1", got)
	}

	// Rejection: the same restricted pool no longer funds a SPELL payment —
	// the {C} instant stays hidden from manaAvailableFor and unoffered.
	if got := e.manaAvailableFor(0, paymentFor(spell, false, e.parseCost("C"))).pool.Total(); got != 0 {
		t.Fatalf("manaAvailableFor({C} spell) = %d, want 0 (Activated batch hidden)", got)
	}
	e.pending = nil
	e.priorityRound()
	if findOption(e.Pending(), "cast", spell) >= 0 {
		t.Fatalf("the {C} spell was offered on an Activated-only pool: %+v", e.Pending())
	}
	replayCheck(t, e, cfg)
}

// TestRestrictValidDotlessPaymentTerms pins the cost-keyed and remaining
// dotless terms, each on its real corpus producer's own batch: CostContainsC
// from a comma tail (Cultivator Drone), CostContainsX (Rosheen Meanderer,
// announced and paid through the real X ask), CantPayGenericCosts (Jegantha,
// with the hybrid/twobrid/X pin), CantCastNonArtifactSpells (Hydraulic
// Helper), CantCastSpellFromHand (Vhal, paid by a genuine from-graveyard
// flashback cast), and CumulativeUpkeep (Adarkar Unicorn, paying Mystic
// Remora's real upkeep window but no ordinary payment).
func TestRestrictValidDotlessPaymentTerms(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	t.Run("CostContainsC comma tail", func(t *testing.T) {
		t.Parallel()
		drone := searchCorpusCard(t, reg, "Cultivator Drone")
		e, cfg := dotlessEngine(t, reg, drone, drone, card(t, dotlessCBearer), card(t, dotlessCharge1W))
		cult := searchMoveByName(t, e, "Cultivator Drone", state.ZBattlefield)
		cult2 := searchMoveByName(t, e, "Cultivator Drone", state.ZBattlefield)
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log replayCheck below reads from, for both
		// drones at once.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		bearer := searchMoveByName(t, e, "C Testbearer", state.ZBattlefield)
		toll := searchMoveByName(t, e, "White Toll", state.ZHand)
		if cult == cult2 {
			t.Fatal("test precondition: the two drones must be distinct objects")
		}

		tapForRestrictedBatch(t, e, cult, "Spell.Colorless,Activated.Permanent+Colorless+inZoneBattlefield,CostContainsC")
		batches := restrictedBatchesOf(e, 0, "Spell.Colorless,Activated.Permanent+Colorless+inZoneBattlefield,CostContainsC")
		if len(batches) != 1 || batches[0].Color != "C" || batches[0].Amount != 1 {
			t.Fatalf("test precondition: Cultivator Drone's batch = %+v, want one 1×{C} batch", batches)
		}

		// Admission through the COMMA TAIL only: the white bearer's {C}
		// activation is neither a colorless spell (term 1) nor an activation
		// of a colorless permanent (term 2 — the bearer is white), so only
		// the tail's CostContainsC can admit it.
		e.pending = nil
		e.priorityRound()
		abIdx := findOption(e.Pending(), "ability", bearer)
		if abIdx < 0 {
			t.Fatalf("the white bearer's {C} activation was not offered — the comma-tail CostContainsC term is unread: %+v", e.Pending())
		}
		submitChoices(t, e, abIdx)
		passUntilStackEmpty(t, e, 30)
		if got := restrictedBatchesOf(e, 0, "Spell.Colorless,Activated.Permanent+Colorless+inZoneBattlefield,CostContainsC"); len(got) != 0 {
			t.Fatalf("after the {C} activation payment the batch(es) = %+v, want none (consumed from the restricted batch)", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("after the {C} activation payment pool total = %d, want 0", got)
		}

		// Rejection: a fresh batch plus two ordinary {C} — the {1}{W}
		// instant is a WHITE spell with no {C} pip and no colourless
		// spell/activation shape, so the batch must stay hidden and the cast
		// unoffered on 2 visible mana (it would be payable if the tail
		// wrongly admitted the generic half).
		tapForRestrictedBatch(t, e, cult2, "Spell.Colorless,Activated.Permanent+Colorless+inZoneBattlefield,CostContainsC")
		addMana(t, e, 0, "CC")
		if got := e.manaAvailableFor(0, paymentFor(toll, false, e.parseCost("1 W"))).pool.Total(); got != 2 {
			t.Fatalf("manaAvailableFor({1}{W} spell) = %d, want 2 (the CostContainsC tail must not admit a C-less cost)", got)
		}
		e.pending = nil
		e.priorityRound()
		if findOption(e.Pending(), "cast", toll) >= 0 {
			t.Fatalf("the {1}{W} cast was offered with a {C}-restricted batch in the pool: %+v", e.Pending())
		}
		replayCheck(t, e, cfg)
	})

	t.Run("CostContainsX", func(t *testing.T) {
		t.Parallel()
		rosheen := searchCorpusCard(t, reg, "Rosheen Meanderer")
		e, cfg := dotlessEngine(t, reg, rosheen,
			searchCorpusCard(t, reg, "Kaervek's Torch"), card(t, dotlessChargeC))
		ros := searchMoveByName(t, e, "Rosheen Meanderer", state.ZBattlefield)
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log replayCheck below reads from.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		glare := searchMoveByName(t, e, "Kaervek's Torch", state.ZHand)
		charge := searchMoveByName(t, e, "Plain Charge", state.ZHand)

		tapForRestrictedBatch(t, e, ros, "CostContainsX")
		batches := restrictedBatchesOf(e, 0, "CostContainsX")
		if len(batches) != 1 || batches[0].Amount != 4 {
			t.Fatalf("test precondition: Rosheen's batch(es) = %+v, want one 4×{C} CostContainsX batch", batches)
		}

		// Rejection: the {C} instant carries no X, so the batch is hidden
		// from its payment and the cast unoffered on the batch alone.
		if got := e.manaAvailableFor(0, paymentFor(charge, false, e.parseCost("C"))).pool.Total(); got != 0 {
			t.Fatalf("manaAvailableFor({C} spell) = %d, want 0 (CostContainsX batch hidden)", got)
		}
		e.pending = nil
		e.priorityRound()
		if findOption(e.Pending(), "cast", charge) >= 0 {
			t.Fatalf("the {C} spell was offered on a CostContainsX-only pool: %+v", e.Pending())
		}

		// Admission: the real X cast — offered on the batch (plus one
		// ordinary {R} for the pip), X announced through the real ask, paid
		// from the restricted batch.
		addMana(t, e, 0, "R")
		e.pending = nil
		e.priorityRound()
		castIdx := findOption(e.Pending(), "cast", glare)
		if castIdx < 0 {
			t.Fatalf("precondition: the {X}{B} cast was not offered on the admitted pool: %+v", e.Pending())
		}
		submitChoices(t, e, castIdx)
		d := e.Pending()
		if d == nil || d.Options[0].Kind != "x" {
			t.Fatalf("the X announce ask did not follow the cast: %+v", d)
		}
		xIdx := -1
		for _, o := range d.Options {
			if o.Kind == "x" && o.Amount == 2 {
				xIdx = o.Index
			}
		}
		if xIdx < 0 {
			t.Fatalf("no X = 2 option on the announced bound: %+v", d.Options)
		}
		submitChoices(t, e, xIdx)
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("the target ask did not follow the X announcement: %+v", d)
		}
		tgtIdx := -1
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == 1 {
				tgtIdx = o.Index
			}
		}
		if tgtIdx < 0 {
			t.Fatalf("the opponent was not offered as the torch's target: %+v", d.Options)
		}
		submitChoices(t, e, tgtIdx)
		passUntilStackEmpty(t, e, 30)
		batches = restrictedBatchesOf(e, 0, "CostContainsX")
		if len(batches) != 1 || batches[0].Amount != 2 {
			t.Fatalf("after the X payment the batches = %+v, want one 2×{C} batch left (X = 2 was paid from the restricted batch)", batches)
		}
		if got, r := e.G.Players[0].Pool.Total(), e.G.Players[0].Pool[state.MR]; got != 2 || r != 0 {
			t.Fatalf("after the X payment pool total = %d (R=%d), want 2/0", got, r)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("CantPayGenericCosts", func(t *testing.T) {
		t.Parallel()
		jeg := searchCorpusCard(t, reg, "Jegantha, the Wellspring")
		e, cfg := dotlessEngine(t, reg, jeg, card(t, dotlessChargeWU), card(t, dotlessCharge1), card(t, dotlessTwobrid),
			searchCorpusCard(t, reg, "Kaervek's Torch"))
		src := searchMoveByName(t, e, "Jegantha, the Wellspring", state.ZBattlefield)
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log replayCheck below reads from.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		wu := searchMoveByName(t, e, "Duo Praise", state.ZHand)
		toll := searchMoveByName(t, e, "One Toll", state.ZHand)
		twobrid := searchMoveByName(t, e, "Twobrid Toll", state.ZHand)
		torch := searchMoveByName(t, e, "Kaervek's Torch", state.ZHand)

		tapForRestrictedBatch(t, e, src, "CantPayGenericCosts")
		batches := restrictedBatchesOf(e, 0, "CantPayGenericCosts")
		if len(batches) != 5 || restrictedBatchUnits(batches) != 5 {
			t.Fatalf("test precondition: Jegantha's batch(es) = %+v, want five 1-unit batches (W U B R G)", batches)
		}

		// Rejection, generic: the {1} instant's pip may be paid generic, so
		// the batch is hidden and the cast unoffered.
		if got := e.manaAvailableFor(0, paymentFor(toll, false, e.parseCost("1"))).pool.Total(); got != 0 {
			t.Fatalf("manaAvailableFor({1} spell) = %d, want 0 (generic cost)", got)
		}
		// An unannounced flexible pip remains offerable: its colour face has
		// no generic component. The generic face is rejected at the real
		// announcement ask, after that face has been folded into the cost.
		if got := e.manaAvailableFor(0, paymentFor(twobrid, false, e.parseCost("2/W"))).pool.Total(); got != 5 {
			t.Fatalf("manaAvailableFor(unannounced {2/W}) = %d, want 5 (its {W} face is non-generic)", got)
		}
		e.pending = nil
		e.priorityRound()
		if findOption(e.Pending(), "cast", toll) >= 0 {
			t.Fatalf("the {1} cast was offered on a CantPayGenericCosts-only pool: %+v", e.Pending())
		}
		castIdx := findOption(e.Pending(), "cast", twobrid)
		if castIdx < 0 {
			t.Fatalf("precondition: the {2/W} cast was not offered for its non-generic face: %+v", e.Pending())
		}
		submitChoices(t, e, castIdx)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("precondition: the twobrid face ask = %+v", d)
		}
		payW, payGeneric := -1, -1
		for _, o := range d.Options {
			switch o.Kind {
			case "pay_W":
				payW = o.Index
			case "pay_generic":
				payGeneric = o.Index
			}
		}
		if payW < 0 || payGeneric >= 0 {
			t.Fatalf("twobrid choices = %+v, want only the {W} face (generic must be withheld)", d.Options)
		}
		submitChoices(t, e, payW)
		passUntilStackEmpty(t, e, 30)
		batches = restrictedBatchesOf(e, 0, "CantPayGenericCosts")
		if len(batches) != 4 || restrictedBatchUnits(batches) != 4 {
			t.Fatalf("after the {W} twobrid payment the batches = %+v, want four 1-unit batches left", batches)
		}

		// The actual X announcement is likewise limited to X=0: raw X may
		// still choose zero, while every positive choice folds generic mana
		// into the cost and is withheld.
		e.pending = nil
		e.priorityRound()
		castIdx = findOption(e.Pending(), "cast", torch)
		if castIdx < 0 {
			t.Fatalf("precondition: the X spell was not offered for X=0: %+v", e.Pending())
		}
		submitChoices(t, e, castIdx)
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("precondition: the X ask = %+v", d)
		}
		xZero, xPositive := -1, -1
		for _, o := range d.Options {
			if o.Kind == "x" && o.Amount == 0 {
				xZero = o.Index
			}
			if o.Kind == "x" && o.Amount > 0 {
				xPositive = o.Index
			}
		}
		if xZero < 0 || xPositive >= 0 {
			t.Fatalf("X choices = %+v, want X=0 only (positive X pays generic)", d.Options)
		}
		submitChoices(t, e, xZero)
		d = e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("precondition: the X=0 target ask = %+v", d)
		}
		submitChoices(t, e, d.Options[0].Index)
		passUntilStackEmpty(t, e, 30)

		// Admission: {W}{U} has no generic component and is paid from the
		// remaining restricted batch. Add one ordinary W because the twobrid
		// face already consumed Jegantha's W batch.
		addMana(t, e, 0, "W")
		e.pending = nil
		e.priorityRound()
		castIdx = findOption(e.Pending(), "cast", wu)
		if castIdx < 0 {
			t.Fatalf("precondition: the {W}{U} cast was not offered on the admitted pool: %+v", e.Pending())
		}
		submitChoices(t, e, castIdx)
		if o := e.G.Obj(wu); o == nil || o.Zone != state.ZStack {
			t.Fatalf("precondition: the {W}{U} spell did not reach the stack paid: %+v", o)
		}
		passUntilStackEmpty(t, e, 30)
		batches = restrictedBatchesOf(e, 0, "CantPayGenericCosts")
		if len(batches) != 2 || restrictedBatchUnits(batches) != 2 {
			t.Fatalf("after the {W}{U} payment the batches = %+v, want two 1-unit batches left (R G)", batches)
		}
		if got := e.G.Players[0].Pool.Total(); got != 2 {
			t.Fatalf("after the {W}{U} payment pool total = %d, want 2", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("CantCastNonArtifactSpells", func(t *testing.T) {
		t.Parallel()
		hh := searchCorpusCard(t, reg, "Hydraulic Helper")
		artifact := card(t, dotlessArtifactU)
		creature := card(t, dotlessCreatureU)
		e, cfg := dotlessEngine(t, reg, hh, hh, artifact, creature)
		src := searchMoveByName(t, e, "Hydraulic Helper", state.ZBattlefield)
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log replayCheck below reads from.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		artID := searchMoveByName(t, e, "Clockwork Beetle", state.ZHand)
		whelpID := searchMoveByName(t, e, "Blue Whelp", state.ZHand)
		if !e.G.Obj(artID).Face().IsArtifact() || e.G.Obj(whelpID).Face().IsArtifact() {
			t.Fatalf("test precondition: artifactness must differ: artifact=%v creature=%v",
				e.G.Obj(artID).Face().IsArtifact(), e.G.Obj(whelpID).Face().IsArtifact())
		}

		tapForRestrictedBatch(t, e, src, "CantCastNonArtifactSpells")
		batches := restrictedBatchesOf(e, 0, "CantCastNonArtifactSpells")
		if len(batches) != 1 || batches[0].Color != "ArtifactU" || batches[0].Amount != 1 {
			t.Fatalf("test precondition: Hydraulic Helper's batch = %+v, want one 1×{U} Artifact batch", batches)
		}

		// Rejection: the nonartifact {U} creature's cast is hidden and
		// unoffered on the batch alone.
		if got := e.manaAvailableFor(0, paymentFor(whelpID, false, e.parseCost("U"))).pool.Total(); got != 0 {
			t.Fatalf("manaAvailableFor(nonartifact spell) = %d, want 0", got)
		}
		e.pending = nil
		e.priorityRound()
		if findOption(e.Pending(), "cast", whelpID) >= 0 {
			t.Fatalf("the nonartifact cast was offered on a CantCastNonArtifactSpells-only pool: %+v", e.Pending())
		}

		// Admission: the artifact {U} creature's cast is offered and paid
		// from the batch.
		e.pending = nil
		e.priorityRound()
		castIdx := findOption(e.Pending(), "cast", artID)
		if castIdx < 0 {
			t.Fatalf("precondition: the artifact cast was not offered on the admitted pool: %+v", e.Pending())
		}
		submitChoices(t, e, castIdx)
		if o := e.G.Obj(artID); o == nil || o.Zone != state.ZStack {
			t.Fatalf("precondition: the artifact spell did not reach the stack paid: %+v", o)
		}
		passUntilStackEmpty(t, e, 30)
		if got := restrictedBatchesOf(e, 0, "CantCastNonArtifactSpells"); len(got) != 0 {
			t.Fatalf("after the artifact payment the batch(es) = %+v, want none (consumed from the restricted batch)", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("after the artifact payment pool total = %d, want 0", got)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("CantCastSpellFromHand", func(t *testing.T) {
		t.Parallel()
		vhal := searchCorpusCard(t, reg, "Vhal, Candlekeep Researcher")
		e, cfg := dotlessEngine(t, reg, vhal, card(t, dotlessFlashback), card(t, dotlessCharge1))
		src := searchMoveByName(t, e, "Vhal, Candlekeep Researcher", state.ZBattlefield)
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log replayCheck below reads from.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		rite := searchMoveByName(t, e, "Grave Rite", state.ZHand)
		toll := searchMoveByName(t, e, "One Toll", state.ZHand)

		tapForRestrictedBatch(t, e, src, "CantCastSpellFromHand")
		batches := restrictedBatchesOf(e, 0, "CantCastSpellFromHand")
		if len(batches) != 1 || batches[0].Amount != 3 {
			t.Fatalf("test precondition: Vhal's batch(es) = %+v, want one 3×{C} batch (toughness 3)", batches)
		}

		// Rejection: a hand spell's cast is denied PRE-push (the hand family
		// keeps the deny — the cast's ByYou origin is not yet in the log), so
		// the batch is hidden from the {1} cast and the cast unoffered.
		if got := e.manaAvailableFor(0, paymentFor(toll, false, e.parseCost("1"))).pool.Total(); got != 0 {
			t.Fatalf("manaAvailableFor(hand {1} spell) = %d, want 0 (hand cast denied at the offer)", got)
		}
		e.pending = nil
		e.priorityRound()
		if findOption(e.Pending(), "cast", toll) >= 0 {
			t.Fatalf("the hand {1} cast was offered on a CantCastSpellFromHand-only pool: %+v", e.Pending())
		}

		// Admission: a genuine NON-hand cast — the flashback {2} from the
		// graveyard — is paid from the batch, which empties with the payment.
		moveToGraveyard(t, e, rite)
		if o := e.G.Obj(rite); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: Grave Rite not in the graveyard: %+v", o)
		}
		if got := e.G.Players[0].Pool.Total(); got != 3 {
			t.Fatalf("precondition: pool total after the graveyard move = %d, want 3", got)
		}
		castMode(t, e, rite, "flashback")
		if o := e.G.Obj(rite); o == nil || o.Zone != state.ZStack {
			t.Fatalf("precondition: the flashback cast did not reach the stack paid: %+v", o)
		}
		passUntilStackEmpty(t, e, 30)
		got := restrictedBatchesOf(e, 0, "CantCastSpellFromHand")
		if len(got) != 1 || got[0].Amount != 1 {
			t.Fatalf("after the flashback payment the batch(es) = %+v, want one 1×{C} batch left (the {2} was paid from the batch)", got)
		}
		if pool := e.G.Players[0].Pool.Total(); pool != 1 {
			t.Fatalf("after the flashback payment pool total = %d, want 1", pool)
		}
		if o := e.G.Obj(rite); o == nil || o.Zone != state.ZExile {
			t.Fatalf("postcondition: the flashback card was not exiled: %+v", o)
		}
		replayCheck(t, e, cfg)
	})

	t.Run("CumulativeUpkeep", func(t *testing.T) {
		t.Parallel()
		uni := searchCorpusCard(t, reg, "Adarkar Unicorn")
		e, cfg := dotlessEngine(t, reg, uni, searchCorpusCard(t, reg, "Mystic Remora"), card(t, dotlessCharge1))
		unicorn := searchMoveByName(t, e, "Adarkar Unicorn", state.ZBattlefield)
		remora := searchMoveByName(t, e, "Mystic Remora", state.ZBattlefield)
		toll := searchMoveByName(t, e, "One Toll", state.ZHand)

		// The REAL window the term names: Remora's cumulative-upkeep trigger
		// is on the stack, and the unicorn's {C}{U} is activated at that
		// priority — the real play the mana exists for.
		driveToStep(t, e, 3, 0, state.StepUpkeep)
		e.priorityRound()
		if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability.API != "CumulativeUpkeep" {
			t.Fatalf("test precondition: Remora's cumulative trigger not on the stack: %v", e.G.Stack)
		}
		tapForRestrictedBatch(t, e, unicorn, "CumulativeUpkeep")
		batches := restrictedBatchesOf(e, 0, "CumulativeUpkeep")
		if len(batches) != 2 || restrictedBatchUnits(batches) != 2 {
			t.Fatalf("test precondition: Adarkar Unicorn's batch(es) = %+v, want two 1-unit CumulativeUpkeep batches", batches)
		}

		// Rejection: neither an ordinary spell nor an activation sees the
		// batch — the {1} cast stays hidden and unoffered while the trigger
		// waits.
		if got := e.manaAvailableFor(0, paymentFor(toll, false, e.parseCost("1"))).pool.Total(); got != 0 {
			t.Fatalf("manaAvailableFor({1} spell) = %d, want 0 (CumulativeUpkeep batch hidden)", got)
		}
		e.pending = nil
		e.priorityRound()
		if findOption(e.Pending(), "cast", toll) >= 0 {
			t.Fatalf("the {1} cast was offered on a CumulativeUpkeep-only pool: %+v", e.Pending())
		}

		// Admission: the window itself — the trigger resolves straight to the
		// pay-or-sacrifice ask (the batch already pays {1}, so no mana window
		// opens), and the payment consumes a restricted batch.
		e.resolveTop()
		d := e.Pending()
		if d == nil || len(d.Options) == 0 || d.Options[0].Kind != "cumulative_pay" {
			t.Fatalf("precondition: the cumulative pay-or-sacrifice ask = %+v (the batch must make pay offered)", d)
		}
		submitChoices(t, e, d.Options[0].Index)
		if o := e.G.Obj(remora); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("postcondition: Remora did not survive the paid upkeep: %+v", o)
		}
		batches = restrictedBatchesOf(e, 0, "CumulativeUpkeep")
		if len(batches) != 1 || restrictedBatchUnits(batches) != 1 {
			t.Fatalf("after the cumulative payment the batches = %+v, want one 1-unit CumulativeUpkeep batch left (the {1} was paid from the batch)", batches)
		}
		if got := e.G.Players[0].Pool.Total(); got != 1 {
			t.Fatalf("after the cumulative payment pool total = %d, want 1", got)
		}
		// And the survivor still pays nothing else: the ordinary {1} cast is
		// still hidden on the remaining batch.
		if got := e.manaAvailableFor(0, paymentFor(toll, false, e.parseCost("1"))).pool.Total(); got != 0 {
			t.Fatalf("manaAvailableFor({1} spell) after the window = %d, want 0", got)
		}
		replayCheck(t, e, cfg)
	})
}
