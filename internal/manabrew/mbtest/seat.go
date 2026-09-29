package mbtest

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/decision"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// Census is the conformance counter set §8 item 4 asks for: posed:<prompt>,
// enumerated:<Kind>:<optkinds>, rejected:<code>, unmapped:<Kind>, and (MBX-7)
// fallback:<Kind> -- a prompt that IS answerable (the generic
// chooseFromSelection over the native option labels, answered by native
// option index) because the ask had no specific mapping. The fallback and
// unmapped counters are deliberately separate: a fallback pose is play, an
// unmapped pose is a deadlock. It is safe for concurrent use so a caller may
// share one Census across seats or games, though the box rules run one game
// at a time regardless.
type Census struct {
	mu         sync.Mutex
	Games      int
	Posed      map[string]int
	Enumerated map[string]int
	Rejected   map[string]int
	Unmapped   map[string]int
	Fallback   map[string]int
}

// NewCensus returns an empty Census.
func NewCensus() *Census {
	return &Census{
		Posed:      map[string]int{},
		Enumerated: map[string]int{},
		Rejected:   map[string]int{},
		Unmapped:   map[string]int{},
		Fallback:   map[string]int{},
	}
}

func (c *Census) AddGame() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.Games++
	c.mu.Unlock()
}

func (c *Census) addPosed(promptType string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.Posed[promptType]++
	c.mu.Unlock()
}

func (c *Census) addEnumerated(kind decision.Kind, options []decision.Option) {
	if c == nil {
		return
	}
	seen := map[string]bool{}
	kinds := make([]string, 0, 4)
	for _, o := range options {
		if !seen[o.Kind] {
			seen[o.Kind] = true
			kinds = append(kinds, o.Kind)
		}
	}
	sort.Strings(kinds)
	key := string(kind) + ":" + strings.Join(kinds, ",")
	c.mu.Lock()
	c.Enumerated[key]++
	c.mu.Unlock()
}

func (c *Census) addRejected(code mb.ErrorCode) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.Rejected[string(code)]++
	c.mu.Unlock()
}

func (c *Census) addUnmapped(kind decision.Kind) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.Unmapped[string(kind)]++
	c.mu.Unlock()
}

func (c *Census) addFallback(kind decision.Kind) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.Fallback[string(kind)]++
	c.mu.Unlock()
}

// TotalUnmapped sums every unmapped:<Kind> count.
func (c *Census) TotalUnmapped() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.Unmapped {
		n += v
	}
	return n
}

// TotalFallback sums every fallback:<Kind> count (MBX-7): prompts that were
// answerable only through the generic chooseFromSelection fallback. A
// non-zero count is not a failure -- it is the register that keeps the
// class visible after promptChoose stopped erroring on an unmapped shape --
// but a test that wants a card's specific prompt must assert it separately
// (e.g. via Posed or Enumerated).
func (c *Census) TotalFallback() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.Fallback {
		n += v
	}
	return n
}

// TotalRejected sums every rejected:<code> count.
func (c *Census) TotalRejected() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.Rejected {
		n += v
	}
	return n
}

// TotalPosed sums every posed:<prompt> count.
func (c *Census) TotalPosed() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.Posed {
		n += v
	}
	return n
}

// Summary renders a one-line, deterministic (sorted-key) report for a test
// log line -- never ranged over map order for anything that could reach an
// assertion, only for a human-readable log tail.
func (c *Census) Summary() string {
	if c == nil {
		return "census: <nil>"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return fmt.Sprintf("games=%d posed=%d enumerated=%d rejected=%d unmapped=%d fallback=%d",
		c.Games, sumMap(c.Posed), sumMap(c.Enumerated), sumMap(c.Rejected), sumMap(c.Unmapped), sumMap(c.Fallback))
}

func sumMap(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// Detail renders the whole census as a deterministic (sorted-key) block: the
// Summary line, then one line per bucket -- "posed <promptType> <n>",
// "enumerated <Kind:optkinds> <n>", "rejected <code> <n>", "unmapped <Kind>
// <n>" -- in that fixed bucket order, keys sorted within a bucket. A
// multi-game run (cmd/cardfuzz -manabrew) folds one shared Census across
// every seat and prints this block once at the end.
//
// Reading note for an overnight-sweep reader: a decision is counted in
// "enumerated" at Decide entry, before its prompt is built -- one that fails
// translation is enumerated but never posed, so enumerated may exceed posed.
func (c *Census) Detail() string {
	if c == nil {
		return "census: <nil>\n"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "census: games=%d posed=%d enumerated=%d rejected=%d unmapped=%d\n",
		c.Games, sumMap(c.Posed), sumMap(c.Enumerated), sumMap(c.Rejected), sumMap(c.Unmapped))
	write := func(name string, m map[string]int) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %s %d\n", name, k, m[k])
		}
	}
	write("posed", c.Posed)
	write("enumerated", c.Enumerated)
	write("rejected", c.Rejected)
	write("unmapped", c.Unmapped)
	return b.String()
}

// optionKinds is a diagnostic helper: the distinct Option.Kind values a
// decision carries, in first-occurrence order.
func optionKinds(opts []decision.Option) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range opts {
		if !seen[o.Kind] {
			seen[o.Kind] = true
			out = append(out, o.Kind)
		}
	}
	return out
}

// TranslatingSeat is a seat.Seat that answers every engine decision by
// routing it through the ManaBrew mapping in-process: decision -> Translator
// .Prompt -> MockClient.Answer -> Translator.TranslateResponse -> intent
// (scoping spec §5.1's "used by MB-8" line). It never touches HTTP or a
// host.Session; it is the pure in-process bijection the replay-equivalence
// proof (§8 item 5) needs.
//
// A Census, when set, is updated on every decision; nil skips counting
// without changing behaviour.
type TranslatingSeat struct {
	Translator *manabrew.Translator
	Client     *MockClient
	Table      string
	Census     *Census
	// Pick, when non-nil, is consulted before the client (see PickPolicy).
	Pick PickPolicy
}

// NewTranslatingSeat builds a TranslatingSeat over a fresh Translator for
// (table, match). client and census may be shared across every seat of one
// game; census may be nil.
func NewTranslatingSeat(table string, match int64, client *MockClient, census *Census) *TranslatingSeat {
	return &TranslatingSeat{
		Translator: manabrew.New(table, match, nil),
		Client:     client,
		Table:      table,
		Census:     census,
	}
}

// PickPolicy is the optional seat-level answer policy a vote fixture needs
// (MBX-7): it is consulted at each Decided decision BEFORE the mock client,
// and returns the prompt output to answer (exactly the wire shape a client
// response's PromptOutput carries -- an act on "opt-<i>", a pass output) or
// ok=false to hand the decision to the client as usual. The policy sees the
// view and the decision -- MORE than a real client sees (the decision is not
// on the wire), which is why it is a fixture tool, not a client behaviour:
// the census games that pin card behaviour need a seat that can build the
// mana for a spell and cast it, which the wire-only mock cannot express (the
// priority prompt carries no phase, so a client cannot tell a wasted upkeep
// activation from a needed main-phase one). A pick is never the pass/concede
// OPTION -- those are answered as the pass output/directive (§6.3), which the
// policy reaches with a PassOutput pick, never with an act.
type PickPolicy func(v view.View, d *decision.Decision) (mb.PromptOutputValue, bool)

// Decide implements seat.Seat. It builds the ManaBrew prompt for d, hands it
// to the mock client (which sees only the PromptMessage, exactly as a real
// client would), and translates the answer back into an intent. A dispatch
// miss (ErrUnmapped) or a translated rejection is returned as an error --
// TestManaBrewCensusNoUnmapped and TestFirstLegalNeverRefused are exactly
// the assertions that no such error occurs across a real game.
func (s *TranslatingSeat) Decide(_ context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	s.Census.addEnumerated(d.Kind, d.Options)
	msg, fellBack, err := s.Translator.PromptFlagged(&d, &v)
	if err != nil {
		s.Census.addUnmapped(d.Kind)
		return decision.Intent{}, fmt.Errorf("mbtest: prompt for %s (seq %d, resumeKind=%q, nopts=%d, opts=%v): %w",
			d.Kind, d.Seq, d.ResumeKind, len(d.Options), optionKinds(d.Options), err)
	}
	if fellBack {
		// MBX-7: the ask had no specific mapping, so the prompt is the
		// generic chooseFromSelection fallback. It is answerable (that is the
		// point) -- count it as fallback, NOT as unmapped, so the census keeps
		// showing the class without ever producing an unanswerable prompt.
		s.Census.addFallback(d.Kind)
	}
	s.Census.addPosed(msg.Input.Value.PromptType())
	if s.Pick != nil {
		if out, ok := s.Pick(v, &d); ok {
			return s.decidePick(v, d, msg, out)
		}
	}
	resp, err := s.Client.Answer(msg)
	if err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: mock client could not answer %s prompt (seq %d): %w",
			msg.Input.Value.PromptType(), d.Seq, err)
	}
	// Round-trip the client's answer through the real wire codec (MB-11's
	// mb.Encode/mb.Decode) before translating it: the mock client builds an
	// mb.ClientMessage as a Go value (§5.1's in-process contract -- HTTP is
	// out of this ticket's scope), but sending it through Encode then Decode
	// here means TranslateResponse sees exactly what a real transport would
	// hand it off the wire, not a value the mock happened to construct by
	// hand. A codec bug (a lossy field, a union that decodes to the wrong
	// concrete type) surfaces as a translation failure here rather than
	// staying invisible because the in-process path skipped serialisation
	// entirely.
	raw, err := mb.Encode(resp)
	if err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: wire-encoding the %s response (seq %d): %w",
			msg.Input.Value.PromptType(), d.Seq, err)
	}
	return s.decodedIntent(msg, &d, v, raw)
}

// decidePick answers one decision with the policy's output, flowing through
// exactly the path a client answer takes (the same mb.Encode/Decode
// round-trip and TranslateResponse -- census pose already recorded by
// Decide). A pass pick is the wire pass output, not an act on the pass
// option (§6.3) -- the translator maps it itself; an act naming the
// pass/concede option is a fixture bug, failed loudly here rather than
// surfacing as a rejected answer.
func (s *TranslatingSeat) decidePick(v view.View, d decision.Decision, msg mb.PromptMessage, out mb.PromptOutputValue) (decision.Intent, error) {
	if act, isAct := out.(mb.ActOutput); isAct {
		if rest, ok := strings.CutPrefix(act.ActionID, "opt-"); ok {
			idx, aerr := strconv.Atoi(rest)
			if aerr != nil || idx < 0 || idx >= len(d.Options) || d.Options[idx].Kind == "pass" || d.Options[idx].Kind == "concede" {
				return decision.Intent{}, fmt.Errorf("mbtest: pick policy chose action %q, which is not an act option (kinds %v, seq %d)",
					act.ActionID, optionKinds(d.Options), d.Seq)
			}
		}
	}
	resp := mb.ClientMessage{Value: mb.ClientResponse{
		Kind:     "response",
		PromptID: msg.PromptID,
		Action:   mb.PromptOutput{Type: out.OutputType(), Output: mb.PromptOutputData{Value: out}},
	}}
	raw, err := mb.Encode(resp)
	if err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: wire-encoding the policy answer (seq %d): %w", d.Seq, err)
	}
	return s.decodedIntent(msg, &d, v, raw)
}

// decodedIntent is the shared tail of Decide and decideAct: mb.Decode the
// raw wire bytes and hand them to TranslateResponse, rejecting on error.
func (s *TranslatingSeat) decodedIntent(msg mb.PromptMessage, d *decision.Decision, v view.View, raw []byte) (decision.Intent, error) {
	var wireResp mb.ClientMessage
	if _, err := mb.Decode(raw, &wireResp); err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: wire-decoding the %s response (seq %d): %w",
			msg.Input.Value.PromptType(), d.Seq, err)
	}
	pending := &manabrew.Pending{Prompt: msg, Decision: d, View: v}
	outcome := s.Translator.TranslateResponse(wireResp, pending, d.Player)
	if outcome.Err != nil {
		s.Census.addRejected(outcome.Err.Code)
		return decision.Intent{}, fmt.Errorf("mbtest: %s prompt (seq %d) rejected: %s (%s) [kind=%s resumeKind=%q opts=%v]",
			msg.Input.Value.PromptType(), d.Seq, outcome.Err.Code, outcome.Err.Message, d.Kind, d.ResumeKind, optionKinds(d.Options))
	}
	if outcome.Undo != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: mock client triggered an undo, which it never sends")
	}
	if outcome.Queued {
		return decision.Intent{}, fmt.Errorf("mbtest: response queued with no intent (concede directive path, which the mock client never sends)")
	}
	if outcome.Intent == nil {
		return decision.Intent{}, fmt.Errorf("mbtest: TranslateResponse returned neither an intent nor an error")
	}
	return *outcome.Intent, nil
}
