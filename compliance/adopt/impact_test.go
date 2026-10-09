package adopt

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
)

// synthetic builds a census by hand: two formats, three sets, five cards.
func synthetic() *Census {
	cfg := &Config{Formats: []Format{{Name: "Std", Sets: []string{"NEW"}}, {Name: "All", Tournament: true}}}
	cs := &Census{Config: cfg, byName: map[string]*Card{}, setCards: map[string][]*Card{},
		SetFormats: map[string][]string{"NEW": {"Std", "All"}, "OLD": {"All"}, "UNH": nil}}
	for _, c := range []*Card{
		{Name: "a", Sets: []string{"NEW"}, Missing: []string{"kw:X"}},
		{Name: "b", Sets: []string{"NEW"}, Missing: []string{"kw:X", "kw:Y"}},
		{Name: "c", Sets: []string{"OLD"}, Missing: []string{"kw:Y"}},
		{Name: "d", Sets: []string{"OLD"}, Missing: []string{"kw:Y"}},
		{Name: "e", Sets: []string{"UNH"}, Missing: []string{"kw:Joke"}, NonTournament: "printed only in non-tournament sets UNH"},
	} {
		for _, s := range c.Sets {
			for _, f := range cs.SetFormats[s] {
				if !contains(c.Formats, f) {
					c.Formats = append(c.Formats, f)
				}
			}
			cs.setCards[s] = append(cs.setCards[s], c)
		}
		cs.byName[c.Name] = c
		cs.Cards = append(cs.Cards, c)
	}
	for s := range cs.SetFormats {
		cs.Sets = mapSet(cs.Sets, s)
	}
	return cs
}

func TestImpactRanksByTargetFormatOrder(t *testing.T) {
	rows := synthetic().Impact()
	var got []string
	for _, r := range rows {
		got = append(got, r.Primitive)
	}
	// kw:X blocks 2 Std cards, kw:Y 1 Std card but 3 All cards: the first
	// target decides. The joke primitive is outside every target.
	if want := []string{"kw:X", "kw:Y", "kw:Joke"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rank %v, want %v", got, want)
	}
	x, y, j := rows[0], rows[1], rows[2]
	if !reflect.DeepEqual(x.ByFormat, []int{2, 2}) || !reflect.DeepEqual(x.SoleByFormat, []int{1, 1}) {
		t.Errorf("kw:X by format %v sole %v", x.ByFormat, x.SoleByFormat)
	}
	if !reflect.DeepEqual(y.ByFormat, []int{1, 3}) || !reflect.DeepEqual(y.Sole, []string{"c", "d"}) {
		t.Errorf("kw:Y by format %v sole %v", y.ByFormat, y.Sole)
	}
	if !j.NonTournament || len(j.Cards) != 0 || !reflect.DeepEqual(j.NonTournamentCards, []string{"e"}) {
		t.Errorf("kw:Joke %+v", j)
	}
	// NEW misses X and Y, OLD only Y; UNH's only card is
	// non-tournament, so nothing is unlocked there.
	if !reflect.DeepEqual(y.SetsUnlocked, []string{"OLD"}) || len(x.SetsUnlocked) != 0 || len(j.SetsUnlocked) != 0 {
		t.Errorf("unlocked: X %v Y %v Joke %v", x.SetsUnlocked, y.SetsUnlocked, j.SetsUnlocked)
	}
	if !reflect.DeepEqual(x.Sets, []string{"NEW"}) {
		t.Errorf("kw:X sets %v", x.Sets)
	}
	var b strings.Builder
	synthetic().WriteImpact(&b, rows, 0)
	if !strings.Contains(b.String(), "| 1 | `kw:X` | 2 (1) | 2 (1) | 0  | a, b |") ||
		!strings.Contains(b.String(), "1 non-tournament primitives excluded") {
		t.Errorf("table:\n%s", b.String())
	}
}

// TestCorpusImpact holds the real table's invariants at this head: every
// tournament row blocks a tournament card, its sole blockers are among its
// cards, a set it unlocks really misses nothing else, and the
// non-tournament primitives sort after every tournament one.
func TestCorpusImpact(t *testing.T) {
	t.Parallel()
	cs := census(t)
	rows := cs.Impact()
	seenNT := false
	byName := map[string]Impact{}
	for _, r := range rows {
		byName[r.Primitive] = r
		if r.NonTournament {
			seenNT = true
			continue
		}
		if seenNT {
			t.Errorf("%s: tournament row after a non-tournament one", r.Primitive)
		}
		if len(r.Cards) == 0 {
			t.Errorf("%s: tournament row with no cards", r.Primitive)
		}
		cards := map[string]bool{}
		for _, c := range r.Cards {
			cards[c] = true
		}
		for _, c := range r.Sole {
			if !cards[c] {
				t.Errorf("%s: sole %s not among its cards", r.Primitive, c)
			}
		}
		for _, s := range r.SetsUnlocked {
			for _, c := range cs.SetCards(s) {
				if c.NonTournament != "" {
					continue
				}
				for _, p := range c.Missing {
					if p != r.Primitive {
						t.Errorf("%s unlocks %s, but %s still misses %s", r.Primitive, s, c.Name, p)
					}
				}
			}
		}
	}
	for _, p := range cs.Config.NonTournamentPrimitives {
		if r, ok := byName[p]; ok && !r.NonTournament {
			t.Errorf("%s is declared non-tournament but ranked as a target", p)
		}
	}
	n := 0
	for _, r := range rows {
		if !r.NonTournament && r.ByFormat[len(r.ByFormat)-1] >= 10 {
			n++
		}
	}
	t.Logf("impact: %d primitives, %d tournament primitives block 10+ tournament cards; top %s", len(rows), n, rows[0].Primitive)
}

func mapSet(m map[string]compliance.Manifest, code string) map[string]compliance.Manifest {
	if m == nil {
		m = map[string]compliance.Manifest{}
	}
	m[code] = compliance.Manifest{Code: code}
	return m
}
