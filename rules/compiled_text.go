package rules

import (
	"sort"
	"sync"
	"unsafe"
	"weak"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// compiledText contains only immutable interpretations of configured card
// text. It never participates in events or mutable game state and is safe to
// share with engine clones.
type compiledText struct {
	predicates *effects.PredicatePrograms
	// costs holds each configured cost text's frozen parse. Stored by
	// pointer so a lookup is a faststr map read and a read-only caller
	// (costRef) takes no copy of the ~750-byte Cost; parseCost still hands
	// out a value copy for callers that modify their cost.
	costs map[string]*compiledCost
	// saFacts holds every configured ability's facts record (sa_facts.go),
	// keyed by the ability's pointer.
	saFacts map[*cards.SA]*saFacts
	// faces holds every configured face's offer-walk facts
	// (walk_face_facts.go).
	faces walkFaceTable
	// base is the token-script layer (tokenCompiledText): the compiled text
	// of cfg.Tokens, shared by every configuration over the same token map.
	// Every table above covers only the configuration's deck cards and falls
	// back to base, so a new deck compiles its own few cards instead of
	// every token script again. Each entry is a pure function of its key
	// (cost text, ability, face, spec), so the layered answer is the union
	// table's. nil for a configuration without tokens, and on base itself.
	base *compiledText
	// pin holds the configured cards this layer was compiled from, so a
	// memo entry that finds this text alive knows every card it recorded by
	// address is alive too (compiledTextConfig).
	pin [][]*cards.Card
}

// costOf is the configured frozen parse of raw in this layer or its base.
func (ct *compiledText) costOf(raw string) (*compiledCost, bool) {
	if c, ok := ct.costs[raw]; ok {
		return c, true
	}
	if ct.base != nil {
		c, ok := ct.base.costs[raw]
		return c, ok
	}
	return nil, false
}

// faceFacts is the layered walk-face table lookup.
func (ct *compiledText) faceFacts(f *cards.Face) *walkFaceFacts {
	if ff := ct.faces.lookup(f); ff != nil || ct.base == nil {
		return ff
	}
	return ct.base.faces.lookup(f)
}

// compiledCost is one configured cost text's frozen parse plus its facts
// (pay.CompiledCost).
type compiledCost = pay.CompiledCost

func newCompiledCost(text string) *compiledCost { return pay.NewCompiledCost(text) }

// compiledTextConfig records exactly the deck card identities whose text
// feeds a compiledText's own layer (its token layer is matched by identity,
// compiledText.base). It intentionally excludes runtime and replay
// configuration: sidecars are keyed only by immutable configured card text.
//
// The identities are addresses, not pointers, so the memo pins nothing (see
// compiledTextCacheEntry). An address is compared only after the entry's
// text was loaded alive, and a live text holds its deck cards (pin), so no
// recorded card can have died and had its address reused.
type compiledTextConfig struct {
	decks [][]uintptr
}

type compiledTextCacheEntry struct {
	key    uintptr
	config compiledTextConfig
	// text is held WEAKLY. Every Face and SA points at its registry's
	// catalog, which lists every face and ability of the corpus, so a strong
	// entry pinned its whole registry (~400 MB) for the life of the process:
	// a test binary that loads the corpus per test kept the last 64
	// configurations' registries -- measured 2026-10-05 at 7.9 GB in use at
	// the end of compliance/oraclegen/templates. Held weakly, an entry
	// serves every engine built while another engine (or a clone, or a
	// replay) still uses its text, and is dropped once none does.
	text weak.Pointer[compiledText]
}

// compiledTextCacheLimit bounds the shared sidecar memo. An embedder that
// starts many games from distinct decks (cardfuzz: fresh random decks every
// game) otherwise grows it without bound -- measured 2026-09-27 at 3-4 GB per
// fuzz run before the process hit its memory cap. A server's tables reuse a
// handful of deck configurations, and a game's replay reuses its own, so a
// small bound keeps every real hit. On overflow, the oldest inserted entry
// is evicted (FIFO): an eviction only costs a recompile, and the text is
// immutable, so it can never reach an event.
const compiledTextCacheLimit = 64

var compiledTextCache = struct {
	sync.Mutex
	entries map[uintptr][]*compiledTextCacheEntry
	order   []*compiledTextCacheEntry
	n       int
}{entries: make(map[uintptr][]*compiledTextCacheEntry)}

func cardAddr(c *cards.Card) uintptr { return uintptr(unsafe.Pointer(c)) }

func newCompiledText(cfg Config) *compiledText {
	var base *compiledText
	if len(cfg.Tokens) > 0 {
		base = tokenCompiledText(cfg.Tokens)
	}
	key := cardAddr(firstConfiguredCard(cfg))
	compiledTextCache.Lock()
	for _, entry := range compiledTextCache.entries[key] {
		if text := entry.text.Value(); text != nil && text.base == base && entry.config.matchesConfig(cfg) {
			compiledTextCache.Unlock()
			return text
		}
	}
	compiledTextCache.Unlock()
	// Build outside the lock: a build walks every configured card and token
	// script, and holding the process-wide lock through it serialised every
	// concurrent engine construction (the compliance audit's worker pool).
	// Two racing builds of one configuration are the same pure function of
	// the same immutable card text; the first to insert wins below.
	config := snapshotCompiledTextConfig(cfg)
	text := buildCompiledTextOver(cfg, base)
	compiledTextCache.Lock()
	defer compiledTextCache.Unlock()
	for _, entry := range compiledTextCache.entries[key] {
		if text := entry.text.Value(); text != nil && text.base == base && entry.config.matchesConfig(cfg) {
			return text
		}
	}
	if compiledTextCache.n >= compiledTextCacheLimit {
		oldest := compiledTextCache.order[0]
		compiledTextCache.order[0] = nil
		compiledTextCache.order = compiledTextCache.order[1:]
		bucket := compiledTextCache.entries[oldest.key]
		for i, entry := range bucket {
			if entry == oldest {
				bucket = append(bucket[:i], bucket[i+1:]...)
				break
			}
		}
		if len(bucket) == 0 {
			delete(compiledTextCache.entries, oldest.key)
		} else {
			compiledTextCache.entries[oldest.key] = bucket
		}
		compiledTextCache.n--
	}
	entry := &compiledTextCacheEntry{key: key, config: config, text: weak.Make(text)}
	compiledTextCache.entries[key] = append(compiledTextCache.entries[key], entry)
	compiledTextCache.order = append(compiledTextCache.order, entry)
	compiledTextCache.n++
	return text
}

func firstConfiguredCard(cfg Config) *cards.Card {
	for _, deck := range cfg.Decks {
		for _, card := range deck {
			if card != nil {
				return card
			}
		}
	}
	return nil
}

func snapshotCompiledTextConfig(cfg Config) compiledTextConfig {
	config := compiledTextConfig{decks: make([][]uintptr, len(cfg.Decks))}
	for i, deck := range cfg.Decks {
		ids := make([]uintptr, len(deck))
		for j, c := range deck {
			ids[j] = cardAddr(c)
		}
		config.decks[i] = ids
	}
	return config
}

// matchesConfig compares cfg's deck card identities; the caller has already
// loaded the entry's text alive and matched its token layer.
func (c compiledTextConfig) matchesConfig(cfg Config) bool {
	if len(c.decks) != len(cfg.Decks) {
		return false
	}
	for i, deck := range c.decks {
		if len(deck) != len(cfg.Decks[i]) {
			return false
		}
		for j, id := range deck {
			if id != cardAddr(cfg.Decks[i][j]) {
				return false
			}
		}
	}
	return true
}

// cardText is one card's contribution to a compiledText, compiled once per
// card and hung on the card (cards.Card.CompiledSlot): every non-empty
// parameter value (the predicate candidates), every cost text, and every
// ability reachable from its faces. shape records the face lists it was
// computed over, so a card whose lists were replaced or grown since is
// recomputed rather than read stale.
type cardText struct {
	shape []cardTextShape
	texts []string
	costs []string
	sas   []*cards.SA
}

type cardTextShape struct {
	face                                *cards.Face
	manaCost                            string
	abilities, triggers, statics, repls int
	// bodies are the face's trigger Execute$ and replacement bodies, in
	// order: Card.Link re-resolves both into fresh abilities without
	// changing any list length, and a cardText over the old ones would
	// leave the new ones without facts records.
	bodies []*cards.SA
}

func faceBodies(f *cards.Face) []*cards.SA {
	out := make([]*cards.SA, 0, len(f.Triggers)+len(f.Repls))
	for i := range f.Triggers {
		out = append(out, f.Triggers[i].Effect)
	}
	for i := range f.Repls {
		out = append(out, f.Repls[i].With)
	}
	return out
}

func sameBodies(want []*cards.SA, f *cards.Face) bool {
	if len(want) != len(f.Triggers)+len(f.Repls) {
		return false
	}
	for i := range f.Triggers {
		if want[i] != f.Triggers[i].Effect {
			return false
		}
	}
	for i := range f.Repls {
		if want[len(f.Triggers)+i] != f.Repls[i].With {
			return false
		}
	}
	return true
}

func cardTextShapeOf(c *cards.Card) []cardTextShape {
	out := make([]cardTextShape, 0, len(c.Faces))
	for _, f := range c.Faces {
		if f == nil {
			out = append(out, cardTextShape{})
			continue
		}
		out = append(out, cardTextShape{face: f, manaCost: f.ManaCost, abilities: len(f.Abilities),
			triggers: len(f.Triggers), statics: len(f.Statics), repls: len(f.Repls), bodies: faceBodies(f)})
	}
	return out
}

func (ct *cardText) matches(c *cards.Card) bool {
	if len(ct.shape) != len(c.Faces) {
		return false
	}
	for i, f := range c.Faces {
		sh := ct.shape[i]
		if f == nil {
			if sh.face != nil {
				return false
			}
			continue
		}
		if sh.face != f || sh.manaCost != f.ManaCost || sh.abilities != len(f.Abilities) ||
			sh.triggers != len(f.Triggers) || sh.statics != len(f.Statics) || sh.repls != len(f.Repls) ||
			!sameBodies(sh.bodies, f) {
			return false
		}
	}
	return true
}

// cardTextOf is c's compiled contribution, from its slot when current.
func cardTextOf(c *cards.Card) *cardText {
	slot := c.CompiledSlot()
	if v, ok := slot.Load(""); ok {
		if ct := v.(*cardText); ct.matches(c) {
			return ct
		}
		return buildCardText(c)
	}
	ct := buildCardText(c)
	if slot == nil {
		return ct
	}
	if got := slot.Store("", ct).(*cardText); got.matches(c) {
		return got
	}
	return ct
}

func buildCardText(c *cards.Card) *cardText {
	predicateTexts := make(map[string]struct{})
	costTexts := make(map[string]struct{})
	seen := make(map[*cards.SA]struct{})
	ct := &cardText{shape: cardTextShapeOf(c)}
	addParams := func(params map[string]string) {}
	var addAbility func(*cards.SA)
	addAbility = func(sa *cards.SA) {
		if sa == nil {
			return
		}
		if _, ok := seen[sa]; ok {
			return
		}
		seen[sa] = struct{}{}
		ct.sas = append(ct.sas, sa)
		addParams(sa.Params)
		addAbility(sa.Sub)
	}
	addParams = func(params map[string]string) {
		keys := make([]string, 0, len(params))
		for key := range params {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := params[key]
			if value != "" {
				predicateTexts[value] = struct{}{}
			}
			if (key == "Cost" || key == "UnlessCost") && value != "" {
				costTexts[value] = struct{}{}
			}
		}
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		if f.ManaCost != "" {
			costTexts[f.ManaCost] = struct{}{}
		}
		// The AlternateAdditionalCost keyword's parts, which the offer walk
		// prices per hand card per walk (parseCost(part)): a configured text
		// is a map read instead of a parse. A part missing here (a face
		// edited since) still parses to the same cost.
		for _, part := range altAddCostParts(f) {
			costTexts[part] = struct{}{}
		}
		for _, sa := range f.Abilities {
			addAbility(sa)
		}
		for _, tr := range f.Triggers {
			addParams(tr.Params)
			addAbility(tr.Effect)
		}
		for _, st := range f.Statics {
			addParams(st.Params)
		}
		for _, rp := range f.Repls {
			addParams(rp.Params)
			addAbility(rp.With)
		}
	}
	for text := range predicateTexts {
		ct.texts = append(ct.texts, text)
	}
	sort.Strings(ct.texts)
	for text := range costTexts {
		ct.costs = append(ct.costs, text)
	}
	sort.Strings(ct.costs)
	return ct
}

func buildCompiledText(cfg Config) *compiledText {
	var base *compiledText
	if len(cfg.Tokens) > 0 {
		base = tokenCompiledText(cfg.Tokens)
	}
	return buildCompiledTextOver(cfg, base)
}

// buildCompiledTextOver compiles cfg's deck cards into one layer over base
// (cfg.Tokens' layer).
func buildCompiledTextOver(cfg Config, base *compiledText) *compiledText {
	var list []*cards.Card
	pin := make([][]*cards.Card, len(cfg.Decks))
	for i, deck := range cfg.Decks {
		list = append(list, deck...)
		pin[i] = append([]*cards.Card(nil), deck...)
	}
	text := compileCards(list, base)
	text.pin = pin
	return text
}

// tokenLayerCacheLimit bounds the token-layer memo's entry count. A
// process normally holds one token map (its registry's); tests that load
// several registries hold a few.
const tokenLayerCacheLimit = 8

// tokenLayerEntry holds its layer WEAKLY, for the reason
// compiledTextCacheEntry does: a strong process-wide entry would pin a dead
// registry for the life of the process. A layer lives exactly as long as
// some engine, or some deck layer (compiledText.base), still uses it.
type tokenLayerEntry struct {
	// tokens records the map's cards by address; a live text pins them
	// (compiledText.pin), so they are compared only once text is loaded.
	tokens map[string]uintptr
	text   weak.Pointer[compiledText]
}

var tokenLayerCache struct {
	sync.Mutex
	entries []tokenLayerEntry // oldest first
}

func (en *tokenLayerEntry) matches(tokens map[string]*cards.Card) bool {
	if len(en.tokens) != len(tokens) {
		return false
	}
	for key, token := range tokens {
		if id, ok := en.tokens[key]; !ok || id != cardAddr(token) {
			return false
		}
	}
	return true
}

// tokenCompiledText is the compiled text of a token map, memoised by its
// content (key -> card pointer), so every configuration over one registry's
// tokens shares one layer. The text is immutable, so a hit or a rebuild can
// never reach an event.
func tokenCompiledText(tokens map[string]*cards.Card) *compiledText {
	tokenLayerCache.Lock()
	defer tokenLayerCache.Unlock()
	live := tokenLayerCache.entries[:0]
	var hit *compiledText
	for _, entry := range tokenLayerCache.entries {
		text := entry.text.Value()
		if text == nil {
			continue // collected: drop the entry
		}
		live = append(live, entry)
		if hit == nil && entry.matches(tokens) {
			hit = text
		}
	}
	clear(tokenLayerCache.entries[len(live):])
	tokenLayerCache.entries = live
	if hit != nil {
		return hit
	}
	keys := make([]string, 0, len(tokens))
	for key := range tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]*cards.Card, 0, len(keys))
	snap := make(map[string]uintptr, len(keys))
	for _, key := range keys {
		list = append(list, tokens[key])
		snap[key] = cardAddr(tokens[key])
	}
	text := compileCards(list, nil)
	text.pin = [][]*cards.Card{list}
	if len(tokenLayerCache.entries) >= tokenLayerCacheLimit {
		copy(tokenLayerCache.entries, tokenLayerCache.entries[1:])
		tokenLayerCache.entries = tokenLayerCache.entries[:len(tokenLayerCache.entries)-1]
	}
	tokenLayerCache.entries = append(tokenLayerCache.entries, tokenLayerEntry{tokens: snap, text: weak.Make(text)})
	return text
}

// compileCards compiles list's cards (in order, each once) into one layer
// over base.
func compileCards(list []*cards.Card, base *compiledText) *compiledText {
	predicateTexts := make(map[string]struct{})
	costTexts := make(map[string]struct{})
	seen := make(map[*cards.SA]struct{})
	cardsSeen := make(map[*cards.Card]struct{})
	var faces []*cards.Face
	// Each card's contribution is compiled once (cardTextOf); a config is
	// the union of its cards', so repeat configurations cost a merge, not a
	// re-walk of every parameter map.
	for _, c := range list {
		if c == nil {
			continue
		}
		if _, ok := cardsSeen[c]; ok {
			continue
		}
		cardsSeen[c] = struct{}{}
		faces = append(faces, c.Faces...)
		ct := cardTextOf(c)
		for _, text := range ct.texts {
			predicateTexts[text] = struct{}{}
		}
		for _, text := range ct.costs {
			costTexts[text] = struct{}{}
		}
		for _, sa := range ct.sas {
			seen[sa] = struct{}{}
		}
	}
	preds := make([]string, 0, len(predicateTexts))
	for text := range predicateTexts {
		preds = append(preds, text)
	}
	sort.Strings(preds)
	costTextList := make([]string, 0, len(costTexts))
	for text := range costTexts {
		costTextList = append(costTextList, text)
	}
	sort.Strings(costTextList)
	costs := make(map[string]*compiledCost, len(costTextList))
	for _, text := range costTextList {
		costs[text] = newCompiledCost(text)
	}
	costOf := func(raw string) *compiledCost { return pay.CompiledCostFor(costs[raw], raw) }
	// Built from the seen set (a map range): each entry depends only on its
	// own ability, so the order the map is filled in cannot matter.
	saFacts := make(map[*cards.SA]*saFacts, len(seen))
	for sa := range seen {
		// The record already published on the ability (its own: f.SA ==
		// sa) is what factsOf serves for sa ahead of any table, so reuse it
		// instead of rebuilding an identical one per configuration.
		if f := effects.LoadSAFacts(sa); f != nil && f.SA == sa {
			saFacts[sa] = f
			continue
		}
		f := buildSAFacts(sa, costOf)
		saFacts[sa] = f
		publishSAFacts(f)
	}
	var parentPreds *effects.PredicatePrograms
	if base != nil {
		parentPreds = base.predicates
	}
	return &compiledText{predicates: effects.CompilePredicateProgramsOver(parentPreds, preds), costs: costs, saFacts: saFacts,
		faces: buildWalkFaceTable(faces), base: base}
}

func freezeCost(c Cost) Cost { return pay.FreezeCost(c) }

func (e *Engine) parseCost(raw string) Cost {
	if c := e.configuredCost(raw); c != nil {
		return c.Cost
	}
	return ParseCost(raw)
}

// faceCost is parseCost(f.ManaCost) through the face's ManaCost slot: the
// compiled cost (the configured frozen parse, or a fresh one for a face
// outside the configured set -- the same content) is hung on the face at
// first use, so later reads follow a pointer instead of hashing the text.
// Like parseCost it hands out a value copy.
func (e *Engine) faceCost(f *cards.Face) Cost {
	return e.faceCompiledCost(f).Cost
}

func (e *Engine) faceCompiledCost(f *cards.Face) *compiledCost {
	return pay.FaceCompiledCost(asPayer(e), f)
}

// freeCost is the parse of an empty cost text, shared read-only by costRef.
var freeCost = &pay.FreeCost

// costRef is parseCost for a READ-ONLY caller: it returns the configured
// text's shared frozen parse without copying it. The result MUST NOT be
// written (not a field, not an element of one of its slices) -- it is the
// same Cost every engine sharing this compiledText reads. A text outside the
// configured set (a runtime-built cost string) is parsed fresh, exactly as
// parseCost does.
func (e *Engine) costRef(raw string) *Cost {
	return &e.compiledCostOf(raw).Cost
}

// compiledCostOf is costRef with the compiled facts; the same read-only
// contract applies to the whole result.
func (e *Engine) compiledCostOf(raw string) *compiledCost {
	return pay.CompiledCostFor(e.configuredCost(raw), raw)
}

// matchesSpecFrom is the engine-owned form of effects.MatchesSpecFrom. It
// preserves the public helper's source-relative semantics while carrying this
// engine's immutable predicate programs into configured filter evaluation.
func (e *Engine) matchesSpecFrom(spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool {
	return e.matchesSpec(spec, id, e.specCtx(source, you))
}

// configuredCost is the compiled-text sidecar's frozen parse of raw, nil for a
// text outside the configured set (or an engine without a sidecar).
func (e *Engine) configuredCost(raw string) *compiledCost {
	if e != nil && e.compiledText != nil {
		if c, ok := e.compiledText.costOf(raw); ok {
			return c
		}
	}
	return nil
}
