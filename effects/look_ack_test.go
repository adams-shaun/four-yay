package effects

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// saWithSubs is sa() plus the SVar bodies a SubAbility$ chain resolves
// through (cards.Link, the same mechanism the compiled corpus uses —
// Ctx.SVars alone does not link a SubAbility$ chain).
func saWithSubs(t *testing.T, line string, svars ...string) *cards.SA {
	t.Helper()
	src := "Name:T\nTypes:Sorcery\nA:" + line
	for _, sv := range svars {
		src += "\nSVar:" + sv
	}
	src += "\nOracle:x\n"
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	return c.Faces[0].Abilities[0]
}

// The bare private look's pacing gate (lookack, task
// fb-20260917T232325Z-35cfca4b): effReveal's two bare arms — NoReveal$ True
// (Mishra's Bauble) and the mandatory Look$ True arm (Gitaxian Probe) — used
// to land their Secret Note with NO decision attached, so a client's
// auto-passing priority streamed the line past before the player could read
// it ("im not sure if other players hand was shown to me... happened way too
// fast"). Both arms now pose a one-option "Continue" KChoose (ResumeKind
// "look_ack") BEFORE the note; the answer re-enters through rules'
// resumeResolution with Ctx.LookAck set together with Ctx.LookAckTarget (the
// decision's ResumeTarget, the index of the Defined$ target that asked), and
// the note lands below the modal. The ask gates only the pacing — there is
// no decline.

// TestBareNoRevealLookPosesTheAckBeforeTheNote pins the NoReveal$ arm's ask
// (Mishra's Bauble's shape): the ack is posed to the looker, names the
// looked-at card and the zone in its prompt, and NO note lands before the
// answer — the chained SubAbility$ (Mishra's slowtrip) must not run before
// the ack either.
func TestBareNoRevealLookPosesTheAckBeforeTheNote(t *testing.T) {
	h, ids := revealBoard(t) // seat 0's library: Bolt on top
	sh := &suspendHost{fakeHost: *h}
	ctx := &Ctx{Controller: 0, Source: ids[2]}
	sa := saWithSubs(t, "SP$ PeekAndReveal | Defined$ You | NoReveal$ True | SubAbility$ DBPing",
		"DBPing:DB$ LoseLife | Defined$ You | LifeAmount$ 1")
	Resolve(sh, ctx, sa)
	if !sh.suspended || sh.asked == nil {
		t.Fatalf("the bare NoReveal$ look posed no ack decision (log %+v)", sh.log)
	}
	d := sh.asked
	if d.Player != 0 {
		t.Fatalf("ack player = %d, want the looker (seat 0)", d.Player)
	}
	if d.Kind != decision.KChoose {
		t.Fatalf("ack kind = %s, want KChoose", d.Kind)
	}
	if d.ResumeKind != "look_ack" {
		t.Fatalf("ResumeKind = %q, want look_ack", d.ResumeKind)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("min/max = %d/%d, want 1/1", d.Min, d.Max)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "yes" || d.Options[0].Label != "Continue" {
		t.Fatalf("options = %+v, want a single Continue", d.Options)
	}
	if !strings.Contains(d.Prompt, "library") || !strings.Contains(d.Prompt, "Bolt") {
		t.Fatalf("prompt = %q, want it to name the card and the zone", d.Prompt)
	}
	// Ask-first: the look note and the chained sub must not run before the
	// answer.
	for _, e := range sh.log {
		if e.Kind == events.Note {
			t.Fatalf("a Note landed before the ack: %+v", e)
		}
		if e.Kind == events.LifeChange {
			t.Fatalf("the chained sub ran before the ack: %+v", e)
		}
	}
}

// TestBareNoRevealLookAckNoHostEmitsImmediately pins the R-9 fallback: a
// host that cannot ask keeps the pre-gate behaviour — the look emitted
// immediately, no decision, deterministically.
func TestBareNoRevealLookAckNoHostEmitsImmediately(t *testing.T) {
	h, ids := revealBoard(t)
	ctx := &Ctx{Controller: 0, Source: ids[2]}
	Resolve(h, ctx, sa(t, "SP$ PeekAndReveal | Defined$ You | NoReveal$ True"))
	notes := secretLookNotes(h.log)
	if len(notes) != 1 {
		t.Fatalf("got %d Secret look Notes (%+v), want exactly one", len(notes), h.log)
	}
	if notes[0].Player != 0 || !slices.Equal(notes[0].IDs, []state.ObjID{ids[0]}) {
		t.Fatalf("look Note = %+v, want the immediate look for seat 0", notes[0])
	}
}
