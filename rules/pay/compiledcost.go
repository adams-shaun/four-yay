package pay

import (
	"reflect"
	"sync/atomic"

	costvocab "github.com/adams-shaun/gorge/rules/cost"
)

// CompiledCost is one configured cost text's frozen parse plus the facts
// the hot read-only callers ask of it. The engine's compiled-text sidecar
// holds one per configured text (Engine.ConfiguredCost); every engine
// sharing the sidecar reads the same value, so it is never written after
// construction.
type CompiledCost struct {
	Cost
	// BareTap: the text is exactly {T} -- Tap set and every other component
	// zero -- the cost of nearly every mana ability. Mana-ability payability
	// prices it without the generic payability walk.
	BareTap bool
	// BeyondTap caches ManaCostBeyondTap(Cost) (the fb-led1 marker test).
	BeyondTap bool
	// text memoizes FormatCost(Cost) (the mana activation's cost marker):
	// the Cost is frozen, so its text never changes; set once, read by any
	// engine.
	text atomic.Pointer[string]
}

// Formatted is FormatCost(cc.Cost), memoized on the frozen cost.
func (cc *CompiledCost) Formatted() string {
	if t := cc.text.Load(); t != nil {
		return *t
	}
	t := costvocab.FormatCost(cc.Cost)
	cc.text.Store(&t)
	return t
}

// NewCompiledCost parses and freezes text.
func NewCompiledCost(text string) *CompiledCost {
	c := FreezeCost(costvocab.ParseCost(text))
	return &CompiledCost{Cost: c, BareTap: costIsBareTap(&c), BeyondTap: costvocab.ManaCostBeyondTap(c)}
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

// FreeCost is the parse of an empty cost text, shared read-only.
var FreeCost CompiledCost

// CompiledCostFor is the compiled cost of raw: the engine's configured entry
// c when it has one, else the shared free cost for an empty text, else a
// fresh frozen parse (a runtime-built cost string).
func CompiledCostFor(c *CompiledCost, raw string) *CompiledCost {
	if c != nil {
		return c
	}
	if raw == "" {
		return &FreeCost
	}
	return NewCompiledCost(raw)
}

// CompiledCostOf is CompiledCostFor over e's configured table. The result is
// READ-ONLY: not a field, not an element of one of its slices may be written.
func CompiledCostOf(e Engine, raw string) *CompiledCost {
	return CompiledCostFor(e.ConfiguredCost(raw), raw)
}

// CostRef is ParseCostOf for a READ-ONLY caller: the configured text's
// shared frozen parse, not copied. The same no-write contract applies.
func CostRef(e Engine, raw string) *Cost { return &CompiledCostOf(e, raw).Cost }

// ParseCostOf is the cost raw parses to, as a value the caller may modify:
// the configured entry's copy, else a fresh parse.
func ParseCostOf(e Engine, raw string) Cost {
	if c := e.ConfiguredCost(raw); c != nil {
		return c.Cost
	}
	return costvocab.ParseCost(raw)
}

// FreezeCost caps every slice of c at its length, so an append to a copy
// reallocates instead of writing into the shared frozen backing array.
func FreezeCost(c Cost) Cost {
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
