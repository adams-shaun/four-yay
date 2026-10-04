package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// unless_pay_family_test.go pins the ONE shared UnlessCost$ gate
// (effects.Resolve's unlessProceed dispatch) on the four SA kinds the
// unless-pay family ticket names besides Counter/CopySpellAbility/Sacrifice:
// Tap (Blood Crypt / Hallowed Fountain's shock-land ETB), DealDamage (Mogis,
// God of Slaughter's upkeep), LoseLife (Isolation Cell) and ChangeZone
// (Meathook Massacre II's opponent-death return). The census
// (TestEveryRepoDeckParamsAreRead) measures the four APIs' UnlessCost$/
// UnlessPayer$ as read through the shared gate; these are the ENGINE tests
// for both branches (pay vs decline) on real corpus cards, so the census
// read is backed by behaviour, not by a param-read shim.
//
// The task brief's premise that the four census entries still stood was
// re-measured FALSE at the worktree base: the shared gate had already
// shrunk every `param:api:<Tap|DealDamage|LoseLife|ChangeZone>.UnlessCost`
// entry; what was missing was exactly this behavioural pinning per SA kind.

// unlessFamilyEngine is a 2-seat mountain-deck engine with both hands
// cleared and seat 0 as the CR 103.1 starter, the discipline every
// mid-resolution fixture in this package shares.
func unlessFamilyEngine(t *testing.T, seed uint64) *Engine {
	t.Helper()
	e := New(seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}))
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	e.Advance()
	return e
}

// seekUnlessPay passes every priority decision until a mid-resolution
// unless_pay ask becomes pending, and returns it.
func seekUnlessPay(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind == decision.KModes && d.ResumeKind == "unless_pay" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision kind %v (seat %d) while seeking the unless-pay ask: %+v", d.Kind, d.Player, d)
		}
		castFirst(t, e, "pass")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tap: Blood Crypt's ETB shock-land shape (UnlessCost$ PayLife<2>,
// UnlessPayer$ You), resolved through the R:Event$ Moved ReplaceWith$ body.
// ---------------------------------------------------------------------------

func TestUnlessPayTapBloodCryptPayEntersUntapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Blood Crypt"))
	e.askPriority(0)
	d := e.Pending()
	land := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			land = o.Index
			break
		}
	}
	if land < 0 {
		t.Fatalf("no play_land option: %+v", d.Options)
	}
	crypt := d.Options[land].Obj
	submitChoices(t, e, land)

	ask := seekUnlessPay(t, e, 40)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for the entering Blood Crypt")
	}
	if ask.Player != 0 {
		t.Fatalf("pay ask player = seat %d, want the entering controller (UnlessPayer$ You)", ask.Player)
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "Tap" {
		t.Fatalf("ask SA = %+v, want the DB$ Tap body", ask.ResumeSA)
	}
	// Pay: the shared unless-pay settlement charges the PayLife<2>.
	submitChoices(t, e, ask.Options[0].Index)
	o := e.G.Obj(crypt)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("paid Blood Crypt zone = %+v, want battlefield", o)
	}
	if o.Tapped {
		t.Fatal("paid Blood Crypt entered tapped; the pay must spare the tap")
	}
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("controller life = %d, want 18 (20 - 2)", life)
	}
}

func TestUnlessPayTapBloodCryptDeclineEntersTapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Blood Crypt"))
	e.askPriority(0)
	d := e.Pending()
	land := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" {
			land = o.Index
			break
		}
	}
	if land < 0 {
		t.Fatalf("no play_land option: %+v", d.Options)
	}
	crypt := d.Options[land].Obj
	submitChoices(t, e, land)

	ask := seekUnlessPay(t, e, 40)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for the entering Blood Crypt")
	}
	submitChoices(t, e, ask.Options[1].Index)
	o := e.G.Obj(crypt)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("declined Blood Crypt zone = %+v, want battlefield (it still enters)", o)
	}
	if !o.Tapped {
		t.Fatal("declined Blood Crypt entered untapped; the decline must run the tap body")
	}
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("controller life = %d, want 20 (nothing paid)", life)
	}
}

// ---------------------------------------------------------------------------
// DealDamage: Mogis, God of Slaughter's upkeep (UnlessCost$ Sac<1/Creature>,
// UnlessPayer$ TriggeredPlayer).
// ---------------------------------------------------------------------------

func mogisEngine(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID) {
	t.Helper()
	e := unlessFamilyEngine(t, 411)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Mogis, God of Slaughter"))
	bear := onBoardCard(t, e, 1, card(t, "Name:Grizzly Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	return e, bear
}

func TestUnlessPayDealDamageMogisPaySacrificesAndSparesTheDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, bear := mogisEngine(t, reg)
	ask := seekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed at the opponent's upkeep")
	}
	if ask.Player != 1 {
		t.Fatalf("pay ask player = seat %d, want the upkeep player (UnlessPayer$ TriggeredPlayer)", ask.Player)
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "DealDamage" {
		t.Fatalf("ask SA = %+v, want the DB$ DealDamage body", ask.ResumeSA)
	}
	// Pay: the Sac<1/Creature> component. The bear is the only eligible
	// creature, so beginUnlessPayment records it without a second ask.
	submitChoices(t, e, ask.Options[0].Index)
	if z := e.G.Obj(bear).Zone; z != state.ZGraveyard {
		t.Fatalf("paid Mogis bear zone = %v, want graveyard (sacrificed)", z)
	}
	if n := countPlayerDamage(e, 1); n != 0 {
		t.Fatalf("seat 1 took %d damage after paying, want none", n)
	}
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("seat 1 life = %d, want 20", life)
	}
}

func TestUnlessPayDealDamageMogisDeclineTakesTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, bear := mogisEngine(t, reg)
	ask := seekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed at the opponent's upkeep")
	}
	submitChoices(t, e, ask.Options[1].Index)
	if z := e.G.Obj(bear).Zone; z != state.ZBattlefield {
		t.Fatalf("declined Mogis bear zone = %v, want battlefield (nothing sacrificed)", z)
	}
	if n := countPlayerDamage(e, 1); n != 2 {
		t.Fatalf("seat 1 took %d damage, want the unpaid 2", n)
	}
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("seat 1 life = %d, want 18", life)
	}
}

// ---------------------------------------------------------------------------
// LoseLife: Isolation Cell (UnlessCost$ 2, UnlessPayer$
// TriggeredCardController) — the plain-mana unless branch, the shape
// Torment of Hailfire's own bodies carry behind its (unimplemented
// GenericChoice) chooser.
// ---------------------------------------------------------------------------

func isolationCellEngine(t *testing.T, reg *cards.Registry) (*Engine, state.ObjID) {
	t.Helper()
	e := unlessFamilyEngine(t, 412)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Isolation Cell"))
	mn := card(t, "Name:Memnite\nManaCost:0\nTypes:Artifact Creature Construct\nPT:1/1\nOracle:x\n")
	o := e.G.AddObject(mn, 1)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{o.ID})
	// The pool the pay branch will spend its {2} from.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "R", Amount: 3})
	// A creature spell is sorcery-speed: drive to seat 1's own main phase
	// before the cast.
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.askPriority(0)
	castFirst(t, e, "pass") // seat 0 passes; seat 1 takes priority
	idx := passToCast(t, e, o.ID)
	submitChoices(t, e, idx)
	return e, o.ID
}

func TestUnlessPayLoseLifeIsolationCellPayDrainsThePool(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, mn := isolationCellEngine(t, reg)
	ask := seekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for the creature cast")
	}
	if ask.Player != 1 {
		t.Fatalf("pay ask player = seat %d, want the caster (UnlessPayer$ TriggeredCardController)", ask.Player)
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "LoseLife" {
		t.Fatalf("ask SA = %+v, want the DB$ LoseLife body", ask.ResumeSA)
	}
	submitChoices(t, e, ask.Options[0].Index)
	if pool := e.G.Players[1].Pool.Total(); pool != 1 {
		t.Fatalf("payer pool = %d, want 1 (3 floated, 2 spent)", pool)
	}
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("seat 1 life = %d, want 20 (paid, no life loss)", life)
	}
	// The spell-cast trigger resolves above the spell (LIFO), so the Memnite
	// is still on the stack while the unless gate holds; drive it down.
	for i := 0; i < 40 && len(e.G.Stack) > 0 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			castFirst(t, e, "pass")
		} else if len(d.Options) > 0 {
			submitChoices(t, e, d.Options[0].Index)
		}
	}
	if z := e.G.Obj(mn).Zone; z != state.ZBattlefield {
		t.Fatalf("Memnite zone = %v, want battlefield once everything resolves", z)
	}
}

func TestUnlessPayLoseLifeIsolationCellDeclineLosesTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := isolationCellEngine(t, reg)
	ask := seekUnlessPay(t, e, 60)
	if ask == nil {
		t.Fatal("no unless-pay ask posed for the creature cast")
	}
	submitChoices(t, e, ask.Options[1].Index)
	if pool := e.G.Players[1].Pool.Total(); pool != 3 {
		t.Fatalf("payer pool = %d, want 3 (declined, nothing spent)", pool)
	}
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("seat 1 life = %d, want 18 (the unpaid LoseLife 2)", life)
	}
}

// ---------------------------------------------------------------------------
// ChangeZone: Meathook Massacre II's opponent-death return (UnlessCost$
// PayLife<3>, UnlessPayer$ TriggeredCardController).
// ---------------------------------------------------------------------------
