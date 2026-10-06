# Implementation report — agent-20261006T212826Z-a78a3907

## Changes

- `rules/disable_triggers.go`: Destination$ now uses `ParamCode(PKDestination)` and `effects.Destination(code).Admits(to)`, sharing the compiled destination-list and wildcard semantics. When Destination$ is absent, no condition is applied, so the static remains unrestricted as before. Origin$ is unchanged.
- `rules/disable_triggers_destination_test.go`: added a synthetic inline-script regression. It asserts the static is active and the compiled list admits Graveyard/Exile but not Battlefield. A live control move to Battlefield queues the creature's trigger; moves to both listed zones do not. It loads no corpus.

The worktree already had `.cards` as a symlink to `/home/sadams/projects/gorge/.cards`; the new fixture itself uses only inline scripts.

## Fails without the fix

Saved the fixed production file to `.ds4/scratch/disable_triggers.go.fixed`, reverted only the production hunk, ran the targeted regression, then restored the file and verified it byte-identically with `cmp`. The no-fix run exited 1 and printed:

```text
--- FAIL: TestDisableTriggersDestinationList (0.00s)
    disable_triggers_destination_test.go:42: transition to graveyard queued 1 triggers, want 0 under Destination$ Graveyard,Exile
    disable_triggers_destination_test.go:42: transition to exile queued 1 triggers, want 0 under Destination$ Graveyard,Exile
FAIL
FAIL	github.com/adams-shaun/gorge/rules	0.007s
FAIL
```

## Gates run

Focused rules tests:

```text
$ systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestDisableTriggersDestinationList|TestDisableTriggersReadsOriginAndDestination' ./rules/ 2>&1 | tail -30
ok  	github.com/adams-shaun/gorge/rules	0.046s
```

Archtest:

```text
$ systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m ./internal/archtest/
ok  	github.com/adams-shaun/gorge/internal/archtest	22.228s
```

Botbench golden:

```text
$ systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/
ok  	github.com/adams-shaun/gorge/cmd/botbench	0.427s
```

Formatting and generated types:

```text
$ gofmt -l rules/disable_triggers.go rules/disable_triggers_destination_test.go

$ go run ./cmd/gentypes -check

```

`git diff --check` also passed with no output.

## Issues

No additional unfixed defects found. The brief reports zero current corpus carriers for comma-list DisableTriggers Destination$; this change is therefore a latent consistency fix, not a corpus behavior change. No Known-approximations row is closed by this task.
