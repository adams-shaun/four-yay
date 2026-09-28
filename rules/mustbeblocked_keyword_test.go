package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// mustBeBlockedCorpusCard loads one linked real corpus card by name.
func mustBeBlockedCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c := searchCorpusCard(t, searchTestRegistry(t), name)
	if d := c.Link(); len(d) != 0 {
		t.Fatalf("link %s: %v", name, d)
	}
	return c
}

// TestRealCorpusMustBeBlockedKeywordIsRead pins the ticket's CR 509.1a
// requirement on the REAL corpus card rather than a synthetic fixture. Forge
// encodes "Raphael must be blocked if able." as the sentence keyword
// `K:CARDNAME must be blocked if able.`; cards/parse.go canonicalises that
// sentence to the MustBlock head (cards/hiddenkeyword.go), and rules reads the
// canonical head through parseHiddenKeyword, so the attacker carries the
// requirement end to end: the face's keyword head is MustBlock (not the
// phantom `kw:CARDNAME ...` primitive the coverage walk used to intern), and a
// legal block pair against it is flagged AttackMust while the empty
// declaration is rejected.
//
// The preconditions are asserted first: the face really prints the line, the
// attacker is on the battlefield, and the blocker can legally block it. If any
// of those were wrong the requirement assertion below could pass vacuously.
func TestRealCorpusMustBeBlockedKeywordIsRead(t *testing.T) {
	t.Parallel()
	raph := mustBeBlockedCorpusCard(t, "Raphael, Ninja Destroyer")
	f := raph.Faces[0]
	printed := false
	for _, k := range f.Keywords {
		if cards.KeywordHead(k) == "MustBlock" {
			printed = true
		}
	}
	if !printed {
		t.Fatalf("precondition: %s prints no canonical MustBlock head; keywords=%v",
			f.Name, f.Keywords)
	}

	e := threeSeatEngine(t)
	attacker := onBoardCard(t, e, 1, raph)
	blocker := onBoardCard(t, e, 0, card(t,
		"Name:Test Ground Blocker\nManaCost:1\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	// Preconditions: the attacker is really on the battlefield and the
	// blocker can really block it, so the only constraint under test is the
	// requirement.
	if e.G.Obj(attacker).Zone != state.ZBattlefield {
		t.Fatal("precondition: Raphael is not on the battlefield")
	}
	if !e.hasMustBeBlockedKeyword(attacker) {
		t.Fatalf("Raphael's printed keyword was not read: derived keywords = %v",
			e.Derived(attacker).Keywords)
	}

	attackSeat0(t, e, attacker)
	// canBlock requires the attacker to actually be attacking, so this
	// precondition is checked after the attack is declared -- before that it
	// would fail for the wrong reason (no combat) and mask the real one.
	if !e.canBlock(blocker, attacker) {
		t.Fatal("precondition: the ground blocker cannot block Raphael")
	}
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no blockers decision posed for the must-be-blocked attacker")
	}
	opt := findBlockOption(d, blocker, attacker)
	if opt == nil {
		t.Fatalf("legal block pair not offered: %+v", d.Options)
	}
	if !opt.AttackMust {
		t.Fatalf("block option against Raphael not flagged as a must-be-blocked requirement: %+v", opt)
	}
	// The empty declaration leaves the requirement unmet and must be rejected.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err == nil {
		t.Fatal("empty declaration satisfied Raphael's must-be-blocked requirement")
	}
	// The legal block commits.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("the legal blocking declaration was rejected: %v", err)
	}
	if !blockCommitted(t, e, attacker, blocker) {
		t.Fatalf("block did not commit: BlockedBy=%v", e.G.Obj(attacker).BlockedBy)
	}
}

// TestRealCorpusPumpMustBeBlockedKeywordIsRead is the runtime-grant control:
// Bumper Cars supplies the SAME requirement through a Pump SVar's
// `KW$ HIDDEN CARDNAME must be blocked if able.`, which never passes through
// the K: parser's canonicalisation. The sentence arm of parseHiddenKeyword
// must still read it, so canonicalising the printed form cannot shrink the
// set of runtime grants that work. The assertion is the oracle the combat
// path uses, on the card's own SVar body.
func TestRealCorpusPumpMustBeBlockedKeywordIsRead(t *testing.T) {
	t.Parallel()
	bumper := mustBeBlockedCorpusCard(t, "Bumper Cars")
	// Precondition: the card really declares the sentence in an SVar body.
	found := false
	for _, body := range bumper.Faces[0].SVars {
		if strings.Contains(body, "HIDDEN CARDNAME must be blocked if able.") {
			found = true
		}
	}
	if !found {
		t.Fatal("precondition: Bumper Cars declares no runtime must-be-blocked sentence")
	}
	// The sentence form reaches the same oracle the printed (canonical) form
	// does.
	if got := parseHiddenKeyword("HIDDEN CARDNAME must be blocked if able."); !got.mustBlock {
		t.Fatalf("runtime sentence grant no longer read: %+v", got)
	}
	if got := parseHiddenKeyword("MustBlock"); !got.mustBlock {
		t.Fatalf("canonical head not read: %+v", got)
	}
}
