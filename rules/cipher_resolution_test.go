package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the timing of Cipher's encode (CR 702.99a): it is the
// resolving spell's own tail instruction, not a triggered ability. The old
// implementation expanded K:Cipher into a ChangesZone trigger, so the encode
// became a separate stack object opponents could respond to and counter, and
// a resolved spell that rested anywhere other than the graveyard never
// triggered it at all. See cards/kw_cipher.go and rules/cipher.go's
// cipherTailFor.

// driveToCipherEncodeAsk casts nothing itself; it drives the stack (passing on
// priority, taking option 0 elsewhere) until the Cipher encode ask is pending,
// and returns that decision. It fails rather than looping if the ask never
// arrives, so a "no extra stack object" assertion below cannot pass on a board
// that never reached the encode at all.
func driveToCipherEncodeAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) > 0 {
				e.priorityRound()
				continue
			}
			t.Fatal("stack emptied before the Cipher encode ask")
		}
		if d.Kind == decision.KModes && d.ResumeKind == "cipher" {
			return d
		}
		idx := 0
		if d.Kind == decision.KPriority {
			if p, ok := cipherFindOption(d, "pass"); ok {
				idx = p
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit %v: %v", d.Kind, err)
		}
	}
	t.Fatal("the Cipher encode ask was never posed")
	return nil
}

// TestCipherEncodeIsPartOfResolution proves the encode runs as the resolving
// spell's own instruction: at the moment the ask is pending there is exactly
// ONE stack object, and it is the spell itself (an ability would carry a
// non-nil Obj.Ability). Under the old triggered-ability expansion the encode
// resolved from a second stack object created after the spell had already left
// the stack, so opponents had a priority window in which to counter it while
// the spell was gone.
func TestCipherEncodeIsPartOfResolution(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, cipherProbeSrc)
	if !cipherHasResolutionTail(probe) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}
	e, cfg, cipherID, creatures := cipherBoard(t, reg, probe, cipherBeanSrc)
	creature := creatures[0]
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: eligible creature missing")
	}

	e.beginPlay(0, cipherID, true, "", false, false)
	if o := e.G.Obj(cipherID); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: the cast spell is not on the stack: %+v", o)
	}
	d := driveToCipherEncodeAsk(t, e)

	// The encode offer arrives while the spell is still resolving: exactly one
	// stack object, the spell, with no ability payload.
	if len(e.G.Stack) != 1 {
		t.Fatalf("Cipher encode ask posed with %d stack objects, want 1: %v", len(e.G.Stack), e.G.Stack)
	}
	so := e.G.Obj(e.G.Stack[0])
	if so == nil || so.ID != cipherID {
		t.Fatalf("the sole stack object at the encode ask is %+v, want the spell %d", so, cipherID)
	}
	if so.Ability != nil {
		t.Fatalf("the encode was delivered by a triggered/activated ability stack object: %+v", so.Ability)
	}

	idx, ok := cipherFindOption(d, "mode")
	if !ok {
		t.Fatalf("Cipher encode ask posed no creature option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cipher encode: %v", err)
	}
	drainCipher(t, e, 40, nil, nil)

	if got := cipherEncodedOn(e, creature); len(got) != 1 || got[0] != cipherID {
		t.Fatalf("creature %d encoded cards = %v, want [%d]", creature, got, cipherID)
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZExile {
		t.Fatalf("encoded card zone = %v, want exile", zoneOf(co))
	}
	replayCheck(t, e, cfg)
}

// cipherSuspendProbeSrc is an inline Cipher sorcery whose body poses a
// mid-resolution ask (the scry arrange) BEFORE the encode tail. It exercises
// the suspended-resolution continuation: the tail must survive the body's
// suspension in the continuation chain and run exactly once afterwards.
const cipherSuspendProbeSrc = "Name:Cipher Suspend Probe\nManaCost:0\nTypes:Sorcery\nK:Cipher\n" +
	"A:SP$ Scry | Defined$ You | ScryNum$ 2\n" +
	"Oracle:x\n"

// TestCipherEncodeRunsAfterSuspendedBody drives a Cipher spell whose own body
// suspends on a mid-resolution ask. The encode tail must be carried in the
// continuation chain and reached exactly once: the count assertion fails if a
// resumed resolution re-appends or drops it.
func TestCipherEncodeRunsAfterSuspendedBody(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, cipherSuspendProbeSrc)
	if !cipherHasResolutionTail(probe) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}
	e, cfg, cipherID, creatures := cipherBoard(t, reg, probe, cipherBeanSrc)
	creature := creatures[0]
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: eligible creature missing")
	}
	if n := len(e.G.Zone(state.ZLibrary, 0)); n < 2 {
		t.Fatalf("precondition: seat 0's library has %d cards, too few for the scry body to ask", n)
	}
	e.beginPlay(0, cipherID, true, "", false, false)
	sawBodyAsk, encodes := false, 0
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.priorityRound()
			continue
		}
		if d.Kind == decision.KModes && d.ResumeKind == "cipher" {
			encodes++
			if len(e.G.Stack) != 1 {
				t.Fatalf("encode ask posed with %d stack objects, want 1: %v", len(e.G.Stack), e.G.Stack)
			}
			if so := e.G.Obj(e.G.Stack[0]); so == nil || so.ID != cipherID || so.Ability != nil {
				t.Fatalf("encode ask stack object is not the resolving spell: %+v", so)
			}
			idx, ok := cipherFindOption(d, "mode")
			if !ok {
				t.Fatalf("Cipher encode ask posed no creature option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit cipher encode: %v", err)
			}
			continue
		}
		if d.Kind != decision.KPriority {
			sawBodyAsk = true
		}
		idx := 0
		if d.Kind == decision.KPriority {
			if p, ok := cipherFindOption(d, "pass"); ok {
				idx = p
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit %v: %v", d.Kind, err)
		}
	}
	if !sawBodyAsk {
		t.Fatal("precondition: the spell body posed no mid-resolution ask, so no suspension was exercised")
	}
	if encodes != 1 {
		t.Fatalf("the encode tail ran %d times across the suspension, want exactly 1", encodes)
	}
	if got := cipherEncodedOn(e, creature); len(got) != 1 || got[0] != cipherID {
		t.Fatalf("creature %d encoded cards = %v, want [%d]", creature, got, cipherID)
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZExile {
		t.Fatalf("encoded card zone = %v, want exile", zoneOf(co))
	}
	replayCheck(t, e, cfg)
}

// TestCipherEncodeAppliesRegardlessOfRestZone proves the encode does not depend
// on the spell first reaching the graveyard. The resolving spell carries
// state.FlagReplaceGraveyard, so its own resolution tail would rest it in
// exile, not the graveyard. The old trigger was gated on Destination$
// Graveyard, so that resting-zone outcome suppressed the encode entirely; the
// new tail runs before the move and is unaffected.
//
// The decline half asserts the precondition that makes the test non-vacuous:
// with the flag set and the encode declined, the card genuinely rests in
// EXILE, so the accepted half below cannot pass because the spell would have
// gone to the graveyard anyway.
func TestCipherEncodeAppliesRegardlessOfRestZone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	probe := card(t, cipherProbeSrc)
	if !cipherHasResolutionTail(probe) {
		t.Fatal("precondition: Cipher keyword did not expand")
	}

	// Control: with the encode declined, the flag rests the spell in exile.
	{
		e, cfg, cipherID, creatures := cipherBoard(t, reg, probe, cipherBeanSrc)
		creature := creatures[0]
		if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
			t.Fatal("precondition: eligible creature missing")
		}
		e.beginPlay(0, cipherID, true, "", true, false)
		if o := e.G.Obj(cipherID); o == nil || o.CastFlags&state.FlagReplaceGraveyard == 0 {
			t.Fatalf("precondition: the resolving spell does not carry the exile resting-zone flag: %+v", o)
		}
		d := driveToCipherEncodeAsk(t, e)
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
			t.Fatalf("submit cipher encode decline: %v", err)
		}
		drainCipher(t, e, 40, nil, nil)
		if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZExile {
			t.Fatalf("declined encode with the flag set: card zone = %v, want exile", zoneOf(co))
		}
		if got := cipherEncodedOn(e, creature); len(got) != 0 {
			t.Fatalf("declined encode still linked %v", got)
		}
		replayCheck(t, e, cfg)
	}

	// The encode is still offered, and still applies, when the spell's own
	// resting zone is exile.
	e, cfg, cipherID, creatures := cipherBoard(t, reg, probe, cipherBeanSrc)
	creature := creatures[0]
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: eligible creature missing")
	}
	e.beginPlay(0, cipherID, true, "", true, false)
	if o := e.G.Obj(cipherID); o == nil || o.CastFlags&state.FlagReplaceGraveyard == 0 {
		t.Fatalf("precondition: the resolving spell does not carry the exile resting-zone flag: %+v", o)
	}
	d := driveToCipherEncodeAsk(t, e)
	idx, ok := cipherFindOption(d, "mode")
	if !ok {
		t.Fatalf("Cipher encode ask posed no creature option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cipher encode: %v", err)
	}
	drainCipher(t, e, 40, nil, nil)

	if got := cipherEncodedOn(e, creature); len(got) != 1 || got[0] != cipherID {
		t.Fatalf("creature %d encoded cards = %v, want [%d]", creature, got, cipherID)
	}
	if co := e.G.Obj(cipherID); co == nil || co.Zone != state.ZExile {
		t.Fatalf("encoded card zone = %v, want exile", zoneOf(co))
	}
	replayCheck(t, e, cfg)
}
