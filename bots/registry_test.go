package bots_test

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/all"
)

func TestRegister(t *testing.T) {
	for _, name := range []string{"bot", "lethal-pressure", "cast-profile"} {
		if _, ok := bots.Lookup(name); !ok {
			t.Fatalf("built-in %q was not registered", name)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"", "bot"}, {"bot", "bot"}, {"lethal-pressure", "lethal-pressure"}, {"cast-profile", "cast-profile"}} {
		got, err := bots.Normalize(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := bots.Normalize("search"); err == nil {
		t.Fatal("Normalize(search) accepted an unregistered policy")
	}
}

func TestEntries(t *testing.T) {
	entries := bots.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() has %d entries, want 3", len(entries))
	}
	if entries[0].Name != "bot" || entries[0].Tier != bots.Production {
		t.Fatalf("production entry is not first: %+v", entries[0])
	}
	for _, e := range entries {
		if e.Label == "" || e.Description == "" || len(e.Strength) == 0 || e.Cost.Note == "" || len(e.Formats) == 0 {
			t.Errorf("entry %q has incomplete Info: %+v", e.Name, e.Info)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := bots.Validate(); err != nil {
		t.Fatal(err)
	}
}
