//go:build policyexp

package main

// Feature-family experiment (ticket agent-20260921T012459Z-cb7a7077):
// priority/cast teacher overrides are not learnable from the option encoding.
//
// This is a MEASUREMENT vehicle, not a shipped test: it is behind the
// `policyexp` build tag (so `go test` without it never compiles it and it
// never counts as a skipped top-level test) and the test itself additionally skips unless
// POLICYNET_EXP_CORPUS names a real searchteacher label corpus. It answers
// the ticket's "Done means" item 1 -- does any additional feature family lift
// the `priority` (the corpus's cast kind) holdout top-1 above the bot
// baseline -- by training the SAME pure-CE + residual model the trainer
// ships, with a per-option augmentation (policynet.Option.Extra) built from a
// named family.
//
// Families (exact widths; see candidateExtra):
//
//	none         width 0  the shipped encoder (EncoderHash unchanged)
//	oracle       width 2  LEAK: the teacher's own candidate value + margin.
//	                      The capacity control -- if even this cannot beat the
//	                      bot, the harness or the corpus is broken.
//	board        width 16 the fuller redacted board snapshot (traceboard.Board)
//	                      the label record carries but Load deliberately drops:
//	                      per-option Castable/InstantSpeed/Counter/ManaValue/
//	                      Power/OnBattlefield/Activated/produces, plus global
//	                      life, board power, hand size and available mana.
//	board+oracle width 18 board ∪ oracle.
//
// Run: POLICYNET_EXP_CORPUS=.ds4/corpus/dev.jsonl \
//         go test -tags policyexp ./cmd/policytrain/ -run TestFeatureFamilyExperiment -v

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/traceboard"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// expRecord is the subset of the label record this harness needs to rebuild a
// per-option Extra vector; the authoritative Example (targets, encodings)
// comes from policynet.Load so the `none` family is exactly the shipped path.
type expRecord struct {
	RecordType    string            `json:"record_type"`
	SchemaVersion int               `json:"schema_version"`
	Kind          decision.Kind     `json:"kind"`
	Seat          state.PlayerID    `json:"seat"`
	Sequence      uint64            `json:"decision_sequence"`
	Board         traceboard.Board  `json:"board"`
	View          json.RawMessage   `json:"view"`
	Options       []decision.Option `json:"options"`
	Candidates    []struct {
		Choices []int   `json:"choices"`
		Bot     bool    `json:"bot"`
		Value   float64 `json:"value"`
		Worlds  int     `json:"worlds"`
	} `json:"candidates"`
	TeacherChoice int     `json:"teacher_choice"`
	BotIndex      int     `json:"bot_index"`
	Margin        float64 `json:"margin"`
	Worlds        int     `json:"worlds"`
}

// expFamilies is the fixed family vocabulary and its augmentation width.
var expFamilies = map[string]int{
	"none":         0,
	"oracle":       2,
	"board":        16,
	"board+oracle": 18,
}

func TestFeatureFamilyExperiment(t *testing.T) {
	path := os.Getenv("POLICYNET_EXP_CORPUS")
	if path == "" {
		t.Skip("set POLICYNET_EXP_CORPUS to a searchteacher label corpus to run the feature-family experiment")
	}
	epochs, _ := strconv.Atoi(envOr("POLICYNET_EXP_EPOCHS", "30"))
	seeds := strings.Split(envOr("POLICYNET_EXP_SEEDS", "1"), ",")
	lr, _ := strconv.ParseFloat(envOr("POLICYNET_EXP_LR", "0.1"), 64)
	clip, _ := strconv.ParseFloat(envOr("POLICYNET_EXP_CLIP", "1"), 64)
	famFilter := map[string]bool{}
	for _, f := range strings.Split(envOr("POLICYNET_EXP_FAMILIES", "none,oracle,board,board+oracle"), ",") {
		famFilter[strings.TrimSpace(f)] = true
	}

	exs, stats, err := policynet.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	// Decode the raw records in the SAME order Load accepted them, so the
	// Extra vectors zip against the authoritative Examples. Load appends one
	// Example per record whose candidates list is non-empty and whose
	// schema/record type match; mirror that skip exactly.
	recs, err := decodeExpRecords(path)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(recs) != len(exs) {
		t.Fatalf("record/example count mismatch: %d raw accepted vs %d loaded (stats %+v)", len(recs), len(exs), stats)
	}

	famNames := []string{"none", "oracle", "board", "board+oracle"}
	var residuals []float64
	for _, r := range strings.Split(envOr("POLICYNET_EXP_RESIDUALS", "0,2"), ",") {
		v, _ := strconv.ParseFloat(strings.TrimSpace(r), 64)
		residuals = append(residuals, v)
	}
	for _, seedStr := range seeds {
		seed, _ := strconv.ParseInt(strings.TrimSpace(seedStr), 10, 64)
		t.Logf("=== corpus %s: %d examples, seed %d, epochs %d ===", path, len(exs), seed, epochs)
		for _, fam := range famNames {
			if !famFilter[fam] {
				continue
			}
			aug := augment(exs, recs, fam)
			if n := firstExtraLen(aug); n > 0 {
				for _, k := range probeByKind(aug) {
					t.Logf("  %-13s LEAK-PROBE kind=%-9s naive=%.3f strict=%.3f weak=%.3f n=%d",
						fam, k.Kind, k.Naive, k.Strict, k.Weak, k.N)
				}
			}
			if fam == "oracle" || fam == "board+oracle" {
				for _, k := range oracleBotTiebreakByKind(exs) {
					t.Logf("  %-13s ORACLE+bot-tiebreak kind=%-9s value=%.3f valueBotTie=%.3f bot=%.3f n=%d",
						fam, k.Kind, k.ValueTop1, k.ValueBotTie, k.BotTop1, k.N)
				}
				for _, k := range oracleHoldoutByKind(exs, seed) {
					t.Logf("  %-13s ORACLE-HOLDOUT kind=%-9s valueBotTie=%.3f bot=%.3f n=%d",
						fam, k.Kind, k.ValueBotTie, k.BotTop1, k.N)
				}
			}
			for _, residual := range residuals {
				cfg := Config{
					Epochs: epochs, Batch: 64, LR: lr, Seed: seed, Holdout: 0.1,
					Embed: 128, Hidden: 128, Mode: policynet.LossCE,
					RankWeight: policynet.DefaultRankWeight, HuberDelta: policynet.DefaultHuberDelta,
					OverrideWeight: 1, ResidualInit: residual, Clip: clip, ExtraW: expFamilies[fam],
				}
				res, err := Train(aug, cfg)
				if err != nil {
					t.Fatalf("train %s residual %g: %v", fam, residual, err)
				}
				// Reproduce Train's split to read the model per-kind on BOTH sides:
				// the point is whether the model can fit the teacher on TRAIN.
				usable, sp := expSplit(aug, seed)
				for _, k := range evaluateByKind(res.Model, usable, sp.train) {
					if k.Kind == decision.KPriority {
						t.Logf("  %-13s residual=%-4g TRAIN-fit kind=priority model=%.3f bot=%.3f n=%d", fam, residual, k.ModelTop1, k.BotTop1, k.Eligible)
					}
				}
				for _, k := range res.ByKind {
					if k.Kind != decision.KPriority {
						continue
					}
					last := res.Epochs[len(res.Epochs)-1]
					t.Logf("  %-13s residual=%-4g kind=%-9s model=%.3f bot=%.3f first=%.3f random=%.3f n=%d trainTop1=%.3f holdoutTop1=%.3f",
						fam, residual, k.Kind, k.ModelTop1, k.BotTop1, k.FirstTop1, k.RandomTop1, k.Eligible, last.TrainTop1, last.HoldoutTop1)
				}
			}
		}
	}
}

// augment returns a copy of exs whose options carry the named family's Extra
// vector. The State and the encoded Slots/Hashed/Dense are untouched, so a
// family changes ONLY the experimental augmentation.
func augment(exs []policynet.Example, recs []expRecord, family string) []policynet.Example {
	out := make([]policynet.Example, len(exs))
	for i := range exs {
		out[i] = exs[i]
		out[i].Options = append([]policynet.Option(nil), exs[i].Options...)
		extras := candidateExtra(&recs[i], family)
		for j := range out[i].Options {
			if j < len(extras) {
				out[i].Options[j].Extra = extras[j]
			}
		}
	}
	return out
}

// candidateExtra builds one Extra vector per option for a family. It reads the
// record's view (for the option's card facts) and its board snapshot (the
// fuller redacted facts Load drops). Deterministic: fixed iteration, no maps.
func candidateExtra(rec *expRecord, family string) [][]float32 {
	switch family {
	case "none":
		return nil
	}
	var v view.View
	_ = json.Unmarshal(rec.View, &v) // a decode failure yields the zero view, never a panic

	// Board lookups: option card_id -> board card, plus global totals.
	boardCard := map[state.ObjID]traceboard.Card{}
	for _, c := range rec.Board.Cards {
		boardCard[c.CardID] = c
	}
	myLife, oppLife := 0, 0
	seenLife := false
	for _, l := range rec.Board.Life {
		if l.Player == rec.Seat {
			myLife = int(l.Life)
			seenLife = true
		} else if !seenLife || oppLife == 0 {
			oppLife = int(l.Life)
		}
	}
	myPower, oppPower, handSize := 0, 0, 0
	for _, ct := range rec.Board.Creatures {
		if ct.Controller == rec.Seat {
			myPower += int(ct.Power)
		} else {
			oppPower += int(ct.Power)
		}
	}
	for i := range v.Players {
		if v.Players[i].ID == rec.Seat {
			handSize = len(v.Players[i].Hand)
		}
	}
	mana := 0
	for _, m := range rec.Board.Mana {
		mana += int(m)
	}

	// option index -> teacher value (the leak): the first candidate covering
	// the option, mirroring the loader's first-wins rule.
	value := make([]float64, len(rec.Options))
	for i := range value {
		value[i] = -1
	}
	for _, c := range rec.Candidates {
		for _, j := range c.Choices {
			if j >= 0 && j < len(value) && value[j] < 0 {
				value[j] = c.Value
			}
		}
	}

	width := expFamilies[family]
	out := make([][]float32, len(rec.Options))
	for i, o := range rec.Options {
		ex := make([]float32, width)
		ss := 0
		if family == "oracle" || family == "board+oracle" {
			if value[i] >= 0 {
				ex[0] = float32(value[i]) * oracleScale
			}
			ex[1] = float32(rec.Margin)
			ss = 2
		}
		if family == "board" || family == "board+oracle" {
			bc, ok := boardCard[o.Obj]
			if ok {
				ex[ss+0] = b2f(bc.Castable)
				ex[ss+1] = b2f(bc.InstantSpeed)
				ex[ss+2] = b2f(bc.Counter)
				ex[ss+3] = float32(bc.ManaValue)
				ex[ss+4] = float32(bc.Power)
				ex[ss+5] = b2f(bc.OnBattlefield)
				ex[ss+6] = float32(bc.Activated)
				prod := int32(0)
				for _, p := range bc.Produces {
					prod += p
				}
				ex[ss+7] = float32(prod)
				ex[ss+8] = b2f(bc.ProducesAny)
				ex[ss+9] = b2f(bc.Basic)
			}
			ex[ss+10] = float32(myLife)
			ex[ss+11] = float32(oppLife)
			ex[ss+12] = float32(myPower)
			ex[ss+13] = float32(oppPower)
			ex[ss+14] = float32(handSize)
			ex[ss+15] = float32(mana)
		}
		out[i] = ex
	}
	return out
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// oracleScale scales the leaky oracle value input; 1 is the raw value. A large
// scale makes the value dominate the residual prior, isolating "can the model
// represent a linear read of the feature" from "does the feature carry signal".
var oracleScale = func() float32 {
	v, _ := strconv.ParseFloat(envOr("POLICYNET_EXP_ORACLE_SCALE", "1"), 64)
	return float32(v)
}()

// firstExtraLen reports the Extra width the first option that has one carries,
// so the harness can prove the augmentation reached the Examples.
func firstExtraLen(exs []policynet.Example) int {
	for i := range exs {
		for j := range exs[i].Options {
			if n := len(exs[i].Options[j].Extra); n > 0 {
				return n
			}
		}
	}
	return 0
}

// LeakStat is one kind's teacher-value recoverability. Naive is the
// first-wins-argmax agreement; Strict is the fraction where every preferred
// option beats every non-preferred option outright (so no tie-break is
// needed); Weak is the fraction where the best preferred value reaches the
// best non-preferred value (recoverable with the right tie-break -- the true
// ceiling of the value signal).
type LeakStat struct {
	Kind   decision.Kind
	N      int
	Naive  float64
	Strict float64
	Weak   float64
}

// probeByKind computes the leak ceiling per decision kind, in sorted kind
// order, over every example with a preferred option.
func probeByKind(exs []policynet.Example) []LeakStat {
	type acc struct{ n, naive, strict, weak int }
	by := map[decision.Kind]*acc{}
	for i := range exs {
		hasPref := false
		for j := range exs[i].Options {
			if exs[i].Options[j].Target.Preferred {
				hasPref = true
				break
			}
		}
		if !hasPref {
			continue
		}
		a := by[exs[i].Kind]
		if a == nil {
			a = &acc{}
			by[exs[i].Kind] = a
		}
		a.n++
		// Naive first-wins argmax.
		best := -1
		for j := range exs[i].Options {
			if len(exs[i].Options[j].Extra) == 0 {
				continue
			}
			if best < 0 || exs[i].Options[j].Extra[0] > exs[i].Options[best].Extra[0] {
				best = j
			}
		}
		if best >= 0 && exs[i].Options[best].Target.Preferred {
			a.naive++
		}
		pMax, oMax := -1e30, -1e30
		pMin, oMin := 1e30, 1e30
		for j := range exs[i].Options {
			if len(exs[i].Options[j].Extra) == 0 {
				continue
			}
			v := float64(exs[i].Options[j].Extra[0])
			if exs[i].Options[j].Target.Preferred {
				if v > pMax {
					pMax = v
				}
				if v < pMin {
					pMin = v
				}
			} else {
				if v > oMax {
					oMax = v
				}
				if v < oMin {
					oMin = v
				}
			}
		}
		if pMin > oMax {
			a.strict++
		}
		if pMax >= oMax {
			a.weak++
		}
	}
	kinds := make([]decision.Kind, 0, len(by))
	for k := range by {
		kinds = append(kinds, k)
	}
	sortKinds(kinds)
	out := make([]LeakStat, 0, len(kinds))
	for _, k := range kinds {
		a := by[k]
		out = append(out, LeakStat{Kind: k, N: a.n,
			Naive: ratio(a.naive, a.n), Strict: ratio(a.strict, a.n), Weak: ratio(a.weak, a.n)})
	}
	return out
}

func sortKinds(ks []decision.Kind) {
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
}

// oracleHoldoutByKind applies the oracle+bot-tiebreak policy to the SAME
// holdout split Train uses (splitCorpus with the run's seed), so its numbers
// are directly comparable to Train's ByKind readout. This is the ceiling the
// search signal could reach if the model could read it perfectly.
func oracleHoldoutByKind(exs []policynet.Example, seed int64) []TieStat {
	usable := make([]policynet.Example, 0, len(exs))
	for i := range exs {
		labelled := false
		for j := range exs[i].Options {
			if exs[i].Options[j].Target.Labelled {
				labelled = true
				break
			}
		}
		if labelled {
			usable = append(usable, exs[i])
		}
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x9E3779B97F4A7C15^uint64(seed)))
	sp := splitCorpus(usable, 0.1, rng)
	return oracleBotTiebreakByKind(indicesToExamples(usable, sp.hold))
}

func indicesToExamples(exs []policynet.Example, idx []int) []policynet.Example {
	out := make([]policynet.Example, 0, len(idx))
	for _, i := range idx {
		out = append(out, exs[i])
	}
	return out
}

// expSplit reproduces Train's usable-filter and splitCorpus with the run's
// seed, so a caller can evaluate a trained model on the exact train/holdout
// partitions Train used.
func expSplit(exs []policynet.Example, seed int64) ([]policynet.Example, split) {
	usable := make([]policynet.Example, 0, len(exs))
	for i := range exs {
		labelled := false
		for j := range exs[i].Options {
			if exs[i].Options[j].Target.Labelled {
				labelled = true
				break
			}
		}
		if labelled {
			usable = append(usable, exs[i])
		}
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x9E3779B97F4A7C15^uint64(seed)))
	return usable, splitCorpus(usable, 0.1, rng)
}

// TieStat is one kind's best-attainable search policy. ValueTop1 is the
// argmax of the teacher's own per-option value (Extra[0]); ValueBotTie adds
// the bot prior as a TIE-BREAK ONLY (prefer the bot's own pick among options
// whose value ties the max) -- the strongest thing the search alone can say.
// BotTop1 is the bot's own agreement. If ValueBotTie does not beat BotTop1,
// the search value carries no override signal the bot lacks.
type TieStat struct {
	Kind        decision.Kind
	N           int
	ValueTop1   float64
	ValueBotTie float64
	BotTop1     float64
}

func oracleBotTiebreakByKind(exs []policynet.Example) []TieStat {
	type acc struct{ n, vt, vbt, bot int }
	by := map[decision.Kind]*acc{}
	for i := range exs {
		hasPref := false
		for j := range exs[i].Options {
			if exs[i].Options[j].Target.Preferred {
				hasPref = true
				break
			}
		}
		if !hasPref {
			continue
		}
		a := by[exs[i].Kind]
		if a == nil {
			a = &acc{}
			by[exs[i].Kind] = a
		}
		a.n++
		best, bestBot := -1, -1
		for j := range exs[i].Options {
			if !exs[i].Options[j].Target.Labelled {
				continue
			}
			if best < 0 || exs[i].Options[j].Target.Value > exs[i].Options[best].Target.Value {
				best = j
			}
			// bot-tie-break: same value, prefer the bot's own pick.
			if bestBot < 0 || exs[i].Options[j].Target.Value > exs[i].Options[bestBot].Target.Value {
				bestBot = j
			} else if exs[i].Options[j].Target.Value == exs[i].Options[bestBot].Target.Value && exs[i].Options[j].BotPick && !exs[i].Options[bestBot].BotPick {
				bestBot = j
			}
		}
		if best >= 0 && exs[i].Options[best].Target.Preferred {
			a.vt++
		}
		if bestBot >= 0 && exs[i].Options[bestBot].Target.Preferred {
			a.vbt++
		}
		if exs[i].TeacherChoice == exs[i].BotIndex {
			a.bot++
		}
	}
	kinds := make([]decision.Kind, 0, len(by))
	for k := range by {
		kinds = append(kinds, k)
	}
	sortKinds(kinds)
	out := make([]TieStat, 0, len(kinds))
	for _, k := range kinds {
		a := by[k]
		out = append(out, TieStat{Kind: k, N: a.n, ValueTop1: ratio(a.vt, a.n), ValueBotTie: ratio(a.vbt, a.n), BotTop1: ratio(a.bot, a.n)})
	}
	return out
}

// decodeExpRecords streams the raw label records, mirroring Load's skip rule
// (non-empty candidates, matching record type/schema), its gzip detection
// (a 1f 8b magic peeks the same two bytes Load does) and its missing-view
// hard error, so the returned slice aligns index-for-index with Load's
// Examples over ANY corpus Load accepts.
func decodeExpRecords(path string) ([]expRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	var stream io.Reader = br
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("reading gzipped label corpus: %w", err)
		}
		defer zr.Close()
		stream = zr
	}
	sc := bufio.NewScanner(stream)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var out []expRecord
	seen := 0 // every non-blank line, matching Load's per-record counting
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		seen++
		var r expRecord
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("record %d: %w", seen, err)
		}
		if r.RecordType != policynet.LabelRecordType || !policynet.LabelSchemaAccepted(r.SchemaVersion) {
			continue
		}
		if len(r.Candidates) == 0 {
			continue
		}
		if len(r.View) == 0 || bytes.Equal(bytes.TrimSpace(r.View), []byte("null")) {
			return nil, fmt.Errorf("record %d: missing view", seen)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
