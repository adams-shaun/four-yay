# Slice 1 implementation report

## Summary

This worktree did **not** complete Slice 1 and must not be treated as Craft support. It implements only the `ExileCtrlOrGrave` cost vocabulary groundwork:

- `rules/cost/cost.go`, `parse.go`, `token_match.go`: recognize the token as a `Cost.Exile` part with a battlefield+graveyard zone-set bitset. `ParseCost("5 G ExileCtrlOrGrave<1/Cave.Other>")` retains the legacy one-generic fallback and also records the part, matching the brief's stated expected `Generic=6` result.
- `rules/cost/format.go`: preserves the token head when formatting a zone-set exile part.
- `rules/pay/castparts.go`, `rules/cast_asks.go`: consider both zones in the offer and choice walks, in stable battlefield-then-graveyard order.
- `rules/cost/exile_ctrl_or_grave_test.go`: verifies parsing and the zone set.

The `.cards` corpus was present in this worktree (not a skipped corpus setup).

## Fails without the fix

I saved the implementation diff, reverse-applied it, and ran the new test against the unmodified implementation. It failed to compile because the parsed CostPart had no `ZoneSet` field, demonstrating that the assertion is sensitive to the change:

```text
# github.com/adams-shaun/gorge/rules/cost [github.com/adams-shaun/gorge/rules/cost.test]
rules/cost/exile_ctrl_or_grave_test.go:16:45: p.ZoneSet undefined (type CostPart has no field or method ZoneSet)
FAIL	github.com/adams-shaun/gorge/rules/cost [build failed]
EXIT=1
```

After restoring the implementation, the test passed.

## Commands and output

```text
$ go test ./rules/cost -run 'TestParseCostExileCtrlOrGrave|TestCostHeadTableIsSorted|TestTokenMatch'
ok   github.com/adams-shaun/gorge/rules/cost  0.823s

$ go test -run 'TestParseCostExileCtrlOrGrave|TestParseCostExileFromGraveX' ./rules/cost ./rules/
ok   github.com/adams-shaun/gorge/rules/cost  0.002s
ok   github.com/adams-shaun/gorge/rules  0.054s

$ go test ./internal/archtest/
ok   github.com/adams-shaun/gorge/internal/archtest  14.447s

$ go run ./cmd/gentypes -check
(no output; exit 0)

$ gofmt -l rules/cost/cost.go rules/cost/format.go rules/cost/parse.go rules/cost/token_match.go rules/cost/exile_ctrl_or_grave_test.go rules/cast_asks.go rules/pay/castparts.go
(no output)
```

## Not completed / deviations

The key Slice-1 behavior is absent: there is no `kw:Craft` expander, no synthetic `api:Craft.OtherShape` marker/census test, no Craft support registration, and no transform-return or remembered-material behavior. Consequently none of the 20 uniform cards has been made supported, and the four exotic carriers have not been shape-gated. The required real-card expansion, payment/bot-validator, transform/remembering, ratchet, behavior-golden, and commit acceptance checks have not been run. This is a partial cost-layer foundation only, not a complete ticket result.

## Issues

- The 20 uniform Craft carriers remain unimplemented and unsupported; `cards/keywords.go` has no Craft expander. This includes Kaslem's Stonetree.
- All four exotic carrier shapes remain unimplemented: Ore-Rich Stalactite (`ExileFromGrave`), Throne of the Grim Captain (multiple materials), Eye of Ojer Taq (`withSharedCardType`), and The Enigma Jewel (`nonLand+hasAbility Activated`). No fail-closed marker was added, so do not register `kw:Craft` until that marker and the expander are implemented together.
- Cogwork Progenitor's Seek use of the new cost token now parses, but its Seek-body payment integration was not addressed.
- No Known-approximations row or ledger entry was changed.
