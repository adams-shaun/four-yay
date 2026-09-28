package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Forum Filibuster and the "when you do" chain it heads: the corpus's 8
// RememberOriginalTokens$ carriers all chain a DB$ ImmediateTrigger whose
// Execute poses a real mid-resolution ask (a ChangeZone/Tap target). These
// tests run on REAL compiled corpus cards only -- no Forge script text is
// committed here (the licensing rule); the deck fixtures are basics and
// corpus cards fetched by name through searchCorpusCard (search_library_test.go).

// whenYouDoEngine seats seat 0 a deck whose first cards are the named
// fixtures followed by basics, moves fixture[0] onto the battlefield and
// fixture[1] into seat 0's graveyard, and returns the engine parked at seat
// 0's Main 1. The toss is forced to seat 0 (seatZeroStart) so the turn
// arithmetic below is stable: the protagonist's NEXT upkeep is turn 3.
func whenYouDoEngine(t *testing.T, reg *cards.Registry, onBF, toGrave string) (*Engine, Config) {
	t.Helper()
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	forest := searchCorpusCard(t, reg, "Forest")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, onBF),
		searchCorpusCard(t, reg, toGrave),
	}
	for len(deck) < 40 {
		deck = append(deck, forest, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: 9217, Names: []string{"protagonist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	searchMoveByName(t, e, onBF, state.ZBattlefield)
	return e, cfg
}

// driveToUpkeepOfProtagonist drives until seat 0's NEXT upkeep (turn > 1,
// active 0, step upkeep) and returns whatever decision is pending there --
// for the "when you do" cards that is the follow-up ask the upkeep trigger
// resolved into (the engine auto-resolves a mandatory trigger when both
// seats pass; a target ask mid-resolution is where the drive stops).
func driveToUpkeepOfProtagonist(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn > 1 && e.G.Active == 0 && e.G.Step == state.StepUpkeep {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before the protagonist's next upkeep (turn %d seat %d step %s)",
				e.G.Turn, e.G.Active, e.G.Step)
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch {
		case d.Kind == decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case d.Kind == decision.KChoose && d.ResumeKind == "" && strings.HasPrefix(d.Prompt, "turn "):
			// A cleanup-step discard down to the hand-size limit (CR 514.1)
			// crossed on the way: answer it with the first offered card and
			// keep driving -- a real, expected decision, not a stop.
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected decision %v while driving to the upkeep: %+v", d.Kind, d)
		}
	}
	t.Fatal("never reached the protagonist's next upkeep")
}

// passUntilAsk submits a pass on every priority decision until a
// non-priority decision is pending (a mid-resolution ask, a discard, ...),
// and returns it. Both seats' passes resolve the pushed trigger whose chain
// then parks on the follow-up question.
func passUntilAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 4000; i++ {
		d := e.Pending()
		if d != nil && d.Kind != decision.KPriority {
			return d
		}
		if d == nil {
			e.Advance()
			continue
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatal("never reached a non-priority decision")
	return nil
}

// TestForumFilibusterUpkeepAsksAndAttaches is the reported card end to end:
// the upkeep trigger mints the 2/1 white-and-black Inkling token
// (RememberOriginalTokens$ True remembers it), the chained DB$ ImmediateTrigger
// ("when you do") poses ONE real ask over the graveyard's Aura/Equipment
// cards (TargetMin$ 0 / TargetMax$ 1), and the answered ChangeZone moves the
// Aura to the battlefield AND emits events.Attach fastening it to the token
// (the AttachedTo$ DelayTriggerRememberedLKI leg), after which the DBCleanup
// tail cleared the remembered set.
func TestForumFilibusterUpkeepAsksAndAttaches(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := whenYouDoEngine(t, reg, "Forum Filibuster", "Divine Favor")
	aura := searchMoveByName(t, e, "Divine Favor", state.ZGraveyard)

	driveToUpkeepOfProtagonist(t, e)
	d := passUntilAsk(t, e)
	if d == nil {
		t.Fatal("no when-you-do ask pending at the upkeep")
	}
	// The token minted first.
	if n := countEmitKind(e, events.TokenCreate); n != 1 {
		t.Fatalf("%d TokenCreate events at the upkeep, want 1 (log tail %+v)", n, tailEmit(e, 8))
	}
	var token state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate {
			token = e.G.NextID - 1
			break
		}
	}
	to := e.G.Obj(token)
	if to == nil || !to.IsToken || to.Face() == nil || to.Face().Name != "Inkling Token" {
		t.Fatalf("minted token %d = %+v, want the white-and-black Inkling Token", token, to)
	}

	// (2) the "when you do" ask: ONE KChoose over the graveyard Aura, up to
	// one (Min 0 / Max 1), addressed to the controller.
	if d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 {
		t.Fatalf("when-you-do ask = %+v, want a KChoose Min 0 Max 1", d)
	}
	auraIdx := -1
	for _, o := range d.Options {
		if o.Obj == aura {
			auraIdx = o.Index
		}
	}
	if auraIdx < 0 {
		t.Fatalf("the graveyard Aura was not offered: %+v", d.Options)
	}
	submitChoices(t, e, auraIdx)

	// (3) the answered ChangeZone moved the Aura onto the battlefield and the
	// AttachedTo$ leg fastened it to the token -- one real Attach event.
	moved := e.G.Obj(aura)
	if moved == nil || moved.Zone != state.ZBattlefield {
		t.Fatalf("the answered Aura did not enter the battlefield (obj %+v)", moved)
	}
	if moved.AttachedTo != token {
		t.Fatalf("the Aura's AttachedTo = %d, want the token %d", moved.AttachedTo, token)
	}
	attaches := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Attach && ev.Obj == aura && len(ev.IDs) == 1 && ev.IDs[0] == token {
			attaches++
		}
	}
	if attaches != 1 {
		t.Fatalf("%d Attach events fastening the Aura to the token, want 1", attaches)
	}
	// (4) the DBCleanup tail ran: the source's persistent remembered set is
	// cleared after the chain completed.
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Forum Filibuster" && len(o.Remembered) != 0 {
			t.Fatalf("the source's remembered set still holds %v after DBCleanup", o.Remembered)
		}
	}
}

// TestDiregrafHordeDividesTokensIntoOneInstance pins the DivideEvenlyDown.2
// read: two Zombie tokens remembered (RememberOriginalTokens$ True), and
// TriggerAmount$ Remembered$Amount/DivideEvenlyDown.2 = ONE instance, so the
// "when you do, exile up to two target cards" follow-up asks EXACTLY once
// (the un-divided amount would pose a second ask after the first answer).
// The ETB trigger fires when the Horde enters, so the ask is pending right
// after the placement. The answered exile moves the chosen graveyard card.
func TestDiregrafHordeDividesTokensIntoOneInstance(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := whenYouDoEngine(t, reg, "Diregraf Horde", "Divine Favor")
	grave := searchMoveByName(t, e, "Divine Favor", state.ZGraveyard)

	d := passUntilAsk(t, e)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("when-you-do ask = %+v, want one KChoose over the graveyards", d)
	}
	if n := countEmitKind(e, events.TokenCreate); n != 2 {
		t.Fatalf("%d Zombie tokens minted, want 2", n)
	}
	if d.Min != 0 || d.Max != 1 {
		t.Fatalf("ask bounds Min %d Max %d, want 0/1 (one DivideEvenlyDown.2 instance)", d.Min, d.Max)
	}
	graveIdx := -1
	for _, o := range d.Options {
		if o.Obj == grave {
			graveIdx = o.Index
		}
	}
	if graveIdx < 0 {
		t.Fatalf("the graveyard card was not offered: %+v", d.Options)
	}
	submitChoices(t, e, graveIdx)
	if got := e.G.Obj(grave); got == nil || got.Zone != state.ZExile {
		t.Fatalf("the answered exile did not move %d to exile (obj %+v)", grave, got)
	}
	if next := e.Pending(); next != nil && next.Kind == decision.KChoose && next.Min == 0 && next.Max == 1 {
		t.Fatalf("a second when-you-do ask followed the answer: %+v (DivideEvenlyDown.2 read the un-divided amount)", next)
	}
}

// TestSpeedYoungAvengerImmediateTrigger is the ratchet's licensed shrink: the
// cast of a noncreature spell fires Speed's SpellCast trigger, whose Execute
// is AB$ ImmediateTrigger | Cost$ 1 -- the "you may pay {1}. When you do"
// idiom. The ordinary triggered-cost window poses the pay/decline ask; the
// paid answer charges one mana and executes TrigEffect (its CantBlockBy
// static used to fall to the unimplemented Note; since the
// Effect-delivered-CantBlockBy ticket -- rules/effect_cantblockby_test.go --
// the paid body registers a REAL CantBlockBy restriction, which is what the
// pay subtest now asserts instead of the old note); a decline (asserted on
// the second game) executes nothing.
func TestSpeedYoungAvengerImmediateTrigger(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	run := func(t *testing.T, shouldPay bool) {
		bear := searchCorpusCard(t, reg, "Grizzly Bears")
		forest := searchCorpusCard(t, reg, "Forest")
		deck := []*cards.Card{
			searchCorpusCard(t, reg, "Speed, Young Avenger"),
			searchCorpusCard(t, reg, "Divine Favor"),
		}
		for len(deck) < 40 {
			deck = append(deck, forest, bear)
		}
		opp := make([]*cards.Card, 40)
		for i := range opp {
			opp[i] = forest
		}
		cfg := seatZeroStart(Config{Seed: 9219, Names: []string{"speed", "opponent"},
			Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
		e := New(cfg)
		e.Advance()
		toMain1(t, e)
		speed := searchMoveByName(t, e, "Speed, Young Avenger", state.ZBattlefield)

		addMana(t, e, 0, "CCW")
		d := e.Pending()
		if d == nil {
			t.Fatal("no priority decision after funding")
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "cast" && o.Obj != 0 {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("no cast option: %+v", d.Options)
		}
		submitChoices(t, e, idx)
		// Divine Favor's cast-time target ask: the only creature is Speed.
		dt := e.Pending()
		if dt == nil || dt.Kind != decision.KTarget {
			t.Fatalf("cast target ask = %+v, want a KTarget", dt)
		}
		tgtIdx := -1
		for _, o := range dt.Options {
			if o.Obj == speed {
				tgtIdx = o.Index
			}
		}
		if tgtIdx < 0 {
			t.Fatalf("Speed not offered as the Aura's target: %+v", dt.Options)
		}
		submitChoices(t, e, tgtIdx)

		// The SpellCast trigger resolved into the triggered-cost window: the
		// "you may pay {1}" ask. Pay (or decline) as the subtest asks.
		pay := passUntilAsk(t, e)
		if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) != 2 {
			t.Fatalf("cost window = %+v, want the Pay 1 / Do not pay KChoose", pay)
		}
		if pay.Options[0].Kind != "trigger_cost_pay" || pay.Options[1].Kind != "trigger_cost_decline" {
			t.Fatalf("cost window options = %+v", pay.Options)
		}
		choice := 1
		if shouldPay {
			choice = 0
		}
		poolBefore := e.G.Players[0].Pool.Total()
		submitChoices(t, e, choice)
		if shouldPay {
			// The paid body is TrigEffect (DB$ Effect | ValidTgts$
			// Creature.withHaste) — the "target creature with haste" the
			// oracle names. It was never placement-covered (the ImmediateTrigger
			// AB root declares no targets), so task mvts1's pre-ask poses its
			// own KChoose here; the census's withHaste candidates are non-empty
			// (Speed itself has haste), so the ask is real. Answer it.
			dt := passUntilAsk(t, e)
			if dt == nil || dt.Kind != decision.KChoose || dt.ResumeKind != "tgts" {
				t.Fatalf("post-pay ask = %+v, want the Effect sub's KChoose with ResumeKind tgts", dt)
			}
			if len(dt.Options) == 0 || dt.Options[0].Obj != speed {
				t.Fatalf("options %+v, want Speed offered (the haste creature)", dt.Options)
			}
			submitChoices(t, e, 0)
		}

		// The paid body is TrigEffect (DB$ Effect | StaticAbilities$ KWPump),
		// whose Mode$ CantBlockBy grant effEffect registers for real (the
		// ticket counterplayeraddedall added the mode to the registration
		// case). The old assertion watched for the "unimplemented" Note the
		// build used to emit; the registration IS the execution now. The
		// registration's Remembered is a separate, pre-existing mvts1 gap:
		// the answered tgts ask does not reach the body's RememberObjects$
		// Targeted read (ledgered in the ticket report), so the grant itself
		// remembers nothing here — the assertion pins the EXECUTION, not the
		// remembered set.
		registered := false
		for i := range e.continuous {
			if ce := &e.continuous[i]; ce.Restriction == "CantBlockBy" {
				registered = true
			}
		}
		count := effectCantBlockByCount(e)
		if shouldPay {
			poolAfter := e.G.Players[0].Pool.Total()
			if poolAfter != poolBefore-1 {
				t.Fatalf("pool %d -> %d, want exactly one mana charged", poolBefore, poolAfter)
			}
			if !registered {
				t.Fatalf("the paid body never executed TrigEffect (tail %+v)", tailEmit(e, 8))
			}
			// The registered restriction is exactly the one static this board can
			// hold, so count must be 1 (main's stricter cbb1 assertion).
			if count != 1 {
				t.Fatalf("CantBlockBy effects registered = %d, want 1 (tail %+v)", count, tailEmit(e, 8))
			}
		} else {
			if registered {
				t.Fatal("the declined cost still executed the body")
			}
			if count != 0 {
				t.Fatalf("the declined cost still registered the CantBlockBy static (%d effects)", count)
			}
		}
	}

	t.Run("pay", func(t *testing.T) { run(t, true) })
	t.Run("decline", func(t *testing.T) { run(t, false) })
}

// countEmitKind counts log events of kind k.
func countEmitKind(e *Engine, k events.Kind) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == k {
			n++
		}
	}
	return n
}

// tailEmit renders the last n log events for failure messages.
func tailEmit(e *Engine, n int) []events.Event {
	ev := e.L.Events
	if len(ev) > n {
		return ev[len(ev)-n:]
	}
	return ev
}
