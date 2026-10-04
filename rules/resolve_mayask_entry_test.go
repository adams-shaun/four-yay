package rules

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/internal/testutil"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestCopiedAuraEntryAsksEnchant: a CopyPermanent whose copied object is an
// Aura (the Dedicated Dollmaker shape: exile a target permanent, its
// controller creates a token copy of it) puts a non-cast Aura onto the
// battlefield, which chooses what it enchants as it enters (CR 303.4f). The
// copied face is board-dependent, so the ask-free predicate must not exempt
// the resolution (cardfuzz seed 4242: "the ask-free predicate missed an ask:
// choose/etb").
func TestCopiedAuraEntryAsksEnchant(t *testing.T) {
	t.Parallel()
	bearA := "Name:Entry Bear A\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	bearB := "Name:Entry Bear B\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	aura := "Name:Entry Aura\nManaCost:0\nTypes:Enchantment Aura\nK:Enchant creature\n" +
		"A:SP$ Attach | Cost$ 0 | ValidTgts$ Creature | AILogic$ Pump\nS:Mode$ Continuous | Affected$ Creature.EnchantedBy | AddPower$ 1 | Description$ x\nOracle:x\n"
	maker := "Name:Entry Dollmaker\nManaCost:0\nTypes:Creature Dwarf\nPT:2/2\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigEx | TriggerDescription$ x\n" +
		"SVar:TrigEx:DB$ ChangeZone | TargetMin$ 0 | TargetMax$ 1 | RememberChanged$ True | ValidTgts$ Permanent.Other+nonLand+!token | Origin$ Battlefield | Destination$ Exile | SubAbility$ DBCopy\n" +
		"SVar:DBCopy:DB$ CopyPermanent | Defined$ Remembered | NonLegendary$ True | AddTypes$ Artifact | Controller$ RememberedController | SubAbility$ DBCleanup\n" +
		"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\nOracle:x\n"
	srcs := []string{bearA, bearB, aura, maker}
	var cs []*cards.Card
	byName := map[string]*cards.Card{}
	for _, s := range srcs {
		c := card(t, s)
		cs = append(cs, c)
		byName[c.Faces[0].Name] = c
	}
	e, cfg := tokenReplGame(t, 4242, cs...)
	moveSeededCard(t, e, 0, byName["Entry Bear A"], state.ZBattlefield)
	moveSeededCard(t, e, 0, byName["Entry Bear B"], state.ZBattlefield)
	auraID := moveSeededCard(t, e, 0, byName["Entry Aura"], state.ZHand)
	moveSeededCard(t, e, 0, byName["Entry Dollmaker"], state.ZHand)

	addMana(t, e, 0, "")
	castSpellOption(t, e, "Entry Aura")
	driveCountingReplacementAsks(t, e, 0)
	if o := e.G.Obj(auraID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("the Aura did not resolve onto the battlefield")
	}
	castSpellOption(t, e, "Entry Dollmaker")

	enchantAsks := 0
	for i := 0; i < 80; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving")
		}
		pick := 0
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Stack) == 0 {
				i = 1000
				continue
			}
			passPriorityOnce(t, e)
			continue
		case decision.KTarget:
			if pick = optionForObj(d, auraID); pick < 0 {
				pick = 0
			}
		case decision.KChoose:
			if d.ResumeKind == "etb" && len(d.Options) > 0 && d.Options[0].Kind == "enchant" {
				enchantAsks++
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatal(err)
		}
	}
	if enchantAsks != 1 {
		t.Fatalf("enchant asks = %d, want 1 (the copied Aura chooses what it enchants)", enchantAsks)
	}
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Entry Aura" {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("copied Aura tokens on the battlefield = %d, want 1", tokens)
	}
	replayCheck(t, e, cfg)
}

// TestTokenEntryAskCensus is the Token half of the entering-object census
// (cards.TestAskFreeEntryCensus): the text half cannot see a token script's
// face, so a Token body whose minted face asks as it enters -- an Aura's
// CR 303.4f enchant choice, an as-enters choice (Dragon Broodmother's Devour
// token) -- must be judged "may ask" by the text half or by the token half
// (cards.SAChainTokenEntryMayAsk, folded into saMayAskState). Every corpus
// Token body is held to it, judged independently here.
func TestTokenEntryAskCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	enchants := func(f *cards.Face) bool {
		for _, kw := range f.Keywords {
			if strings.HasPrefix(kw, "Enchant") {
				return true
			}
		}
		return false
	}
	var bad []string
	caught := 0
	check := func(f *cards.Face, root *cards.SA) {
		if root == nil || cards.SAChainMayAsk(root, f.SVars, f, true) {
			return
		}
		if cards.SAChainTokenEntryMayAsk(root, f.SVars, reg.Tokens) {
			caught++
			return
		}
		for s := root; s != nil; s = s.Sub {
			if s.API != "Token" || s.HasParam(cards.PKAttachedTo) {
				continue
			}
			for _, n := range strings.Split(s.ParamStr(cards.PKTokenScript), ",") {
				tok := reg.Tokens[strings.TrimSpace(n)]
				if tok == nil || len(tok.Faces) == 0 {
					continue
				}
				tf := tok.Faces[0]
				if enchants(tf) || cards.FaceEntryMayAsk(tf) {
					bad = append(bad, f.Name+" -> "+strings.TrimSpace(n))
				}
			}
		}
	}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, a := range f.Abilities {
				check(f, a)
			}
			for i := range f.Triggers {
				check(f, f.Triggers[i].Effect)
			}
			for name := range f.SVars {
				check(f, cards.ResolveSVar(f.SVars, name))
			}
		}
	}
	if caught == 0 {
		t.Error("the token half caught no corpus carrier (Dragon Broodmother's Devour token): is the census live?")
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("%d ask-free Token bodies mint a face that asks as it enters:\n%s", len(bad), strings.Join(bad, "\n"))
	}
}
