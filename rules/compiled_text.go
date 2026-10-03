package rules

import (
	"reflect"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
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
}

// compiledCost is one configured cost text's frozen parse plus the facts
// the hot read-only callers ask of it.
type compiledCost struct {
	Cost
	// bareTap: the text is exactly {T} -- Tap set and every other component
	// zero -- the cost of nearly every mana ability. manaAbilityCostPayable
	// prices it without the generic payability walk.
	bareTap bool
	// beyondTap caches manaCostBeyondTap(Cost) (the fb-led1 marker test).
	beyondTap bool
	// text memoizes formatCost(Cost) (manaActivationCostMarker): the Cost
	// is frozen, so its text never changes; set once, read by any engine.
	text atomic.Pointer[string]
}

// formatted is formatCost(cc.Cost), memoized on the frozen cost.
func (cc *compiledCost) formatted() string {
	if t := cc.text.Load(); t != nil {
		return *t
	}
	t := formatCost(cc.Cost)
	cc.text.Store(&t)
	return t
}

func newCompiledCost(text string) *compiledCost {
	c := freezeCost(ParseCost(text))
	return &compiledCost{Cost: c, bareTap: costIsBareTap(&c), beyondTap: manaCostBeyondTap(c)}
}

// costIsBareTap reports whether c is exactly {T}. Any component it cannot
// prove zero (a non-nil empty slice included) answers false, the direction
// that only ever keeps the full walk.
func costIsBareTap(c *Cost) bool {
	if !c.Tap {
		return false
	}
	rest := *c
	rest.Tap = false
	return reflect.DeepEqual(rest, Cost{})
}

// compiledTextConfig snapshots exactly the card pointers whose text feeds a
// compiledText. It intentionally excludes runtime and replay configuration:
// sidecars are keyed only by immutable configured card text.
type compiledTextConfig struct {
	decks  [][]*cards.Card
	tokens map[string]*cards.Card
}

type compiledTextCacheEntry struct {
	key    *cards.Card
	config compiledTextConfig
	text   *compiledText
}

// compiledTextCacheLimit bounds the shared sidecar memo. Each entry holds
// every configured deck card's AND every token script's compiled text, so an
// embedder that starts many games from distinct decks (cardfuzz: fresh random
// decks every game) grew it without bound -- measured 2026-09-27 at 3-4 GB per
// fuzz run before the process hit its memory cap. A server's tables reuse a
// handful of deck configurations, and a game's replay reuses its own, so a
// small bound keeps every real hit. On overflow, the oldest inserted entry
// is evicted (FIFO): an eviction only costs a recompile, and the text is
// immutable, so it can never reach an event.
const compiledTextCacheLimit = 64

var compiledTextCache = struct {
	sync.Mutex
	entries map[*cards.Card][]*compiledTextCacheEntry
	order   []*compiledTextCacheEntry
	n       int
}{entries: make(map[*cards.Card][]*compiledTextCacheEntry)}

func newCompiledText(cfg Config) *compiledText {
	key := firstConfiguredCard(cfg)
	compiledTextCache.Lock()
	defer compiledTextCache.Unlock()
	for _, entry := range compiledTextCache.entries[key] {
		if entry.config.matchesConfig(cfg) {
			return entry.text
		}
	}
	config := snapshotCompiledTextConfig(cfg)
	text := buildCompiledText(cfg)
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
	entry := &compiledTextCacheEntry{key: key, config: config, text: text}
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
	config := compiledTextConfig{
		decks:  make([][]*cards.Card, len(cfg.Decks)),
		tokens: make(map[string]*cards.Card, len(cfg.Tokens)),
	}
	for i, deck := range cfg.Decks {
		config.decks[i] = append([]*cards.Card(nil), deck...)
	}
	for key, token := range cfg.Tokens {
		config.tokens[key] = token
	}
	return config
}

func (c compiledTextConfig) matchesConfig(cfg Config) bool {
	if len(c.decks) != len(cfg.Decks) || len(c.tokens) != len(cfg.Tokens) {
		return false
	}
	for i, deck := range c.decks {
		if len(deck) != len(cfg.Decks[i]) {
			return false
		}
		for j, card := range deck {
			if card != cfg.Decks[i][j] {
				return false
			}
		}
	}
	for key, token := range c.tokens {
		if otherToken, ok := cfg.Tokens[key]; !ok || token != otherToken {
			return false
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
	predicateTexts := make(map[string]struct{})
	costTexts := make(map[string]struct{})
	seen := make(map[*cards.SA]struct{})
	cardsSeen := make(map[*cards.Card]struct{})
	var faces []*cards.Face
	// Each card's contribution is compiled once (cardTextOf); a config is
	// the union of its cards', so repeat configurations -- and the token
	// scripts every configuration shares -- cost a merge, not a re-walk of
	// every parameter map.
	addCard := func(c *cards.Card) {
		if c == nil {
			return
		}
		if _, ok := cardsSeen[c]; ok {
			return
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
	for _, deck := range cfg.Decks {
		for _, c := range deck {
			addCard(c)
		}
	}
	tokenKeys := make([]string, 0, len(cfg.Tokens))
	for key := range cfg.Tokens {
		tokenKeys = append(tokenKeys, key)
	}
	sort.Strings(tokenKeys)
	for _, key := range tokenKeys {
		addCard(cfg.Tokens[key])
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
	costOf := func(raw string) *compiledCost {
		if c, ok := costs[raw]; ok {
			return c
		}
		if raw == "" {
			return &freeCost
		}
		return newCompiledCost(raw)
	}
	// Built from the seen set (a map range): each entry depends only on its
	// own ability, so the order the map is filled in cannot matter.
	saFacts := make(map[*cards.SA]*saFacts, len(seen))
	for sa := range seen {
		f := buildSAFacts(sa, costOf)
		saFacts[sa] = f
		publishSAFacts(f)
	}
	return &compiledText{predicates: effects.CompilePredicatePrograms(preds), costs: costs, saFacts: saFacts,
		faces: buildWalkFaceTable(faces)}
}

func freezeCost(c Cost) Cost {
	c.Hybrid = c.Hybrid[:len(c.Hybrid):len(c.Hybrid)]
	c.Phyrexian = c.Phyrexian[:len(c.Phyrexian):len(c.Phyrexian)]
	c.Twobrid = c.Twobrid[:len(c.Twobrid):len(c.Twobrid)]
	c.HybridPhyrexian = c.HybridPhyrexian[:len(c.HybridPhyrexian):len(c.HybridPhyrexian)]
	c.Sac = c.Sac[:len(c.Sac):len(c.Sac)]
	c.Discard = c.Discard[:len(c.Discard):len(c.Discard)]
	c.SubCounter = c.SubCounter[:len(c.SubCounter):len(c.SubCounter)]
	c.AddCounter = c.AddCounter[:len(c.AddCounter):len(c.AddCounter)]
	c.Exile = c.Exile[:len(c.Exile):len(c.Exile)]
	c.ExileFromTop = c.ExileFromTop[:len(c.ExileFromTop):len(c.ExileFromTop)]
	c.Reveal = c.Reveal[:len(c.Reveal):len(c.Reveal)]
	c.RevealOrChoose = c.RevealOrChoose[:len(c.RevealOrChoose):len(c.RevealOrChoose)]
	c.Behold = c.Behold[:len(c.Behold):len(c.Behold)]
	c.TapPermanent = c.TapPermanent[:len(c.TapPermanent):len(c.TapPermanent)]
	c.Blight = c.Blight[:len(c.Blight):len(c.Blight)]
	c.Draw = c.Draw[:len(c.Draw):len(c.Draw)]
	c.Energy = c.Energy[:len(c.Energy):len(c.Energy)]
	c.LifeX = c.LifeX[:len(c.LifeX):len(c.LifeX)]
	c.DamageYou = c.DamageYou[:len(c.DamageYou):len(c.DamageYou)]
	c.GainLife = c.GainLife[:len(c.GainLife):len(c.GainLife)]
	c.Return = c.Return[:len(c.Return):len(c.Return)]
	c.PutToLib = c.PutToLib[:len(c.PutToLib):len(c.PutToLib)]
	c.MoveToGrave = c.MoveToGrave[:len(c.MoveToGrave):len(c.MoveToGrave)]
	c.Unknown = c.Unknown[:len(c.Unknown):len(c.Unknown)]
	return c
}

func (e *Engine) parseCost(raw string) Cost {
	if e != nil && e.compiledText != nil {
		if c, ok := e.compiledText.costs[raw]; ok {
			return c.Cost
		}
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
	slot := f.ManaCostSlot()
	if v, ok := slot.Load(f.ManaCost); ok {
		return v.(*compiledCost)
	}
	c := e.compiledCostOf(f.ManaCost)
	if slot == nil {
		return c
	}
	return slot.Store(f.ManaCost, c).(*compiledCost)
}

// freeCost is the parse of an empty cost text, shared read-only by costRef.
var freeCost compiledCost

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
	if e != nil && e.compiledText != nil {
		if c, ok := e.compiledText.costs[raw]; ok {
			return c
		}
	}
	if raw == "" {
		return &freeCost
	}
	return newCompiledCost(raw)
}

// matchesSpecFrom is the engine-owned form of effects.MatchesSpecFrom. It
// preserves the public helper's source-relative semantics while carrying this
// engine's immutable predicate programs into configured filter evaluation.
func (e *Engine) matchesSpecFrom(spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool {
	return e.matchesSpec(spec, id, e.specCtx(source, you))
}
