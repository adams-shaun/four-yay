// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import (
	"strconv"
	"strings"
)

// kwClass expands the Class enchantment keyword (CR 702.118). The script form
// is one line per level above the first:
//
//	K:Class:<level>:<cost>:Add<Kind>$ <SVar-name> [ | Add<Kind>$ <SVar-name> ]...
//
// A Class has level 1 by default (CR 716.2b), without a counter or an
// entry replacement. Each line adds two things:
//
//   - a level-up activator, "<cost>: Gain the next level as a sorcery" --
//     a sorcery-speed ClassLevelUp designation change, gated on the Class's level
//     being BELOW this line's level (CR 702.118b: "you may activate a level
//     ability ... only if this Class's level is less than N"), so a Class at
//     level 2 is offered the level-3 activator and no longer the level-2 one;
//   - the granted ability itself, appended to the face with its own
//     `ClassBand$ <N>` band so it becomes live exactly when the Class's level
//     reaches N. AddStaticAbility$ names a static body
//     (Mode$ Continuous and Mode$ ReduceCost/RaiseCost/SetCost both occur in
//     the corpus), AddTrigger$ a trigger body and AddReplacementEffect$ a
//     replacement body; each is parsed with the SAME reader the printed line
//     uses (ParseStaticLines/ParseTriggerLine/parseParams), so the grant runs
//     through the ordinary static/trigger/replacement machinery with no
//     Class-specific consumer anywhere.
//
// Appending the parsed body directly (rather than wrapping it in the
// `Mode$ Continuous | AddStaticAbility$ ...` grant static the Exploration
// Broodship shape uses) is what makes a ReduceCost body work: layers.go's
// AddStaticAbility$ grant branch emits only an inner `Mode$ Continuous`
// static through staticEffects, while collectCostStatics reads the face's
// OWN statics -- so a directly-appended `Mode$ ReduceCost` is picked up by
// the cost-static collector and a directly-appended `Mode$ Continuous` by the
// layer walk.
//
// The keyword is idempotent per LINE (KeywordLine tag), so a second Link()
// of a cached face neither double-adds an activator nor re-grants an ability.
func kwClass(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// param is "<level>:<cost>:<body>"; more than three colon fields do not
	// occur (the body itself may carry further ` | Key$ value` segments).
	levelStr, rest, ok := strings.Cut(param, ":")
	if !ok {
		return
	}
	level, err := strconv.Atoi(strings.TrimSpace(levelStr))
	if err != nil || level < 2 {
		// A malformed level (or level 1, the intrinsic default designation)
		// grants nothing -- fail closed rather than guess a band.
		return
	}
	cost, body, _ := strings.Cut(rest, ":")
	cost = strings.TrimSpace(cost)
	if cost == "" {
		return
	}

	// Level 1 is the default designation, not an entry replacement or counter.
	// The level-up activator sets the designation to this line's level, never a
	// counter, and is offered only while the Class is at exactly N-1 (CR 716.2d,
	// matching Constructed's class level-up activation status).
	if !has("A", k) {
		gate := "Card.Self+classLevel_EQ" + strconv.Itoa(level-1)
		sa, _ := parseSA("", "AB$ ClassLevelUp | Cost$ "+cost+" | IsPresent$ "+gate+
			" | Level$ "+strconv.Itoa(level)+
			" | SorcerySpeed$ True | Keyword$ Class | SpellDescription$ Level "+strconv.Itoa(level))
		if sa != nil {
			sa.Params["KeywordLine"] = k
			f.Abilities = append(f.Abilities, sa)
		}
	}

	// 3. The granted ability, live from level N on. The value of each Add* key
	// may name several bodies joined with " & " (SMayLook & SMayPlay), and a
	// line may carry several Add* keys at once (ProdigysWill | AddTrigger$ ...),
	// so every segment is read independently.
	for seg := range strings.SplitSeq(body, "|") {
		key, val, ok := strings.Cut(strings.TrimSpace(seg), "$")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if val == "" {
			continue
		}
		switch key {
		case "AddStaticAbility":
			for _, name := range SplitGrantNames(val) {
				inners, ok := ParseStaticLines(f.SVars[name])
				if !ok {
					continue
				}
				tag := k + "#static#" + name
				if has("S", tag) {
					continue
				}
				for _, inner := range inners {
					// The split statics share one Params map (ParseStaticLines
					// over one body), so the level gate and the grant tag below
					// stamp every mode of a compound body identically.
					addLevelGate(inner.Params, level)
					inner.Params["KeywordLine"] = tag
					inner.Params["Keyword"] = "Class"
					f.Statics = append(f.Statics, inner)
				}
			}
		case "AddTrigger":
			for _, name := range SplitGrantNames(val) {
				tr, ok := ParseTriggerLine(f.SVars[name])
				if !ok {
					continue
				}
				tag := k + "#trigger#" + name
				if has("T", tag) {
					continue
				}
				addLevelGate(tr.Params, level)
				tr.Params["KeywordLine"] = tag
				tr.Params["Keyword"] = "Class"
				f.Triggers = append(f.Triggers, tr)
			}
		case "AddReplacementEffect":
			for _, name := range SplitGrantNames(val) {
				rp := parseParams(f.SVars[name])
				if ev := strings.TrimSpace(rp["Event"]); ev == "" {
					continue
				}
				tag := k + "#repl#" + name
				if has("R", tag) {
					continue
				}
				addLevelGate(rp, level)
				rp["KeywordLine"] = tag
				rp["Keyword"] = "Class"
				f.Repls = append(f.Repls, Repl{Event: strings.TrimSpace(rp["Event"]), Params: rp})
			}
		}
	}
}

// SplitGrantNames splits an Add*$ value into the one or more SVar names it
// names, joined by Forge's " & " list separator (SMayLook & SMayPlay, and the
// multi-part AddStaticAbility$ ProdigysWill line's single name). Empty members
// are dropped. Exported so rules' printed-static scanner (staticEffects) reads
// an "A & B" AddTrigger$ value with the same grammar this file's K:Class:
// grant path uses -- ONE home for the split.
func SplitGrantNames(v string) []string {
	var out []string
	for part := range strings.SplitSeq(v, "&") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// addLevelGate attaches a Class level band to a granted body as its own
// dedicated ClassBand$ N parameter. It deliberately does NOT write
// IsPresent$/IsPresent2$: those carry per-family semantics this band must not
// inherit. The trigger gate once read IsPresent$+IsPresent2$ as a UNION, so a
// band in IsPresent2$ was ORed with the body's own IsPresent$ and fired the
// level-N grant at level 1 (Hunter's Talent's end-step draw); a body may also
// carry its own IsPresent2$; and replacementConditionHolds reads no
// IsPresent2$ at all, so the band would vanish (wrong-wide). ClassBand$ is
// read as an independent AND gate by every family a Class grant can reach --
// rules/class_level.go holds the one evaluator, and each gate family calls it
// beside its own IsPresent$ read.
func addLevelGate(params map[string]string, level int) {
	params["ClassBand"] = strconv.Itoa(level)
}

func init() { registerKeyword(kwClass, "Class") }
