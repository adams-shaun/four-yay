# Auto-pay V1 hardening wave (2026-09-26)

Authority: [the cast payment plans spec](../../specs/2026-09-24-cast-payment-plans.md)
as amended 2026-09-26 (operator decisions: no planner versioning; costly
sources as last resort with life-only confirmation; CR 302.6 inherited from
`cr302-6-sick-mana`; the shipped web mode switch plus one auto-pass fix;
float-first manual payment accepted). This directory is the deduplicated,
dependency-ordered ticket wave built from the five 2026-09-26 audits (gaps,
mana census, engine research, A/B mirror, corpus fuzz). Every brief is written
for direct `agentctl issue add --brief-file` intake and is self-contained:
seats cannot read the audit directories, so evidence is quoted and proof tests
are cited as `git show <sha>:<path>` on the throwaway proof commits
`124ed89fb` (gaps), `6c0072d63` (census), `0da6e8e59` (mirror) and
`53e8283b7` / `d22a2ea19` / `4e5224d34` (fuzz; `4e5224d34` holds the
cumulative file).

Base: main `6c711fece`. Citations were verified there.

## Queue order

Priority is agentctl's 1-3 scale (1 highest). "Offers" says whether the ticket
changes which PaymentActions/plans are published (spec §7: recorded planned
logs from before an offer change may diverge on replay; no golden may move).

| # | Ticket | Pri | Size | Lane | Depends on | Offers |
|---|---|---|---|---|---|---|
| 1 | [aph-cast-shape-gate](aph-cast-shape-gate.md) | 1 | M | pipeline | none | yes (fewer) |
| 2 | [aph-producer-tiers](aph-producer-tiers.md) | 1 | M | pipeline | none | yes (fewer) |
| 3 | [aph-lazy-offers](aph-lazy-offers.md) | 1 | M | pipeline | none | no |
| 4 | [aph-web-autopass](aph-web-autopass.md) | 2 | S | web | none | no |
| 5 | [aph-pp-coverage-port](aph-pp-coverage-port.md) | 2 | M | pipeline | autopay-exec-harden | no |
| 6 | [aph-combo-chosen-identity](aph-combo-chosen-identity.md) | 1 | M | pipeline | aph-producer-tiers, autopay-exec-harden | yes (more) |
| 7 | [aph-interference-scope](aph-interference-scope.md) | 1 | M | pipeline | aph-combo-chosen-identity, aph-cast-shape-gate | yes (more) |
| 8 | [aph-potential-granted-intrinsics](aph-potential-granted-intrinsics.md) | 2 | S | pipeline | cr302-6-sick-mana | yes (more) |
| 9 | [aph-offer-one-pass](aph-offer-one-pass.md) | 2 | M | pipeline | aph-lazy-offers, aph-interference-scope | no |
| 10 | [aph-rank-order](aph-rank-order.md) | 2 | M | pipeline | aph-interference-scope | yes (reorders) |
| 11 | [aph-plan-diagnostics](aph-plan-diagnostics.md) | 2 | S | pipeline | aph-offer-one-pass, aph-interference-scope | no |
| 12 | [aph-rank-search](aph-rank-search.md) | 2 | M | pipeline | aph-rank-order | yes (wide boards) |
| 13 | [aph-last-resort-plans](aph-last-resort-plans.md) | 2 | L | pipeline | aph-rank-search, autopay-exec-harden | yes (more; wire field) |
| 14 | [aph-last-resort-web](aph-last-resort-web.md) | 2 | M | web | aph-last-resort-plans, aph-web-autopass | no |
| 15 | [aph-hand-reserve](aph-hand-reserve.md) | 3 | S | pipeline | aph-last-resort-plans | yes (tie-breaks) |
| 16 | [aph-mirror-in-fuzz-bench](aph-mirror-in-fuzz-bench.md) | 3 | S | pipeline | aph-lazy-offers | no |
| 17 | [aph-pp-matrix-acceptance](aph-pp-matrix-acceptance.md) | 3 | M | pipeline | aph-hand-reserve, aph-last-resort-web, aph-plan-diagnostics, aph-mirror-in-fuzz-bench, aph-potential-granted-intrinsics, aph-pp-coverage-port, cr302-6-sick-mana | no |

Immediately dispatchable: 1, 2, 3, 4 (in parallel; 1 and 2 edit disjoint
functions of `rules/payment_plan.go`, see the collision map). 5 and 6 wait for
`autopay-exec-harden`; 8 waits for `cr302-6-sick-mana`.

## Dependency DAG

```text
autopay-exec-harden (in flight, Opus, wt/autopay-exec-harden) ──┐
                                                                │
aph-producer-tiers ─────────────► aph-combo-chosen-identity ◄───┤
aph-cast-shape-gate ───┐                    │                   │
                       └──────► aph-interference-scope ◄────────┘
                                   │         │
aph-lazy-offers ──► aph-offer-one-pass ◄─────┤
      │                  │                   │
      │                  └──► aph-plan-diagnostics ◄──┘ (also after interference-scope)
      │                                      │
      └──► aph-mirror-in-fuzz-bench          ▼
                                     aph-rank-order ──► aph-rank-search ──► aph-last-resort-plans ──► aph-hand-reserve
                                                                                   │
aph-web-autopass ─────────────────────────────────────────────────────► aph-last-resort-web
autopay-exec-harden ──► aph-pp-coverage-port
cr302-6-sick-mana ──► aph-potential-granted-intrinsics

all of: hand-reserve, last-resort-web, plan-diagnostics, mirror-in-fuzz-bench,
potential-granted-intrinsics, pp-coverage-port, cr302-6-sick-mana
        ──► aph-pp-matrix-acceptance
```

The long spine (`producer-tiers → combo → interference → rank-order →
rank-search → last-resort-plans → hand-reserve`) is inherent: each step edits
the source-alternative and rank functions of `rules/payment_plan.go`. It is
kept as short as the collisions allow by moving everything that does not touch
those functions off it (shape gate, lazy offers, one-pass builder,
diagnostics, potential mana, web, tooling, tests).

## File / function collision map

Tickets sharing a function are sequential (an edge above). Tickets in the
same file but disjoint functions may run in parallel; the second to merge
rebases (small conflicts expected only at the boundaries named here).

| File | Function / region | Tickets (in order) |
|---|---|---|
| `rules/payment_plan.go` | `PlanCastPayment` | cast-shape-gate → interference-scope → offer-one-pass → plan-diagnostics |
| | `paymentPlanCastShapeOK` | cast-shape-gate → interference-scope (sunburst narrowing) |
| | `paymentPlanHasTargetDependentModifier` | interference-scope |
| | `ValidateCastPayment` | cast-shape-gate → last-resort-plans |
| | `PaymentActionsForPriority`, `paymentPlanCastCandidate` | offer-one-pass → plan-diagnostics |
| | `paymentPlanManaUnits`, `paymentPlanUnitAlternatives`, `paymentPlanAltOK`, `plannedManaActivation` | producer-tiers → combo-chosen-identity → interference-scope → rank-order (flex) → last-resort-plans |
| | `paymentPlanManaInterference` | interference-scope (signature kept) |
| | `paymentPlanStepAlternative` | last-resort-plans (consequence compare) |
| | `planPaymentCost` | rank-order (rank inputs) → rank-search (rewrite) → last-resort-plans (phase 2, lethal guard) → hand-reserve |
| | `paymentPlanRank`, `less`, `rankPaymentPlan` | rank-order → last-resort-plans (key 1 inputs) → hand-reserve |
| | `paymentSourceZoneSeq` | rank-search (index) |
| | `paymentWitness` | last-resort-plans |
| `rules/cast.go` | planned executor (`paymentPlanCheck`, `paymentPlanStepReady`, `executePlannedManaActivation`, `paymentPlanFallback`) | autopay-exec-harden → combo-chosen-identity (`step.exec`) → plan-diagnostics (fallback counter) → last-resort-plans (consequence/lethal revalidation) |
| `rules/engine.go` | `ask`, `Submit`, `EnablePaymentPlanReplay` | lazy-offers → offer-one-pass (pass Options) → plan-diagnostics (Submit counter) |
| `rules/clone.go` | `replayPaymentPlans`, cached offers | lazy-offers → plan-diagnostics (sink not cloned) |
| `rules/potential.go` | `PotentialMana` | cr302-6-sick-mana → potential-granted-intrinsics |
| `rules/autopay_census_test.go` | calls planner internals | every planner ticket keeps it compiling |
| `decision/payment_plan.go` | `PaymentActivation`, codec, `validateShape` | last-resort-plans only |
| `web/src/protocol.ts` | generated | last-resort-plans (gentypes) |
| `seat/bot.go` | `paymentIntent`, consumer interface | lazy-offers (interface) → last-resort-plans (caretaker option); **bot-adapter wave** below also edits it |
| `host/match.go` | `projectNext`; caretaker construction | lazy-offers → last-resort-plans |
| `host/viewat.go`, `host/feedback.go` | pending clone | lazy-offers |
| `internal/bench/bench.go`, `internal/paymirror/*` | decision loop | lazy-offers |
| `cmd/cardfuzz/*` | drive loop / autopay | lazy-offers → mirror-in-fuzz-bench |
| `cmd/botbench/main.go` | flags | plan-diagnostics, mirror-in-fuzz-bench (disjoint flags; either order) |
| `web/src/lib/autopilot.ts`, `seatpanel.svelte.ts` (`derivePass`) | auto-pass | web-autopass |
| `web/src/lib/seatpanel.svelte.ts` (`paymentPlanSummary`, `submitPayment`), `SeatPanel.svelte`, `HandFan.svelte`, `HotButtonStrip.svelte`, `Table.svelte` | disclosure/confirm | web-autopass (dead branch) → last-resort-web |
| test files | — | each ticket adds its own new `rules/payment_plan_<slug>_test.go`; the in-flight exec-harden branch rewrites `rules/payment_plan_test.go` |

## Lanes

- `pipeline`: DeepSeek implement tier via agentctl, as usual.
- `web`: `aph-web-autopass`, `aph-last-resort-web`. Implemented by a Claude or
  codex seat, not the DeepSeek pipeline (the local seats fail on web tasks);
  the orchestrator dispatches these by hand or to a web-capable tier.

## Orchestrator notes (read before intake)

- `autopay-exec-harden` is a PLACEHOLDER dependency id for the Opus fix on
  `wt/autopay-exec-harden` (free-cast P0: residual `chooseCast` → mid-cast
  priority → spell resolves unpaid, Harrow/Thrill/Gurmag; the per-step
  production check; the choose-flow flag leaks). It is not an agentctl issue.
  agentctl treats an unknown dependency id as unmet forever, and it parses
  `Depends-On:` from the brief itself (not only `--depends`), so dropping the
  flag is not enough. Either add a placeholder issue with that id and mark it
  merged when the branch lands (preferred), or delete `autopay-exec-harden`
  from the `Depends-On:` lines of tickets 5, 6 and 13 before intake and hold
  them (and everything after 6) until it merges. Tickets that touch the planned
  executor in `rules/cast.go` (6, 11, 13) must not start before it.
- Already queued or merged audit tickets are NOT re-planned here:
  `cr302-6-sick-mana`, `offstack-mana-ask-stack-top`,
  `discard-after-remembered-draw`, `contchain-clone-fidelity`,
  `offstack-mana-counter-attr`, `mana-wheel-amount-labels`,
  `snowblind-stack-overflow`, `count-valid-derived-pt`,
  `targeted-first-spell-discount`, `bot-attack-tax-crash`,
  `offered-cast-no-legal-target`, `mana-checksvar-gate` (merged),
  `mana-tapxtype-cost`, `mana-flare-produced` (merged), `ap-wire-strict-arrays`
  (merged), `ap-feedback-copy` (merged), `ap-suffix-golden` (merged), the
  dual-land executor fix (merged `7fb66ee5f`).
- Golden policy for the wave: no approval exists. Every brief says STOP on a
  moved `TestHeads` / repo-deck replay / `make sim` / payment golden. The only
  approved head move is `cr302-6-sick-mana`'s own.
- Standing sweeps (spec §10) are on main: `cmd/paymirror`, `cmd/cardfuzz
  -autopay`, the env-gated census `rules/autopay_census_test.go`.

## Bot adapter wave (pending G1 audit)

Not planned here; placeholder so the collision map stays honest. The G1 bot
audit (`/mnt/sata/gorge-training/autopay-audit/bot/`) is still running. Known
stories to fold into that wave:

- gaps `autopay-bot-adapter-keeps-cast-mode`: `seat.Bot.paymentIntent`
  (`seat/bot.go:206`, `a, ok := payable[o.Obj]`, unchanged on main) maps the policy's chosen `cast` option back to a PaymentAction by
  object id only, so a kicked, alternative-cost (`AltCostIndex > 0`) or
  other-face cast is replaced by the plain plan. Proof
  `git show 124ed89fb:seat/bot_payment_audit_test.go`
  `TestAutoPayManaBugKeepsTheChosenCastMode` (3/3 fail). Same finding as the
  bot audit's `autopay-bot-mode-substitution`.
- The adapter hides EVERY `Kind == "activate"` option whenever any plan exists
  (`seat/bot.go:175` in `paymentIntent`, the `if o.Kind == "activate"`
  filter), so an auto-pay bot can never activate a mana ability manually on
  such a decision (the fuzz audit measured its effect on the explore seat:
  `-explore-autopay` lanes had the highest violation rate).
- Bot-audit stories already on disk and not yet triaged:
  `autopay-bot-dead-counter-blocks-fallback`, `autopay-bot-mode-substitution`,
  `autopay-bot-reserve-producible`, `autopay-cast-timing-features`,
  `autopay-hosted-bot-replay-test`, `autopay-refit-cast-profile`, and
  `planner-v1-combo-choice-sources` (a duplicate of
  `aph-combo-chosen-identity`; supersede it at intake).
- Collision: `seat/bot.go` is also edited by `aph-lazy-offers` (consumer
  interface) and `aph-last-resort-plans` (caretaker life-skip option). Sequence
  bot-adapter tickets after `aph-lazy-offers`, and either before or after
  `aph-last-resort-plans`, never concurrently with it.

## Deferred (manual only; not scheduled)

Per spec §3.2 (amended) and operator decision 2. Audit stories kept for a
later wave: census `p2-autopay-dynamic-amount` (Gaea's Cradle, Tron, Priest of
Titania, Everflowing Chalice), `p2-autopay-mana-cost-sources` (Signets, filter
lands, Cabal Coffers), `p2-autopay-reflected-production` (Chrome Mox, Mox
Amber, Fellwar Stone, Exotic Orchard), `p2-autopay-restricted-production`
(Cavern of Souls, Eldrazi Temple, Powerstone, Mishra's Workshop),
`p3-autopay-triggered-extra-mana` (Wild Growth, Utopia Sprawl, Crypt Ghast:
production-altering); plus sacrifice/tap another permanent, discard/exile/
counter/energy costs, conditional/special production, other riders, `Combo
Any`, granted/foreign and hand/graveyard mana abilities.

## Audit story disposition (dedupe)

| Audit story | Disposition |
|---|---|
| gaps `autopay-gate-announcement-shapes`, mirror `payplan-planner-announcement-costs`, fuzz `autopay-spree-plan-unexecutable`, `autopay-plans-offered-for-cast-contributions`, planner half of `autopay-additional-cost-cast-abandoned-midpayment`, gaps `autopay-mana-spent-rider-exclusions` | aph-cast-shape-gate |
| census `p1-autopay-exclude-side-effect-abilities`, gaps `autopay-exclude-side-effect-producers`, mirror `payplan-planner-side-effect-producers`, fuzz `autopay-mana-subability-side-effects-planned`, research `payplan-v2-eligibility-tiers` (tier classifier part) | aph-producer-tiers |
| census `p1-autopay-combo-and-chosen-colour`, `p2-autopay-colour-identity` | aph-combo-chosen-identity |
| census `p1-autopay-interference-scoping`, mirror `payplan-planner-manaconvert-statics`, research eligibility-tiers (scoped interference part) | aph-interference-scope |
| gaps `autopay-lazy-offer-publication`, fuzz `autopay-plan-publication-cpu-cost` | aph-lazy-offers + aph-offer-one-pass |
| gaps `autopay-potential-mana-granted-intrinsics` | aph-potential-granted-intrinsics |
| research `payplan-v2-ranking` | aph-rank-order |
| gaps `autopay-planner-search-canonical-sources`, research `payplan-v2-rank-aware-search` | aph-rank-search |
| census `p2-autopay-sacrifice-self-sources`, `p3-autopay-life-cost-sources`, research `payplan-v2-last-resort-execution-ux` | aph-last-resort-plans + aph-last-resort-web |
| research `payplan-v2-hand-reserve` | aph-hand-reserve |
| gaps `autopay-diagnostics-counters` | aph-plan-diagnostics |
| gaps `autopay-web-spec8-divergences` | spec §8 amended; aph-web-autopass |
| mirror `paymirror-in-fuzz-and-bench` | aph-mirror-in-fuzz-bench + spec §10 standing sweeps |
| gaps `autopay-acceptance-coverage-tests` | aph-pp-coverage-port; final matrix aph-pp-matrix-acceptance |
| mirror `payplan-manual-route-cr601-order` | D-MANUAL accepted (spec amendment item 5); no ticket |
| research `payplan-v1-freeze-golden`, `payplan-v2-version-pin` | DROPPED (operator decision 1) |
| gaps `autopay-executor-per-step-production-check`, mirror `payplan-exec-armed-choose-flow`, `choose-flow-flags-survive-their-decision`, executor half of fuzz `autopay-additional-cost-cast-abandoned-midpayment` | autopay-exec-harden (in flight) |
| gaps `autopay-intrinsic-executor-wrong-ability`, mirror `payplan-exec-multitype-land-colour`, fuzz `autopay-dual-land-executes-wrong-colour` | merged `7fb66ee5f` |
| gaps `rules-cr302-6-sick-mana-abilities`, census `p0-mana-ability-summoning-sickness` | cr302-6-sick-mana (queued) |
| census `p1-mana-ability-checksvar-gate`, `p2-mana-ability-tapxtype-cost`, `p2-triggered-manareflected-produced`; gaps `autopay-wire-strict-quantity-arrays`, `autopay-feedback-snapshot-copy-boundary`, `autopay-payment-suffix-golden`; mirror `offstack-*`, `mana-wheel-amount-labels`, `contchain-*`, `discard-after-remembered-draw-skipped`; fuzz `snowblind-*`, `count-valid-*`, `targeted-first-spell-*`, `bot-attack-tax-*`, `offered-cast-without-legal-target` | already queued or merged (see Orchestrator notes) |
| census `p2-autopay-dynamic-amount`, `p2-autopay-mana-cost-sources`, `p2-autopay-reflected-production`, `p2-autopay-restricted-production`, `p3-autopay-triggered-extra-mana` | deferred tier |
| gaps `autopay-bot-adapter-keeps-cast-mode`, all `bot/` stories | bot adapter wave (pending) |

## Standing rules

Every brief repeats these at its end (the web variant drops the Go-only
items). Source of truth for the copies.

- Authority: `docs/superpowers/specs/2026-09-24-cast-payment-plans.md` as
  amended 2026-09-26 (read the Amendment section first). Where this brief and
  the spec disagree, stop and report; do not pick one silently.
- Work in your own worktree (`scripts/agent-worktree.sh <ticket-id>`), stage
  explicit paths only, never `git add -A`, and add no `Co-Authored-By` or other
  attribution trailer (the hook rejects them).
- Never commit `.cards` content or Forge script text. Tests use authored IR
  (`newFixtureDeck`, `onBoard`, `card` in package `rules`) or the linked corpus
  through `testutil.CorpusRegistry` / the `fpCorpus` pattern, which reads a
  card by name at test time. Confirm your worktree has `.cards` and that your
  test runs report 0 SKIP; a corpus-dependent test that skips is not a pass.
- Do not add or grow any AGENTS.md "Known approximations" row. A deviation you
  cannot close goes in the commit message and your report.
- Goldens: if `TestHeads`, `TestRepoDeckGamesReplayExactly`,
  `TestEveryRepoDeck*`, `make sim`, `rules.TestPaymentPlanDecisionMadeGolden`
  or `decision.TestPaymentPlanIdentityIsIndependentOfPresentation` changes,
  STOP and report. No golden-update approval exists for this wave. (A planned
  intent log recorded before your change may legitimately diverge on replay,
  spec §7; tests that record and replay in-test must still pass.)
- The proof tests cited in Evidence live on throwaway branches. Read them with
  `git show <sha>:<path>` and port what you need into a NEW test file named
  for this ticket (for rules: `rules/payment_plan_<slug>_test.go`; the
  in-flight `autopay-exec-harden` branch rewrites `rules/payment_plan_test.go`).
  Never merge or cherry-pick a proof branch. Nothing under `/mnt/sata` is
  reachable from your worktree; everything you need is in this brief.
- `rules/autopay_census_test.go` (env-gated, but compiled with every
  `go test ./rules`) calls planner internals: `paymentPlanCastCandidate`,
  `paymentPlanCastShapeOK`, `paymentPlanHasTargetDependentModifier`,
  `paymentPlanManaInterference`, `paymentPlanManaUnits`,
  `paymentPlanUnitAlternatives`, `paymentPlanTapOnlyCost`. If you change one
  of those signatures, update the census's call so it still compiles and still
  measures the same thing.
- `rules/cast.go`'s planned executor (`paymentPlanCheck`,
  `paymentPlanStepReady`, `executePlannedManaActivation` on the
  `autopay-exec-harden` branch) calls `paymentPlanManaUnits`,
  `paymentPlanUnitAlternatives`, `paymentPlanStepAlternative`,
  `paymentPlanManaInterference`, `paymentPlanPoolOK`. Keep those names and
  signatures unless your brief says otherwise.
- Search the corpus with `/usr/bin/grep` (bare `grep` is ugrep and skips
  `.cards/`). Line numbers in this brief are at main `6c711fece`; navigate by
  symbol.
- Gates before you hand back: `go vet ./...`; the brief's targeted tests;
  `go test ./rules ./decision ./seat ./host ./host/httpapi ./cmd/repro -run 'PaymentPlan|AutoPayMana|AutoMana' -count=1`;
  `go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly' -count=1`;
  plus the sweep your brief names.

Web-lane briefs carry a variant: node 24 + scratch Vite cache, never `npm install`/`npm ci`/`make web`, client-only (stop on any server or Go golden change), and gates = the named vitest files, the full web suite and `svelte-check`.

## Intake commands

Run from the gorge checkout with the pinned agentctl, after resolving the
`autopay-exec-harden` placeholder (see Orchestrator notes). The web-lane
tickets are listed last and may instead be dispatched by hand.

```sh
add() { PYTHONPATH="$HOME/.agentctl/pins/current" python3 -m agentctl issue add . "$@"; }
D=docs/superpowers/plans/autopay-v1-hardening
add --id aph-cast-shape-gate --title 'Auto-pay: withhold plans from casts whose cost the witness cannot bind' --kind payment-plan --priority 1 --brief-file $D/aph-cast-shape-gate.md
add --id aph-producer-tiers --title 'Auto-pay: classify mana abilities into normal/last-resort/deferred; plan from normal only' --kind payment-plan --priority 1 --brief-file $D/aph-producer-tiers.md
add --id aph-lazy-offers --title 'Auto-pay: build PaymentActions lazily, only for consumers' --kind payment-plan --priority 1 --brief-file $D/aph-lazy-offers.md
add --id aph-pp-coverage-port --title 'Auto-pay: port passing PP-matrix audit tests; PP-20 asserts planned casts succeed' --kind payment-plan --priority 2 --depends autopay-exec-harden --brief-file $D/aph-pp-coverage-port.md
add --id aph-combo-chosen-identity --title 'Auto-pay: admit Combo, Chosen and commander colour-identity producers' --kind payment-plan --priority 1 --depends aph-producer-tiers --depends autopay-exec-harden --brief-file $D/aph-combo-chosen-identity.md
add --id aph-interference-scope --title 'Auto-pay: scope mana/tap interference per source; one global reason' --kind payment-plan --priority 1 --depends aph-combo-chosen-identity --depends aph-cast-shape-gate --brief-file $D/aph-interference-scope.md
add --id aph-potential-granted-intrinsics --title 'Auto-pay: PotentialMana sees type-granted intrinsic mana (Urborg)' --kind payment-plan --priority 2 --depends cr302-6-sick-mana --brief-file $D/aph-potential-granted-intrinsics.md
add --id aph-offer-one-pass --title 'Auto-pay: build every offer from one candidate walk and the pending Options' --kind payment-plan --priority 2 --depends aph-lazy-offers --depends aph-interference-scope --brief-file $D/aph-offer-one-pass.md
add --id aph-rank-order --title 'Auto-pay: rank by cost tier, creatures, sources; numeric tie-break' --kind payment-plan --priority 2 --depends aph-interference-scope --brief-file $D/aph-rank-order.md
add --id aph-plan-diagnostics --title 'Auto-pay: planner outcome/fallback statistics and botbench report' --kind payment-plan --priority 2 --depends aph-offer-one-pass --depends aph-interference-scope --brief-file $D/aph-plan-diagnostics.md
add --id aph-rank-search --title 'Auto-pay: rank-aware bounded search; O(1) incarnation lookup' --kind payment-plan --priority 2 --depends aph-rank-order --brief-file $D/aph-rank-search.md
add --id aph-last-resort-plans --title 'Auto-pay: last-resort plans with disclosed consequences' --kind payment-plan --priority 2 --depends aph-rank-search --depends autopay-exec-harden --brief-file $D/aph-last-resort-plans.md
add --id aph-hand-reserve --title 'Auto-pay: hand-aware tie-break' --kind payment-plan --priority 3 --depends aph-last-resort-plans --brief-file $D/aph-hand-reserve.md
add --id aph-mirror-in-fuzz-bench --title 'Auto-pay: A/B mirror inside cardfuzz and botbench (opt-in)' --kind payment-plan --priority 3 --depends aph-lazy-offers --brief-file $D/aph-mirror-in-fuzz-bench.md
add --id aph-pp-matrix-acceptance --title 'Auto-pay hardening: PP-01..28 matrix and sweep report' --kind payment-plan --priority 3 --depends aph-hand-reserve --depends aph-last-resort-web --depends aph-plan-diagnostics --depends aph-mirror-in-fuzz-bench --depends aph-potential-granted-intrinsics --depends aph-pp-coverage-port --depends cr302-6-sick-mana --brief-file $D/aph-pp-matrix-acceptance.md
# web lane (Claude/codex seat):
add --id aph-web-autopass --title 'Web auto-pay: plan-only casts are not an empty window; own-spell auto-pass follows the preference' --kind payment-plan --priority 2 --brief-file $D/aph-web-autopass.md
add --id aph-last-resort-web --title 'Web auto-pay: disclose last-resort consequences; confirm life payments' --kind payment-plan --priority 2 --depends aph-last-resort-plans --depends aph-web-autopass --brief-file $D/aph-last-resort-web.md
```
