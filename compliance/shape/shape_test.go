package shape

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
)

func diverge(card, tmpl, cp, field, g, x string) compliance.VerdictRow {
	return compliance.VerdictRow{Card: card, Template: tmpl, Status: compliance.StatusDiverge,
		Detail: cp + " " + field + ": gorge " + strconv.Quote(g) + ", xmage " + strconv.Quote(x)}
}

func TestOfNormalisesByRole(t *testing.T) {
	cases := []struct {
		row  compliance.VerdictRow
		want Shape
	}{
		// Library filler only, whichever zone it surfaced in: one shape.
		{diverge("Diresight", "cast-resolve", "step 1 (resolve)", "p0.graveyard", "[Diresight]", "[Wastes, Wastes, Diresight]"),
			Shape{"cast-resolve", "resolve", "p0.library", "placement"}},
		{diverge("Stock Up", "cast-resolve", "step 1 (resolve)", "p0.hand", "[Wastes, Wastes]", "[Wastes]"),
			Shape{"cast-resolve", "resolve", "p0.library", "placement"}},
		{diverge("Erode", "cast-resolve", "step 1 (resolve)", "p1.library_count", "39", "38"),
			Shape{"cast-resolve", "resolve", "p1.library", "placement"}},
		// A fixture in the diff stays literal; counts drop.
		{diverge("Cut In", "cast-resolve", "step 1 (resolve)", "p0.graveyard", "[]", "[Grizzly Bears]"),
			Shape{"cast-resolve", "resolve", "p0.graveyard", "gorge+[] xmage+[Grizzly Bears]"}},
		// Permanents pair by name role and name the differing part.
		{diverge("Artist's Talent", "cast-resolve", "step 1 (resolve)", "permanents",
			"c0 o0 Artist's Talent [class enchantment] {R} counters=LEVEL=1", "c0 o0 Artist's Talent [class enchantment] {R}"),
			Shape{"cast-resolve", "resolve", "permanents", "$CARD gorge+[counters=LEVEL=1] xmage+[]"}},
		{diverge("Room of Refuge", "play-land", "step 0 (play)", "permanents",
			"c0 o0 Room of Refuge [cave land] {} tapped", "c0 o0 Room of Refuge [land] {} tapped"),
			Shape{"play-land", "play", "permanents", "$CARD types gorge+[cave] xmage+[]"}},
		{diverge("Honest Work", "cast-resolve", "step 1 (resolve)", "permanents",
			"c0 o0 Honest Work [aura enchantment] {U} on=Grizzly Bears; c1 o1 Grizzly Bears [citizen creature] {G} 1/1 tapped",
			"c0 o0 Honest Work [aura enchantment] {U} on=Humble Merchant"),
			Shape{"cast-resolve", "resolve", "permanents", "$CARD gorge+[on=Grizzly Bears] xmage+[on=Humble Merchant]; gorge+Grizzly Bears"}},
		{diverge("Blooming Blast", "cast-resolve", "step 1 (resolve)", "p1.life", "18", "16"),
			Shape{"cast-resolve", "resolve", "p1.life", "gorge-xmage=2"}},
		// A driver message loses the card and the target list it echoes.
		{compliance.VerdictRow{Card: "Bushy Bodyguard", Template: "cast-resolve", Status: compliance.StatusHarness,
			Detail: "xmage: AssertionError: Can't find ability to activate command: Cast Bushy Bodyguard$target=Grizzly Bears^Grizzly Bears"},
			Shape{"cast-resolve", "harness", "xmage", "AssertionError: Can't find ability to activate command: Cast $CARD"}},
	}
	for _, c := range cases {
		got, ok := Of(c.row)
		if !ok || got != c.want {
			t.Errorf("%s: got %+v (%v), want %+v", c.row.Card, got, ok, c.want)
		}
	}
	if _, ok := Of(compliance.VerdictRow{Status: compliance.StatusAgree}); ok {
		t.Error("an agree row has no shape")
	}
}

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		p, s string
		want bool
	}{
		{"", "anything", true}, {"a[b]", "a[b]", true}, {"a[b]", "a[c]", false},
		{"$CARD gorge+[counters=LEVEL=*] xmage+[]", "$CARD gorge+[counters=LEVEL=2] xmage+[]", true},
		{"*Surveil*", "Surveil+Draw", true}, {"*Surveil", "Surveil+Draw", false}, {"a*b*c", "abc", true}, {"a*b*c", "acb", false},
	} {
		if got := Glob(c.p, c.s); got != c.want {
			t.Errorf("Glob(%q, %q) = %v", c.p, c.s, got)
		}
	}
}

func TestClassifyAppliesAndReverts(t *testing.T) {
	rs := []Ruling{{ID: "cave", Status: compliance.StatusXMageWrong, Ruling: "XMage omits Cave", CR: []string{"205.3i"},
		Match: Match{Field: "permanents", Diff: "$CARD types gorge+[cave] xmage+[]"}}}
	r := diverge("Room of Refuge", "play-land", "step 0 (play)", "permanents",
		"c0 o0 Room of Refuge [cave land] {} tapped", "c0 o0 Room of Refuge [land] {} tapped")
	m, _, changed := Classify(&r, rs, "-")
	if m == nil || !changed || r.Status != compliance.StatusXMageWrong || r.RulingID != "cave" || r.Ruling != "XMage omits Cave (CR 205.3i)" {
		t.Fatalf("not applied: %+v", r)
	}
	if want := Sampled(r.Card, r.Template, "cave"); (r.Review == ReviewPending) != want {
		t.Errorf("review %q, sampled %v", r.Review, want)
	}
	// Narrowed to another API: no match, and the automatic ruling reverts.
	rs[0].Match.API = []string{"Surveil*"}
	r.Frozen = []compliance.Frozen{{Field: "checkpoints", Value: "x"}}
	if m, _, changed := Classify(&r, rs, "-"); m != nil || !changed || r.Status != compliance.StatusDiverge || r.RulingID != "" || r.HasExpectation() {
		t.Fatalf("not reverted: %+v", r)
	}
	// A hand ruling is never touched.
	hand := r
	hand.Status, hand.Ruling = compliance.StatusXMageWrong, "by hand"
	rs[0].Match.API = nil
	if _, _, changed := Classify(&hand, rs, "-"); changed {
		t.Fatalf("hand ruling changed: %+v", hand)
	}
}

func TestSampledIsAboutOneInTen(t *testing.T) {
	n := 0
	for i := 0; i < 1000; i++ {
		if Sampled("card"+strconv.Itoa(i), "cast-resolve", "r") {
			n++
		}
	}
	if n < 60 || n > 140 {
		t.Fatalf("%d of 1000 sampled", n)
	}
}

func TestLoadRulingsValidates(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.json", `{"id":"a","status":"harness","match":{"field":"x"},"ruling":"r"}`)
	if rs, err := LoadRulings(dir); err != nil || len(rs) != 1 {
		t.Fatalf("%v %v", rs, err)
	}
	for name, body := range map[string]string{
		"b.json": `{"id":"other","status":"harness","match":{"field":"x"},"ruling":"r"}`,
		"c.json": `{"id":"c","status":"diverge","match":{"field":"x"},"ruling":"r"}`,
		"d.json": `{"id":"d","status":"harness","match":{},"ruling":"r"}`,
		"e.json": `{"id":"e","status":"harness","match":{"field":"x"},"ruling":"r","typo":1}`,
	} {
		write(name, body)
		if _, err := LoadRulings(dir); err == nil {
			t.Errorf("%s loaded: %s", name, body)
		}
		os.Remove(filepath.Join(dir, name))
	}
}

// TestCommittedRulingsLoad holds compliance/rulings well formed.
func TestCommittedRulingsLoad(t *testing.T) {
	rs, err := LoadRulings(filepath.Join("..", "..", RulingDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d committed shape rulings", len(rs))
}
