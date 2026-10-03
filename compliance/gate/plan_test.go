package gate

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func corpus(t *testing.T) *cards.Registry {
	t.Helper()
	reg, err := cards.LoadRegistry(cards.CachePath(filepath.Join("..", "..", ".cards")))
	if err != nil {
		t.Fatalf("the compliance gate needs the corpus (make fetch-cards compile-cards): %v", err)
	}
	return reg
}

// TestPlanItemRerunsOnlyStaleRows: a pass skips a passing verdict for the
// same scenario and XMAGE_REF that gorge still meets, recomputes every
// other one, and replays in XMage only what the cache lacks (C1).
func TestPlanItemRerunsOnlyStaleRows(t *testing.T) {
	reg := corpus(t)
	it, skip := oraclegen.Generate(reg, "Shock")
	if skip != nil {
		t.Fatal(skip.Reason)
	}
	canon, err := GorgeCanon(reg, it)
	if err != nil {
		t.Fatal(err)
	}
	const ref = "ref1"
	pass := compliance.VerdictRow{Card: it.Card, Template: it.Template, ID: it.ID, ScenarioSHA: ItemSHA(it),
		XMageRef: ref, Status: compliance.StatusAgree, CanonSHA: canon}
	cache := oraclediff.Cache{Dir: t.TempDir()}
	if n, why := PlanItem(reg, it, pass, true, ref, cache); n != Fresh {
		t.Fatalf("passing row: %v (%s), want fresh", n, why)
	}
	stale := map[string]compliance.VerdictRow{}
	r := pass
	r.XMageRef = "ref0"
	stale["xmage_ref changed"] = r
	r = pass
	r.ScenarioSHA = "old"
	stale["scenario changed"] = r
	r = pass
	r.Status = compliance.StatusDiverge
	stale["status diverge"] = r
	r = pass
	r.CanonSHA = "moved"
	stale["gorge no longer meets the frozen expectation"] = r
	for want, row := range stale {
		if n, why := PlanItem(reg, it, row, true, ref, cache); n != Replay || why != want {
			t.Errorf("%s: got %v (%s), want replay", want, n, why)
		}
	}
	if n, why := PlanItem(reg, it, compliance.VerdictRow{}, false, ref, cache); n != Replay || why != "no verdict" {
		t.Errorf("missing row: %v (%s)", n, why)
	}
	if err := cache.Put(ItemSHA(it), oraclediff.XResult{ID: it.ID}); err != nil {
		t.Fatal(err)
	}
	if n, _ := PlanItem(reg, it, stale["status diverge"], true, ref, cache); n != Rediff {
		t.Errorf("cached stale row: %v, want rediff", n)
	}
}
