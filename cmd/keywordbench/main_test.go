package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

func TestPresenceOffersAndEventTimeAttribution(t *testing.T) {
	// Synthetic IR, not a corpus script. The same source has unrelated
	// abilities and changes face; neither fact may inflate keyword use.
	c := &cards.Card{Faces: []*cards.Face{
		{Name: "Front", Keywords: []string{"Prowess"}, Abilities: []*cards.SA{
			{API: "Draw"}, {API: "Regenerate"},
		}, Triggers: []cards.Trigger{{Params: map[string]string{"Keyword": "Prowess"}, Effect: &cards.SA{API: "Pump", Params: map[string]string{"Keyword": "Prowess"}}}}},
		{Name: "Back", Keywords: []string{"Equip:1"}, Abilities: []*cards.SA{{API: "Attach", Params: map[string]string{"Keyword": "Equip"}}}},
	}}
	e := rules.New(rules.Config{Names: []string{"a", "b"}, Decks: [][]*cards.Card{{c, c, c, c, c, c, c, c}, {c, c, c, c, c, c, c, c}}})
	id := state.ObjID(1)
	got := tally{}
	got.presence([]*cards.Card{c, c})
	d := &decision.Decision{Options: []decision.Option{{Index: 0, Kind: "ability", Obj: id, Ability: 1}}}
	got.choice(e.G, d, decision.Intent{Choices: []int{0}})
	if n := got.at("api:Regenerate"); len(n.cards) != 1 || n.offered != 1 || n.selected != 1 || n.activated != 0 {
		t.Fatalf("presence/selection mistaken for use: %+v", n)
	}
	d.Options = append(d.Options, decision.Option{Index: 1, Kind: "pass"})
	got.choice(e.G, d, decision.Intent{Choices: []int{1}})
	if n := got.at("api:Regenerate"); n.offered != 2 || n.selected != 1 || n.noPrintedCreature != 1 || n.alternatives["pass"] != 1 || n.activated != 0 {
		t.Fatalf("unselected context confused with activation: %+v", n)
	}
	seen := map[string]bool{}
	got.consume(e.G, []events.Event{
		{Kind: events.MoveZone, Obj: id, To: state.ZBattlefield},
		{Kind: events.AbilityPush, Obj: id, Amount: 0}, // Draw is NOT regeneration or Prowess.
	}, seen)
	if got.at("api:Regenerate").activated != 0 || got.at("kw:Prowess").triggered != 0 {
		t.Fatal("attributed unrelated ability to source keyword")
	}
	got.consume(e.G, []events.Event{
		{Kind: events.AbilityPush, Obj: id, Amount: 1},
		{Kind: events.TriggerPush, Obj: id, Amount: 0},
		{Kind: events.MoveZone, Obj: id, To: state.ZGraveyard},
		{Kind: events.MoveZone, Obj: id, To: state.ZBattlefield},
		{Kind: events.FlipFace, Obj: id, Amount: 1},
		{Kind: events.AbilityPush, Obj: id, Amount: 0}, // Back index 0 IS Equip.
		{Kind: events.CastInfo, Obj: id, Counter: "kicked,flashback"},
	}, seen)
	if got.at("api:Regenerate").activated != 1 || got.at("kw:Equip").activated != 1 || got.at("kw:Prowess").triggered != 1 {
		t.Fatalf("wrong event-time attribution: regen=%+v equip=%+v prowess=%+v", got.at("api:Regenerate"), got.at("kw:Equip"), got.at("kw:Prowess"))
	}
	if got.at("kw:Prowess").battlefield != 1 || got.at("kw:Equip").battlefield != 1 {
		t.Fatal("battlefield counted reentry or wrong face")
	}
	if got.at("kw:Kicker").cast != 1 || got.at("kw:Flashback").cast != 1 {
		t.Fatal("lost cast flags")
	}
}

func TestMergeDistinctCardsAndSortedOutput(t *testing.T) {
	a, b := tally{}, tally{}
	a.at("kw:Prowess").cards["shared"] = true
	b.at("kw:Prowess").cards["shared"] = true
	b.at("kw:Prowess").triggered = 2
	b.at("kw:Equip").activated = 1
	merge(a, b)
	if len(a.at("kw:Prowess").cards) != 1 || a.at("kw:Prowess").triggered != 2 {
		t.Fatal("merge counts copies as distinct cards")
	}
	var out bytes.Buffer
	printTally(&out, "TOTAL", a)
	if !strings.HasPrefix(out.String(), "TOTAL\tkw:Equip\t") {
		t.Fatal("unsorted output")
	}
}

func TestPlayedLogIsReproducible(t *testing.T) {
	threat, _ := cards.ParseBytes("inline", []byte("Name:Apprentice\nManaCost:0\nTypes:Creature Human\nPT:1/2\nK:Prowess\n"))
	spell, _ := cards.ParseBytes("inline", []byte("Name:Practice\nManaCost:0\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1 | Defined$ You\n"))
	threat.Link()
	spell.Link()
	deck := []*cards.Card{}
	for i := 0; i < 15; i++ {
		deck = append(deck, threat, spell)
	}
	cfg := rules.Config{Seed: 110000, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}}
	outputs := []string{}
	for i := 0; i < 2; i++ {
		got := tally{}
		status, err := play(cfg, got)
		if err != nil || status != "replay_OK" {
			t.Fatalf("status=%s err=%v", status, err)
		}
		if got.at("kw:Prowess").triggered == 0 {
			t.Fatal("no actual keyword trigger in played log")
		}
		var out bytes.Buffer
		printTally(&out, "game", got)
		outputs = append(outputs, out.String())
	}
	if outputs[0] != outputs[1] {
		t.Fatal("same seed has different measurement")
	}
}

// writeBenchCorpus lays down a one-card cardsfolder fixture (the minimum
// CompileDir accepts) and optionally a deliberately-corrupt fingerprint-keyed
// cache so the cache load fails without needing a stale shared cache on disk.
func writeBenchCorpus(t *testing.T, withCache bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cardsfolder"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "Name:Test Card\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	if err := os.WriteFile(filepath.Join(dir, "cardsfolder", "test_card.txt"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if withCache {
		if err := os.WriteFile(cards.CachePath(dir), []byte("not a cache"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestLoadRegistryForBenchRecompilesInMemoryOnRequest covers the -recompile
// path: any cache load failure (here: unreadable file) falls back to an
// in-memory CompileDir over cardsfolder and NEVER writes the cache — the
// load's original error is reported, not the compile's.
func TestLoadRegistryForBenchRecompilesInMemoryOnRequest(t *testing.T) {
	dir := writeBenchCorpus(t, true)
	reg, recompiled, err := loadRegistryForBench(dir, true)
	if err != nil {
		t.Fatalf("loadRegistryForBench(recompile): %v", err)
	}
	if !recompiled {
		t.Fatal("recompiled = false, want true")
	}
	if _, ok := reg.Lookup("Test Card"); !ok {
		t.Fatal("in-memory rebuild lost the fixture card")
	}
	data, err := os.ReadFile(cards.CachePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "not a cache" {
		t.Fatal("recompile wrote under dir/ — shared-cache mutation hazard")
	}
}

// TestLoadRegistryForBenchDefaultFailsHard covers the default path: without
// -recompile an unusable cache is a hard error, nothing is recompiled, and
// the compile is never even attempted.
func TestLoadRegistryForBenchDefaultFailsHard(t *testing.T) {
	dir := writeBenchCorpus(t, true)
	reg, recompiled, err := loadRegistryForBench(dir, false)
	if err == nil || reg != nil || recompiled {
		t.Fatalf("default path err=%v reg=%v recompiled=%v, want hard error", err, reg, recompiled)
	}
}

// TestPrintSharedCacheAdvice pins the fingerprint-aware hint: a
// *cards.CacheVersionError gets the compiler-fingerprint explanation plus the
// -recompile remedy; any other error prints nothing.
func TestPrintSharedCacheAdvice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ir-abc.gob.gz")
	var buf bytes.Buffer
	printSharedCacheAdvice(&buf, &cards.CacheVersionError{Got: 3, Want: 4}, path)
	out := buf.String()
	for _, want := range []string{
		"IR cache version 3, want 4",
		path,
		"compiler fingerprint",
		"-recompile",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("advice missing %q:\n%s", want, out)
		}
	}
	buf.Reset()
	printSharedCacheAdvice(&buf, errors.New("no card scripts under /x"), path)
	if buf.Len() != 0 {
		t.Fatalf("non-version error printed advice: %s", buf.String())
	}
}
