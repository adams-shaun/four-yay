package adopt

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/shape"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestTicketsAtHead holds the C4 generator's contract over the committed
// data: one ticket per class (stable unique ids, never one per card in a
// shape), a complete brief with a class-census skeleton that parses, no
// ticket for a non-tournament primitive, and no primitive ticket whose
// cards another primitive ticket already covers (a duplicate fix).
func TestTicketsAtHead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cs := census(t)
	ts, notes, err := cs.Tickets(reg, root, TicketOptions{MinCards: 10, AnyIn: cs.Config.Formats[0].Name})
	if err != nil {
		t.Fatal(err)
	}
	impact := map[string]Impact{}
	for _, r := range cs.Impact() {
		impact[r.Primitive] = r
	}
	seen := map[string]bool{}
	byClass := map[string]int{}
	var prim []Ticket
	for _, tk := range ts {
		byClass[tk.Class]++
		if seen[tk.ID] {
			t.Errorf("duplicate ticket id %s", tk.ID)
		}
		seen[tk.ID] = true
		for _, want := range []string{"## Done means", "## Class-census ratchet", "```go\n", "## Out of scope"} {
			if !strings.Contains(tk.Body, want) {
				t.Errorf("%s: brief lacks %q", tk.ID, want)
			}
		}
		if len(tk.Cards) == 0 {
			t.Errorf("%s: no cards", tk.ID)
		}
		if tk.Class == ClassPrimitive {
			prim = append(prim, tk)
			for _, p := range tk.Primitives {
				if impact[p].NonTournament {
					t.Errorf("%s: non-tournament primitive %s got a ticket", tk.ID, p)
				}
			}
		}
	}
	for i, a := range prim {
		for j, b := range prim {
			if i == j {
				continue
			}
			covered := map[string]bool{}
			for _, c := range b.Cards {
				covered[c] = true
			}
			sub := true
			for _, c := range a.Cards {
				sub = sub && covered[c]
			}
			if sub {
				t.Errorf("%s's cards are all %s's: one class, two tickets", a.ID, b.ID)
			}
		}
	}
	t.Logf("tickets: %d (%v); not filed: %v", len(ts), byClass, notes)
}

// TestFixTicketsGroupByRuling: gorge_wrong rows group by their shape
// ruling -- one ticket for the ruling however many cards it covers --
// and a ruling that already names a ticket is reported, not re-filed.
func TestFixTicketsGroupByRuling(t *testing.T) {
	cs := synthetic()
	row := func(card, ruling, detail string) compliance.VerdictRow {
		return compliance.VerdictRow{Card: card, Template: "cast-resolve", Status: compliance.StatusGorgeWrong,
			RulingID: ruling, Ruling: "gorge is wrong", Detail: detail}
	}
	v := map[string]map[string]compliance.VerdictRow{
		"a": {"cast-resolve": row("a", "r1", `step 1 (resolve) permanents: gorge "x", xmage "y"`)},
		"b": {"cast-resolve": row("b", "r1", `step 1 (resolve) permanents: gorge "x", xmage "y"`)},
		"c": {"cast-resolve": row("c", "r2", `step 1 (resolve) permanents: gorge "x", xmage "y"`)},
		"d": {"cast-resolve": row("d", "", `step 1 (resolve) p0.life: gorge "20", xmage "17"`)},
	}
	rulings := []shape.Ruling{
		{ID: "r1", Status: compliance.StatusGorgeWrong, Ruling: "levels are not counters", CR: []string{"716.2b"}},
		{ID: "r2", Status: compliance.StatusGorgeWrong, Ruling: "elsewhere", Ticket: "agent-123"},
	}
	ts, notes := cs.fixTickets(testutil.CorpusRegistry(t), v, rulings)
	if len(notes) != 1 || !strings.Contains(notes[0], "agent-123") {
		t.Errorf("notes %v, want the r2 ruling's ticket", notes)
	}
	if len(ts) != 2 {
		t.Fatalf("tickets %+v, want r1 and d's shape", ts)
	}
	var r1 *Ticket
	for i := range ts {
		if ts[i].ID == "compliance-fix-r1" {
			r1 = &ts[i]
		}
	}
	if r1 == nil || len(r1.Cards) != 2 || r1.Priority != 1 || !strings.Contains(r1.Body, "levels are not counters") {
		t.Fatalf("r1 ticket %+v", r1)
	}
	if _, err := formatGoBlocks(r1.Body); err != nil {
		t.Errorf("r1 skeleton: %v", err)
	}
}
