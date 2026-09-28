# AlphaZero-style MCTS (tickets 1–2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/azmcts` (a single-perspective PUCT tree whose leaf is a value, never a rollout, over clairvoyant engine clones) and the `az` seat in `cmd/botbench`, then hand the operator a runnable Stage 0 procedure that measures gen-0 cost and strength against `bot` before any training code (tickets 3–5) exists.

**Architecture:** A pure PUCT core (`RunTree`) runs over a small `Env`/`EnvSource` seam, so its arithmetic is tested against a fake environment with no engine. An engine adapter (`engineEnv`) walks one engine clone per simulation: the searching seat's *searched* decisions (priority, attackers, blockers, single target) are tree nodes whose candidates come from the exported `searchprobe` enumerators and are keyed by semantic `searchprobe.Action` lists; every other decision, for both seats, is answered by `botpolicy.Decide`. `Search` builds the root, runs the tree, and maps the chosen candidate back to an intent (candidate 0 is always the bot's answer, and the answer on every failure path). The `az` seat implements `searchseat.SearchSeat`, so `internal/bench.PlayGame` hands it the live engine exactly as it does the L10 search seat. A clairvoyant world is refused unless the driving command calls `azmcts.AllowClairvoyant()`, which only `cmd/botbench` does.

**Tech Stack:** Go (stdlib only), gorge rules engine, internal/policynet
**Spec:** docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md

## Global Constraints

- Go stdlib only: no third-party dependency, no cgo (AGENTS.md hard rules).
- No wall clock in `internal/azmcts`: timing reaches it only through the injected `azmcts.Millis`, installed by `cmd/botbench` (`internal/archtest` `TestTimeIsImportedOnlyByTheHost`, `internal/archtest/arch_test.go:100`).
- No ambient randomness: every stream is `math/rand/v2` PCG seeded from `azmcts.DecisionSeed(seat seed, d.Seq)`; never `math/rand` v1 (`TestNoLegacyMathRand`).
- No `map` range whose order reaches a choice, an event or printed output; the tree uses slices only; reports iterate fixed slices.
- All state mutation goes through `events.Apply`: `azmcts` never writes a `state.Game` field; it only calls `Clone`, `CloneHypothetical`, `Submit` and `SubmitHypothetical` on clones and never touches the real engine.
- Opt-in only: the default `bot`, the `TestHeads` goldens in `rules/heads_test.go`, and botbench output for any run without an `az` side are byte-unchanged; the `searchseat` pn10 prior ranks byte-identically after `candidateScore` moves.
- Spec values: PUCT `c = 1.5`; first-play urgency 0.1; Dirichlet noise α 0.3, ε 0.25 on the root prior; moves sampled ∝ visits (τ = 1) on turns 1–4. Noise and sampling are generation-only; eval is argmax with no noise. `-az-sims` defaults to 100. Candidate 0 is the bot's answer and wins every tie.
- A clairvoyant world is bench/training only. `host`, `host/httpapi` and `cmd/gorged` must never link `internal/azmcts`.
- Every fallback is counted in `azmcts.Stats` and printed by the botbench az report. None is silent (spec §4).
- Tests run as `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 ./<pkg>/...` (with `-count=1 -run '<regex>'` where a step names one), and only one heavy job runs at a time on the machine.
- Do not run `go build ./...`/`go test ./...` across the whole module in a task step; each step names its package.
- Corpus-dependent tests need `.cards` in the worktree. A worktree without it SKIPS those tests and still reports `ok` in about 2 ms. If `ls .cards` fails, stop and say so; do not report green.
- Work in a task worktree made by `scripts/agent-worktree.sh <id>`. Never `git checkout`, `git switch`, `git reset` or bare `git stash`, and never `git add -A`.
- Commits stage explicit paths only. No `Co-Authored-By` or any other attribution trailer, and no issue-tracker reference (`.githooks/commit-msg` rejects both).
- AGENTS.md "Known approximations" is frozen. No row may be added or grown. A deviation goes in the commit message and the task report.
- Stage 0 runs (Task 9) use exactly: `systemd-run --user --scope -q -p MemoryMax=5G`, `GOMEMLIMIT=1GiB`, `taskset -c 12,28`, `-workers 2`, with output and `GOTMPDIR` under `/mnt/sata/gorge-training/az/stage0/`. Task 9 writes the procedure and does not run it.

## Review Focus

These are the input classes the spec implies but no happy-path test would reach, most likely first. Each one has a test in the task that owns it.

1. **The bot's answer lies outside the candidate vocabulary.** At priority the bot plays a land (`"play_land"`), taps mana (`"activate"`), or returns an auto-pay `Payment` intent. `searchprobe.Candidates` pools only `cast`/`ability`/`pass` (`internal/searchprobe/rollout.go:17-58`), and `Collector.Actions` cannot express `Intent.Payment` (`decision/decision.go:1127-1129`). The decision must be answered with the bot's own intent, counted `Skipped`, and never mis-mapped. Tests: Task 4 `TestEnumerateRefusesAPaymentIntent`; Task 5 `TestSearchSkipsWhenTheBotPlaysALand`.
2. **The game ends inside the env step,** for example the opponent's lethal attack while `bot` is answering between searched decisions. The walk then ends with no next point, at depth 1 or deeper, and must back up 1/0/0.5 and count `Terminal`. Tests: Task 3 `TestBackupAlongPath` (terminal leaves at depths 1 and 2); Task 5 `checkResult`'s invariant `Terminal + StepCapped + Expanded == Completed` over real games.
3. **A world that is not at the root decision when it is produced:** `Pending()` nil, game over, or a different `Seq`. Such a simulation is discarded and counted `BadWorlds`/`NoWorld`. If every simulation fails, the bot's answer is played and `AllFailed` is counted. Test: Task 5 `TestSearchBadWorldPlaysTheBot`.
4. **Candidate lists of length 0 or 1,** at the root and inside a simulation. Such a decision is never a node. At the root the bot's answer is returned without asking the world source; inside a walk the env plays the bot. Tests: Task 4 `TestEnumerateOneOrZeroCandidates`; Task 5 `TestSearchSkipsWhenTheBotPlaysALand` (asserts the source is never called).
5. **The step cap is hit on every simulation.** Every leaf is a capped evaluation, visits still decide, and the counters stay exact. Tests: Task 3 `TestStepCapOnEverySimulation`; Task 5 `TestSearchStepCapBoundsEveryWalk`.

The owning tasks also test these: an engine panic inside a clone is recovered and counted (Task 5 `TestSubmitRecoversAnEnginePanic`), and the root collector must be a fresh one, never a `searchseat.Feed`'s. The latter is documented on `Collector.ObserveDecision`, and the seat always builds a new collector.

---

## Spec statements found false against the code, and how this plan handles them

| Spec statement | Code at base `457efcb9` | Plan |
|---|---|---|
| §1 Env step: the seat's own trivial kinds are answered "and mana taps through the auto-pay adapter". | The bench `bot` is the manual bot (`host.NewBotPolicySeat` → `seat.NewBot`, `host/bot_policy.go:42,98`), and so are the search rollouts (`botpolicy.Decide`, `internal/searchprobe/teacher.go:215`). An auto-pay answer is `Intent{Payment: ...}`, which is exclusive with `Choices` (`decision/decision.go:1127-1129`), and `Collector.IntentActions` translates only `Choices`/`Rest` (`internal/searchprobe/action.go:48-72`). | The seat's own non-searched answers and every env answer use the manual `botpolicy.Decide`. The seat wraps `seat.NewBot`, the policy the Stage 0 opponent plays. A `Payment` intent is never searched; it is counted `Skipped` (Review Focus 1). Auto-pay support belongs to a later ticket. |
| §1 Priority is searched "when it offers more than pass/concede: the option list". | `searchprobe.Candidates` keeps only `cast`/`ability`/`pass` options and returns nil when the bot's answer has another kind (`rollout.go:17-58`). Land plays are `"play_land"` and mana taps are `"activate"` (`rules/legal.go`). | The plan keeps `searchprobe.Candidates`, as the spec directs. Land plays and mana taps are never candidates, and a decision the bot answers with one is played as the bot and counted `Skipped`. |
| §1/§4 Attackers **and blockers** priors use "the `candidateScore` log-likelihood". | `candidateScore` (`internal/searchseat/prior.go:155`) scores every kind other than `"attackers"` as one softmax option, so a blockers candidate (a subset) scores `-Inf`. It is also unexported. | It moves to `policynet.CandidateScore(subset bool, scores, choices)` (Task 1), with `subset` set for attackers and blockers. `searchseat.candidateScore` delegates with `subset = kind == "attackers"`, which is byte-identical and pinned by a test. |
| §1 `Search(src WorldSource, net *policynet.Model, opts Options)`. | Candidate 0 (the bot's answer, drawn from the seat's own RNG) and the real decision cannot be derived from a world source. | `Search(root Root, src WorldSource, net *policynet.Model, opts Options)`. `Root` carries the real engine, the decision, the bot's answer and the root collector. |
| §1 Action identity via `Collector.Actions`/`Collector.Match` inside simulations. | `Collector.action` refuses any object the collector has not introduced ("action references an unobserved object", `internal/searchprobe/action.go:127-129`), and only `Capture` introduces objects. A clone walked past the last capture therefore cannot name its own options. | Task 2 adds `(*Collector).ObserveDecision(e, d)`, which introduces a decision's own objects cheaply. Each simulation clones the root collector. |

`searchseat.Eligible` (`internal/searchseat/searchseat.go:242`) is not reused either. Its priority gate is "≥ 2 distinct castable objects" (`:250`), which is not the spec's rule, and the dispatch it pairs with, `searchseat.candidates` (`:445`), is unexported and bound to `searchseat.Options` and feed frames. Task 4 therefore writes a roughly 50-line dispatch in `azmcts` over the same exported enumerators. The other spec citations were checked at this base and hold: `rules/clone.go:51`, `rules/chance.go:69,83`, `searchprobe/sample.go:158`, `redeal.go:39`, `rollout.go:17`, `teacher.go:89,317,414,501,512`, `action.go:39,76` and `policynet/net.go:212`.

## Decisions the operator should know about

- **`candidateScore` moves; it is not exported from `searchseat`.** Ticket 3's `policytrain -loss visits` needs the same Bernoulli form, and importing `searchseat` into the trainer would pull in the rules engine. `policynet` owns head semantics and has no engine dependency.
- **The seat is `azmcts.Seat`**, not a botbench-local type, so ticket 3's `cmd/azgen` can reuse it.
- **Env RNG is common across simulations.** Every simulation of one decision seeds the env bots from the same decision seed. A clairvoyant clone is therefore deterministic along a path (spec §2 "reduces to an ordinary tree").
- **The clairvoyant root collector is fresh (`NewCollector`), not the feed's.** The seat still takes the `SearchSeat` route to receive the engine, so the driver still captures a feed at every decision that stage 1 does not read. Stage 0's games/h includes that capture overhead (INFERRED small; measured nowhere yet).
- **`-az-world` has no default.** An `az` side must say `clairvoyant`, and `sampled` is refused until ticket 5.
- **`-az-candidates` defaults to 6** (the L10 teacher's `Limit`), and `-az-max-steps` to 1000 env submits per simulation.
- **Stage 0 "200 games per arm"** is read as 40 games per pair × 5 pairs per arm, with three arms: bot-vs-bot control, az at 25 sims, and az at 100 sims.
- **Stage 0 kill criterion, operationalised (controller decision 2026-09-27; operator may override):** KILL if no az arm's pooled win-rate 95% CI lies entirely above the same-seed bot-vs-bot control's pooled win rate (the control arm, not a flat 50%: the deck pairs are not symmetric).

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `internal/policynet/candidate.go` | create | `CandidateScore`: a head's log-likelihood/score of one candidate (subset BCE or single-option softmax). |
| `internal/policynet/candidate_test.go` | create | Pins `CandidateScore` values. |
| `internal/searchseat/prior.go` | modify | `candidateScore` delegates to `policynet.CandidateScore`; its private `softplus` is removed. |
| `internal/searchseat/prior_move_test.go` | create | Pins the delegation bit-for-bit against the pre-move formula. |
| `internal/searchprobe/observedecision.go` | create | `(*Collector).ObserveDecision`: introduces one decision's objects without a Capture. |
| `internal/searchprobe/observedecision_test.go` | create | Synthetic refusal tests plus a corpus round-trip test. |
| `internal/azmcts/options.go` | create | `Kinds`, `ParseKinds`, `Options`, `DefaultOptions`, `Stats`, `Stats.Add`, error sentinels. |
| `internal/azmcts/tree.go` | create | Package doc and the PUCT core over `Env`/`EnvSource`: `RunTree`, selection, backup, availability, Dirichlet noise, move choice. |
| `internal/azmcts/tree_test.go` | create | Fake env plus PUCT/FPU/availability/backup/failure/step-cap/noise/sampling tests. |
| `internal/azmcts/candidates.go` | create | Searched-kind dispatch over the searchprobe enumerators, semantic keys, priors (uniform or network softmax). |
| `internal/azmcts/candidates_test.go` | create | Enumeration, 0/1-candidate, payment-intent, key and prior tests (no corpus). |
| `internal/azmcts/world.go` | create | `World`, `WorldSource`, the `Clairvoyant` source, `AllowClairvoyant`/`ErrClairvoyantRefused`. |
| `internal/azmcts/env.go` | create | `engineEnv`: walks one engine clone; bot answers between searched decisions; step cap; panic recovery; leaf value. |
| `internal/azmcts/search.go` | create | `Root`, `Result`, `Options.Validate`, `Search`, `DecisionSeed`. |
| `internal/azmcts/world_test.go` | create | Clairvoyant refusal, submit panic/classification (no corpus). |
| `internal/azmcts/search_test.go` | create | Real-deck integration, determinism, network prior, hypothetical worlds and Review Focus tests. |
| `internal/azmcts/seat.go` | create | `Seat` (a `searchseat.SearchSeat`), `SeatConfig`, `Diag`, `Watch`, `Millis`. |
| `internal/azmcts/seat_test.go` | create | `PlayGame` tests: zero sims equals bot, replay determinism, refusal. |
| `internal/azmcts/bench_test.go` | create | `BenchmarkSearchDecisionEarly`/`Late`: ns per simulation and allocations per searched decision. |
| `cmd/botbench/azcost.go` | create | az flags, `azFrontDoor`, cost/counter report. |
| `cmd/botbench/azcost_test.go` | create | Front door, report arithmetic, end-to-end pair run. |
| `cmd/botbench/main.go` | modify | `az` policy entry, flag registration, `-checkpoint` gate, report printing, opp-mix exclusion. |
| `host/bot_policy_az_test.go` | create | Pins that no search policy is a hosted policy. |
| `host/httpapi/game_test.go` | modify | Adds `az` to the HTTP 400 refusal list. |
| `internal/archtest/arch_test.go` | modify | Forbids `host`, `host/httpapi`, `cmd/gorged` → `internal/azmcts`. |
| `scripts/az-stage0.sh` | create | The Stage 0 measurement procedure (not run by the implementer). |

---

## Task 1: Move `candidateScore` into `policynet.CandidateScore`

**Files:**
- Create: `internal/policynet/candidate.go`, `internal/policynet/candidate_test.go`, `internal/searchseat/prior_move_test.go`
- Modify: `internal/searchseat/prior.go` (lines 150-185)

**Interfaces:**
- Consumes: `internal/searchseat/prior.go:155` `func candidateScore(kind string, scores []float32, choices []int) float64` (the current body).
- Produces: `func CandidateScore(subset bool, scores []float32, choices []int) float64` in package `policynet`; `searchseat.candidateScore` keeps its signature.

- [ ] **Step 1: Write the failing tests**

`internal/policynet/candidate_test.go`:

```go
package policynet

import (
	"math"
	"testing"
)

func TestCandidateScoreSingleChoice(t *testing.T) {
	scores := []float32{0.5, -1.25, 2}
	if got := CandidateScore(false, scores, []int{2}); got != 2 {
		t.Fatalf("single choice 2 = %v, want its score 2", got)
	}
	for _, bad := range [][]int{nil, {0, 1}, {-1}, {3}} {
		if got := CandidateScore(false, scores, bad); !math.IsInf(got, -1) {
			t.Errorf("single-option head, choices %v = %v, want -Inf", bad, got)
		}
	}
}

func TestCandidateScoreSubsetIsBernoulliLogLikelihood(t *testing.T) {
	scores := []float32{0.5, -1.25, 2}
	sig := func(z float64) float64 { return 1 / (1 + math.Exp(-z)) }
	want := math.Log(sig(0.5)) + math.Log(1-sig(-1.25)) + math.Log(sig(2))
	got := CandidateScore(true, scores, []int{0, 2})
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("subset {0,2} = %v, want %v", got, want)
	}
	wantNone := math.Log(1-sig(0.5)) + math.Log(1-sig(-1.25)) + math.Log(1-sig(2))
	if got := CandidateScore(true, scores, nil); math.Abs(got-wantNone) > 1e-12 {
		t.Fatalf("empty declaration = %v, want %v", got, wantNone)
	}
	if again := CandidateScore(true, scores, []int{0, 2, 9, -1}); again != got {
		t.Fatalf("out-of-range choices changed the score: %v vs %v", again, got)
	}
}
```

`internal/searchseat/prior_move_test.go`:

```go
package searchseat

import (
	"math"
	"testing"
)

// preMoveCandidateScore is candidateScore exactly as it stood at 457efcb9
// (prior.go:155-185), kept here so the move to policynet is pinned bit for
// bit: the pn10 prior's ranking must not change.
func preMoveCandidateScore(kind string, scores []float32, choices []int) float64 {
	sp := func(z float64) float64 {
		if z > 0 {
			return z + math.Log1p(math.Exp(-z))
		}
		return math.Log1p(math.Exp(z))
	}
	if kind != "attackers" {
		if len(choices) != 1 || choices[0] < 0 || choices[0] >= len(scores) {
			return math.Inf(-1)
		}
		return float64(scores[choices[0]])
	}
	in := make([]bool, len(scores))
	for _, c := range choices {
		if c >= 0 && c < len(in) {
			in[c] = true
		}
	}
	ll := 0.0
	for i, s := range scores {
		if in[i] {
			ll -= sp(-float64(s))
		} else {
			ll -= sp(float64(s))
		}
	}
	return ll
}

func TestCandidateScoreMoveIsBitIdentical(t *testing.T) {
	scores := []float32{0.3, -2, 1.5, 0, 45, -45}
	for _, kind := range []string{"attackers", "cast", "blockers", "target"} {
		for _, choices := range [][]int{nil, {0}, {2}, {0, 2}, {4}, {5}, {0, 1, 2, 3, 4, 5}, {7}} {
			got := candidateScore(kind, scores, choices)
			want := preMoveCandidateScore(kind, scores, choices)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("candidateScore(%s, %v) = %v, pre-move %v", kind, choices, got, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCandidateScore' ./internal/policynet/`
Expected: FAIL to compile: `undefined: CandidateScore`.
(`./internal/searchseat` still passes at this point: its test pins the unchanged function.)

- [ ] **Step 3: Implement**

`internal/policynet/candidate.go`:

```go
package policynet

import "math"

// CandidateScore is a trained policy head's opinion of one candidate answer
// to a decision, given the per-option scores Model.Score returned for it
// (scores parallels the decision's Options; choices are the candidate's
// option indices).
//
// subset selects the head's loss family. Attackers and blockers are trained
// with a per-option binary (BCE) loss, so a declared subset's log-likelihood
// is sum log sigmoid(s_i) over the included options plus sum
// log(1 - sigmoid(s_j)) over the excluded ones (out-of-range choices are
// ignored). Priority and target are softmax (CE) heads: a single option's
// score ranks it, and any other shape -- no choice, several, an index out of
// range -- is -Inf.
//
// It is the one definition shared by the pn10 candidate prior
// (internal/searchseat delegates to it), the AlphaZero search's priors
// (internal/azmcts) and ticket 3's visit-distribution loss, which spreads a
// subset target onto the same Bernoulli form. candidateSoftplus is kept apart
// from the training softplus in net.go on purpose: the pn10 prior ranked with
// this exact function before it moved here, and the training version's +/-30
// cut-offs would change low bits of a log-likelihood and could reorder
// near-tied candidates.
func CandidateScore(subset bool, scores []float32, choices []int) float64 {
	if !subset {
		if len(choices) != 1 || choices[0] < 0 || choices[0] >= len(scores) {
			return math.Inf(-1)
		}
		return float64(scores[choices[0]])
	}
	in := make([]bool, len(scores))
	for _, c := range choices {
		if c >= 0 && c < len(in) {
			in[c] = true
		}
	}
	ll := 0.0
	for i, s := range scores {
		if in[i] {
			ll -= candidateSoftplus(-float64(s)) // log sigmoid(s)
		} else {
			ll -= candidateSoftplus(float64(s)) // log(1 - sigmoid(s))
		}
	}
	return ll
}

// candidateSoftplus is log(1 + e^z) without overflow -- byte for byte the
// softplus internal/searchseat's prior used before CandidateScore moved here.
func candidateSoftplus(z float64) float64 {
	if z > 0 {
		return z + math.Log1p(math.Exp(-z))
	}
	return math.Log1p(math.Exp(z))
}
```

In `internal/searchseat/prior.go`, replace everything from the comment line `// candidateScore is the head's opinion of one candidate. Attackers are` through the end of `func softplus` (lines 150-185) with:

```go
// candidateScore is the head's opinion of one candidate: the attackers head
// is a per-option BCE head (subset log-likelihood), every other kind this
// prior ranks is a single-option softmax head. The definition lives in
// policynet.CandidateScore, shared with the AlphaZero search; the pn10 prior
// has never ranked blockers, so kind == "attackers" is exactly the pre-move
// behaviour (pinned by TestCandidateScoreMoveIsBitIdentical).
func candidateScore(kind string, scores []float32, choices []int) float64 {
	return policynet.CandidateScore(kind == "attackers", scores, choices)
}
```

Then delete `"math"` from the import block of `internal/searchseat/prior.go`, since nothing else in the file uses it (verify with `git grep -n "math\." internal/searchseat/prior.go`, which must print nothing).

- [ ] **Step 4: Run and pass**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCandidateScore' ./internal/policynet/`
Expected: PASS.
Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCandidateScoreMoveIsBitIdentical|TestPriorOrder|TestPriorOnRealGameDecisions' ./internal/searchseat/`
Expected: PASS (`TestPriorOnRealGameDecisions` needs `.cards`; confirm it ran with `-v` and did not SKIP).

- [ ] **Step 5: Commit**

```bash
git add internal/policynet/candidate.go internal/policynet/candidate_test.go internal/searchseat/prior.go internal/searchseat/prior_move_test.go
git commit -m "refactor(policynet): move the candidate log-likelihood out of searchseat

CandidateScore(subset, scores, choices) is the pn10 prior's candidateScore,
moved so the AlphaZero search (and ticket 3's visits loss) can score
attackers AND blockers subsets without importing searchseat. searchseat
delegates with subset = kind == \"attackers\"; a test pins the move bit for
bit against the pre-move formula."
```

---

## Task 2: `Collector.ObserveDecision`

**Files:**
- Create: `internal/searchprobe/observedecision.go`, `internal/searchprobe/observedecision_test.go`

**Interfaces:**
- Consumes: `(*Collector).introduce(e *rules.Engine, id state.ObjID)` (`internal/searchprobe/observation.go:324`), `(*Collector).observeDecision(d *decision.Decision) (*ObservedDecision, error)` (`internal/searchprobe/action.go:139`), `NewCollector` (`observation.go:63`), `(*Collector).Actions`/`Match` (`action.go:39,76`), `BotRandoms` (`rollout.go:213`), `botpolicy.Decide` (`botpolicy/policy.go:351`), `botpolicy.BoardFromGameInto` (`botpolicy/combat.go:144`), `testutil.CorpusRegistry`/`LoadRepoDeck` (`internal/testutil/decks.go:218,97`).
- Produces: `func (c *Collector) ObserveDecision(e *rules.Engine, d *decision.Decision) (*ObservedDecision, error)`.

- [ ] **Step 1: Write the failing tests**

`internal/searchprobe/observedecision_test.go`:

```go
package searchprobe

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestObserveDecisionRefusesOtherSeatsAndNil(t *testing.T) {
	c := NewCollector(0)
	if _, err := c.ObserveDecision(nil, nil); err == nil {
		t.Fatal("a nil decision was observed")
	}
	d := &decision.Decision{Seq: 3, Player: 1, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
	if _, err := c.ObserveDecision(nil, d); err == nil {
		t.Fatal("another seat's private decision was observed")
	}
}

// A fresh collector that has never Captured anything can name, and match
// back, every option of the actor's own decision once ObserveDecision has
// run -- the property an engine clone walked past the last Capture needs.
func TestObserveDecisionNamesEveryOptionOfARealDecision(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, "mono-red-prowess")
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, "mono-blue-tempo")
	if err != nil {
		t.Fatal(err)
	}
	const seed = uint64(30000000)
	e := rules.New(rules.Config{Seed: seed, Names: []string{"mono-red-prowess", "mono-blue-tempo"},
		Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens})
	e.Advance()
	rngs := BotRandoms(seed, 2)
	board := botpolicy.NewBoard(2)
	checked := 0
	for steps := 0; steps < 600 && !e.G.Over && checked < 25; steps++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision at step %d", steps)
		}
		if d.Player == 0 && len(d.Options) > 1 {
			c := NewCollector(0)
			od, err := c.ObserveDecision(e, d)
			if err != nil {
				t.Fatalf("step %d (%s): ObserveDecision: %v", steps, d.Kind, err)
			}
			if len(od.Options) != len(d.Options) {
				t.Fatalf("step %d: observed %d options, decision has %d", steps, len(od.Options), len(d.Options))
			}
			for i := range d.Options {
				in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{i}}
				if d.Validate(in) != nil {
					continue
				}
				acts, err := c.Actions(d, in)
				if err != nil {
					t.Fatalf("step %d option %d: Actions after ObserveDecision: %v", steps, i, err)
				}
				if acts[0] != od.Options[i].Action {
					t.Fatalf("step %d option %d: Actions %+v, observed %+v", steps, i, acts[0], od.Options[i].Action)
				}
				back, err := c.Match(d, acts)
				if err != nil {
					if strings.Contains(err.Error(), "unobserved") {
						t.Fatalf("step %d option %d: Match: %v", steps, i, err)
					}
					continue // an ambiguous semantic action is a legitimate refusal
				}
				if len(back.Choices) != 1 || back.Choices[0] != i {
					t.Fatalf("step %d option %d: matched back to %v", steps, i, back.Choices)
				}
			}
			checked++
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if err := e.Submit(in); err != nil {
			t.Fatalf("step %d submit: %v", steps, err)
		}
	}
	if checked == 0 {
		t.Fatal("seat 0 never faced a multi-option decision")
	}
}
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestObserveDecision' ./internal/searchprobe/`
Expected: FAIL to compile: `c.ObserveDecision undefined (type *Collector has no field or method ObserveDecision)`.

- [ ] **Step 3: Implement**

`internal/searchprobe/observedecision.go`:

```go
package searchprobe

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
)

// ObserveDecision introduces the objects the actor's own decision d names --
// its source, then every option's object and attacker, in option order --
// and returns d's observed form, the shape Capture records as
// Frame.Decision. Unlike Capture it projects no board, encodes nothing and
// reads no event burst, so it is cheap enough to call at every searched
// decision of a private engine clone walked past the last Capture
// (internal/azmcts's simulations), where Actions and Match would otherwise
// refuse objects no frame has shown ("action references an unobserved
// object").
//
// It mutates c: identities are assigned in introduction order. A caller that
// still needs c's Capture stream -- a searchseat.Feed's collector -- must
// Clone it first, or the feed's next frame would omit these identities.
// Options are read raw, not through view.Project's label rewrite, so the
// returned actions agree with Actions and Match, which read the raw decision
// too. e may be nil only when d names no object (every Source, Obj and
// Attacker zero).
func (c *Collector) ObserveDecision(e *rules.Engine, d *decision.Decision) (*ObservedDecision, error) {
	if d == nil {
		return nil, fmt.Errorf("no decision to observe")
	}
	if d.Player != c.actor {
		return nil, fmt.Errorf("opponent private decision entered observation")
	}
	c.introduce(e, d.Source)
	for _, o := range d.Options {
		c.introduce(e, o.Obj)
		c.introduce(e, o.Attacker)
	}
	return c.observeDecision(d)
}
```

- [ ] **Step 4: Run and pass**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -v -run 'TestObserveDecision' ./internal/searchprobe/`
Expected: PASS, and `TestObserveDecisionNamesEveryOptionOfARealDecision` reports `--- PASS`, not `--- SKIP`.

- [ ] **Step 5: Commit**

```bash
git add internal/searchprobe/observedecision.go internal/searchprobe/observedecision_test.go
git commit -m "feat(searchprobe): ObserveDecision names a decision past the last Capture

A search that walks an engine clone beyond its collector's last Capture
could not translate its own options (Collector.action refuses unobserved
objects). ObserveDecision introduces exactly the decision's source, option
objects and attackers, in option order, and returns the observed decision
without projecting or encoding a board."
```

---

## Task 3: The PUCT core over a fake-env seam

**Files:**
- Create: `internal/azmcts/options.go`, `internal/azmcts/tree.go`, `internal/azmcts/tree_test.go`

**Interfaces:**
- Consumes: stdlib only.
- Produces:
  - `type Key string`; `type Point struct { Keys []Key; Prior []float64 }`; `type Leaf struct { V float64; Terminal, Capped bool }`
  - `type Env interface { Root() *Point; Play(k Key) (*Point, error); Leaf() Leaf }`; `type EnvSource interface { Env(sim int) (Env, error) }`
  - `type Kinds struct { Priority, Attackers, Blockers, Target bool }`; `func AllKinds() Kinds`; `func ParseKinds(s string) (Kinds, error)`
  - `type Options struct { Sims int; CPUCT, FPU float64; Limit, MaxSteps int; Kinds Kinds; Seed uint64; Noise bool; DirichletAlpha, DirichletEps float64; Sample bool }`; `func DefaultOptions() Options`
  - `type Stats struct { Searched, Skipped, Simulations, Completed, ChanceFailures, Panics, SubmitErrors, BadWorlds, NoWorld, AllFailed, StepCapped, Terminal, Expanded, Unavailable, EnvSteps, PriorFallbacks, FeedStopped int }`; `func (s *Stats) Add(o Stats)`
  - `var ErrChance, ErrPanic, ErrSubmit, ErrBadWorld, ErrNoWorld error`
  - `type TreeResult struct { Visits []int; Q []float64; Avail []int; RootValue float64 }`; `func RunTree(root *Point, src EnvSource, opts Options, st *Stats) (TreeResult, error)`
  - unexported: `puctScore`, `choose`, `dirichlet`, `gammaDraw`, `noisyPrior`, `uniform`, `classify`

- [ ] **Step 1: Write the failing tests**

`internal/azmcts/tree_test.go`:

```go
package azmcts

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

// fakeNode is one position of a hand-built game tree, addressed by the path
// of keys played from the root ("" is the root, "/a/x" two plies down).
type fakeNode struct {
	keys     []Key
	prior    []float64
	value    float64
	terminal bool
	capped   bool
	err      error // Play into this path always fails with err
}

type fakeGame struct {
	nodes    map[string]fakeNode
	failOnce map[string]error        // Play into this path fails once, then plays normally
	rootFor  func(sim int) *Point // per-world root offer (availability); nil = nodes[""]
}

type fakeEnv struct {
	g    *fakeGame
	sim  int
	path string
}

func (f *fakeEnv) Root() *Point {
	if f.g.rootFor != nil {
		return f.g.rootFor(f.sim)
	}
	n := f.g.nodes[""]
	return &Point{Keys: n.keys, Prior: n.prior}
}

func (f *fakeEnv) Play(k Key) (*Point, error) {
	f.path += "/" + string(k)
	if err, ok := f.g.failOnce[f.path]; ok {
		delete(f.g.failOnce, f.path)
		return nil, err
	}
	n, ok := f.g.nodes[f.path]
	if !ok {
		return nil, fmt.Errorf("fake: no node at %q", f.path)
	}
	if n.err != nil {
		return nil, n.err
	}
	if n.terminal || n.capped {
		return nil, nil
	}
	return &Point{Keys: n.keys, Prior: n.prior}, nil
}

func (f *fakeEnv) Leaf() Leaf {
	n := f.g.nodes[f.path]
	return Leaf{V: n.value, Terminal: n.terminal, Capped: n.capped}
}

type fakeSource struct {
	g     *fakeGame
	err   error
	calls int
}

func (s *fakeSource) Env(sim int) (Env, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &fakeEnv{g: s.g, sim: sim}, nil
}

func rootOf(g *fakeGame) *Point {
	n := g.nodes[""]
	return &Point{Keys: n.keys, Prior: n.prior}
}

func treeOpts(sims int) Options {
	o := DefaultOptions()
	o.Sims = sims
	return o
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPUCTScore(t *testing.T) {
	if got := puctScore(0.4, 0.8, 1, 0, 1.5); !near(got, 1.6) {
		t.Errorf("puctScore(0.4, 0.8, 1, 0) = %v, want 1.6", got)
	}
	if got := puctScore(0.25, 0.5, 4, 1, 1.5); !near(got, 1.0) {
		t.Errorf("puctScore(0.25, 0.5, 4, 1) = %v, want 1.0", got)
	}
	if got, want := puctScore(0.7, 0.5, 3, 2, 1.5), 0.7+0.75*math.Sqrt(3)/3; !near(got, want) {
		t.Errorf("puctScore(0.7, 0.5, 3, 2) = %v, want %v", got, want)
	}
}

// Sim 0 evaluates the root (0.5), then picks by U alone: both unvisited
// children read Q = 0.5-0.1 = 0.4, U(a)=1.5*0.2=0.3, U(b)=1.5*0.8=1.2 -> b.
func TestFirstSimulationFollowsThePrior(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.2, 0.8}, value: 0.5},
		"/a": {value: 0.1, terminal: true},
		"/b": {value: 0.9, terminal: true},
	}}
	var st Stats
	tr, err := RunTree(rootOf(g), &fakeSource{g: g}, treeOpts(1), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{0, 1}) {
		t.Fatalf("visits %v, want [0 1]", tr.Visits)
	}
	if !near(tr.RootValue, 0.7) {
		t.Fatalf("root value %v, want (0.5+0.9)/2", tr.RootValue)
	}
	if st.Simulations != 1 || st.Completed != 1 || st.Terminal != 1 {
		t.Fatalf("stats %+v", st)
	}
}

// Candidate 0 is the bot's answer and wins every exact tie.
func TestTiesGoToCandidateZero(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
		"/a": {value: 0.5, terminal: true},
		"/b": {value: 0.5, terminal: true},
	}}
	var st Stats
	tr, err := RunTree(rootOf(g), &fakeSource{g: g}, treeOpts(1), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{1, 0}) {
		t.Fatalf("visits %v, want [1 0]", tr.Visits)
	}
	if c := choose(tr.Visits, false, nil); c != 0 {
		t.Fatalf("choose = %d, want 0", c)
	}
	if c := choose([]int{2, 2}, false, nil); c != 0 {
		t.Fatalf("choose on a visit tie = %d, want 0", c)
	}
	if c := choose([]int{0, 0}, true, rand.New(rand.NewPCG(1, 2))); c != 0 {
		t.Fatalf("choose with no visits = %d, want 0", c)
	}
}

// With FPU 0.1 the unvisited b reads Q = parentQ-0.1 = 0.15 and wins sim 1
// (0.15+0.849 > 0+0.636); with FPU 1.0 it reads -0.75 and a is chosen again.
func TestFirstPlayUrgency(t *testing.T) {
	nodes := map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.6, 0.4}, value: 0.5},
		"/a": {value: 0, terminal: true},
		"/b": {value: 1, terminal: true},
	}
	for _, tc := range []struct {
		fpu  float64
		want []int
	}{{0.1, []int{1, 1}}, {1.0, []int{2, 0}}} {
		g := &fakeGame{nodes: nodes}
		o := treeOpts(2)
		o.FPU = tc.fpu
		var st Stats
		tr, err := RunTree(rootOf(g), &fakeSource{g: g}, o, &st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tr.Visits, tc.want) {
			t.Errorf("FPU %v: visits %v, want %v", tc.fpu, tr.Visits, tc.want)
		}
	}
}

// Two levels. sim0 expands a (0.6); sim1 takes b (terminal 0.2); sim2 walks
// a -> x (terminal 1). Root N=4 W=0.5+0.6+0.2+1; edge a N=2 W=1.6.
func TestBackupAlongPath(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":     {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
		"/a":   {keys: []Key{"x", "y"}, prior: []float64{0.5, 0.5}, value: 0.6},
		"/a/x": {value: 1, terminal: true},
		"/a/y": {value: 0, terminal: true},
		"/b":   {value: 0.2, terminal: true},
	}}
	var st Stats
	tr, err := RunTree(rootOf(g), &fakeSource{g: g}, treeOpts(3), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{2, 1}) {
		t.Fatalf("visits %v, want [2 1]", tr.Visits)
	}
	if !near(tr.RootValue, 0.575) || !near(tr.Q[0], 0.8) || !near(tr.Q[1], 0.2) {
		t.Fatalf("root value %v Q %v, want 0.575 [0.8 0.2]", tr.RootValue, tr.Q)
	}
	if st.Completed != 3 || st.Expanded != 1 || st.Terminal != 2 {
		t.Fatalf("stats %+v, want completed 3 expanded 1 terminal 2", st)
	}
}

// Odd worlds do not offer b: b is scored only in worlds offering it (the
// ISMCTS availability rule) and each miss is counted.
func TestAvailabilityCountsOnlyOfferingWorlds(t *testing.T) {
	both := &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":   {value: 0.5},
			"/a": {value: 0.5, terminal: true},
			"/b": {value: 0.5, terminal: true},
		},
		rootFor: func(sim int) *Point {
			if sim%2 == 0 {
				return both
			}
			return &Point{Keys: []Key{"a"}, Prior: []float64{1}}
		},
	}
	var st Stats
	tr, err := RunTree(both, &fakeSource{g: g}, treeOpts(4), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{3, 1}) || !reflect.DeepEqual(tr.Avail, []int{4, 2}) {
		t.Fatalf("visits %v avail %v, want [3 1] [4 2]", tr.Visits, tr.Avail)
	}
	if st.Unavailable != 2 {
		t.Fatalf("unavailable %d, want 2", st.Unavailable)
	}
}

// A key first offered by a later world joins the node with that world's
// prior; Visits stays parallel to the root's own keys.
func TestNewKeyFromALaterWorld(t *testing.T) {
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":   {value: 0.5},
			"/a": {value: 0.5, terminal: true},
			"/c": {value: 0.9, terminal: true},
		},
		rootFor: func(sim int) *Point {
			if sim == 0 {
				return &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
			}
			return &Point{Keys: []Key{"c"}, Prior: []float64{1}}
		},
	}
	root := &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
	var st Stats
	tr, err := RunTree(root, &fakeSource{g: g}, treeOpts(2), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{1, 0}) || st.Completed != 2 || st.Unavailable != 2 {
		t.Fatalf("visits %v stats %+v", tr.Visits, st)
	}
	if !near(tr.RootValue, (0.5+0.5+0.9)/3) {
		t.Fatalf("root value %v", tr.RootValue)
	}
}

// A failed simulation changes no visit, value or availability count.
func TestFailedSimulationIsDiscarded(t *testing.T) {
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
			"/a": {value: 0.3, terminal: true},
			"/b": {value: 0.8, terminal: true},
		},
		failOnce: map[string]error{"/a": fmt.Errorf("%w: test", ErrChance)},
	}
	var st Stats
	tr, err := RunTree(rootOf(g), &fakeSource{g: g}, treeOpts(2), &st)
	if err != nil {
		t.Fatal(err)
	}
	if st.ChanceFailures != 1 || st.Completed != 1 || st.Simulations != 2 {
		t.Fatalf("stats %+v", st)
	}
	if !reflect.DeepEqual(tr.Visits, []int{1, 0}) || !reflect.DeepEqual(tr.Avail, []int{1, 1}) || !near(tr.RootValue, 0.4) {
		t.Fatalf("visits %v avail %v root %v", tr.Visits, tr.Avail, tr.RootValue)
	}
}

func TestEveryFailureKindIsCounted(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{"": {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}}}}
	for _, tc := range []struct {
		err   error
		count func(Stats) int
	}{
		{fmt.Errorf("%w: t", ErrNoWorld), func(s Stats) int { return s.NoWorld }},
		{fmt.Errorf("%w: t", ErrBadWorld), func(s Stats) int { return s.BadWorlds }},
		{fmt.Errorf("%w: t", ErrChance), func(s Stats) int { return s.ChanceFailures }},
		{fmt.Errorf("%w: t", ErrPanic), func(s Stats) int { return s.Panics }},
		{errors.New("anything else"), func(s Stats) int { return s.SubmitErrors }},
	} {
		var st Stats
		tr, err := RunTree(rootOf(g), &fakeSource{g: g, err: tc.err}, treeOpts(3), &st)
		if err != nil {
			t.Fatal(err)
		}
		if tc.count(st) != 3 || st.Completed != 0 || !reflect.DeepEqual(tr.Visits, []int{0, 0}) {
			t.Errorf("%v: stats %+v visits %v", tc.err, st, tr.Visits)
		}
	}
}

// Review Focus 5: every walk stops at the cap; visits still decide.
func TestStepCapOnEverySimulation(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
		"/a": {value: 0.7, capped: true},
		"/b": {value: 0.3, capped: true},
	}}
	var st Stats
	tr, err := RunTree(rootOf(g), &fakeSource{g: g}, treeOpts(4), &st)
	if err != nil {
		t.Fatal(err)
	}
	if st.StepCapped != 4 || st.Terminal != 0 || st.Expanded != 0 || st.Completed != 4 {
		t.Fatalf("stats %+v, want every simulation capped", st)
	}
	if !reflect.DeepEqual(tr.Visits, []int{3, 1}) || !near(tr.RootValue, 0.58) {
		t.Fatalf("visits %v root %v, want [3 1] 0.58", tr.Visits, tr.RootValue)
	}
}

func TestRunTreeRejectsAMalformedRoot(t *testing.T) {
	var st Stats
	for _, root := range []*Point{nil, {}, {Keys: []Key{"a"}, Prior: []float64{0.5, 0.5}}} {
		if _, err := RunTree(root, &fakeSource{}, treeOpts(1), &st); err == nil {
			t.Errorf("root %+v accepted", root)
		}
	}
}

func TestDirichletNoise(t *testing.T) {
	eta := dirichlet(rand.New(rand.NewPCG(1, 2)), 4, 0.3)
	sum := 0.0
	for _, x := range eta {
		if x < 0 {
			t.Fatalf("negative component %v", eta)
		}
		sum += x
	}
	if !near(sum, 1) {
		t.Fatalf("components sum to %v", sum)
	}
	if again := dirichlet(rand.New(rand.NewPCG(1, 2)), 4, 0.3); !reflect.DeepEqual(eta, again) {
		t.Fatal("the same seed drew different noise")
	}
	rng := rand.New(rand.NewPCG(3, 4))
	mean := 0.0
	for i := 0; i < 2000; i++ {
		mean += dirichlet(rng, 3, 0.3)[0]
	}
	if mean /= 2000; mean < 0.30 || mean > 0.37 {
		t.Fatalf("component mean %v, want about 1/3", mean)
	}
	prior := []float64{0.25, 0.25, 0.25, 0.25}
	p := noisyPrior(prior, rand.New(rand.NewPCG(5, 6)), 0.3, 0.25)
	sum = 0
	for _, x := range p {
		if x < 0.75*0.25-1e-12 {
			t.Fatalf("noisy prior %v fell below (1-eps)*prior", p)
		}
		sum += x
	}
	if !near(sum, 1) {
		t.Fatalf("noisy prior sums to %v", sum)
	}
	if same := noisyPrior(prior, rand.New(rand.NewPCG(5, 6)), 0.3, 0); !reflect.DeepEqual(same, prior) {
		t.Fatalf("eps 0 changed the prior: %v", same)
	}
}

func TestChooseSamplesProportionalToVisits(t *testing.T) {
	if c := choose([]int{1, 3}, false, nil); c != 1 {
		t.Fatalf("argmax = %d, want 1", c)
	}
	rng := rand.New(rand.NewPCG(7, 8))
	ones := 0
	for i := 0; i < 4000; i++ {
		if choose([]int{1, 3}, true, rng) == 1 {
			ones++
		}
	}
	if f := float64(ones) / 4000; f < 0.72 || f > 0.78 {
		t.Fatalf("sampled share of the 3-visit child %v, want about 0.75", f)
	}
}

func TestParseKinds(t *testing.T) {
	k, err := ParseKinds("priority, attackers")
	if err != nil || k != (Kinds{Priority: true, Attackers: true}) {
		t.Fatalf("ParseKinds = %+v, %v", k, err)
	}
	if k, err := ParseKinds("priority,attackers,blockers,target"); err != nil || k != AllKinds() {
		t.Fatalf("all four = %+v, %v", k, err)
	}
	for _, bad := range []string{"", "cast", "priority,"} {
		if _, err := ParseKinds(bad); err == nil {
			t.Errorf("ParseKinds(%q) accepted", bad)
		}
	}
}

func TestStatsAddSumsEveryCounter(t *testing.T) {
	var one Stats
	v := reflect.ValueOf(&one).Elem()
	for i := 0; i < v.NumField(); i++ {
		v.Field(i).SetInt(1)
	}
	var sum Stats
	sum.Add(one)
	sum.Add(one)
	s := reflect.ValueOf(sum)
	for i := 0; i < s.NumField(); i++ {
		if got := s.Field(i).Int(); got != 2 {
			t.Errorf("Stats.%s = %d after adding 1 twice (Add misses the field)", s.Type().Field(i).Name, got)
		}
	}
}
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/azmcts/`
Expected: FAIL to compile. The package has no non-test files yet (`no non-test Go files` / `undefined: Key`).

- [ ] **Step 3: Implement**

`internal/azmcts/options.go`:

```go
package azmcts

import (
	"errors"
	"fmt"
	"strings"
)

// Kinds are the searched decision kinds (spec §1). Every other decision --
// and a decision of a searched kind whose candidates cannot be built -- is
// answered by the bot.
type Kinds struct {
	Priority, Attackers, Blockers, Target bool
}

// AllKinds is the spec's searched set.
func AllKinds() Kinds { return Kinds{Priority: true, Attackers: true, Blockers: true, Target: true} }

// ParseKinds parses a comma list of priority, attackers, blockers, target
// (botbench's -az-kinds). An unknown or empty entry is an error.
func ParseKinds(s string) (Kinds, error) {
	var k Kinds
	for _, part := range strings.Split(s, ",") {
		switch p := strings.TrimSpace(part); p {
		case "priority":
			k.Priority = true
		case "attackers":
			k.Attackers = true
		case "blockers":
			k.Blockers = true
		case "target":
			k.Target = true
		default:
			return Kinds{}, fmt.Errorf("unknown searched kind %q (want priority, attackers, blockers, target)", p)
		}
	}
	return k, nil
}

// Options are one Search's knobs; DefaultOptions holds the spec's values.
type Options struct {
	// Sims is the simulation budget per searched decision (-az-sims). 0 or
	// less never builds a tree: the bot's answer is played.
	Sims int
	// CPUCT is PUCT's exploration constant c (spec §2: 1.5).
	CPUCT float64
	// FPU is the first-play-urgency reduction: an unvisited child's Q is its
	// parent's Q minus FPU (spec §2: 0.1).
	FPU float64
	// Limit caps the candidates per searched decision, the bot's answer
	// first (the enumerators' limit argument).
	Limit int
	// MaxSteps caps the environment's submits per simulation, the livelock
	// guard of spec §2; a walk that reaches it is evaluated where it stopped.
	MaxSteps int
	// Kinds are the searched decision kinds.
	Kinds Kinds
	// Seed is the per-decision seed (DecisionSeed): it seeds the root noise,
	// the move sampling, and -- identically for every simulation -- the
	// environment bots' streams.
	Seed uint64
	// Noise mixes Dirichlet(DirichletAlpha) noise into the root prior with
	// weight DirichletEps. Generation only (spec §2); eval leaves it false.
	Noise                        bool
	DirichletAlpha, DirichletEps float64
	// Sample picks the move with probability proportional to its root
	// visits (tau = 1) instead of the argmax. Generation only, turns 1-4
	// (the seat decides); eval leaves it false.
	Sample bool
}

// DefaultOptions are the spec's values (§2) and this plan's candidate and
// step caps.
func DefaultOptions() Options {
	return Options{
		Sims: 100, CPUCT: 1.5, FPU: 0.1, Limit: 6, MaxSteps: 1000, Kinds: AllKinds(),
		DirichletAlpha: 0.3, DirichletEps: 0.25,
	}
}

// Stats are spec §4's failure-handling counters plus the work counts the
// cost report needs. Every fallback lands here; none is silent.
type Stats struct {
	Searched       int // decisions a tree was built for
	Skipped        int // searched-kind decisions with fewer than two candidates, or a bot answer outside the candidate vocabulary: the bot's answer was played
	Simulations    int // simulations attempted
	Completed      int // simulations that backed up a value
	ChanceFailures int // chance failure in a hypothetical world: simulation discarded
	Panics         int // engine panic inside a world (the livelock watcher included): discarded
	SubmitErrors   int // any other world error (a rejected intent, no pending decision, an unknown key): discarded
	BadWorlds      int // a world not positioned at the root decision: discarded
	NoWorld        int // the source could not produce a world: discarded
	AllFailed      int // every simulation failed: the bot's answer was played
	StepCapped     int // walks stopped by MaxSteps, evaluated where they stopped
	Terminal       int // walks that reached game over
	Expanded       int // new tree nodes
	Unavailable    int // known children a world did not offer, summed over node visits
	EnvSteps       int // environment submits (the searched intents excluded)
	PriorFallbacks int // decisions whose network prior fell back to uniform
	FeedStopped    int // decisions the driver routed around the search (its observation feed stopped): the bot's answer was played
}

// Add sums o into s.
func (s *Stats) Add(o Stats) {
	s.Searched += o.Searched
	s.Skipped += o.Skipped
	s.Simulations += o.Simulations
	s.Completed += o.Completed
	s.ChanceFailures += o.ChanceFailures
	s.Panics += o.Panics
	s.SubmitErrors += o.SubmitErrors
	s.BadWorlds += o.BadWorlds
	s.NoWorld += o.NoWorld
	s.AllFailed += o.AllFailed
	s.StepCapped += o.StepCapped
	s.Terminal += o.Terminal
	s.Expanded += o.Expanded
	s.Unavailable += o.Unavailable
	s.EnvSteps += o.EnvSteps
	s.PriorFallbacks += o.PriorFallbacks
	s.FeedStopped += o.FeedStopped
}

// The error classes a world reports; RunTree counts a discarded simulation
// by the first class its error wraps (anything else is a SubmitError).
var (
	ErrChance   = errors.New("azmcts: chance failure in a hypothetical world")
	ErrPanic    = errors.New("azmcts: engine panic inside a world")
	ErrSubmit   = errors.New("azmcts: a world rejected a submit")
	ErrBadWorld = errors.New("azmcts: world is not at the root decision")
	ErrNoWorld  = errors.New("azmcts: no world could be produced")
)
```

`internal/azmcts/tree.go`:

```go
// Package azmcts is the AlphaZero-style tree search of
// docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md: a PUCT tree
// over ONE seat's searched decisions (single perspective: every value is
// that seat's win probability, no sign flips), whose leaf is a value -- the
// policynet value head, or the frozen heuristic searchprobe.LeafValue for
// generation 0 -- never a rollout.
//
// The package is pure search. It reads no clock (internal/archtest), ranges
// no map whose order could reach a choice, and draws randomness only from
// math/rand/v2 PCG streams its caller seeds, so the same seed, position and
// checkpoint give a byte-identical Result.
//
// Layers, bottom up:
//   - options.go, tree.go: knobs, counters, and the PUCT arithmetic over the
//     Env seam (RunTree), testable against a fake environment;
//   - candidates.go: the searched kinds, candidate enumeration over the
//     searchprobe enumerators, semantic keys, priors;
//   - world.go, env.go: where a simulation's world comes from (the
//     Clairvoyant source) and how it is walked (engineEnv);
//   - search.go: Search, the entry point;
//   - seat.go: the az seat, a searchseat.SearchSeat fed by internal/bench.
package azmcts

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
)

// Key is one candidate's semantic identity at a node: the canonical encoding
// of its []searchprobe.Action (actionsKey). Equal keys name the same action
// in every world, which is what lets one tree be shared across worlds.
type Key string

// Point is one searched decision of the searching seat as the tree sees it.
type Point struct {
	// Keys are the candidates offered here; Keys[0] is the bot's answer.
	Keys []Key
	// Prior parallels Keys: non-negative, summing to 1.
	Prior []float64
}

// Leaf is the value of the position a walk stopped at, for the searching
// seat.
type Leaf struct {
	V        float64
	Terminal bool // the game is over (V is 1, 0 or 0.5)
	Capped   bool // the env step cap stopped the walk (V is the leaf evaluator's)
}

// Env is one simulation's private world -- the fake-env seam spec §4 asks
// for ("clone, pending, submit, over, winner"): EnvSource.Env is the clone,
// Root and Play's returned Point are the pending searched decision, Play is
// the submit plus the environment's answers up to the next searched
// decision, and Leaf carries over/winner.
type Env interface {
	// Root is the root decision as this world offers it. A candidate the
	// tree knows but this world does not offer is unavailable here (spec §2,
	// the ISMCTS availability rule).
	Root() *Point
	// Play submits candidate k at the current point and advances to the
	// searching seat's next searched decision. A nil Point means the walk
	// ended (game over, or the step cap); an error discards the simulation.
	Play(k Key) (*Point, error)
	// Leaf evaluates the current position.
	Leaf() Leaf
}

// EnvSource hands each simulation its own world.
type EnvSource interface {
	Env(sim int) (Env, error)
}

// TreeResult is the root's statistics after RunTree.
type TreeResult struct {
	Visits    []int     // per root key (parallel to root.Keys)
	Q         []float64 // mean backed-up value per root key; 0 when unvisited
	Avail     []int     // simulations in which each root key was available
	RootValue float64   // the root's mean value, its own evaluation included; 0.5 before any
}

type node struct {
	n    int
	w    float64
	kids []*edge
}

type edge struct {
	key   Key
	prior float64
	n     int
	w     float64
	avail int
	next  *node
}

func newNode(pt *Point) *node {
	nd := &node{kids: make([]*edge, len(pt.Keys))}
	for i, k := range pt.Keys {
		nd.kids[i] = &edge{key: k, prior: pt.Prior[i]}
	}
	return nd
}

func (nd *node) q() float64 {
	if nd.n == 0 {
		return 0.5
	}
	return nd.w / float64(nd.n)
}

func (nd *node) child(k Key) *edge {
	for _, e := range nd.kids {
		if e.key == k {
			return e
		}
	}
	return nil
}

func hasKey(keys []Key, k Key) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}

// puctScore is Q + c * P * sqrt(N_avail) / (1 + N) (spec §2).
func puctScore(q, prior float64, avail, n int, c float64) float64 {
	return q + c*prior*math.Sqrt(float64(avail))/float64(1+n)
}

// selectEdge is PUCT over the children this world offers. N_avail counts the
// simulations the child was available in, THIS one included (so the first
// selection at a node is guided by the prior rather than a 0 * U tie). An
// unvisited child's Q is the parent's Q minus FPU. Strict > keeps the lower
// index on a tie: candidate 0, the bot's answer, wins ties.
func selectEdge(nd *node, pt *Point, opts Options) *edge {
	parentQ := nd.q()
	var best *edge
	bestScore := math.Inf(-1)
	for _, kid := range nd.kids {
		if !hasKey(pt.Keys, kid.key) {
			continue
		}
		q := parentQ - opts.FPU
		if kid.n > 0 {
			q = kid.w / float64(kid.n)
		}
		if s := puctScore(q, kid.prior, kid.avail+1, kid.n, opts.CPUCT); best == nil || s > bestScore {
			best, bestScore = kid, s
		}
	}
	return best
}

// RunTree runs opts.Sims simulations from root over src and returns the
// root's statistics. Each simulation takes a fresh Env, walks the tree by
// PUCT, expands exactly one new node (or stops where the walk ended),
// evaluates that position once and backs the value up the path: no rollouts
// (spec §2). A simulation whose Env or Play fails is discarded and counted in
// st by its error class; it changes no visit, value or availability count.
// The first Env the source produces also evaluates the root itself
// (AlphaZero's expansion value), the parent Q first-play urgency reads there.
//
// Children are kept in root order and ties go to the lower index, so
// root.Keys[0] -- the bot's answer -- wins every tie. No map is ranged.
func RunTree(root *Point, src EnvSource, opts Options, st *Stats) (TreeResult, error) {
	if root == nil || len(root.Keys) == 0 || len(root.Prior) != len(root.Keys) {
		return TreeResult{}, fmt.Errorf("azmcts: a root point needs keys and a parallel prior")
	}
	top := newNode(root)
	for i := 0; i < opts.Sims; i++ {
		st.Simulations++
		env, err := src.Env(i)
		if err != nil {
			classify(st, err)
			continue
		}
		if err := simulate(top, env, opts, st); err != nil {
			classify(st, err)
			continue
		}
		st.Completed++
	}
	res := TreeResult{
		Visits: make([]int, len(root.Keys)), Q: make([]float64, len(root.Keys)),
		Avail: make([]int, len(root.Keys)), RootValue: top.q(),
	}
	for i := range root.Keys {
		k := top.kids[i]
		res.Visits[i], res.Avail[i] = k.n, k.avail
		if k.n > 0 {
			res.Q[i] = k.w / float64(k.n)
		}
	}
	return res, nil
}

// simulate is one simulation. Availability marks, visits and values are
// committed only when it succeeds.
func simulate(top *node, env Env, opts Options, st *Stats) error {
	if top.n == 0 {
		l := env.Leaf()
		top.n, top.w = 1, l.V
	}
	var (
		nodes   []*node
		path    []*edge
		marks   []*edge
		unavail int
	)
	nd, pt := top, env.Root()
	for {
		if pt == nil || len(pt.Keys) == 0 || len(pt.Prior) != len(pt.Keys) {
			return fmt.Errorf("%w: a world offered a malformed point", ErrSubmit)
		}
		for _, kid := range nd.kids {
			if hasKey(pt.Keys, kid.key) {
				marks = append(marks, kid)
			} else {
				unavail++
			}
		}
		for j, k := range pt.Keys {
			if nd.child(k) == nil {
				kid := &edge{key: k, prior: pt.Prior[j]}
				nd.kids = append(nd.kids, kid)
				marks = append(marks, kid)
			}
		}
		sel := selectEdge(nd, pt, opts)
		nodes, path = append(nodes, nd), append(path, sel)
		next, err := env.Play(sel.key)
		if err != nil {
			return err
		}
		if next == nil {
			l := env.Leaf()
			if l.Terminal {
				st.Terminal++
			}
			if l.Capped {
				st.StepCapped++
			}
			commit(nodes, path, marks, l.V)
			st.Unavailable += unavail
			return nil
		}
		if sel.next == nil {
			l := env.Leaf()
			sel.next = newNode(next)
			sel.next.n, sel.next.w = 1, l.V
			st.Expanded++
			commit(nodes, path, marks, l.V)
			st.Unavailable += unavail
			return nil
		}
		nd, pt = sel.next, next
	}
}

func commit(nodes []*node, path, marks []*edge, v float64) {
	for _, n := range nodes {
		n.n++
		n.w += v
	}
	for _, e := range path {
		e.n++
		e.w += v
	}
	for _, e := range marks {
		e.avail++
	}
}

func classify(st *Stats, err error) {
	switch {
	case errors.Is(err, ErrChance):
		st.ChanceFailures++
	case errors.Is(err, ErrPanic):
		st.Panics++
	case errors.Is(err, ErrBadWorld):
		st.BadWorlds++
	case errors.Is(err, ErrNoWorld):
		st.NoWorld++
	default:
		st.SubmitErrors++
	}
}

// choose is the move: the most-visited root child (ties to the lower index),
// or -- generation only -- a draw proportional to visits (tau = 1).
func choose(visits []int, sample bool, rng *rand.Rand) int {
	best := 0
	for i := 1; i < len(visits); i++ {
		if visits[i] > visits[best] {
			best = i
		}
	}
	if !sample || rng == nil {
		return best
	}
	total := 0
	for _, v := range visits {
		total += v
	}
	if total == 0 {
		return best
	}
	r := rng.IntN(total)
	for i, v := range visits {
		if r < v {
			return i
		}
		r -= v
	}
	return best
}

func uniform(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1 / float64(n)
	}
	return out
}

// noisyPrior is (1-eps)*prior + eps*Dirichlet(alpha) (spec §2).
func noisyPrior(prior []float64, rng *rand.Rand, alpha, eps float64) []float64 {
	eta := dirichlet(rng, len(prior), alpha)
	out := make([]float64, len(prior))
	for i, p := range prior {
		out[i] = (1-eps)*p + eps*eta[i]
	}
	return out
}

// dirichlet draws a symmetric Dirichlet(alpha) vector of length n:
// independent Gamma(alpha, 1) draws, normalised.
func dirichlet(rng *rand.Rand, n int, alpha float64) []float64 {
	out := make([]float64, n)
	sum := 0.0
	for i := range out {
		out[i] = gammaDraw(rng, alpha)
		sum += out[i]
	}
	if sum <= 0 {
		return uniform(n)
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// gammaDraw is Marsaglia and Tsang's Gamma(alpha, 1) sampler; alpha < 1 uses
// the boost Gamma(alpha) = Gamma(alpha+1) * U^(1/alpha).
func gammaDraw(rng *rand.Rand, alpha float64) float64 {
	if alpha < 1 {
		u := rng.Float64()
		for u == 0 {
			u = rng.Float64()
		}
		return gammaDraw(rng, alpha+1) * math.Pow(u, 1/alpha)
	}
	d := alpha - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		x := rng.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
```

- [ ] **Step 4: Run and pass**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/azmcts/`
Expected: PASS (no corpus needed).

- [ ] **Step 5: Commit**

```bash
git add internal/azmcts/options.go internal/azmcts/tree.go internal/azmcts/tree_test.go
git commit -m "feat(azmcts): PUCT tree core over a fake-env seam

Single-perspective PUCT (c 1.5, FPU 0.1) with ISMCTS availability counts,
one-node expansion, value backup and no rollouts, run over an Env interface
so the arithmetic is tested against a hand-built game tree. Discarded
simulations are counted by error class and change no statistic. Dirichlet
noise and visit sampling are generation-only helpers."
```

---

## Task 4: Candidates, semantic keys and priors

**Files:**
- Create: `internal/azmcts/candidates.go`, `internal/azmcts/candidates_test.go`

**Interfaces:**
- Consumes: `(*searchprobe.Collector).ObserveDecision` (Task 2); `(*Collector).Actions`, `Match` (`internal/searchprobe/action.go:39,76`); `searchprobe.Candidates` (`rollout.go:17`), `AttackCandidates` (`teacher.go:317`), `BlockCandidates` (`teacher.go:414`), `SingleTarget` (`teacher.go:501`), `TargetCandidates` (`teacher.go:512`); `botpolicy.BoardFromGame` (`botpolicy/combat.go:124`), `botpolicy.LegalBlockChoices` (`botpolicy/blocks.go:400`); `view.Project` (`view/view.go:447`); `policynet.EncodeStateWith`/`EncodeOptionWith` (`internal/policynet/features.go:152,178`), `(*Model).Score` (`net.go:522`), `policynet.CandidateScore` (Task 1); `seat.MarkBotPicks` (`seat/policynet.go:420`); `uniform` (Task 3).
- Produces (unexported, used by Tasks 5–6): `type cand struct { acts []searchprobe.Action; key Key; in decision.Intent }`; `func enumerate(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int) ([]cand, string, bool)`; `func actionsKey(acts []searchprobe.Action) Key`; `func priors(net *policynet.Model, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands []cand) ([]float64, bool)`; `func softmax(logits []float64) ([]float64, bool)`.

- [ ] **Step 1: Write the failing tests**

`internal/azmcts/candidates_test.go`:

```go
package azmcts

import (
	"math"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/state"
)

// playerTarget is a single-target decision over the first n players: it
// names no object, so a collector needs no engine to observe it.
func playerTarget(seq uint64, n int) *decision.Decision {
	d := &decision.Decision{Seq: seq, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1}
	labels := []string{"you", "opponent"}
	for i := 0; i < n; i++ {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "target", Player: state.PlayerID(i), Label: labels[i]})
	}
	return d
}

// passOrAbility is a priority decision whose options name no object.
func passOrAbility(seq uint64) *decision.Decision {
	return &decision.Decision{Seq: seq, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "pass", Label: "Pass"},
		{Index: 1, Kind: "ability", Label: "Draw a card"},
	}}
}

func choicesOf(cands []cand) [][]int {
	out := make([][]int, len(cands))
	for i, c := range cands {
		out[i] = c.in.Choices
	}
	return out
}

func TestEnumerateTargetBotFirstWithStableKeys(t *testing.T) {
	d := playerTarget(4, 2)
	bot := decision.Intent{Seq: 4, Player: 0, Choices: []int{1}}
	cands, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if !ok || kind != "target" {
		t.Fatalf("enumerate = %v, %q", ok, kind)
	}
	if got := choicesOf(cands); !reflect.DeepEqual(got, [][]int{{1}, {0}}) {
		t.Fatalf("candidates %v, want the bot's pick first", got)
	}
	if cands[0].key == cands[1].key {
		t.Fatal("two different targets share a key")
	}
	again, _, _ := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if again[0].key != cands[0].key || again[1].key != cands[1].key {
		t.Fatal("keys differ between two fresh collectors")
	}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, Kinds{Priority: true}, 6); ok || kind != "" {
		t.Fatalf("an unsearched kind enumerated: %v, %q", ok, kind)
	}
}

func TestEnumeratePriorityBotFirstThenPass(t *testing.T) {
	d := passOrAbility(5)
	for _, tc := range []struct {
		bot  int
		want [][]int
	}{{0, [][]int{{0}, {1}}}, {1, [][]int{{1}, {0}}}} {
		bot := decision.Intent{Seq: 5, Player: 0, Choices: []int{tc.bot}}
		cands, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
		if !ok || kind != "priority" {
			t.Fatalf("bot %d: enumerate = %v, %q", tc.bot, ok, kind)
		}
		if got := choicesOf(cands); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("bot %d: candidates %v, want %v", tc.bot, got, tc.want)
		}
	}
}

// Review Focus 4: a searched kind with fewer than two candidates is never a
// node.
func TestEnumerateOneOrZeroCandidates(t *testing.T) {
	one := playerTarget(6, 1)
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, one, decision.Intent{Seq: 6, Player: 0, Choices: []int{0}}, AllKinds(), 6); ok || kind != "" {
		t.Fatalf("a one-option target enumerated: %v, %q", ok, kind)
	}
	passOnly := &decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, passOnly, decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}, AllKinds(), 6); ok || kind != "priority" {
		t.Fatalf("a pass-only priority enumerated: %v, %q", ok, kind)
	}
	noAttack := &decision.Decision{Seq: 8, Player: 0, Kind: decision.KAttackers}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, noAttack, decision.Intent{Seq: 8, Player: 0}, AllKinds(), 6); ok || kind != "attackers" {
		t.Fatalf("an attackers decision with no attacker enumerated: %v, %q", ok, kind)
	}
}

// Review Focus 1: an auto-pay answer cannot be expressed as semantic actions
// (Intent.Payment is exclusive with Choices), so the decision is skipped.
func TestEnumerateRefusesAPaymentIntent(t *testing.T) {
	d := passOrAbility(9)
	bot := decision.Intent{Seq: 9, Player: 0, Payment: &decision.PaymentSelection{}}
	if _, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6); ok || kind != "priority" {
		t.Fatalf("a payment intent enumerated: %v, %q", ok, kind)
	}
}

func TestEnumerateRefusesAnotherSeatsCollector(t *testing.T) {
	d := passOrAbility(10)
	bot := decision.Intent{Seq: 10, Player: 0, Choices: []int{1}}
	if _, _, ok := enumerate(searchprobe.NewCollector(1), nil, d, bot, AllKinds(), 6); ok {
		t.Fatal("seat 1's collector enumerated seat 0's decision")
	}
}

func TestActionsKeyIsCanonical(t *testing.T) {
	a := []searchprobe.Action{{Decision: decision.KPriority, Kind: "pass", Value: "Pass"}}
	b := []searchprobe.Action{{Decision: decision.KPriority, Kind: "pass", Value: "Pass"}}
	c := []searchprobe.Action{{Decision: decision.KPriority, Kind: "pass", Value: "Pass!"}}
	if actionsKey(a) != actionsKey(b) || actionsKey(a) == actionsKey(c) {
		t.Fatal("keys do not follow action equality")
	}
	if got := actionsKey([]searchprobe.Action{}); got != "[]" {
		t.Fatalf("empty declaration key %q, want []", got)
	}
}

func TestSoftmax(t *testing.T) {
	p, ok := softmax([]float64{0, math.Log(3)})
	if !ok || !near(p[0], 0.25) || !near(p[1], 0.75) {
		t.Fatalf("softmax = %v, %v", p, ok)
	}
	if p, ok := softmax([]float64{math.Inf(-1), 0}); !ok || p[0] != 0 || !near(p[1], 1) {
		t.Fatalf("softmax with -Inf = %v, %v", p, ok)
	}
	for _, bad := range [][]float64{{math.Inf(-1), math.Inf(-1)}, {math.NaN(), 0}, {math.Inf(1), 0}} {
		if _, ok := softmax(bad); ok {
			t.Errorf("softmax(%v) accepted", bad)
		}
	}
}

func TestPriorsUniformWithoutANetwork(t *testing.T) {
	d := playerTarget(11, 2)
	bot := decision.Intent{Seq: 11, Player: 0, Choices: []int{0}}
	cands, kind, ok := enumerate(searchprobe.NewCollector(0), nil, d, bot, AllKinds(), 6)
	if !ok {
		t.Fatal("enumerate failed")
	}
	p, fell := priors(nil, nil, d, bot, kind, cands)
	if fell || !reflect.DeepEqual(p, []float64{0.5, 0.5}) {
		t.Fatalf("priors = %v (fell back %v), want uniform", p, fell)
	}
}
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestEnumerate|TestActionsKey|TestSoftmax|TestPriors' ./internal/azmcts/`
Expected: FAIL to compile: `undefined: enumerate`, `undefined: cand`, `undefined: actionsKey`, `undefined: softmax`, `undefined: priors`.

- [ ] **Step 3: Implement**

`internal/azmcts/candidates.go`:

```go
package azmcts

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// cand is one candidate answer at a searched decision: its semantic actions
// (the tree identity), their key, and the intent that plays it on THIS
// decision of THIS engine.
type cand struct {
	acts []searchprobe.Action
	key  Key
	in   decision.Intent
}

// enumerate builds the candidates of the searching seat's decision d, the
// bot's answer bot first (spec §1), over the exported searchprobe
// enumerators the L10 search seat also uses:
//
//   - attackers: searchprobe.AttackCandidates;
//   - blockers: searchprobe.BlockCandidates, legality from the bot's own
//     block guard on the deciding seat's board (botpolicy.LegalBlockChoices);
//   - target (single-choice only, searchprobe.SingleTarget):
//     searchprobe.TargetCandidates;
//   - priority: searchprobe.Candidates over d's observed options -- the bot's
//     action, then pass, then the other cast/ability options. Land plays and
//     mana activations are never candidates, so a priority the bot answers
//     with one is not searched.
//
// It does not reuse searchseat.Eligible/candidates: that gate's priority arm
// is ">= 2 distinct castable objects" (searchseat.go:250), not the spec's
// rule, and the dispatch is unexported and bound to searchseat.Options.
//
// kind is "" when d is not a searched kind at all; ok is false for a searched
// kind that cannot be searched (fewer than two candidates, an auto-pay
// Payment answer, a collector for another seat, an action that cannot be
// translated). obs must be a collector for d.Player; ObserveDecision runs on
// it first. e is read only by the blockers arm.
func enumerate(obs *searchprobe.Collector, e *rules.Engine, d *decision.Decision, bot decision.Intent, kinds Kinds, limit int) ([]cand, string, bool) {
	var kind string
	switch {
	case d == nil:
		return nil, "", false
	case d.Kind == decision.KAttackers && kinds.Attackers:
		kind = "attackers"
	case d.Kind == decision.KBlockers && kinds.Blockers:
		kind = "blockers"
	case kinds.Target && searchprobe.SingleTarget(d):
		kind = "target"
	case d.Kind == decision.KPriority && kinds.Priority:
		kind = "priority"
	default:
		return nil, "", false
	}
	if bot.Payment != nil {
		return nil, kind, false
	}
	od, err := obs.ObserveDecision(e, d)
	if err != nil {
		return nil, kind, false
	}
	var ins []decision.Intent
	switch kind {
	case "attackers":
		ins = searchprobe.AttackCandidates(d, bot, limit)
	case "blockers":
		b := botpolicy.BoardFromGame(e.G, e, d.Player)
		legal := func(choices []int) []int { return botpolicy.LegalBlockChoices(b, d, choices) }
		ins = searchprobe.BlockCandidates(d, bot, limit, legal)
	case "target":
		ins = searchprobe.TargetCandidates(d, bot, limit)
	case "priority":
		base, err := obs.Actions(d, bot)
		if err != nil || len(base) != 1 {
			return nil, kind, false
		}
		for _, a := range searchprobe.Candidates(od, base[0], limit) {
			in, err := obs.Match(d, []searchprobe.Action{a})
			if err != nil {
				return nil, kind, false
			}
			ins = append(ins, in)
		}
	}
	if len(ins) < 2 {
		return nil, kind, false
	}
	out := make([]cand, 0, len(ins))
	for _, in := range ins {
		acts, err := obs.Actions(d, in)
		if err != nil {
			return nil, kind, false
		}
		out = append(out, cand{acts: acts, key: actionsKey(acts), in: in})
	}
	return out, kind, true
}

// actionsKey is the canonical key of a semantic action list: its JSON
// encoding (fixed field order, so equal lists give equal keys).
func actionsKey(acts []searchprobe.Action) Key {
	b, err := json.Marshal(acts)
	if err != nil {
		// Action holds only integers and strings; Marshal cannot fail.
		panic(fmt.Sprintf("azmcts: encoding a semantic action: %v", err))
	}
	return Key(b)
}

// priors is the candidates' prior (spec §2): uniform without a network;
// otherwise a softmax, across candidates, of policynet.CandidateScore over
// the head's option scores on the deciding seat's redacted view -- the
// subset log-likelihood for attackers and blockers, the single option's
// score for priority and target. The bot's options are marked (BotPick) as a
// residual checkpoint was trained. fellBack reports a network prior that
// could not be formed (every candidate -Inf or NaN) and fell back to uniform.
func priors(net *policynet.Model, e *rules.Engine, d *decision.Decision, bot decision.Intent, kind string, cands []cand) ([]float64, bool) {
	if net == nil {
		return uniform(len(cands)), false
	}
	v := view.Project(e.G, e, d.Player, d)
	st := policynet.EncodeStateWith(net.Features, v, d.Player, nil)
	enc := make([]policynet.Option, len(d.Options))
	for i := range d.Options {
		enc[i] = policynet.EncodeOptionWith(net.Features, v, d.Player, d.Kind, d.Options[i], i, len(d.Options))
	}
	seat.MarkBotPicks(d, enc, bot)
	scores := net.Score(st, enc)
	subset := kind == "attackers" || kind == "blockers"
	logits := make([]float64, len(cands))
	for i, c := range cands {
		logits[i] = policynet.CandidateScore(subset, scores, c.in.Choices)
	}
	p, ok := softmax(logits)
	if !ok {
		return uniform(len(cands)), true
	}
	return p, false
}

// softmax normalises logits; ok is false when no logit is finite or any is
// NaN or +Inf.
func softmax(logits []float64) ([]float64, bool) {
	hi := math.Inf(-1)
	for _, x := range logits {
		if math.IsNaN(x) || math.IsInf(x, 1) {
			return nil, false
		}
		if x > hi {
			hi = x
		}
	}
	if math.IsInf(hi, -1) {
		return nil, false
	}
	out := make([]float64, len(logits))
	sum := 0.0
	for i, x := range logits {
		out[i] = math.Exp(x - hi)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out, true
}
```

- [ ] **Step 4: Run and pass**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/azmcts/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/azmcts/candidates.go internal/azmcts/candidates_test.go
git commit -m "feat(azmcts): searched-kind candidates, semantic keys and priors

Priority, attackers, blockers and single-target decisions enumerate their
candidates through the exported searchprobe enumerators, the bot's answer
first, keyed by the JSON of their semantic actions. A decision with fewer
than two candidates, or an auto-pay Payment answer, is not searched. Priors
are uniform for generation 0 and a softmax of policynet.CandidateScore
otherwise."
```

---

## Task 5: World sources, the engine environment, and `Search`

**Files:**
- Create: `internal/azmcts/world.go`, `internal/azmcts/env.go`, `internal/azmcts/search.go`, `internal/azmcts/world_test.go`, `internal/azmcts/search_test.go`

**Interfaces:**
- Consumes: `(*rules.Engine).Clone` (`rules/clone.go:51`), `CloneHypothetical` (`rules/chance.go:189`), `Submit` (`rules/engine.go:3643`), `SubmitHypothetical` (`rules/chance.go:69`), `Pending` (`rules/engine.go:3355`), `RNGDraws` (`rules/rng.go:109`); `(*events.Log).Head` (`events/log.go:105`); `searchprobe.BotRandoms` (`rollout.go:213`), `searchprobe.LeafValue` (`teacher.go:89`), `(*Collector).Clone` (`observation.go:70`); `botpolicy.Decide`/`NewBoard`/`BoardFromGameInto`; `view.Project`; `(*policynet.Model).Value` (`net.go:212`), `HasValue` (`net.go:160`), `FeatureSet.Diagnostic` (`features.go:76`), `policynet.NewModel` (`net.go:376`), `InitValue` (`net.go:166`), `policynet.TableRows`; Tasks 3–4.
- Produces:
  - `type World struct { Engine *rules.Engine; Observer *searchprobe.Collector; Hypothetical bool }`; `type WorldSource interface { World(sim int) (World, error) }`
  - `var ErrClairvoyantRefused error`; `func AllowClairvoyant()`; `func NewClairvoyant(e *rules.Engine, obs *searchprobe.Collector) (WorldSource, error)`
  - `type Root struct { Engine *rules.Engine; Decision *decision.Decision; Bot decision.Intent; Observer *searchprobe.Collector }`
  - `type Result struct { Kind string; Candidates []decision.Intent; Keys []Key; Visits []int; Prior []float64; Q []float64; RootValue float64; Choice int; Intent decision.Intent; Stats Stats }`
  - `func (o Options) Validate(net *policynet.Model) error`
  - `func Search(root Root, src WorldSource, net *policynet.Model, opts Options) (Result, error)`
  - `func DecisionSeed(seatSeed, seq uint64) uint64`
  - test helpers (package-internal, reused by Task 6): `allowClairvoyantForTest(t testing.TB)`, `testConfig(t testing.TB, a, b string, seed uint64) rules.Config`, `findPosition(cfg rules.Config, want decision.Kind, minTurn int32, maxSteps int) (*rules.Engine, *decision.Decision, decision.Intent, error)`, `botPosition(t testing.TB, cfg rules.Config, want decision.Kind, minTurn int32, maxSteps int) (*rules.Engine, *decision.Decision, decision.Intent)`, `searchAt(t testing.TB, e *rules.Engine, d *decision.Decision, bot decision.Intent, net *policynet.Model, opts Options) Result`, `const testSeed`

- [ ] **Step 1: Write the failing tests**

`internal/azmcts/world_test.go`:

```go
package azmcts

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// allowClairvoyantForTest opens the clairvoyant gate for one test and
// restores it after.
func allowClairvoyantForTest(t testing.TB) {
	t.Helper()
	prev := clairvoyantAllowed.Load()
	clairvoyantAllowed.Store(true)
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
}

func TestClairvoyantRefusedUnlessAllowed(t *testing.T) {
	prev := clairvoyantAllowed.Load()
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
	clairvoyantAllowed.Store(false)
	if _, err := NewClairvoyant(nil, nil); !errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("NewClairvoyant without AllowClairvoyant: %v, want ErrClairvoyantRefused", err)
	}
	AllowClairvoyant()
	if _, err := NewClairvoyant(nil, nil); err == nil || errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("allowed, nil engine: %v, want a plain refusal", err)
	}
}

// A panic inside a world's engine is recovered and classified.
func TestSubmitRecoversAnEnginePanic(t *testing.T) {
	env := &engineEnv{} // nil engine: Submit dereferences it and panics
	if err := env.submit(&decision.Decision{}, decision.Intent{}); !errors.Is(err, ErrPanic) {
		t.Fatalf("submit on a nil engine: %v, want ErrPanic", err)
	}
}

// In a hypothetical world an intent the decision itself rejects is a submit
// error, never a chance failure.
func TestHypotheticalSubmitClassifiesRejections(t *testing.T) {
	d := passOrAbility(3)
	bad := decision.Intent{Seq: 99, Player: 0, Choices: []int{0}}
	if err := (&engineEnv{hyp: true}).submit(d, bad); !errors.Is(err, ErrSubmit) || errors.Is(err, ErrChance) {
		t.Fatalf("rejected intent in a hypothetical world: %v, want ErrSubmit", err)
	}
}
```

`internal/azmcts/search_test.go`:

```go
package azmcts

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

const testSeed = uint64(30000000)

func testConfig(t testing.TB, a, b string, seed uint64) rules.Config {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	return rules.Config{Seed: seed, Names: []string{a, b}, Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens}
}

var errNoPosition = errors.New("no searchable seat-0 decision")

// findPosition plays the default bot for both seats from genesis until seat
// 0 faces a decision Search would search (a searched kind with >= 2
// candidates; of kind want unless want is "") at turn >= minTurn, and returns
// the engine there with the bot's answer, unsubmitted.
func findPosition(cfg rules.Config, want decision.Kind, minTurn int32, maxSteps int) (*rules.Engine, *decision.Decision, decision.Intent, error) {
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	for steps := 0; steps < maxSteps && !e.G.Over; steps++ {
		d := e.Pending()
		if d == nil {
			return nil, nil, decision.Intent{}, fmt.Errorf("no pending decision at step %d", steps)
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if d.Player == 0 && e.G.Turn >= minTurn && (want == "" || d.Kind == want) {
			if _, _, ok := enumerate(searchprobe.NewCollector(0), e, d, in, AllKinds(), DefaultOptions().Limit); ok {
				return e, d, in, nil
			}
		}
		if err := e.Submit(in); err != nil {
			return nil, nil, decision.Intent{}, fmt.Errorf("step %d submit: %w", steps, err)
		}
	}
	return nil, nil, decision.Intent{}, errNoPosition
}

func botPosition(t testing.TB, cfg rules.Config, want decision.Kind, minTurn int32, maxSteps int) (*rules.Engine, *decision.Decision, decision.Intent) {
	t.Helper()
	e, d, in, err := findPosition(cfg, want, minTurn, maxSteps)
	if err != nil {
		t.Fatalf("seed %d, kind %q, turn >= %d: %v", cfg.Seed, want, minTurn, err)
	}
	return e, d, in
}

func searchAt(t testing.TB, e *rules.Engine, d *decision.Decision, bot decision.Intent, net *policynet.Model, opts Options) Result {
	t.Helper()
	obs := searchprobe.NewCollector(d.Player)
	src, err := NewClairvoyant(e, obs)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, src, net, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// checkResult asserts the invariants of a Search that built a tree over
// clairvoyant worlds with no failure.
func checkResult(t *testing.T, d *decision.Decision, bot decision.Intent, res Result, sims int) {
	t.Helper()
	n := len(res.Candidates)
	if n < 2 || len(res.Keys) != n || len(res.Prior) != n || len(res.Visits) != n || len(res.Q) != n {
		t.Fatalf("shape: %d candidates, %d keys, %d prior, %d visits, %d Q", n, len(res.Keys), len(res.Prior), len(res.Visits), len(res.Q))
	}
	st := res.Stats
	if st.Searched != 1 || st.Simulations != sims || st.Completed != sims {
		t.Fatalf("stats %+v, want %d completed simulations", st, sims)
	}
	sum := 0
	for _, v := range res.Visits {
		sum += v
	}
	if sum != st.Completed {
		t.Fatalf("root visits sum to %d, completed %d", sum, st.Completed)
	}
	if st.Terminal+st.StepCapped+st.Expanded != st.Completed {
		t.Fatalf("terminal %d + capped %d + expanded %d != completed %d", st.Terminal, st.StepCapped, st.Expanded, st.Completed)
	}
	psum := 0.0
	for _, p := range res.Prior {
		psum += p
	}
	if math.Abs(psum-1) > 1e-9 {
		t.Fatalf("prior sums to %v", psum)
	}
	if res.Choice < 0 || res.Choice >= n {
		t.Fatalf("choice %d of %d", res.Choice, n)
	}
	if err := d.Validate(res.Intent); err != nil {
		t.Fatalf("chosen intent invalid: %v", err)
	}
	if res.Choice == 0 && !reflect.DeepEqual(res.Intent, bot) {
		t.Fatalf("choice 0 played %+v, want the bot's own intent %+v", res.Intent, bot)
	}
	if res.RootValue < 0 || res.RootValue > 1 {
		t.Fatalf("root value %v", res.RootValue)
	}
}

func TestSearchRealDeckPriority(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	head, draws, turn := e.L.Head(), e.RNGDraws(), e.G.Turn
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 12, 7
	res := searchAt(t, e, d, bot, nil, opts)
	checkResult(t, d, bot, res, 12)
	if res.Kind != "priority" {
		t.Fatalf("kind %q", res.Kind)
	}
	if e.L.Head() != head || e.RNGDraws() != draws || e.G.Turn != turn || e.Pending() != d {
		t.Fatal("Search moved the real engine")
	}
}

func TestSearchRealDeckAttackers(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 12, 8
	res := searchAt(t, e, d, bot, nil, opts)
	checkResult(t, d, bot, res, 12)
	if res.Kind != "attackers" {
		t.Fatalf("kind %q", res.Kind)
	}
}

// Spec §2/§4: the same seed and checkpoint give a byte-identical choice,
// in eval and in generation mode.
func TestSearchIsDeterministic(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 16, 99
	if a, b := searchAt(t, e, d, bot, nil, opts), searchAt(t, e, d, bot, nil, opts); !reflect.DeepEqual(a, b) {
		t.Fatalf("eval searches differ:\n%+v\n%+v", a, b)
	}
	opts.Noise, opts.Sample = true, true
	if a, b := searchAt(t, e, d, bot, nil, opts), searchAt(t, e, d, bot, nil, opts); !reflect.DeepEqual(a, b) {
		t.Fatalf("generation searches differ:\n%+v\n%+v", a, b)
	}
}

// A network with a value head drives both the prior and the leaf.
func TestSearchWithANetworkLeafAndPrior(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	m := policynet.NewModel(policynet.TableRows, 1, 2, rand.New(rand.NewPCG(11, 12)))
	m.InitValue(2, rand.New(rand.NewPCG(13, 14)))
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 8, 5
	res := searchAt(t, e, d, bot, m, opts)
	checkResult(t, d, bot, res, 8)
	if res.Stats.PriorFallbacks != 0 {
		t.Fatalf("prior fell back %d times", res.Stats.PriorFallbacks)
	}
	for _, p := range res.Prior {
		if p <= 0 {
			t.Fatalf("network prior %v has a non-positive entry", res.Prior)
		}
	}
	if again := searchAt(t, e, d, bot, m, opts); !reflect.DeepEqual(res, again) {
		t.Fatal("a network search is not deterministic")
	}
}

// hypSource hands out hypothetical worlds (fresh future chance per
// simulation), the shape ticket 5's sampled worlds take.
type hypSource struct {
	e   *rules.Engine
	obs *searchprobe.Collector
}

func (h hypSource) World(sim int) (World, error) {
	return World{Engine: h.e.CloneHypothetical(uint64(sim) + 1), Observer: h.obs.Clone(), Hypothetical: true}, nil
}

func TestSearchHypotheticalWorlds(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	obs := searchprobe.NewCollector(0)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 8, 3
	res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, hypSource{e: e, obs: obs}, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, d, bot, res, 8)
}

type fixedSource struct {
	w   World
	err error
}

func (f fixedSource) World(int) (World, error) { return f.w, f.err }

// Review Focus 3: a world that is not at the root decision is discarded and
// counted; when every simulation fails the bot's answer is played.
func TestSearchBadWorldPlaysTheBot(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	past := e.Clone()
	if err := past.Submit(bot); err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	opts.Sims = 5
	for _, tc := range []struct {
		name  string
		src   WorldSource
		count func(Stats) int
	}{
		{"one intent past the root", fixedSource{w: World{Engine: past, Observer: searchprobe.NewCollector(0)}}, func(s Stats) int { return s.BadWorlds }},
		{"no engine", fixedSource{w: World{Observer: searchprobe.NewCollector(0)}}, func(s Stats) int { return s.BadWorlds }},
		{"no world", fixedSource{err: fmt.Errorf("%w: sampler starved", ErrNoWorld)}, func(s Stats) int { return s.NoWorld }},
	} {
		obs := searchprobe.NewCollector(0)
		res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, tc.src, nil, opts)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.count(res.Stats) != 5 || res.Stats.Completed != 0 || res.Stats.AllFailed != 1 {
			t.Errorf("%s: stats %+v", tc.name, res.Stats)
		}
		if res.Choice != 0 || !reflect.DeepEqual(res.Intent, bot) {
			t.Errorf("%s: played %+v, want the bot's answer", tc.name, res.Intent)
		}
	}
}

// Review Focus 5 on the real engine: no walk submits more than MaxSteps.
func TestSearchStepCapBoundsEveryWalk(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	opts := DefaultOptions()
	opts.Sims, opts.MaxSteps = 8, 1
	res := searchAt(t, e, d, bot, nil, opts)
	checkResult(t, d, bot, res, 8)
	if res.Stats.EnvSteps > res.Stats.Simulations*opts.MaxSteps {
		t.Fatalf("env steps %d exceed %d simulations x cap %d", res.Stats.EnvSteps, res.Stats.Simulations, opts.MaxSteps)
	}
}

type countingSource struct{ calls int }

func (c *countingSource) World(int) (World, error) {
	c.calls++
	return World{}, ErrNoWorld
}

// Review Focus 1 and 4: a priority the bot answers with a land play is not a
// node; the bot's own intent is played and the source is never asked.
func TestSearchSkipsWhenTheBotPlaysALand(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	for steps := 0; steps < 2000 && !e.G.Over; steps++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision at step %d", steps)
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if d.Player == 0 && d.Kind == decision.KPriority && len(in.Choices) == 1 && d.Options[in.Choices[0]].Kind == "play_land" {
			src := &countingSource{}
			res, err := Search(Root{Engine: e, Decision: d, Bot: in, Observer: searchprobe.NewCollector(0)}, src, nil, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if res.Kind != "priority" || res.Stats.Skipped != 1 || res.Stats.Searched != 0 {
				t.Fatalf("result %+v, want a skipped priority", res)
			}
			if !reflect.DeepEqual(res.Intent, in) || src.calls != 0 {
				t.Fatalf("played %+v with %d world calls, want the bot's land and none", res.Intent, src.calls)
			}
			return
		}
		if err := e.Submit(in); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("seat 0 never played a land")
}

func TestValidateRefusesUnusableOptionsAndNetworks(t *testing.T) {
	for _, o := range []Options{
		func() Options { o := DefaultOptions(); o.CPUCT = 0; return o }(),
		func() Options { o := DefaultOptions(); o.FPU = -1; return o }(),
		func() Options { o := DefaultOptions(); o.Limit = 1; return o }(),
		func() Options { o := DefaultOptions(); o.MaxSteps = 0; return o }(),
		func() Options { o := DefaultOptions(); o.DirichletAlpha = 0; return o }(),
		func() Options { o := DefaultOptions(); o.DirichletEps = 2; return o }(),
		func() Options { o := DefaultOptions(); o.Kinds = Kinds{}; return o }(),
	} {
		if o.Validate(nil) == nil {
			t.Errorf("options %+v accepted", o)
		}
	}
	if err := DefaultOptions().Validate(&policynet.Model{}); err == nil {
		t.Error("a checkpoint without a value head accepted")
	}
	if err := DefaultOptions().Validate(&policynet.Model{ValueHidden: 1, Features: policynet.FeaturesMZOppHand}); err == nil {
		t.Error("an oracle feature set accepted")
	}
	if err := DefaultOptions().Validate(nil); err != nil {
		t.Errorf("defaults refused: %v", err)
	}
}
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/azmcts/`
Expected: FAIL to compile: `undefined: clairvoyantAllowed`, `undefined: NewClairvoyant`, `undefined: engineEnv`, `undefined: Search`, `undefined: Root`, `undefined: World`.

- [ ] **Step 3: Implement**

`internal/azmcts/world.go`:

```go
package azmcts

import (
	"errors"
	"sync/atomic"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// World is one simulation's world: an engine positioned at the root
// decision, and an observer (a collector for the searching seat) that knows
// the root decision's objects. Hypothetical marks an engine built by
// rules.NewHypothetical or CloneHypothetical, stepped with
// SubmitHypothetical so a chance failure is reported, not panicked.
type World struct {
	Engine       *rules.Engine
	Observer     *searchprobe.Collector
	Hypothetical bool
}

// WorldSource hands each simulation a fresh world (spec §1). Nothing else in
// the search touches hidden information. An error wrapping ErrNoWorld means
// no world could be produced (ticket 5's sampler starving with no redeal).
type WorldSource interface {
	World(sim int) (World, error)
}

// ErrClairvoyantRefused is NewClairvoyant's answer until the driving command
// calls AllowClairvoyant.
var ErrClairvoyantRefused = errors.New("azmcts: the clairvoyant world source clones the REAL engine -- hidden zones and future chance included -- and is refused unless the driving command called AllowClairvoyant (cmd/botbench does for a -az-world clairvoyant side; no host or gorged path does)")

var clairvoyantAllowed atomic.Bool

// AllowClairvoyant opens the clairvoyant source for this process. Only a
// bench or training command calls it, once, before any game starts (spec
// §1: stage 1 is bench and training only). host, host/httpapi and
// cmd/gorged cannot link this package at all (internal/archtest).
func AllowClairvoyant() { clairvoyantAllowed.Store(true) }

type clairvoyant struct {
	e   *rules.Engine
	obs *searchprobe.Collector
}

// NewClairvoyant is the stage-1 source: every simulation walks a Clone of
// the real engine e, with a Clone of obs. obs must be the collector Search
// is given as Root.Observer; World clones it lazily, so every clone is taken
// after Search has observed the root decision.
func NewClairvoyant(e *rules.Engine, obs *searchprobe.Collector) (WorldSource, error) {
	if !clairvoyantAllowed.Load() {
		return nil, ErrClairvoyantRefused
	}
	if e == nil || obs == nil {
		return nil, errors.New("azmcts: the clairvoyant source needs the root engine and observer")
	}
	return &clairvoyant{e: e, obs: obs}, nil
}

func (c *clairvoyant) World(int) (World, error) {
	return World{Engine: c.e.Clone(), Observer: c.obs.Clone()}, nil
}
```

`internal/azmcts/env.go`:

```go
package azmcts

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// walkConfig is what every simulation of one Search shares.
type walkConfig struct {
	net       *policynet.Model
	kinds     Kinds
	limit     int
	maxSteps  int
	envSeed   uint64
	actor     state.PlayerID
	root      *Point
	rootCands []cand
	rootDec   *decision.Decision
	stats     *Stats
}

// worldEnvs adapts a WorldSource to the tree's EnvSource.
type worldEnvs struct {
	src WorldSource
	cfg *walkConfig
}

func (w *worldEnvs) Env(sim int) (Env, error) {
	world, err := w.src.World(sim)
	if err != nil {
		return nil, err
	}
	env, err := newEngineEnv(world, w.cfg)
	if err != nil {
		return nil, err // never a typed-nil *engineEnv inside a non-nil Env
	}
	return env, nil
}

// engineEnv walks one world. The searching seat's searched decisions are
// the tree's points; botpolicy.Decide answers every other decision of both
// seats (spec §1's env step), from bot streams seeded identically for every
// simulation of the decision, so a clairvoyant clone is deterministic along
// a path.
type engineEnv struct {
	e      *rules.Engine
	obs    *searchprobe.Collector
	hyp    bool
	cfg    *walkConfig
	rngs   []*rand.Rand
	board  botpolicy.Board
	cur    *decision.Decision
	cands  []cand
	steps  int
	capped bool
}

func newEngineEnv(w World, cfg *walkConfig) (*engineEnv, error) {
	if w.Engine == nil || w.Observer == nil {
		return nil, fmt.Errorf("%w: the world has no engine or observer", ErrBadWorld)
	}
	pd, rd := w.Engine.Pending(), cfg.rootDec
	if w.Engine.G.Over || pd == nil || pd.Seq != rd.Seq || pd.Player != rd.Player || pd.Kind != rd.Kind {
		return nil, fmt.Errorf("%w (root seq %d)", ErrBadWorld, rd.Seq)
	}
	n := len(w.Engine.G.Players)
	return &engineEnv{
		e: w.Engine, obs: w.Observer, hyp: w.Hypothetical, cfg: cfg,
		rngs: searchprobe.BotRandoms(cfg.envSeed, n), board: botpolicy.NewBoard(n),
		cur: pd, cands: cfg.rootCands,
	}, nil
}

func (e *engineEnv) Root() *Point { return e.cfg.root }

func (e *engineEnv) Play(k Key) (*Point, error) {
	if e.cur == nil {
		return nil, fmt.Errorf("%w: play after the walk ended", ErrSubmit)
	}
	i := -1
	for j, c := range e.cands {
		if c.key == k {
			i = j
			break
		}
	}
	if i < 0 {
		return nil, fmt.Errorf("%w: candidate %s is not offered here", ErrSubmit, k)
	}
	if err := e.submit(e.cur, e.cands[i].in); err != nil {
		return nil, err
	}
	return e.advance()
}

// advance answers decisions with the bot until the searching seat's next
// searched decision (a point), game over, or the step cap (nil).
func (e *engineEnv) advance() (*Point, error) {
	for {
		g := e.e.G
		if g.Over {
			e.cur, e.cands = nil, nil
			return nil, nil
		}
		pd := e.e.Pending()
		if pd == nil {
			return nil, fmt.Errorf("%w: no pending decision and the game is not over", ErrSubmit)
		}
		if e.steps >= e.cfg.maxSteps {
			e.capped = true
			e.cur, e.cands = nil, nil
			return nil, nil
		}
		b := botpolicy.BoardFromGameInto(g, e.e, pd.Player, &e.board)
		in := botpolicy.Decide(b, pd, e.rngs[pd.Player])
		if pd.Player == e.cfg.actor {
			if cands, kind, ok := enumerate(e.obs, e.e, pd, in, e.cfg.kinds, e.cfg.limit); ok {
				e.cur, e.cands = pd, cands
				prior, fell := priors(e.cfg.net, e.e, pd, in, kind, cands)
				if fell {
					e.cfg.stats.PriorFallbacks++
				}
				keys := make([]Key, len(cands))
				for i, c := range cands {
					keys[i] = c.key
				}
				return &Point{Keys: keys, Prior: prior}, nil
			}
		}
		if err := e.submit(pd, in); err != nil {
			return nil, err
		}
		e.steps++
		e.cfg.stats.EnvSteps++
	}
}

// submit plays in on the world, recovering any engine panic (the livelock
// watcher panics with *rules.LivelockError) into ErrPanic. A hypothetical
// world reports chance failures and ordinary rejections through one error,
// so an intent d itself rejects is classified first as ErrSubmit and any
// other SubmitHypothetical error is classified ErrChance.
func (e *engineEnv) submit(d *decision.Decision, in decision.Intent) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
	if !e.hyp {
		if serr := e.e.Submit(in); serr != nil {
			return fmt.Errorf("%w: %v", ErrSubmit, serr)
		}
		return nil
	}
	if verr := d.Validate(in); verr != nil {
		return fmt.Errorf("%w: %v", ErrSubmit, verr)
	}
	if herr := e.e.SubmitHypothetical(in); herr != nil {
		return fmt.Errorf("%w: %v", ErrChance, herr)
	}
	return nil
}

// Leaf is 1/0/0.5 at game over, else the leaf evaluator on the searching
// seat's redacted view.
func (e *engineEnv) Leaf() Leaf {
	g := e.e.G
	if g.Over {
		v := 0.0
		switch {
		case g.Draw:
			v = 0.5
		case g.Winner == e.cfg.actor:
			v = 1
		}
		return Leaf{V: v, Terminal: true}
	}
	return Leaf{V: leafValue(e.cfg.net, e.e, e.cfg.actor), Capped: e.capped}
}

// leafValue is spec §1's leaf: the value head on the actor's REDACTED view
// (policynet.Model.Value), or -- generation 0, no network -- the frozen
// heuristic searchprobe.LeafValue. Clamped into [0,1]; NaN reads 0.5.
func leafValue(net *policynet.Model, e *rules.Engine, actor state.PlayerID) float64 {
	v := view.Project(e.G, e, actor, e.Pending())
	if net == nil {
		return searchprobe.LeafValue(v, actor)
	}
	x := float64(net.Value(policynet.EncodeStateWith(net.Features, v, actor, nil)))
	switch {
	case math.IsNaN(x):
		return 0.5
	case x < 0:
		return 0
	case x > 1:
		return 1
	}
	return x
}
```

`internal/azmcts/search.go`:

```go
package azmcts

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// Root is the real decision being searched.
type Root struct {
	// Engine is the real engine. Search reads it (the actor's own decision,
	// its board for the blockers guard, its view for the prior) and never
	// submits to it.
	Engine *rules.Engine
	// Decision is Engine's pending decision, the searching seat's own.
	Decision *decision.Decision
	// Bot is the wrapped bot's answer: candidate 0, the tie-winner, and the
	// answer on every failure path.
	Bot decision.Intent
	// Observer is a FRESH collector for Decision.Player -- never a
	// searchseat.Feed's, whose Capture stream ObserveDecision would perturb.
	// Every world's Observer must be a clone of it taken after Search starts.
	Observer *searchprobe.Collector
}

// Result is one Search. Candidates, Keys, Visits, Prior and Q are parallel;
// index 0 is the bot's answer.
type Result struct {
	Kind       string // "", or the searched kind
	Candidates []decision.Intent
	Keys       []Key
	Visits     []int
	Prior      []float64 // the prior before any root noise
	Q          []float64
	RootValue  float64
	Choice     int
	Intent     decision.Intent // the answer to play
	Stats      Stats
}

// Validate refuses options and networks Search must not run with.
func (o Options) Validate(net *policynet.Model) error {
	switch {
	case o.CPUCT <= 0:
		return fmt.Errorf("azmcts: CPUCT %g must be > 0", o.CPUCT)
	case o.FPU < 0:
		return fmt.Errorf("azmcts: FPU %g must be >= 0", o.FPU)
	case o.Limit < 2:
		return fmt.Errorf("azmcts: candidate limit %d must be >= 2", o.Limit)
	case o.MaxSteps < 1:
		return fmt.Errorf("azmcts: step cap %d must be >= 1", o.MaxSteps)
	case o.DirichletAlpha <= 0:
		return fmt.Errorf("azmcts: Dirichlet alpha %g must be > 0", o.DirichletAlpha)
	case o.DirichletEps < 0 || o.DirichletEps > 1:
		return fmt.Errorf("azmcts: Dirichlet epsilon %g must be in [0,1]", o.DirichletEps)
	case o.Kinds == (Kinds{}):
		return errors.New("azmcts: no searched decision kinds")
	}
	if net != nil {
		if !net.HasValue() {
			return errors.New("azmcts: checkpoint has no value head; the value head is the search's leaf (spec §1), so a policy-only checkpoint cannot drive it")
		}
		if net.Features.Diagnostic() {
			return fmt.Errorf("azmcts: checkpoint feature set %s reads hidden information; the network must read the searching seat's redacted view (spec §1)", net.Features)
		}
	}
	return nil
}

// Search runs the tree at the searching seat's current decision (spec §1-§2)
// and returns the answer to play. A decision that is not searched -- not a
// searched kind, fewer than two candidates, a bot answer outside the
// candidate vocabulary, Sims <= 0 -- returns the bot's intent without asking
// src for a world. If every simulation fails, the bot's intent is played and
// Stats.AllFailed is 1. The error return is reserved for misconfiguration
// (invalid options or network, a missing root field or source).
//
// net nil is generation 0: a uniform prior and the heuristic leaf. The
// result is a pure function of (root position, bot answer, net, opts): the
// same Seed gives a byte-identical Result.
func Search(root Root, src WorldSource, net *policynet.Model, opts Options) (Result, error) {
	res := Result{Intent: root.Bot}
	if err := opts.Validate(net); err != nil {
		return res, err
	}
	if root.Engine == nil || root.Decision == nil || root.Observer == nil {
		return res, errors.New("azmcts: Search needs the root engine, decision and observer")
	}
	cands, kind, ok := enumerate(root.Observer, root.Engine, root.Decision, root.Bot, opts.Kinds, opts.Limit)
	res.Kind = kind
	if !ok {
		if kind != "" {
			res.Stats.Skipped = 1
		}
		return res, nil
	}
	res.Candidates = make([]decision.Intent, len(cands))
	res.Keys = make([]Key, len(cands))
	for i, c := range cands {
		res.Candidates[i], res.Keys[i] = c.in, c.key
	}
	prior, fell := priors(net, root.Engine, root.Decision, root.Bot, kind, cands)
	if fell {
		res.Stats.PriorFallbacks++
	}
	res.Prior = prior
	if opts.Sims <= 0 {
		return res, nil
	}
	if src == nil {
		return res, errors.New("azmcts: Search needs a world source")
	}
	res.Stats.Searched = 1
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))
	treePrior := prior
	if opts.Noise {
		treePrior = noisyPrior(prior, rng, opts.DirichletAlpha, opts.DirichletEps)
	}
	rootPt := &Point{Keys: res.Keys, Prior: treePrior}
	cfg := &walkConfig{
		net: net, kinds: opts.Kinds, limit: opts.Limit, maxSteps: opts.MaxSteps,
		envSeed: splitmix(opts.Seed ^ 0x656e762d73656564), actor: root.Decision.Player,
		root: rootPt, rootCands: cands, rootDec: root.Decision, stats: &res.Stats,
	}
	tr, err := RunTree(rootPt, &worldEnvs{src: src, cfg: cfg}, opts, &res.Stats)
	if err != nil {
		return res, err
	}
	res.Visits, res.Q, res.RootValue = tr.Visits, tr.Q, tr.RootValue
	if res.Stats.Completed == 0 {
		res.Stats.AllFailed = 1
		return res, nil
	}
	res.Choice = choose(res.Visits, opts.Sample, rng)
	if res.Choice != 0 {
		res.Intent = cands[res.Choice].in
	}
	return res, nil
}

// DecisionSeed is the per-decision seed of spec §2: a mix of the seat's seed
// (itself derived from the game seed, internal/bench.RunPairs) and the
// decision's sequence number.
func DecisionSeed(seatSeed, seq uint64) uint64 { return splitmix(seatSeed ^ splitmix(seq)) }

// splitmix is SplitMix64's finaliser.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
```

- [ ] **Step 4: Run and pass**

Run: `ls .cards >/dev/null && systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -v ./internal/azmcts/ 2>&1 | tail -60`
Expected: PASS, with every `TestSearch*` reporting `--- PASS`, not `--- SKIP`. If `TestSearchRealDeckAttackers` fails with "no searchable seat-0 decision", raise that test's `maxSteps` from 3000 to 6000; do not change the seed or the decks. If it still fails, report it: the searchseat attackers tests use the same pair and seed.

- [ ] **Step 5: Commit**

```bash
git add internal/azmcts/world.go internal/azmcts/env.go internal/azmcts/search.go internal/azmcts/world_test.go internal/azmcts/search_test.go
git commit -m "feat(azmcts): clairvoyant worlds, the engine env step, and Search

Search builds the root candidates, runs the PUCT tree over one engine clone
per simulation (the bot answers everything between the seat's searched
decisions; a step cap stops livelocks; engine panics and chance failures
discard the simulation and are counted), and plays the most-visited
candidate -- the bot's own intent on every fallback. The clairvoyant source
is refused unless the driving command calls AllowClairvoyant."
```

---

## Task 6: The `az` seat, its diagnostics hook, and the per-simulation benchmark

**Files:**
- Create: `internal/azmcts/seat.go`, `internal/azmcts/seat_test.go`, `internal/azmcts/bench_test.go`

**Interfaces:**
- Consumes: `searchseat.SearchSeat` and `searchseat.Env` (`internal/searchseat/searchbot.go:61,71`); `seat.NewBot` (`seat/bot.go:76`), `(*seat.Bot).Decide`/`DecideBoard` (`seat/bot.go:384,394`); `seat.Seat`, `seat.BoardSeat` (`seat/seat.go:19,34`); `internal/bench.PlayGame` (`internal/bench/bench.go:137`, which type-asserts `searchseat.SearchSeat` at `:159`); Task 5.
- Produces:
  - `type SeatConfig struct { Search Options; Explore bool; ExploreTurns int32 }`; `func DefaultSeatConfig() SeatConfig`
  - `type Diag struct { Turn int32; Kind string; Searched bool; Candidates, Choice int; MS float64; Stats Stats }`
  - `var Millis func() float64`; `var Watch func(Diag)`
  - `type Seat struct{...}`; `func NewSeat(seed uint64, net *policynet.Model, cfg SeatConfig) (*Seat, error)`; methods `Decide`, `DecideBoard`, `DecideSearch` (the `searchseat.SearchSeat` signatures)

- [ ] **Step 1: Write the failing tests**

`internal/azmcts/seat_test.go`:

```go
package azmcts

import (
	"errors"
	"testing"

	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

func playAZ(t *testing.T, cfg rules.Config, az seat.Seat, maxIntents int) (string, error) {
	t.Helper()
	_, e, err := gbench.PlayGame(cfg, []seat.Seat{az, seat.NewBot(cfg.Seed ^ 2)}, 200, maxIntents, gbench.Hooks{})
	if e == nil {
		return "", err
	}
	return e.L.Head(), err
}

// With no simulations the seat is exactly the bot it wraps: same rng stream,
// same answers, same chain head (the delegation contract).
func TestSeatWithoutSimulationsIsTheBot(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 0
	az, err := NewSeat(cfg.Seed^1, nil, sc)
	if err != nil {
		t.Fatal(err)
	}
	azHead, err := playAZ(t, cfg, az, 400)
	if err != nil {
		t.Fatal(err)
	}
	botHead, err := playAZ(t, cfg, seat.NewBot(cfg.Seed^1), 400)
	if err != nil {
		t.Fatal(err)
	}
	if azHead != botHead {
		t.Fatalf("az with 0 sims head %s, bot head %s", azHead, botHead)
	}
}

// Spec §4 determinism, end to end: the same seeds replay the same game, and
// the search did run.
func TestSeatSearchesAndReplaysExactly(t *testing.T) {
	allowClairvoyantForTest(t)
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 4
	var diags []Diag
	prev := Watch
	Watch = func(d Diag) { diags = append(diags, d) }
	t.Cleanup(func() { Watch = prev })
	heads := make([]string, 2)
	for i := range heads {
		az, err := NewSeat(cfg.Seed^1, nil, sc)
		if err != nil {
			t.Fatal(err)
		}
		if heads[i], err = playAZ(t, cfg, az, 300); err != nil {
			t.Fatal(err)
		}
	}
	if heads[0] != heads[1] {
		t.Fatalf("replay diverged: %s vs %s", heads[0], heads[1])
	}
	searched := 0
	for _, d := range diags {
		if d.Searched {
			searched++
			if d.Stats.Simulations != 4 {
				t.Fatalf("a searched decision ran %d simulations, want 4", d.Stats.Simulations)
			}
		}
	}
	if searched == 0 {
		t.Fatal("the az seat never searched")
	}
}

func TestSeatRefusesClairvoyantByDefault(t *testing.T) {
	prev := clairvoyantAllowed.Load()
	clairvoyantAllowed.Store(false)
	t.Cleanup(func() { clairvoyantAllowed.Store(prev) })
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims = 4
	az, err := NewSeat(cfg.Seed^1, nil, sc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := playAZ(t, cfg, az, 300); !errors.Is(err, ErrClairvoyantRefused) {
		t.Fatalf("game error %v, want ErrClairvoyantRefused", err)
	}
}

func TestNewSeatValidates(t *testing.T) {
	sc := DefaultSeatConfig()
	sc.Search.CPUCT = 0
	if _, err := NewSeat(1, nil, sc); err == nil {
		t.Fatal("invalid options accepted")
	}
}
```

`internal/azmcts/bench_test.go`:

```go
package azmcts

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// stagePosition finds a searchable seat-0 decision at turn >= minTurn with
// uw-tempo in seat 0 against mono-blue-tempo (the Stage 0 deck set), trying
// the eval block's first ten seeds in order.
func stagePosition(b *testing.B, minTurn int32) (*rules.Engine, *decision.Decision, decision.Intent) {
	b.Helper()
	for s := uint64(90000000); s < 90000010; s++ {
		cfg := testConfig(b, "uw-tempo", "mono-blue-tempo", s)
		if e, d, bot, err := findPosition(cfg, "", minTurn, 6000); err == nil {
			return e, d, bot
		}
	}
	b.Fatalf("no seed in 90000000..90000009 reached a searchable seat-0 decision at turn %d", minTurn)
	return nil, nil, decision.Intent{}
}

// benchSearch reports ns per simulation (the spec's per-simulation cost)
// plus -benchmem's allocations per searched decision (one op = one Search of
// 25 simulations, heuristic leaf, clairvoyant).
func benchSearch(b *testing.B, minTurn int32) {
	allowClairvoyantForTest(b)
	e, d, bot := stagePosition(b, minTurn)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 25, 1
	b.ReportAllocs()
	b.ResetTimer()
	sims := 0
	for i := 0; i < b.N; i++ {
		obs := searchprobe.NewCollector(d.Player)
		src, err := NewClairvoyant(e, obs)
		if err != nil {
			b.Fatal(err)
		}
		res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, src, nil, opts)
		if err != nil {
			b.Fatal(err)
		}
		sims += res.Stats.Simulations
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(max(sims, 1)), "ns/sim")
}

func BenchmarkSearchDecisionEarly(b *testing.B) { benchSearch(b, 3) }

// Late: the event log is long here, so the first append after each Clone
// copies it (spec §2 "Known cost risk").
func BenchmarkSearchDecisionLate(b *testing.B) { benchSearch(b, 9) }
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestSeat|TestNewSeat' ./internal/azmcts/`
Expected: FAIL to compile: `undefined: DefaultSeatConfig`, `undefined: NewSeat`, `undefined: Watch`, `undefined: Diag`.

- [ ] **Step 3: Implement**

`internal/azmcts/seat.go`:

```go
package azmcts

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// SeatConfig is the az seat's configuration.
type SeatConfig struct {
	// Search are the per-decision knobs; Seed, Noise and Sample are set per
	// decision by the seat and ignored here.
	Search Options
	// Explore is generation mode (ticket 3's azgen): Dirichlet noise at every
	// searched root, and moves sampled proportional to visits while the game
	// turn is <= ExploreTurns (spec §2: turns 1-4). Eval leaves it false:
	// argmax, no noise.
	Explore      bool
	ExploreTurns int32
}

// DefaultSeatConfig is the eval seat with the spec's knobs.
func DefaultSeatConfig() SeatConfig { return SeatConfig{Search: DefaultOptions(), ExploreTurns: 4} }

// Diag is one decision of a searched kind as the cost report sees it.
type Diag struct {
	Turn       int32
	Kind       string // priority, attackers, blockers, target; "" for a FeedStopped record
	Searched   bool   // a tree was built
	Candidates int
	Choice     int
	MS         float64 // wall ms of the whole decision (Millis); 0 when untimed
	Stats      Stats
}

// Millis is the monotonic elapsed-milliseconds clock the driving command
// installs before any game starts (this package may not import time --
// internal/archtest). Nil means untimed. It never reaches an answer.
var Millis func() float64

// Watch receives one Diag per decision of a searched kind the seat answers,
// plus one FeedStopped record per decision the driver routed around the
// search. Installed once before any game starts; games run on several
// goroutines, so the consumer synchronises itself. Nil is silent.
var Watch func(Diag)

// Seat is the az policy: the default bot (seat.NewBot, the same PCG
// derivation, so its delegation consumes exactly the bot's stream) with
// Search answering the searched kinds. It is a searchseat.SearchSeat, so
// internal/bench.PlayGame hands it the live engine at its own decisions --
// the route the L10 search seat takes. Stage 1 reads only the engine; the
// driver's observation feed is for ticket 5's sampled worlds.
type Seat struct {
	def  *seat.Bot
	seed uint64
	net  *policynet.Model
	cfg  SeatConfig
}

var (
	_ seat.Seat             = (*Seat)(nil)
	_ seat.BoardSeat        = (*Seat)(nil)
	_ searchseat.SearchSeat = (*Seat)(nil)
)

// NewSeat builds one seat of one game. seed is the per-seat seed the bench
// derives; net nil is generation 0.
func NewSeat(seed uint64, net *policynet.Model, cfg SeatConfig) (*Seat, error) {
	if err := cfg.Search.Validate(net); err != nil {
		return nil, err
	}
	return &Seat{def: seat.NewBot(seed), seed: seed, net: net, cfg: cfg}, nil
}

// Decide is the plain Seat half: the wrapped bot.
func (s *Seat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.def.Decide(ctx, v, d)
}

// DecideBoard is the driver's fallback when the seat's observation feed has
// stopped: the wrapped bot, counted (FeedStopped) so it is never silent.
func (s *Seat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	if Watch != nil {
		Watch(Diag{Stats: Stats{FeedStopped: 1}})
	}
	return s.def.DecideBoard(ctx, b, d)
}

// DecideSearch answers one of the seat's own decisions: the bot's answer
// first (candidate 0 and every fallback), then Search over clairvoyant
// clones of env.Engine. A refused clairvoyant source is an error: the game
// fails loudly rather than playing an unsearched seat under the az name.
func (s *Seat) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	botIn, err := s.def.DecideBoard(ctx, env.Board, d)
	if err != nil {
		return decision.Intent{}, err
	}
	if s.cfg.Search.Sims <= 0 || env.Engine == nil {
		return botIn, nil
	}
	var t0 float64
	if Millis != nil {
		t0 = Millis()
	}
	obs := searchprobe.NewCollector(d.Player)
	src, err := NewClairvoyant(env.Engine, obs)
	if err != nil {
		return decision.Intent{}, err
	}
	opts := s.cfg.Search
	opts.Seed = DecisionSeed(s.seed, d.Seq)
	opts.Noise, opts.Sample = false, false
	if s.cfg.Explore {
		opts.Noise = true
		opts.Sample = env.Engine.G.Turn <= s.cfg.ExploreTurns
	}
	res, err := Search(Root{Engine: env.Engine, Decision: &d, Bot: botIn, Observer: obs}, src, s.net, opts)
	if err != nil {
		return decision.Intent{}, err
	}
	if res.Kind != "" && Watch != nil {
		dg := Diag{
			Turn: env.Engine.G.Turn, Kind: res.Kind, Searched: res.Stats.Searched == 1,
			Candidates: len(res.Candidates), Choice: res.Choice, Stats: res.Stats,
		}
		if Millis != nil {
			dg.MS = Millis() - t0
		}
		Watch(dg)
	}
	return res.Intent, nil
}
```

- [ ] **Step 4: Run and pass, then measure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -v -run 'TestSeat|TestNewSeat' ./internal/azmcts/ 2>&1 | tail -20`
Expected: PASS, with each `TestSeat*` reporting `--- PASS` (not SKIP).

Run (the whole package, with its peak RSS; spec §4 memory rule): `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -exec '/usr/bin/time -f peak-rss-kb=%M' ./internal/azmcts/ 2>&1 | tail -5`
Expected: `ok`, and a `peak-rss-kb=` line. Record the number in the task report. A value above 1 000 000 KB is a bug to investigate before committing.

Run (a benchmark smoke test, to prove the benchmarks run; this is not the Stage 0 measurement): `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run '^$' -bench 'BenchmarkSearchDecision' -benchmem -benchtime 2x ./internal/azmcts/`
Expected: two benchmark lines, each with `ns/op`, `ns/sim`, `B/op`, `allocs/op`.

- [ ] **Step 5: Commit**

```bash
git add internal/azmcts/seat.go internal/azmcts/seat_test.go internal/azmcts/bench_test.go
git commit -m "feat(azmcts): the az seat and its per-simulation benchmark

Seat wraps the default bot (same rng stream) and answers the searched kinds
with Search over clairvoyant clones of the engine the bench driver feeds it
(the searchseat.SearchSeat route). With 0 simulations it is the bot byte for
byte. Diagnostics reach the driving command through Watch/Millis; a driver
fallback around the search is counted, never silent."
```

---

## Task 7: Wire `-a az` into `cmd/botbench`

**Files:**
- Create: `cmd/botbench/azcost.go`, `cmd/botbench/azcost_test.go`
- Modify: `cmd/botbench/main.go` (import block; `policies` map after the `"search"` entry at `:258-260`; `parseOppMix` at `:333`; `main()` around `flag.Parse()` at `:2246-2251`; `mainExit` checkpoint gate at `:2367-2369`; hook install at `:2434-2436`; reports at `:2526-2528` and `:2537-2539`)

**Interfaces:**
- Consumes: `azmcts.DefaultSeatConfig`, `DefaultOptions`, `ParseKinds`, `NewSeat`, `AllowClairvoyant`, `Diag`, `Stats.Add`, `Watch`, `Millis`, `Options.Validate` (Tasks 3–6); `meanF`/`quantF` (`cmd/botbench/searchcost.go:214,226`); `mainExit` (`cmd/botbench/main.go:2281`); `corpusDirOrSkip` (`cmd/botbench/main_test.go:29`); `writeZeroCheckpoint` (`cmd/botbench/policynet_test.go:27`).
- Produces: `func registerAZFlags(fs *flag.FlagSet)`; `func azFrontDoor(aName, bName string, m *policynet.Model) error`; `func installAZCostStats()`; `func azCostReport(totalGames int) string`; package vars `azCfg`, `azNet`, `azWorldArg`, `azKindsArg`, `azFlagsGiven`, `azStats`; botbench flags `-az-sims`, `-az-world`, `-az-cpuct`, `-az-fpu`, `-az-candidates`, `-az-max-steps`, `-az-kinds`; policy name `az`.

- [ ] **Step 1: Write the failing tests**

`cmd/botbench/azcost_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// saveAZ restores every az package variable after the test.
func saveAZ(t *testing.T) {
	t.Helper()
	cfg, net, world, kinds, given := azCfg, azNet, azWorldArg, azKindsArg, azFlagsGiven
	azStats.mu.Lock()
	diags := azStats.diags
	azStats.mu.Unlock()
	millis, watch := azmcts.Millis, azmcts.Watch
	t.Cleanup(func() {
		azCfg, azNet, azWorldArg, azKindsArg, azFlagsGiven = cfg, net, world, kinds, given
		azStats.mu.Lock()
		azStats.diags = diags
		azStats.mu.Unlock()
		azmcts.Millis, azmcts.Watch = millis, watch
	})
}

func TestAZFrontDoor(t *testing.T) {
	saveAZ(t)
	azWorldArg, azFlagsGiven = "", false
	if err := azFrontDoor("bot", "bot", nil); err != nil {
		t.Fatalf("no az side, no az flags: %v", err)
	}
	azFlagsGiven = true
	if err := azFrontDoor("bot", "bot", nil); err == nil || !strings.Contains(err.Error(), "neither side is az") {
		t.Fatalf("az flags without an az side: %v", err)
	}
	azFlagsGiven = false
	for _, tc := range []struct{ world, want string }{
		{"", "requires -az-world clairvoyant"},
		{"sampled", "ticket 5"},
		{"oracle", "want clairvoyant or sampled"},
	} {
		azWorldArg = tc.world
		if err := azFrontDoor("az", "bot", nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("-az-world %q: %v, want %q", tc.world, err, tc.want)
		}
	}
	azWorldArg = "clairvoyant"
	azKindsArg = "priority,bogus"
	if err := azFrontDoor("az", "bot", nil); err == nil || !strings.Contains(err.Error(), "-az-kinds") {
		t.Fatalf("bad -az-kinds: %v", err)
	}
	azKindsArg = "attackers"
	if err := azFrontDoor("bot", "az", &policynet.Model{}); err == nil || !strings.Contains(err.Error(), "no value head") {
		t.Fatalf("policy-only checkpoint: %v", err)
	}
	if err := azFrontDoor("bot", "az", &policynet.Model{ValueHidden: 1, Features: policynet.FeaturesMZOppHand}); err == nil || !strings.Contains(err.Error(), "hidden information") {
		t.Fatalf("oracle checkpoint: %v", err)
	}
	if err := azFrontDoor("az", "bot", nil); err != nil {
		t.Fatalf("valid az side: %v", err)
	}
	if azCfg.Search.Kinds != (azmcts.Kinds{Attackers: true}) || azNet != nil {
		t.Fatalf("front door stored kinds %+v net %v", azCfg.Search.Kinds, azNet)
	}
}

func TestAZCostReportNumbers(t *testing.T) {
	saveAZ(t)
	azStats.mu.Lock()
	azStats.diags = []azmcts.Diag{
		{Turn: 3, Kind: "priority", Searched: true, Candidates: 3, Choice: 0, MS: 100,
			Stats: azmcts.Stats{Searched: 1, Simulations: 10, Completed: 10, EnvSteps: 50, Expanded: 8, Terminal: 1, StepCapped: 1}},
		{Turn: 8, Kind: "attackers", Searched: true, Candidates: 4, Choice: 2, MS: 300,
			Stats: azmcts.Stats{Searched: 1, Simulations: 10, Completed: 9, Panics: 1, EnvSteps: 70, Expanded: 9}},
		{Turn: 14, Kind: "priority", MS: 1, Stats: azmcts.Stats{Skipped: 1}},
		{Stats: azmcts.Stats{FeedStopped: 1}},
	}
	azStats.mu.Unlock()
	out := azCostReport(2)
	for _, want := range []string{
		"az cost report: 3 decisions of a searched kind over 2 games, searched 2 (1.0/game), overrides 1 (50.0% of searched)",
		"ms/searched decision: mean 200.0 p50 100.0 p95 100.0; ms/simulation mean 20.000; simulations/decision mean 10.0; env steps/simulation mean 6.0",
		"  kind priority: asked 2, searched 1, overrides 0, ms mean 100.0 p95 100.0",
		"  kind attackers: asked 1, searched 1, overrides 1, ms mean 300.0 p95 300.0",
		"  kind blockers: none",
		"  kind target: none",
		"  t01-06: searched 1, ms mean 100.0 p95 100.0",
		"  t07-12: searched 1, ms mean 300.0 p95 300.0",
		"  t13+: searched 0",
		"counters: simulations 20, completed 19, chance-failures 0, panics 1, submit-errors 0, bad-worlds 0, no-world 0, all-failed 0, step-capped 1, terminal 1, expanded 17, unavailable 0, prior-fallbacks 0, skipped 1, feed-stopped 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestAZCostReportEmpty(t *testing.T) {
	saveAZ(t)
	azStats.mu.Lock()
	azStats.diags = nil
	azStats.mu.Unlock()
	if out := azCostReport(3); !strings.Contains(out, "no decisions of a searched kind") {
		t.Fatalf("empty report = %q", out)
	}
}

// End to end through the matrix path: `-a az -az-world clairvoyant -az-sims
// 2 -b bot -pairs <pair> -games 1` exits 0 and the seat reported decisions;
// a checkpoint with no value head is refused at the front door.
func TestAZPlaysPairMatrix(t *testing.T) {
	dir := corpusDirOrSkip(t)
	saveAZ(t)
	azWorldArg = "clairvoyant"
	azCfg.Search.Sims = 2
	code := mainExit("az", "bot", 1, 42, 2, 0, "mono-red-goblins:mono-blue-tempo", "constructed", "text", 0,
		200, 20000, dir, "", false, false, "", 0, 0, "", "", "", "", "")
	if code != 0 {
		t.Fatalf("az pair run exited %d", code)
	}
	azStats.mu.Lock()
	n := len(azStats.diags)
	azStats.mu.Unlock()
	if n == 0 {
		t.Fatal("the az seat reported no decision of a searched kind")
	}
	code = mainExit("az", "bot", 1, 42, 2, 0, "mono-red-goblins:mono-blue-tempo", "constructed", "text", 0,
		200, 20000, dir, "", false, false, "", 0, 0, "", "", "", "", writeZeroCheckpoint(t))
	if code == 0 {
		t.Fatal("an az side accepted a checkpoint without a value head")
	}
}
```

- [ ] **Step 2: Run them and see the failure**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestAZ' ./cmd/botbench/`
Expected: FAIL to compile: `undefined: azCfg`, `undefined: azFrontDoor`, `undefined: azCostReport`, `undefined: azStats`.

- [ ] **Step 3: Implement**

`cmd/botbench/azcost.go`:

```go
package main

// The az seat's front door and cost report (spec 2026-09-27-alphazero-mcts
// §1, §4). The clock lives here -- cmd/botbench may import time
// (internal/archtest) -- and reaches the seat only through azmcts.Millis;
// azmcts.Watch delivers one Diag per decision of a searched kind. Both are
// installed once before any game starts; nothing recorded here reaches a
// game, an event, a view or a replay.

import (
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
)

// The az policy's configuration, write-once before any game starts and
// read-only afterwards (the searchKnobs pattern). azNet is the -checkpoint
// model when one was given (nil = generation 0).
var (
	azCfg        = azmcts.DefaultSeatConfig()
	azNet        *policynet.Model
	azWorldArg   string
	azKindsArg   = "priority,attackers,blockers,target"
	azFlagsGiven bool
)

// registerAZFlags defines the -az-* flags on fs, bound to azCfg.
func registerAZFlags(fs *flag.FlagSet) {
	d := azmcts.DefaultOptions()
	fs.IntVar(&azCfg.Search.Sims, "az-sims", d.Sims, "az policy: simulations per searched decision (spec default 100; 0 plays the bot)")
	fs.StringVar(&azWorldArg, "az-world", "", "az policy, required: clairvoyant (every simulation walks a clone of the REAL engine, hidden zones and future chance included -- bench and training only). sampled arrives with ticket 5")
	fs.Float64Var(&azCfg.Search.CPUCT, "az-cpuct", d.CPUCT, "az policy: PUCT exploration constant c")
	fs.Float64Var(&azCfg.Search.FPU, "az-fpu", d.FPU, "az policy: first-play urgency (an unvisited child's Q is its parent's Q minus this)")
	fs.IntVar(&azCfg.Search.Limit, "az-candidates", d.Limit, "az policy: candidates per searched decision, the bot's answer first")
	fs.IntVar(&azCfg.Search.MaxSteps, "az-max-steps", d.MaxSteps, "az policy: environment submits per simulation before the walk stops and its leaf is evaluated")
	fs.StringVar(&azKindsArg, "az-kinds", azKindsArg, "az policy: comma list of searched decision kinds (priority, attackers, blockers, target)")
}

// azFrontDoor validates the az configuration before any game starts. It is a
// no-op without an az side, except that -az-* flags then are an error. m is
// the -checkpoint model, nil when none was given. On success it stores the
// validated config and opens the clairvoyant source for this process -- the
// only azmcts.AllowClairvoyant call outside tests.
func azFrontDoor(aName, bName string, m *policynet.Model) error {
	if aName != "az" && bName != "az" {
		if azFlagsGiven {
			return fmt.Errorf("-az-* flags were given but neither side is az")
		}
		return nil
	}
	switch azWorldArg {
	case "clairvoyant":
	case "sampled":
		return fmt.Errorf("-az-world sampled is not implemented yet (ticket 5); use -az-world clairvoyant")
	case "":
		return fmt.Errorf("policy az requires -az-world clairvoyant (the only world source implemented; sampled arrives with ticket 5)")
	default:
		return fmt.Errorf("-az-world %q: want clairvoyant or sampled", azWorldArg)
	}
	kinds, err := azmcts.ParseKinds(azKindsArg)
	if err != nil {
		return fmt.Errorf("-az-kinds: %w", err)
	}
	cfg := azCfg
	cfg.Search.Kinds = kinds
	if err := cfg.Search.Validate(m); err != nil {
		return fmt.Errorf("policy az: %w", err)
	}
	azCfg, azNet = cfg, m
	azmcts.AllowClairvoyant()
	return nil
}

var azStats struct {
	mu    sync.Mutex
	diags []azmcts.Diag
}

// installAZCostStats wires the az seat's hooks to this command's collector,
// starting from an empty collection.
func installAZCostStats() {
	azStats.mu.Lock()
	azStats.diags = nil
	azStats.mu.Unlock()
	t0 := time.Now()
	azmcts.Millis = func() float64 { return float64(time.Since(t0).Microseconds()) / 1000 }
	azmcts.Watch = func(dg azmcts.Diag) {
		azStats.mu.Lock()
		azStats.diags = append(azStats.diags, dg)
		azStats.mu.Unlock()
	}
}

func azBucket(turn int32) string {
	switch {
	case turn > 12:
		return "t13+"
	case turn > 6:
		return "t07-12"
	default:
		return "t01-06"
	}
}

func azRatio(num float64, den int) float64 {
	if den == 0 {
		return 0
	}
	return num / float64(den)
}

// azCostReport renders the per-searched-decision cost and every azmcts
// counter. totalGames is the run's game count (pairs x games in matrix
// mode). Kinds and buckets print in fixed order; no map is ranged.
func azCostReport(totalGames int) string {
	azStats.mu.Lock()
	diags := append([]azmcts.Diag(nil), azStats.diags...)
	azStats.mu.Unlock()
	var b strings.Builder
	if len(diags) == 0 {
		b.WriteString("az cost report: no decisions of a searched kind (no az seat ran, or none was asked)\n")
		return b.String()
	}
	type kindAgg struct {
		asked, searched, overrides int
		ms                         []float64
	}
	kinds := []string{"priority", "attackers", "blockers", "target"}
	byKind := make(map[string]*kindAgg, len(kinds))
	for _, k := range kinds {
		byKind[k] = &kindAgg{}
	}
	buckets := []string{"t01-06", "t07-12", "t13+"}
	byBucket := make(map[string][]float64, len(buckets))
	var total azmcts.Stats
	var searchedMS []float64
	asked, searched, overrides := 0, 0, 0
	msSum := 0.0
	for _, dg := range diags {
		total.Add(dg.Stats)
		ka := byKind[dg.Kind]
		if ka == nil {
			continue // a FeedStopped record: counted in total only
		}
		asked++
		ka.asked++
		if !dg.Searched {
			continue
		}
		searched++
		ka.searched++
		if dg.Choice != 0 {
			overrides++
			ka.overrides++
		}
		ka.ms = append(ka.ms, dg.MS)
		searchedMS = append(searchedMS, dg.MS)
		msSum += dg.MS
		bk := azBucket(dg.Turn)
		byBucket[bk] = append(byBucket[bk], dg.MS)
	}
	fmt.Fprintf(&b, "az cost report: %d decisions of a searched kind over %d games, searched %d (%.1f/game), overrides %d (%.1f%% of searched)\n",
		asked, totalGames, searched, azRatio(float64(searched), totalGames), overrides, 100*azRatio(float64(overrides), searched))
	fmt.Fprintf(&b, "ms/searched decision: mean %.1f p50 %.1f p95 %.1f; ms/simulation mean %.3f; simulations/decision mean %.1f; env steps/simulation mean %.1f\n",
		meanF(searchedMS), quantF(searchedMS, .5), quantF(searchedMS, .95),
		azRatio(msSum, total.Simulations), azRatio(float64(total.Simulations), searched), azRatio(float64(total.EnvSteps), total.Simulations))
	for _, k := range kinds {
		ka := byKind[k]
		if ka.asked == 0 {
			fmt.Fprintf(&b, "  kind %s: none\n", k)
			continue
		}
		fmt.Fprintf(&b, "  kind %s: asked %d, searched %d, overrides %d, ms mean %.1f p95 %.1f\n",
			k, ka.asked, ka.searched, ka.overrides, meanF(ka.ms), quantF(ka.ms, .95))
	}
	for _, bk := range buckets {
		ms := byBucket[bk]
		if len(ms) == 0 {
			fmt.Fprintf(&b, "  %s: searched 0\n", bk)
			continue
		}
		fmt.Fprintf(&b, "  %s: searched %d, ms mean %.1f p95 %.1f\n", bk, len(ms), meanF(ms), quantF(ms, .95))
	}
	fmt.Fprintf(&b, "counters: simulations %d, completed %d, chance-failures %d, panics %d, submit-errors %d, bad-worlds %d, no-world %d, all-failed %d, step-capped %d, terminal %d, expanded %d, unavailable %d, prior-fallbacks %d, skipped %d, feed-stopped %d\n",
		total.Simulations, total.Completed, total.ChanceFailures, total.Panics, total.SubmitErrors, total.BadWorlds, total.NoWorld,
		total.AllFailed, total.StepCapped, total.Terminal, total.Expanded, total.Unavailable, total.PriorFallbacks, total.Skipped, total.FeedStopped)
	return b.String()
}
```

`cmd/botbench/main.go` edits (use exact-match replacement for each):

1. Import block: immediately before the line `	gbench "github.com/adams-shaun/gorge/internal/bench"`, add the line `	"github.com/adams-shaun/gorge/internal/azmcts"` (it sorts before `internal/bench`). Then run `gofmt -l cmd/botbench/`, which must print nothing.

2. `policies` map. Replace

```go
	"search": func(seed uint64) seat.Seat {
		return searchseat.NewSearchBot(seed, searchKnobs)
	},
}
```

with

```go
	"search": func(seed uint64) seat.Seat {
		return searchseat.NewSearchBot(seed, searchKnobs)
	},
	// az is the AlphaZero-style MCTS seat (internal/azmcts, spec
	// 2026-09-27): a PUCT tree over the seat's own searched decisions whose
	// leaf is the -checkpoint value head (no checkpoint = generation 0: a
	// uniform prior and the frozen heuristic leaf). Like search, it answers
	// from the driver's engine feed (internal/bench.PlayGame's
	// searchseat.SearchSeat branch). -az-world clairvoyant searches clones
	// of the REAL engine, so it is bench and training only: azFrontDoor is
	// the only azmcts.AllowClairvoyant caller, host.NormalizeBotPolicy does
	// not know the name, and internal/archtest forbids host, host/httpapi
	// and cmd/gorged from linking azmcts at all.
	"az": func(seed uint64) seat.Seat {
		s, err := azmcts.NewSeat(seed, azNet, azCfg)
		if err != nil {
			panic("botbench: " + err.Error()) // validated by azFrontDoor before any game
		}
		return s
	},
}
```

3. `parseOppMix`. Replace `		if _, ok := policies[name]; !ok || name == "policynet" || name == "search" {` with `		if _, ok := policies[name]; !ok || name == "policynet" || name == "search" || name == "az" {`.

4. `main()`. Replace

```go
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "policynet-kinds" {
			policynetKindsGiven = true
		}
	})
```

with

```go
	registerAZFlags(flag.CommandLine)
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "policynet-kinds" {
			policynetKindsGiven = true
		}
		if strings.HasPrefix(f.Name, "az-") {
			azFlagsGiven = true
		}
	})
```

5. `mainExit` checkpoint gate. Replace

```go
	if !policynetSide && checkpoint != "" {
		return fail(fmt.Errorf("-checkpoint was given but neither side is policynet"))
	}
```

with

```go
	// -checkpoint also feeds an az side: its value head is the search's leaf
	// and its policy head the prior (azFrontDoor refuses a checkpoint with
	// no value head). A package-level policynetModel left by an earlier
	// in-process run must never reach az, so only this run's checkpoint is
	// passed on.
	azSide := aName == "az" || bName == "az"
	if !policynetSide && !azSide && checkpoint != "" {
		return fail(fmt.Errorf("-checkpoint was given but neither side is policynet or az"))
	}
	var ckModel *policynet.Model
	if checkpoint != "" {
		ckModel = policynetModel
	}
	if err := azFrontDoor(aName, bName, ckModel); err != nil {
		return fail(err)
	}
```

6. Hook install. Replace

```go
	if searchSide {
		installSearchCostStats()
	}
```

with

```go
	if searchSide {
		installSearchCostStats()
	}
	if azSide {
		installAZCostStats()
	}
```

7. Matrix report. Replace

```go
		if searchSide {
			fmt.Fprint(os.Stdout, searchCostReport(games*len(ps)))
		}
```

with

```go
		if searchSide {
			fmt.Fprint(os.Stdout, searchCostReport(games*len(ps)))
		}
		if azSide {
			fmt.Fprint(os.Stdout, azCostReport(games*len(ps)))
		}
```

8. Single-run report. Replace

```go
	if searchSide {
		fmt.Fprint(os.Stdout, searchCostReport(games))
	}
	return 0
}
```

with

```go
	if searchSide {
		fmt.Fprint(os.Stdout, searchCostReport(games))
	}
	if azSide {
		fmt.Fprint(os.Stdout, azCostReport(games))
	}
	return 0
}
```

- [ ] **Step 4: Run and pass**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -v -run 'TestAZ|TestPolicynetCheckpointFlagValidation|TestSeatCtorForDeck|TestDeckPolicyShadowCheck|TestSearchCostReport' ./cmd/botbench/ 2>&1 | tail -30`
Expected: PASS; `TestAZPlaysPairMatrix` reports `--- PASS`, not SKIP.
Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go vet ./cmd/botbench/`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add cmd/botbench/azcost.go cmd/botbench/azcost_test.go cmd/botbench/main.go
git commit -m "feat(botbench): -a az, the AlphaZero-style search seat

-az-sims N -az-world clairvoyant [-checkpoint ck] seats azmcts.Seat through
the same driver feed the search seat uses. The front door requires an
explicit world (sampled is refused until ticket 5), refuses a checkpoint
without a value head or with an oracle feature set, and is the only place
the clairvoyant source is opened. A run with an az side appends the
per-searched-decision cost and every azmcts fallback counter; any other run
prints exactly what it printed before."
```

---

## Task 8: Clairvoyant refusal outside the bench

**Files:**
- Create: `host/bot_policy_az_test.go`
- Modify: `host/httpapi/game_test.go` (the loop at `:140`), `internal/archtest/arch_test.go` (the `forbidden` list ending at `:138-139`)

**Interfaces:**
- Consumes: `host.NormalizeBotPolicy` (`host/bot_policy.go:27`), `NewBotPolicySeat` (`:42`), `NewBotPolicySeatWithAutoPayMana` (`:67`); `TestCreateGameBotPolicyDecodeAndRejectsDiagnostic` (`host/httpapi/game_test.go:119`); `TestDependencyOrderHolds` (`internal/archtest/arch_test.go:125`).
- Produces: `func TestHostedVocabularyRefusesSearchPolicies(t *testing.T)`; three new forbidden dependency arrows.

This task pins an existing property. The hosted vocabulary is already closed (`host/bot_policy.go:27-36`), so the new tests pass on first run, and Step 2 proves each one bites with a temporary mutation that is then reverted.

- [ ] **Step 1: Write the tests**

`host/bot_policy_az_test.go`:

```go
package host

import "testing"

// The search policies read the engine a live table must never hand a seat
// (az's clairvoyant world clones the REAL engine, spec 2026-09-27 §1), so
// none of them may become a hosted opponent under any spelling of the
// factory. internal/archtest additionally forbids host from linking
// internal/azmcts at all.
func TestHostedVocabularyRefusesSearchPolicies(t *testing.T) {
	for _, name := range []string{"az", "search", "policynet"} {
		if _, err := NormalizeBotPolicy(name); err == nil {
			t.Errorf("NormalizeBotPolicy(%q) accepted a bench-only search policy", name)
		}
		if _, err := NewBotPolicySeat(name, 1); err == nil {
			t.Errorf("NewBotPolicySeat(%q) built a seat", name)
		}
		if _, err := NewBotPolicySeatWithAutoPayMana(name, 1, true); err == nil {
			t.Errorf("NewBotPolicySeatWithAutoPayMana(%q) built a seat", name)
		}
	}
}
```

In `host/httpapi/game_test.go`, replace
`	for _, body := range []string{`{"bot_policy":"legacy"}`, `{"bot_policy":"random"}`} {`
with
`	for _, body := range []string{`{"bot_policy":"legacy"}`, `{"bot_policy":"random"}`, `{"bot_policy":"az"}`} {`

In `internal/archtest/arch_test.go`, replace

```go
		{module + "/deck", module + "/rules"},
	}
```

with

```go
		{module + "/deck", module + "/rules"},
		// The az search seat's clairvoyant world clones the REAL engine,
		// hidden zones and future chance included (spec 2026-09-27 §1): it
		// is bench and training only, so nothing that seats a non-bench
		// opponent may link it.
		{module + "/host", module + "/internal/azmcts"},
		{module + "/host/httpapi", module + "/internal/azmcts"},
		{module + "/cmd/gorged", module + "/internal/azmcts"},
	}
```

- [ ] **Step 2: Run the tests, then prove they bite**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHostedVocabularyRefusesSearchPolicies|TestNormalizeBotPolicy' ./host/`
Expected: PASS.

Mutation check (temporary): in `host/bot_policy.go`, change `case BotPolicy, LethalPressurePolicy, CastProfilePolicy:` to `case BotPolicy, LethalPressurePolicy, CastProfilePolicy, "az":` and rerun the same command.
Expected: FAIL: `NormalizeBotPolicy("az") accepted a bench-only search policy`.
Revert it and confirm with `git diff --exit-code host/bot_policy.go`, which must exit 0 and print nothing.

- [ ] **Step 3: Run the other two pins**

Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestCreateGameBotPolicyDecodeAndRejectsDiagnostic' ./host/httpapi/`
Expected: PASS (`az` is a 400 `bad_request` and never reaches the builder).
Run: `systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestDependencyOrderHolds|TestTimeIsImportedOnlyByTheHost|TestNoLegacyMathRand' ./internal/archtest/`
Expected: PASS. `internal/azmcts` imports no `time` and no `math/rand`, and nothing forbidden links it.

- [ ] **Step 4: Commit**

```bash
git add host/bot_policy_az_test.go host/httpapi/game_test.go internal/archtest/arch_test.go
git commit -m "test: pin that the clairvoyant az seat cannot reach a hosted table

The hosted policy vocabulary already refuses az, search and policynet; pin
it at the factory and at POST /api/games, and forbid host, host/httpapi and
cmd/gorged from linking internal/azmcts at all."
```

---

## Task 9: Stage 0 measurement procedure (written here, run later by the operator)

**Files:**
- Create: `scripts/az-stage0.sh`

**Interfaces:**
- Consumes: the `botbench` flags from Task 7; `BenchmarkSearchDecisionEarly`/`Late` from Task 6; the botbench matrix text lines `pooled az win rate: ...` / `pooled bot win rate: ...` (`cmd/botbench/main.go:1595`, `writeMatrixText`) and the az report lines from Task 7.
- Produces: `scripts/az-stage0.sh`. It writes `commit.txt`, `bench.txt`, `rss-azmcts.txt`, `rss-botbench.txt`, `smoke.{txt,time}`, `control.{txt,time}`, `az25.{txt,time}`, `az100.{txt,time}` and `summary.txt` under `$OUT`.

The implementer does NOT run the script; the operator does, as the one heavy job on the machine. The implementer verifies syntax only.

- [ ] **Step 1: Write the script**

`scripts/az-stage0.sh`:

```bash
#!/usr/bin/env bash
# Stage 0 of the AlphaZero-style MCTS effort
# (docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md §3): measure
# the generation-0 clairvoyant az seat -- no network, uniform prior,
# heuristic leaf -- for cost and strength against bot, BEFORE any training
# code (tickets 3-5) is built. It trains nothing and commits nothing.
#
# Resource rules (spec §3): this is THE one heavy job on the machine -- do
# not start it while another go test, botbench, sweep or training run is
# live. Every heavy step runs in a MemoryMax scope with GOMEMLIMIT=1GiB,
# pinned to two cores, and writes only under $OUT.
#
# Usage (from a checkout at the commit to measure, .cards fetched):
#   scripts/az-stage0.sh
# Overrides: OUT CORES MEM SEED GAMES_PER_PAIR SIMS_ARMS
# Results: fill docs/superpowers/reports/<date>-az-stage0.md from the
# "Stage 0 report template" section of
# docs/superpowers/plans/2026-09-27-alphazero-mcts.md.
set -euo pipefail

REPO=$(git rev-parse --show-toplevel)
OUT=${OUT:-/mnt/sata/gorge-training/az/stage0}
CORES=${CORES:-12,28}
MEM=${MEM:-5G}
SEED=${SEED:-90000000}
GAMES_PER_PAIR=${GAMES_PER_PAIR:-40} # x 5 pairs = 200 games per arm
SIMS_ARMS=${SIMS_ARMS:-"25 100"}
PAIRS=uw-tempo:mono-white-equipment,uw-tempo:mono-blue-tempo,uw-tempo:mono-black-aggro,uw-tempo:mono-red-prowess,uw-tempo:mono-green-stompy
NPAIRS=5

if [ ! -d "$REPO/.cards" ]; then
	echo "az-stage0: $REPO/.cards is missing; run make fetch-cards compile-cards first" >&2
	exit 1
fi
mkdir -p "$OUT/gotmp"
export GOTMPDIR="$OUT/gotmp"

# heavy runs one command in the capped scope, pinned to $CORES.
heavy() {
	systemd-run --user --scope -q -p MemoryMax="$MEM" \
		env GOMEMLIMIT=1GiB GOTMPDIR="$GOTMPDIR" taskset -c "$CORES" "$@"
}

cd "$REPO"
{
	echo "commit $(git rev-parse HEAD)"
	echo "forge_ref $(/usr/bin/grep -E '^FORGE_REF' Makefile | head -1)"
	echo "dirty $(git status --porcelain | wc -l) path(s)"
} | tee "$OUT/commit.txt"

# 1. Build once.
heavy go build -o "$OUT/botbench" ./cmd/botbench

# 2. Per-simulation and per-searched-decision cost (spec §3, §4): ns/sim and
#    allocs per searched decision, early and late positions.
heavy go test -p 1 -count=1 -run '^$' -bench 'BenchmarkSearchDecision' -benchmem -benchtime 20x \
	./internal/azmcts/ | tee "$OUT/bench.txt"

# 3. Peak RSS of the new test binaries (spec §4: a multi-GB test binary is a bug).
heavy go test -p 1 -count=1 -exec '/usr/bin/time -f peak-rss-kb=%M' ./internal/azmcts/ 2>&1 | tee "$OUT/rss-azmcts.txt"
heavy go test -p 1 -count=1 -run 'TestAZ' -exec '/usr/bin/time -f peak-rss-kb=%M' ./cmd/botbench/ 2>&1 | tee "$OUT/rss-botbench.txt"

# arm NAME GAMES_PER_PAIR BOTBENCH_ARGS... -- one matrix run on the eval block.
arm() {
	local name=$1 games=$2
	shift 2
	heavy /usr/bin/time -f "wall_s=%e peak_rss_kb=%M" -o "$OUT/$name.time" \
		"$OUT/botbench" -pairs "$PAIRS" -games "$games" -seed "$SEED" -workers 2 -dir "$REPO/.cards" "$@" \
		> "$OUT/$name.txt"
	echo "== $name: $(cat "$OUT/$name.time")"
	/usr/bin/grep -E '^pooled .* win rate|^az cost report|^ms/searched|^counters' "$OUT/$name.txt" || true
}

# 4. Smoke: one game per pair at the cheaper setting, so a broken seat fails
#    in minutes, not after hours.
arm smoke 1 -a az -b bot -az-world clairvoyant -az-sims 25

# 5. The arms: control (bot vs bot, same block) and gen-0 az at each budget.
arm control "$GAMES_PER_PAIR" -a bot -b bot
for sims in $SIMS_ARMS; do
	arm "az$sims" "$GAMES_PER_PAIR" -a az -b bot -az-world clairvoyant -az-sims "$sims"
done

# 6. Throughput: games per hour on the two granted cores.
{
	for name in control $(for s in $SIMS_ARMS; do echo "az$s"; done); do
		wall=$(sed -n 's/^wall_s=\([0-9.]*\).*/\1/p' "$OUT/$name.time")
		awk -v n="$name" -v g=$((GAMES_PER_PAIR * NPAIRS)) -v w="$wall" \
			'BEGIN { printf "%s: %d games in %.0f s = %.1f games/h (2 cores)\n", n, g, w, g * 3600 / w }'
	done
} | tee "$OUT/summary.txt"
echo "az-stage0: done; fill the Stage 0 report from $OUT"
```

- [ ] **Step 2: Check syntax (do not run it)**

Run: `chmod +x scripts/az-stage0.sh && bash -n scripts/az-stage0.sh && echo syntax-ok`
Expected: `syntax-ok`.
Run: `/usr/bin/grep -cE '^[[:space:]]*heavy (go|/usr/bin/time) ' scripts/az-stage0.sh`
Expected: `5` (build, bench, two RSS runs, and the one inside `arm`). Every heavy command goes through `heavy`.

- [ ] **Step 3: Final verification of tickets 1–2 (one package at a time)**

Run each in turn, and wait for each to finish before starting the next:

```bash
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/azmcts/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/policynet/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/searchseat/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/searchprobe/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./cmd/botbench/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./host/ ./host/httpapi/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 ./internal/archtest/
systemd-run --user --scope -q -p MemoryMax=4G env GOMEMLIMIT=1GiB go test -p 1 -count=1 -run 'TestHeads' ./rules/
```

Expected: every package `ok`. `TestHeads` passing shows the default `bot` and the chain goldens are unchanged. Any red package is this branch's to fix before the operator merges; a failure that also occurs on `main` must still be bisected (AGENTS.md).

- [ ] **Step 4: Commit**

```bash
git add scripts/az-stage0.sh
git commit -m "chore(az): the Stage 0 measurement procedure

scripts/az-stage0.sh measures gen-0 clairvoyant az against bot on the eval
block (seed 90,000,000, uw-tempo vs the five mono decks, 200 games per arm
at 25 and 100 simulations plus a bot-vs-bot control), the per-simulation
benchmark and test-binary peak RSS, inside the 5G / GOMEMLIMIT 1GiB / two
core envelope with all output under /mnt/sata/gorge-training/az/stage0."
```

### Stage 0 report template

This is the file the operator fills after running `scripts/az-stage0.sh`: `docs/superpowers/reports/2026-09-2X-az-stage0.md`, with `X` the run date.

```markdown
# AZ stage 0: gen-0 clairvoyant MCTS vs bot (2026-09-2X)

Spec: docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md §3 (Stage 0).
Plan: docs/superpowers/plans/2026-09-27-alphazero-mcts.md (tickets 1–2).
Commit measured: <commit.txt: commit>. Corpus pin: <commit.txt: forge_ref>. Dirty paths: <n>.

## Setup

- Decks: uw-tempo vs mono-white-equipment, mono-blue-tempo, mono-black-aggro,
  mono-red-prowess, mono-green-stompy; seats traded every game.
- Eval block: seed 90,000,000; 40 games per pair = 200 games per arm.
- Arms: control (`-a bot -b bot`), az25 and az100 (`-a az -b bot -az-world
  clairvoyant -az-sims 25|100`), gen 0 (no checkpoint: uniform prior,
  searchprobe.LeafValue leaf), every other knob at its default (c 1.5, FPU 0.1,
  6 candidates, 1000 env steps, kinds priority/attackers/blockers/target).
- Resources: systemd-run --user --scope -p MemoryMax=5G, GOMEMLIMIT=1GiB,
  taskset -c 12,28, -workers 2; output /mnt/sata/gorge-training/az/stage0/.
- Command: scripts/az-stage0.sh (unmodified | overrides: ...).

## Cost

| measure | az25 | az100 | source |
|---|---|---|---|
| ns per simulation, early position (turn ≥ 3) | <bench.txt Early ns/sim> | — | bench.txt |
| ns per simulation, late position (turn ≥ 9) | <bench.txt Late ns/sim> | — | bench.txt |
| B/op, allocs/op per searched decision (25 sims) early / late | | — | bench.txt |
| ms per searched decision, mean / p50 / p95 | | | azNN.txt "ms/searched decision" |
| ms per simulation in play | | | azNN.txt "ms/simulation mean" |
| simulations per searched decision | | | azNN.txt |
| env steps per simulation | | | azNN.txt |
| searched decisions per game | | | azNN.txt "searched N (x/game)" |
| games/h on 2 cores (control: <x>) | | | summary.txt |
| botbench peak RSS (KB) | | | azNN.time |
| test-binary peak RSS (KB): azmcts / botbench | | | rss-*.txt |

Does the event-log copy after Clone bind? Late ns/sim ÷ early ns/sim = <r>.
(Spec "Known cost risk": a persistent log prefix is in scope only if this binds.)

## Strength (200 games per arm; ±~7pp, a pipeline check, not a verdict)

| arm | A wins | B wins | draws | stalls | pooled A win rate (95% CI) | pairs A loses / wins / undecided |
|---|---|---|---|---|---|---|
| control (A = bot) | | | | | | |
| az25 (A = az) | | | | | | |
| az100 (A = az) | | | | | | |

Per pair, az100: paste the matrix table from az100.txt.

## Counters (spec §4: every fallback, never silent)

| counter | az25 | az100 |
|---|---|---|
| simulations / completed | | |
| chance-failures, panics, submit-errors | | |
| bad-worlds, no-world, all-failed | | |
| step-capped, terminal, expanded | | |
| unavailable, prior-fallbacks | | |
| skipped, feed-stopped | | |
| per kind asked / searched / overrides: priority | | |
| attackers | | |
| blockers | | |
| target | | |

## Kill criterion (spec §3)

"If full-information search with no network cannot beat bot, the candidates or
the leaf are broken; diagnose before any training."

Operationalised (controller decision 2026-09-27; operator may override): KILL if no az arm's pooled
win-rate 95% CI lies entirely above the same-seed bot-vs-bot control arm's pooled
win rate.

Verdict: PASS | KILL. <one sentence with the numbers>.

If KILL, diagnose before tickets 3–5, in this order:
1. Skipped share per kind: are candidates being built at all?
2. Override share: does the search ever leave the bot's answer?
3. Step-capped share of completed simulations: are leaves mostly capped mid-walk?
4. -az-kinds subsets (attackers alone, priority alone) on one pair: which kind loses?
5. -az-sims 400 on one pair: does strength rise with budget (tree) or not (leaf)?
6. The heuristic leaf's scale (searchprobe.LeafScore /20): value spread at decision points.

## Recommendation for tickets 3–5

- Simulation budget for generation, from games/h: <n> sims → <x> games/h →
  <h> hours per 100–200-game generation on 2 cores.
- Anything in the counters that must be fixed first.
```

---

## Deferred to plan 2, after the stage-0 checkpoint

Spec §3 makes Stage 0 a checkpoint with the operator: its numbers come back before any of the following is built.

- **Ticket 3:** `cmd/azgen` (plays the `az` seat, with `SeatConfig.Explore`, against `bot` over a seed block and writes a JSONL visit corpus: redacted state and option features, per-candidate visits and π, `root_value`, the outcome filled at game end), `cmd/policytrain -loss visits` (soft cross-entropy against π, with subset targets spread onto `policynet.CandidateScore`'s Bernoulli form, and residual 0), and the TD-blended value target `λ·outcome + (1−λ)·root_value` with λ scheduled 0.95, 0.92, 0.85, 0.70.
- **Ticket 4:** `cmd/exitloop -mode az`: per generation, run azgen, then policytrain over a sliding window of the last 5 corpora, then the four readouts, then the gate. It includes the per-generation summary table.
- **Ticket 5:** the `Sampled` world source (`searchprobe.Sample` worlds drawn once per decision, the pn21 `RedealBase` fallback, simulations round-robin over worlds with a `CloneHypothetical`/`Clone` per simulation and `World.Hypothetical` set), the ISMCTS availability rule across worlds, `-az-world sampled`, and the leak test. The core already counts availability and new keys (`RunTree`), but ticket 5 must revisit identity: `ObserveDecision` assigns refs in introduction order, so the same object may get a different ref in different sampled worlds. The root observer must also then be a clone of the feed's collector, never the collector itself.

---

## Self-review

**Spec coverage (tickets 1–2):**

| Spec requirement | Task |
|---|---|
| §1 `internal/azmcts`, pure search, no I/O; `Search(...) (Result, error)` returning Candidates, Visits, Prior, RootValue, Choice, Stats | 3, 5 (the signature gains `Root`; see the false-statements table) |
| §1 `WorldSource`; `Clairvoyant` = `Clone()` of the real engine, bench/training only | 5 (`NewClairvoyant`, `AllowClairvoyant`), 7 (front door), 8 (refusal pins) |
| §1 Env step: bot answers everything until the next searched decision, game over or the step cap; hypothetical worlds via `SubmitHypothetical` | 5 (`engineEnv.advance`, `submit`, `TestSearchHypotheticalWorlds`) |
| §1 Searched kinds and candidates via the searchprobe enumerators; one candidate is not searched | 4 (`enumerate`, `TestEnumerateOneOrZeroCandidates`) |
| §1 Semantic action identity (`Collector.Actions`/`Match`) | 2 (`ObserveDecision`), 4 (`actionsKey`) |
| §1 Leaf: `Model.Value` on the redacted view; 1/0 at game over; gen 0 = `LeafValue` and a uniform prior | 5 (`leafValue`, `Leaf`), 4 (`priors`), 5 (`TestSearchWithANetworkLeafAndPrior`) |
| §1 Seat `az` in botbench: `-a az -az-sims N -az-world clairvoyant -checkpoint`; the driver feeds engine and history as for L10 | 6 (`Seat` as a `searchseat.SearchSeat`), 7 |
| §1 `-az-world clairvoyant` refused wherever a non-bench opponent could be seated | 5, 7, 8 |
| §2 Single perspective; PUCT with c 1.5; FPU parent Q − 0.1 | 3 (`selectEdge`, `TestFirstPlayUrgency`, `TestPUCTScore`) |
| §2 Simulation: one clone per simulation, walk, expand one node, evaluate, back up, no rollouts | 3 (`simulate`, `TestBackupAlongPath`), 5 |
| §2 Priors: softmax of option scores for priority/target, `candidateScore` subsets for attackers/blockers, uniform at gen 0, candidate 0 = bot wins ties | 1, 4, 3 (`TestTiesGoToCandidateZero`) |
| §2 Availability counts (`N_avail`) | 3 (`TestAvailabilityCountsOnlyOfferingWorlds`, `TestNewKeyFromALaterWorld`) |
| §2 Budget `-az-sims` default 100; per-simulation step cap; no wall clock | 3 (`DefaultOptions`), 5, 7 |
| §2 Dirichlet noise (α 0.3, ε 0.25) and τ = 1 sampling on turns 1–4, generation only; eval argmax | 3 (`noisyPrior`, `choose`, tests), 6 (`SeatConfig.Explore`) |
| §2 Determinism: RNG from (game seed, decision index), sequential simulations, byte-identical | 5 (`DecisionSeed`, `TestSearchIsDeterministic`), 6 (`TestSeatSearchesAndReplaysExactly`) |
| §2 Known cost risk measured per simulation | 6 (`BenchmarkSearchDecisionEarly`/`Late`), 9 |
| §3 Stage 0: cost per simulation and per searched decision; games/h at 25/100 sims on 2 cores; gen-0 vs bot, 200 games per arm; kill; numbers to the operator | 7 (report), 9 (script, template, kill) |
| §3 Resource rules | Global Constraints, 9 |
| §4 Tests: fake env; one real-deck integration; determinism; clairvoyant refusal; opt-in only; memory benchmark and peak RSS | 3; 5; 5–6; 5, 6, 8; 1 (bit-identical prior), 6 (0 sims = bot), 9 Step 3 (`TestHeads`); 6 Step 4, 9 |
| §4 Failure table: chance failure, candidate not mappable, every simulation failed or no world, step cap | 3 (`classify`, `TestEveryFailureKindIsCounted`, `TestStepCapOnEverySimulation`), 5 (`TestSearchBadWorldPlaysTheBot`, `TestSubmitRecoversAnEnginePanic`), 7 (printed) |

The §4 leak test (stage 2) belongs to ticket 5 and is deferred.

**Placeholder scan:** no TBD, TODO or "similar to Task N". Each code step carries the complete file or an exact old/new replacement. The report template's `<...>` fields are the operator's to fill after the run; they are not plan placeholders.

**Type and name consistency:** `Key`, `Point`, `Leaf`, `Env`, `EnvSource`, `TreeResult`, `RunTree`, `Options`, `Kinds`, `Stats` and the five error sentinels are defined in Task 3 and used unchanged in Tasks 4–7. `cand{acts, key, in}`, `enumerate(obs, e, d, bot, kinds, limit)` and `priors(net, e, d, bot, kind, cands)` are defined in Task 4 and used in Tasks 5–6. `World`, `WorldSource`, `NewClairvoyant`, `AllowClairvoyant`, `ErrClairvoyantRefused`, `Root`, `Result`, `Search`, `Options.Validate` and `DecisionSeed` come from Task 5 and are used in Tasks 6–7. `SeatConfig`, `DefaultSeatConfig`, `Seat`, `NewSeat`, `Diag`, `Watch` and `Millis` come from Task 6 and are used in Task 7. The test helpers `allowClairvoyantForTest(testing.TB)`, `testConfig`, `findPosition`, `botPosition`, `searchAt` and `testSeed` are defined in Task 5 and reused in Task 6. `passOrAbility` (Task 4 test) is reused by the Task 5 `world_test.go`. Every `Stats` field appears in `Add`, in the botbench counters line, and in the report test.

**Review Focus tests present:** (1) `TestEnumerateRefusesAPaymentIntent` in Task 4 and `TestSearchSkipsWhenTheBotPlaysALand` in Task 5. (2) `TestBackupAlongPath` in Task 3 and the `checkResult` invariant in Task 5. (3) `TestSearchBadWorldPlaysTheBot` in Task 5. (4) `TestEnumerateOneOrZeroCandidates` in Task 4 and `TestSearchSkipsWhenTheBotPlaysALand` in Task 5. (5) `TestStepCapOnEverySimulation` in Task 3 and `TestSearchStepCapBoundsEveryWalk` in Task 5. Panic recovery: `TestSubmitRecoversAnEnginePanic` in Task 5.
