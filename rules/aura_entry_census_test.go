package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// aura_entry_census_test.go pins the CR 303.4f/g class fix
// (rules/aura_entry.go) against the code that moves cards onto the
// battlefield. The rule lives at ONE chokepoint -- Engine.emit's pre-pass
// over every MoveZone into the battlefield and every TokenCreate -- so a
// mover is covered by construction as long as (a) it reaches the battlefield
// through Engine.emit (effects only ever hold Host.Emit; the rules side's raw
// events.Emit sites are pinned below), and (b) a mover that attaches the
// entering card ITSELF marks that bearer on the move
// (events.MarkNamedAttachEntry), so the engine settles the Aura's bearer from
// the effect's named set rather than posing a CR 303.4f choice the effect
// already answered.
//
// The census is a bidirectional ratchet in the repo's usual shape: the
// measured function sets must EQUAL the tables, so a new mover, a new
// explicit-attach site or a new raw fold fails here, named, until it is
// classified -- and a classified site that disappears fails too.

// auraCensusFunc is "file.go:Recv.Func" (or "file.go:Func").
type auraCensusFacts struct {
	mover, attach, mark, rawEmit bool
}

func auraCensusScan(t *testing.T, dir string) map[string]auraCensusFacts {
	t.Helper()
	fset := token.NewFileSet()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]auraCensusFacts{}
	for _, ent := range ents {
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			key := name + ":" + fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				typ := fd.Recv.List[0].Type
				if st, ok := typ.(*ast.StarExpr); ok {
					typ = st.X
				}
				if id, ok := typ.(*ast.Ident); ok {
					key = name + ":" + id.Name + "." + fd.Name.Name
				}
			}
			var facts auraCensusFacts
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					switch auraCallName(x.Fun) {
					case "moveZoneEvent", "EmitTokenCreate":
						facts.mover = true
					case "changeZoneAttachedTo", "changeZoneAttachedToPlayer", "emitAttach":
						facts.attach = true
					case "MarkNamedAttachEntry", "markChangeZoneAttach":
						facts.mark = true
					}
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "events" &&
							(sel.Sel.Name == "Emit" || sel.Sel.Name == "EmitPtr") {
							facts.rawEmit = true
						}
					}
				case *ast.CompositeLit:
					for _, el := range x.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Kind" {
							continue
						}
						sel, ok := kv.Value.(*ast.SelectorExpr)
						if !ok {
							continue
						}
						switch sel.Sel.Name {
						case "MoveZone", "TokenCreate", "CopyToken", "CardToken":
							// A MoveZone literal is a mover only when it names
							// the battlefield as its destination somewhere in
							// the literal; the variable-destination movers go
							// through moveZoneEvent above.
							if sel.Sel.Name != "MoveZone" || auraLitNamesBattlefieldTo(x) {
								facts.mover = true
							}
						case "Attach":
							facts.attach = true
						}
					}
				}
				return true
			})
			if facts != (auraCensusFacts{}) {
				prev := out[key]
				out[key] = auraCensusFacts{prev.mover || facts.mover, prev.attach || facts.attach,
					prev.mark || facts.mark, prev.rawEmit || facts.rawEmit}
			}
		}
	}
	return out
}

func auraCallName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func auraLitNamesBattlefieldTo(lit *ast.CompositeLit) bool {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "To" {
			continue
		}
		// A state.Z<zone> literal names its destination; anything else (a
		// variable, a field such as entry.From) may be the battlefield.
		if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "state" {
				return sel.Sel.Name == "ZBattlefield"
			}
		}
		return true
	}
	return false
}

func auraCensusKeys(m map[string]auraCensusFacts, keep func(auraCensusFacts) bool) []string {
	var out []string
	for k, v := range m {
		if keep(v) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func auraCensusDiff(t *testing.T, what string, got []string, want map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	for _, k := range got {
		seen[k] = true
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unclassified site %s -- classify it in the census table (see rules/aura_entry.go)", what, k)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("%s: classified site %s no longer measured -- remove its row", what, k)
		}
	}
}

func TestAuraEntryCensusPrint(t *testing.T) {
	if os.Getenv("AURA_CENSUS_PRINT") == "" {
		t.Skip("set AURA_CENSUS_PRINT=1 to print the measured sets")
	}
	for _, dir := range []string{"../effects", "."} {
		m := auraCensusScan(t, dir)
		for _, k := range auraCensusKeys(m, func(f auraCensusFacts) bool { return true }) {
			t.Logf("%s %s %+v", dir, k, m[k])
		}
	}
}

// Classification tags (the text after the tag is the reason):
//
//	gate:          puts (or may put) a card onto the battlefield and names no
//	               bearer of its own -- the emit gate and the CR 303.4f choice
//	               cover it.
//	marks:         puts a card onto the battlefield AND attaches it itself;
//	               must call events.MarkNamedAttachEntry / markChangeZoneAttach
//	               (asserted: mark is measured true).
//	marked-by X:   attaches after a battlefield move made by callee X, which
//	               marks (asserted: X is classified marks:).
//	stack:         a resolving spell/ability or cast leaving/entering the
//	               stack -- CR 303.4f/g exempt a resolving Aura spell (it
//	               attaches through its cast-time target).
//	token:         a token mint; the TokenCreate gate (auraTokenGate) and
//	               effects' auraTokenWithheld cover an Aura token.
//	noentry:       never moves a card onto the battlefield.
//	attach:        attaches a permanent already on the battlefield (or is an
//	               attach helper); no entry.
//	raw:           a rules-side events.Emit that bypasses Engine.emit; must
//	               never carry a fresh battlefield entry past the gate.
var auraEntryCensusEffects = map[string]string{
	"airbend.go:effAirbend":                     "noentry: exiles",
	"amass.go:effAmass":                         "token: Army token mint",
	"attach.go:effAttach":                       "attach: DB$ Attach on a battlefield (or still-on-stack Aura spell) object",
	"attach.go:emitAttach":                      "attach: helper",
	"attach.go:emitPlayerAttach":                "attach: helper",
	"dig.go:effDig":                             "gate: Dig's DestinationZone$ Battlefield",
	"digmultiple.go:effDigMultiple":             "gate: DigMultiple's destination may be the battlefield",
	"diguntil.go:effDigUntil":                   "marks: revealed-Aura bearer (its own diguntil_aura ask) precedes the move",
	"clone.go:effClone":                         "attach: re-attaches the object becoming a copy; no zone change",
	"context.go:moveZoneEvent":                  "gate: the shared MoveZone constructor (variable destination)",
	"copypermanent.go:effCopyPermanent":         "marks: AttachedTo$ copy token",
	"empower.go:effEmpower":                     "noentry: library/hand moves",
	"encore.go:effEncore":                       "token: CardToken copy",
	"endure.go:endureCreateSpirit":              "token: white Spirit mint; w_x_x_spirit is never an Aura, and the TokenCreate gate covers any Aura a future rider mints",
	"explore.go:exploreOnce":                    "noentry: hand/graveyard",
	"incubate.go:incubateLoop":                  "token: Incubator mint",
	"investigate.go:investigateFor":             "token: Clue mint",
	"misc.go:effWard":                           "noentry: counters to graveyard",
	"myriad.go:myriadCreate":                    "token: CopyToken attacking copies",
	"recruit.go:effRecruit":                     "noentry: library to hand",
	"resolve.go:effCounter":                     "stack: countered spell leaves the stack",
	"token.go:applyTokenMintRiders":             "token: AttachedTo$ rider after the mint; auraTokenWithheld refuses an Aura token whose named bearer is gone",
	"token.go:runTokenMints":                    "token: TokenCreate mint loop",
	"zone.go:effSeek":                           "gate: Seek's battlefield destination",
	"zone_change.go:changeZoneAttachedTo":       "attach: AttachedTo$ helper (callers mark before the move)",
	"zone_change.go:changeZoneAttachedToPlayer": "attach: AttachedToPlayer$ helper (callers mark before the move)",
	"zone_change.go:effChangeZone":              "marks: object-target loop marks before its inlined move",
	"zone_change.go:markChangeZoneAttach":       "attach: the ChangeZone marking helper itself (resolves the named set before the move)",
	"zone_change.go:settleChangeZoneMoveAs":     "marks: the shared ChangeZone settle (no attach of its own; callers attach)",
	"zone_changeall.go:effChangeZoneAll":        "gate: ChangeZoneAll has no AttachedTo$; every entry takes the CR 303.4f choice",
	"zone_hidden.go:effHiddenPick":              "marked-by zone_change.go:settleChangeZoneMoveAs",
	"zone_manifest.go:effCloak":                 "noentry: face-down entry (CR 708.5: not an Aura)",
	"zone_manifest.go:effManifest":              "noentry: face-down entry (CR 708.5: not an Aura)",
	"zone_manifest.go:manifestDreadMove":        "noentry: face-down entry (CR 708.5: not an Aura)",
	"zone_search.go:applyLibrarySearch":         "marks: library branch marks before its inlined move; other zones settle through settleChangeZoneMoveAs",
}

var auraEntryCensusRules = map[string]string{
	"aura_entry.go:settleAuraEntry":                             "attach: the CR 303.4f post-fold attach itself",
	"cast_commit.go:Engine.abortCast":                           "stack: CR 733.1 reversal",
	"cast_commit.go:Engine.payCast":                             "stack: cast",
	"emit.go:Engine.emitBookkeeping":                            "raw: priority bookkeeping kinds only",
	"emit.go:Engine.sweepExileReturn":                           "gate: UntilHostLeavesPlay return (a blink-style return to the battlefield)",
	"entry_counters.go:Engine.entryBodyCounterGrants":           "noentry: a synthetic match event, never emitted",
	"entry_counters.go:Engine.foldEntryMove":                    "raw: the entry fold, reached only after the gate",
	"entry_counters.go:Engine.foldEntryWithPlaced":              "raw: the entry fold, reached only after the gate",
	"mutate.go:Engine.resolveMutate":                            "stack: mutating spell resolution",
	"opening_hand.go:Engine.applyOpeningEffect":                 "gate: Leyline-style opening-hand entry",
	"oracle_run.go:oracleRun.build":                             "gate: oracle-audit scenario setup through e.emit",
	"oracle_run.go:oracleRun.do":                                "gate: oracle-audit scenario move op through e.emit",
	"oracle_run.go:oracleAttach":                                "attach: oracle-audit attach prelude for an existing battlefield permanent",
	"replacement_cmdzone.go:Engine.handleCmdZone":               "noentry: CR 903.9 command-zone redirect",
	"replacement_copytoken.go:Engine.ProposeCopyTokens":         "token: CreateToken replacement copy mints",
	"replacement_copytoken.go:Engine.continueCopyTokenProposal": "token: CreateToken replacement copy mints",
	"replacement_etb.go:Engine.emitAttachedMove":                "attach: Attached replacement's parked Attach",
	"replacement_mana.go:Engine.continueManaReplacements":       "raw: mana production only",
	"replacement_token.go:Engine.emitChosenCopyToken":           "token: chosen-copy token replacement",
	"replacement_token.go:Engine.emitTokenPlanMints":            "token: replacement plan mints",
	"replacement_token.go:Engine.tokenReplacementMatchesMint":   "token: a match probe, never emitted",
	"resolution_chain.go:Engine.moveResolvedOffStack":           "stack: a resolved permanent spell enters from the stack",
	"resolve_board.go:resolveBoard.Emit":                        "raw: the W3 kernel's bookkeeping emit",
	"split.go:Engine.resolveFused":                              "stack: fused split spell",
	"stack.go:Engine.ensureLeftTheStack":                        "stack: resting-zone backstop",
	"stack.go:Engine.resolveTop":                                "stack: resolution",
	"staging.go:NewStaged":                                      "raw: test-scenario staging (Config.Staged), not a game action",
	"target_ask.go:Engine.askTarget":                            "stack: fizzle off the stack",
	"token_entry_replacements.go:Engine.applyTokenEntryUpdates": "noentry: a synthetic match event, never emitted",
}

// TestAuraEntryCensus pins every effects-side and rules-side site that moves
// a card onto the battlefield, attaches one, or folds an event past
// Engine.emit (bidirectional), and asserts the explicit-bearer movers mark
// their entry before the move.
func TestAuraEntryCensus(t *testing.T) {
	t.Parallel()
	check := func(dir string, table map[string]string) {
		m := auraCensusScan(t, dir)
		auraCensusDiff(t, dir, auraCensusKeys(m, func(auraCensusFacts) bool { return true }), table)
		for k, why := range table {
			f, ok := m[k]
			if !ok {
				continue
			}
			tag, _, _ := strings.Cut(why, ":")
			if strings.HasPrefix(why, "marked-by ") {
				tag = why
			}
			switch {
			case tag == "marks" && !f.mark:
				t.Errorf("%s/%s is classified marks: but never calls MarkNamedAttachEntry/markChangeZoneAttach", dir, k)
			case strings.HasPrefix(tag, "marked-by "):
				callee := strings.TrimPrefix(tag, "marked-by ")
				if c := table[callee]; !strings.HasPrefix(c, "marks:") {
					t.Errorf("%s/%s is marked-by %s, which is not classified marks: (%q)", dir, k, callee, c)
				}
			case tag == "raw" && !f.rawEmit:
				t.Errorf("%s/%s is classified raw: but no longer calls events.Emit", dir, k)
			case f.rawEmit && tag != "raw":
				t.Errorf("%s/%s bypasses Engine.emit (events.Emit) but is classified %q", dir, k, tag)
			case f.attach && f.mover && tag != "marks" && tag != "token" && tag != "raw":
				t.Errorf("%s/%s both moves and attaches but is classified %q: an explicit-bearer entry must mark", dir, k, tag)
			}
		}
	}
	check("../effects", auraEntryCensusEffects)
	check(".", auraEntryCensusRules)
}
