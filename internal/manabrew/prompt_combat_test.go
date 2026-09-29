package manabrew

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
)

// TestAttackersPromptAndResponse covers the §6.3 chooseAttackers build (the
// per-creature grouping, the per-attacker CR 509.1d requirement, the target
// labels/kinds) and the declareAttackers response mapping.
func TestAttackersBlockersAttackersPromptAndResponse(t *testing.T) {
	tr := New("table", 2, nil)
	v := battleView()
	// Precondition: the fixture resolves the attacker, the battle and the
	// planeswalker projections the labels are drawn from.
	if findCard(&v, 2) == nil || findCard(&v, 5) == nil || playerLabel(&v, 1) != "Bob" {
		t.Fatal("fixture must resolve the attacker creature, the battle and Bob")
	}
	d := &decision.Decision{Seq: 21, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 2, Prompt: "Declare attackers",
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Label: "Bear attacks Bob", Obj: 2, Player: 1},
			{Index: 1, Kind: "attacker", Label: "Bear attacks Siege", Obj: 2, Battle: 5},
			{Index: 2, Kind: "attacker", Label: "Bear attacks Chandra", Obj: 2, Battle: 6},
			{Index: 3, Kind: "attacker", Label: "Elf attacks Bob (required)", Obj: 4, Player: 1, Required: true},
		}}
	msg := pendingMustBuild(t, d, v)
	in, ok := msg.Input.Value.(mb.ChooseAttackersInput)
	if !ok {
		t.Fatalf("input type = %s, want chooseAttackers", msg.Input.Value.PromptType())
	}
	if len(in.Attackers) != 2 {
		t.Fatalf("attackers = %#v, want one per creature (o2, o4)", in.Attackers)
	}
	bear, elf := in.Attackers[0], in.Attackers[1]
	if bear.AttackerID != "o2" || !reflect.DeepEqual(bear.ValidTargetIDs, []string{"player-1", "o5", "o6"}) {
		t.Fatalf("bear grouping = %#v, want all three targets in option order", bear)
	}
	if bear.MustAttack || !elf.MustAttack {
		t.Fatalf("CR 509.1d requirements: bear %v elf %v, want elf only", bear.MustAttack, elf.MustAttack)
	}
	if elf.AttackerID != "o4" {
		t.Fatalf("elf grouping = %#v", elf)
	}
	if len(in.AttackTargets) != 3 {
		t.Fatalf("attackTargets = %#v, want three distinct refs", in.AttackTargets)
	}
	byID := make(map[string]mb.AttackTargetDto)
	for _, at := range in.AttackTargets {
		byID[at.ID] = at
	}
	if at := byID["player-1"]; at.Kind != "player" || at.Label != "Bob" {
		t.Fatalf("player target = %#v, want Bob/player", at)
	}
	if at := byID["o5"]; at.Kind != "battle" || at.Label != "Siege" {
		t.Fatalf("battle target = %#v, want Siege/battle", at)
	}
	if at := byID["o6"]; at.Kind != "planeswalker" || at.Label != "Chandra" {
		t.Fatalf("planeswalker target = %#v, want Chandra/planeswalker", at)
	}

	p := pendingFor(d, v)
	// One creature, one target, response order kept.
	o := tr.TranslateResponse(respFor(p, mb.DeclareAttackersDecision{Assignments: []mb.AttackerAssignment{
		{AttackerID: "o4", TargetID: "player-1"}, {AttackerID: "o2", TargetID: "o5"}}}), p, 0)
	got := mustIntent(t, o)
	if !reflect.DeepEqual(got.Choices, []int{3, 1}) {
		t.Fatalf("choices = %v, want the response order [3 1]", got.Choices)
	}
	// An empty declaration is legal (Min 0).
	o = tr.TranslateResponse(respFor(p, mb.DeclareAttackersDecision{}), p, 0)
	if !reflect.DeepEqual(mustIntent(t, o).Choices, []int{}) {
		t.Fatal("empty declaration must map to no choices")
	}
	// A pairing the engine did not offer: invalidShape.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.DeclareAttackersDecision{Assignments: []mb.AttackerAssignment{
		{AttackerID: "o2", TargetID: "o9"}}}), p, 0), mb.CodeInvalidShape)
	// A pairing whose ids are real but the combination is not offered.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.DeclareAttackersDecision{Assignments: []mb.AttackerAssignment{
		{AttackerID: "o4", TargetID: "o5"}}}), p, 0), mb.CodeInvalidShape)
	// A size bound Decision.Validate enforces: Max 2 already used above; a
	// Max=1 decision rejects the second assignment with its own text.
	d1 := *d
	d1.Max = 1
	p1 := pendingFor(&d1, v)
	o = tr.TranslateResponse(respFor(p1, mb.DeclareAttackersDecision{Assignments: []mb.AttackerAssignment{
		{AttackerID: "o4", TargetID: "player-1"}, {AttackerID: "o2", TargetID: "o5"}}}), p1, 0)
	wantErrCode(t, o, mb.CodeInvalidShape)
}

// TestBlockersPromptAndResponse covers the §6.3 chooseBlockers build (the
// per-attacker bounds and must-be-blocked flag, the blocker pool) and the
// declareBlockers response mapping.
func TestAttackersBlockersBlockersPromptAndResponse(t *testing.T) {
	tr := New("table", 2, nil)
	v := smallView()
	maxOne := 1
	d := &decision.Decision{Seq: 22, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 2, Prompt: "Declare blockers",
		Options: []decision.Option{
			{Index: 0, Kind: "block", Label: "Bear blocks Shock-attacker", Obj: 2, Attacker: 3, MinBlockers: 1, MaxBlockers: maxOne, AttackMust: true},
			{Index: 1, Kind: "block", Label: "Elf blocks Shock-attacker", Obj: 4, Attacker: 3, MinBlockers: 1, MaxBlockers: maxOne},
		}}
	msg := pendingMustBuild(t, d, v)
	in, ok := msg.Input.Value.(mb.ChooseBlockersInput)
	if !ok {
		t.Fatalf("input type = %s, want chooseBlockers", msg.Input.Value.PromptType())
	}
	if len(in.Attackers) != 1 {
		t.Fatalf("attackers = %#v, want one per attacking creature", in.Attackers)
	}
	a := in.Attackers[0]
	if a.AttackerID != "o3" {
		t.Fatalf("attacker id = %q, want o3", a.AttackerID)
	}
	// Precondition: the engine offered the bounds this asserts against.
	if d.Options[0].MinBlockers != 1 || d.Options[0].MaxBlockers != 1 {
		t.Fatalf("fixture must carry a 1..1 blocker bound, got %v", d.Options[0])
	}
	if a.MinBlockers != 1 || a.MaxBlockers == nil || *a.MaxBlockers != 1 {
		t.Fatalf("blocker bounds = %#v, want 1..1", a)
	}
	if !a.MustBeBlocked {
		t.Fatal("CR 509.1c must-be-blocked must surface")
	}
	if !reflect.DeepEqual(a.ValidBlockerIDs, []string{"o2", "o4"}) {
		t.Fatalf("validBlockerIds = %v, want both blockers in option order", a.ValidBlockerIDs)
	}
	if !reflect.DeepEqual(in.AvailableBlockerIDs, []string{"o2", "o4"}) {
		t.Fatalf("availableBlockerIds = %v", in.AvailableBlockerIDs)
	}

	p := pendingFor(d, v)
	o := tr.TranslateResponse(respFor(p, mb.DeclareBlockersDecision{Assignments: []mb.BlockerAssignment{
		{BlockerID: "o4", AttackerID: "o3"}}}), p, 1)
	if !reflect.DeepEqual(mustIntent(t, o).Choices, []int{1}) {
		t.Fatal("block assignment must map to its option")
	}
	// Unknown blocker → invalidShape.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.DeclareBlockersDecision{Assignments: []mb.BlockerAssignment{
		{BlockerID: "o9", AttackerID: "o3"}}}), p, 1), mb.CodeInvalidShape)
	// Unknown attacker for a real blocker → invalidShape.
	wantErrCode(t, tr.TranslateResponse(respFor(p, mb.DeclareBlockersDecision{Assignments: []mb.BlockerAssignment{
		{BlockerID: "o2", AttackerID: "o7"}}}), p, 1), mb.CodeInvalidShape)
}

// TestAttackTargetIDPinsRefMint pins the §6.1 ref mint each side of an
// attacker option uses, so a player/battle ref mix-up fails here rather
// than on a live table.
func TestAttackersBlockersRefMint(t *testing.T) {
	playerOpt := decision.Option{Obj: 2, Player: state.PlayerID(1)}
	battleOpt := decision.Option{Obj: 2, Battle: 5}
	if attackTargetID(playerOpt) != "player-1" {
		t.Fatalf("player-attack ref = %q, want player-1", attackTargetID(playerOpt))
	}
	if attackTargetID(battleOpt) != "o5" {
		t.Fatalf("permanent-attack ref = %q, want o5", attackTargetID(battleOpt))
	}
}
