// Package registry names the SpellBench policies and decorators
// -spellbench plays. A policy name resolves to a seat factory; a decorator
// name resolves to a wrapper that may answer a decision itself or delegate
// it to the seat it wraps. A policy spec is `base` or `base+dec1+dec2`; the
// spec string is the policy's name in the match ledger, so a composed
// candidate shows up under its composed name.
//
// Registration is one file plus one call from init(): a new policy adds a
// file in this package (or in cmd/botbench, for the names that need the
// command's own state) with a single init() calling Register (or
// RegisterDecorator); nothing else changes. The SpellBench builtins
// (sb-*, sb.go) are registered here; "bot" and "az" are registered by
// cmd/botbench because they need that package's hosted-policy and
// azmcts wiring.
//
// The factories take the mana surface (builtins.ManaMode) so a future
// caller can play the plain sb-* names on another surface; -spellbench
// itself always builds the plain names at builtins.AutoPay (v2
// "engine_autopay") and reaches the other surfaces through their dedicated
// -manual/-planned names, exactly as before the registry existed.
package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
)

// Factory builds one seat of one game from that seat's per-game seed, at the
// given mana surface. A factory's decisions are a pure function of (seed,
// mana surface) and nothing else.
type Factory func(seed uint64, mana builtins.ManaMode) seat.Seat

// Decorator wraps a base seat into a decorated one. It may answer a
// decision itself or delegate it to inner; seed is the seat's per-game seed
// (the same seed the base factory got), for decorators that need their own
// stream.
//
// A decorator that DELEGATES should implement Unwrapper (passguard does):
// a runner that inspects the wrapped seat -- cmd/botbench's refused-answer
// fallback and stats collection reach the *builtins.Seat underneath -- can
// then see through the decoration instead of treating a decorated builtin
// as some opaque seat the fallback does not cover.
type Decorator func(inner seat.Seat, seed uint64) seat.Seat

// cardLookup resolves a card name to its compiled IR, for decorators that
// read printed card facts (lethal's burn and pump classification). It is
// nil until the host wires the run's corpus (SetCardLookup); a decorator
// that needs it treats nil as "card unknown" and delegates. It is written
// once before any seat is built and read-only afterwards, the same
// write-once package-scope pattern as cmd/botbench's own registries.
var cardLookup func(name string) *cards.Card

// SetCardLookup installs the process-wide card lookup. cmd/botbench wires
// the run's corpus once before any seat is built; a test may install its
// own lookup (the last writer wins) since package tests run sequentially.
// The write-once discipline is by convention, not enforced: the wiring must
// happen before any match starts (the engine tier is single-goroutine per
// match, but a lookup installed mid-run would race with readers across
// concurrent matches), so an embedder other than cmd/botbench must wire it
// at startup or leave it unset.
// The value is read-only once games start. A decorator sees nil until a
// caller wires it and must treat nil as "card unknown".
func SetCardLookup(l func(name string) *cards.Card) {
	cardLookup = l
}

// Unwrapper is the seat contract a delegating decorator implements to
// expose the seat it wraps.
type Unwrapper interface {
	UnwrapSeat() seat.Seat
}

// UnwrapSeat returns the base seat underneath a decorated one: it follows
// Unwrapper seats until it reaches one that is not, guarding against a
// decorator that wraps itself. An undecorated seat is returned as-is.
func UnwrapSeat(s seat.Seat) seat.Seat {
	for {
		u, ok := s.(Unwrapper)
		if !ok {
			return s
		}
		inner := u.UnwrapSeat()
		if inner == nil || inner == s {
			return s
		}
		s = inner
	}
}

var (
	// factories and decorators share one namespace: a name registers as
	// exactly one of the two, so a spec's grammar (`base+dec`) is
	// unambiguous.
	factories  = map[string]Factory{}
	decorators = map[string]Decorator{}
)

// Register installs a policy under name. Both maps share one namespace; a
// double registration is a programming error and panics.
func Register(name string, f Factory) {
	if name == "" || f == nil {
		panic("registry: Register needs a name and a factory")
	}
	if _, dup := factories[name]; dup {
		panic(fmt.Sprintf("registry: policy %q registered twice", name))
	}
	if _, dup := decorators[name]; dup {
		panic(fmt.Sprintf("registry: name %q is already a decorator", name))
	}
	factories[name] = f
}

// RegisterDecorator installs a decorator under name (same namespace as
// Register).
func RegisterDecorator(name string, d Decorator) {
	if name == "" || d == nil {
		panic("registry: RegisterDecorator needs a name and a decorator")
	}
	if _, dup := decorators[name]; dup {
		panic(fmt.Sprintf("registry: decorator %q registered twice", name))
	}
	if _, dup := factories[name]; dup {
		panic(fmt.Sprintf("registry: name %q is already a policy", name))
	}
	decorators[name] = d
}

// Names is the sorted policy vocabulary, for error messages.
func Names() []string {
	out := make([]string, 0, len(factories))
	for name := range factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// DecoratorNames is the sorted decorator vocabulary, for error messages.
func DecoratorNames() []string {
	out := make([]string, 0, len(decorators))
	for name := range decorators {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Has reports whether spec is a valid policy spec (base plus registered
// decorators), without building a seat.
func Has(spec string) bool {
	_, err := parse(spec)
	return err == nil
}

// CheckSpec validates spec and returns the error that names what is wrong
// -- an unknown base or decorator, with the registered names listed.
func CheckSpec(spec string) error {
	_, err := parse(spec)
	return err
}

// Build resolves a spec to a seat at the -spellbench surface (AutoPay; see
// the package doc). seed is the seat's per-game seed; every decorator wraps
// left to right, so `base+d1+d2` builds base, wraps it in d1, and wraps
// that in d2.
func Build(spec string, seed uint64) (seat.Seat, error) {
	return build(spec, seed, builtins.AutoPay)
}

func build(spec string, seed uint64, mana builtins.ManaMode) (seat.Seat, error) {
	parts, err := parse(spec)
	if err != nil {
		return nil, err
	}
	s := factories[parts[0]](seed, mana)
	for _, name := range parts[1:] {
		s = decorators[name](s, seed)
	}
	return s, nil
}

// parse splits a spec into base and decorators and checks every part.
func parse(spec string) ([]string, error) {
	parts := strings.Split(spec, "+")
	if parts[0] == "" {
		return nil, fmt.Errorf("registry: spec %q has no base policy; registered policies: %s",
			spec, strings.Join(Names(), ", "))
	}
	if _, ok := factories[parts[0]]; !ok {
		return nil, fmt.Errorf("registry: unknown policy %q; registered policies: %s",
			parts[0], strings.Join(Names(), ", "))
	}
	for _, name := range parts[1:] {
		if name == "" {
			return nil, fmt.Errorf("registry: spec %q has an empty decorator; registered decorators: %s",
				spec, strings.Join(DecoratorNames(), ", "))
		}
		if _, ok := decorators[name]; !ok {
			return nil, fmt.Errorf("registry: unknown decorator %q (in spec %q); registered decorators: %s",
				name, spec, strings.Join(DecoratorNames(), ", "))
		}
	}
	return parts, nil
}
