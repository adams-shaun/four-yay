package bots_test

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/all"
)

func TestRegister(t *testing.T) {
	for _, name := range []string{"bot", "lethal-pressure", "cast-profile", "search"} {
		if _, ok := bots.Lookup(name); !ok {
			t.Fatalf("built-in %q was not registered", name)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"", "bot"}, {"bot", "bot"}, {"lethal-pressure", "lethal-pressure"}, {"cast-profile", "cast-profile"}, {"search", "search"}} {
		got, err := bots.Normalize(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := bots.Normalize("no-such-policy"); err == nil {
		t.Fatal("Normalize(no-such-policy) accepted an unregistered policy")
	}
}

func TestEntries(t *testing.T) {
	entries := bots.Entries()
	if len(entries) != 4 {
		t.Fatalf("Entries() has %d entries, want 4", len(entries))
	}
	if entries[0].Name != "bot" || entries[0].Tier != bots.Production {
		t.Fatalf("production entry is not first: %+v", entries[0])
	}
	// BP-11: the search teacher is the registry's one experimental entry, and
	// it is an Env/search policy — asserted by name, not by index, so a later
	// experimental entry cannot silently re-order this test's expectations.
	var searchEntry *bots.Entry
	for i := range entries {
		if entries[i].Name == "search" {
			searchEntry = &entries[i]
		}
	}
	if searchEntry == nil {
		t.Fatal("no search entry in Entries()")
	}
	if searchEntry.Tier != bots.Experimental || !searchEntry.Env || !searchEntry.Search {
		t.Errorf("search entry is not the experimental Env policy: %+v", searchEntry.Info)
	}
	for _, e := range entries {
		if e.Label == "" || e.Description == "" || len(e.Strength) == 0 || e.Cost.Note == "" || len(e.Formats) == 0 {
			t.Errorf("entry %q has incomplete Info: %+v", e.Name, e.Info)
		}
	}
}

// TestValidateUnlinkedCaretakerRefused pins the caretaker rule from the
// UNLINKED side (the linked side is bots.Validate above): a caretaker name
// the binary has not registered is an error, not a silent nil.
func TestValidateUnlinkedCaretakerRefused(t *testing.T) {
	_, ok := bots.Lookup("definitely-not-registered")
	if ok {
		t.Fatal("precondition: a policy named definitely-not-registered is linked")
	}
	if _, err := bots.New("definitely-not-registered", bots.Options{}); err == nil {
		t.Error("New(definitely-not-registered) built a seat for an unregistered policy")
	}
}

func TestValidate(t *testing.T) {
	if err := bots.Validate(); err != nil {
		t.Fatal(err)
	}
}
