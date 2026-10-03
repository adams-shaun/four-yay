# Repo map — the engine chain

Part of the [repo map](repo-map.md) index. See also:
[invariants.md](invariants.md) (the rules every change must keep) and
[do-not.md](do-not.md) (the mistakes that have actually happened here).

## The engine chain

The import direction is one-way. `internal/archtest`'s
`TestDependencyOrderHolds` pins the arrows that must never appear (see
[invariants.md](invariants.md#1-dependency-direction)).

| Package | What it owns |
|---|---|
| `cards` | Compiles Forge card scripts into the card IR; keyword expanders (`cards/kw_*.go`); `cards.Registry`, coverage. `cards.OpenCorpusFor` (`cards/subset.go`) opens only a known card pool's cards from the per-card segment file, falling back to the whole corpus when the pool reads the name universe; botbench's deck and `-spellbench` modes use it. `cards.CompilerFingerprint` hashes every non-test `.go` file here, so **any edit in `cards/` invalidates every IR cache** and forces a recompile. |
| `cards/oracletext` | Printed-card (Oracle) view for the Oracle audit. A subpackage on purpose so it does not move the fingerprint. |
| `state` | Objects, zones, players, `state.Game`, the `ContinuousEffect` vocabulary. Plain data; mutated only by `events.Apply`. |
| `decision` | `Decision` / `Option` / `Intent`, and the closed set of `decision.Kind`s (`decision.Kinds`). The whole engine↔seat contract. |
| `deck` | `{name, count}` deck-list JSON resolved against a `cards.Registry`. |
| `botpolicy` | The one heuristic bot policy (`botpolicy.Decide`), in one copy, shared by `seat` and rules' own fuzz driver. Reads a `Board`, never a view or an engine. Cast profiles in `botpolicy/profiles/`. |
| `bots` | Registry of hosted bot policies (`bots/registry.go`); built-ins register from `bots/bot`, `bots/lethalpressure` and `bots/castprofile`, linked together by `bots/all`. |
| `events` | The event union, `events.Apply` (the only state mutator), the log, the sha256 hash chain. `events.Kind` is append-only. |
| `effects` | `api:` primitive implementations, filter/count/value evaluators. Talks to the engine only through the small `effects.Host` interface; never imports `rules`. |
| `rules` | The engine: turn structure, priority, stack, combat, SBAs, layers, casting and payment, and the `kw:`/`trig:`/`stat:`/`repl:` primitives. `rules.New`, `Pending`, `Submit`, `Advance`, `Clone`. `rules.NewStaged` (`staging.go`) builds a hypothetical engine at a described position. Also holds the coverage ratchets (`rules/*_test.go`) and the golden heads (`rules/testdata/heads/`, pinned by `TestHeads`). |
| `rules/combat` | Attack/block legality (W5 step E5): `CanAttack`/`CanAttackPair`/`CanBlock`, the CR 508.1d requirement set (`AttackRequirements`), goad, the CantAttack/CantBlock/CantBlockBy/CanAttackDefender restriction walks, hidden-keyword grants, AttackRestrict and MinMaxBlocker bounds, MustBlock duties. Reads the game only through `combat.Board`, which `rules/combat_board.go` implements; never imports `rules`. The declaration flow (asks, validators, attack/block charges, events) stays in `rules/combat.go` and `rules/attack_cost.go`. A new legality rule goes here; a new Board method fails `TestCombatBoardOnlyShrinks`. |
| `view` | Projects one seat's redacted `view.View` (`Project`, `ProjectFor`, `RedactEvents`). The only way a client reads state. `view/view.go` is the orchestration (`Project`/`project`, `View`, the shared text helpers); the card/battlefield projection family lives in `view/card.go` (`CardView`, `cardViews`, `cardView`) and the stack family in `view/stack.go` (`StackView`, `stackViews`). A projection change goes in the family file for its seam, not in `view/view.go` — the two families were interleaved in one file and two projection tickets collided on it (split out in `97655ff0b`). |
| `seat` | Who answers decisions: `seat.Seat` (+ `BoardSeat`, `PaymentPlanConsumer`), `NewBot`, `NewAttackSimBot`, `NewPolicyNetBot`, explore bot. Handed a view, never an engine. |
| `replay` | Re-executes `(Config, Log)` and names the first divergent event (`Replay`, `ReplayTo`). |
| `protocol` | Versioned wire types for host↔client. Types only; never imports `rules`. |
| `host` | Tables (one goroutine each), sessions, snapshots, persistence, undo, the hosted bot-policy vocabulary (`host/bot_policy.go`). Its only clock is an injected sleep. |
| `host/httpapi` | `net/http` JSON + SSE + the embedded web client. Every seat route goes through `claimForTable`. |
