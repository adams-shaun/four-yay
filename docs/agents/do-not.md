# What NOT to do

Every item here is something that has actually gone wrong in this repo.
Several sessions and agent seats share one machine and one `main` checkout,
so most of these hurt someone other than you.

## Git and the shared checkout

- **Don't `git checkout`, `git switch`, `git reset` or bare `git stash` in the
  main checkout.** A working tree has one HEAD, and other sessions are using
  it. The stash stack is shared across every worktree. If you must stash, use
  `git stash push -u -m <unique-tag>` and apply it by SHA.
- **Don't `git checkout` inside another worktree.** It discards that seat's
  uncommitted work. Back it up with `cp`, or make a WIP commit first.
- **Don't create worktrees with a bare `git worktree add`.** Use
  `scripts/agent-worktree.sh`, which links `.cards/`. Without the corpus, tests
  skip and read green.
- **Don't `git add -A`.** Stage explicit paths.
- **Don't leave a worktree behind after its merge.** Stale worktrees double
  every `git grep` and `find` hit.
- **Don't add `Co-Authored-By:`, "Generated with …", `Ref:` or issue-tracker
  references to commits.** Hooks reject them. This is a repo rule, and it
  overrides harness defaults.

## Engine code

- **Don't write a `state.Game` field outside `events`.** That is a replay bug,
  even when every test passes today.
- **Don't insert an `events.Kind` between existing kinds,** and don't reorder
  kinds. Append only.
- **Don't add a `decision.Kind`** without an operator decision.
- **Don't import `rules` from `effects`.** If `effects.Host` is not enough,
  the work belongs in `rules`.
- **Don't read the wall clock, use `math/rand` (v1) or unseeded randomness,
  or range over a map on a path that reaches events, option order, traces or
  checkpoints.**
- **Don't add a Go dependency or cgo.** `go.mod` has no `require` lines, and
  it stays that way.
- **Don't use maps or per-call string parsing on hot paths.** Use bitsets and
  dense slices, and compile strings to masks once.
- **Don't hand-edit `web/src/protocol.ts`.** Run `make gentypes`.
- **Don't edit `cards/` casually.** Any change moves `CompilerFingerprint` and
  forces every IR cache to recompile. `cards/oracletext` is exempt.

## Licensing

- **Don't commit Forge card or token scripts.** That includes as fixtures,
  pasted script lines in tests or docs, and `match.json` token text. `.cards/`
  and `cmd/repro/testdata/.tokens/` are gitignored for this reason.
- **Don't commit `oraclepacket` output.** Write it to a gitignored path.

## Tests, ratchets and docs

- **Don't report a green suite from a worktree without `.cards/`.** A package
  printing `ok` in about 2 ms ran nothing.
- **Don't call red on `main` "pre-existing".** Bisect it and fix it. But
  don't "fix" a peer's file that is seconds old in a shared checkout; that is
  work in progress.
- **Don't add a row to AGENTS.md's Known-approximations table,** and don't
  grow an existing row. Deviations go in the commit message.
- **Don't add an entry to a coverage ratchet to make a test pass** without
  saying why in the commit. Don't leave a stale entry behind when you close
  one: the ratchets fail both ways.
- **Don't move `TestHeads` goldens without naming the first diverging event**
  in the commit body (`cmd/headdiff` finds it), and don't write head-move
  prose into `rules/testdata/heads/` or `docs/agents/heads-history.md`.
- **Don't add a fuzz test to the default build.** A `Fuzz*` target or a
  seed-sweep test starts with `//go:build fuzz` and runs via `make fuzz` or
  `go test -tags fuzz`; the default suite and the pipeline gates skip it.
  Regression tests pinning a fixed fuzz finding stay untagged.
- **Don't add a row to `wall_exceptions.txt` / `rss_exceptions.txt`** (`internal/testutil/testdata/`): they only shrink. Split the over-budget test instead.
- **Don't write or run a test outside the budget** (4 GB RSS, 4 vCPU, 1 min
  wall; operator 2026-10-05, doubled 2026-10-09). Don't call `cards.LoadRegistry` in a test (use
  `internal/testutil.CorpusRegistry`), don't pass `-count=1`, don't run
  `go test ./...`, `./compliance/...` or a whole package uncapped, and don't
  run a known-heavy test whole "for a baseline": measure one chunk under the
  2 GB cap. Split a heavy test into chunk tests sharing fixtures; never
  delete assertions or shrink a census to make it fit.
- **Don't grow `AGENTS.md` with detail.** It is loaded on every agent turn.
  Put the detail in `docs/agents/` or `docs/superpowers/` and link to it.
- **Don't quote an unmeasured number as fact.** Name the command or report,
  or label it a hypothesis.

## Running things on this box

- **Don't bind ports 8080–8081** (the demo), and **don't run `make
  deploy-demo`** or `make gorged` as-is (it binds `:8080`). Engine-side task
  agents use ports 8090–8099; `scripts/fleet.sh port` prints a free one.
- **Don't point two servers at one persistence directory,** and don't use the
  repo-root `gorged-data/`. Use `-dir /tmp/gorge-<unique>`.
- **Don't stop a server with `pkill -f` or a bare `pgrep -f`.** It has killed a
  session here. Find the pid from the listening socket (`ss -lptn`) and signal
  that pid.
- **Don't run heavy jobs unbounded or in parallel.** Run one at a time, under
  a `systemd-run` scope with `MemoryMax`, and set `GOMEMLIMIT`. The box has
  been OOMed.
- **Don't put training data or checkpoints in `/tmp`** (it is RAM-backed).
  Use `/mnt/sata/gorge-training`.

## Bots and training

- **Don't let clairvoyant or oracle modes near a hosted table or a rated
  game.** They are measurement ceilings only.
- **Don't add a policy to `host/bot_policy.go`'s hosted vocabulary** without
  passing the adoption gate (see the README's *Bot player training and
  adoption guidelines*).
- **Don't train through PyTorch or any out-of-tree runtime** for anything
  meant to ship. Learned policies are pure Go and deterministic.
- **Don't commit neural checkpoints (`.gpol`)** without an operator decision.
  Small JSON weight profiles may be committed.
- **Don't read a 200-game bench as a verdict.** Its CI is about ±7pp.
