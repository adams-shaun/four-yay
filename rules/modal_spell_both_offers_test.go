package rules

// agent-20260925T031327Z-d7cbb90c: pin the independent two-face cast offer of
// a Modal DFC's hand loop (CR 712.8) with a real corpus card. The offer walk
// in (*Engine).legalActionsPriced appends the front-face cast offer AND a
// separate modal_spell offer for a nonland spell back face; both carries are
// priced and gated against their OWN face (Extus front {1}{W}{B}{B}, Awaken
// back {6}{B}{R} plus its spell-ability cost fold via withSpellAbilityExtras).
// TestAwakenTheBloodAvatarAbilityOfferSacrificeReducesManaCost already
// exercises the discounted spell-back cast through resolution; this test
// pins the OFFER contract itself: exactly ONE plain cast option for the
// front face and exactly ONE modal_spell option for the back face, together,
// for the same object, at distinct indices, with the read-only offer walk
// having left the card front-up in hand.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// modalSpellOffersOf returns the plain front-face cast option and the
// modal_spell back-face cast option offered for id, failing the test on any
// duplicate of either shape.
func modalSpellOffersOf(t *testing.T, e *Engine, id state.ObjID) (front, back *decision.Option) {
	t.Helper()
	for i := range e.Pending().Options {
		o := &e.Pending().Options[i]
		switch {
		case o.Kind != "cast" || o.Obj != id:
		case o.Mode == "modal_spell":
			if back != nil {
				t.Fatalf("duplicate modal_spell cast option for object %d: %+v and %+v", id, back, o)
			}
			back = o
		case o.Mode == "":
			if front != nil {
				t.Fatalf("duplicate plain cast option for object %d: %+v and %+v", id, front, o)
			}
			front = o
		}
	}
	return front, back
}

// TestModalSpellBackAndFrontOffersTogether is the offer-contract pin: with
// the real Extus, Oriq Overlord // Awaken the Blood Avatar front-up in hand
// and a pool covering both printed costs, the active player's empty-stack
// main-phase priority offers exactly one plain cast option for the creature
// front AND exactly one modal_spell cast option for the sorcery back, both
// for the same object at distinct indices, and the read-only walk leaves the
// card front-up in hand.
func TestModalSpellBackAndFrontOffersTogether(t *testing.T) {
	t.Parallel()
	// No sacrifice-reduction permanents: the back face's Cost$ 6 B R
	// Sac<X/Creature> fold is priced with X=0, so the full printed {6}{B}{R}
	// must be affordable. WWBBBBRRRR = {W}{W}{B}{B}{B}{B}{R}{R}{R}{R} covers
	// the front {1}{W}{B}{B} and the back {6}{B}{R} (10 mana total).
	e, spell, ids := sacXFixture(t, "Awaken the Blood Avatar", nil, "WWBBBBRRRR")
	if len(ids) != 0 {
		t.Fatalf("precondition: %d permanents requested, want none", len(ids))
	}

	// Preconditions: the real corpus card is the modal front in hand, with a
	// nonland spell back face whose printed cost differs from the front's.
	o := e.G.Obj(spell)
	if o == nil || o.FaceIdx != 0 || o.Zone != state.ZHand {
		t.Fatalf("precondition: card is nil=%v face=%d zone=%s, want Extus front in hand", o == nil, o.FaceIdx, o.Zone)
	}
	if got := o.Face().Name; got != "Extus, Oriq Overlord" {
		t.Fatalf("precondition: front face %q, want Extus, Oriq Overlord", got)
	}
	mf := modalSpellBack(o)
	if mf == nil {
		t.Fatal("precondition: no nonland Modal spell back face reachable from the hand card")
	}
	if got := mf.Name; got != "Awaken the Blood Avatar" {
		t.Fatalf("precondition: back face %q, want Awaken the Blood Avatar", got)
	}
	if mf.IsLand() {
		t.Fatal("precondition: back face reads as a land, want a sorcery spell back")
	}
	if o.Card.Faces[0].ManaCost == mf.ManaCost {
		t.Fatalf("precondition: face costs %q are identical, want the two printed costs to differ", mf.ManaCost)
	}
	if o.Card.Faces[0].ManaCost != "1 W B B" || mf.ManaCost != "6 B R" {
		t.Fatalf("precondition: costs %q / %q, want the printed 1 W B B / 6 B R",
			o.Card.Faces[0].ManaCost, mf.ManaCost)
	}
	// Preconditions: it is the active player's main phase with an empty stack
	// (sorcery speed, no interruption), and the pool covers both printed
	// costs with nothing spent.
	if e.G.Step != state.StepMain1 || e.G.Active != 0 || e.G.Priority != 0 {
		t.Fatalf("precondition: step=%v active=%d priority=%d, want seat 0's main phase", e.G.Step, e.G.Active, e.G.Priority)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("precondition: %d spells on the stack, want an empty stack", len(e.G.Stack))
	}
	if pool := e.G.Players[0].Pool; pool.Total() < 10 {
		t.Fatalf("precondition: pool %+v totals %d, want >=10 for both printed costs", pool, pool.Total())
	}

	front, back := modalSpellOffersOf(t, e, spell)
	if front == nil {
		t.Fatalf("plain front-face cast offer missing: %+v", e.Pending().Options)
	}
	if front.Label != "Cast Extus, Oriq Overlord" {
		t.Fatalf("plain cast option label %q, want \"Cast Extus, Oriq Overlord\"", front.Label)
	}
	if back == nil {
		t.Fatalf("modal_spell back-face cast offer missing: %+v", e.Pending().Options)
	}
	if back.Label != "Cast Awaken the Blood Avatar" {
		t.Fatalf("modal_spell option label %q, want \"Cast Awaken the Blood Avatar\"", back.Label)
	}
	if front.Index == back.Index {
		t.Fatalf("both options share index %d, want distinct option indices", front.Index)
	}

	// The read-only offer walk left the card front-up in hand.
	if o := e.G.Obj(spell); o.Zone != state.ZHand || o.FaceIdx != 0 || o.Face().Name != "Extus, Oriq Overlord" {
		t.Fatalf("offer walk moved the card: zone=%s face=%d %q, want Extus front-up in hand",
			o.Zone, o.FaceIdx, o.Face().Name)
	}
	// The offered pool is still intact: the walk priced the costs without
	// paying them.
	if pool := e.G.Players[0].Pool; pool.Total() < 10 {
		t.Fatalf("pool after the offer walk is %+v totals %d, want the funded pool untouched", pool, pool.Total())
	}
}
