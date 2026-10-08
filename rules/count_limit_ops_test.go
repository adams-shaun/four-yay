package rules

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// limitOpRe matches Forge's clamp suffixes (AbilityUtils.doXMath): LimitMax.N
// is min(value, N), LimitMin.N is max(value, N).
var limitOpRe = regexp.MustCompile(`/Limit(Max|Min)\.([A-Za-z0-9_]+)$`)

// TestCountLimitOpsClampCorpusWide is the class census for the /LimitMax. and
// /LimitMin. value suffixes: every corpus SVar body carrying one names an
// operand the evaluator can read (a literal, a printed SVar of the same face,
// or a name a StoreSVar on the face writes at run time -- Face to Face's
// Losses), and every such body whose head the evaluator resolves evaluates to
// the clamped value, not the bare head's. Before the fix the suffix was a
// silent no-op on every head but the two distinct-set Count$Valid properties
// (Colors, CreatureType), so 53 /LimitMax.1 presence bits read raw counts and
// the LimitMin floors read nothing at all.
func TestCountLimitOpsClampCorpusWide(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	bodies, evaluated := 0, 0
	var bad []string
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			names := make([]string, 0, len(f.SVars))
			for name := range f.SVars {
				names = append(names, name)
			}
			sort.Strings(names)
			var id = e.G.AddObject(c, 0).ID
			for _, name := range names {
				body := strings.TrimSpace(f.SVars[name])
				m := limitOpRe.FindStringSubmatch(body)
				if m == nil {
					continue
				}
				bodies++
				operand := m[2]
				ctx := &effects.Ctx{Source: id, Controller: 0, SVars: f.SVars}
				var bound int32
				if n, err := strconv.Atoi(operand); err == nil {
					bound = int32(n)
				} else if ob, ok := f.SVars[operand]; ok {
					bound = effects.EvalCount(e, ctx, ob)
				} else if faceStoresSVar(f.SVars, operand) {
					continue // a run-time-only name: Face to Face's own test drives it
				} else {
					bad = append(bad, f.Name+": "+name+" = "+body+" (operand "+operand+" names nothing)")
					continue
				}
				head := strings.TrimSuffix(body, m[0])
				raw, ok := effects.EvalCountOK(e, ctx, head)
				if !ok {
					continue // the head itself is not evaluable off-board
				}
				want := raw
				if m[1] == "Max" && want > bound {
					want = bound
				}
				if m[1] == "Min" && want < bound {
					want = bound
				}
				got, ok := effects.EvalCountOK(e, ctx, body)
				evaluated++
				if !ok || got != want {
					bad = append(bad, f.Name+": "+name+" = "+body+": got "+strconv.Itoa(int(got))+
						" ok="+strconv.FormatBool(ok)+", want "+strconv.Itoa(int(want)))
				}
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if bodies < 90 || evaluated == 0 {
		t.Fatalf("census measured %d Limit bodies (%d evaluated): the corpus scan is not reading them", bodies, evaluated)
	}
	t.Logf("%d /LimitMax./LimitMin. SVar bodies, %d evaluated against the clamp", bodies, evaluated)
}

// faceStoresSVar reports whether a StoreSVar sub-ability line on the face
// writes name (Face to Face's ResetLoss/ScoreLoss write Losses).
func faceStoresSVar(svars map[string]string, name string) bool {
	needle := "SVar$ " + name + " "
	for _, b := range svars {
		if strings.Contains(b, "StoreSVar") && strings.Contains(b+" ", needle) {
			return true
		}
	}
	return false
}

// TestCountLimitOpsBindBothWays pins the clamp arithmetic where the corpus
// census above cannot make it bind off-board: literal and SVar-named operands
// on both ops, under the SVar$, Number$ and Count$ heads, including a
// negative value floored at 0 (Equipoise's SVar$ExcessLand/LimitMin.0).
func TestCountLimitOpsBindBothWays(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	svars := map[string]string{"Three": "Number$3", "Five": "Number$5", "Neg": "Number$-2"}
	ctx := &effects.Ctx{Controller: 0, SVars: svars}
	for _, tc := range []struct {
		expr string
		want int32
	}{
		{"SVar$Five/LimitMax.Three", 3},
		{"SVar$Three/LimitMax.Five", 3},
		{"SVar$Three/LimitMin.Five", 5},
		{"SVar$Five/LimitMin.Three", 5},
		{"SVar$Neg/LimitMin.0", 0},
		{"Number$7/LimitMax.2", 2},
		{"Number$1/LimitMin.2", 2},
		{"Count$YourLifeTotal/LimitMax.11", 11},
		{"Count$YourLifeTotal/LimitMin.30", 30},
		{"Count$YourLifeTotal/LimitMax.Five", 5},
	} {
		got, ok := effects.EvalCountOK(e, ctx, tc.expr)
		if !ok || got != tc.want {
			t.Errorf("%s = %d (ok=%v), want %d", tc.expr, got, ok, tc.want)
		}
	}
}
