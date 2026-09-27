package host

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/view"
)

func TestMaxOnDemandTablesRefusesNewGameWithoutEvictingExistingPrivateTable(t *testing.T) {
	opts := testOptions(t)
	opts.MaxOnDemandTables = 1
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	first := TableConfig{ID: "g1", Seats: 2, Decks: []string{"a", "b"}, Spectator: view.Public, OnDemand: true}
	if err := r.AddTable(first); err != nil {
		t.Fatal(err)
	}
	if err := r.AddTable(TableConfig{ID: "g2", Seats: 2, Decks: []string{"a", "b"}, Spectator: view.Public, OnDemand: true}); err == nil || !strings.Contains(err.Error(), "on-demand table limit 1") {
		t.Fatalf("second private table error = %v, want configured limit", err)
	}
	if got := r.Tables(); len(got) != 1 || got[0].ID != "g1" {
		t.Fatalf("limit evicted or exposed first private table: %+v", got)
	}
	// Durable hosted tables do not consume the private-game budget.
	if err := r.AddTable(TableConfig{ID: "t1", Seats: 2, Decks: []string{"a", "b"}, Spectator: view.Public}); err != nil {
		t.Fatalf("durable table rejected by private limit: %v", err)
	}
}

func TestNegativeMaxOnDemandTablesIsRejected(t *testing.T) {
	opts := testOptions(t)
	opts.MaxOnDemandTables = -1
	if _, err := New(opts); err == nil || !strings.Contains(err.Error(), "MaxOnDemandTables -1") {
		t.Fatalf("New negative on-demand limit error = %v", err)
	}
}
