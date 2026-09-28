package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// saonEngine installs the carrier and ability vehicle directly on the board.
// This keeps the test focused on the AbilityCast matcher rather than genesis.
func saonEngine(t *testing.T, board0, board1 []*cards.Card) *Engine {
	t.Helper()
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: testutil.CorpusRegistry(t).Tokens})
	for p, cards := range [][]*cards.Card{board0, board1} {
		var ids []state.ObjID
		for _, c := range cards {
			o := e.G.AddObject(c, state.PlayerID(p))
			o.Zone = state.ZBattlefield
			ids = append(ids, o.ID)
		}
		e.G.SetZone(state.ZBattlefield, state.PlayerID(p), ids)
	}
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority, e.G.Turn = 0, 0, 1
	return e
}

const saonArtifact = "Name:Test Saon Artifact\nManaCost:2\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ T | NumCards$ 1\nOracle:x\n"

const saonCreature = "Name:Test Saon Creature\nManaCost:2\nTypes:Creature Bear\nPT:2/2\n" +
	"A:AB$ Draw | Cost$ T | NumCards$ 1\nOracle:x\n"

const saonManaOnlyWatcher = "Name:Test SAon Card Watcher\nManaCost:2\nTypes:Artifact\n" +
	"T:Mode$ AbilityCast | ValidSAonCard$ Activated.ManaAbility | TriggerZones$ Battlefield | Execute$ TrigDmg | TriggerDescription$ x\n" +
	"SVar:TrigDmg:DB$ DealDamage | NumDmg$ 1 | Defined$ TriggeredActivator\nOracle:x\n"

// saonArtifactOnlyWatcher carries ValidCard$ and NO ValidSA$/ValidSAonCard$,
// so it depends on nothing but the source-card filter. It is the independent
// discriminator for the ValidCard$ block: on a matcher that does not read
// ValidCard$ (main) an AbilityCast fires WIDE, so the creature activation
// below deals damage and the negative test fails.
const saonArtifactOnlyWatcher = "Name:Test SAon Card Watcher\nManaCost:2\nTypes:Artifact\n" +
	"T:Mode$ AbilityCast | ValidCard$ Artifact.inZoneBattlefield | TriggerZones$ Battlefield | Execute$ TrigDmg | TriggerDescription$ x\n" +
	"SVar:TrigDmg:DB$ DealDamage | NumDmg$ 1 | Defined$ TriggeredActivator\nOracle:x\n"

func saonSettle(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
			return
		}
		if e.pending == nil {
			e.priorityRound()
			continue
		}
		d := e.Pending()
		switch d.Kind {
		case decision.KPriority:
			passFirst(t, e)
		case decision.KTriggerOptional:
			submitChoices(t, e, 0)
		case decision.KChoose:
			if len(d.Options) < d.Min {
				t.Fatalf("choose has too few options: %+v", d)
			}
			idx := make([]int, d.Min)
			for j := range idx {
				idx[j] = d.Options[j].Index
			}
			submitChoices(t, e, idx...)
		case decision.KAttackers, decision.KBlockers:
			submitChoices(t, e)
		default:
			t.Fatalf("unexpected decision %q", d.Kind)
		}
	}
	t.Fatalf("activation did not settle: %d triggers, %d stack objects", len(e.pendingTriggers), len(e.G.Stack))
}

func saonActivate(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	e.G.Priority = p
	e.askPriority(p)
	d := e.Pending()
	if d == nil || d.Player != p {
		t.Fatalf("expected priority for seat %d, got %+v", p, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "ability" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("seat %d has no ability option: %+v", p, d.Options)
	}
	submitChoices(t, e, idx)
}

func damageEvents(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage {
			n++
		}
	}
	return n
}

// The real carrier's ValidSAonCard$ Activated.YouCtrl is evaluated against
// the activated card, while ValidSA$ Activated.OppCtrl is relative to the
// trigger source. An opponent's artifact activation therefore deals damage.
func TestAvalancheOfSector7OpponentArtifactAbilityDealsDamage(t *testing.T) {
	t.Parallel()
	av := corpusAlternativeCard(t, "Avalanche of Sector 7")
	e := saonEngine(t, []*cards.Card{av, card(t, saonArtifact)}, []*cards.Card{card(t, saonArtifact)})
	if e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[0]).Face().Name != "Avalanche of Sector 7" {
		t.Fatal("test precondition: carrier is not on seat 0's battlefield")
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("test precondition: seat 1 life = %d, want 20", e.G.Players[1].Life)
	}
	gear := e.G.Zone(state.ZBattlefield, 1)[0]
	if !effects.MatchesSpecCtx(e.G, "Artifact.inZoneBattlefield", gear, e.specCtx(0, 0)) {
		t.Fatalf("test precondition: artifact source does not match ValidCard spec")
	}
	saonActivate(t, e, 1)
	saonSettle(t, e)
	if e.G.Players[1].Life != 19 {
		t.Fatalf("artifact ability did not trigger damage: life %d, want 19", e.G.Players[1].Life)
	}
}

// This is the required ValidSAonCard$ discriminator: a non-mana
// activation must not satisfy a card clause that asks for ManaAbility.
func TestAbilityCastValidSAonCardRejectsNonMatchingAbility(t *testing.T) {
	t.Parallel()
	e := saonEngine(t, []*cards.Card{card(t, saonManaOnlyWatcher)}, []*cards.Card{card(t, saonArtifact)})
	if e.G.Players[1].Life != 20 {
		t.Fatalf("test precondition: seat 1 life = %d, want 20", e.G.Players[1].Life)
	}
	saonActivate(t, e, 1)
	saonSettle(t, e)
	if e.G.Players[1].Life != 20 {
		t.Fatalf("non-mana ability satisfied ValidSAonCard ManaAbility: life %d", e.G.Players[1].Life)
	}
}

// This is the independent negative discriminator for the ValidCard$ block:
// an AbilityCast trigger scoped ONLY by ValidCard$ (no ValidSA$, no
// ValidSAonCard$) must reject the activated ability of a non-matching source
// card. On main the param is unread, so the trigger fires wide and this test
// fails; the artifact-only watcher needs no other gate to reach the filter.
func TestAbilityCastValidCardRejectsNonMatchingAbilitySource(t *testing.T) {
	t.Parallel()
	e := saonEngine(t, []*cards.Card{card(t, saonArtifactOnlyWatcher)}, []*cards.Card{card(t, saonCreature)})
	watcher := e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[0])
	if watcher.Face().Name != "Test SAon Card Watcher" {
		t.Fatal("test precondition: watcher is not on seat 0's battlefield")
	}
	if len(watcher.Face().Triggers) != 1 || watcher.Face().Triggers[0].Params["ValidCard"] == "" {
		t.Fatal("test precondition: watcher trigger does not carry ValidCard$")
	}
	vehicle := e.G.Obj(e.G.Zone(state.ZBattlefield, 1)[0])
	if vehicle.Face().Types[0] == "Artifact" {
		t.Fatal("test precondition: vehicle must be non-artifact")
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("test precondition: seat 1 life = %d, want 20", e.G.Players[1].Life)
	}
	before := damageEvents(e)
	saonActivate(t, e, 1)
	saonSettle(t, e)
	if e.G.Players[1].Life != 20 || damageEvents(e) != before {
		t.Fatalf("non-artifact ability incorrectly satisfied ValidCard$: life=%d damage-events=%d (before %d)", e.G.Players[1].Life, damageEvents(e), before)
	}
}

// This is the isolated-hunk negative for the carrier's ValidCard$: the same
// activated ability shape on a creature must not trigger Avalanche. It is a
// second, card-real instance of the filter the independent carrier above
// pins; retained so a future change to either validSAonCard or ValidCard
// cannot regress Avalanche silently.
func TestAvalancheOfSector7RejectsNonArtifactAbilitySource(t *testing.T) {
	t.Parallel()
	av := corpusAlternativeCard(t, "Avalanche of Sector 7")
	e := saonEngine(t, []*cards.Card{av}, []*cards.Card{card(t, saonCreature)})
	if e.G.Obj(e.G.Zone(state.ZBattlefield, 1)[0]).Face().Types[0] == "Artifact" {
		t.Fatal("test precondition: vehicle must be non-artifact")
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("test precondition: seat 1 life = %d, want 20", e.G.Players[1].Life)
	}
	before := damageEvents(e)
	saonActivate(t, e, 1)
	saonSettle(t, e)
	if e.G.Players[1].Life != 20 || damageEvents(e) != before {
		t.Fatalf("non-artifact ability incorrectly triggered Avalanche: life=%d damage-events=%d (before %d)", e.G.Players[1].Life, damageEvents(e), before)
	}
}
