# Report — cli-20261006T071710Z-e6e1ad76

## What changed

- `compliance/oraclegen/templates/condition_prelude.go`: added the reusable, ordered heuristic condition-candidate builder. It maps trigger parameter/SVar text to plausible board fixtures (tapped creatures, creatures, artifacts, lands, graveyard types) and event preludes (life gain, draw, spell cast, attack, revolt/morbid). It does not evaluate conditions; `triggerWith` remains authoritative.
- `trigger_recipes.go`, `trigger.go`, `trigger_fixtures.go`: phase-trigger causes now carry additional setup and resolved prelude steps. Counter fixtures survive fixture-seat copying; source references in tapped/counter fixtures resolve against the card under test. ClassBand-only phase failures now report `trigger condition: class level` rather than the generic did-not-fire skip.
- `condition_prelude_test.go`: added representative classifier/scenario checks for Lunar Convocation, Insectoid Exterminator, Frontline War-Rager, and Creakwood Safewright. Frontline's two-tapped-creature candidate is asserted, but is not currently shown to fire (see concerns).
- `trigger_census_test.go`: re-pinned both directions for the changed trigger phase outcomes in EOE, FDN, and FRA.

The worktree had `.cards` present, so corpus-backed tests were not vacuously skipped.

## Measured results and deviations

The target census measured (compared with the prior pins): EOE served phase 6→7; FDN 14→25 and generic phase skips 12→2 (including removal of the graveyard generic skip); FRA served phase 9→10 and generic skips 6→5. Thus this implementation serves 13 additional rows in the census-covered sets, not the brief's required >=70/105. The four representative checks pass for three full generated scenarios; Frontline currently proves only that the helper offers two tapped creatures, not that the rules probe fires. The brief is therefore not fully satisfied; this is reported as `DONE_WITH_CONCERNS` rather than overstating completion.

Class-level activation is not introduced: phase requirements carrying `ClassBand$` get the named `trigger condition: class level` skip.

## Tests and gates (real output)

Focused template command:

```text
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestTriggerCensus$|TestTriggerRecipes$|TestTriggerPhase|TestConditionPrelude' ./compliance/oraclegen/templates
ok   github.com/adams-shaun/gorge/compliance/oraclegen/templates  2.130s
```

Brief's dependent packages, run one package per capped invocation:

```text
... go test -timeout 2m ./compliance/levelb/
ok   github.com/adams-shaun/gorge/compliance/levelb  0.717s
... go test -timeout 2m ./compliance/gate/
ok   github.com/adams-shaun/gorge/compliance/gate  13.085s
... go test -timeout 2m ./cmd/oraclediff/
ok   github.com/adams-shaun/gorge/cmd/oraclediff  3.052s
... go test -timeout 2m ./internal/codeshape/
ok   github.com/adams-shaun/gorge/internal/codeshape  2.087s
```

Additional required repository checks:

```text
... go test -timeout 2m ./internal/archtest/
ok   github.com/adams-shaun/gorge/internal/archtest  6.891s
... go test -timeout 2m -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/
ok   github.com/adams-shaun/gorge/cmd/botbench  0.265s
go run ./cmd/gentypes -check
(no output; exit 0)
gofmt -l <six changed Go files>
(no output)
git diff --check
(no output; exit 0)
```

## Fails without the fix

Copied `condition_prelude.go` to `.ds4/scratch/condition_prelude.go`, temporarily made `conditionPreludes` return nil, and ran:

```text
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestConditionPreludePhaseExamples' ./compliance/oraclegen/templates
--- FAIL: TestConditionPreludePhaseExamples (0.66s)
    --- FAIL: TestConditionPreludePhaseExamples/Lunar_Convocation
        condition_prelude_test.go:47: no condition candidate contains "Angel's Mercy": []
    --- FAIL: TestConditionPreludePhaseExamples/Insectoid_Exterminator
        condition_prelude_test.go:47: no condition candidate contains "Murder": []
    --- FAIL: TestConditionPreludePhaseExamples/Frontline_War-Rager
        condition_prelude_test.go:47: no condition candidate contains "Grizzly Bears": []
    --- FAIL: TestConditionPreludePhaseExamples/Creakwood_Safewright
        condition_prelude_test.go:47: no condition candidate contains "Llanowar Elves": []
FAIL
```

Restored from the scratch copy and verified `cmp` reported `RESTORED_BYTE_IDENTICAL`.

## Host replay

Newly used fixture/probe names for the host replay pass: Angel's Mercy, Divination, Grizzly Bears, Murder, Llanowar Elves, Nessian Asp, Elvish Mystic, Bonesplitter, Sol Ring, Arcane Signet, Town of Orazca, Plains, Island, Swamp, Mountain, Forest, Shock. Expected host-agree rows are the phase trigger rows newly yielding scenarios after this change; identify the full set on the operator's all-set compliance pass. XMage is unavailable in the seat.

## Issues

- Target achieved so far is 13 additional served phase rows in the available pinned trigger census, below the required >=70/105. The current matcher only offers the heuristic candidates in the helper and this round did not measure all 17 sets with the `oraclediff status -level B` sweep.
- Frontline War-Rager still produces `trigger did not fire` even with two tapped creature fixtures. The test confirms the candidate exists; a real scenario-fire check is still needed to determine whether the setup encoding or trigger matching is the limiting factor. No rules-engine behavior was changed because that is outside the brief's template scope.
- The brief calls for >=70 rows and four representative cards actually served. Those requirements remain open; operator host replay is also pending.
