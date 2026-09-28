package azmcts

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// allowClairvoyantForTest opens the clairvoyant gate for one test and
// restores it after.
func allowClairvoyantForTest(t testing.TB) {
	t.Helper()
	prev := clairvoyantAllowed.Load()
	clairvoyantAllowed.Store(true)
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
}

func TestClairvoyantRefusedUnlessAllowed(t *testing.T) {
	prev := clairvoyantAllowed.Load()
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
	clairvoyantAllowed.Store(false)
	if _, err := NewClairvoyant(nil, nil); !errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("NewClairvoyant without AllowClairvoyant: %v, want ErrClairvoyantRefused", err)
	}
	AllowClairvoyant()
	if _, err := NewClairvoyant(nil, nil); err == nil || errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("allowed, nil engine: %v, want a plain refusal", err)
	}
}

// A panic inside a world's engine is recovered and classified.
func TestSubmitRecoversAnEnginePanic(t *testing.T) {
	env := &engineEnv{} // nil engine: Submit dereferences it and panics
	if err := env.submit(&decision.Decision{}, decision.Intent{}); !errors.Is(err, ErrPanic) {
		t.Fatalf("submit on a nil engine: %v, want ErrPanic", err)
	}
}

// In a hypothetical world an intent the decision itself rejects is a submit
// error, never a chance failure.
func TestHypotheticalSubmitClassifiesRejections(t *testing.T) {
	d := passOrAbility(3)
	bad := decision.Intent{Seq: 99, Player: 0, Choices: []int{0}}
	if err := (&engineEnv{hyp: true}).submit(d, bad); !errors.Is(err, ErrSubmit) || errors.Is(err, ErrChance) {
		t.Fatalf("rejected intent in a hypothetical world: %v, want ErrSubmit", err)
	}
}
