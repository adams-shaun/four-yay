package mzbridge

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// The fixture cards are invented for these tests: no card in the corpus has
// these names or texts.
const (
	srcWood = "Name:Testwood\nManaCost:no cost\nTypes:Basic Land Forest\nOracle:({T}: Add {G}.)\n"
	srcPeak = "Name:Testpeak\nManaCost:no cost\nTypes:Basic Land Mountain\nOracle:({T}: Add {R}.)\n"
	srcBear = "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:\n"
	srcHawk = "Name:Test Hawk\nManaCost:1 W\nTypes:Creature Bird\nPT:1/1\nK:Flying\nOracle:Flying\n"
	srcZap  = "Name:Test Zap\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 2 | SpellDescription$ CARDNAME deals 2 damage to any target.\n" +
		"Oracle:Test Zap deals 2 damage to any target.\n"
	srcCat = "Name:Test Cat Token\nManaCost:no cost\nColors:white\nTypes:Creature Cat\nPT:1/1\nOracle:\n"
)

type fixtureCards struct {
	wood, peak, bear, hawk, zap *cards.Card
	tokens                      map[string]*cards.Card
}

func fixtureCard(t testing.TB, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("fixture.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("fixture card: %v", diags)
	}
	if diags := c.Link(); len(diags) != 0 {
		t.Fatalf("fixture card link: %v", diags)
	}
	for _, f := range c.Faces {
		f.ApplyIntrinsics() // what the registry does on load
	}
	return c
}

func fixtures(t testing.TB) fixtureCards {
	t.Helper()
	return fixtureCards{
		wood: fixtureCard(t, srcWood), peak: fixtureCard(t, srcPeak), bear: fixtureCard(t, srcBear),
		hawk: fixtureCard(t, srcHawk), zap: fixtureCard(t, srcZap),
		tokens: map[string]*cards.Card{"t_cat": fixtureCard(t, srcCat)},
	}
}

func rep(c *cards.Card, n int) []*cards.Card {
	out := make([]*cards.Card, n)
	for i := range out {
		out[i] = c
	}
	return out
}

// stage builds a staged engine and advances it to its first decision.
func stage(t testing.TB, fx fixtureCards, st rules.Stage) (*rules.Engine, rules.StagedObjects, *decision.Decision) {
	t.Helper()
	e, ids, err := rules.NewStaged(rules.Config{Seed: 5, Names: []string{"a", "b"}, Tokens: fx.tokens}, st)
	if err != nil {
		t.Fatal(err)
	}
	if e.Pending() == nil {
		e.Advance()
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("staged engine has no decision")
	}
	return e, ids, d
}

// pick submits the option the predicate selects and returns the next
// decision.
func pick(t testing.TB, e *rules.Engine, want func(o *decision.Option) bool) *decision.Decision {
	t.Helper()
	d := e.Pending()
	for i := range d.Options {
		if want(&d.Options[i]) {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[i].Index}}); err != nil {
				t.Fatal(err)
			}
			if e.Pending() == nil {
				e.Advance()
			}
			return e.Pending()
		}
	}
	t.Fatalf("no such option in %s decision: %+v", d.Kind, d.Options)
	return nil
}

// names is the state's feature-name set: every "path" of the debug dump,
// sorted.
func names(t testing.TB, e *rules.Engine, viewer state.PlayerID, d *decision.Decision, opts EncodeOptions) []string {
	t.Helper()
	lines, err := DescribeState(e, viewer, d, Priority, "priority", opts)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		_, path, _ := strings.Cut(l, "\t")
		out = append(out, path)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func ids(t testing.TB, e *rules.Engine, viewer state.PlayerID, d *decision.Decision, opts EncodeOptions) []int32 {
	t.Helper()
	fs := NewFeatureSet()
	if err := Encode(e, viewer, d, Priority, "priority", fs, opts); err != nil {
		t.Fatal(err)
	}
	return fs.IDs()
}

// compactNames folds every thermometer run "name@0#k .. name@(n-1)#k" of a
// node into one line "name@0..(n-1)#k", so a golden list stays readable.
// Breakpoint features (@32, @64, ...) that are not part of a run from 0 stay
// as they are.
func compactNames(paths []string) []string {
	type run struct{ seen map[int]bool }
	runs := map[string]*run{}
	key := func(p string) (string, int, bool) {
		at := strings.LastIndexByte(p, '@')
		hash := strings.LastIndexByte(p, '#')
		if at < 0 || hash < at {
			return "", 0, false
		}
		n, err := strconv.Atoi(p[at+1 : hash])
		if err != nil {
			return "", 0, false
		}
		return p[:at] + "\x00" + p[hash:], n, true
	}
	for _, p := range paths {
		if k, n, ok := key(p); ok {
			if runs[k] == nil {
				runs[k] = &run{seen: map[int]bool{}}
			}
			runs[k].seen[n] = true
		}
	}
	var out []string
	for _, p := range paths {
		k, n, ok := key(p)
		if !ok {
			out = append(out, p)
			continue
		}
		top := 0
		for runs[k].seen[top] {
			top++
		}
		switch {
		case n >= top || top == 1:
			out = append(out, p)
		case n == 0:
			pre, post, _ := strings.Cut(k, "\x00")
			out = append(out, pre+"@0.."+strconv.Itoa(top-1)+post)
		}
	}
	slices.Sort(out)
	return out
}

// golden compares a state's compacted feature-name set with
// testdata/encode/<name>.txt. MZBRIDGE_UPDATE_GOLDEN=1 rewrites the file;
// a rewritten file is a claim about StateEncoder.java and is reviewed line
// by line against it before it is committed.
func golden(t *testing.T, name string, got []string) {
	t.Helper()
	got = compactNames(got)
	path := filepath.Join("testdata", "encode", name+".txt")
	if os.Getenv("MZBRIDGE_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, g := range got {
		if !slices.Contains(want, g) {
			t.Errorf("unexpected feature %q", g)
		}
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing feature %q", w)
		}
	}
}
