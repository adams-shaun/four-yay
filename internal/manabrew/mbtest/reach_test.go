//go:build manabrew

package mbtest

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/bench"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	"github.com/adams-shaun/gorge/internal/testutil"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// MBX-3 reachability tests. TestManaBrewReachability is the census over the
// same 20 repo-deck games as the MB-8 census (TestManaBrewCensusNoUnmapped's
// seed set), asserting the ratchet: every native option a game posed must be
// selectable through the ManaBrew wire, or its (decision kind, option kind)
// must be an allowlisted entry naming the reason the protocol cannot express
// it. The unit tests below pin the inverse mapper (ResponseForIntent) itself
// on the shapes where the wire's expressible-vs-not line is non-obvious.

// reachTranslator is a fresh Translator for the synthetic unit decisions.
func reachTranslator(t *testing.T) *manabrew.Translator {
	t.Helper()
	return manabrew.New("reach", 2, nil)
}

// reachPending builds the prompt for d over v and the Pending a probe
// translates against. A decision the translator cannot build is a test bug.
func reachPending(t *testing.T, d *decision.Decision, v view.View) *manabrew.Pending {
	t.Helper()
	msg, err := reachTranslator(t).Prompt(d, &v)
	if err != nil {
		t.Fatalf("Prompt(%s, resume %q): %v", d.Kind, d.ResumeKind, err)
	}
	return &manabrew.Pending{Prompt: msg, Decision: d, View: v}
}

// roundTrip runs ResponseForIntent's message through the real wire codec and
// TranslateResponse, returning the translated intent.
func roundTrip(t *testing.T, p *manabrew.Pending, want decision.Intent) (decision.Intent, error) {
	t.Helper()
	msg, err := ResponseForIntent(p, want)
	if err != nil {
		return decision.Intent{}, err
	}
	raw, err := mb.Encode(msg)
	if err != nil {
		return decision.Intent{}, err
	}
	var wireResp mb.ClientMessage
	if _, err := mb.Decode(raw, &wireResp); err != nil {
		return decision.Intent{}, err
	}
	outcome := reachTranslator(t).TranslateResponse(wireResp, p, p.Decision.Player)
	if outcome.Err != nil {
		return decision.Intent{}, fmt.Errorf("%s: %s", outcome.Err.Code, outcome.Err.Message)
	}
	if outcome.Intent == nil {
		return decision.Intent{}, fmt.Errorf("translate returned no intent")
	}
	return *outcome.Intent, nil
}

// reachView is a two-seat view with two Bears (o2, o3) for card-shaped
// options.
func reachView() view.View {
	return view.View{Viewer: 0, Turn: 3, Step: "main1", Active: 0, Priority: 1, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, HandSize: 0, LibrarySize: 30,
			Battlefield: []view.CardView{
				{ID: 2, Name: "Bear", Printing: view.Printing{Name: "Bear"}, Types: "Creature — Bear"},
				{ID: 3, Name: "Bear", Printing: view.Printing{Name: "Bear"}, Types: "Creature — Bear"},
			}},
		{ID: 1, Name: "Bob", Life: 18},
	}}
}

// TestResponseForIntentBooleanSides pins the chooseBoolean inverse: both
// sides of a yes/no ask round-trip to the identical single-pick intent.
func TestResponseForIntentBooleanSides(t *testing.T) {
	v := reachView()
	d := &decision.Decision{Seq: 7, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Sacrifice?",
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Label: "Sacrifice it"},
			{Index: 1, Kind: "no", Label: "Keep it"},
		}}
	p := reachPending(t, d, v)
	for _, wantIdx := range []int{0, 1} {
		want := decision.Intent{Seq: 7, Player: 0, Choices: []int{wantIdx}}
		got, err := roundTrip(t, p, want)
		if err != nil {
			t.Fatalf("boolean side %d: %v", wantIdx, err)
		}
		if len(got.Choices) != 1 || got.Choices[0] != wantIdx {
			t.Fatalf("boolean side %d translated to choices %v", wantIdx, got.Choices)
		}
	}
}

// TestResponseForIntentPriorityShapes pins the priority inverse: act, pass
// and the announce action each round-trip to the identical intent, and the
// concede option goes through the directive path to its own intent.
func TestResponseForIntentPriorityShapes(t *testing.T) {
	v := reachView()
	castIdx := 0
	d := &decision.Decision{Seq: 9, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Prompt: "Priority",
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Label: "Activate Bear", Obj: 2},
			{Index: 1, Kind: "pass", Label: "Pass priority"},
			{Index: 2, Kind: "concede", Label: "Concede"},
		},
		PaymentActions: []decision.PaymentAction{{ID: "cast1", Cast: decision.PlannedCast{},
			BaseOptionIndex: &castIdx, Label: "Announce a cast",
			Plans: []decision.PaymentPlan{{Version: decision.PaymentPlanV1}}}},
	}
	p := reachPending(t, d, v)
	for _, tc := range []struct {
		name string
		want decision.Intent
	}{
		{"act", decision.Intent{Seq: 9, Player: 0, Choices: []int{0}}},
		{"pass", decision.Intent{Seq: 9, Player: 0, Choices: []int{1}}},
		{"announce", decision.Intent{Seq: 9, Player: 0, Announce: &decision.AnnounceSelection{ActionID: "cast1"}}},
	} {
		got, err := roundTrip(t, p, tc.want)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !sameIntent(p, tc.want, got) {
			t.Fatalf("%s: translated choices %v announce %v, want %v / %v",
				tc.name, got.Choices, got.Announce, tc.want.Choices, tc.want.Announce)
		}
	}
	concede := decision.Intent{Seq: 9, Player: 0, Choices: []int{2}}
	msg, err := ResponseForIntent(p, concede)
	if err != nil {
		t.Fatalf("concede: %v", err)
	}
	raw, err := mb.Encode(msg)
	if err != nil {
		t.Fatalf("concede encode: %v", err)
	}
	var wireResp mb.ClientMessage
	if _, err := mb.Decode(raw, &wireResp); err != nil {
		t.Fatalf("concede decode: %v", err)
	}
	outcome := reachTranslator(t).TranslateResponse(wireResp, p, 0)
	if outcome.Err != nil || outcome.Intent == nil {
		t.Fatalf("concede directive did not translate to a queued intent: err=%v intent=%v", outcome.Err, outcome.Intent)
	}
	if len(outcome.Intent.Choices) != 1 || outcome.Intent.Choices[0] != 2 {
		t.Fatalf("concede translated to choices %v, want [2]", outcome.Intent.Choices)
	}
}

// TestResponseForIntentScryPileB pins the scry inverse with an explicit pile
// B order (Intent.Rest): both piles round-trip to the identical choices and
// rest.
func TestResponseForIntentScryPileB(t *testing.T) {
	v := reachView()
	d := &decision.Decision{Seq: 11, Player: 0, Kind: decision.KArrange, Min: 0, Max: 2, Restable: true,
		Prompt: "Scry 2", Options: []decision.Option{
			{Index: 0, Kind: "bottom", Label: "A", Obj: 1},
			{Index: 1, Kind: "bottom", Label: "B", Obj: 2},
			{Index: 2, Kind: "bottom", Label: "C", Obj: 3},
		}}
	p := reachPending(t, d, v)
	want := decision.Intent{Seq: 11, Player: 0, Choices: []int{1, 2}, Rest: []int{0}}
	got, err := roundTrip(t, p, want)
	if err != nil {
		t.Fatalf("scry: %v", err)
	}
	if len(got.Choices) != 2 || got.Choices[0] != 1 || got.Choices[1] != 2 || len(got.Rest) != 1 || got.Rest[0] != 0 {
		t.Fatalf("scry translated to choices %v rest %v, want [1 2] / [0]", got.Choices, got.Rest)
	}
}

// TestResponseForIntentTriggerOrderDirection pins the reorder inverse's
// KTriggerOrder direction flip: the want placement order round-trips through
// the reversed wire list back to the identical choices.
func TestResponseForIntentTriggerOrderDirection(t *testing.T) {
	v := reachView()
	d := &decision.Decision{Seq: 13, Player: 0, Kind: decision.KTriggerOrder, Min: 2, Max: 2, Prompt: "Order",
		Options: []decision.Option{
			{Index: 0, Kind: "trigger", Label: "T1", Obj: 1},
			{Index: 1, Kind: "trigger", Label: "T2", Obj: 2},
		}}
	p := reachPending(t, d, v)
	want := decision.Intent{Seq: 13, Player: 0, Choices: []int{0, 1}}
	got, err := roundTrip(t, p, want)
	if err != nil {
		t.Fatalf("trigger order: %v", err)
	}
	if len(got.Choices) != 2 || got.Choices[0] != 0 || got.Choices[1] != 1 {
		t.Fatalf("trigger order translated to choices %v, want [0 1]", got.Choices)
	}
}

// TestResponseForIntentCombatAssignments pins the combat inverse for both
// halves: one declared attacker at a chosen target and one declared blocker
// at a chosen attacker, each round-tripping to the identical single-pick
// intent. The 20-game census never posed a declareBlockers decision (no
// blockers: key appears in its tally), so the blockers half is pinned here
// instead of left to luck.
func TestResponseForIntentCombatAssignments(t *testing.T) {
	v := reachView()
	// Attackers: Bear o2 attacks Bob (opt.Player 1); the prompt advertises
	// o2 with Bob's player id among its valid targets.
	da := &decision.Decision{Seq: 15, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1, Prompt: "Attack?",
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Label: "Attack Bob", Obj: 2, Player: 1},
		}}
	pa := reachPending(t, da, v)
	inA, ok := pa.Prompt.Input.Value.(mb.ChooseAttackersInput)
	if !ok {
		t.Fatalf("precondition: attackers prompt is %T", pa.Prompt.Input.Value)
	}
	if !attackerAdvertised(inA.Attackers, "o2", "player-1") {
		t.Fatalf("precondition: the prompt does not advertise o2 -> player-1: %+v", inA.Attackers)
	}
	got, err := roundTrip(t, pa, decision.Intent{Seq: 15, Player: 0, Choices: []int{0}})
	if err != nil {
		t.Fatalf("attackers: %v", err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("attackers translated to choices %v, want [0]", got.Choices)
	}
	// Blockers: Bear o3 blocks the attacking Bear o2; the prompt advertises
	// o2 with o3 among its valid blockers.
	db := &decision.Decision{Seq: 17, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 1, Prompt: "Block?",
		Options: []decision.Option{
			{Index: 0, Kind: "blocker", Label: "Block", Obj: 3, Attacker: 2},
		}}
	pb := reachPending(t, db, v)
	inB, ok := pb.Prompt.Input.Value.(mb.ChooseBlockersInput)
	if !ok {
		t.Fatalf("precondition: blockers prompt is %T", pb.Prompt.Input.Value)
	}
	if !blockerAdvertised(inB.Attackers, "o3", "o2") {
		t.Fatalf("precondition: the prompt does not advertise o3 blocking o2: %+v", inB.Attackers)
	}
	got, err = roundTrip(t, pb, decision.Intent{Seq: 17, Player: 1, Choices: []int{0}})
	if err != nil {
		t.Fatalf("blockers: %v", err)
	}
	if len(got.Choices) != 1 || got.Choices[0] != 0 {
		t.Fatalf("blockers translated to choices %v, want [0]", got.Choices)
	}
}

// playReachGame plays one game at the given seat count and seed through
// ReachSeats (first-legal answers, sweep after every answer).
func playReachGame(t *testing.T, reg *cards.Registry, seats int, seed uint64, client *MockClient, r *Reach) {
	t.Helper()
	names := testutil.LegacyDeckNames()
	playerNames := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := 0; i < seats; i++ {
		playerNames[i] = names[(int(seed)+i)%len(names)]
		decks[i] = testutil.RepoDeck(t, reg, playerNames[i])
	}
	cfg := rules.Config{Seed: seed, Names: playerNames, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}
	seats_ := make([]seat.Seat, seats)
	for i := range seats_ {
		base := NewTranslatingSeat("reach", int64(seed), client, nil)
		seats_[i] = NewReachSeat(base, r, seed*10+uint64(i)+1)
	}
	outcome, _, err := bench.PlayGame(cfg, seats_, censusMaxTurns, censusMaxIntents, bench.Hooks{})
	if err != nil {
		t.Fatalf("seats=%d seed=%d: PlayGame: %v", seats, seed, err)
	}
	r.addGame()
	if bench.IsAbort(outcome.StallOn) {
		t.Fatalf("seats=%d seed=%d: engine abort (%s): %s", seats, seed, outcome.StallOn, outcome.Livelock)
	}
}

// TestProbeReachDetectsSameObjCollapse proves the census machinery can
// fail: two options that share one Obj cannot both be selected through the
// wire (the card-id matcher resolves both to the first option), and
// ProbeReach must report the (decision kind, option kind) unreachable --
// while the distinct-Obj control reaches everything. This is the suspected
// class the ticket named (spec gap G-4: two options differing only in cost/kind/group); the 20-game census did not pose one, so the detector is
// pinned here.
func TestProbeReachDetectsSameObjCollapse(t *testing.T) {
	v := reachView()
	mk := func(objs ...state.ObjID) *decision.Decision {
		opts := make([]decision.Option, len(objs))
		for i, id := range objs {
			opts[i] = decision.Option{Index: i, Kind: "card", Label: fmt.Sprintf("Card %d", i), Obj: id}
		}
		return &decision.Decision{Seq: 21, Player: 0, Kind: decision.KChoose, Min: 2, Max: 2, Prompt: "Pick two",
			Options: opts}
	}
	collapsed := mk(2, 2) // two options, one Obj: the same wire card id
	control := mk(2, 3)   // distinct Objs: distinct wire card ids
	// Precondition: the native answer space of BOTH asks accepts the two
	// distinct picks -- the collapse is a wire-side loss, not an engine-side
	// impossibility, so the census would be vacuous if this failed.
	want := decision.Intent{Seq: 21, Player: 0, Choices: []int{0, 1}}
	for name, d := range map[string]*decision.Decision{"collapsed": collapsed, "control": control} {
		if err := d.Validate(want); err != nil {
			t.Fatalf("%s precondition: the two distinct picks are not a legal answer: %v", name, err)
		}
	}
	// The control reaches everything.
	rControl := NewReach()
	ProbeReach(reachTranslator(t), reachPending(t, control, v), rand.New(rand.NewPCG(21, 3)), rControl)
	if keys := rControl.UnreachableKeys(); len(keys) != 0 {
		t.Fatalf("control with distinct Objs should reach every option, got %v (%s)", keys, rControl.ReasonFor(keys[0]))
	}
	// The collapsed pair cannot be selected: the probe must name it.
	r := NewReach()
	ProbeReach(reachTranslator(t), reachPending(t, collapsed, v), rand.New(rand.NewPCG(21, 2)), r)
	keys := r.UnreachableKeys()
	if len(keys) != 1 || keys[0] != "choose:card" {
		t.Fatalf("same-Obj collapse not detected: keys %v", keys)
	}
	reason := r.ReasonFor(keys[0])
	if !strings.Contains(reason, "rejected") && !strings.Contains(reason, "mismatch") {
		t.Fatalf("collapse reason is not a selection failure: %s", reason)
	}
}

// reachAllowlist is the ratchet register: every (decision kind, option kind)
// the census observed as unreachable, with the reason the ManaBrew wire
// cannot express it. A NEW unreachable key fails the census; an entry the
// census no longer observes fails it too (stale), so a translator fix must
// delete its row.
var reachAllowlist = map[string]string{}

// TestManaBrewReachability plays the same 20 repo-deck games as the MB-8
// census and sweeps every posed decision for per-option reachability. Done
// means the log line reports total options tried / unreachable; a non-zero
// unreachable count must be fully allowlisted, and every allowlist entry
// must be observed.
func TestManaBrewReachability(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	r := NewReach()
	client := NewFirstLegalClient()
	for _, seats := range []int{2, 4} {
		for g := 0; g < censusGamesPerSeatCount; g++ {
			seed := uint64(seats)*1000 + uint64(g)
			playReachGame(t, reg, seats, seed, client, r)
		}
	}
	t.Logf("reachability: %s; allowlisted=%d", r.Summary(), len(reachAllowlist))
	t.Logf("reachability per key: %s", r.Detail())
	if r.Games == 0 {
		t.Fatal("reachability recorded zero games -- the .cards corpus is not reachable in this worktree")
	}
	var unallowed, stale []string
	for _, k := range r.UnreachableKeys() {
		if _, ok := reachAllowlist[k]; !ok {
			unallowed = append(unallowed, fmt.Sprintf("%s (%s)", k, r.ReasonFor(k)))
		}
	}
	for k := range reachAllowlist {
		if r.ReasonFor(k) == "" {
			stale = append(stale, k)
		}
	}
	sort.Strings(unallowed)
	sort.Strings(stale)
	if len(unallowed) > 0 || len(stale) > 0 {
		t.Fatalf("reachability ratchet failed: %d unallowlisted unreachable key(s) %v; %d stale allowlist entr(y/ies) %v",
			len(unallowed), unallowed, len(stale), stale)
	}
}
