package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/gate"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/rules"
)

// runRefreeze converts legacy passing rows (a whole-snapshot canon_sha) to
// field-level frozen expectations (section 11.3 C3). A row converts only
// when its scenario is still exactly what the generator makes and gorge
// still reproduces the hashed snapshots byte for byte, so the frozen fields
// are the very result XMage agreed with (or the ruling froze). Any other
// row keeps its canon_sha and says why.
func runRefreeze(dir string, apply bool) error {
	if err := installXMageKnown(filepath.Join("compliance", "manifests")); err != nil {
		return err
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	all, err := compliance.LoadVerdicts(compliance.VerdictDir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)
	var done []compliance.VerdictRow
	kept := map[string]int{}
	var keptCards []string
	fields := 0
	for _, card := range names {
		for _, r := range all[card] {
			if r.CanonSHA == "" || len(r.Frozen) > 0 {
				continue
			}
			why := ""
			it, skip := templates.ItemFor(reg, card, r.Template)
			switch {
			case skip != nil:
				why = "no scenario now: " + skip.Reason
			case gate.ItemSHA(it) != r.ScenarioSHA:
				why = "scenario changed"
			}
			var res rules.OracleResult
			if why == "" {
				res, err = rules.RunOracleScenarioJSON(reg, it.Raw())
				switch {
				case err != nil:
					why = "gorge replay: " + err.Error()
				case gate.Hash([]byte(oraclediffCanonical(res, it.Compare, it.Ignore))) != r.CanonSHA:
					why = "gorge no longer reproduces the hashed snapshots"
				}
			}
			if why != "" {
				kept[why]++
				keptCards = append(keptCards, card+": "+why)
				continue
			}
			r.Frozen, r.CanonSHA = freezeResult(res, it.Compare, it.Ignore), ""
			fields += len(r.Frozen)
			done = append(done, r)
		}
	}
	if apply && len(done) > 0 {
		if err := compliance.ReplaceVerdicts(compliance.VerdictDir, done); err != nil {
			return err
		}
	}
	verb := "would convert"
	if apply {
		verb = "converted"
	}
	fmt.Printf("refreeze: %s %d rows to %d frozen fields; %d keep canon_sha\n", verb, len(done), fields, len(keptCards))
	for k, n := range kept {
		fmt.Printf("  kept: %-50s %d\n", k, n)
	}
	for _, c := range keptCards {
		fmt.Println("  ", c)
	}
	return nil
}

func oraclediffCanonical(res rules.OracleResult, compare, ignore []string) string {
	return oraclediff.CanonicalOpts(res.Snapshots, compare, ignore...)
}

func freezeResult(res rules.OracleResult, compare, ignore []string) []compliance.Frozen {
	return oraclediff.FreezeOpts(res, compare, ignore...)
}
