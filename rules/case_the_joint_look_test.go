package rules

// The multi-target bare look's pacing ack, pinned END TO END on the real
// corpus card (lookack, task fb-20260917T232325Z-35cfca4b; the r2 review's
// CRITICAL): Case the Joint ("Draw two cards, then look at the top card of
// each player's library") carries `DB$ PeekAndReveal | Defined$ Player |
// NoReveal$ True`, and `Defined$ Player` resolves to BOTH seats — so the
// resolution walk holds two bare-look targets. The r1 gate consumed its
// answered flag at the FIRST bare-look target, so the later target's ack was
// never marked answered and every resume re-emitted the earlier notes and
// re-posed the later ack forever: a live table wedged on an endless Continue
// modal while the log filled with duplicate Secret notes. The per-target
// cursor (LookAckTarget = the decision's ResumeTarget, the DigTarget
// pattern) makes the chain terminate: one Continue and one Secret note per
// target, each prompt naming that target's own top library card.
import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCaseTheJointLookAcksEachTargetOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Case the Joint")},
		[]*cards.Card{})
	id := moveByName(t, e, 0, "Case the Joint", state.ZHand)
	addMana(t, e, 0, "UUUU")
	d := e.Pending()
	var cast *decision.Option
	for i := range d.Options {
		if d.Options[i].Kind == "cast" && d.Options[i].Obj == id {
			cast = &d.Options[i]
		}
	}
	if cast == nil {
		t.Fatalf("Case the Joint is not castable: %+v", d.Options)
	}
	submitChoices(t, e, cast.Index)
	viviPass(t, e)
	viviPass(t, e) // the spell resolves: the draws happen, then DBLook's walk
	if got, want := len(e.G.Zone(state.ZHand, 0)), 7+2; got != want {
		t.Fatalf("seat 0 hand %d after the draw, want %d", got, want)
	}
	// First look ack: target 0 (seat 0's own library), prompt naming its top
	// card; no Secret look note has landed yet (ask-first).
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "look_ack" || d.ResumeTarget != 0 {
		t.Fatalf("expected the first look ack bound to target 0, got %+v", d)
	}
	if top := e.G.Zone(state.ZLibrary, 0); len(top) > 0 {
		if name := e.G.Obj(top[0]).Face().Name; !strings.Contains(d.Prompt, name) {
			t.Fatalf("first ack prompt %q does not name seat 0's top card %q", d.Prompt, name)
		}
	}
	if looks := secretLookNoteCount(t, e); looks != 0 {
		t.Fatalf("%d Secret look notes before the first ack was answered", looks)
	}
	submitChoices(t, e, d.Options[0].Index)
	// Second look ack: target 1 (seat 1's library), its own Continue,
	// bound to ResumeTarget 1 — the cursor advanced, not consumed.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "look_ack" || d.ResumeTarget != 1 {
		t.Fatalf("expected the second look ack bound to target 1, got %+v", d)
	}
	if top := e.G.Zone(state.ZLibrary, 1); len(top) > 0 {
		if name := e.G.Obj(top[0]).Face().Name; !strings.Contains(d.Prompt, name) {
			t.Fatalf("second ack prompt %q does not name seat 1's top card %q", d.Prompt, name)
		}
	}
	if looks := secretLookNoteCount(t, e); looks != 1 {
		t.Fatalf("%d Secret look notes after the first target, want exactly 1", looks)
	}
	submitChoices(t, e, d.Options[0].Index)
	viviPass(t, e)
	viviPass(t, e)
	// Terminated: exactly one Secret look note per target (in target order,
	// seat 0's library then seat 1's), no public reveal leak, and no third
	// ack — the pending decision after the passes is ordinary priority.
	if looks := secretLookNoteCount(t, e); looks != 2 {
		t.Fatalf("%d Secret look notes total, want exactly one per target", looks)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) > 0 {
			t.Fatalf("a public reveal leaked: %+v", ev)
		}
	}
	d = e.Pending()
	if d != nil && d.ResumeKind == "look_ack" {
		t.Fatalf("the walk re-posed an ack after both targets were answered: %+v", d)
	}
}

// secretLookNoteCount counts the Secret Notes in the engine's log — the
// bare-look record shape.
func secretLookNoteCount(t *testing.T, e *Engine) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Secret {
			n++
		}
	}
	return n
}
