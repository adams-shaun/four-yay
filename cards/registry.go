package cards

import (
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Registry is the compiled corpus: every card, indexed by normalised name.
type Registry struct {
	Cards   []*Card
	byName  map[string]*Card
	catalog *CompiledCatalog

	// Tokens holds compiled token scripts (forge-gui/res/tokenscripts),
	// keyed by file stem — e.g. "r_1_1_goblin" — the name a card's
	// TokenScript$ parameter references. Tokens are never Add-ed to Cards
	// or byName: a token is not a card a deck can contain, and Lookup must
	// not resolve a token's printed name to one.
	Tokens map[string]*Card

	// sub is set on a registry OpenCorpusSubset built: Cards then holds only
	// the requested cards, and Lookup answers from the whole corpus's name
	// index, decoding a card outside the subset on first use (subset.go).
	sub *subsetSource
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]*Card{}, Tokens: map[string]*Card{}}
}

// NormalizeName folds case, collapses whitespace and drops punctuation so
// catalogue names from Scryfall match Forge script names. A "Front // Back"
// name resolves to the front face, which is how Forge names the file.
func NormalizeName(s string) string {
	if i := strings.Index(s, "//"); i > 0 {
		s = s[:i]
	}
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			// Apostrophes, commas, colons and hyphens are dropped, not turned
			// into spaces: "Urza's Tower" and "Urzas Tower" must agree.
		}
	}
	return strings.TrimSpace(b.String())
}

func (r *Registry) Add(c *Card) {
	r.invalidateCatalog()
	r.Cards = append(r.Cards, c)
	if r.byName == nil {
		r.byName = map[string]*Card{}
	}
	for _, f := range c.Faces {
		if k := NormalizeName(f.Name); k != "" {
			if _, exists := r.byName[k]; !exists {
				r.byName[k] = c
			}
		}
		// Universes-Within flavour names resolve through the same index, so
		// Lookup and every consumer built on it (deck.File.Resolve,
		// deck.Validate) accept the WotC-printed alias. First-wins, and the
		// canonical loop above runs first, so an alias can never shadow a
		// card's real name.
		for _, a := range f.Aliases {
			if k := NormalizeName(a); k != "" {
				if _, exists := r.byName[k]; !exists {
					r.byName[k] = c
				}
			}
		}
	}
}

func (r *Registry) Lookup(name string) (*Card, bool) {
	if r.sub != nil {
		return r.sub.lookup(NormalizeName(name))
	}
	c, ok := r.byName[NormalizeName(name)]
	return c, ok
}

// rebuildNameIndex rebuilds byName from Cards with natively-named faces taking
// priority over derived and back faces. A card's identity is its own printed
// front-face name (Forge names the file "Front // Back" and NormalizeName
// already folds such a name to the front), so a face whose name came from a
// CopyFaceFrom directive -- or a back face -- must never shadow the real card
// that natively prints that name. That matters once CopyFaceFrom resolves: a
// Prepare inset spell face carries the REAL referenced spell's name
// ("Lightning Bolt"), a Split card's halves carry standalone cards' names
// ("Bind", "Liberate"), and the referring card can sort before the referenced
// one (e/emeritus_of_conflict before l/lightning_bolt), so a plain sorted-order
// index would make Lookup return the stub and break every decklist naming the
// real card.
//
// Tier 1 is every natively-named front face; tier 2 is every other face
// (native or derived backs, and CopyFaceFrom-derived fronts, which a Split card
// has). Within a tier the first card wins, Add's rule; back names are still
// indexed when no native front claims them, preserving the pre-existing lookup
// of a transforming or split back face.
func (r *Registry) rebuildNameIndex() { r.byName = nameIndexOf(r.Cards) }

// nameIndexOf is rebuildNameIndex's tiered index over cs, as a pure function
// so the segment file (subset.go) records exactly the index LoadRegistry
// builds over the whole corpus.
func nameIndexOf(cs []*Card) map[string]*Card {
	byName := map[string]*Card{}
	index := func(f *Face, c *Card) {
		if k := NormalizeName(f.Name); k != "" {
			if _, exists := byName[k]; !exists {
				byName[k] = c
			}
		}
		for _, a := range f.Aliases {
			if k := NormalizeName(a); k != "" {
				if _, exists := byName[k]; !exists {
					byName[k] = c
				}
			}
		}
	}
	for _, c := range cs {
		if len(c.Faces) > 0 && c.Faces[0] != nil && c.Faces[0].CopyFaceFrom == "" {
			index(c.Faces[0], c)
		}
	}
	for _, c := range cs {
		for i := range c.Faces {
			f := c.Faces[i]
			if f == nil || (i == 0 && f.CopyFaceFrom == "") {
				continue
			}
			index(f, c)
		}
	}
	return byName
}

// resolveCopyFaces resolves every face carrying a `CopyFaceFrom:<Card>`
// directive by copying the named card's front-face printed characteristics
// onto it, then re-deriving and re-linking the face so its cmc, power/
// toughness, colour identity, word sets and keyword expansions match a face
// compiled from that text directly. A face that already has a Name is left
// alone, so the pass is idempotent and an inline ALTERNATE back is never
// overwritten. An unresolvable reference (no card by that name in this
// registry) leaves the face nameless -- CompileDir's caller diagnoses that
// case; it is never a hard error.
//
// It MUST run after every card has been Add-ed: compileScripts sorts paths,
// so the referenced card can compile later than the referring one (e.g.
// start_fire.txt sorts after bind_liberate.txt but not necessarily after the
// card it names). It runs in BOTH construction routes -- CompileDir and
// LoadRegistry -- so a decoded cache that still held stub faces ends with the
// same resolved faces a fresh compile builds.
func (r *Registry) resolveCopyFaces() {
	resolved := map[*Card]bool{}
	changed := false
	var resolveCard func(c *Card)
	resolveCard = func(c *Card) {
		if c == nil || resolved[c] {
			return
		}
		// Mark before recursing: a cyclic reference (A's back copies B, B's
		// back copies A) then terminates, leaving at most one face unresolved
		// rather than looping forever.
		resolved[c] = true
		for _, f := range c.Faces {
			if f == nil || f.CopyFaceFrom == "" || f.Name != "" {
				continue
			}
			src, ok := r.byName[NormalizeName(f.CopyFaceFrom)]
			if !ok || src == nil || len(src.Faces) == 0 || src.Faces[0] == nil {
				continue
			}
			// The referenced card may itself carry an unresolved
			// CopyFaceFrom face; resolve it first so the copy sees real
			// characteristics rather than another stub.
			resolveCard(src)
			f.copyPrintedFrom(src.Faces[0])
			// derive before link for the same reason both construction routes
			// do it: link's keyword expansion reads cmc (Transmute).
			f.derive()
			f.link(c.Path)
			f.ApplyIntrinsics()
			changed = true
		}
	}
	for _, c := range r.Cards {
		resolveCard(c)
	}
	if changed {
		r.invalidateCatalog()
	}
}

// cacheFile is the on-disk shape. Only Cards and Tokens are encoded; the
// byName index is rebuilt on load so it can never disagree with Cards.
type cacheFile struct {
	Version int
	Cards   []*Card
	Tokens  map[string]*Card
}

// cacheVersion 2 adds Tokens: a v1 cache predates Registry.Tokens entirely,
// so LoadRegistry refuses it outright (see the version check below) rather
// than silently serving a registry with no tokens compiled in. cacheVersion
// 3 adds keyword expansion at link time (Face.expandKeywords, keywords.go):
// a v2 cache was compiled before Faces carried the triggers, replacements
// and abilities that expansion adds, so its Primitives() would undercount
// exactly the way a v1-vs-Tokens cache would. Version 4 carries Card's
// AlternateMode, which name-characteristic matching needs to distinguish a
// split card from a transforming double-faced card away from the battlefield.
// Version 5 carries Face.Aliases, the Universes-Within flavour names read out
// of Variant: lines. Version 6 carries Face.SpecializeColor: the SPECIALIZE:
// boundary token is not retained elsewhere, so a stale cache cannot be repaired
// in memory and must be recompiled. Keyword expansions are also re-linked after
// decoding below, so a newly added idempotent expansion does not force every
// worktree to rewrite its corpus.
const cacheVersion = 6

// CacheVersionError is returned by LoadRegistry when the cache file on disk
// was written by a different cacheVersion than this build's. Callers detect
// it with errors.As so they can offer a targeted remedy (an in-memory
// recompile) instead of the generic load-failure handling.
type CacheVersionError struct {
	Got  int
	Want int
}

func (e *CacheVersionError) Error() string {
	return fmt.Sprintf("IR cache version %d, want %d — run `make compile-cards`", e.Got, e.Want)
}

// Save writes the gob cache to path and, beside it, the per-card segment
// file (SegmentPath) the subset loader reads. Both carry the same values.
func (r *Registry) Save(path string) error {
	if r.sub != nil {
		return fmt.Errorf("cards: a subset registry cannot be saved as a corpus cache")
	}
	if err := r.saveGob(path); err != nil {
		return err
	}
	return writeSegments(SegmentPath(path), r.Cards, r.Tokens)
}

func (r *Registry) saveGob(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// A unique temp name: OpenCorpus writes a recompiled cache back, so
	// parallel processes (go test ./..., botbench fleets) can race to save
	// the same path, and a shared fixed ".tmp" name would interleave their
	// writes. Each writes its own file and the rename is atomic.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	// CreateTemp opens 0600; the cache has always been a plain 0644 file.
	if err := f.Chmod(0o644); err != nil {
		return fail(err)
	}
	zw, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		return fail(err)
	}
	if err := gob.NewEncoder(zw).Encode(cacheFile{Version: cacheVersion, Cards: r.Cards, Tokens: r.Tokens}); err != nil {
		zw.Close()
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func LoadRegistry(path string) (*Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var cf cacheFile
	if err := gob.NewDecoder(zr).Decode(&cf); err != nil {
		zr.Close()
		return nil, err
	}
	if _, err := io.Copy(io.Discard, zr); err != nil {
		zr.Close()
		return nil, err
	}
	if err := zr.Close(); err != nil {
		return nil, err
	}
	if cf.Version != cacheVersion {
		return nil, &CacheVersionError{Got: cf.Version, Want: cacheVersion}
	}
	return finishDecoded(cf.Cards, cf.Tokens)
}

// finishDecoded is the post-decode half of LoadRegistry: it turns cards and
// tokens exactly as the gob cache stores them into a usable registry. It is
// shared by LoadRegistry and the subset loader (OpenCorpusSubset), so a card
// decoded from the per-card segment file goes through the same repair,
// derive, link, index and catalog steps as the same card decoded from the
// whole cache -- the property that makes a subset registry play games
// byte-identical to the full one.
func finishDecoded(decoded []*Card, tokens map[string]*Card) (*Registry, error) {
	r := NewRegistry()
	for _, c := range decoded {
		// The derived fields are unexported, so gob never encodes them and a
		// stale cache decodes them as zero. derive recomputes them here — the
		// gob construction route must end with the same derived values as the
		// ParseBytes route.
		for _, f := range c.Faces {
			// derive FIRST: a decoded face's cmc is zero, and link's keyword
			// expansion reads it — Transmute's search spec is
			// Card.cmcEQ<Cmc()>, so relinking before deriving would expand a
			// reusable cache as cmcEQ0 instead of the source's mana value.
			// The ParseBytes route has the same order (parse derives, then
			// compileScripts links) for the same reason. derive is a pure
			// function of printed fields, so the second derive after link is
			// a no-op for them; it stays so both routes end with the same
			// derived values link's added abilities could someday depend on.
			// The same stale-cache repair discipline as the keyword relink
			// below, one step earlier: a cache predating the comma-mode split
			// stores each compound S: line ("Mode$ CantAttack,CantBlock") as
			// ONE Static; resplitStatics re-applies the parse-time split in
			// memory (the full Mode$ text survives in Params), so a decoded
			// shared cache ends with the same static list a fresh compile
			// builds. Runs before derive for the same derive-before-link
			// reason the gob route documents above.
			f.Statics = resplitStatics(f.Statics)
			f.derive()
			// Re-link decoded faces so a newly added idempotent keyword expansion
			// is present even when this worktree intentionally reuses the shared,
			// read-only corpus cache. This covers the cumulative-upkeep keyword
			// expansions this task added too: link re-runs expandKeywords and
			// re-resolves every trigger's Execute$ SVar.
			f.link(c.Path)
			f.derive()
		}
		// A cache can predate a newly added idempotent keyword expansion.
		// Link runs that expansion again while preserving existing generated
		// entries, so loading an older cache never turns a supported keyword
		// into a behaviourless printed label.
		c.Link()
		r.Add(c)
	}
	// Resolve CopyFaceFrom references after every card is indexed: a decoded
	// cache produced before this pass existed holds stub faces whose raw
	// directive the pre-pass parser dropped, so this is the second construction
	// route that must end with the same resolved faces CompileDir builds. A
	// face already named (a cache saved after resolution) is skipped, so the
	// pass is idempotent.
	r.resolveCopyFaces()
	r.rebuildNameIndex()
	if tokens != nil {
		r.Tokens = tokens
		for _, c := range r.Tokens {
			for _, f := range c.Faces {
				// The same derive-before-link order as the cards loop above:
				// token scripts share compileScripts' parse/link pipeline, so a
				// stale token cache relinks its keyword expansions too.
				f.Statics = resplitStatics(f.Statics)
				f.derive()
				f.link(c.Path)
				f.derive()
			}
			c.Link()
		}
	}
	if err := r.CompileMetadata(); err != nil {
		return nil, err
	}
	return r, nil
}

// CompileDir walks a cardsfolder tree, parses, links and applies intrinsics.
// Paths are sorted so the resulting Cards slice — and therefore the cache
// bytes — are byte-identical across runs.
func CompileDir(dir string) (*Registry, []Diag, error) {
	parsed, diags, err := compileScripts(dir)
	if err != nil {
		return nil, nil, err
	}
	if len(parsed) == 0 {
		return nil, nil, fmt.Errorf("no card scripts under %s — run `make fetch-cards`", dir)
	}

	r := NewRegistry()
	for _, c := range parsed {
		r.Add(c)
	}
	// Resolve CopyFaceFrom references only after every card is Add-ed: the
	// referenced card may compile later in sorted path order. The nameless-face
	// diagnostic below runs afterward so a card whose only identity comes from
	// a resolvable reference is not flagged as a defect.
	r.resolveCopyFaces()
	// Rebuild the name index with front faces taking priority over back faces
	// (see rebuildNameIndex): a resolved CopyFaceFrom inset or a Split half
	// must not shadow the real card it names. Unconditional, because a decoded
	// cache may already carry resolved faces.
	r.rebuildNameIndex()
	for _, c := range parsed {
		// A card with no named face parsed without error but carries no
		// identity — e.g. a script whose only content is a directive the parser
		// cannot resolve. That is worth a diagnostic per card, not per face: an
		// ALTERNATE face alone being nameless is normal (a legitimate
		// CopyFaceFrom whose reference is absent, or an inline back), but a card
		// with no named face at all silently drops out of Coverage, and a human
		// should be told which file did that.
		if !c.named() {
			diags = append(diags, Diag{c.Path, "card has no named face on any face; excluded from coverage (likely an unresolved CopyFaceFrom or similar directive)"})
		}
	}

	if err := compileTokens(r, dir, &diags); err != nil {
		return nil, nil, err
	}
	if err := r.CompileMetadata(); err != nil {
		return nil, nil, err
	}

	return r, diags, nil
}

// compileScripts walks dir for every .txt script and parses, links and
// applies intrinsics to each, in sorted-path order — so compilation order,
// and therefore the cache bytes, is reproducible across runs. It is the one
// pipeline shared by CompileDir's card loop and compileTokens' token loop:
// the two differ only in what they do with the resulting Cards afterward
// (add to Cards/byName plus diagnose namelessness, vs. key by file stem
// into Tokens), never in how a script gets from bytes on disk to a fully
// linked Card.
func compileScripts(dir string) ([]*Card, []Diag, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".txt") {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)

	var diags []Diag
	out := make([]*Card, 0, len(paths))
	for _, p := range paths {
		c, d := Parse(p)
		diags = append(diags, d...)
		if c == nil {
			continue
		}
		diags = append(diags, c.Link()...)
		for _, f := range c.Faces {
			f.ApplyIntrinsics()
		}
		out = append(out, c)
	}
	return out, diags, nil
}

// compileTokens walks dir's tokenscripts sibling — Fetch's TokensDir — and
// compiles every script into r.Tokens, keyed by file stem ("r_1_1_goblin").
// Tokens share compileScripts' parse/link/intrinsics pipeline but are never
// Add-ed to Cards or byName: a token is not a card a deck can contain, and
// a card's Lookup-by-name must not resolve to one. A missing tokenscripts
// directory is not an error — plenty of fixtures (and every pre-M2r cache)
// have no tokens at all.
func compileTokens(r *Registry, cardsDir string, diags *[]Diag) error {
	tokensDir := TokensDir(filepath.Dir(cardsDir))
	info, err := os.Stat(tokensDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	tokens, d, err := compileScripts(tokensDir)
	if err != nil {
		return err
	}
	*diags = append(*diags, d...)
	for _, c := range tokens {
		stem := strings.TrimSuffix(filepath.Base(c.Path), ".txt")
		r.Tokens[stem] = c
	}
	return nil
}
