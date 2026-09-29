// Package bots provides the closed registry of policies available to callers.
package bots

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/seat"
)

type Tier string

const (
	Production           Tier = "production"
	Experimental         Tier = "experimental"
	Default                   = "bot"
	BotPolicy                 = Default
	LethalPressurePolicy      = "lethal-pressure"
	CastProfilePolicy         = "cast-profile"
)

type Measurement struct {
	Claim, Versus, Setting, Source string
}

type Cost struct {
	MeanMS, P95MS       float64
	Scope, Note, Source string
}

type Info struct {
	Name, Label, Description string
	Tier                     Tier
	Strength                 []Measurement
	Cost                     Cost
	Env, Search              bool
	Formats                  []string
	MaxSeats                 int
	Caretaker                string
}

type Options struct {
	Seed              uint64
	AutoPayMana       bool
	SearchParallelism int
	Deps              Deps
}

type Deps struct{ Cards *cards.Registry }
type Factory func(Options) (seat.Seat, error)
type Entry struct {
	Info
	New Factory
}

var entries = map[string]Entry{}
var validName = regexp.MustCompile(`^[a-z0-9-]+$`)

func Register(e Entry) {
	if !validName.MatchString(e.Name) || e.New == nil || (e.Tier != Production && e.Tier != Experimental) {
		panic(fmt.Sprintf("bots: invalid entry %q", e.Name))
	}
	if _, exists := entries[e.Name]; exists {
		panic("bots: duplicate entry " + e.Name)
	}
	entries[e.Name] = e
}

func Lookup(name string) (Entry, bool) { e, ok := entries[name]; return e, ok }

func Normalize(name string) (string, error) {
	if name == "" {
		name = Default
	}
	if _, ok := Lookup(name); ok {
		return name, nil
	}
	return "", fmt.Errorf("bots: unknown bot policy %q (known: %s)", name, joinNames())
}

func New(name string, o Options) (seat.Seat, error) {
	name, err := Normalize(name)
	if err != nil {
		return nil, err
	}
	return entries[name].New(o)
}

func Names() []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Entries() []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier == Production
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func Validate() error {
	for _, e := range Entries() {
		if e.Caretaker == "" {
			continue
		}
		caretaker, ok := Lookup(e.Caretaker)
		if !ok {
			return fmt.Errorf("bots: %q caretaker %q is not registered", e.Name, e.Caretaker)
		}
		if caretaker.Env {
			return fmt.Errorf("bots: %q caretaker %q requires Env", e.Name, e.Caretaker)
		}
	}
	return nil
}

func joinNames() string {
	names := Names()
	ordered := make([]string, 0, len(names))
	for _, preferred := range []string{Default, LethalPressurePolicy} {
		if _, ok := Lookup(preferred); ok {
			ordered = append(ordered, preferred)
		}
	}
	for _, name := range names {
		if name != Default && name != LethalPressurePolicy {
			ordered = append(ordered, name)
		}
	}
	return strings.Join(ordered, ", ")
}
