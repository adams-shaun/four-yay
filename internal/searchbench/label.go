package searchbench

import (
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/state"
)

// ItemLabel is the human's answer in canonical indices.
type ItemLabel struct {
	// Members is upstream's "label": every acceptable canonical answer
	// (spell: the turn's legal casts and activations, plus Pass when the
	// human attacked -- the lenient timing convention).
	Members []int
	// Strict is upstream's "label_strict" (spell: Members without Pass).
	Strict []int
	// Act is whether the human acted (a non-passive strict member).
	Act  bool
	Note string
}

// PrecheckKind applies make_item's label-only gates before any build:
// spell needs a keyed cast or activation ("no cast"); hold and attack need
// none ("cast"); attack needs a recorded attack question ("no attack
// question recorded"). Upstream's fidelity gate (provenance tier and risky
// flags) is applied by the preparation side, not here.
func PrecheckKind(spec *statespec.Spec, kind DecisionType) error {
	l, err := spec.ParseLabels()
	if err != nil {
		return refuse("labels", "%v", err)
	}
	acts := l.Acts()
	switch kind {
	case DecisionSpell:
		if len(acts) == 0 {
			return refuse("no cast", "")
		}
	case DecisionHold, DecisionAttack:
		if len(acts) > 0 {
			return refuse("cast", "")
		}
		if kind == DecisionAttack && len(l.Attacks) == 0 {
			return refuse("no attack question recorded", "")
		}
	}
	return nil
}

var reminder = regexp.MustCompile(`\s*<i>\(.*?\)</i>`)

// matchLabel is coach.match_label over the canon's labels: exact; modulo
// XMage reminder text; an ability text without its cost; any case. It
// returns the canonical index or -1.
func matchLabel(want string, labels []string) int {
	want = strings.TrimSpace(want)
	for i, l := range labels {
		if l == want {
			return i
		}
	}
	for i, l := range labels {
		if reminder.ReplaceAllString(l, "") == want {
			return i
		}
	}
	for i, l := range labels {
		if j := strings.Index(l, ": "); j > 0 && l[j+2:] == want {
			return i
		}
	}
	for i, l := range labels {
		if strings.EqualFold(l, want) {
			return i
		}
	}
	return -1
}

var (
	nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	// selfRef is the card's reference to itself: XMage's "{this}", Forge's
	// CARDNAME/NICKNAME.
	selfRef = regexp.MustCompile(`\{this\}|CARDNAME|NICKNAME`)
)

func normText(s string) string {
	s = selfRef.ReplaceAllString(reminder.ReplaceAllString(s, ""), "")
	return nonAlnum.ReplaceAllString(strings.ToLower(s), "")
}

// matchAct maps one recorded land/cast/activation onto a canonical index
// (-1 when it is not legal here). Casts and lands match by label
// ("Cast X", "Play X"); a cast whose plain cost is not offered falls back to
// a unique alternative-mode cast of the same card (upstream's flashback
// fallback). Activations cannot match by label -- the key is XMage rule
// text, the canon's is gorge's "<source>: <description>" -- so they match by
// source name (a token source "Food" is gorge's "Food Token"): the one
// ability of that source, or the one whose description equals the key's
// rule text (with or without its cost) modulo reminder text, the card's
// self-reference, case and punctuation.
func (c *Canon) matchAct(a statespec.LabelAct, activation bool) int {
	if !activation {
		if i := matchLabel(a.Key, c.Labels()); i >= 0 {
			return i
		}
		if name, ok := strings.CutPrefix(a.Key, "Cast "); ok {
			hit := -1
			for i, o := range c.Options {
				if o.Kind == "cast" && o.Name == name {
					if hit >= 0 {
						return -1
					}
					hit = i
				}
			}
			return hit
		}
		return -1
	}
	var cand []int
	for i, o := range c.Options {
		if o.Kind == "ability" && (o.Name == a.Source || o.Name == a.Source+" Token") {
			cand = append(cand, i)
		}
	}
	if len(cand) == 1 {
		return cand[0]
	}
	want, wantText := normText(a.Key), ""
	if j := strings.Index(a.Key, ": "); j > 0 {
		wantText = normText(a.Key[j+2:]) // the rule text without its cost
	}
	hit := -1
	for _, i := range cand {
		l := c.Options[i].Label
		if j := strings.Index(l, ": "); j > 0 && (normText(l[j+2:]) == want || normText(l[j+2:]) == wantText) {
			if hit >= 0 {
				return -1
			}
			hit = i
		}
	}
	return hit
}

func addUnique(xs []int, v int) []int {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

// LabelItem matches the spec's 17lands labels onto the canon, porting
// items.py make_item + coach.human_17lands / match_label:
//
//   - trivial: fewer than 2 canonical options.
//   - spell: the human's set is the turn's lands, casts and activations
//     (Pass added when the human attacked); members are the ones legal here.
//     Strict drops Pass and must be non-empty ("no human cast legal here").
//   - hold: members must be exactly [Pass] ("hold label"), and some card
//     must be castable ("nothing castable").
//   - attack: the recorded attack of X, by name as upstream asks "attack
//     with: <name>?" (every labelled creature of X's name must agree), kept
//     only when the defender is open (an untapped creature or untapped basic
//     land, upstream's defender_open).
//   - block: the recorded block of X, by name (every labelled blocker of
//     X's name must be exact and agree); members are every canonical block
//     of an attacker of the labelled name (upstream's target labels are
//     names, so same-named attackers are one answer).
func LabelItem(m *Materialized, c *Canon) (ItemLabel, error) {
	var out ItemLabel
	if len(c.Options) < 2 {
		return out, refuse("trivial", "%d options", len(c.Options))
	}
	l, err := m.Spec.ParseLabels()
	if err != nil {
		return out, refuse("labels", "%v", err)
	}
	e := m.Engine
	switch c.Kind {
	case DecisionSpell, DecisionHold:
		if !l.TurnLabel() {
			return out, refuse("label", "17lands has no timing for plays in the opponent's turn")
		}
		type act struct {
			a          statespec.LabelAct
			activation bool
		}
		var acts []act
		for _, x := range l.KeyedLands() {
			acts = append(acts, act{x, false})
		}
		for _, x := range l.Casts {
			if x.Key != "" {
				acts = append(acts, act{x, false})
			}
		}
		for _, x := range l.Activations {
			if x.Key != "" {
				acts = append(acts, act{x, true})
			}
		}
		attacked := l.Attacked()
		pass := len(acts) == 0 || attacked
		if len(acts) == 0 {
			out.Note = "no land, cast or activation this turn"
		} else if attacked {
			out.Note = "Pass included: the user attacked, so some of these may have come after combat"
		}
		for _, x := range acts {
			if i := c.matchAct(x.a, x.activation); i >= 0 {
				out.Members = addUnique(out.Members, i)
			}
		}
		if pass {
			out.Members = addUnique(out.Members, 0)
		}
		for _, i := range out.Members {
			if i != 0 || c.Kind == DecisionHold {
				out.Strict = append(out.Strict, i)
			}
		}
		if c.Kind == DecisionSpell {
			if len(out.Strict) == 0 {
				return out, refuse("no human cast legal here", "")
			}
			if attacked {
				out.Members = addUnique(out.Members, 0)
			}
			out.Act = true
		} else {
			if len(out.Members) != 1 || out.Members[0] != 0 {
				return out, refuse("hold label", "%v", out.Members)
			}
			castable := false
			for _, o := range c.Options {
				castable = castable || o.Kind == "cast"
			}
			if !castable {
				return out, refuse("nothing castable", "")
			}
		}
	case DecisionAttack:
		name := objName(e, c.Focus)
		ans := map[bool]bool{} // lookup only
		for a, v := range l.Attacks {
			n, isNew := strings.CutPrefix(a, "new:")
			if !isNew {
				id, ok := m.Alias[a]
				if !ok {
					continue
				}
				n = objName(e, id)
			}
			if n == name {
				ans[v] = true
			}
		}
		if len(ans) != 1 {
			return out, refuse("label", "attack of '%s' not recorded unambiguously (%d answers)", name, len(ans))
		}
		if ans[true] {
			out.Members, out.Act = []int{1}, true
		} else {
			out.Members = []int{0}
		}
		out.Strict = out.Members
		if !defenderOpen(m, 1-c.Decision.Player) {
			return out, refuse("defender closed", "")
		}
	case DecisionBlock:
		name := objName(e, c.Focus)
		var rows []statespec.BlockLabel
		for _, b := range l.Blocks {
			if id, ok := m.Alias[b.Blocker]; ok && objName(e, id) == name {
				rows = append(rows, b)
			}
		}
		if len(rows) == 0 {
			return out, refuse("label", "no block label for '%s'", name)
		}
		for _, b := range rows {
			if !b.Exact {
				return out, refuse("label", "block of '%s' not exact (turn pairing %s)", name, l.BlockPairing)
			}
		}
		ans := map[string]bool{} // lookup only
		for _, b := range rows {
			if b.Attacker == "" {
				ans[""] = true
			} else if id, ok := m.Alias[b.Attacker]; ok {
				ans[objName(e, id)] = true
			} else {
				ans["?"] = true
			}
		}
		if len(ans) != 1 {
			return out, refuse("label", "copies of '%s' blocked differently", name)
		}
		var want string
		for k := range ans {
			want = k
		}
		if want == "" {
			out.Members = []int{0}
		} else {
			for i, o := range c.Options {
				if o.Kind == "block" && o.Name == want {
					out.Members = append(out.Members, i)
				}
			}
			out.Act = true
		}
		out.Strict = out.Members
	}
	if len(out.Members) == 0 {
		return out, refuse("human unmatched", "")
	}
	sort.Ints(out.Members)
	out.Strict = append([]int(nil), out.Strict...)
	sort.Ints(out.Strict)
	return out, nil
}

var basicLands = map[string]bool{"Plains": true, "Island": true, "Swamp": true, "Mountain": true, "Forest": true}

// defenderOpen ports items.py defender_open: the defending seat has an
// untapped creature or an untapped basic land (a real attack question).
// Upstream reads XMage's dump (a creature's canBlock, or a power and not
// tapped); gorge reads the engine's creature test on the untapped
// permanents.
func defenderOpen(m *Materialized, p state.PlayerID) bool {
	e := m.Engine
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Tapped {
			continue
		}
		if e.IsCreature(id) || basicLands[o.Face().Name] {
			return true
		}
	}
	return false
}
