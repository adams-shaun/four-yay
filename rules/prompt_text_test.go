package rules

import (
	"regexp"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Text quality is a server contract (client-table UI rework spec §4,
// docs/superpowers/specs/2026-09-28-client-table-ui-rework-design.md): a
// decision's Prompt -- the question title a client displays -- and its option
// labels must never carry raw Forge cost or filter syntax. Players saw
// "Pay Sac<1/Creature.Other/another creature>"; this file is the gate that
// keeps that class out.

// engineSyntaxPatterns is the one table of forbidden shapes. Each pattern is
// something English prompt text never contains but Forge script syntax does.
var engineSyntaxPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	// A cost-part token: Sac<1/Creature>, SubCounter<1/P1P1>, tapXType<2/Elf>,
	// Discard<1/Card>, Exile<1/Card.YouOwn>, PayLife<2>, Return<1/Land>.
	// English never puts '<' straight after a word.
	{"cost-part token Word<...>", regexp.MustCompile(`[A-Za-z]<`)},
	// A script parameter key: ValidTgts$, Defined$, Count$xPaid.
	{"script parameter Key$", regexp.MustCompile(`[A-Za-z]\$`)},
	// A dotted filter: Creature.Other, Card.YouCtrl, Permanent.nonLand,
	// .YouCtrl. English puts a space after a sentence's full stop, and a
	// card name's abbreviations (B.F.M.) are upper-dot-upper, so a dot
	// touching a lowercase letter on either side without a space is script.
	{"dotted filter Type.Prop", regexp.MustCompile(`[a-z]\.[A-Za-z]|[A-Za-z]\.[a-z]|(^|[\s(])\.[A-Za-z]`)},
	// A '+'-joined filter property: Creature.Other+withFlying, +YouCtrl.
	{"joined filter property +prop", regexp.MustCompile(`[A-Za-z]\+[A-Za-z]|\+(with|non|You|Opp|Other|Attached)[A-Z]`)},
	// Bare script vocabulary that names engine plumbing, never a game idea.
	{"script keyword", regexp.MustCompile(`\b(ValidTgts|ValidCards|TgtPrompt|ChoiceTitle|SVars?|AB\$|SP\$|DB\$|UnlessCost|SubAbility)\b`)},
}

// allowedDotted are the few English abbreviations whose dot legitimately
// touches a lowercase letter
// ("Big Apple, 3 a.m." is a real card name a NameCard ask offers).
var allowedDotted = []string{"e.g.", "i.e.", "a.m.", "p.m."}

// engineSyntaxIn reports the name of the first forbidden pattern text
// matches, or "" when the text is clean.
func engineSyntaxIn(text string) string {
	scrubbed := text
	for _, a := range allowedDotted {
		scrubbed = strings.ReplaceAll(scrubbed, a, " ")
	}
	for _, p := range engineSyntaxPatterns {
		if p.re.MatchString(scrubbed) {
			return p.name
		}
	}
	return ""
}

func TestEngineSyntaxChecker(t *testing.T) {
	bad := []string{
		"Pay Sac<1/Creature.Other/another creature>",
		"Pay SubCounter<1/P1P1>, or decline",
		"Pay tapXType<2/Elf>",
		"Discard<1/Card>",
		"Pay Exile<1/Card.YouOwn>",
		"Pay PayLife<2>",
		"Return<1/Land.YouCtrl>",
		"Choose ValidTgts$ Creature",
		"Defined$ You",
		"Choose a Creature.Other",
		"Choose Card.YouCtrl",
		"Destroy target Permanent.nonLand",
		"target .YouCtrl creature",
		"Creature.Other+withFlying",
		"Creature+withFlying",
		"TgtPrompt",
		"Select ValidTgts",
		"SVar missing",
	}
	good := []string{
		"Pay {1}",
		"Pay {2}{U}, or decline",
		"Pay 2 life",
		"Put a +1/+1 counter on target creature",
		"Sacrifice another creature",
		"Choose target creature you control",
		"Cast Mr. Orfeo, the Boulder?",
		"Big Apple, 3 a.m.",
		"B.F.M. (Big Furry Monster)",
		"Draw a card. Then discard a card.",
		"Search your library (e.g. for a land)",
		"Pay the replicate cost how many times?",
		"Pay 3 to block -- tap a mana source",
		"Choose a number: 0-5",
		"Deal 2 damage — accept?",
		"",
	}
	for _, s := range bad {
		if engineSyntaxIn(s) == "" {
			t.Errorf("checker passed engine syntax %q", s)
		}
	}
	for _, s := range good {
		if n := engineSyntaxIn(s); n != "" {
			t.Errorf("checker flagged English %q as %s", s, n)
		}
	}
}

// TestEngineSyntaxCheckerPassesCardNames holds the checker to the corpus:
// option labels carry card names verbatim (a NameCard ask offers every
// name), so no real card name may read as engine syntax.
func TestEngineSyntaxCheckerPassesCardNames(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	n := 0
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			n++
			if p := engineSyntaxIn(f.Name); p != "" {
				t.Errorf("card name %q flagged as %s", f.Name, p)
			}
		}
	}
	t.Logf("checked %d card face names", n)
}

// promptTextGame is one seeded repo-deck game the contract scan plays.
type promptTextGame struct {
	label string
	cfg   Config
	bot   uint64
}

// promptTextGames builds the sample: every Legacy deck once in a 4-seat
// game, every other 60-card repo deck and every Commander repo deck once
// each in a 2-seat game.
func promptTextGames(t *testing.T, reg *cards.Registry) []promptTextGame {
	var games []promptTextGame
	legacy := testutil.LegacyDeckNames()
	for g := 0; g*4 < len(legacy); g++ {
		names := legacy[g*4 : g*4+4]
		decks := make([][]*cards.Card, len(names))
		for i, n := range names {
			decks[i] = testutil.RepoDeck(t, reg, n)
		}
		games = append(games, promptTextGame{
			label: "legacy " + strings.Join(names, ","),
			cfg: Config{Seed: uint64(g), Names: append([]string(nil), names...), Decks: decks,
				Tokens: reg.Tokens, NameUniverse: reg.AllCards(), Mulligans: 1},
			bot: uint64(g),
		})
	}
	isLegacy := map[string]bool{}
	for _, n := range legacy {
		isLegacy[n] = true
	}
	var constructed, commander []string
	for _, n := range testutil.RepoDeckNames() {
		switch {
		case isLegacy[n]:
		case len(testutil.RepoDeckFile(t, n).CommanderNames()) > 0:
			commander = append(commander, n)
		default:
			constructed = append(constructed, n)
		}
	}
	for i, n := range constructed {
		opp := legacy[i%len(legacy)]
		games = append(games, promptTextGame{
			label: "constructed " + n,
			cfg: Config{Seed: uint64(100 + i), Names: []string{n, opp},
				Decks:  [][]*cards.Card{testutil.RepoDeck(t, reg, n), testutil.RepoDeck(t, reg, opp)},
				Tokens: reg.Tokens, NameUniverse: reg.AllCards(), Mulligans: 1},
			bot: uint64(100 + i),
		})
	}
	for i := 0; i < len(commander); i += 2 {
		a, b := commander[i], commander[(i+1)%len(commander)]
		games = append(games, promptTextGame{
			label: "commander " + a + "," + b,
			cfg: Config{Seed: uint64(1000 + i), Names: []string{a, b},
				Decks:        [][]*cards.Card{testutil.RepoDeck(t, reg, a), testutil.RepoDeck(t, reg, b)},
				Tokens:       reg.Tokens,
				NameUniverse: reg.AllCards(),
				Format:       FormatCommander,
				StartingLife: 40,
				Commanders:   [][]int{testutil.RepoDeckFile(t, a).CommanderIndices(), testutil.RepoDeckFile(t, b).CommanderIndices()},
				Mulligans:    1},
			bot: uint64(5000 + i),
		})
	}
	return games
}
