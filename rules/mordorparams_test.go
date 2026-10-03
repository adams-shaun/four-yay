package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Task mordorparams1 — five small unread parameters from the Hosts of Mordor
// census. Every test drives the real corpus card (compiled scripts only; no
// Forge script text is committed here), the search_library_test.go /
// commander_colour_identity_test.go way.

// mordorEngine deals seat 0 a 40-card deck led by the named fixtures (deck
// order is shuffled at genesis, so callers bring cards to hand with
// searchMoveByName) and seat 1 a mountain-only deck, then drives to seat 0's
// turn-1 Main1.
func mordorEngine(t *testing.T, reg *cards.Registry, seed uint64, seat0 ...string) (*Engine, Config) {
	t.Helper()
	deck := make([]*cards.Card, 0, 40)
	for _, name := range seat0 {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	opp := mountainDeck(t, 40)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"mordor", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// drainPriorities submits pass on every priority decision until a
// non-priority decision is pending (or the limit trips), and returns it.
func drainPriorities(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision (stack %d)", len(e.G.Stack))
		}
		if d.Kind != decision.KPriority {
			return d
		}
		passOnce(t, e)
	}
	t.Fatalf("no non-priority decision within %d passes (stack %d)", limit, len(e.G.Stack))
	return nil
}

// castNoDrain submits the cast option for id (and answers a target ask with
// option 0 if one appears) WITHOUT draining the stack — castFixture resolves
// the spell, which the Arcane Denial flow must not do.
func castNoDrain(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
}

// mordorMove is searchMoveByName for any seat: the named card moves from
// hand (or library) to the target zone through a logged MoveZone.
func mordorMove(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from seat %d's hand/library", name, p)
	return 0
}

// driveMordor drives across turns to the named step, answering the
// bookkeeping decisions the drive crosses: passes, empty declarations, and
// cleanup-step discards (CR 514.1 — the first Max options in hand order).
// A target or modal ask during the drive is unexpected and fails loudly.
func driveMordor(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 8000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before turn %d seat %d step %s", turn, active, step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
			if d == nil {
				t.Fatal("no decision pending while driving")
			}
		}
		switch d.Kind {
		case decision.KPriority:
			passOnce(t, e)
		case decision.KAttackers, decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KChoose:
			n := d.Max
			if n > len(d.Options) {
				n = len(d.Options)
			}
			choices := make([]int, 0, n)
			for j := 0; j < n; j++ {
				choices = append(choices, d.Options[j].Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("submit choose %v: %v", choices, err)
			}
		default:
			t.Fatalf("unexpected decision %v while driving: %+v", d.Kind, d)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s", turn, active, step)
}

// drainTriggersThenAsk answers any trigger_order ask (both delays of the
// Arcane Denial slowtrip fire simultaneously) by keeping the offered order,
// then passes priorities until the next non-priority decision and returns it.
func drainTriggersThenAsk(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision (stack %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KTriggerOrder {
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			submitChoices(t, e, choices...)
			continue
		}
		if d.Kind != decision.KPriority {
			return d
		}
		passOnce(t, e)
	}
	t.Fatalf("no ask within %d passes", limit)
	return nil
}

// activateAbility submits the activation option for id's ability.
func activateAbility(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority for the activation of %d: %+v", id, d)
	}
	idx := -1
	for _, o := range d.Options {
		// The non-mana ability option ("ability") outranks the mana
		// activation ("activate") when the face carries both — the caller
		// names the ability it wants by being at priority with the source
		// untapped, and a same-Obj mana activation would tap it for mana
		// instead of running the ability.
		if o.Kind == "ability" && o.Obj == id {
			idx = o.Index
			break
		}
		if o.Kind == "activate" && o.Obj == id && idx < 0 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no activate/ability option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
}

// submitObj submits the option of kind kind carrying Obj obj.
func submitObj(t *testing.T, e *Engine, kind string, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == kind && o.Obj == obj {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no %s option for %d: %+v", kind, obj, d.Options)
}

// --- item 3: Choices$ Player.withMostLife (a PIN of the already-fixed read) ---

// TestBlackGateChoosePlayerOffersTheTiedLeaders pins the withMostLife
// player-spec read end to end (effects/filter.go's playerHasMost, landed
// 2026-09-14): tied life totals offer BOTH players; a strict leader offers
// only the leader. This is a pin of an already-working read — if it fails
// the premise moved.
func TestBlackGateChoosePlayerOffersTheTiedLeaders(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := mordorEngine(t, reg, 4101, "The Black Gate", "Grizzly Bears")
	// The gate is PLAYED (the real land-drop flow, not a raw move): its
	// "As ... enters, you may pay 3 life" replacement poses its ask inside
	// the flow, and a raw MoveZone would clobber the posed ask.
	gate := searchMoveByName(t, e, "The Black Gate", state.ZHand)
	e.priorityRound()
	d := e.Pending()
	gateIdx := castOptionIdx(d, "play_land", gate)
	if gateIdx < 0 {
		t.Fatalf("no play_land option for the gate: %+v", d.Options)
	}
	submitChoices(t, e, gateIdx)

	// The entry replacement asks "pay 3 life?" — decline (entering tapped is
	// fine; the turn-3 untap covers the first activation, turn 5 the second).
	d = drainPriorities(t, e, 8)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("entry pay-3-life ask = %+v, want KModes", d)
	}
	submitChoices(t, e, d.Options[len(d.Options)-1].Index) // the decline option
	// A creature on the battlefield so the chained DBEffect's target ask has
	// a legal target.
	moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
	driveMordor(t, e, 3, 0, state.StepMain1)

	fundPool(t, e, "BB") // {1}{B}, {T}: the generic and the black pip
	activateAbility(t, e, gate)
	// CR 602.2b / 601.2c: "Target creature can't be blocked ..." is a target,
	// so it is announced as the ability is ACTIVATED -- before the player
	// choice, which is made as the ability resolves.
	if tgt := castSubAsk(t, e); len(tgt.Options) == 0 || tgt.Options[0].Kind == "player" {
		t.Fatalf("expected the CantBlock creature target ask on activation, got %+v", tgt)
	}
	submitChoices(t, e, e.Pending().Options[0].Index)
	d = drainPriorities(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("withMostLife ask = %+v, want KChoose 1-of", d)
	}
	var offered []state.PlayerID
	for _, o := range d.Options {
		if o.Kind != "player" {
			t.Fatalf("non-player option in a ChoosePlayer ask: %+v", d.Options)
		}
		offered = append(offered, o.Player)
	}
	sort.Slice(offered, func(i, j int) bool { return offered[i] < offered[j] })
	if len(offered) != 2 || offered[0] != 0 || offered[1] != 1 {
		t.Fatalf("tied 20/20 offered %v, want both seats", offered)
	}
	submitChoices(t, e, d.Options[0].Index)

	// The chained Effect's target was announced on activation, so the
	// resolution finishes without another ask.
	if len(e.G.Stack) != 0 {
		t.Fatalf("the ability did not finish resolving after the player choice: stack %v, pending %+v", e.G.Stack, e.Pending())
	}

	// Seat 1 falls behind; the next activation offers only seat 0.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -3})
	driveMordor(t, e, 5, 0, state.StepMain1)
	fundPool(t, e, "BB")
	activateAbility(t, e, gate)
	castSubAsk(t, e)
	submitChoices(t, e, e.Pending().Options[0].Index)
	d = drainPriorities(t, e, 20)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("second withMostLife ask = %+v, want KChoose", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "player" || d.Options[0].Player != 0 {
		t.Fatalf("17-vs-20 offered %+v, want only seat 0", d.Options)
	}
	replayCheck(t, e, cfg)
}

// --- item 5: ConditionDefined$ Discarded ---

// amassTokenEvents reports the log's Orc-Army amass footprint.
func amassArmyCount(t *testing.T, e *Engine) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && (strings.Contains(ev.Text, "orc_army") || strings.Contains(ev.Text, "army")) {
			n++
		}
	}
	return n
}

// TestMoriaScavengerAmassesOnlyOnCreatureDiscard activates the real corpus
// card's draw ability twice: discarding a creature amasses Orcs 1 (an Army
// with a +1/+1 counter exists), discarding a noncreature does not. The gate
// is the cost-discard log channel (rules/stack.go DiscardedInWindow) feeding
// effects' ConditionDefined$ Discarded.
func TestMoriaScavengerAmassesOnlyOnCreatureDiscard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := mordorEngine(t, reg, 4102, "Moria Scavenger", "Grizzly Bears", "Forest")
	moria := moveToBattlefieldByName(t, e, 0, "Moria Scavenger")
	creature := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	nonCreature := searchMoveByName(t, e, "Forest", state.ZHand)
	driveMordor(t, e, 3, 0, state.StepMain1)

	// First activation: discard the creature.
	activateAbility(t, e, moria)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "discard" {
		t.Fatalf("cost discard ask = %+v", d)
	}
	submitObj(t, e, "discard", creature)
	passUntilStackEmpty(t, e, 30)
	if n := amassArmyCount(t, e); n != 1 {
		t.Fatalf("creature discard amassed %d armies, want 1 (log %v)", n, e.L.Events[len(e.L.Events)-8:])
	}
	army := state.ObjID(0)
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Face() != nil && strings.Contains(o.Face().Name, "Army") {
			army = o.ID
		}
	}
	if army == 0 {
		t.Fatal("no Army permanent after the creature-discard amass")
	}
	counted := false
	for _, ct := range e.G.Obj(army).Counters {
		if ct.Kind == "P1P1" && ct.N >= 1 {
			counted = true
		}
	}
	if !counted {
		t.Fatalf("the Army carries no +1/+1 counter: %+v", e.G.Obj(army).Counters)
	}

	// Second activation: discard the noncreature — no amass.
	driveMordor(t, e, 5, 0, state.StepMain1)
	activateAbility(t, e, moria)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "discard" {
		t.Fatalf("second cost discard ask = %+v", d)
	}
	submitObj(t, e, "discard", nonCreature)
	passUntilStackEmpty(t, e, 30)
	if n := amassArmyCount(t, e); n != 1 {
		t.Fatalf("noncreature discard changed the army count to %d, want 1", n)
	}
	for _, ct := range e.G.Obj(army).Counters {
		if ct.Kind == "P1P1" && ct.N != 1 {
			t.Fatalf("the noncreature discard amassed anyway: %+v", e.G.Obj(army).Counters)
		}
	}
	replayCheck(t, e, cfg)
}

// --- item 1: Upto$ True on DB$ Draw ---

// TestTruceOffersEachPlayerAnUptoDraw drives the SAME Upto$ read through a
// real cast end to end: Truce's `SP$ Draw | Defined$ Player | Upto$ True |
// NumCards$ 2` asks EACH player in turn, and the two answers are different —
// seat 0 takes both cards, seat 1 declines — so a per-target count that was
// applied to the wrong target, or a single shared answer, fails here. The
// Arcane Denial test above pins the same primitive against a remembered
// PLAYER target; this one pins it against the multi-target Defined$ walk and
// through the ordinary stack resolution.
func TestTruceOffersEachPlayerAnUptoDraw(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := mordorEngine(t, reg, 4107, "Truce")
	truce := searchMoveByName(t, e, "Truce", state.ZHand)
	addMana(t, e, 0, "WWW")
	before0, before1 := countDrawFor(e, 0), countDrawFor(e, 1)
	lib0 := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
	castNoDrain(t, e, truce)

	// Seat 0 (the controller, first in APNAP order) is asked first.
	d := drainPriorities(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 0 || d.Max != 2 {
		t.Fatalf("seat 0 upto ask = %+v, want KChoose Min 0 Max 2 for seat 0", d)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "card" || d.Options[0].Obj != lib0[0] {
		t.Fatalf("seat 0 upto options = %+v, want its own library top", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)

	// Seat 1 is asked next and declines — the empty answer, legal at Min 0.
	d = drainPriorities(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.Player != 1 || d.Min != 0 || d.Max != 2 {
		t.Fatalf("seat 1 upto ask = %+v, want KChoose Min 0 Max 2 for seat 1", d)
	}
	submitChoices(t, e)
	passUntilStackEmpty(t, e, 30)

	if got := countDrawFor(e, 0) - before0; got != 2 {
		t.Fatalf("seat 0 answered both and drew %d, want 2", got)
	}
	if got := countDrawFor(e, 1) - before1; got != 0 {
		t.Fatalf("seat 1 declined and drew %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}

func countDrawFor(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// --- item 4: RandomOrder$ True on ChangeZoneAll ---

// TestGrimaRestRandomOrderShufflesAndReplays resolves Gríma, Saruman's
// Footman's DBRestRandomOrder ChangeZoneAll directly against a seeded exile
// zone: the returned cards' bottom-of-library order is a permutation of the
// same ids, and a log-only replay reproduces the game exactly (the shuffle
// draws the seeded engine rng).
func TestGrimaRestRandomOrderShufflesAndReplays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	grim := searchCorpusCard(t, reg, "Gríma, Saruman's Footman")
	e, cfg := mordorEngine(t, reg, 4106, "Grizzly Bears", "Grizzly Bears", "Grizzly Bears", "Grizzly Bears")
	src := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
	// Seed the exile zone through logged moves and remember the exiled ids
	// into the resolution context (the IsRemembered alternative the real
	// chain feeds through its RememberFound/ImprintRevealed machinery).
	var exiled []state.Target
	for i := 0; i < 3; i++ {
		id := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZExile, Text: "exiled"})
		exiled = append(exiled, state.Target{Obj: id})
	}
	e.priorityRound()
	sa := cards.ResolveSVar(grim.Faces[0].SVars, "DBRestRandomOrder")
	if sa == nil {
		t.Fatal("Gríma's DBRestRandomOrder SVar did not resolve")
	}
	ctx := &effects.Ctx{Source: src, Controller: 0, Remembered: exiled, ResolvingObj: src}
	effects.Resolve(e, ctx, sa)
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < len(exiled) {
		t.Fatalf("library %d shorter than the exiled set", len(lib))
	}
	bottom := lib[len(lib)-len(exiled):]
	seen := map[state.ObjID]bool{}
	for _, id := range bottom {
		seen[id] = true
	}
	for _, tgt := range exiled {
		if !seen[tgt.Obj] {
			t.Fatalf("exiled card %d is not at the library bottom: bottom=%v", tgt.Obj, bottom)
		}
	}
	if len(seen) != len(exiled) {
		t.Fatalf("the bottom placement duplicated ids: %v", bottom)
	}
	// The shuffle must actually reorder: at this seed the returned order
	// differs from the exile order. A permutation assertion alone passes
	// even if RandomOrder$ is dropped (the scan order IS a permutation),
	// so the shuffle is only pinned by demanding the divergence.
	same := true
	for i, tgt := range exiled {
		if bottom[i] != tgt.Obj {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("bottom order equals the exile order — RandomOrder$ did not shuffle: bottom=%v", bottom)
	}
	replayCheck(t, e, cfg)
}

// --- item 2: TriggersWhenSpent$ on AB$ Mana ---

// whenspentGame deals seat 0 a commander game whose deck leads with the
// commander (deck index 0, placed in the command zone at genesis), then the
// named fixtures; seat 1 gets mountains only.
func whenspentGame(t *testing.T, reg *cards.Registry, seed uint64, commander string, fixtures ...string) (*Engine, Config) {
	t.Helper()
	deck0 := []*cards.Card{searchCorpusCard(t, reg, commander)}
	for _, fx := range fixtures {
		deck0 = append(deck0, searchCorpusCard(t, reg, fx))
	}
	deck0 = append(deck0, mountainDeck(t, 40-len(deck0))...)
	opp := mountainDeck(t, 40)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"ws", "opp"},
		Decks: [][]*cards.Card{deck0, opp}, Tokens: reg.Tokens,
		Commanders: [][]int{{0}, {}}, Format: FormatCommander, StartingLife: 40})
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// countGrantPushes counts the log's GrantTriggerPush events naming src.
func countGrantPushes(e *Engine, src state.ObjID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.GrantTriggerPush && ev.Obj == src {
			n++
		}
	}
	return n
}

// TestGilanraCallerOfWirewoodSpentManaDraws: activate the real mana ability,
// float, cast a mana-value-6 spell paying that mana — the trigger fires
// exactly once; a mana-value-2 spell paid with it fires nothing; plain
// (unprovenanced) mana on the matching cast fires nothing.
func TestGilanraCallerOfWirewoodSpentManaDraws(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	t.Run("matching cast fires exactly once", func(t *testing.T) {
		e, cfg := whenspentGame(t, reg, 4201, "Yisan, the Wanderer Bard", "Gilanra, Caller of Wirewood", "Shivan Dragon")
		toMain1(t, e)
		gilanra := moveToBattlefieldByName(t, e, 0, "Gilanra, Caller of Wirewood")
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log commanderReplayCheck below reads from.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		e.priorityRound()
		activateMana(t, e, gilanra)
		recs := e.G.Players[0].RestrictedMana
		// main's mtsp1 encoding: the rider'd ability emits a
		// provenance-ONLY batch (empty Valid, spendable anywhere) whose
		// Source is what the spend-time capture keys the trigger on.
		if len(recs) != 1 || recs[0].Color != "G" || recs[0].Valid != "" || recs[0].Source != gilanra {
			t.Fatalf("floating mana carries no when-spent provenance: %+v", recs)
		}
		addMana(t, e, 0, "GGGRR")
		shivan := searchMoveByName(t, e, "Shivan Dragon", state.ZHand)
		drawsBefore := countDrawFor(e, 0)
		castNoDrain(t, e, shivan)
		passUntilStackEmpty(t, e, 30)
		if n := countGrantPushes(e, gilanra); n != 1 {
			t.Fatalf("GrantTriggerPush count = %d, want exactly 1", n)
		}
		if got := countDrawFor(e, 0) - drawsBefore; got != 1 {
			t.Fatalf("the fired trigger drew %d, want 1", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool after payment = %d, want 0", got)
		}
		if len(e.G.Players[0].RestrictedMana) != 0 {
			t.Fatalf("the consumed when-spent provenance lingered: %+v", e.G.Players[0].RestrictedMana)
		}
		commanderReplayCheck(t, e, cfg)
	})

	t.Run("non-matching cast silent", func(t *testing.T) {
		e, cfg := whenspentGame(t, reg, 4202, "Yisan, the Wanderer Bard", "Gilanra, Caller of Wirewood", "Grizzly Bears")
		toMain1(t, e)
		gilanra := moveToBattlefieldByName(t, e, 0, "Gilanra, Caller of Wirewood")
		// A real logged TurnChange (not a raw SummonSick write) clears CR
		// 302.6 sickness on the log commanderReplayCheck below reads from.
		e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
		e.priorityRound()
		activateMana(t, e, gilanra)
		addMana(t, e, 0, "G")
		bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
		castNoDrain(t, e, bears)
		passUntilStackEmpty(t, e, 30)
		if n := countGrantPushes(e, gilanra); n != 0 {
			t.Fatalf("a mana-value-2 cast fired the when-spent trigger %d time(s)", n)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool after payment = %d, want 0", got)
		}
		if len(e.G.Players[0].RestrictedMana) != 0 {
			t.Fatalf("the consumed when-spent provenance lingered: %+v", e.G.Players[0].RestrictedMana)
		}
		commanderReplayCheck(t, e, cfg)
	})

	t.Run("plain mana silent", func(t *testing.T) {
		e, cfg := whenspentGame(t, reg, 4203, "Yisan, the Wanderer Bard", "Gilanra, Caller of Wirewood", "Shivan Dragon")
		toMain1(t, e)
		gilanra := moveToBattlefieldByName(t, e, 0, "Gilanra, Caller of Wirewood")
		e.priorityRound()
		// No Gilanra activation: the whole payment is plain pool mana.
		addMana(t, e, 0, "GGGGGGRR")
		shivan := searchMoveByName(t, e, "Shivan Dragon", state.ZHand)
		castNoDrain(t, e, shivan)
		passUntilStackEmpty(t, e, 30)
		if n := countGrantPushes(e, gilanra); n != 0 {
			t.Fatalf("plain mana fired the when-spent trigger %d time(s)", n)
		}
		commanderReplayCheck(t, e, cfg)
	})
}

// TestPathOfAncestrySpentManaScrOne: tap the real Path of Ancestry, cast a
// creature sharing a type with the commander (Ezuri is an Elf) paying that
// mana — the scry-1 trigger fires exactly once (the KArrange ask appears); a
// non-matching cast and plain mana fire nothing.
func TestPathOfAncestrySpentManaScrOne(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	t.Run("matching cast asks the scry", func(t *testing.T) {
		e, cfg := whenspentGame(t, reg, 4211, "Ezuri, Renegade Leader", "Path of Ancestry", "Llanowar Elves")
		// Path of Ancestry is dealt to hand (deck slot 1).
		toMain1(t, e)
		path := searchMoveByName(t, e, "Path of Ancestry", state.ZHand)
		// Play it (the real land drop; it enters tapped via its ETB
		// replacement, whose DB$ Tap body asks nothing).
		e.priorityRound()
		d := e.Pending()
		playIdx := castOptionIdx(d, "play_land", path)
		if playIdx < 0 {
			t.Fatalf("no play_land option for Path: %+v", d.Options)
		}
		submitChoices(t, e, playIdx)
		passUntilStackEmpty(t, e, 20)
		// Turn 3 (seat 0's second turn): the Path untaps.
		driveMordor(t, e, 3, 0, state.StepMain1)
		activateMana(t, e, path)
		recs := e.G.Players[0].RestrictedMana
		if len(recs) != 1 || recs[0].Color != "G" || recs[0].Valid != "" || recs[0].Source != path {
			t.Fatalf("floating mana carries no when-spent provenance: %+v", recs)
		}
		// The ws unit alone pays the Elves' {G}; the pool empties.
		elves := searchMoveByName(t, e, "Llanowar Elves", state.ZHand)
		drawsBefore := countDrawFor(e, 0)
		castNoDrain(t, e, elves)
		if n := countGrantPushes(e, path); n != 1 {
			t.Fatalf("GrantTriggerPush count = %d, want exactly 1", n)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool after payment = %d, want 0", got)
		}
		// The scry 1: the triggered ability's KArrange ask is pending.
		d = drainTriggersThenAsk(t, e, 30)
		if d == nil || d.Kind != decision.KArrange || len(d.Options) != 1 {
			t.Fatalf("scry ask = %+v, want KArrange over one card", d)
		}
		submitChoices(t, e, d.Options[0].Index)
		passUntilStackEmpty(t, e, 30)
		if got := countDrawFor(e, 0) - drawsBefore; got != 0 {
			t.Fatalf("the scry trigger drew %d card(s), want 0", got)
		}
		commanderReplayCheck(t, e, cfg)
	})

	t.Run("non-matching cast silent", func(t *testing.T) {
		e, cfg := whenspentGame(t, reg, 4212, "Ezuri, Renegade Leader", "Path of Ancestry", "Grizzly Bears")
		toMain1(t, e)
		path := searchMoveByName(t, e, "Path of Ancestry", state.ZHand)
		e.priorityRound()
		d := e.Pending()
		playIdx := castOptionIdx(d, "play_land", path)
		if playIdx < 0 {
			t.Fatalf("no play_land option for Path: %+v", d.Options)
		}
		submitChoices(t, e, playIdx)
		passUntilStackEmpty(t, e, 20)
		driveMordor(t, e, 3, 0, state.StepMain1)
		activateMana(t, e, path)
		addMana(t, e, 0, "G")
		bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
		castNoDrain(t, e, bears)
		passUntilStackEmpty(t, e, 30)
		if n := countGrantPushes(e, path); n != 0 {
			t.Fatalf("a non-creature cast fired the when-spent trigger %d time(s)", n)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool after payment = %d, want 0 (the provenance was consumed)", got)
		}
		if len(e.G.Players[0].RestrictedMana) != 0 {
			t.Fatalf("the consumed when-spent provenance lingered: %+v", e.G.Players[0].RestrictedMana)
		}
		commanderReplayCheck(t, e, cfg)
	})

	t.Run("plain mana silent", func(t *testing.T) {
		e, cfg := whenspentGame(t, reg, 4213, "Ezuri, Renegade Leader", "Path of Ancestry", "Grizzly Bears")
		toMain1(t, e)
		path := searchMoveByName(t, e, "Path of Ancestry", state.ZHand)
		e.priorityRound()
		d := e.Pending()
		playIdx := castOptionIdx(d, "play_land", path)
		if playIdx < 0 {
			t.Fatalf("no play_land option for Path: %+v", d.Options)
		}
		submitChoices(t, e, playIdx)
		passUntilStackEmpty(t, e, 20)
		driveMordor(t, e, 3, 0, state.StepMain1)
		// No Path activation: plain pool mana only.
		addMana(t, e, 0, "GG")
		bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
		castNoDrain(t, e, bears)
		passUntilStackEmpty(t, e, 30)
		if n := countGrantPushes(e, path); n != 0 {
			t.Fatalf("plain mana fired the when-spent trigger %d time(s)", n)
		}
		commanderReplayCheck(t, e, cfg)
	})
}
