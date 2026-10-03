package testutil

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
)

// decksFS embeds the project's own 12 Legacy archetype deck lists -- a bare
// {name, count} card list per deck, authored for this repo and Apache-2.0
// like the rest of it. Embedding these is not the licensing hazard
// cards/boundary_test.go guards against: that guard fires on a Forge
// script's own Name:/Oracle: shape (GPL-3.0), which a JSON name-and-count
// list never carries, no matter how it is packaged (Ruling P11).
//
//go:embed decks/*.json
var decksFS embed.FS

const decksDir = "decks"

// RepoDeckNames lists the embedded decks by file stem (e.g.
// "mono-red-goblins"), sorted. Sorting matters: callers index into this
// slice to assign decks to seats (Task 26's acceptance test does exactly
// that for 2/4/6/8 seats), and embed.FS.ReadDir's own order, while already
// alphabetical in the standard library, is not a documented guarantee this
// package should lean on.
func RepoDeckNames() []string {
	entries, err := decksFS.ReadDir(decksDir)
	if err != nil {
		// The embed is compiled into the binary at build time; a failure
		// here means decks/ vanished from the source tree, not something a
		// caller can recover from at runtime.
		panic("testutil: decks/*.json embed unreadable: " + err.Error())
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// legacyDeckNames is the 12 60-card constructed Legacy decks the acceptance
// suite pins its golden games to (Ruling M38-P). The deck directory also
// holds the five interim 100-card Commander decks (foundations-*), which are
// not part of the 12: the acceptance chain heads in rules/testdata/heads/ are
// byte-identical games over these 12, and adding commander decks earlier
// silently moved those heads by changing the index-based round-robin seat
// assignment (i % len(all)). Seating the golden games off an explicit,
// closed list -- never RepoDeckNames() -- keeps "add a deck file" from
// perturbing the replay-golden games, which is the contract rules' TestHeads
// pins. Kept sorted so the seat assignment is the same order the 12
// were always seated in.
var legacyDeckNames = [...]string{
	"death-n-taxes",
	"dimir-tempo",
	"eldrazi-stompy",
	"mono-black-aggro",
	"mono-blue-tempo",
	"mono-green-stompy",
	"mono-red-goblins",
	"the-epic-storm",
	"tron",
	"ur-delver",
	"uw-control",
	"uw-tempo",
}

// LegacyDeckNames is the closed, sorted list of the 12 60-card constructed
// Legacy repo decks that the replay-golden acceptance games seat from. The
// captain of the replay chain: a deck file added to the directory must never
// change what these games are, or every golden head in rules/testdata/heads/
// moves for no behavioural reason.
func LegacyDeckNames() []string {
	return legacyDeckNames[:]
}

// AcceptanceDecks seats the deterministic acceptance game at seats seats:
// LegacyDeckNames round-robined from deck 0, each seat's deck resolved
// through LoadRepoDeck. It is the seating half of the game rules'
// TestHeads pins and cmd/headdiff replays (rules.AcceptanceConfig and
// rules.PlayAcceptance are the rest), so both play exactly the same game.
func AcceptanceDecks(r *cards.Registry, seats int) ([]string, [][]*cards.Card, error) {
	all := LegacyDeckNames()
	names := make([]string, seats)
	decks := make([][]*cards.Card, seats)
	for i := 0; i < seats; i++ {
		names[i] = all[i%len(all)]
		d, err := LoadRepoDeck(r, names[i])
		if err != nil {
			return nil, nil, err
		}
		decks[i] = d
	}
	return names, decks, nil
}

// LoadRepoDeck reads decks/<name>.json and resolves it through the deck
// package (deck.Parse then File.Resolve, which runs cards.NormalizeName so
// Scryfall-shaped catalogue names and Forge script names fold to the same
// key) into a flat, count-many-times-repeated slice of *cards.Card pointers
// -- the shape rules.Config.Decks wants. It is the non-testing.TB variant
// mtgsim uses directly; RepoDeck below is the t.Fatalf-on-error wrapper for
// tests (Ruling P11).
func LoadRepoDeck(r *cards.Registry, name string) ([]*cards.Card, error) {
	// embed.FS paths are always slash-separated regardless of host OS --
	// filepath.Join would emit "decks\name.json" on Windows and the read
	// would fail there. path.Join is the correct call for an embed.FS path.
	raw, err := decksFS.ReadFile(path.Join(decksDir, name+".json"))
	if err != nil {
		return nil, fmt.Errorf("testutil: deck %q: %w", name, err)
	}
	f, err := deck.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("testutil: deck %q: %w", name, err)
	}
	cs, err := f.Resolve(r)
	if err != nil {
		return nil, fmt.Errorf("testutil: %w", err)
	}
	return cs, nil
}

// RepoDeck is LoadRepoDeck for tests: a missing embedded deck file or a card
// name the registry cannot resolve is a fixture problem the test has no
// useful way to continue past, so it fails immediately via t.Fatalf rather
// than handing the caller an error to (potentially inconsistently) check.
func RepoDeck(t testing.TB, r *cards.Registry, name string) []*cards.Card {
	t.Helper()
	deck, err := LoadRepoDeck(r, name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return deck
}

// LoadRepoDeckFile reads and parses one embedded deck file un-resolved,
// for non-test callers that need the deck.File shape (cmd/botbench's
// commander mode needs CommanderIndices — one flat index per commander, a
// CR 903.13 partner pair naming two — and a deck that names a commander
// carries them to tell a commander deck from a constructed one). It is
// RepoDeckFile's non-test sibling, exactly as LoadRepoDeck is RepoDeck's.
func LoadRepoDeckFile(name string) (deck.File, error) {
	// embed.FS paths are always slash-separated regardless of host OS;
	// path.Join keeps an embed path on every platform (the comment in
	// LoadRepoDeck explains why filepath.Join would be wrong here).
	raw, err := decksFS.ReadFile(path.Join(decksDir, name+".json"))
	if err != nil {
		return deck.File{}, fmt.Errorf("testutil: deck %q: %w", name, err)
	}
	f, err := deck.Parse(raw)
	if err != nil {
		return deck.File{}, fmt.Errorf("testutil: deck %q: %w", name, err)
	}
	return f, nil
}

// RepoDeckFile is LoadRepoDeckFile for tests: a missing embedded deck file
// is a fixture problem the test has no useful way to continue past, so it
// fails immediately via t.Fatalf rather than handing the caller an error to
// (potentially inconsistently) check -- the same wrapper RepoDeck is over
// LoadRepoDeck (Ruling P11).
func RepoDeckFile(t testing.TB, name string) deck.File {
	t.Helper()
	f, err := LoadRepoDeckFile(name)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return f
}

// OpenCorpusRegistry opens dir's compiled corpus, compiling it fresh from
// dir/cardsfolder when the cache is missing or stale. It is
// cards.SharedCorpus: one registry per directory per process, since a
// re-opened copy stays pinned by the rules layer's pointer-keyed memos.
func OpenCorpusRegistry(dir string) (*cards.Registry, error) {
	return cards.SharedCorpus(dir)
}

type corpusRegistryResult struct {
	reg     *cards.Registry
	dir     string
	missing bool
	err     error
}

type corpusRegistryCache struct {
	once   sync.Once
	result corpusRegistryResult
}

func (c *corpusRegistryCache) get(load func() corpusRegistryResult) corpusRegistryResult {
	c.once.Do(func() { c.result = load() })
	return c.result
}

var repoCorpusRegistry corpusRegistryCache

func loadRepoCorpusRegistry() corpusRegistryResult {
	cmd := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel")
	cmd.Env = cards.GitEnv()
	out, err := cmd.Output()
	if err != nil {
		return corpusRegistryResult{err: fmt.Errorf("testutil: could not resolve git repo root: %v", err)}
	}
	dir := filepath.Join(strings.TrimSpace(string(out)), ".cards")
	if _, err := os.Stat(dir); err != nil {
		return corpusRegistryResult{dir: dir, missing: true}
	}
	r, err := OpenCorpusRegistry(dir)
	if err != nil {
		return corpusRegistryResult{dir: dir, err: fmt.Errorf("testutil: could not open corpus at %s: %v", dir, err)}
	}
	return corpusRegistryResult{reg: r, dir: dir}
}

// CorpusRegistry finds the repo root the way cards/boundary_test.go does --
// `git rev-parse --show-toplevel`, not a hard-coded relative path, which is
// exactly what silently broke that file's own license-boundary test when
// this package was transplanted to a new module root (Ruling P11) -- and
// opens its .cards/ corpus. A checkout with no .cards/ directory at all
// (nothing ever fetched) is not a test failure: it Skips, so `go test
// ./...` still passes on a clean clone with no network access. Once .cards/
// exists, though, a corpus that fails to open is a real problem worth
// stopping on, not skipping past.
func CorpusRegistry(t testing.TB) *cards.Registry {
	t.Helper()
	// The child git MUST run with cards.GitEnv() -- os.Environ() with every
	// inherited GIT_* variable stripped -- for the same reason cards/fetch.go
	// and cards/boundary_test.go do. Git EXPORTS GIT_DIR to every hook it
	// runs, usually as the relative path ".git", and GIT_DIR outranks the
	// "-C ." above. A test binary's working directory is its own package
	// directory (rules/, effects/, ...), never the repo root, so under a hook
	// that relative GIT_DIR resolves to rules/.git, which does not exist:
	// rev-parse fails, the corpus is never found, and every corpus test in
	// the package quietly stops testing the corpus.
	//
	// That is not hypothetical. It is why the pre-commit test-time hook has
	// been recording ~3s rows for the rules package in git worktrees while an
	// identical manual run of the same command measures ~18s. Those rows are
	// fiction, and they became the baseline other measurements were judged
	// against.
	result := repoCorpusRegistry.get(loadRepoCorpusRegistry)
	if result.missing {
		t.Skip("testutil: no .cards/ corpus present -- run `make fetch-cards compile-cards`")
	}
	if result.err != nil {
		t.Fatalf("%v", result.err)
	}
	return result.reg
}
