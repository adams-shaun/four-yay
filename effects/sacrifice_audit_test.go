package effects

import (
	"fmt"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The two tests below are corpus-audit harnesses, not behaviour tests: they
// reproduce the measurements that justify the `Permanent` default in
// effSacrifice and that quantify what a player-targeted sacrifice does not
// implement (Amount$, Optional$, RememberSacrificed$). They print
// their findings rather than assert them, because the numbers are a property
// of the corpus pin, not of this package's behaviour: upstream data can move
// them, and a number that silently fails a build is worse than one a reader
// can re-derive. Re-run with `go test -run TestSacrificeCorpusAudit ./effects -v`.

// sacTargetKind classifies how a Sacrifice SA's Defined$ resolves: "player"
// (a defined player target, the branch that consults SacValid$), "object" (a
// definite object, the branch that never reads SacValid$), or "inherit"
// (Targeted/ParentTarget/Remembered or unrecognised — resolved to whatever the
// parent captured or targeted, so possibly either).
func sacTargetKind(sa *cards.SA) string {
	switch sa.Params["Defined"] {
	case "You", "Opponent", "Player", "TriggeredDefendingPlayer", "TriggeredPlayer", "TriggeredCardController":
		return "player"
	case "Self", "Parent", "TriggeredCard", "TriggeredCardLKICopy", "TriggeredNewCardLKICopy",
		"TriggeredSpellAbility", "TriggeredAttacker", "TriggeredSource",
		"DelayTriggerRememberedLKI", "RememberedLKI", "TriggeredAttackerLKICopy",
		"ReplacedCard", "Equipped", "Enchanted", "AttachedTo":
		return "object"
	case "Remembered", "Targeted", "ParentTarget":
		return "inherit"
	case "":
		if _, ok := sa.Params["ValidTgts"]; ok {
			return "inherit"
		}
		return "object"
	}
	return "inherit"
}

// sacrificeSAs enumerates every Sacrifice SA the engine can execute:
// abilities, trigger effects, replacement Withs, and every SVar-referenced
// sub-ability (which a plain Sub-chain walk misses — MatchedAbility$ points at
// its own body). Deduped per card file.
func sacrificeSAs(r *cards.Registry) []*cards.SA {
	seen := map[string]bool{}
	var sacs []*cards.SA
	addLine := func(sa *cards.SA, cf string) {
		if sa == nil {
			return
		}
		var rec func(*cards.SA)
		rec = func(s *cards.SA) {
			if s == nil {
				return
			}
			if s.API == "Sacrifice" && !seen[cf+"\n"+s.Line] {
				seen[cf+"\n"+s.Line] = true
				sacs = append(sacs, s)
			}
			rec(s.Sub)
		}
		rec(sa)
	}
	for _, c := range r.AllCards() {
		for _, f := range c.Faces {
			for _, a := range f.Abilities {
				addLine(a, c.Path)
			}
			for _, tr := range f.Triggers {
				addLine(tr.Effect, c.Path)
			}
			for _, rp := range f.Repls {
				addLine(rp.With, c.Path)
			}
			for name := range f.SVars {
				addLine(cards.ResolveSVar(f.SVars, name), c.Path)
			}
		}
	}
	return sacs
}

func TestSacrificeCorpusAudit(t *testing.T) {
	r := testutil.CorpusRegistry(t)
	sacs := sacrificeSAs(r)
	total := len(sacs)
	noSac := 0
	noSacObject, noSacInherit, noSacPlayer := 0, 0, 0
	playerTotal, playerSelf := 0, 0
	var noSacPlayerLines []string
	for _, sa := range sacs {
		_, has := sa.Params["SacValid"]
		switch sacTargetKind(sa) {
		case "player":
			playerTotal++
			if sa.Params["SacValid"] == "Self" {
				playerSelf++
			}
			if !has {
				noSacPlayer++
				noSacPlayerLines = append(noSacPlayerLines, sa.Line)
			}
		case "inherit":
			if !has {
				noSacInherit++
			}
		case "object":
			if !has {
				noSacObject++
			}
		}
		if !has {
			noSac++
		}
	}
	fmt.Printf("Reachability of the SacValid$ default (Permanent)\n")
	fmt.Printf("  total Sacrifice SAs                                  = %d\n", total)
	fmt.Printf("  no SacValid$                                         = %d\n", noSac)
	fmt.Printf("    object path (spec never read)                       = %d\n", noSacObject)
	fmt.Printf("    inherited/unknown target                            = %d\n", noSacInherit)
	fmt.Printf("    player-targeted (reaches the default)               = %d\n", noSacPlayer)
	fmt.Printf("  player-targeted total                                 = %d\n", playerTotal)
	fmt.Printf("  player-targeted SacValid$ Self                        = %d\n", playerSelf)
	for _, l := range noSacPlayerLines {
		fmt.Printf("    no-SacValid player-targeted: %s\n", l)
	}

	amt := map[string]int{}
	amtCount := 0
	optionalAll, rememberAll, validCardAll := 0, 0, 0
	player, inherit := 0, 0
	pAmt, pOptional, pRemember, pValidCard := 0, 0, 0, 0
	pAmtDist := map[string]int{}
	inhAmt, inhOptional, inhRemember := 0, 0, 0
	for _, sa := range sacs {
		if v := sa.Params["Amount"]; v != "" {
			amt[v]++
			amtCount++
		}
		if sa.Params["Optional"] == "True" {
			optionalAll++
		}
		if sa.Params["RememberSacrificed"] != "" {
			rememberAll++
		}
		if sa.Params["ValidCard"] != "" {
			validCardAll++
		}
		switch sacTargetKind(sa) {
		case "player":
			player++
			if v := sa.Params["Amount"]; v != "" && v != "1" {
				pAmt++
				pAmtDist[v]++
			}
			if sa.Params["Optional"] == "True" {
				pOptional++
			}
			if sa.Params["RememberSacrificed"] != "" {
				pRemember++
			}
			if sa.Params["ValidCard"] != "" {
				pValidCard++
			}
		case "inherit":
			inherit++
			if v := sa.Params["Amount"]; v != "" && v != "1" {
				inhAmt++
			}
			if sa.Params["Optional"] == "True" {
				inhOptional++
			}
			if sa.Params["RememberSacrificed"] != "" {
				inhRemember++
			}
		}
	}
	fmt.Printf("\nUnread narrowings on player-targeted sacrifice\n")
	fmt.Printf("  player-targeted total                                                = %d\n", player)
	fmt.Printf("  player-targeted Amount$ != 1 (wants more than one)                   = %d\n", pAmt)
	fmt.Printf("  player-targeted Optional$ True                                       = %d\n", pOptional)
	fmt.Printf("  player-targeted RememberSacrificed$                                  = %d\n", pRemember)
	fmt.Printf("  player-targeted ValidCard$                                           = %d\n", pValidCard)
	fmt.Printf("  player-targeted Amount$ != 1 distribution:\n")
	pks := make([]string, 0, len(pAmtDist))
	for k := range pAmtDist {
		pks = append(pks, k)
	}
	sort.Strings(pks)
	for _, k := range pks {
		fmt.Printf("    %-8s x%d\n", k, pAmtDist[k])
	}
	fmt.Printf("\nPlayer-branch upper bound (player + inherit, n=%d)\n", player+inherit)
	fmt.Printf("  Amount$ != 1               = %d\n", pAmt+inhAmt)
	fmt.Printf("  Optional$ True             = %d\n", pOptional+inhOptional)
	fmt.Printf("  RememberSacrificed$        = %d\n", pRemember+inhRemember)
	fmt.Printf("  any-SA Amount$ != \"\"    = %d  |  Optional$ True = %d  |  RememberSacrificed$ = %d  |  ValidCard$ = %d\n",
		amtCount, optionalAll, rememberAll, validCardAll)
	fmt.Printf("  Amount$ distribution (any Sacrifice SA):\n")
	ks := make([]string, 0, len(amt))
	for k := range amt {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("    %-8s x%d\n", k, amt[k])
	}
}
