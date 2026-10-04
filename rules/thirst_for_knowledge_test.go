package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The Discard primitive's UnlessType$ read (task
// inbox-paramcensus-final-stragglers, Thirst for Knowledge entry). A Mode$
// TgtChoose discard carrying UnlessType$ (Thirst for Knowledge's "discard
// two cards unless you discard an artifact card") poses the unless ELECTION
// whenever a card of the type is in hand: "discard one <type> instead" or
// "discard normally". The unless arm's follow-up pick re-uses the ordinary
// "discard" ask; a declined election falls through to the ordinary
// TgtChoose path, whose strict-supersets no-ask rule then governs.

// tfkSrc is Thirst for Knowledge's real script shape (the SP$/DBDiscard pair
// verbatim).
const tfkSrc = "Name:Thirst for Knowledge\nManaCost:2 U\nTypes:Instant\n" +
	"A:SP$ Draw | NumCards$ 3 | SpellDescription$ Draw three cards. Then discard two cards unless you discard an artifact card. | SubAbility$ DBDiscard\n" +
	"SVar:DBDiscard:DB$ Discard | Defined$ You | NumCards$ 2 | Mode$ TgtChoose | UnlessType$ Artifact\nOracle:x\n"

// tfkHand builds the caster's hand: the spell plus the named extra cards, so
// the discard decisions have a known eligible set.
func tfkHand(t *testing.T, extras ...string) (*Engine, state.ObjID) {
	t.Helper()
	e, _, id := newFixtureDeck(t, 96, tfkSrc, "Name:Filler\nTypes:Creature\nPT:1/1\nOracle:x\n")
	e.G.SetZone(state.ZHand, 0, nil)
	hand := []state.ObjID{id}
	for _, spec := range extras {
		o := e.G.AddObject(card(t, spec), 0)
		o.Zone = state.ZHand
		hand = append(hand, o.ID)
	}
	e.G.SetZone(state.ZHand, 0, hand)
	e.priorityRound()
	return e, id
}

func tfkArtifact(spec string) string {
	return "Name:" + spec + "\nManaCost:0\nTypes:Artifact\nOracle:x\n"
}

func tfkOptionFor(d *decision.Decision, id state.ObjID) int {
	for _, o := range d.Options {
		if o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// TestThirstForKnowledgeDiscardsTheArtifactInstead drives the unless arm:
// the election is posed (an artifact is in hand), answered "unless", and the
// single artifact leaves the hand -- the two ordinary cards stay.
func TestThirstForKnowledgeDiscardsTheArtifactInstead(t *testing.T) {
	t.Parallel()
	e, id := tfkHand(t, tfkArtifact("My Mox"), "Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n")
	addMana(t, e, 0, "UUU")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "discard_unless" {
		t.Fatalf("expected the unless election, got %+v", d)
	}
	unlessIdx := -1
	for _, o := range d.Options {
		if o.Kind == "unless" {
			unlessIdx = o.Index
		}
	}
	submitChoices(t, e, unlessIdx)
	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(firstHandObj(t, e, "My Mox")).Zone; z != state.ZGraveyard {
		t.Fatalf("the artifact's zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(firstHandObj(t, e, "Frog")).Zone; z != state.ZHand {
		t.Fatalf("the ordinary card's zone = %s, want Hand (the unless arm must not discard it)", z)
	}
}

// TestThirstForKnowledgeDeclinedElectionDiscardsTwo answers the election
// "ordinary": the ordinary TgtChoose ask follows and two non-artifact cards
// leave the hand -- the artifact stays.
func TestThirstForKnowledgeDeclinedElectionDiscardsTwo(t *testing.T) {
	t.Parallel()
	e, id := tfkHand(t, tfkArtifact("My Mox"), "Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Bird\nTypes:Creature\nPT:1/1\nOracle:x\n", "Name:Fish\nTypes:Creature\nPT:1/1\nOracle:x\n")
	addMana(t, e, 0, "UUU")
	d := castFixture(t, e, id, -1)
	if d == nil || d.ResumeKind != "discard_unless" {
		t.Fatalf("expected the unless election, got %+v", d)
	}
	ordIdx := -1
	for _, o := range d.Options {
		if o.Kind == "ordinary" {
			ordIdx = o.Index
		}
	}
	submitChoices(t, e, ordIdx)
	// The ordinary ask: discard exactly two of the eligible hand.
	d2 := e.Pending()
	if d2 == nil || d2.ResumeKind != "discard" {
		t.Fatalf("expected the ordinary discard ask, got %+v", d2)
	}
	mox := firstHandObj(t, e, "My Mox")
	picks := []int{}
	for _, o := range d2.Options {
		if o.Obj != mox && len(picks) < 2 {
			picks = append(picks, o.Index)
		}
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(mox).Zone; z != state.ZHand {
		t.Fatalf("the artifact's zone = %s, want Hand (the declined election keeps it)", z)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 5 {
		t.Fatalf("hand = %d, want 5 (4 - cast + 3 drawn - 2 discarded)", len(e.G.Zone(state.ZHand, 0)))
	}
}

// TestThirstForKnowledgeNoArtifactAsksOrdinarily is the control: with no
// artifact in hand the election is never posed and the ordinary ask comes
// straight away.
func TestThirstForKnowledgeNoArtifactAsksOrdinarily(t *testing.T) {
	t.Parallel()
	e, id := tfkHand(t, "Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n",
		"Name:Bird\nTypes:Creature\nPT:1/1\nOracle:x\n", "Name:Fish\nTypes:Creature\nPT:1/1\nOracle:x\n")
	addMana(t, e, 0, "UUU")
	d := castFixture(t, e, id, -1)
	if d == nil || d.ResumeKind != "discard" {
		t.Fatalf("expected the ordinary discard ask straight away, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)
	if len(e.G.Zone(state.ZHand, 0)) != 4 {
		t.Fatalf("hand = %d, want 4 (3 - cast + 3 drawn - 2 discarded)", len(e.G.Zone(state.ZHand, 0)))
	}
}

// TestThirstForKnowledgeChoosesAmongArtifacts answers the election "unless"
// with TWO artifacts in hand: the unless arm's own one-card ask runs, and
// the artifact NOT chosen stays in hand.
func TestThirstForKnowledgeChoosesAmongArtifacts(t *testing.T) {
	t.Parallel()
	e, id := tfkHand(t, tfkArtifact("Mox A"), tfkArtifact("Mox B"),
		"Name:Frog\nTypes:Creature\nPT:1/1\nOracle:x\n")
	addMana(t, e, 0, "UUU")
	d := castFixture(t, e, id, -1)
	if d == nil || d.ResumeKind != "discard_unless" {
		t.Fatalf("expected the unless election, got %+v", d)
	}
	unlessIdx := -1
	for _, o := range d.Options {
		if o.Kind == "unless" {
			unlessIdx = o.Index
		}
	}
	submitChoices(t, e, unlessIdx)
	// The unless arm's own pick: exactly one artifact.
	d2 := e.Pending()
	if d2 == nil || d2.ResumeKind != "discard" || len(d2.Options) != 2 {
		t.Fatalf("expected the one-artifact pick over 2 options, got %+v", d2)
	}
	keep := firstHandObj(t, e, "Mox B")
	take := firstHandObj(t, e, "Mox A")
	pick := tfkOptionFor(d2, take)
	if pick < 0 {
		t.Fatalf("Mox A not offered: %+v", d2.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(take).Zone; z != state.ZGraveyard {
		t.Fatalf("the chosen artifact's zone = %s, want Graveyard", z)
	}
	if z := e.G.Obj(keep).Zone; z != state.ZHand {
		t.Fatalf("the kept artifact's zone = %s, want Hand", z)
	}
}

func firstHandObj(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("%s not in seat 0's hand or graveyard", name)
	return 0
}
