package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// validSelfArg extracts the Card$ argument of every `Count$ValidSelf` body in
// a parameter value, stripping the Count$ /Op suffix so what is recorded is the
// property family. Returns one entry per occurrence.
func validSelfArgs(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "Count$ValidSelf")
		if i < 0 {
			return out
		}
		rest := s[i+len("Count$ValidSelf"):]
		s = rest
		arg := strings.TrimSpace(rest)
		if j := strings.IndexAny(arg, " |"); j >= 0 { // end at the next param or whitespace
			arg = arg[:j]
		}
		if j := strings.IndexByte(arg, '/'); j >= 0 { // strip the /Op cap
			arg = arg[:j]
		}
		if arg != "" {
			out = append(out, arg)
		}
	}
}

// sortedParamKeys returns a map's keys in sorted order so the walk below is
// deterministic.
func sortedParamKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// TestCountValidSelfCensus records every corpus carrier of the
// `Count$ValidSelf` head and the Card$ argument family each one passes.
//
// Only the `Card$CreatureType` family is modelled (Diligent Zookeeper's
// +1/+1-per-creature-type static, effects/count.go's ValidSelf case). The
// other carriers read a DIFFERENT property off the source object and
// deliberately stay fail-closed to the unresolvable verdict:
//
//   - `Card.!IsPrepared` -- the prepared mechanic's per-object boolean
//     (Woodwork Prodigy, Paradox Shaper, Stingerquill Voxmancer), an
//     upkeep-trigger CheckSVar gate, not a type count.
//   - `Card.IsSuspected` -- the suspected designation (Frantic Scapegoat),
//     another per-object boolean gate.
//   - `Creature.greatestPowerControlledByCardController` (Kraven the
//     Hunter) -- not even a `Card$` argument: a greatest-power reduction over
//     a controlled set, the shape `Count$Valid <spec>$GreatestCardPower`
//     models under a Valid* head, NOT a self read.
//
// This census is a ratchet over the pinned corpus: a NEW ValidSelf argument
// family, or a carrier joining an existing one, fails here so the coverage
// claim in effects/count.go cannot silently go stale. The count is the
// measured number, not a claim copied from a brief.
func TestCountValidSelfCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	type carrier struct {
		card string
		arg  string
	}
	seen := map[string]bool{}
	var carriers []carrier
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f.Name == "" {
				continue
			}
			var args []string
			for _, k := range sortedParamKeys(f.SVars) {
				args = append(args, validSelfArgs(f.SVars[k])...)
			}
			for _, tr := range f.Triggers {
				for _, k := range sortedParamKeys(tr.Params) {
					args = append(args, validSelfArgs(tr.Params[k])...)
				}
			}
			for _, st := range f.Statics {
				for _, k := range sortedParamKeys(st.Params) {
					args = append(args, validSelfArgs(st.Params[k])...)
				}
			}
			for _, rp := range f.Repls {
				for _, k := range sortedParamKeys(rp.Params) {
					args = append(args, validSelfArgs(rp.Params[k])...)
				}
			}
			sort.Strings(args)
			for _, arg := range args {
				key := f.Name + "\x00" + arg
				if seen[key] {
					continue
				}
				seen[key] = true
				carriers = append(carriers, carrier{card: f.Name, arg: arg})
			}
		}
	}
	sort.Slice(carriers, func(i, j int) bool {
		if carriers[i].card != carriers[j].card {
			return carriers[i].card < carriers[j].card
		}
		return carriers[i].arg < carriers[j].arg
	})

	// Precondition: the census must actually see the carriers, or every
	// assertion below is vacuous. The pinned corpus carries exactly six
	// files, one ValidSelf body each.
	if len(carriers) != 6 {
		t.Fatalf("Count$ValidSelf carriers = %d (%+v), want 6 -- the corpus pin moved or a carrier was added/removed; re-measure and update this census", len(carriers), carriers)
	}

	wantByCard := map[string]string{
		"Diligent Zookeeper":     "Card$CreatureType",
		"Frantic Scapegoat":      "Card.IsSuspected",
		"Kraven the Hunter":      "Creature.greatestPowerControlledByCardController",
		"Paradox Shaper":         "Card.!IsPrepared",
		"Stingerquill Voxmancer": "Card.!IsPrepared",
		"Woodwork Prodigy":       "Card.!IsPrepared",
	}
	gotByCard := map[string]string{}
	for _, c := range carriers {
		if prev, dup := gotByCard[c.card]; dup && prev != c.arg {
			t.Errorf("%s carries two ValidSelf argument families (%q and %q); this census assumes one per face", c.card, prev, c.arg)
		}
		gotByCard[c.card] = c.arg
	}
	for card, want := range wantByCard {
		got, ok := gotByCard[card]
		if !ok {
			t.Errorf("%s carries no Count$ValidSelf body, want argument %q", card, want)
			continue
		}
		if got != want {
			t.Errorf("%s Count$ValidSelf argument = %q, want %q", card, got, want)
		}
	}
	t.Logf("Count$ValidSelf census (%d carriers): %+v", len(carriers), carriers)
}
