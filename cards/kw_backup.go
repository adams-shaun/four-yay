// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import (
	"strconv"
	"strings"
)

// kwBackup expands CR 702.165 Backup: "When this creature enters, put N +1/+1
// counters on target creature. If that's another creature, it gains the
// following abilities until end of turn." (The abilities themselves are the
// keyword's second colon field, a named SVar -- Forge spells the whole rider
// as e.g. `K:Backup:1:BackupAbilities` with
// `SVar:BackupAbilities:DB$ Animate | Keywords$ Flying | ...`.)
//
// The expansion is one ChangesZone self-entry trigger whose body is a
// TARGETED PutCounter (the Mentor shape, cards/kw_mentor.go) chained to a
// grant SubAbility built from the named SVar's own body. The named body is
// already the correct primitive -- `DB$ Animate` or `DB$ Pump` on every
// corpus carrier -- so streaming it verbatim keeps whatever riders the
// carrier prints (Keywords$, Triggers$, staticAbilities$, Abilities$, sVars$)
// flowing through their ordinary parsing, never re-implemented here.
//
// Two injections make that verbatim body behave as the keyword's rider:
//
//   - `Defined$ Targeted` anchors it on the PutCounter's own target. The
//     corpus bodies name no Defined$ at all, and an untargeted sub-ability
//     defaults to the resolving SOURCE (effects/context.go's Defined), which
//     would grant the abilities to the entering creature -- always, even when
//     the counter went elsewhere.
//
//   - `ConditionDefined$ Targeted | ConditionPresent$ Creature.Other` is CR
//     702.70's "if that's ANOTHER creature": a grant on the source itself
//     would double every ability the source already prints (its printed
//     attack trigger plus the identical granted one fire twice), so the
//     grant sub is gated on the target not being the source. The condition
//     gate is the supported Targeted group (effects/conditions.go), and
//     `Other` is the source-relative filter predicate.
func kwBackup(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("T", k) {
		return
	}
	// param is "<N>:<SVarName>". N is a literal count on every corpus line
	// (26 files, values 1/2/3); the SVar names the abilities to copy.
	n, svarName, _ := strings.Cut(param, ":")
	n = strings.TrimSpace(n)
	svarName = strings.TrimSpace(svarName)
	if n == "" {
		n = "1"
	}
	root := "__kwBackup" + strconv.Itoa(i)
	put := "DB$ PutCounter | ValidTgts$ Creature | CounterType$ P1P1 | CounterNum$ " + n
	grantNames := strings.Split(svarName, ",")
	firstGrant := ""
	for j := len(grantNames) - 1; j >= 0; j-- {
		name := strings.TrimSpace(grantNames[j])
		body := strings.TrimSpace(f.SVars[name])
		grant := "__kwBackupGrant" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
		if body == "" {
			// Keep the unresolved reference in the ability chain so Link
			// reports the missing SVar rather than silently dropping it.
			firstGrant = name
			continue
		}
		// A body that already names a Defined$ is left alone, so an
		// explicitly-scoped script cannot have its referent rewritten.
		if !strings.Contains(body, "Defined$") {
			body += " | Defined$ Targeted"
		}
		body += " | ConditionDefined$ Targeted | ConditionPresent$ Creature.Other"
		if firstGrant != "" {
			body += " | SubAbility$ " + firstGrant
		}
		f.setSVar(grant, body)
		firstGrant = grant
	}
	if firstGrant == "" {
		f.setSVar(root, put)
	} else {
		f.setSVar(root, put+" | SubAbility$ "+firstGrant)
	}
	p := parseParams("Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self" +
		" | Execute$ " + root + " | Keyword$ Backup | TriggerDescription$ Backup " + n)
	p["KeywordLine"] = k
	f.Triggers = append(f.Triggers, Trigger{Mode: p["Mode"], Params: p})
}

func init() { registerKeyword(kwBackup, "Backup") }
