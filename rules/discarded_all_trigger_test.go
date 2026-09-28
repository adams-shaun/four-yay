package rules

// The batch discard trigger mode, end to end on the real compiled corpus
// (task agent-20260919T183145Z-c6610d46): Mode$ DiscardedAll fires once for
// the WHOLE discard action, with ValidCard$/ValidPlayer$ filtering the batch
// and the number of matching cards bound to the trigger's
// TriggerCount$Amount (Magmakin Artillerist's X), plus its FirstTime$/ and
// ActivationLimit$ once-per-turn cadences.
//
// No Forge script text is committed: every card is fetched from the corpus
// registry, and the discarding hands are authored fixtures (the discard
// primitive's own tests use the same unbuilt fixtures).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// nonlandDiscardCard is an authored nonland fixture card. DiscardedAll's
// ValidCard$ Card.nonLand filter is one of the corpus's four filters, so the
// filter arm needs a card whose land-ness is unambiguous: this is a Creature
// and matches Card.nonLand.
const nonlandDiscardCard = "Name:Discard Fodder\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// landDiscardCard is the land fixture: Card.nonLand must reject it, so a
// batch that discards it alone can never advance a nonland-filtered trigger.
const landDiscardCard = "Name:Discard Land\nTypes:Basic Land\nOracle:x\n"

// discardedAllEngine builds a two-seat game with seat 0 holding want (a real
// corpus card moved onto its battlefield by the caller) over a Mountain deck,
// seat 1 on a Mountain deck, and the corpus token scripts available (Veronica
// and Dying to Serve mint real tokens). It returns the engine, its Config and
// the registry.
func discardedAllEngine(t *testing.T, seed uint64, want *cards.Card) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	e, cfg := tokenReplGame(t, seed, want)
	return e, cfg, reg
}

// seedHand puts one authored fixture (named by src) into p's hand and returns
// its object id.
func seedHand(t *testing.T, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	return onHand(t, e, p, src)
}

// clearHand moves every card in p's hand to the bottom of p's library through
// a real (but trigger-inert) MoveZone, so a subsequent Mode$ Hand discard
// holds exactly the cards the caller seeded. A raw hand->library move carries
// no action marker, so it is not a discard and fires no DiscardedAll -- unlike
// emptying the hand through real discards, which would fire the trigger the
// test is about to measure.
func emptyHandToLibrary(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary, Player: p})
	}
	e.pending = nil
}

// resolveDiscardHand resolves one api:Discard in Mode$ Hand for p -- "discard
// your hand" -- which discards every card in p's hand in one action, with no
// ask and therefore no suspension. It is the discard twin of the mill tests'
// resolveMill, and it is the engine's own effDiscard path, so the Mode$
// DiscardedAll batch bracket is the real one.
func resolveDiscardHand(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	effects.Resolve(e, &effects.Ctx{Controller: p},
		&cards.SA{Kind: "DB", API: "Discard", Params: map[string]string{"Mode": "Hand"}})
	// Opening a fresh priority round places the triggers the discard queued,
	// so the drain below has a decision to pass.
	e.priorityRound()
}

// zoneOfObj is a small readable accessor for a card's zone.
func zoneOfObj(e *Engine, id state.ObjID) state.Zone {
	if o := e.G.Obj(id); o != nil {
		return o.Zone
	}
	return state.Zone(0)
}

// TestDiscardedAllVeronicaFirstTimeBatch pins the batch, ValidCard$ and
// ValidPlayer$ behavior AND FirstTime$ together on Veronica, Dissident
// Scribe: "Whenever you discard one or more nonland cards for the first time
// each turn, create a Junk token."
//
//   - One batch that discards a land AND a nonland creates exactly ONE Junk
//     token: the ValidCard$ Card.nonLand filter admits the nonland only, and
//     the batch fires once for the whole action however many cards it held.
//   - A SECOND, separate discard batch of a nonland the same turn creates no
//     second Junk token: FirstTime$ True.
//   - A discard of an OPPONENT's (seat 1's) hand does not fire Veronica at
//     all: ValidPlayer$ You admits only her controller's discards.
func TestDiscardedAllVeronicaFirstTimeBatch(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	veronica := searchCorpusCard(t, reg, "Veronica, Dissident Scribe")
	e, _, _ := discardedAllEngine(t, 9101, veronica)

	// PRECONDITION: Veronica is actually on seat 0's battlefield, so her
	// TriggerZones$ Battlefield trigger is live for the scan, and it is her
	// controller's discard that ValidPlayer$ You will accept. Seated eventless
	// (onBoardCard) so no other trigger fires on the setup.
	vID := onBoardCard(t, e, 0, veronica)
	if o := e.G.Obj(vID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Veronica id %d zone = %v, want battlefield (vacuous setup)", vID, o)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != 0 {
		t.Fatalf("seat 0 already holds %d Junk tokens (vacuous setup)", got)
	}

	// The first batch: one land and one nonland, so the filter arm is really
	// exercised (a batch of only nonlands could not tell the filter apart
	// from "any discard"). clearHand first, so the batch holds EXACTLY these.
	emptyHandToLibrary(t, e, 0)
	land := seedHand(t, e, 0, landDiscardCard)
	fodder := seedHand(t, e, 0, nonlandDiscardCard)
	if f := e.G.Obj(land).Face(); f == nil || !f.IsLand() {
		t.Fatalf("land fixture face = %+v, want a Land type (vacuous setup)", f)
	}
	if f := e.G.Obj(fodder).Face(); f == nil || f.IsLand() {
		t.Fatalf("fodder fixture face = %+v, want a nonland (vacuous setup)", f)
	}

	resolveDiscardHand(t, e, 0)
	// Both cards really left the hand: the batch was real.
	if zoneOfObj(e, land) != state.ZGraveyard || zoneOfObj(e, fodder) != state.ZGraveyard {
		t.Fatalf("post-batch zones: land=%v fodder=%v, want both in the graveyard",
			zoneOfObj(e, land), zoneOfObj(e, fodder))
	}
	drainMillTrigger(t, e, 40)

	// Exactly one Junk token from the one matching card in the batch.
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != 1 {
		t.Fatalf("after one mixed batch seat 0 holds %d Junk tokens, want 1 (batch + Card.nonLand filter)", got)
	}

	// A SECOND, separate discard action the same turn: FirstTime$ True means
	// no second token. The discard still happens (the card reaches the
	// graveyard), so this is not a "nothing occurred" pass.
	emptyHandToLibrary(t, e, 0)
	second := seedHand(t, e, 0, nonlandDiscardCard)
	resolveDiscardHand(t, e, 0)
	if zoneOfObj(e, second) != state.ZGraveyard {
		t.Fatalf("second nonland zone = %v, want graveyard (the second batch must really run)", zoneOfObj(e, second))
	}
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != 1 {
		t.Fatalf("after a second same-turn batch seat 0 holds %d Junk tokens, want 1 (FirstTime$)", got)
	}

	// ValidPlayer$ You: a discard of OPPONENT seat 1's hand must not fire
	// Veronica. Seat 1's card really is discarded, so the batch is real.
	emptyHandToLibrary(t, e, 1)
	opp := seedHand(t, e, 1, nonlandDiscardCard)
	tokensBefore := countTokensNamedOnSeat(t, e, 0, "Junk Token")
	resolveDiscardHand(t, e, 1)
	if zoneOfObj(e, opp) != state.ZGraveyard {
		t.Fatalf("opponent's card zone = %v, want graveyard (the opponent's batch must really run)", zoneOfObj(e, opp))
	}
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != tokensBefore {
		t.Fatalf("opponent's discard changed seat 0 Junk tokens %d -> %d, want unchanged (ValidPlayer$ You)", tokensBefore, got)
	}
}

// TestDiscardedAllMagmakinAmount pins the batch COUNT: Magmakin Artillerist's
// "Whenever you discard one or more cards, this creature deals that much
// damage to each opponent", whose SVar:X is TriggerCount$Amount.
//
// Two independent engines discard different batch sizes and the damage must
// differ by exactly the batch size, and within one engine a second, distinct
// batch adds its own count rather than being conflated with the first. A
// count that came from anywhere but the batch (0, or the number of events
// since the turn began) fails at least one of these.
func TestDiscardedAllMagmakinAmount(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	magmakin := searchCorpusCard(t, reg, "Magmakin Artillerist")

	// Batch of ONE: one nonland discarded, one damage to seat 1.
	e1, _, _ := discardedAllEngine(t, 9102, magmakin)
	mID := onBoardCard(t, e1, 0, magmakin)
	if o := e1.G.Obj(mID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin id %d zone = %v, want battlefield (vacuous setup)", mID, o)
	}
	before := e1.G.Players[1].Life
	emptyHandToLibrary(t, e1, 0)
	seedHand(t, e1, 0, nonlandDiscardCard)
	resolveDiscardHand(t, e1, 0)
	drainMillTrigger(t, e1, 40)
	if got := e1.G.Players[1].Life; got != before-1 {
		t.Fatalf("batch of 1 dealt %d damage, want 1 (TriggerCount$Amount = batch size)", before-got)
	}

	// Batch of THREE, then a second distinct batch of TWO in the same engine:
	// the totals must be 3 then 3+2 = 5, never 3 then 3 (conflated) and never
	// the running turn total.
	e3, _, _ := discardedAllEngine(t, 9103, magmakin)
	mID3 := onBoardCard(t, e3, 0, magmakin)
	if o := e3.G.Obj(mID3); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin id %d zone = %v, want battlefield (vacuous setup)", mID3, o)
	}
	before3 := e3.G.Players[1].Life
	emptyHandToLibrary(t, e3, 0)
	for i := 0; i < 3; i++ {
		seedHand(t, e3, 0, nonlandDiscardCard)
	}
	resolveDiscardHand(t, e3, 0)
	drainMillTrigger(t, e3, 40)
	if got := e3.G.Players[1].Life; got != before3-3 {
		t.Fatalf("first batch of 3 dealt %d damage, want 3 (TriggerCount$Amount = batch size)", before3-got)
	}
	// The batch fired ONCE: a per-event trigger would push three times.
	if got := triggerPushesFor(e3, mID3); got != 1 {
		t.Fatalf("first 3-card batch pushed %d triggers, want 1 (one trigger per batch)", got)
	}
	for i := 0; i < 2; i++ {
		seedHand(t, e3, 0, nonlandDiscardCard)
	}
	resolveDiscardHand(t, e3, 0)
	drainMillTrigger(t, e3, 40)
	if got := e3.G.Players[1].Life; got != before3-5 {
		t.Fatalf("second batch of 2 left seat 1 at %d, want %d beyond the first batch's 3 (distinct batches not conflated)",
			got, before3-5)
	}
	// Distinct batches: exactly one more trigger, not two and not zero.
	if got := triggerPushesFor(e3, mID3); got != 2 {
		t.Fatalf("two distinct batches pushed %d triggers, want 2 (one each, not conflated)", got)
	}
}

// TestDiscardedAllValidPlayerAnyAndActivationLimit covers the other two
// corpus parameters: ValidPlayer$ Player (Tinybones, Pocket Nuisance, whose
// trigger admits ANY player's discard) and ActivationLimit$ 1 (Dying to
// Serve, "This ability triggers only once each turn", enforced through
// actionTriggerModes).
func TestDiscardedAllValidPlayerAnyAndActivationLimit(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	// ValidPlayer$ Player: an OPPONENT's discard counts. Tinybones deals 1 to
	// each opponent; as seat 0's trigger, seat 1 is the only opponent.
	tinybones := searchCorpusCard(t, reg, "Tinybones, Pocket Nuisance")
	e, _, _ := discardedAllEngine(t, 9104, tinybones)
	tbID := onBoardCard(t, e, 0, tinybones)
	if o := e.G.Obj(tbID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Tinybones id %d zone = %v, want battlefield (vacuous setup)", tbID, o)
	}
	before := e.G.Players[1].Life
	emptyHandToLibrary(t, e, 1)
	seedHand(t, e, 1, nonlandDiscardCard)
	resolveDiscardHand(t, e, 1)
	drainMillTrigger(t, e, 40)
	if got := e.G.Players[1].Life; got != before-1 {
		t.Fatalf("opponent's discard left seat 1 at %d, want %d (ValidPlayer$ Player must admit any player)", got, before-1)
	}

	// ActivationLimit$ 1: two same-turn batches mint one token, not two.
	dts := searchCorpusCard(t, reg, "Dying to Serve")
	e2, _, _ := discardedAllEngine(t, 9105, dts)
	dID := onBoardCard(t, e2, 0, dts)
	if o := e2.G.Obj(dID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Dying to Serve id %d zone = %v, want battlefield (vacuous setup)", dID, o)
	}
	if got := countTokensNamedOnSeat(t, e2, 0, "Zombie Token"); got != 0 {
		t.Fatalf("seat 0 already holds %d Zombie tokens (vacuous setup)", got)
	}
	for batch := 0; batch < 2; batch++ {
		emptyHandToLibrary(t, e2, 0)
		seedHand(t, e2, 0, nonlandDiscardCard)
		resolveDiscardHand(t, e2, 0)
		drainMillTrigger(t, e2, 40)
	}
	if got := countTokensNamedOnSeat(t, e2, 0, "Zombie Token"); got != 1 {
		t.Fatalf("two same-turn batches made %d Zombie tokens, want 1 (ActivationLimit$ 1)", got)
	}
}

// TestDiscardedAllSurvivesSuspensionAcrossAsk pins the one shape the batch
// bracket must survive: api:Discard's Mode$ TgtChoose N=2 asks the discarding
// player mid-resolution, so the discards are emitted on the RESUMED pass, not
// the pass that opened the bracket. If the bracket were closed on the
// suspending return, the resumed discards would be a different (or no) batch
// and the count would be wrong. Mind Rot (a real corpus spell: "Target player
// discards two cards") is cast on the stack so the ask and its resume ride
// the production resolution path; the test fails loudly if no ask was posed.
func TestDiscardedAllSurvivesSuspensionAcrossAsk(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	magmakin := searchCorpusCard(t, reg, "Magmakin Artillerist")
	e, _, _ := discardedAllEngine(t, 9106, magmakin)
	mID := onBoardCard(t, e, 0, magmakin)
	if o := e.G.Obj(mID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Magmakin id %d zone = %v, want battlefield (vacuous setup)", mID, o)
	}
	// Three eligible nonlands so the NumCards$ 2 ask has a real choice.
	emptyHandToLibrary(t, e, 0)
	for i := 0; i < 3; i++ {
		seedHand(t, e, 0, nonlandDiscardCard)
	}
	before := e.G.Players[1].Life

	// Seat 0 casts Mind Rot at itself and resolves it, posing the TgtChoose ask.
	castOnStack(t, e, reg, "Mind Rot", 0, 0)
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "discard" || len(d.Options) < 2 {
		t.Fatalf("discard ask = %+v, want a real TgtChoose suspension (the bracket must span it)", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	// The two chosen cards really left the hand, so the resumed batch was real.
	grave := 0
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if f := e.G.Obj(id).Face(); f != nil && f.Name == "Discard Fodder" {
			grave++
		}
	}
	if grave != 2 {
		t.Fatalf("%d chosen cards reached the graveyard, want 2 (the resumed discard must run)", grave)
	}
	if got := e.G.Players[1].Life; got != before-2 {
		t.Fatalf("a suspended 2-card discard dealt %d damage, want 2 (the batch must span the ask)", before-got)
	}
}

// TestDiscardedAllFirstTimeWithoutBatch pins that FirstTime$ is enforced for a
// discard that is NOT an api:Discard (so no batch bracket opens): a cost or
// cleanup discard is its own batch-of-one, and Veronica's "for the first time
// each turn" must still admit only the first such discard of the turn. Two
// cost discards of nonlands are emitted; only the first may make a Junk.
func TestDiscardedAllFirstTimeWithoutBatch(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	veronica := searchCorpusCard(t, reg, "Veronica, Dissident Scribe")
	e, _, _ := discardedAllEngine(t, 9107, veronica)
	onBoardCard(t, e, 0, veronica)
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != 0 {
		t.Fatalf("seat 0 already holds %d Junk tokens (vacuous setup)", got)
	}
	emptyHandToLibrary(t, e, 0)

	first := seedHand(t, e, 0, nonlandDiscardCard)
	e.emit(events.DiscardCost(first))
	if zoneOfObj(e, first) != state.ZGraveyard {
		t.Fatalf("first cost discard zone = %v, want graveyard (the discard must really run)", zoneOfObj(e, first))
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != 1 {
		t.Fatalf("first cost discard made %d Junk tokens, want 1", got)
	}

	second := seedHand(t, e, 0, nonlandDiscardCard)
	e.emit(events.DiscardCost(second))
	if zoneOfObj(e, second) != state.ZGraveyard {
		t.Fatalf("second cost discard zone = %v, want graveyard (the second discard must really run)", zoneOfObj(e, second))
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Junk Token"); got != 1 {
		t.Fatalf("second same-turn cost discard made %d Junk tokens, want 1 (FirstTime$ outside a batch)", got)
	}
}

// TestDiscardedAllPrimitiveRegistered pins the registration
// (effects.RegisterNonAPI) so a revert of the registration -- not just the
// matcher -- fails loudly rather than leaving the trigger silently inert.
func TestDiscardedAllPrimitiveRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:DiscardedAll"] {
		t.Fatal("primitive trig:DiscardedAll not registered (effects.Supported)")
	}
}
