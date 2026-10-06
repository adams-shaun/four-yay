// Package templates holds the level-A oracle scenario templates, one file
// per template, each with its own version
// (docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section
// 11.3 C3). A template's version is part of its scenarios' ids and names,
// so bumping it stales only that template's verdicts; a change to the
// shared core (package oraclegen) stales exactly the scenarios whose bytes
// it changes, through the verdict's scenario hash.
//
// A new template is a new file here: its Template value, its builder, and
// one line in Generate's dispatch.
package templates

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Template identifies one scenario template.
type Template struct {
	ID      string // the verdict row's template
	Version int    // bump when the template's output changes
}

func (t Template) item(f *cards.Face, card string, sc oraclegen.Scenario) oraclegen.Item {
	return oraclegen.NewItem(f, card, t.ID, t.Version, sc)
}

// All lists every template.
var All = []Template{PlayLand, CastResolve, CounterSpell}

// Generate builds the level-A scenario for one card, or says why not: a
// land is played, a spell that targets a spell counters one of ours, and
// any other spell is cast and resolved.
func Generate(reg *cards.Registry, name string) (oraclegen.Item, *oraclegen.Skip) {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "not in corpus"}
	}
	f := requestedFace(c, name)
	if oraclegen.HasType(f, "Land") {
		return playLand(reg, name, f), nil
	}
	mana, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: why}
	}
	if targetsSpell(f) {
		return counterSpell(reg, f, name, mana)
	}
	return castResolve(reg, f, name, mana)
}
