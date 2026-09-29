package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestSetAudit_spm_MisterNegative_InversionExchangeLife(t *testing.T) {
	assertExchangeLifeCorpusCensus(t)
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Mister Negative")
	// The real zone-change trigger must pose its target ask; the exchange
	// body must then execute, including its RememberOwnLoss draw rider.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -5})
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 15 {
		t.Fatal("precondition: life totals not 20/15")
	}
	before := len(e.G.Zone(state.ZHand, 0))
	id := searchMoveByName(t, e, "Mister Negative", state.ZBattlefield)
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("precondition: Mister Negative not on battlefield")
	}
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		if d.Kind == decision.KTarget {
			idx := indexOfPlayerOption(d, 1)
			if idx < 0 {
				t.Fatalf("opponent not offered: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			break
		}
		if d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, 0)
			continue
		}
		if d.Kind == decision.KChoose {
			submitChoices(t, e, 0)
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected ask: %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatal("no pass")
		}
		submitChoices(t, e, idx)
	}
	for i := 0; i < 40 && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		if d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, 0)
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("pending during exchange: %s", d.Kind)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatal("no pass")
		}
		submitChoices(t, e, idx)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API ExchangeLife" {
			t.Fatal("ExchangeLife reached the unimplemented API fallback")
		}
	}
	if e.G.Players[0].Life != 15 || e.G.Players[1].Life != 20 {
		t.Fatalf("life = %d/%d, want 15/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - before; got != 5 {
		t.Fatalf("draw = %d, want 5", got)
	}
}

// Keep the mechanism-class census beside the regression: this is not a
// Mister Negative-only API, and all corpus scripts using it must be covered.
func assertExchangeLifeCorpusCensus(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", ".cards", "cardsfolder")
	var got []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "ExchangeLife") {
			got = append(got, filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan ExchangeLife corpus: %v", err)
	}
	sort.Strings(got)
	want := []string{
		"axis_of_mortality.txt", "cliffside_market.txt", "evra_halcyon_witness.txt",
		"magus_of_the_mirror.txt", "mirror_universe.txt", "mister_negative.txt",
		"profane_transfusion.txt", "psychic_transfer.txt", "soul_conduit.txt",
		"tree_of_perdition.txt", "tree_of_redemption.txt",
	}
	if len(got) != 11 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ExchangeLife corpus census = %v, want the 11 known card scripts %v", got, want)
	}
}
