package rules

import (
	"reflect"
	"sort"
	"sync"

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
}

// compiledCost is one configured cost text's frozen parse plus the facts
// the hot read-only callers ask of it.
type compiledCost struct {
	Cost
	// bareTap: the text is exactly {T} -- Tap set and every other component
	// zero -- the cost of nearly every mana ability. manaAbilityCostPayable
	// prices it without the generic payability walk.
	bareTap bool
}

func newCompiledCost(text string) *compiledCost {
	c := freezeCost(ParseCost(text))
	return &compiledCost{Cost: c, bareTap: costIsBareTap(&c)}
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
	config compiledTextConfig
	text   *compiledText
}

var compiledTextCache = struct {
	sync.Mutex
	entries map[*cards.Card][]compiledTextCacheEntry
}{entries: make(map[*cards.Card][]compiledTextCacheEntry)}

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
	compiledTextCache.entries[key] = append(compiledTextCache.entries[key], compiledTextCacheEntry{
		config: config,
		text:   text,
	})
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

func buildCompiledText(cfg Config) *compiledText {
	predicateTexts := make(map[string]struct{})
	costTexts := make(map[string]struct{})
	seen := make(map[*cards.SA]struct{})
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
	addCard := func(c *cards.Card) {
		if c == nil {
			return
		}
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			if f.ManaCost != "" {
				costTexts[f.ManaCost] = struct{}{}
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
	return &compiledText{predicates: effects.CompilePredicatePrograms(preds), costs: costs}
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
