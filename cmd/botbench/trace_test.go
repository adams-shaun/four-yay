package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

func TestTraceBoardProjectionIsSortedAndRedacted(t *testing.T) {
	dmg := 3
	b := botpolicy.NewBoard(2)
	b.IsMain = true
	b.Pool = state.Mana{1, 2, 3, 4, 5, 6}
	b.Cards.Set(9, botpolicy.Card{Creature: true, Power: 4, CMC: 3, Basic: false, ManaCost: "2 R", Castable: true, Produces: cards.ManaProduction{Colour: [6]int32{0, 0, 0, 1, 0, 0}}})
	b.Cards.Set(2, botpolicy.Card{Basic: true, OnBattlefield: true})
	b.Life.Set(1, 17)
	b.Life.Set(0, 20)
	b.Creatures.Set(8, botpolicy.Creature{Power: 3, Toughness: 2, Damage: 1, Keywords: []string{"Haste"}, Tapped: true, Controller: 1})
	b.Creatures.Set(3, botpolicy.Creature{Power: 1, Toughness: 1, Controller: 0})
	b.Commanders.Set(8, botpolicy.Commander{Casts: 2, InCommandZone: false, Damage: map[state.PlayerID]int32{1: 7, 0: 2}})
	b.Stack = []botpolicy.StackEntry{{ID: 11, Controller: 1, IsSpell: true}}

	d := &decision.Decision{
		Seq: 12, Player: 0, Kind: decision.KTarget, Prompt: "SECRET PROMPT", Min: 1, Max: 1, Source: 99,
		TargetEffect: &decision.TargetEffect{API: "DealDamage", Damage: &decision.DamageEffect{Amount: &dmg}},
		Options: []decision.Option{{
			Index: 0, Kind: "target", Label: "SECRET LABEL", Obj: 8, Player: 1,
			Attacker: 3, Required: true, Group: "g", AltCostIndex: 2, Mode: "kicked",
			Amount: 4, Ability: 5, SVar: "SECRET_SVAR", Cost: "SECRET_COST",
			Grant: &decision.Grant{Keywords: []string{"Flying"}},
		}},
		ResumeKind: "SECRET_RESUME", ResumeSA: &cards.SA{Kind: "SP", API: "SECRET_API"},
		ResumeModes: []string{"SECRET_MODE"}, ResumeTarget: 17, Rolls: []int32{6},
	}
	g := newGameTrace()
	if err := g.record(d, decision.Intent{Seq: 12, Player: 0, Choices: []int{0}}, &b, traceDecisionMeta{PairIndex: 4, Pair: "a:b", GameIndex: 6, Seed: 10, Policy: "bot"}); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(g.Decisions[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, secret := range []string{
		"SECRET PROMPT", "SECRET LABEL", "SECRET_SVAR", "SECRET_COST", "SECRET_RESUME", "SECRET_API", "SECRET_MODE",
		`"prompt"`, `"source"`, `"rolls"`, `"grant"`, `"resume_kind"`, `"resume_sa"`, `"resume_modes"`,
		`"resume_target"`, `"resume_choices"`, `"resume_chosen_valid"`, `"resume_remembered"`, `"resume_moved"`,
	} {
		if strings.Contains(s, secret) {
			t.Errorf("trace leaked %q: %s", secret, s)
		}
	}
	if !strings.Contains(s, `"card_id":2`) || strings.Index(s, `"card_id":2`) > strings.Index(s, `"card_id":9`) {
		t.Errorf("cards are not sorted by id: %s", s)
	}
	if !strings.Contains(s, `"player":0,"life":20`) || strings.Index(s, `"player":0,"life":20`) > strings.Index(s, `"player":1,"life":17`) {
		t.Errorf("life totals are not sorted by player: %s", s)
	}
	if !strings.Contains(s, `"target_effect":{"api":"DealDamage","damage":{"amount":3}}`) {
		t.Errorf("target effect missing: %s", s)
	}
}

func TestTraceSnapshotSurvivesBoardReuse(t *testing.T) {
	b := botpolicy.NewBoard(2)
	b.Cards.Set(2, botpolicy.Card{Power: 4, ManaCost: "2 G"})
	b.Creatures.Set(3, botpolicy.Creature{Power: 2, Keywords: []string{"Flying"}})
	b.Commanders.Set(3, botpolicy.Commander{Damage: map[state.PlayerID]int32{1: 5}})
	g := newGameTrace()
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{{Index: 0, Kind: "pass"}}}
	if err := g.record(d, decision.Intent{Seq: 1, Player: 0, Choices: []int{0}}, &b, traceDecisionMeta{}); err != nil {
		t.Fatal(err)
	}

	b.Cards.Set(2, botpolicy.Card{Power: 99, ManaCost: "SECRET"})
	b.Creatures.Set(3, botpolicy.Creature{Power: 99, Keywords: []string{"SECRET"}})
	b.Commanders.Get(3).Damage[1] = 99
	b.Cards.Delete(2)

	got, err := json.Marshal(g.Decisions[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, `"power":4`) || !strings.Contains(s, `"keywords":["Flying"]`) || !strings.Contains(s, `"damage":5`) {
		t.Fatalf("snapshot changed after board reuse: %s", s)
	}
	if strings.Contains(s, "SECRET") || strings.Contains(s, `"power":99`) {
		t.Fatalf("snapshot retained board aliases: %s", s)
	}
}

func TestTraceRejectsUnknownSchema(t *testing.T) {
	err := validateTraceRecord(traceDecisionV1{RecordType: "decision-v1", SchemaVersion: 2})
	if err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("validate unknown schema = %v, want schema version error", err)
	}
}

func TestTraceDoesNotChangeGameOrReplay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := []string{"mono-red-prowess", "mono-green-stompy"}
	decks := [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])}
	cfg := rules.Config{Seed: 17, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
	newSeats := func() []seat.Seat { return []seat.Seat{seat.NewBot(16), seat.NewBot(19)} }

	wantOutcome, wantEngine, err := playMatchOnce(cfg, []string{"bot", "bot"}, newSeats(), 200, 20000, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	trace := newGameTrace()
	gotOutcome, gotEngine, err := playMatchOnceTraced(cfg, []string{"bot", "bot"}, newSeats(), 200, 20000, nil, nil, trace, traceDecisionMeta{PairIndex: 0, Pair: "mono-red-prowess:mono-green-stompy", GameIndex: 0, Seed: 17})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotOutcome, wantOutcome) {
		t.Fatalf("trace changed outcome: got %+v want %+v", gotOutcome, wantOutcome)
	}
	if !reflect.DeepEqual(gotEngine.L.Events, wantEngine.L.Events) || gotEngine.L.Head() != wantEngine.L.Head() {
		t.Fatal("trace changed event stream or chain head")
	}
	if len(trace.Decisions) == 0 {
		t.Fatal("trace recorded no decisions")
	}
	for i, d := range trace.Decisions {
		if i > 0 && d.Sequence <= trace.Decisions[i-1].Sequence {
			t.Fatalf("decision %d sequence %d does not follow %d", i, d.Sequence, trace.Decisions[i-1].Sequence)
		}
		if d.Policy != "bot" {
			t.Fatalf("decision %d policy = %q, want bot", i, d.Policy)
		}
	}
	r, err := replay.Replay(gotEngine.L, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.L.Head() != gotEngine.L.Head() || !reflect.DeepEqual(r.L.Events, gotEngine.L.Events) {
		t.Fatal("trace-enabled game did not replay exactly")
	}
}

func TestDecisionTraceWritesRunThenOrderedGamesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	games := []*gameTrace{
		{Decisions: []traceDecisionV1{{RecordType: "decision-v1", SchemaVersion: 1, PairIndex: 0, Pair: "a:b", GameIndex: 0, Sequence: 2, Options: []traceOptionV1{}, Choices: []int{}, Board: traceBoardV1{SchemaVersion: 1}}}, Terminal: traceGameV1{RecordType: "game-v1", SchemaVersion: 1, PairIndex: 0, Pair: "a:b", GameIndex: 0}},
		{Decisions: []traceDecisionV1{{RecordType: "decision-v1", SchemaVersion: 1, PairIndex: 1, Pair: "a:c", GameIndex: 0, Sequence: 1, Options: []traceOptionV1{}, Choices: []int{}, Board: traceBoardV1{SchemaVersion: 1}}}, Terminal: traceGameV1{RecordType: "game-v1", SchemaVersion: 1, PairIndex: 1, Pair: "a:c", GameIndex: 0}},
	}
	run := traceRunV1{RecordType: "run-v1", SchemaVersion: 1, BoardSchemaVersion: 1, PolicyA: "bot", PolicyB: "legacy", BaseSeed: 0, GamesPerPair: 1, Seats: 2, Format: "constructed", Split: "development", Suite: "mono5", Pairs: []string{"a:b", "a:c"}}
	if err := writeDecisionTrace(path, run, games); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) != 5 {
		t.Fatalf("trace lines = %d, want 5: %s", len(lines), b)
	}
	wantTypes := []string{"run-v1", "decision-v1", "game-v1", "decision-v1", "game-v1"}
	for i, line := range lines {
		var hdr struct {
			RecordType string `json:"record_type"`
		}
		if err := json.Unmarshal(line, &hdr); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if hdr.RecordType != wantTypes[i] {
			t.Fatalf("line %d type = %q, want %q", i, hdr.RecordType, wantTypes[i])
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "trace.jsonl" {
		t.Fatalf("atomic writer left sibling artifacts: %v", entries)
	}
}

func TestDecisionTraceRefusesUnsafeDestinations(t *testing.T) {
	dir := t.TempDir()
	run := traceRunV1{RecordType: "run-v1", SchemaVersion: 1, BoardSchemaVersion: 1}
	existing := filepath.Join(dir, "existing.jsonl")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeDecisionTrace(existing, run, nil); err == nil {
		t.Fatal("existing destination was overwritten")
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "keep" {
		t.Fatalf("existing destination changed to %q", got)
	}
	missing := filepath.Join(dir, "absent", "trace.jsonl")
	if err := writeDecisionTrace(missing, run, nil); err == nil {
		t.Fatal("absent parent was created or accepted")
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatalf("absent parent unexpectedly exists: %v", err)
	}
}

func TestDecisionTraceValidationFailureLeavesNoDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	run := traceRunV1{RecordType: "run-v1", SchemaVersion: 99, BoardSchemaVersion: 1}
	if err := writeDecisionTrace(path, run, nil); err == nil {
		t.Fatal("unknown schema version was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("completed destination exists after validation failure: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("validation failure left temporary files: %v", entries)
	}
}

type failingTraceTemp struct {
	*os.File
	err error
}

func (f *failingTraceTemp) Write([]byte) (int, error) { return 0, f.err }

func TestDecisionTraceWriteFailureLeavesNoDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	wantErr := errors.New("injected write failure")
	ops := defaultTraceWriterOps()
	ops.createTemp = func(parent, pattern string) (traceTempFile, error) {
		f, err := os.CreateTemp(parent, pattern)
		if err != nil {
			return nil, err
		}
		return &failingTraceTemp{File: f, err: wantErr}, nil
	}
	err := writeDecisionTraceWithOps(path, traceRunV1{RecordType: "run-v1", SchemaVersion: 1, BoardSchemaVersion: 1}, nil, ops)
	if !errors.Is(err, wantErr) {
		t.Fatalf("write error = %v, want injected failure", err)
	}
	assertNoTraceArtifacts(t, dir, path)
}

func TestDecisionTracePublicationFailureLeavesNoDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	wantErr := errors.New("injected rename failure")
	ops := defaultTraceWriterOps()
	ops.publish = func(_, _ string) error { return wantErr }
	err := writeDecisionTraceWithOps(path, traceRunV1{RecordType: "run-v1", SchemaVersion: 1, BoardSchemaVersion: 1}, nil, ops)
	if !errors.Is(err, wantErr) {
		t.Fatalf("rename error = %v, want injected failure", err)
	}
	assertNoTraceArtifacts(t, dir, path)
}

func TestDecisionTraceConcurrentDestinationIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	ops := defaultTraceWriterOps()
	publish := ops.publish
	ops.publish = func(old, new string) error {
		if err := os.WriteFile(new, []byte("concurrent writer"), 0o600); err != nil {
			return err
		}
		return publish(old, new)
	}
	err := writeDecisionTraceWithOps(path, traceRunV1{RecordType: "run-v1", SchemaVersion: 1, BoardSchemaVersion: 1}, nil, ops)
	if err == nil {
		t.Fatal("concurrently created destination was overwritten")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "concurrent writer" {
		t.Fatalf("concurrently created destination changed to %q", got)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "trace.jsonl" {
		t.Fatalf("race failure left temporary files: %v", entries)
	}
}

func assertNoTraceArtifacts(t *testing.T, dir, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("completed destination exists after failure: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failure left temporary files: %v", entries)
	}
}

func TestDecisionTraceHeaderRecognizesExactMono5Splits(t *testing.T) {
	pairs := mono5Pairs()
	dev := newTraceRunV1(0, 100, "bot", "legacy", pairs, 200, 20000, false)
	if dev.Suite != "mono5" || dev.Split != "development" {
		t.Fatalf("development header suite/split = %q/%q", dev.Suite, dev.Split)
	}
	held := newTraceRunV1(1_000_000, 400, "bot", "legacy", pairs, 200, 20000, false)
	if held.Suite != "mono5" || held.Split != "heldout" {
		t.Fatalf("heldout header suite/split = %q/%q", held.Suite, held.Split)
	}
	other := newTraceRunV1(0, 99, "bot", "legacy", pairs, 200, 20000, false)
	if other.Suite != "mono5" || other.Split != "" {
		t.Fatalf("nonstandard sample suite/split = %q/%q", other.Suite, other.Split)
	}
}

func mono5Pairs() []pairDef {
	names := []string{"mono-white-equipment", "mono-blue-tempo", "mono-black-aggro", "mono-red-prowess", "mono-green-stompy"}
	return fullPairs(names)
}

func TestTraceWorkerCountDeterminism(t *testing.T) {
	pairs := []pairDef{
		{a: "mono-red-prowess", b: "mono-green-stompy"},
		{a: "mono-blue-tempo", b: "mono-black-aggro"},
	}
	corpus := corpusDirOrSkip(t)
	run := func(workers int) []byte {
		path := filepath.Join(t.TempDir(), "trace.jsonl")
		if err := runMatrixTraced(23, 4, 2, "bot", "legacy", corpus, "json", pairs, workers, 200, 20000, false, nil, path, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	one := run(1)
	many := run(4)
	if !bytes.Equal(one, many) {
		t.Fatal("decision trace differs between workers=1 and workers=4")
	}
}
