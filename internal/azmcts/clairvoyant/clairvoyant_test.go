package clairvoyant

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchseat"
)

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

// Source is the injected factory and carries the same gate: an unallowed
// process is refused through it too (a seat whose Source skips the gate
// would leak the real engine's hidden zones).
func TestSourceRefusesUnlessAllowed(t *testing.T) {
	prev := clairvoyantAllowed.Load()
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
	clairvoyantAllowed.Store(false)
	if _, err := Source(searchseat.Env{}, nil); !errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("Source without AllowClairvoyant: %v, want ErrClairvoyantRefused", err)
	}
}
