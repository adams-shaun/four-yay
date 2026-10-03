package compliance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const fixtureSet = `package mage.sets;

public final class Fixture extends ExpansionSet {
    private Fixture() {
        super("Fixture \"Set\"", "FXT", ExpansionSet.buildDate(2026, 10, 2), SetType.EXPANSION);
        this.blockName = "Fixture";
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));
        cards.add(new SetCardInfo("Aerid Konstrari", 121, Rarity.MYTHIC, mage.cards.a.AeridKonstrari.class, NON_FULL_USE_VARIOUS));
        cards.add(new SetCardInfo("Aerid Konstrari", "347a", Rarity.MYTHIC, mage.cards.a.AeridKonstrari.class, NON_FULL_USE_VARIOUS));
        cards.add(new SetCardInfo("Aerid Konstrari", 121, Rarity.MYTHIC, mage.cards.a.AeridKonstrari.class, NON_FULL_USE_VARIOUS));
        cards.add(new SetCardInfo("Fire // Ice", 9, Rarity.UNCOMMON, mage.cards.f.FireIce.class));
    }
}
`

func TestParseSetClass(t *testing.T) {
	m, err := ParseSetClass([]byte(fixtureSet), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	want := Manifest{
		Code: "FXT", Name: `Fixture "Set"`, Released: "2026-10-02", SetType: "EXPANSION", XMageRef: "abc123",
		Cards: []ManifestCard{
			{Name: "Aerid Konstrari", Numbers: []string{"121", "347a"}, Rarity: "MYTHIC"},
			{Name: "Fire // Ice", Numbers: []string{"9"}, Rarity: "UNCOMMON"},
			{Name: "Zap", Numbers: []string{"2"}, Rarity: "COMMON"},
		},
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("got  %+v\nwant %+v", m, want)
	}
}

func TestParseSetClassOldHeader(t *testing.T) {
	src := `super("Limited Edition Alpha", "LEA", buildDate(1993, 8, 5), SetType.CORE);
        cards.add(new SetCardInfo("Black Lotus", 232, Rarity.RARE, mage.cards.b.BlackLotus.class));`
	m, err := ParseSetClass([]byte(src), "r")
	if err != nil {
		t.Fatal(err)
	}
	if m.Code != "LEA" || m.Released != "1993-08-05" || m.SetType != "CORE" || len(m.Cards) != 1 {
		t.Fatalf("got %+v", m)
	}
}

func TestParseSetClassUnescapesNames(t *testing.T) {
	src := `super("Unhinged", "UNH", ExpansionSet.buildDate(2004, 11, 19), SetType.JOKE_SET);
        cards.add(new SetCardInfo("\"Ach! Hans, Run!\"", 116, Rarity.RARE, mage.cards.a.AchHansRun.class));
        cards.add(new SetCardInfo("Déjà Vu", 26, Rarity.COMMON, mage.cards.d.DejaVu.class));`
	m, err := ParseSetClass([]byte(src), "r")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{m.Cards[0].Name, m.Cards[1].Name}
	want := []string{`"Ach! Hans, Run!"`, "Déjà Vu"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseSetClassRefusesUnparsedEntry(t *testing.T) {
	src := `super("Fixture", "FXT", ExpansionSet.buildDate(2026, 1, 1), SetType.EXPANSION);
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));
        cards.add(new SetCardInfo("Bolt", NUMBER, Rarity.COMMON, mage.cards.b.Bolt.class));`
	_, err := ParseSetClass([]byte(src), "r")
	if err == nil || !strings.Contains(err.Error(), "2 SetCardInfo entries but 1 parsed") {
		t.Fatalf("err = %v, want a count mismatch", err)
	}
}

func TestParseSetClassRefusesMissingHeader(t *testing.T) {
	_, err := ParseSetClass([]byte(`public final class X {}`), "r")
	if err == nil || !strings.Contains(err.Error(), "no ExpansionSet super(...) header") {
		t.Fatalf("err = %v", err)
	}
}

func TestMarshalLinesRoundTripsAndIsOneCardPerLine(t *testing.T) {
	m, err := ParseSetClass([]byte(fixtureSet), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	out := m.MarshalLines()
	var back Manifest
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !reflect.DeepEqual(back, m) {
		t.Fatalf("round trip: got %+v want %+v", back, m)
	}
	if n := strings.Count(string(out), `{"name":`); n != len(m.Cards) {
		t.Fatalf("%d card objects, want %d", n, len(m.Cards))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Count(line, `{"name":`) > 1 {
			t.Fatalf("two cards on one line: %s", line)
		}
	}
	if string(m.MarshalLines()) != string(out) {
		t.Fatal("MarshalLines is not deterministic")
	}
}
