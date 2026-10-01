package searchbench

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// fallbackStore is the sb-v1 item store the fallback test reads
// (SEARCHBENCH_STORE, else the training store's sb-v1 build; never
// committed). The test skips when it is absent.
func fallbackStore(t *testing.T) map[string]*StoreItem {
	t.Helper()
	path := os.Getenv("SEARCHBENCH_STORE")
	if path == "" {
		path = "/mnt/sata/gorge-training/searchbench/sb-v1/gorge2/items.jsonl.gz"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no sb-v1 item store at %s (SEARCHBENCH_STORE)", path)
	}
	store, err := ReadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestFullRootOnItemsThatFellBack: the three smoke items (sb-v1-gorge
// test-0954, test-1109, test-1285; smoke40 test-0046/0057/0074) on which
// every search arm played the bot's answer because the auto-pay bot's root
// answer was a mana tap (Fallback "skipped", no tree). With the full root
// they are searched: no fallback, every simulation completes, the bot's
// candidate is never a mana tap, and the root covers every canonical
// option except those the engine proves unpayable (test-1109's three casts
// that only an unconfigured Heraldic Banner could pay).
func TestFullRootOnItemsThatFellBack(t *testing.T) {
	store := fallbackStore(t)
	reg := testutil.CorpusRegistry(t)
	clairvoyant.AllowClairvoyant()
	for id, unreached := range map[string]int{"sb-v1-test-0954": 0, "sb-v1-test-1109": 3, "sb-v1-test-1285": 0} {
		s := store[id]
		if s == nil {
			t.Fatalf("%s is not in the store", id)
		}
		pos, err := PositionItem(reg, s, WorldCount)
		if err != nil {
			t.Fatal(err)
		}
		real := pos.Real.M.Engine
		seed := ItemSeed(0, id)
		botSeed := splitMix64(seed ^ 0x626f742d61727365)
		if raw := botAnswer(real, botSeed); !IsManaActivation(real.Pending(), raw) {
			t.Fatalf("%s: the bot's root answer %+v is not a mana tap (the precondition)", id, raw)
		}
		it := s.ManifestItem("g", "d")
		for _, arm := range []SearchArm{ArmClairvoyant, ArmPIMC1, ArmPIMC4, ArmISMCTS} {
			const sims = 24
			r, err := RunItem(context.Background(), reg, it, s, RunConfig{Arm: arm, Name: string(arm), Sims: sims})
			if err != nil {
				t.Fatalf("%s %s: %v", id, arm, err)
			}
			if r.Fallback != "" || r.Sims != sims || r.Completed != sims {
				t.Fatalf("%s %s: fallback %q, %d/%d simulations", id, arm, r.Fallback, r.Completed, r.Sims)
			}
			if got, want := len(r.Root), len(it.Options)-unreached; got != want || r.RootUnreached != unreached {
				t.Fatalf("%s %s: root covers %d canonical options (%d unreached), want %d (%d)", id, arm, got, r.RootUnreached, want, unreached)
			}
			in := ArmInput{Arm: arm, Options: BenchOptions(sims), Seed: seed, Real: real, Worlds: pos.WorldEngines()}
			res, err := RunArm(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Searched || res.Stats.Skipped != 0 || len(res.Table) < 2 || strings.Contains(res.Table[0].Label, "for mana") {
				t.Fatalf("%s %s: searched %v skipped %d, bot candidate %q", id, arm, res.Searched, res.Stats.Skipped, res.Table[0].Label)
			}
		}
	}
}
