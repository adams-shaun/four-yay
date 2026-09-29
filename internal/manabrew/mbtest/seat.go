package mbtest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/decision"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// Census is the conformance counter set §8 item 4 asks for: posed:<prompt>,
// enumerated:<Kind>:<optkinds>, rejected:<code>, unmapped:<Kind>. It is safe
// for concurrent use so a caller may share one Census across seats or games,
// though the box rules run one game at a time regardless.
type Census struct {
	mu         sync.Mutex
	Games      int
	Posed      map[string]int
	Enumerated map[string]int
	Rejected   map[string]int
	Unmapped   map[string]int
}

// NewCensus returns an empty Census.
func NewCensus() *Census {
	return &Census{
		Posed:      map[string]int{},
		Enumerated: map[string]int{},
		Rejected:   map[string]int{},
		Unmapped:   map[string]int{},
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
	return fmt.Sprintf("games=%d posed=%d enumerated=%d rejected=%d unmapped=%d",
		c.Games, sumMap(c.Posed), sumMap(c.Enumerated), sumMap(c.Rejected), sumMap(c.Unmapped))
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

// Decide implements seat.Seat. It builds the ManaBrew prompt for d, hands it
// to the mock client (which sees only the PromptMessage, exactly as a real
// client would), and translates the answer back into an intent. A dispatch
// miss (ErrUnmapped) or a translated rejection is returned as an error --
// TestManaBrewCensusNoUnmapped and TestFirstLegalNeverRefused are exactly
// the assertions that no such error occurs across a real game.
func (s *TranslatingSeat) Decide(_ context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	s.Census.addEnumerated(d.Kind, d.Options)
	msg, err := s.Translator.Prompt(&d, &v)
	if err != nil {
		s.Census.addUnmapped(d.Kind)
		return decision.Intent{}, fmt.Errorf("mbtest: prompt for %s (seq %d, resumeKind=%q, nopts=%d, opts=%v): %w",
			d.Kind, d.Seq, d.ResumeKind, len(d.Options), optionKinds(d.Options), err)
	}
	s.Census.addPosed(msg.Input.Value.PromptType())
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
	var wireResp mb.ClientMessage
	if _, err := mb.Decode(raw, &wireResp); err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: wire-decoding the %s response (seq %d): %w",
			msg.Input.Value.PromptType(), d.Seq, err)
	}
	resp = wireResp
	pending := &manabrew.Pending{Prompt: msg, Decision: &d, View: v}
	outcome := s.Translator.TranslateResponse(resp, pending, d.Player)
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
