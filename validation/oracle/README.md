# XMage compliance oracle: operator scripts and runbook

Everything the host operator needs to refresh XMage compliance verdicts is in
this directory. /mnt/sata is volatile scratch (operator, 2026-10-05): only
rebuildable outputs go there. That means the XMage build
(`make xmage-oracle-setup`), the result cache and the run directories. A lost
/mnt/sata costs one rebuild, never a script.

| Script | What it does |
|---|---|
| `full-replay.sh OUT` | regenerate + replay EVERY printed set in XMage, `diff -write` verdicts, apply triage (after any driver or generator-wide change) |
| `std-write.sh WT OLDRUN NEWRUN SET...` | per-set regenerate, replay only changed scenario files (or `FORCE_XMAGE` sets), `diff -write` |
| `std-batch.sh OUTROOT SET...` | read-only survey: gen + replay + diff, writes no verdicts |
| `verdict-compare.py BASE NEW` | row-status diff between two checkouts; exit 1 on any agree→not-agree regression |
| `../../scripts/compliance-pass.sh` | the planned pass: only stale scenarios replay (cached by XMAGE_REF + driver sha) |

## Landing a ticket parked on stale XMage compliance records

Seats cannot run XMage. A behavioural fix that moves a card's result leaves
`compliance/adopt` TestCertificationRatchet and/or `compliance/gate`
TestDeclaredSetsCompliant red; the seat commits the fix, lists the cards under
`## Stale compliance records` in its report and finishes DONE_WITH_CONCERNS
(rule in .superpowers/ds4/gorge-context.md). The module gate still fails, so
the ticket parks at human_needed. No controller step refreshes the records
automatically: it would mean the daemon running XMage and committing into the
staging worktree (an agentctl change plus a daemon restart). Land it by hand:

1. Rebase the ticket branch onto main in its worktree. Check codeshape
   (`go test ./internal/codeshape/`) first: a branch older than a ratchet
   landing fails there too.
2. Re-run the pass on the host, in the worktree (it writes
   compliance/verdicts and compliance/triage there):

       flock -o <heavy.lock> systemd-run --user --scope --quiet -p MemoryMax=24G \
         --slice=gorge-heavy.slice env COMPLIANCE_PASS_LOCK=none \
         COMPLIANCE_PASS_OUT=/mnt/sata/gorge-training/xmageoracle/runs/<ticket> \
         ./scripts/compliance-pass.sh

   Only stale scenarios replay (the XMage result cache is keyed by
   XMAGE_REF + driver sha), so a pass is minutes. Do not wrap
   scripts/xmage-oracle-run.sh itself in an outer systemd-run scope: it
   starts its own and the nested one fails ("Unit ... already loaded").
3. Compare every verdict row (`validation/oracle/verdict-compare.py <main> <worktree>`)'s status against main's before trusting the
   result (a regression is AGREE on main, not AGREE on the branch). A fix
   that is right per the rules can still regress others through the
   harness; the 2026-10-05 answer-routing ticket regressed 93 cards before
   the routing was re-derived from real replays.
4. A row whose scenario sha changed loses its per-row ruling (status back to
   diverge). Re-apply it with `oraclediff rule -card X -status xmage_wrong
   -ruling "<the same CR/Oracle citation>"` only when the citation still
   holds. Never rule a divergence the harness caused (ambiguous target
   names, an unscripted XMage choice) as xmage_wrong; leave it outstanding
   and file it. XMage answers an unconsumed choice with its AI, so a row
   can flip on re-run alone: replay the single scenario twice before
   believing a flip.
5. Record every set that fell: `oraclediff status -all -sets A,B
   -write-ratchet` (it only lowers). Never raise a value.
6. Lean gate: touched packages, `go test ./compliance/...
   ./internal/codeshape/ ./internal/archtest/`, and
   `git show main:scripts/ratchet_gate.py | python3 - main`; the full
   suite for engine changes. Then `git merge --ff-only` + push from the main
   checkout and mark the ticket merged through IssueStore.

## After a driver or generator-wide change: force a full replay

compliance-pass.sh's plan only replays a scenario whose scenario sha or
XMAGE_REF changed. A change to tools/xmageoracle (the driver) changes the
cache key but NOT the plan's staleness test, so verdict rows written by the
old driver stay in place and look fresh. On 2026-10-05 that hid a driver
bug that broke 35 counter-spell scenarios for two merges. After any driver
change, run the forced replay instead (every scenario, no plan):

    cd <worktree> && flock -o <heavy.lock> \
      validation/oracle/full-replay.sh \
      /mnt/sata/gorge-training/xmageoracle/runs/<name>

(~20 minutes for the 20 Standard sets), then compare every row against
main's before trusting it, and re-confirm any sampled automatic ruling the
rewrite reset (`oraclediff rule -card X -confirm`, e.g. Tarnation Vista).

## The Forge oracle: the standing second reference

Forge is the third reference beside gorge and XMage. It never enters the CI
gate; it is corroboration weighed alongside the Oracle text and the CR
(.ds4/forge-oracle/DESIGN.md section 3). The pass writes the compact
three-way ledger under `compliance/adjudication/<a-z>.jsonl` (operator
decision D1=C): card, template, scenario sha, forge ref, the section 8.1
pattern and the first-difference field of each pair. Those are gorge's own
vocabulary; no Forge-produced value, message or script text is committed.
Forge's raw rows, request files and caches stay under `$FORGE_ORACLE_DIR`
and are rebuildable from the pinned driver.

Run the pass after `scripts/compliance-pass.sh`, so the XMage result cache it
adjudicates against is current. Run the XMage pass at the same level (the
ledger is level B):

    LEVEL=B scripts/compliance-pass.sh    # first: the XMage leg
    scripts/forge-pass.sh                 # then: every printed set
    scripts/forge-pass.sh FDN OTJ         # or named sets only

Under the heavy lock it runs, per set, `oraclediff gen` (level B),
`forge-export`, a replay of only the requests the Forge cache does not hold,
and `forge-diff`; then one `adjudicate` over every set. The pass does not
pass `-mark-review`, so it never writes `compliance/verdicts` (P3-3's
contradiction review is a deliberate separate step: `oraclediff adjudicate
-mark-review`).

`adjudicate`'s ledger writer replaces whole `<a-z>.jsonl` shards, so a pass
over a named subset writes only those sets' rows: run the full pass (no set
arguments) to refresh the committed ledger, and treat a named-set pass as a
survey whose ledger is partial.

The cache is keyed by request sha and its directory is named by the driver
source sha and `FORGE_ORACLE_REF`, so a warm cache replays 0 scenarios and a
cold one replays all of them. **A change to the Forge driver, and a
`FORGE_ORACLE_REF` bump, are the analogue of an XMage driver change:** a new
driver starts cold, but a `FORGE_ORACLE_REF` bump whose driver source is
otherwise unchanged does not. Force every request to replay when you need to
be sure the rows are current:

    scripts/forge-pass.sh --force         # or FORGE_PASS_FORCE=1

`scripts/tests/forge_pass_smoke.sh` pins the incremental behaviour (cold
replays all, warm replays 0, `--force` replays all) with the runner and
`oraclediff` stubbed, so it needs no Java, Forge checkout or corpus. A
`FORGE_REF` bump still follows DESIGN section 7.3: merge the fork's `gorge`
branch into `oracle-harness`, bump `FORGE_ORACLE_REF`, rerun
`scripts/forge-oracle-setup.sh`.
