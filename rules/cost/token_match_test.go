package cost

import "testing"

// matcherEdgeTokens are hand-picked boundary tokens for the hand-compiled
// cost grammar: empty and missing fields, doubled separators, stray angle
// brackets, the counted alternatives (X, X1+, -1) and near-miss heads.
var matcherEdgeTokens = []string{
	"", "<", ">", "<>", "Sac", "Sac<", "Sac>", "Sac<>", "Sac<1>", "Sac<1/>", "Sac<1//>", "Sac<1/a>",
	"Sac<1/a/>", "Sac<1/a//b>", "Sac<1/a/b/c>", "Sac<1/a>b>", "Sac<1/a<b>", "Sac<<1/a>", "Sac<X/a>",
	"Sac<X/a/b>", "Sac<X//a>", "Sac<01/a>", "Sac<-1/a>", "Sac<1/a> ", "sac<1/a>", "SacX<1/a>",
	"SubCounter<1/P1P1>", "SubCounter<X/LOYALTY>", "SubCounter<X2+/Any>", "SubCounter<X+/Any>",
	"SubCounter<2X+/Any>", "SubCounter<X/Any/Artifact.YouCtrl/desc>", "SubCounter<X/Any//desc>",
	"SubCounter<X/Any/>", "SubCounter<X/Any/a b>", "SubCounter<1/Any/t/d/e>", "RemoveAnyCounter<1/Any/Creature/x>",
	"Draw<1/You>", "Draw<X/You>", "Draw<X1/You/d>", "Draw<1X/You>", "Draw<_/You>",
	"ExileFromHand<1/Card>", "ExileFromGrave<X/Card/desc>", "ExileAnyGrave<1/Card>", "ExileFromLibrary<1/Card>",
	"Exile<1/CARDNAME>", "Exile<X/CARDNAME>", "ExileFromTop<1/Card>", "ExiledMoveToGrave<1/Card.OppOwn/x>",
	"AddCounter<1/LOYALTY>", "AddCounter<0/LOYALTY/d>", "AddCounter<1/LOYALTYX>", "AddCounter<1/M1M1>",
	"AddCounter<1/M1M1/x>", "AddCounter<1/PAGE_2>", "AddCounter<1/a-b>",
	"Exert<1/CARDNAME>", "Exert<1/NICKNAME/d>", "Exert<2/CARDNAME>", "Exert<1/CARDNAMEX>",
	"PayLife<2>", "PayLife<X>", "PayLife<>", "PayLife<2/x>", "PayLife<99999999999999999999>",
	"Reveal<1/Card>", "Behold<1/Elf>", "BeholdExile<1/Kithkin/d>", "tapXType<2/Creature>", "tapXType<X/Creature>",
	"tapXType<Any/Creature.Other+withTotalPowerGE10>", "tapXType<Anything/Creature>",
	"RevealOrChoose<1/Dragon>", "RevealChosen<Player>", "RevealChosen<Type/creature type>", "RevealChosen<Players>",
	"untapYType<1/Land.OppCtrl/d>", "Blight<1>", "Blight<X>", "Blight<XX>", "Blight<>",
	"PayEnergy<3>", "PayEnergy<X/d>", "PayEnergy<X3>", "Return<1/CARDNAME>",
	"PutCardToLibFromHand<1/0/Card>", "PutCardToLibFromGrave<1/-1/Card/d>", "PutCardToLibFromBattlefield<1/--1/Card>",
	"PutCardToLibFromHand<1/0>", "PutCardToLibFromHand<1/0//>", "PutCardToLibFromSameGrave<1/0/Card>",
	"Mill<2>", "Mill<X>", "CollectEvidence<6>", "CollectEvidence<X>", "CollectEvidence<>",
	"DamageYou<4>", "DamageYou<4/d/e>", "DamageYou</d>", "GainLife<6/Player.Opponent>", "GainLife<5/Player.Other/*>",
	"GainLife<5/Player>", "GainLife<5/Players/x>", "RollDice<1/20/X>", "RollDice<>", "XMin1", "XMin", "XMinX",
	"XMin12a", "Waterbend<4>", "Waterbend<X>", "Waterbend<4/x>",
	"withTotalPowerGE", "withTotalPowerGEx withTotalPowerGE3", "a+withTotalPowerGE10+b", "withTotalPowerGEwithTotalPowerGE7",
	"Sac<1/Cr\xffe>", "Sac<1/Cré/é>",
}

func TestTokenMatchersAgreeWithTheirGrammar(t *testing.T) {
	for _, tok := range matcherEdgeTokens {
		CheckToken(t, tok)
		for _, m := range Mutations(tok) {
			CheckToken(t, m)
		}
	}
}

// FuzzTokenMatchers holds every hand-compiled matcher to its reference
// regexp on arbitrary input (`go test -fuzz FuzzTokenMatchers ./rules/cost`).
func FuzzTokenMatchers(f *testing.F) {
	for _, tok := range matcherEdgeTokens {
		f.Add(tok)
	}
	f.Fuzz(func(t *testing.T, sym string) { CheckToken(t, sym) })
}

// The matchers allocate nothing, on a hit or a miss.
func TestTokenMatchersDoNotAllocate(t *testing.T) {
	syms := []string{"Sac<1/Artifact;Creature/artifact or creature>", "SubCounter<X/Any/Artifact.YouCtrl/x>",
		"PutCardToLibFromGrave<1/-1/Card/d>", "XMin1", "Bogus<1/x>", "2"}
	allocs := testing.AllocsPerRun(100, func() {
		for _, s := range syms {
			for _, mc := range matcherCases {
				mc.Match(s)
			}
			findGroupPowerFloor(s)
		}
	})
	if allocs != 0 {
		t.Fatalf("token matchers allocated %.1f objects per run, want 0", allocs)
	}
}

// TestCostHeadTableIsSorted holds lookupCostHead's binary-search
// precondition and its round trip over every head.
func TestCostHeadTableIsSorted(t *testing.T) {
	for h := hNone + 1; h < numCostHeads; h++ {
		if costHeadNames[h] == "" {
			t.Fatalf("costHead %d has no name", h)
		}
		if h > hNone+1 && costHeadNames[h-1] >= costHeadNames[h] {
			t.Fatalf("costHeadNames out of order at %q, %q", costHeadNames[h-1], costHeadNames[h])
		}
		if got := lookupCostHead(costHeadNames[h]); got != h {
			t.Fatalf("lookupCostHead(%q) = %d, want %d", costHeadNames[h], got, h)
		}
	}
	for _, miss := range []string{"", "A", "Sa", "Sacc", "zzz", "Exil", "PutCardToLibFromSameGrave"} {
		if got := lookupCostHead(miss); got != hNone {
			t.Fatalf("lookupCostHead(%q) = %d, want hNone", miss, got)
		}
	}
}
