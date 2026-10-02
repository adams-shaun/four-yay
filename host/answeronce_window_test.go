package host

import "testing"

// TestAnswerOnceWaitsPastTheStaleAskWindow pins the seq-aware shape of
// answerOnce: between SubmitIntent's acceptance and the match goroutine
// consuming the answer, the human seat's slot is still installed, so a
// first-200 Pending poll re-serves the just-answered decision. answerOnce's
// initial poll must therefore wait for a Seq past the previously answered
// one; a first-200 poll would re-serve the stale ask, the duplicate submit
// would be orphaned (or rejected once the slot re-parks), and the helper
// would return the answered decision as if it were the next ask.
func TestAnswerOnceWaitsPastTheStaleAskWindow(t *testing.T) {
	t.Parallel()
	r, id := startHumanTable(t)
	d1 := waitPending(t, r, id, 1, 0) // pre-answer: waitPending is fine here
	if err := r.SubmitIntent(id, 1, 0, legalIntent(d1)); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}
	// Called with no delay after the submit, answerOnce's first Pending
	// lands inside the stale window (the parked await has not yet woken to
	// clear its slot).
	d2 := answerOnce(t, r, id, d1.Seq)
	if d2.Seq <= d1.Seq {
		t.Fatalf("answerOnce returned seq %d after answering seq %d: it re-served the stale ask", d2.Seq, d1.Seq)
	}
}
