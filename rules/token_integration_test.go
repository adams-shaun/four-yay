// Task 13 fix round 1, review finding "Important 1": TestHeads and
// TestRepoDecksPlayAtEverySeatCount passing unchanged after api:Token's
// registration proved nothing about whether the primitive actually
// resolves end-to-end in a real game. Two of the ratchet's own
// api:Token-carrying cards ARE drawn into the acceptance games those tests
// play -- Batterskull (death-n-taxes, every seat count 2/4/6/8) and Empty
// the Warrens (the-epic-storm, 8 seats) -- so the unchanged chain heads
// mean the testbot never chose to cast either one, not that Token has any
// coverage there. This file closes that gap with a direct, real-corpus
// integration test.
//
// The real Batterskull card and the real b_0_0_phyrexian_germ token
// definition both come from the compiled corpus registry at test time --
// never copied into this file. Even the expected token name is read back
// from the registry's own token definition (germDef.Faces[0].Name) rather
// than hardcoded, so nothing here repeats Forge's own script text.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBatterskullLivingWeaponCreatesAGermAndReachesAttach casts the real
// Batterskull, drains the stack (its own spell, then the Living Weapon
// trigger its entry queues), and checks two things end to end: a token
// actually lands on the battlefield under the casting player's control
// (api:Token's own job), and Ctx.Remembered carries that token far enough
// for the trigger's chained SubAbility$ -- __kwLWAttach, `DB$ Attach |
// Defined$ Remembered | Object$ Self` -- to actually run and hit
// Resolve's "unimplemented API" fallback (Attach is Task 14's job, not
// registered yet). That Note is today's correct, honest outcome; once Task
// 14 registers Attach, this test's last assertion should flip to checking
// the token is actually attached instead of checking the fallback fired.
func TestBatterskullLivingWeaponCreatesAGermAndReachesAttach(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bsk, ok := reg.Lookup("Batterskull")
	if !ok {
		t.Fatal("Batterskull not found in the compiled corpus registry")
	}
	germDef, ok := reg.Tokens["b_0_0_phyrexian_germ"]
	if !ok {
		t.Fatal("b_0_0_phyrexian_germ not found in the compiled corpus registry's token scripts")
	}
	wantGermName := germDef.Faces[0].Name

	cfg := Config{Seed: 900, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{bsk}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	}
	e := New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == "Batterskull" {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == "Batterskull" {
				id = cand
			}
		}
		if id == 0 {
			t.Fatal("Batterskull not found in seat 0's hand or library")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}

	driveToStep(t, e, 1, 0, state.StepMain1)
	// Funded after driving to main1, not before: setStep clears every
	// player's mana pool on every step transition (CR 500.4), so mana
	// funded earlier would already be gone -- the same ordering
	// castAndResolveTappedCreature's own doc comment explains.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 5})
	e.priorityRound()

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected seat 0's priority after funding mana, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "cast" && opt.Obj == id {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Batterskull: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
	if len(e.G.Stack) != 1 || e.G.Stack[0] != id {
		t.Fatalf("stack = %v, want [%d] right after casting Batterskull", e.G.Stack, id)
	}

	beforeEvents := len(e.L.Events)
	const bound = 60
	passUntilStackEmpty(t, e, bound)
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v after %d bounded priority passes -- Batterskull's spell and/or its "+
			"Living Weapon trigger did not resolve", e.G.Stack, bound)
	}
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("Batterskull zone = %s, want battlefield", got)
	}

	// The TokenCreate event itself is the proof this test exists to get:
	// events.Apply's TokenCreate case mints the object AND moves it onto the
	// battlefield in one step, with no separate logged MoveZone for that
	// placement (the same "mint and place atomically, nothing else to log"
	// shape TriggerPush's own Apply case uses for its stack object) --
	// events/apply_test.go and effects/token_test.go already cover that
	// placement directly, so this integration test's job is only to prove
	// the REAL Batterskull's REAL Living Weapon trigger actually reaches
	// events.TokenCreate at all, with the right stem and the right owner.
	var tokenCreate *events.Event
	for i := range e.L.Events[beforeEvents:] {
		ev := &e.L.Events[beforeEvents+i]
		if ev.Kind == events.TokenCreate && ev.Text == "b_0_0_phyrexian_germ" {
			tokenCreate = ev
		}
	}
	if tokenCreate == nil {
		t.Fatalf("no TokenCreate event for b_0_0_phyrexian_germ since the cast: %+v",
			e.L.Events[beforeEvents:])
	}
	if tokenCreate.Player != 0 {
		t.Fatalf("TokenCreate.Player = %d, want 0 (TokenOwner$ You, cast and controlled by seat 0)",
			tokenCreate.Player)
	}

	// Cross-check against the resulting object: a 0/0 Phyrexian Germ with no
	// Attach to grant Batterskull's own +4/+4 (Attach is unregistered -- Task
	// 14) is immediately lethal to itself under CR 704.5f, and this build's
	// own ceaseDeadTokens (Task 13) correctly cleans it up the moment it
	// dies -- so by the time passUntilStackEmpty returns, the germ is
	// legitimately gone from the battlefield again. That is the CORRECT,
	// expected interaction of two working systems (Token creation,
	// state-based actions) with one still-missing one (Attach); identity
	// fields (Owner/Controller/Card) are never reset by leaving the
	// battlefield, unlike Tapped/Damage/Counters/etc., so this can still
	// assert on them regardless of where the object ends up.
	var germID state.ObjID
	for i := range e.G.Objs {
		if e.G.Objs[i].IsToken {
			germID = e.G.Objs[i].ID
		}
	}
	if germID == 0 {
		t.Fatal("TokenCreate event logged, but no IsToken object exists -- Apply's own TokenCreate " +
			"case did not mint anything")
	}
	germ := e.G.Obj(germID)
	if germ.Owner != 0 || germ.Controller != 0 {
		t.Fatalf("germ token owner=%d controller=%d, want both 0", germ.Owner, germ.Controller)
	}
	if got := germ.Face().Name; got != wantGermName {
		t.Fatalf("token name = %q, want %q (b_0_0_phyrexian_germ)", got, wantGermName)
	}
	// Task 14: the Living Weapon's __kwLWAttach sub-ability now reaches a
	// working effects.Attach, so the germ is actually attached to Batterskull
	// and Batterskull's own EquippedBy static (+4/+4) keeps the 0/0 germ alive
	// at 4/4 -- the reason the token is still on the battlefield at the end,
	// rather than dying to CR 704.5f and being moved to ZCeased by
	// ceaseDeadTokens the way pre-14 it always did. assert the real attach.
	if got := germ.Zone; got != state.ZBattlefield {
		t.Fatalf("germ zone = %s after passUntilStackEmpty, want battlefield (Batterskull's +4/+4 "+
			"should have kept it alive at 4/4)", got)
	}
	if got := e.G.Obj(id).AttachedTo; got != germID {
		t.Fatalf("Batterskull.AttachedTo = %d, want %d (the living-weapon germ)", got, germID)
	}
	if got := e.Power(germID); got != 4 {
		t.Fatalf("germ power = %d, want 4 from Batterskull's +4/+4", got)
	}
	// identity fields (Owner/Controller/Card) are never reset by leaving the
	// battlefield, unlike Tapped/Damage/Counters/etc., so this remains
	// assertable regardless.
}
