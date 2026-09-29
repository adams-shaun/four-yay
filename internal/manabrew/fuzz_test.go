//go:build fuzz

package manabrew

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// MB-7: response fuzzing (scoping spec §9). Fuzz tests are opt-in
// (docs/agents/do-not.md): this file is excluded from the default suite and
// every pipeline gate, and runs only via `make fuzz` or `go test -tags fuzz`.
//
// FuzzTranslateResponse exercises Translator.TranslateResponse (errors.go),
// the one entry point that maps a ManaBrew client->engine message against a
// Pending prompt into an Outcome. It is fed mb.ClientResponse/ClientDirective
// values built directly from fuzzer-supplied primitives (string, int, bool),
// not decoded from fuzzer-supplied JSON bytes: protocol/manabrew's own
// strict decoders currently hand PromptOutputData a pointer-typed
// PromptOutputValue (a bug MB-11 is fixing concurrently, in a package this
// ticket must not touch), so every message decoded off the wire today fails
// translateResponse's very first type-switch case before reaching a single
// per-kind parser. Fuzzing that path would only rediscover the MB-11 bug,
// not exercise this package's own mapping. Constructing the union values
// directly reaches the real parsers (parseChooseNumber, parseChooseCards,
// parseChooseColor, parseChooseBoolean, parseChooseFromSelection,
// parseArrangeScry, parseArrangeReorder/parseTriggerOrder,
// parsePayManaCost, parseAttackers/parseBlockers, parseMulligan*,
// parseBoardTargets, parseChooseAction*), each of which parses attacker-
// supplied strings (ManaBrew ids) and slices with no schema-level bound.
//
// The invariant (ticket MB-7):
//  1. TranslateResponse never panics, on any Pending (one representative per
//     decision.Kind, covering every wire prompt type MB-4 through MB-6
//     wired) and any fuzzer-shaped response or directive.
//  2. Whenever it returns a non-error Outcome, that Outcome's Intent passes
//     decision.Validate against the SAME pending decision it answers -- the
//     one property that would let a malformed or adversarial client
//     response reach events.Apply as though it were a legal answer.
//
// The seed corpus below is drawn from the recorded fixtures MB-4 through
// MB-6 already committed (errors_test.go's smallView/battleView,
// prompt_arrange_test.go's arrangeView, and the option/id shapes those
// files' Test* functions send through TranslateResponse): real actionIds
// ("opt-0", "opt-1"), real card/player ids ("o1", "o2", "player-0",
// "player-1"), the payment-action prefix ("pay-x"), and the boundary and
// adversarial strings those tests already prove are rejected without a
// panic (an out-of-range numeric id, an empty string, a value with the
// wrong prefix, a lookalike id for an object that was never offered).
var fuzzSeedStrings = []string{
	"opt-0", "opt-1", "opt-2", "opt-99", "opt--1", "opt-",
	"o1", "o2", "o99", "o-1", "o",
	"s9", "s1",
	"player-0", "player-1", "player-99", "player-",
	"pay-x", "pay-nope", "pay-",
	"h-hand-0-0",
	"", " ", "\x00", "opt-0\x00", "opt-00", "opt-0x",
	"W", "U", "B", "R", "G", "C", "notacolor",
}

var fuzzSeedInts = []int{-1, 0, 1, 2, 3, 9, 99, -99}

func FuzzTranslateResponse(f *testing.F) {
	for _, s := range fuzzSeedStrings {
		for _, n := range fuzzSeedInts {
			f.Add(s, n, true)
			f.Add(s, n, false)
		}
	}

	tr := New("table", 2, nil)
	pends := fuzzPendings(tr)

	f.Fuzz(func(t *testing.T, s string, n int, flag bool) {
		outs := fuzzOutputs(s, n, flag)
		for _, p := range pends {
			for _, out := range outs {
				assertTranslateResponseSafe(t, tr, p, respFor(p, out))
			}
			// The out-of-band concede directive takes no fuzzer-shaped
			// payload, but every pending shape (including the ones that
			// have no priority decision at all, so the directive queues
			// rather than answering) must stay panic-free and, when it
			// does produce an intent, still validate.
			dir := mb.ClientMessage{Value: mb.ClientDirective{Kind: "directive", Directive: mb.DirectiveInput{Type: "concede"}}}
			assertTranslateResponseSafe(t, tr, p, dir)
		}
	})
}

// assertTranslateResponseSafe calls TranslateResponse under a recover so a
// panic is reported as a normal fuzz failure (with the crashing input kept
// as a corpus seed) rather than crashing the whole fuzz run, then checks the
// Intent invariant on whatever Outcome comes back.
func assertTranslateResponseSafe(t *testing.T, tr *Translator, p *Pending, msg mb.ClientMessage) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateResponse panicked for pending kind %s: %v", p.Decision.Kind, r)
		}
	}()
	o := tr.TranslateResponse(msg, p, p.Decision.Player)
	if o.Intent == nil {
		return
	}
	if err := p.Decision.Validate(*o.Intent); err != nil {
		t.Fatalf("TranslateResponse (pending kind %s) returned an Intent that fails Decision.Validate: %v (intent=%+v)",
			p.Decision.Kind, err, *o.Intent)
	}
}

// fuzzOutputs builds one instance of every PromptOutputValue shape
// translateResponse's switch routes on, each seeded from the fuzzer's own
// primitives so every parser sees fuzzer-controlled ids, indices, slice
// contents and boolean flags.
func fuzzOutputs(s string, n int, flag bool) []mb.PromptOutputValue {
	idxs := []int{n, n + 1, -n}
	two := n
	return []mb.PromptOutputValue{
		mb.ActOutput{ActionID: s},
		mb.PassOutput{ExhaustStack: flag},
		mb.PassOutput{Until: &mb.PassUntil{PlayerID: s, Phase: mb.StepKind(s)}, ExhaustStack: flag},
		mb.RestoreSnapshotOutput{CheckpointID: int64(n)},
		mb.PayOutput{Auto: flag},
		mb.CancelOutput{},
		mb.NumberDecision{ChosenNumber: &two},
		mb.NumberDecision{},
		mb.ChooseCardsDecision{ChosenCardIDs: []string{s, s}},
		mb.ColorDecision{ChosenColors: map[string]int{s: n}},
		mb.BooleanDecision{Value: flag},
		mb.SelectionDecision{ChosenIndices: idxs},
		mb.ScryDecision{ZoneCardIDs: [][]string{{s}, {s, s}}},
		mb.ScryDecision{},
		mb.ReorderDecision{OrderedIDs: []string{s, s}},
		mb.MulliganDecision{Keep: flag},
		mb.MulliganPutBackDecision{CardIDs: []string{s}},
		mb.DeclareAttackersDecision{Assignments: []mb.AttackerAssignment{{AttackerID: s, TargetID: s}}},
		mb.DeclareBlockersDecision{Assignments: []mb.BlockerAssignment{{BlockerID: s, AttackerID: s}}},
		mb.BoardTargetsDecision{Chosen: []mb.TargetRef{{Kind: mb.TargetRefKind(s), ID: s}}},
	}
}

// fuzzPendings builds one Pending per decision.Kind, mirroring
// TestEveryDecisionKindTranslates' representative set (errors_test.go) plus
// the extra KChoose/KModes/KReplacement/KArrange shapes MB-5/MB-6 wired to
// their OWN wire prompt type (chooseNumber, chooseCards, chooseColor,
// chooseBoolean/chooseFromSelection, payManaCost, scry, reorder), so every
// case translateResponse's switch can reach gets a live Pending to fuzz
// against, not just one representative per decision.Kind.
func fuzzPendings(tr *Translator) []*Pending {
	bv := battleView()
	sv := smallView()
	av := arrangeView()

	mk := func(d *decision.Decision) *Pending {
		msg, err := tr.Prompt(d, &bv)
		if err != nil {
			panic("fuzzPendings: representative decision must build a prompt: " + err.Error())
		}
		return &Pending{Prompt: msg, Decision: d, View: bv}
	}

	var pends []*Pending

	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "cast", Label: "Cast Shock", Obj: 2}, {Index: 1, Kind: "pass", Label: "Pass priority"},
		{Index: 2, Kind: "concede", Label: "Concede"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KTarget, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "permanent", Label: "Bear", Obj: 2}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Label: "Attack Bob", Obj: 2, Player: 1}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "block", Label: "Bear blocks", Obj: 2, Attacker: 1}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "keep", Label: "keep"}, {Index: 1, Kind: "mulligan", Label: "mulligan"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KModes, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "mode", Label: "Mode A"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KTriggerOrder, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "trigger", Label: "Trigger A"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KTriggerOptional, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "trigger", Label: "Trigger A"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KCommanderZone, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "commander_zone", Label: "Command zone"}, {Index: 1, Kind: "leave", Label: "Leave"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "choice", Label: "Choice A"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KReplacement, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "replacement", Label: "Replace A"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KArrange, Min: 1, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "bottom", Label: "A"}, {Index: 1, Kind: "bottom", Label: "B"}}}))
	pends = append(pends, mk(&decision.Decision{Seq: 1, Player: 1, Kind: decision.KStartingPlayer, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "player", Label: "Alice", Player: 0}}}))

	// The extra wire shapes: chooseNumber (contiguous "x"), chooseCards
	// (discard), chooseColor (repeatable mana), chooseBoolean (yes/no,
	// asunblocked), the KArrange scry split and single-list reorder, the
	// KTriggerOrder reorder, and both payManaCost windows (announced and
	// legacy) -- built against smallView/arrangeView the way the MB-4/5/6
	// tests already do (errors_test.go, prompt_arrange_test.go).
	number := &decision.Decision{Seq: 40, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Choose X",
		Options: []decision.Option{
			{Index: 0, Kind: "x", Label: "X = 0", Amount: 0},
			{Index: 1, Kind: "x", Label: "X = 1", Amount: 1},
			{Index: 2, Kind: "x", Label: "X = 2", Amount: 2},
		}}
	pends = append(pends, pendingFor(number, sv))

	discard := &decision.Decision{Seq: 41, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Discard",
		Options: []decision.Option{
			{Index: 0, Kind: "discard", Label: "Island", Obj: 1},
			{Index: 1, Kind: "discard", Label: "Bear", Obj: 2},
		}}
	pends = append(pends, pendingFor(discard, sv))

	mana := &decision.Decision{Seq: 42, Player: 0, Kind: decision.KChoose, Min: 2, Max: 2, Repeatable: true, Prompt: "Add mana",
		Options: []decision.Option{
			{Index: 0, Kind: "mana", Label: "Add W", ManaSymbol: "W"},
			{Index: 1, Kind: "mana", Label: "Add U", ManaSymbol: "U"},
		}}
	pends = append(pends, pendingFor(mana, sv))

	yesNo := &decision.Decision{Seq: 43, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Confirm?",
		Options: []decision.Option{{Index: 0, Kind: "yes", Label: "Yes"}, {Index: 1, Kind: "no", Label: "No"}}}
	pends = append(pends, pendingFor(yesNo, sv))

	asUnblocked := &decision.Decision{Seq: 44, Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Assign",
		Options: []decision.Option{
			{Index: 0, Kind: "asunblocked", Label: "assign normally (blocked)"},
			{Index: 1, Kind: "asunblocked", Label: "assign as though not blocked"},
		}}
	pends = append(pends, pendingFor(asUnblocked, sv))

	scry := &decision.Decision{Seq: 45, Player: 0, Kind: decision.KArrange, Min: 0, Max: 2, Prompt: "Scry 2",
		Options: []decision.Option{
			{Index: 0, Kind: "bottom", Label: "Island", Obj: 1},
			{Index: 1, Kind: "bottom", Label: "Bear", Obj: 2},
		}}
	pends = append(pends, pendingFor(scry, av))

	reorderArrange := &decision.Decision{Seq: 46, Player: 0, Kind: decision.KArrange, Min: 2, Max: 2, Prompt: "Rearrange",
		Options: []decision.Option{
			{Index: 0, Kind: "top", Label: "Island", Obj: 1},
			{Index: 1, Kind: "top", Label: "Bear", Obj: 2},
		}}
	pends = append(pends, pendingFor(reorderArrange, av))

	triggerOrder := &decision.Decision{Seq: 47, Player: 0, Kind: decision.KTriggerOrder, Min: 2, Max: 2, Prompt: "Order your triggers",
		Options: []decision.Option{
			{Index: 0, Kind: "trigger", Label: "Alpha"},
			{Index: 1, Kind: "trigger", Label: "Beta"},
		}}
	pends = append(pends, pendingFor(triggerOrder, av))

	payAnnounce := &decision.Decision{Seq: 48, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		Prompt:      "Pay for Bear",
		ManaPayment: &decision.ManaPaymentWindow{Card: 2, Cost: decision.PaymentCost{Generic: 1}, Owed: decision.PaymentCost{Generic: 1}},
		Options: []decision.Option{
			{Index: 0, Kind: "mana", Obj: 1, Ability: 0, ManaSymbol: "W", Label: "Add {W}"},
			{Index: 1, Kind: decision.OptAutoFill, Label: "Auto-fill: tap Island"},
			{Index: 2, Kind: decision.OptCancelCast, Label: "Cancel cast"},
		}}
	pends = append(pends, pendingFor(payAnnounce, sv))

	payLegacy := &decision.Decision{Seq: 49, Player: 1, Kind: decision.KChoose, Min: 1, Max: 1, Source: 2,
		Prompt: "Activate mana abilities to pay for Bear",
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1, Label: "Activate Island for mana"},
			{Index: 1, Kind: "done", Label: "Done"},
		}}
	pends = append(pends, pendingFor(payLegacy, sv))

	return pends
}
