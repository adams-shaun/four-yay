package manabrew

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// docTargets maps each standalone doc example file to the type it decodes
// into. A new *.json file under testdata/docs must be listed here or handled
// by TestDocExamplesRoundTrip's case walker, or the test fails.
func docTargets() map[string]func() any {
	return map[string]func() any{
		"state.json":           func() any { return new(EngineMessage) },
		"prompt.json":          func() any { return new(AgentPrompt) },
		"client-response.json": func() any { return new(ClientMessage) },
	}
}

// TestDocExamplesRoundTrip decodes every doc example leniently, asserts the
// package models every field it uses (no unknown paths), re-encodes it
// canonically, and proves the re-encode decodes to the same value and encodes
// to the same bytes again.
func TestDocExamplesRoundTrip(t *testing.T) {
	entries, err := os.ReadDir("testdata/docs")
	if err != nil {
		t.Fatalf("read testdata/docs: %v", err)
	}
	handled := map[string]bool{"prompt-cases.json": true}
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		if name == "prompt-cases.json" {
			roundTripPromptCases(t, name)
			continue
		}
		mk, ok := docTargets()[name]
		if !ok {
			t.Errorf("unhandled doc example %s: add it to docTargets or the case walker", name)
			continue
		}
		handled[name] = true
		raw, err := os.ReadFile(filepath.Join("testdata/docs", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			t.Fatalf("%s: doc example is empty", name)
		}
		roundTripDocValue(t, name, raw, mk)
	}
	for name := range docTargets() {
		if !handled[name] {
			t.Errorf("docTargets lists %s but testdata/docs has no such file", name)
		}
	}
}

// roundTripDocValue is the shared decode→encode→decode→encode cycle.
func roundTripDocValue(t *testing.T, name string, raw []byte, mk func() any) {
	t.Helper()
	v := mk()
	unknown, err := Decode(raw, v)
	if err != nil {
		t.Fatalf("%s: lenient decode: %v", name, err)
	}
	if len(unknown) > 0 {
		t.Fatalf("%s: doc example uses fields this package does not model: %v", name, unknown)
	}
	first, err := Encode(v)
	if err != nil {
		t.Fatalf("%s: encode: %v", name, err)
	}
	v2 := mk()
	if _, err := Decode(first, v2); err != nil {
		t.Fatalf("%s: decode of canonical encoding: %v\nbytes: %s", name, err, first)
	}
	if !reflect.DeepEqual(v, v2) {
		t.Fatalf("%s: canonical re-decode differs\nfirst:  %s\nbefore: %#v\nafter:  %#v", name, first, v, v2)
	}
	second, err := Encode(v2)
	if err != nil {
		t.Fatalf("%s: re-encode: %v", name, err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("%s: encode is not stable:\n%s\n%s", name, first, second)
	}
}

// roundTripPromptCases walks testdata/docs/prompt-cases.json: one input and
// one output example per prompt type, both round-tripped through their union
// codecs.
func roundTripPromptCases(t *testing.T, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata/docs", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var cases []struct {
		Prompt string          `json:"prompt"`
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s: no cases", name)
	}
	for _, c := range cases {
		if c.Prompt == "" {
			t.Fatalf("%s: case without a prompt name", name)
		}
		var in PromptInput
		unknown, err := Decode(c.Input, &in)
		if err != nil {
			t.Errorf("%s/%s: input decode: %v", name, c.Prompt, err)
			continue
		}
		if len(unknown) > 0 {
			t.Errorf("%s/%s: input example unmodelled: %v", name, c.Prompt, unknown)
			continue
		}
		if in.Value == nil || in.Value.PromptType() == "" {
			t.Errorf("%s/%s: input decoded without a type", name, c.Prompt)
			continue
		}
		b1, err := Encode(in)
		if err != nil {
			t.Errorf("%s/%s: input encode: %v", name, c.Prompt, err)
			continue
		}
		var in2 PromptInput
		if _, err := Decode(b1, &in2); err != nil {
			t.Errorf("%s/%s: input re-decode: %v\nbytes: %s", name, c.Prompt, err, b1)
			continue
		}
		b2, err := Encode(in2)
		if err != nil || !bytes.Equal(b1, b2) {
			t.Errorf("%s/%s: input encode unstable: %v\n%s\n%s", name, c.Prompt, err, b1, b2)
			continue
		}

		var out PromptOutputData
		unknown, err = Decode(c.Output, &out)
		if err != nil {
			t.Errorf("%s/%s: output decode: %v", name, c.Prompt, err)
			continue
		}
		if len(unknown) > 0 {
			t.Errorf("%s/%s: output example unmodelled: %v", name, c.Prompt, unknown)
			continue
		}
		o1, err := Encode(out)
		if err != nil {
			t.Errorf("%s/%s: output encode: %v", name, c.Prompt, err)
			continue
		}
		var out2 PromptOutputData
		if _, err := Decode(o1, &out2); err != nil {
			t.Errorf("%s/%s: output re-decode: %v\nbytes: %s", name, c.Prompt, err, o1)
			continue
		}
		if !reflect.DeepEqual(out, out2) {
			t.Errorf("%s/%s: output re-decode differs: %#v vs %#v", name, c.Prompt, out, out2)
		}
	}
}

// TestEveryPromptTypeHasCodec round-trips a sample input and output for each
// of the 19 prompt types through the union codecs, and asserts each type's
// discriminator is what its codec emits.
func TestEveryPromptTypeHasCodec(t *testing.T) {
	rows := []struct {
		prompt string
		input  PromptInputData
		output PromptOutputValue
	}{
		{"chooseNumber", &ChooseNumberInput{PromptBase: base("N"), Min: 0, Max: 2}, &NumberDecision{ChosenNumber: ip(1)}},
		{"chooseCards", &ChooseCardsInput{PromptBase: base("C"), Cards: []CardDto{card("c1")}, Min: 1, Max: 1}, &ChooseCardsDecision{ChosenCardIDs: []string{"c1"}}},
		{"chooseColor", &ChooseColorInput{PromptBase: base("K"), ValidColors: []string{"W"}, Amount: 1}, &ColorDecision{ChosenColors: map[string]int{"W": 1}}},
		{"chooseBoolean", &ChooseBooleanInput{PromptBase: base("B"), ConfirmLabel: "Yes", DenyLabel: "No"}, &BooleanDecision{Value: true}},
		{"chooseFromSelection", &ChooseFromSelectionInput{PromptBase: base("S"), Options: []SelectionOption{{Label: "A", Weight: 1}}, MinTotal: 1, MaxTotal: 1}, &SelectionDecision{ChosenIndices: []int{0}}},
		{"revealCards", &RevealCardsInput{PromptBase: base("R"), Cards: []CardDto{card("c1")}, Zone: ZoneHand, OwnerPlayerID: "p0"}, &RevealCardsAcknowledged{}},
		{"scry", &ScryInput{PromptBase: base("Y"), Cards: []CardDto{card("c1")}, Zones: []ScryDestination{DestinationLibraryTop}}, &ScryDecision{ZoneCardIDs: [][]string{{"c1"}}}},
		{"reorder", &ReorderInput{PromptBase: base("O"), Items: []ReorderItem{{ID: "t1"}}}, &ReorderDecision{OrderedIDs: []string{"t1"}}},
		{"diceRolled", &DiceRolledInput{PromptBase: base("D"), Sides: 6, Rolls: []DiceRollEntry{{Round: 1, NaturalResults: []int{3}, FinalResults: []int{3}, IgnoredRolls: []int{}}}}, &DiceRolledAcknowledged{}},
		{"chooseAction", &ChooseActionInput{Actions: []AvailableAction{{ID: "a1", Type: "pass"}}}, &PassOutput{ExhaustStack: true}},
		{"payManaCost", &PayManaCostInput{PromptBase: base("P"), CardID: "c1", CardName: "Shock", ManaCost: "R", Actions: []PaymentAction{{ID: "p1", CardID: "c2", AbilityIndex: 0, IsManaAbility: true}}}, &PayOutput{Auto: true}},
		{"mulligan", &MulliganInput{HandCardIDs: []string{"c1"}, MulliganCount: 1}, &MulliganDecision{Keep: false}},
		{"mulliganPutBack", &MulliganPutBackInput{HandCardIDs: []string{"c1"}, Cards: []CardDto{card("c1")}, Count: 1}, &MulliganPutBackDecision{CardIDs: []string{"c1"}}},
		{"chooseAttackers", &ChooseAttackersInput{Attackers: []AttackerOptionDto{{AttackerID: "a", ValidTargetIDs: []string{"p1"}}}, AttackTargets: []AttackTargetDto{{ID: "p1", Label: "P1", Kind: "player"}}}, &DeclareAttackersDecision{Assignments: []AttackerAssignment{{AttackerID: "a", TargetID: "p1"}}}},
		{"chooseBlockers", &ChooseBlockersInput{Attackers: []BlockableAttackerDto{{AttackerID: "a", ValidBlockerIDs: []string{"b"}, MinBlockers: 0}}, AvailableBlockerIDs: []string{"b"}}, &DeclareBlockersDecision{Assignments: []BlockerAssignment{{BlockerID: "b", AttackerID: "a"}}}},
		{"chooseDamageAssignmentOrder", &ChooseDamageAssignmentOrderInput{AttackerID: "a", BlockerIDs: []string{"b"}, BlockerCards: []CardDto{card("b")}}, &DamageAssignmentOrderDecision{OrderedBlockerIDs: []string{"b"}}},
		{"chooseCombatDamageAssignment", &ChooseCombatDamageAssignmentInput{AttackerID: "a", BlockerIDs: []string{"b"}, TotalDamage: 2}, &DamageAssignmentDecision{Assignments: []DamageAssignment{{AssigneeID: "b", Damage: 2}}}},
		{"chooseBoardTargets", &ChooseBoardTargetsInput{PromptBase: base("T"), Candidates: []TargetRef{{Kind: RefPlayer, ID: "p1"}}, Intent: IntentDamage, MinTargets: 1, MaxTargets: 1}, &BoardTargetsDecision{Chosen: []TargetRef{{Kind: RefPlayer, ID: "p1", Intent: IntentDamage}}}},
		{"gameOver", &GameOverInput{}, nil},
	}
	if len(rows) != 19 {
		t.Fatalf("prompt type table has %d rows, want the protocol's 19", len(rows))
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.prompt] {
			t.Errorf("duplicate prompt type %s in table", row.prompt)
		}
		seen[row.prompt] = true

		if row.input.PromptType() != row.prompt {
			t.Errorf("%s: input codec reports type %q", row.prompt, row.input.PromptType())
			continue
		}
		in1, err := roundTripInput(t, row.prompt, PromptInput{Value: row.input})
		if err != nil {
			t.Errorf("%s: input round trip: %v", row.prompt, err)
			continue
		}
		if !strings.Contains(string(in1), `"type":"`+row.prompt+`"`) {
			t.Errorf("%s: input encoding misses its discriminator: %s", row.prompt, in1)
		}

		if row.output == nil { // gameOver is terminal: no response exists
			continue
		}
		if row.output.OutputType() == "" {
			t.Errorf("%s: output codec reports an empty type", row.prompt)
		}
		if err := roundTripOutput(t, row.prompt, PromptOutputData{Value: row.output}); err != nil {
			t.Errorf("%s: output round trip: %v", row.prompt, err)
		}
	}
	// Every case registered in the codec maps must be in the table (so the
	// table is the closed prompt catalogue) and vice versa.
	var registered []string
	for k := range promptInputCases {
		registered = append(registered, k)
	}
	sort.Strings(registered)
	if len(registered) != len(rows) {
		t.Errorf("promptInputCases registers %d types, table has %d", len(registered), len(rows))
	}
	for _, k := range registered {
		if !seen[k] {
			t.Errorf("promptInputCases registers %q, which the test table does not cover", k)
		}
	}
}

func roundTripInput(t *testing.T, prompt string, in PromptInput) ([]byte, error) {
	t.Helper()
	b1, err := Encode(in)
	if err != nil {
		return nil, err
	}
	var in2 PromptInput
	if _, err := Decode(b1, &in2); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(in, in2) {
		return nil, errMismatch{what: "input", b: b1, a: in, c: in2}
	}
	b2, err := Encode(in2)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(b1, b2) {
		return nil, errMismatch{what: "input bytes", b: b1, a: b2}
	}
	return b1, nil
}

func roundTripOutput(t *testing.T, prompt string, out PromptOutputData) error {
	t.Helper()
	o1, err := Encode(out)
	if err != nil {
		return err
	}
	var out2 PromptOutputData
	if _, err := Decode(o1, &out2); err != nil {
		return err
	}
	if !reflect.DeepEqual(out, out2) {
		return errMismatch{what: "output", b: o1, a: out, c: out2}
	}
	o2, err := Encode(out2)
	if err != nil {
		return err
	}
	if !bytes.Equal(o1, o2) {
		return errMismatch{what: "output bytes", b: o1, a: o2}
	}
	return nil
}

type errMismatch struct {
	what string
	b    []byte
	a, c any
}

func (e errMismatch) Error() string {
	return e.what + " differs across the round trip: " + string(e.b) + ": " +
		reflect.ValueOf(e.a).String() + " vs " + reflect.ValueOf(e.c).String()
}

func base(title string) PromptBase {
	return PromptBase{Presentation: PromptPresentation{Title: title, Targets: []TargetRef{}}}
}

func card(id string) CardDto {
	name := "Card " + id
	return CardDto{
		ID:            id,
		Identity:      CardIdentity{Name: name, SetCode: "tst", CardNumber: "1"},
		Color:         []string{},
		Types:         []string{"Creature"},
		Subtypes:      []string{},
		Supertypes:    []string{},
		Power:         sp("2"),
		Toughness:     sp("2"),
		ClassLevels:   []ClassLevelDto{},
		SagaChapters:  []SagaChapterDto{},
		Choices:       []CardChoiceDto{},
		ControllerID:  "p0",
		OwnerID:       "p0",
		Keywords:      []string{},
		Counters:      map[string]int{},
		AttachmentIDs: []string{},
		MergedCardIDs: []string{},
	}
}

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

// TestLenientDecodeReportsUnknown checks that Decode surfaces the paths of
// fields the schema does not model, at every depth, while still decoding the
// rest of the message.
func TestLenientDecodeReportsUnknown(t *testing.T) {
	t.Run("nested unknown fields in a response", func(t *testing.T) {
		raw := []byte(`{"kind":"response","promptId":1,"wat":true,` +
			`"action":{"type":"chooseNumber","output":{"type":"numberDecision","chosenNumber":2,"extra":false}}}`)
		var m ClientMessage
		paths, err := Decode(raw, &m)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := []string{"action.output.extra", "wat"}
		if !reflect.DeepEqual(paths, want) {
			t.Fatalf("unknown paths = %v, want %v", paths, want)
		}
		resp, ok := m.Value.(ClientResponse)
		if !ok {
			t.Fatalf("decoded %T, want ClientResponse", m.Value)
		}
		num, ok := resp.Action.Output.Value.(*NumberDecision)
		if !ok {
			t.Fatalf("action output is %T, want *NumberDecision", resp.Action.Output)
		}
		if num.ChosenNumber == nil || *num.ChosenNumber != 2 {
			t.Fatalf("chosenNumber = %v, want 2", num.ChosenNumber)
		}
	})
	t.Run("unknown field inside a prompt input", func(t *testing.T) {
		raw := []byte(`{"promptId":5,"decidingPlayerId":"p0",` +
			`"input":{"type":"mulligan","handCardIds":["c1"],"mulliganCount":0,"bogus":1}}`)
		var p AgentPrompt
		paths, err := Decode(raw, &p)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if want := []string{"input.bogus"}; !reflect.DeepEqual(paths, want) {
			t.Fatalf("unknown paths = %v, want %v", paths, want)
		}
		if p.Input.Value.PromptType() != "mulligan" {
			t.Fatalf("input type = %v", p.Input.Value.PromptType())
		}
	})
	t.Run("unknown fields in state and card views", func(t *testing.T) {
		raw := []byte(`{"kind":"state","gameView":{"gameId":"g","turn":1,"step":"main1",` +
			`"combatAssignments":[],"activePlayerId":"p0","priorityPlayerId":"p0",` +
			`"players":[],"zones":[{"zone":"hand","ownerId":"p0","cards":[{"visibility":"hidden","id":"c1","sideboard":true}],"count":1,"modifier":2}],"stack":[],` +
			`"gameOver":false,"winnerId":null,"monarchId":null,"initiativeHolderId":null,"dayTime":""}}`)
		var m EngineMessage
		paths, err := Decode(raw, &m)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := []string{"gameView.zones[0].cards[0].sideboard", "gameView.zones[0].modifier"}
		if !reflect.DeepEqual(paths, want) {
			t.Fatalf("unknown paths = %v, want %v", paths, want)
		}
	})
	t.Run("unknown discriminators are errors, naming the field", func(t *testing.T) {
		cases := []struct {
			raw  string
			into any
			want string
		}{
			{`{"kind":"banana"}`, new(ClientMessage), `unknown kind discriminator "banana"`},
			{`{"kind":"banana"}`, new(EngineMessage), `unknown kind discriminator "banana"`},
			{`{"promptId":1,"decidingPlayerId":"p","input":{"type":"nope"}}`, new(AgentPrompt), `unknown input.type discriminator "nope"`},
			{`{"kind":"response","promptId":1,"action":{"type":"chooseNumber","output":{"type":"nope"}}}`, new(ClientMessage), `unknown output.type discriminator "nope"`},
			{`{"kind":"directive","directive":{"type":"surrender"}}`, new(ClientMessage), `unknown type discriminator "surrender"`},
			{`{"visibility":"sideways"}`, new(CardView), `unknown visibility discriminator "sideways"`},
		}
		for _, c := range cases {
			_, err := Decode([]byte(c.raw), c.into)
			if err == nil {
				t.Errorf("%s: decode succeeded, want error %q", c.raw, c.want)
				continue
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: error %q does not name the discriminator: want %q", c.raw, err, c.want)
			}
		}
	})
	t.Run("marshalling rejects a conflicting kind", func(t *testing.T) {
		if _, err := Encode(StateUpdate{Kind: "banana"}); err == nil {
			t.Errorf("marshalling StateUpdate with a foreign kind succeeded")
		}
		if _, err := Encode(EngineMessage{Value: StateUpdate{}}); err != nil {
			t.Errorf("marshalling a state with an unset kind: %v", err)
		}
		for _, u := range []any{EngineMessage{}, ClientMessage{}, PromptInput{}, CardView{}} {
			if _, err := Encode(u); err == nil {
				t.Errorf("marshalling an empty %T union succeeded", u)
			}
		}
		// A response whose action carries no output payload cannot be encoded
		// canonically: the wire shape requires action.output.type.
		if _, err := Encode(ClientResponse{Kind: "response", PromptID: 1,
			Action: PromptOutput{Type: "chooseNumber"}}); err == nil {
			t.Errorf("encoding a response without action.output succeeded")
		}
	})
}

// TestEncodeIsCanonical pins the canonical-encode properties the adapter and
// replay rely on: compact output and sorted map keys (the determinism rule
// that no map range may reach the wire).
func TestEncodeIsCanonical(t *testing.T) {
	p := PlayerDto{
		ID:              "p0",
		Counters:        map[PlayerCounterKind]int{CounterLoyalty: 1, "ZZZ": 2, "AAA": 3},
		ManaPool:        map[ManaColor]int{ColorGreen: 1, ColorBlue: 2},
		CommanderDamage: map[string]int{"p2": 1, "p1": 4},
	}
	b, err := Encode(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := string(b)
	var compact bytes.Buffer
	if err := json.Compact(&compact, b); err != nil {
		t.Fatalf("encode is not valid JSON: %v", err)
	}
	if !bytes.Equal(compact.Bytes(), b) {
		t.Fatalf("encode is not compact: %s", s)
	}
	for _, pair := range [][2]string{
		{`"AAA":3`, `"ZZZ":2`},
		{`"AAA":3`, `"Loyalty":1`},
		{`"U":2`, `"G":1`},
		{`"p1":4`, `"p2":1`},
	} {
		i := strings.Index(s, pair[0])
		j := strings.Index(s, pair[1])
		if i < 0 || j < 0 {
			t.Fatalf("encoded player is missing %s or %s: %s", pair[0], pair[1], s)
		}
		if i > j {
			t.Fatalf("map keys are not sorted: %s before %s in %s", pair[0], pair[1], s)
		}
	}
}
