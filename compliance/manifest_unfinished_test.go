package compliance

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The unfinished mechanism: the set class declares a name list and then
// removes those cards from the set, so they are in SetCardInfo but not in
// XMage's card database. This is SecretsOfStrixhaven's exact shape.
const unfinishedSet = `package mage.sets;

import java.util.Arrays;
import java.util.List;

public final class Fixture extends ExpansionSet {
    private static final List<String> unfinished = Arrays.asList("Decorum Dissertation", "Echocasting Symposium");
    private Fixture() {
        super("Fixture", "FXT", ExpansionSet.buildDate(2026, 10, 2), SetType.EXPANSION);
        cards.add(new SetCardInfo("Zap", 2, Rarity.COMMON, mage.cards.z.Zap.class));
        cards.add(new SetCardInfo("Decorum Dissertation", 3, Rarity.MYTHIC, mage.cards.d.DecorumDissertation.class));
        cards.add(new SetCardInfo("Echocasting Symposium", 4, Rarity.MYTHIC, mage.cards.e.EchocastingSymposium.class));
        cards.removeIf(setCardInfo -> unfinished.contains(setCardInfo.getName()));
    }
}
`

func TestParseSetClassReadsUnfinished(t *testing.T) {
	m, err := ParseSetClass([]byte(unfinishedSet), "r")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Decorum Dissertation", "Echocasting Symposium"}
	if !reflect.DeepEqual(m.Unfinished, want) {
		t.Fatalf("Unfinished = %v, want %v", m.Unfinished, want)
	}
	// The entries are still in the manifest's card list: SetCardInfo lists
	// them before the removal, and the printed claim needs them.
	if len(m.Cards) != 3 {
		t.Fatalf("cards = %d, want 3", len(m.Cards))
	}
}

// A lone `unfinished` list that nothing acts on is not a removal; reading it
// would wrongly move a playable card to the no-XMage bucket.
func TestParseSetClassIgnoresUnusedUnfinishedList(t *testing.T) {
	src := strings.Replace(unfinishedSet,
		"        cards.removeIf(setCardInfo -> unfinished.contains(setCardInfo.getName()));\n", "", 1)
	m, err := ParseSetClass([]byte(src), "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Unfinished) != 0 {
		t.Fatalf("Unfinished = %v, want none (the list is never applied)", m.Unfinished)
	}
}

func TestMarshalLinesRoundTripsUnfinished(t *testing.T) {
	m, err := ParseSetClass([]byte(unfinishedSet), "r")
	if err != nil {
		t.Fatal(err)
	}
	var back Manifest
	if err := json.Unmarshal(m.MarshalLines(), &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Unfinished, m.Unfinished) {
		t.Fatalf("round trip lost unfinished: got %v want %v", back.Unfinished, m.Unfinished)
	}
}

// A set without an unfinished list must not gain an `unfinished` key: the
// committed manifests outside SOS are byte-identical after the field was
// added, so a regeneration diff stays one file.
func TestMarshalLinesOmitsEmptyUnfinished(t *testing.T) {
	m, err := ParseSetClass([]byte(fixtureSet), "r")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(m.MarshalLines()), "unfinished") {
		t.Fatalf("empty unfinished is serialized:\n%s", m.MarshalLines())
	}
}
