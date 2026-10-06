package oraclediff

import (
	"sort"
	"strings"
	"unicode"

	"github.com/adams-shaun/gorge/rules"
)

// offeredKeys is shared by comparison, canonical output and frozen views.
// Source/kind is load-bearing. A singleton needs no label; multiple abilities
// retain normalized labels to distinguish, for example, Add B from Add R.
func offeredKeys(offers []rules.OracleSnapOffered) string {
	entries := append([]rules.OracleSnapOffered(nil), offers...)
	for i := range entries {
		entries[i].Label = offeredLabel(entries[i].Label)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Label < b.Label
	})
	parts := make([]string, 0, len(entries))
	for i, o := range entries {
		label := o.Label
		prev := i > 0 && entries[i-1].Source == o.Source && entries[i-1].Kind == o.Kind
		next := i+1 < len(entries) && entries[i+1].Source == o.Source && entries[i+1].Kind == o.Kind
		if !prev && !next {
			label = ""
		}
		parts = append(parts, o.Source+"|"+o.Kind+"|"+label)
	}
	return strings.Join(parts, "\n")
}

func offeredLabel(label string) string {
	// XMage includes the activation cost before the rule text. Gorge's named
	// mana reader emits the effect alone ("Add R"). Costs are not identity.
	if before, after, ok := strings.Cut(label, ":"); ok && strings.Contains(before, "{") {
		label = after
	}
	label = strings.ToLower(strings.TrimSpace(label))
	for _, prefix := range []string{"cast ", "activate ", "play "} {
		label = strings.TrimPrefix(label, prefix)
	}
	return strings.Map(func(r rune) rune {
		if r == '{' || r == '}' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, label)
}

func fieldEqual(a, b field) bool {
	if a.name != b.name {
		return false
	}
	if a.name == "offered" {
		return offeredEqual(a.value, b.value)
	}
	return a.value == b.value
}

// offeredEqual also serves Meets: normalized rule-text prefixes can agree
// without having identical canonical strings. Match one-to-one, not by list
// position: an advisory prefix can overlap two labels, and a greedy election
// could miss a valid matching or reuse one ability to hide a missing ability.
func offeredEqual(a, b string) bool {
	if a == b {
		return true
	}
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(x) != len(y) {
		return false
	}
	matched := make([]int, len(y))
	for i := range matched {
		matched[i] = -1
	}
	var match func(int, []bool) bool
	match = func(i int, seen []bool) bool {
		for j := range y {
			if seen[j] || !offeredEntryMatches(x[i], y[j]) {
				continue
			}
			seen[j] = true
			if matched[j] == -1 || match(matched[j], seen) {
				matched[j] = i
				return true
			}
		}
		return false
	}
	for i := range x {
		if !match(i, make([]bool, len(y))) {
			return false
		}
	}
	return true
}

func offeredEntryMatches(a, b string) bool {
	x, y := strings.SplitN(a, "|", 3), strings.SplitN(b, "|", 3)
	if len(x) != 3 || len(y) != 3 || x[0] != y[0] || x[1] != y[1] {
		return false
	}
	xl, yl := offeredLabel(x[2]), offeredLabel(y[2])
	return strings.HasPrefix(xl, yl) || strings.HasPrefix(yl, xl)
}
