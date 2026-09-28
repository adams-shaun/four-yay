package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Tests for Count$Compare-driven card behaviour at the engine level:
// Nissa's Pilgrimage's spell-mastery search (ChangeNum$ X behind
// SVar:X:Count$Compare Y GE2.3.2) and the Will-of-the-X commander cycle's
// inline CharmNum$ Count$Compare Y GE1.2.1.
//
// Scope note: this task's fix is the Compare head in effects/count.go plus
// the remembered-filter plumbing. The card-text MOVEMENT contract ("one
// Forest onto the battlefield tapped, the rest into hand") is pinned in full
// below: the `Card.IsRemembered` filter predicate (effects/filter.go) makes
// DBBattlefield's sub-search offer the remembered Forests and the answer
// moves one to the battlefield tapped, the mid-resolution Remembered set
// survives the sub-search's suspension (effects/zone.go ResumeRemembered,
// restored by rules/resolution.go's resume), and DBHand's object-valued
// Defined$ Remembered is a direct fetch list (effects/zone.go
// moveDefinedLibraryObjects) rather than a new search over hidden cards, so
// the un-chosen Forests reach the hand without a further ask.

func TestNissasPilgrimageSearchMaxFollowsSpellMastery(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	run := func(t *testing.T, e *Engine, cfg Config, mainMax, picks int) {
		t.Helper()
		_, d := castSearchSpell(t, e, "Nissa's Pilgrimage")
		if d.Kind != decision.KChoose || d.Min != 0 || d.Max != mainMax {
			t.Fatalf("main search = %v %d..%d, want KChoose 0..%d", d.Kind, d.Min, d.Max, mainMax)
		}
		if len(d.Options) < picks {
			t.Fatalf("main search has %d options, need %d", len(d.Options), picks)
		}
		picked := append([]decision.Option(nil), d.Options[:picks]...)
		indices := make([]int, 0, picks)
		for _, o := range picked {
			indices = append(indices, o.Index)
		}
		submitChoices(t, e, indices...)

		// DBBattlefield sees the exact list remembered by the main search,
		// never a fresh offer over the whole library.
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) != picks || d.Max != 1 {
			t.Fatalf("remembered battlefield fetch = %+v, want %d options and max 1", d, picks)
		}
		for _, o := range d.Options {
			found := false
			for _, want := range picked {
				found = found || o.Obj == want.Obj
			}
			if !found {
				t.Fatalf("battlefield fetch offered non-selected library card %d", o.Obj)
			}
		}
		battlefield := d.Options[0].Obj
		submitChoices(t, e, d.Options[0].Index)
		if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
			t.Fatalf("after fetch list chain pending = %+v, want priority", d)
		}
		if o := e.G.Obj(battlefield); o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
			t.Fatalf("selected Forest = %+v, want tapped battlefield Forest", o)
		}
		for _, want := range picked[1:] {
			if o := e.G.Obj(want.Obj); o == nil || o.Zone != state.ZHand {
				t.Fatalf("remaining selected Forest %d in %+v, want hand", want.Obj, o)
			}
		}
		shuffled := false
		for _, ev := range e.L.Events {
			shuffled = shuffled || ev.Kind == events.Shuffle && ev.Player == 0
		}
		if !shuffled {
			t.Fatal("Nissa's final Defined$ fetch list did not shuffle the library")
		}
		replayCheck(t, e, cfg)
	}

	// Without spell mastery, one selected Forest reaches the battlefield
	// tapped. Before Count$Compare this main decision incorrectly had max 0.
	e, cfg := searchEngine(t, reg, "Nissa's Pilgrimage")
	run(t, e, cfg, 2, 1)

	// Spell mastery raises the offer to three; selecting two proves one enters
	// tapped and the other travels through DBHand's Defined$ fetch list.
	e2, cfg2 := searchEngine(t, reg, "Nissa's Pilgrimage", "Giant Growth", "Giant Growth")
	searchMoveByName(t, e2, "Giant Growth", state.ZGraveyard)
	searchMoveByName(t, e2, "Giant Growth", state.ZGraveyard)
	run(t, e2, cfg2, 3, 2)
}

func TestWillOfTheJeskaiCharmNumCountsACommander(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// No commander on the battlefield: the compared SVar counts 0, GE1
	// fails, and the modal ask is the plain choose-one.
	e, id := commanderCharmCast(t, reg, "Will of the Jeskai", "RRRR", false)
	d := castFixture(t, e, id, -1)
	if d.Kind != decision.KModes || d.Min != 1 || d.Max != 1 {
		t.Fatalf("without a commander: modal decision min=%d max=%d kind=%v, want KModes 1..1", d.Min, d.Max, d.Kind)
	}

	// Commander on the battlefield: CharmNum resolves to 2, but
	// MinCharmNum$ 1 makes the second mode optional.
	e2, id2 := commanderCharmCast(t, reg, "Will of the Jeskai", "RRRR", true)
	d2 := castFixture(t, e2, id2, -1)
	if d2.Kind != decision.KModes || d2.Min != 1 || d2.Max != 2 {
		t.Fatalf("with a commander: modal decision min=%d max=%d, want KModes 1..2", d2.Min, d2.Max)
	}
	if err := d2.Validate(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: []int{d2.Options[1].Index}}); err != nil {
		t.Fatalf("one-mode answer rejected: %v", err)
	}
	submitChoices(t, e2, d2.Options[1].Index)
	passUntilStackEmpty(t, e2, 20)
	if z := e2.G.Obj(id2).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved Will in %s, want graveyard", z)
	}
}

// commanderCharmCast makes a corpus Charm cast with an optional commander on
// the battlefield, so Count$Valid Card.IsCommander+YouCtrl can exercise the
// same cast-time context production uses.
func commanderCharmCast(t *testing.T, reg *cards.Registry, spell, mana string, commander bool) (*Engine, state.ObjID) {
	t.Helper()
	card := searchCorpusCard(t, reg, spell)
	isamaru := searchCorpusCard(t, reg, "Isamaru, Hound of Konda")
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{card, isamaru}
	for len(deck) < 40 {
		if len(deck)%2 == 0 {
			deck = append(deck, mountain)
		} else {
			deck = append(deck, forest)
		}
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 4210, Names: []string{"charmcaster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens,
		Commanders: [][]int{{1}, {}}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	if commander {
		var cmdr state.ObjID
		for _, id := range e.G.Zone(state.ZCommand, 0) {
			cmdr = id
		}
		if cmdr == 0 {
			t.Fatal("no commander in the command zone")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: cmdr, From: state.ZCommand, To: state.ZBattlefield})
		e.pending = nil
		e.priorityRound()
	}
	id := searchMoveByName(t, e, spell, state.ZHand)
	addMana(t, e, 0, mana)
	return e, id
}

func TestKamahlsWillCanChooseOneLegalMode(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, id := commanderCharmCast(t, reg, "Kamahl's Will", "GGGG", true)
	d := castFixture(t, e, id, -1)
	if d.Kind != decision.KModes || d.Min != 1 || d.Max != 1 || len(d.Options) != 1 {
		t.Fatalf("modal decision = %+v, want its one target-legal mode at 1..1", d)
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("the remaining legal mode was rejected: %v", err)
	}
}

func TestWailOfTheForgottenAllowsOneOfThreeModes(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Wail of the Forgotten")
	// Make DBReturn target-legal too, so all three modes are eligible.
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				break
			}
		}
		if len(e.G.Zone(state.ZBattlefield, 0)) > 0 {
			break
		}
	}
	moved := 0
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range append([]state.ObjID(nil), e.G.Zone(z, 0)...) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name != "Wail of the Forgotten" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZGraveyard})
				moved++
				if moved == 8 {
					break
				}
			}
		}
		if moved == 8 {
			break
		}
	}
	if moved != 8 {
		t.Fatalf("moved %d permanents to graveyard, want 8", moved)
	}
	e.pending = nil
	e.priorityRound()
	id := searchMoveByName(t, e, "Wail of the Forgotten", state.ZHand)
	addMana(t, e, 0, "UB")
	d := castFixture(t, e, id, -1)
	if d.Kind != decision.KModes || d.Min != 1 || d.Max != 3 {
		t.Fatalf("modal decision min=%d max=%d kind=%v, want KModes 1..3", d.Min, d.Max, d.Kind)
	}
}

func TestTriggeredCharmMinCharmNumUsesCorpusScript(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Invasion of Fiora")
	id := searchMoveByName(t, e, "Invasion of Fiora", state.ZBattlefield)
	f := e.G.Obj(id).Face()
	if len(f.Triggers) == 0 || f.Triggers[0].Effect == nil {
		t.Fatal("Invasion of Fiora has no compiled triggered Charm")
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want MoveZone-triggered ability", e.G.Stack)
	}
	e.askTriggerModes(0, e.G.Stack[0], f.Triggers[0].Effect)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.Min != 1 || d.Max != 2 {
		t.Fatalf("triggered modal decision = %+v, want KModes 1..2", d)
	}
}
