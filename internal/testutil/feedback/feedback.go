// Package feedback loads a feedback snapshot directory — the match.json /
// log.json (plus optional view.json / report.json) files the server's
// feedback capture (task fbrepro1) writes beside a bug report — back into
// the (events.Log, rules.Config) pair a deterministic replay needs, and
// drives that replay to a chosen intent.
//
// It exists so turning a report into a test case is one command (cmd/repro)
// and one call in the resulting test (EngineAt): a triage or implementer
// seat starts from the exact board the reporter saw, not from a guessed
// one. The contract it serves is replay's: (Config, Log) together, never
// the Log alone — the log cannot recover deck contents or the token table,
// so match.json's recorded card-name lists and token scripts are rebuilt
// into a Config through the same corpus the live match loaded its cards
// from.
//
// This package sits at the far end of the dependency order (it imports
// cards, state, events, rules and replay), one step before cmd/*: nothing
// in the engine may import it. It mirrors the snapshot's JSON shapes
// locally rather than importing host, so a package anywhere in the tree —
// including a generated repro test inside an early package — can depend on
// it without dragging the host tier in. The mirror is pinned by host's own
// marshal-shape test on one side and by cmd/repro's end-to-end tests on the
// other, which replay a snapshot produced by the live capture code; a field
// either side forgets fails there instead of silently replaying a
// different match.
package feedback

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// matchJSON is match.json's shape, mirroring host.FeedbackMatch's tags
// field for field. Only the fields a replay's Config needs are read here,
// but every field host.FeedbackMatch marshals is declared, so an
// unrecognised key is at worst ignored, never a decode error — and the
// end-to-end tests replay a snapshot host actually produced, which is what
// catches drift.
type matchJSON struct {
	Table        string            `json:"table"`
	Match        int               `json:"match"`
	Seed         uint64            `json:"seed"`
	Names        []string          `json:"names"`
	PlayerNames  []string          `json:"player_names,omitempty"`
	Decks        []string          `json:"decks"`
	DeckCards    [][]string        `json:"deck_cards"`
	Sideboards   [][]string        `json:"sideboards,omitempty"`
	Spectator    string            `json:"spectator"`
	State        string            `json:"state"`
	Result       string            `json:"result,omitempty"`
	Winner       *uint8            `json:"winner"`
	Head         string            `json:"head,omitempty"`
	Events       int               `json:"events"`
	Turns        int32             `json:"turns"`
	Reason       string            `json:"reason,omitempty"`
	Mulligans    int               `json:"mulligans,omitempty"`
	Format       string            `json:"format,omitempty"`
	StartingLife int32             `json:"starting_life,omitempty"`
	Commanders   [][]int           `json:"commanders,omitempty"`
	Tokens       map[string]string `json:"tokens,omitempty"`
	TokensUnread []string          `json:"tokens_unread,omitempty"`
	// NameUniverse is the match MODE bit: true when the live match was
	// played with a card-name universe, so its NameCard effects posed real
	// asks. config() must restore it or the rebuilt engine takes the legacy
	// no-ask path and every recorded name intent misaligns. Absent (a
	// pre-feature capture) is the legacy path, unchanged.
	NameUniverse bool `json:"name_universe,omitempty"`
	// NameUniverseNames pins the exact ordered labels that match offered.
	// A committed fixture has it stripped for size, the way `tokens` is
	// stripped for licensing; config() then leaves it nil and the engine
	// re-derives the list from the live corpus at genesis, which replays a
	// recorded name choice exactly as long as the corpus pin has not moved
	// (the documented DIVERGED cause, see AGENTS.md).
	NameUniverseNames []string `json:"name_universe_names,omitempty"`
}

// logJSON is log.json's shape, mirroring host.FeedbackLog: the embedded
// events.Log (seed, events, intents) plus the prefix facts. The embedded
// Log decodes straight back into an events.Log — Seed, Events and Intents
// are the replay input; Head is what a replay must reproduce.
type logJSON struct {
	events.Log
	Head        string         `json:"head"`
	IntentCount int            `json:"intent_count"`
	Turn        int32          `json:"turn"`
	Step        string         `json:"step"`
	Priority    state.PlayerID `json:"priority"`
	Active      state.PlayerID `json:"active"`
}

// reportJSON is report.json's shape, mirroring cmd/gorged's report struct.
// Only the snapshot status is load-bearing here (it says whether the
// capture was complete); the rest is carried in Meta for context.
type reportJSON struct {
	Received string `json:"received"`
	Text     string `json:"text"`
	URL      string `json:"url,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
}

// Meta describes the snapshot as loaded: where it came from, the game
// facts log.json recorded at the capture point, and the capture's own
// status. It is provenance and triage context, not replay input — the
// replay input is the returned Log and Config.
type Meta struct {
	// Dir is the snapshot directory as the caller named it; ID is its base
	// name (the feedback report id the server generated).
	Dir, ID string
	// Report is report.json's snapshot status line ("captured: ...",
	// "partial: ...", "unavailable: ..."), empty when the report carries
	// none. A "partial" or "unavailable" snapshot may not replay.
	Report string
	// Text is the reporter's own words, when report.json is present.
	Text string
	// Table and Match name the table and match the snapshot captured.
	Table string
	Match int
	// Head is the chain hash log.json recorded at the capture prefix: what
	// a full replay must reproduce.
	Head string
	// IntentCount is derived from len(Log.Intents), not trusted from the
	// redundant intent_count metadata in log.json; Turn/Step/Priority/Active
	// are the game facts read off the same locked instant the log was copied.
	// Step is the Step's String() spelling.
	IntentCount int
	Turn        int32
	Step        string
	Priority    state.PlayerID
	Active      state.PlayerID
	// Seats is the number of seats the match dealt.
	Seats int
	// HasView is true when the snapshot carries a view.json (the report
	// named a seat and the projection succeeded).
	HasView bool
	// TokensUnread mirrors match.json's tokens_unread: token scripts that
	// could not be read back at capture time. A replay that reaches one of
	// these stems will diverge the same way a missing token table would.
	TokensUnread []string
}

// Load reads a feedback snapshot directory and rebuilds the (Log, Config)
// pair replay needs, resolving the recorded card names and token scripts
// through the repo's own corpus (the same card loader the live match used).
//
// match.json and log.json are required; report.json and view.json are
// optional (a report may name no seat, and older capture shapes may have
// written none). Missing files, unparseable JSON, a card name the corpus
// does not know and a token script that does not compile are errors — a
// repro that silently replayed a different match would be worse than one
// that refuses. The corpus is found through the repo root (.cards beside
// it); a checkout with no .cards/ fails here rather than skipping, because
// a caller of Load asked for a reproduction, not a test harness.
func Load(dir string) (*events.Log, rules.Config, Meta, error) {
	var cfg rules.Config
	matchRaw, err := os.ReadFile(filepath.Join(dir, "match.json"))
	if err != nil {
		return nil, cfg, Meta{}, fmt.Errorf("feedback: %s: %w", dir, err)
	}
	logRaw, err := os.ReadFile(filepath.Join(dir, "log.json"))
	if err != nil {
		return nil, cfg, Meta{}, fmt.Errorf("feedback: %s: %w", dir, err)
	}
	var m matchJSON
	if err := json.Unmarshal(matchRaw, &m); err != nil {
		return nil, cfg, Meta{}, fmt.Errorf("feedback: %s: match.json: %w", dir, err)
	}
	var lg logJSON
	if err := json.Unmarshal(logRaw, &lg); err != nil {
		return nil, cfg, Meta{}, fmt.Errorf("feedback: %s: log.json: %w", dir, err)
	}

	meta := Meta{
		Dir:          dir,
		ID:           filepath.Base(dir),
		Head:         lg.Head,
		IntentCount:  len(lg.Intents),
		Turn:         lg.Turn,
		Step:         lg.Step,
		Priority:     lg.Priority,
		Active:       lg.Active,
		Table:        m.Table,
		Match:        m.Match,
		Seats:        len(m.DeckCards),
		TokensUnread: m.TokensUnread,
	}
	if rep, err := os.ReadFile(filepath.Join(dir, "report.json")); err == nil {
		var r reportJSON
		if err := json.Unmarshal(rep, &r); err == nil {
			meta.Report = r.Snapshot
			meta.Text = r.Text
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "view.json")); err == nil {
		meta.HasView = true
	}

	reg, err := openCorpus()
	if err != nil {
		return nil, cfg, meta, err
	}
	tokens, err := resolveTokens(dir, m, reg)
	if err != nil {
		return nil, cfg, meta, err
	}
	cfg, err = config(m, reg, tokens)
	if err != nil {
		return nil, cfg, meta, err
	}
	return &lg.Log, cfg, meta, nil
}

// config rebuilds a rules.Config from match.json's content: decks from the
// recorded card-name lists (resolved through the registry exactly the way
// deck.File.Resolve resolves a deck file — Lookup normalises the name, so
// the recorded printed name finds its card), the sideboards the same way
// when the capture recorded any (Sideboards are genesis configuration: the
// engine mints their objects before the first event, so a replay without
// them shifts every later object ID), the already-compiled token
// scripts (resolved by resolveTokens — never raw text, which may be
// GPL-3.0), and the format/life/commander/mulligan settings. The seed is
// carried but replay overwrites it with the log's own seed (replay's
// Ruling P5), which is the one that produced the log.
func config(m matchJSON, reg *cards.Registry, tokens map[string]*cards.Card) (rules.Config, error) {
	if len(m.DeckCards) == 0 {
		return rules.Config{}, fmt.Errorf("feedback: match.json records no deck_cards")
	}
	decks := make([][]*cards.Card, len(m.DeckCards))
	for i, names := range m.DeckCards {
		decks[i] = make([]*cards.Card, len(names))
		for j, n := range names {
			c, ok := reg.Lookup(n)
			if !ok {
				return rules.Config{}, fmt.Errorf("feedback: seat %d card %d (%q) is not in the corpus", i, j, n)
			}
			decks[i][j] = c
		}
	}
	var sideboards [][]*cards.Card
	if len(m.Sideboards) > 0 {
		sideboards = make([][]*cards.Card, len(m.Sideboards))
		for i, names := range m.Sideboards {
			sideboards[i] = make([]*cards.Card, len(names))
			for j, n := range names {
				c, ok := reg.Lookup(n)
				if !ok {
					return rules.Config{}, fmt.Errorf("feedback: sideboard %d card %d (%q) is not in the corpus", i, j, n)
				}
				sideboards[i][j] = c
			}
		}
	}
	cfg := rules.Config{
		Seed:        m.Seed,
		Names:       m.Names,
		PlayerNames: m.PlayerNames,
		Decks:       decks,
		Sideboards:  sideboards,
		Mulligans:   m.Mulligans,
		Tokens:      tokens,
	}
	if m.Format == "commander" {
		cfg.Format = rules.FormatCommander
		cfg.StartingLife = m.StartingLife
		cfg.Commanders = m.Commanders
	}
	// The name-card universe is a match MODE, restored the way host's own
	// matchForLog restores it (host/viewat.go): only a capture that records
	// the mode replays with one, so a pre-feature snapshot still takes the
	// legacy no-ask path and reproduces unchanged. With the mode set, the
	// universe itself is the live corpus (the same registry the decks were
	// resolved through above) and the pinned label list overrides the
	// derived one when the capture carried it — a live, uncommitted capture
	// always does; a size-stripped committed fixture does not, and then the
	// engine derives the list from this corpus at genesis.
	if m.NameUniverse {
		cfg.NameUniverse = reg.AllCards()
		cfg.NameUniverseNames = append([]string(nil), m.NameUniverseNames...)
	}
	return cfg, nil
}

// resolveTokens decides which token scripts a Load replays with. Three
// sources, in priority order:
//
//  1. A live, uncommitted capture carries the exact script text in
//     match.json's `tokens` field (host.FeedbackMatch.Tokens): compile it
//     directly. That field is the whole cfg.Tokens the live match was
//     built with, so this reproduces the match exactly.
//  2. A committed fixture has that text stripped out — Forge token scripts
//     are GPL-3.0 and must never be committed — so Load consults the
//     gitignored token sync directory keyed by the fixture id: the exact
//     historical text, preserved for reproducible replay after a corpus
//     pin move.
//  3. When the sync directory is missing (a fresh checkout that never ran
//     the fixture generator), fall back to the whole live corpus token map
//     at the current FORGE_REF. The fixture then replays against the corpus
//     as it is today — never a hard fail, never the stripped text
//     re-embedded into anything committed.
func resolveTokens(dir string, m matchJSON, reg *cards.Registry) (map[string]*cards.Card, error) {
	if len(m.Tokens) > 0 {
		return compileTokenScripts(m.Tokens)
	}
	syncDir, err := TokenSyncDir(filepath.Base(dir))
	if err != nil {
		return nil, err
	}
	if texts, err := readTokenScripts(syncDir); err != nil {
		return nil, err
	} else if len(texts) > 0 {
		return compileTokenScripts(texts)
	}
	return corpusTokens(reg), nil
}

// compileTokenScripts recompiles a token-script map (stem → script text)
// into compiled cards, walking stems in sorted order so a failure message
// is deterministic.
func compileTokenScripts(texts map[string]string) (map[string]*cards.Card, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	stems := make([]string, 0, len(texts))
	for s := range texts {
		stems = append(stems, s)
	}
	sort.Strings(stems)
	out := make(map[string]*cards.Card, len(stems))
	for _, s := range stems {
		c, err := compileToken(s, texts[s])
		if err != nil {
			return nil, err
		}
		out[s] = c
	}
	return out, nil
}

// corpusTokens returns the whole live corpus token map (already compiled)
// as cfg.Tokens. This is the superset the host itself captures, so a
// stripped fixture with no sync directory replays against the same token
// set the live match had.
func corpusTokens(reg *cards.Registry) map[string]*cards.Card {
	out := make(map[string]*cards.Card, len(reg.Tokens))
	for k, v := range reg.Tokens {
		out[k] = v
	}
	return out
}

// TokenSyncDir returns the gitignored directory holding the exact token
// script text behind a feedback fixture id — the text a committed
// match.json strips out of its `tokens` field because Forge scripts are
// GPL-3.0 and must never be committed. It returns the path whether or not
// it exists; callers consult existence (Load's fallback and cmd/repro's
// sync writer both do). An empty id names no fixture and is an error.
func TokenSyncDir(id string) (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("feedback: a token sync directory needs a fixture id")
	}
	return filepath.Join(root, "cmd", filepath.FromSlash("repro/testdata/.tokens"), id), nil
}

// readTokenScripts reads the token-script map a fixture's sync directory
// carries (one tokens.json, keyed stem → text). Returns nil when the
// directory holds nothing, which sends resolveTokens on to the live
// corpus fallback.
func readTokenScripts(syncDir string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(syncDir, "tokens.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("feedback: token sync dir %s: %w", syncDir, err)
	}
	var texts map[string]string
	if err := json.Unmarshal(raw, &texts); err != nil {
		return nil, fmt.Errorf("feedback: token sync dir %s: %w", syncDir, err)
	}
	return texts, nil
}

// compileToken recompiles one recorded token script through the corpus's
// own parse/link/intrinsics pipeline — the exact pipeline cards' compile
// runs — so the rebuilt card is the one the match minted from, not a
// zero-derived shell (a compiled card's P/T and colour identity are
// derived, which is why match.json ships script text and not cards).
func compileToken(stem, src string) (*cards.Card, error) {
	c, diags := cards.ParseBytes("tokenscripts/"+stem+".txt", []byte(src))
	if len(diags) != 0 {
		return nil, fmt.Errorf("feedback: token %q: %v", stem, diags)
	}
	if d := c.Link(); len(d) != 0 {
		return nil, fmt.Errorf("feedback: token %q: link: %v", stem, d)
	}
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c, nil
}

// EngineAt loads the snapshot at dir and replays it to the first n
// recorded intents — an engine positioned exactly where the reporter's
// game stood after n answers. n < 0 (the default the generated test
// skeletons pass) replays every recorded intent, i.e. to the capture
// point. Load errors and replay divergences are t.Fatalf: a repro test is
// about what happens AT the reported state, and getting there is fixture
// work, not an error to continue past.
//
// A negative n is NORMALIZED HERE, not left for replay.ReplayTo: ReplayTo
// clamps its own argument to [0, len(l.Intents)], so a bare -1 handed
// through would mean genesis — zero intents, the board before the match
// began — which is not what a caller passing the default asked for. EngineAt
// turns any negative into len(l.Intents) first, so the documented "every
// recorded intent" is what happens.
//
// The replay is replay.ReplayTo — the same code path cmd/repro drives — so
// an engine this returns and an engine a caller builds from Load's own
// return values are the same replay by construction; the head-equality
// gate is pinned in cmd/repro's tests.
func EngineAt(t testing.TB, dir string, n int) *rules.Engine {
	t.Helper()
	l, cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if n < 0 {
		n = len(l.Intents) // ReplayTo clamps negatives to 0 (genesis); the default here is ALL intents
	}
	e, err := replay.ReplayTo(l, cfg, n)
	if err != nil {
		t.Fatalf("feedback: %s: replay to intent %d: %v", dir, n, err)
	}
	return e
}

// openCorpus finds and opens the repo's .cards corpus. The repo root is
// resolved the way cards/boundary_test.go and testutil.CorpusRegistry do —
// `git rev-parse --show-toplevel` with every inherited GIT_* variable
// stripped (cards.GitEnv), because a hook or test harness exports GIT_DIR
// as a relative path that does not resolve from this package's own
// directory. A checkout with no .cards/ is an error, not a skip: Load's
// callers asked for a reproduction.
func openCorpus() (*cards.Registry, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, ".cards")
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("feedback: no .cards/ corpus at %s — run `make fetch-cards compile-cards`", dir)
	}
	reg, err := cards.SharedCorpus(dir)
	if err != nil {
		return nil, fmt.Errorf("feedback: corpus at %s: %w", dir, err)
	}
	return reg, nil
}

// repoRoot resolves the worktree root the same way testutil.CorpusRegistry
// does. Exported through Root so cmd/repro can resolve -emit-test package
// paths against the same root the corpus was found at.
func repoRoot() (string, error) {
	out, err := gitRevParse()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Root is the resolved repo root — where .cards lives and where -emit-test
// resolves a relative package path against.
func Root() (string, error) { return repoRoot() }

// gitRevParse runs `git rev-parse --show-toplevel` from this package's own
// directory, with every inherited GIT_* variable stripped — a hook or test
// harness exports GIT_DIR as a relative path (usually ".git") that only
// resolves from the repo root, so an unstripped environment makes this
// read the wrong place or fail outright.
func gitRevParse() (string, error) {
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("feedback: could not resolve git repo root: %v", err)
	}
	return string(out), nil
}
