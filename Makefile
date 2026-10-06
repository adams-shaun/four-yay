SHELL      := /bin/bash
BIN_DIR    := bin
# Go tooling must never see web/node_modules (stray .go files from
# transitive npm deps) or .worktrees (sibling task checkouts): go's ./...
# skips dot-directories and stops at web/go.mod, but find/gofmt do not.
#
# .ds4 and .superpowers are pruned for the same reason: both are agent scratch
# trees, neither carries a single TRACKED .go file (verified: `git ls-files
# '.ds4/**/*.go' '.superpowers/**/*.go'` is empty), and both are gitignored.
# Without the prune `make lint` fails on the formatting of files nobody can
# commit -- an archived agent's scratch main.go was enough to turn the lane
# red -- and GO_SRC picks them up as build dependencies, so dropping a stray
# .go file into a scratch directory needlessly rebuilds every binary.
GO_FILES   := find . \( -name node_modules -o -name .worktrees -o -name .ds4 -o -name .superpowers \) -prune -o -type f -name '*.go' -print
GO_SRC     := $(shell $(GO_FILES) 2>/dev/null) go.mod

# Resource caps for every Go invocation below. `go test ./...` defaults -p to
# GOMAXPROCS (32 on this box), so the full tree can hold 32 test binaries
# resident at once; with a second worktree gating at the same time and the
# local agent fleet on the machine, that OOMed and locked the box. -p bounds
# how many package binaries run concurrently, and GOMEMLIMIT gives each one's
# GC a soft ceiling to collect against instead of growing until the kernel
# steps in (host/ peaks at 8.5G unbounded, 5.2G under this limit, and
# still finishes inside its 30s budget at 24.8s -- 4GiB thrashes it to 33.5s).
#
# -parallel and GOMAXPROCS are deliberately left at their defaults: capping
# them to 2 was measured and bought only ~1.4G more headroom while doubling
# rules/ (4.6s -> 11.0s) and pushing every package past its recorded budget.
#
# GOMEMLIMIT bounds the Go HEAP only. The race detector's shadow memory sits
# outside it, so -race stays one package at a time, and never in two
# worktrees at once.
#
# Both ?= so a caller can raise them for a deliberate one-off measurement.
export GOMEMLIMIT ?= 5GiB
# -trimpath (operator, 2026-10-05): without it each worktree's absolute path
# keys every compile, so no worktree reuses another's build cache.
export GOFLAGS ?= -p=2 -trimpath
GO_TEST_FLAGS ?= -p=2

# Where forgec puts the fetched corpus and the IR compiled from it. Never
# committed — the scripts are GPL-3.0.
CARDS_DIR  ?= .cards
# gorge's fork of Card-Forge/forge: its `gorge` branch is upstream plus
# script corrections against the printed card, each also sent upstream.
FORGE_REPO ?= https://github.com/adams-shaun/forge.git
# Pinned to the lock's commit for M2r; a corpus bump is a deliberate,
# ledgered change, not a side effect of Forge's master moving.
# ab2a79bd8 = fb4d80911 (Card-Forge master 2026-10-02) + Card-Forge/forge#12153
# (Bard's Company P/T, Voice of Victory type, Yip Yip! target).
FORGE_REF  ?= ab2a79bd87b87df17c32a4eef77c4a6f3fdc998e
# The XMage commit the compliance oracle (manifests, out-of-tree driver)
# is pinned to. XMage is MIT; nothing from it is a build dependency.
# docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md
XMAGE_REF  ?= 6b602a1c85e8ed738a4b40b1ef44c54845b57f68
XMAGE_ORACLE_DIR ?= /mnt/sata/gorge-training/xmageoracle

.PHONY: help
help:
	@echo "mtgcore targets:"
	@echo "  make build          — compile forgec, mtgsim and gorged"
	@echo "  make fetch-cards    — fetch Forge cardsfolder + tokenscripts at FORGE_REF into $(CARDS_DIR)"
	@echo "  make compile-cards  — compile the fetched corpus into the IR cache"
	@echo "  make report         — print card coverage against implemented primitives"
	@echo "  make enginebench-pair BASE=main CAND=. — paired engine-speed comparison (cmd/enginebench)"
	@echo "  make enginebench-profile [REV=.]       — CPU/alloc profiles of the bench rows, tops printed"
	@echo "  make enginebench-verify [REV=.]        — memo-verify-mode smoke over the bench rows"
	@echo "  make compliance-manifests — regenerate compliance/manifests from XMage set classes at XMAGE_REF"
	@echo "  make xmage-oracle-setup — build XMage at XMAGE_REF out of tree (heavy; run alone)"
	@echo "  make compliance-pass SETS='FRA BLB' — XMage compliance pass over the sets (default: every printed list); reruns only stale verdicts, under the heavy-job lock"
	@echo "  make compliance-status — generated per-set/per-format compliance status (compliance/status.md, never committed) + ratchet check"
	@echo "  make sim            — build mtgsim and play 20 verified 4-seat games"
	@echo "  make gorged         — run the M2a table server (browser client at the addr)"
	@echo "  make deploy-demo    — rebuild and (re)serve the demo on :8080 (bot tables omniscient)"
	@echo "                        (PACE=1.5s / TABLES / SEATS / FORMATS / SEED override the defaults)"
	@echo "  make stop-demo      — stop every running gorged, start nothing"
	@echo "  make gentypes       — regenerate web/src/protocol.ts from package protocol"
	@echo "  make seed-ask PROMPT=\"...\" — queue a one-off research/planning ask into the pipeline (PRIORITY, KIND override)"
	@echo "  make web            — npm ci and build the spectator client into cmd/gorged/webdist"
	@echo "  make web-dev        — run the Vite dev server for web/"
	@echo "  make test-web       — run web/'s Vitest suite"
	@echo "  make lint-web       — svelte-check and eslint over web/"
	@echo "  make smoke          — headless-browser smoke gate vs two real gorged servers (public+omniscient); fails on any browser error or a hung loading state"
	@echo "  make test lint cover"
	@echo "  make conformance    — run the CR 601/733 conformance suites (see the target's comment)"
	@echo "  make oracle-audit   — run the Oracle-text card audit (rules/testdata/oracle; ORACLE_RUN=<Card> to filter)"
	@echo "  make clean-seats    — delete finished pi-agent seat dirs (~/.cache/pi-agent); dry run unless APPLY=1"
	@echo "  make clean-worktrees — remove merged, clean .worktrees/* and their branches; dry run unless APPLY=1"
	@echo "  NOTE: make test-web / npm test needs Node >=22 (vitest 5); see web/README.md"

.PHONY: build
build: $(BIN_DIR)/forgec $(BIN_DIR)/mtgsim $(BIN_DIR)/gorged

$(BIN_DIR)/forgec: $(GO_SRC)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -o $@ ./cmd/forgec

$(BIN_DIR)/mtgsim: $(GO_SRC)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -o $@ ./cmd/mtgsim

$(BIN_DIR)/gorged: $(GO_SRC)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -o $@ ./cmd/gorged

.PHONY: gorged
# gorged runs the M2a table server: perpetual bot tables served to a browser
# at the listen address. make web builds the Svelte client it embeds first.
# -vsbot arms the landing page's play-a-human-vs-bot entry point; without it
# the server is spectator-only, exactly as before the feature existed.
gorged: $(BIN_DIR)/gorged
	$(BIN_DIR)/gorged -decks internal/testutil/decks -tables 4 -seats 4 -pace 1.5s -format commander,constructed -vsbot

.PHONY: traindash
# traindash serves the read-only live training dashboard over the policynet/PPO
# runs under TRAIN_ROOT (cmd/traindash). Port range 8082-8089, never 8080/8081.
TRAIN_ROOT ?= /mnt/sata/gorge-training
TRAINDASH_ADDR ?= 127.0.0.1:8086
traindash:
	go run ./cmd/traindash -root $(TRAIN_ROOT) -addr $(TRAINDASH_ADDR)

.PHONY: deploy-demo stop-demo
# deploy-demo refreshes the local demo: one server on 127.0.0.1:8080 whose
# bot-only tables spectate omniscient (play-vs-bot tables stay public), four
# 2-seat constructed tables at a 50ms pace, bots playing az-redeal (2026-09-29:
# the strongest measured hosted policy, +20.5pp vs the production bot --
# scripts/deploy-demo.sh has the full rationale and the override to restore
# the previous 4-seat two-Commander-two-constructed/lethal-pressure shape)
# (scripts/deploy-demo.sh; the :8081 omniscient server retired 2026-09-28). Run BY HAND, by the operator, when the demo should pick up
# main: it stops the running servers, which aborts every in-flight vs-bot
# game, so nothing runs it automatically any more (the post-merge hook and
# the daemon's landing.deploy_cmd were removed on 2026-09-22).
#
# The binary is rebuilt unconditionally rather than through
# $(BIN_DIR)/gorged: that rule depends on the Go sources, but the client
# reaches the server through //go:embed all:webdist, so a web-only change
# leaves every .go file untouched and the embedded client stale -- which is
# precisely the change a demo exists to show.
deploy-demo: web
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -o $(BIN_DIR)/gorged ./cmd/gorged
	scripts/deploy-demo.sh

# The durable card-art cache deploy-demo fills before it starts any server
# (gorged -prewarm-art-only, called from scripts/deploy-demo.sh).
ART_DIR ?= /mnt/sata/gorge-data/art

# prewarm-art fills the durable card-art cache from the repo's deck files and
# exits — the fill the deploy runs ahead of both demo servers, usable by hand
# or from cron. A no-op (zero Scryfall fetches) when the cache is already
# complete; exits non-zero if any name failed. Run by hand it is strict and
# unbounded; the deploy adds -prewarm-art-budget and a failure-streak limit
# and starts its servers whatever the fill's exit.
.PHONY: prewarm-art
prewarm-art:
	go run ./cmd/gorged -prewarm-art-only -decks internal/testutil/decks -art-dir $(ART_DIR)

# stop-demo takes the demo down without starting it again. Same socket
# lookup as the deploy: never `pkill -f`.
stop-demo:
	scripts/deploy-demo.sh --stop-only

.PHONY: sim
sim: $(BIN_DIR)/mtgsim
	$(BIN_DIR)/mtgsim -seats 4 -games 20 -verify

.PHONY: fetch-cards
fetch-cards: $(BIN_DIR)/forgec
	$(BIN_DIR)/forgec fetch -dir $(CARDS_DIR) -repo $(FORGE_REPO) -ref $(FORGE_REF)

.PHONY: compile-cards
compile-cards: $(BIN_DIR)/forgec
	$(BIN_DIR)/forgec compile -dir $(CARDS_DIR)

.PHONY: compliance-manifests
compliance-manifests:
	go run ./cmd/compliance manifest -xmage $(XMAGE_ORACLE_DIR)/sets-only -ref $(XMAGE_REF) -out compliance/manifests

.PHONY: xmage-oracle-setup
xmage-oracle-setup:
	XMAGE_REF=$(XMAGE_REF) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) scripts/xmage-oracle-setup.sh

# compliance-pass runs the XMage compliance pass (gen, plan, XMage replay of
# the stale scenarios only, diff -write into compliance/verdicts, summary)
# over SETS, under the heavy-job lock. Needs the xmage-oracle-setup build.
SETS ?=
.PHONY: compliance-pass
compliance-pass:
	XMAGE_REF=$(XMAGE_REF) XMAGE_ORACLE_DIR=$(XMAGE_ORACLE_DIR) scripts/compliance-pass.sh $(SETS)

# compliance-status writes the generated per-set / per-format status page
# (compliance/status.md, gitignored: never committed) and checks the
# certification ratchet (compliance/ratchet.json). Spec 2026-10-03 11.3 C8.
.PHONY: compliance-status
compliance-status:
	go run ./cmd/oraclediff status -all -out compliance/status.md

.PHONY: report
report: $(BIN_DIR)/forgec
	$(BIN_DIR)/forgec report -dir $(CARDS_DIR)

# gentypes regenerates web/src/protocol.ts from package protocol's structs
# (internal/tsgen does the reflection); lint below runs it with -check so a
# stale committed file fails the build instead of drifting from the server.
.PHONY: gentypes
gentypes:
	go run ./cmd/gentypes -o web/src/protocol.ts

# seed-ask queues a one-off research/planning ask through the same agentctl
# pipeline the reward-loop seed agent and the operator both use -- see
# scripts/seed-ask.sh for what the brief wraps around the raw prompt. The seed
# agent itself (scripts/seed-agent.sh, the 20-minute timer) is deterministic
# and takes no prompt; this is the door for an ad-hoc ask.
PRIORITY ?= 2
KIND     ?= research
.PHONY: seed-ask
seed-ask:
	@test -n "$(PROMPT)" || { echo 'usage: make seed-ask PROMPT="<your question>" [PRIORITY=2] [KIND=research]' >&2; exit 1; }
	scripts/seed-ask.sh "$(PROMPT)" --priority "$(PRIORITY)" --kind "$(KIND)"

.PHONY: test
# GOGC=200: rules.test measured 104 s vs 112 s CPU (28.3 s vs 30.4 s wall) for 1.2-1.4 GB vs 0.9 GB RSS.
test:
	GOGC=200 go test $(GO_TEST_FLAGS) ./...

# conformance runs the CR 601 and CR 733 conformance suites. I-2
# (mandatory-target feasibility), I-7 (targets before payment), and the CR 733.1
# illegal-cast reversal are fixed and asserted by the ordinary suite; the
# historical opt-in guard and GORGE_CR_CONFORMANCE switch were removed after
# the following fixes landed:
#
#   38846fb2  withhold casts without mandatory targets (I-2)
#   22ea5da0  make the cast proposal a transaction (I-7)
#   81ade672  undo as-enters choice on a mana abort (CR 733.1)
#
# This remains an explicit lane because it is a focused CR audit rather than a
# default full-suite target. Use -run TestCR, not TestCR601: the 733 reversal
# tests are named TestCR733* and a narrower pattern silently drops them.
.PHONY: conformance
conformance:
	@echo "== CR 601/733 conformance: all audited leaves fixed; ordinary-suite assertions =="
	go test $(GO_TEST_FLAGS) -count=1 ./rules -run TestCR -v

# oracle-audit runs the Oracle-text card audit: scenarios whose expected
# outcomes were written from printed card text and the CR, never the Forge
# script (docs/superpowers/specs/2026-09-27-oracle-text-card-audit.md). It is
# also part of the ordinary rules suite; this lane is for a focused run and a
# per-card filter, e.g. `make oracle-audit ORACLE_RUN='Fatal_Push'`.
ORACLE_RUN ?=
.PHONY: oracle-audit
oracle-audit:
	go test $(GO_TEST_FLAGS) -count=1 ./rules -run 'TestOracleAudit/$(ORACLE_RUN)' -v

# gc-gate budgets the share of consumed CPU a package's tests spend collecting
# garbage. GC_PROCS pins GOMAXPROCS so the figure is a property of the code
# rather than of the box that ran it — idle-mark CPU scales with core count,
# and it is real CPU taken from everything else on a shared machine, so the
# budget counts it. GC_BUDGET is a fraction of user+sys, NOT the percentage
# gctrace prints (that one divides by every core the process could have used
# and reads ~1% however badly the code allocates).
GC_PKG    ?= ./host
GC_PROCS  ?= 32
GC_BUDGET ?= 0.30
.PHONY: gc-gate
gc-gate:
	go run ./cmd/gcgate -pkg $(GC_PKG) -maxprocs $(GC_PROCS) -budget $(GC_BUDGET)

# test-time measures every package's test wall time and records it, plus its
# budget, in each package's TEST_HISTORY.md (Task TT). The pre-commit hook
# enforces the budget on changed packages.
.PHONY: test-time
# fuzz runs the opt-in fuzz tests (build tag `fuzz`): the rules seed-fuzz
# acceptance sweep, the ManaBrew decoder fuzz target's seed corpus, and the
# ManaBrew response translator fuzz target. They are excluded from `make
# test` and the pipeline gates; run by hand or at night.
.PHONY: fuzz
fuzz:
	go test $(GO_TEST_FLAGS) -tags fuzz,manabrew -count=1 -run 'Fuzz' ./rules ./protocol/manabrew ./internal/manabrew

# test-manabrew runs the ManaBrew wire/translator tests (build tag `manabrew`).
# ManaBrew work is deprioritised (operator, 2026-10-02), so these test files
# are excluded from `go test ./...`, `make test` and the pipeline gates.
.PHONY: test-manabrew
test-manabrew:
	go test $(GO_TEST_FLAGS) -tags manabrew -count=1 ./protocol/manabrew ./internal/manabrew/... ./host/manabrewhttp ./cmd/gorged ./cmd/cardfuzz

test-time:
	go run ./cmd/testtime -all

# alloc-gate measures every package's test peak RSS and total allocation and
# records them, plus hard budgets, in each package's ALLOC_HISTORY.md (Task
# A4). The budgets bind: a package over its alloc_budget_mb (total GC work)
# or rss_budget_mb (what actually OOMs a shared box) fails the gate. This is
# the absolute-scale counterpart to gc-gate's ratio budget.
.PHONY: alloc-gate
alloc-gate:
	go run ./cmd/allocgate -all

# Engine-speed benchmarking (cmd/enginebench; scripts in cmd/enginebench/scripts,
# workload decks committed in cmd/enginebench/testdata/decks). Every run is
# capped in a systemd scope and serialised on HEAVY_LOCK; binaries, results
# and profiles go to BENCH_DIR (rebuildable scratch). Throughput is per
# CPU-second so a loaded box still compares.
#   make enginebench-pair BASE=main CAND=.     paired base-vs-candidate rows
#   make enginebench-profile [REV=.]           CPU + allocation profiles, tops printed
#   make enginebench-verify [REV=.]            memo-verify-mode smoke (cache changes)
# Knobs: ROWS="-row random -pair A;-row sampler" REPS=3 SECS=10 GOGC=...
# BENCH_DIR HEAVY_LOCK (see cmd/enginebench/scripts/lib.sh).
BASE ?= main
CAND ?= .
REV  ?= .
.PHONY: enginebench-pair enginebench-profile enginebench-verify
enginebench-pair:
	cmd/enginebench/scripts/pair.sh $(BASE) $(CAND)
enginebench-profile:
	cmd/enginebench/scripts/profile.sh $(REV)
enginebench-verify:
	cmd/enginebench/scripts/verify.sh $(REV)

COVER_OUT  ?= coverage.out
COVER_HTML ?= coverage.html
.PHONY: cover cover-html
cover:
	go test $(GO_TEST_FLAGS) -covermode=atomic -coverprofile=$(COVER_OUT) -coverpkg=./... ./...
	@go tool cover -func=$(COVER_OUT) | tail -1

cover-html: cover
	go tool cover -html=$(COVER_OUT) -o $(COVER_HTML)

.PHONY: tidy
tidy:
	go mod tidy

# npm's own install fingerprint; a clean checkout has no web/node_modules,
# so lint-web/test-web/web (and web-dev) install first instead of dying with
# "eslint: not found" / "vite: not found".
web/node_modules/.package-lock.json: web/package-lock.json
	@# REFUSE to install through a symlink. Task worktrees get web/node_modules
	@# as a symlink to the main checkout's install (agent-worktree.sh --web), so
	@# ~166MB is shared instead of copied. `npm ci` DELETES node_modules before
	@# installing, and through a symlink that delete lands on the SHARED
	@# directory -- wiping web tooling for the main checkout and every other
	@# worktree at once. It has happened: an agent ran `make web`, this rule
	@# fired because package-lock.json was newer than the marker, and four
	@# concurrent worktrees lost their install. The agent had been told not to
	@# run `npm ci` and did not -- make did. So the guard belongs here, where
	@# the command actually is, not in a brief nobody can enforce.
	@if [ -L web/node_modules ]; then 		echo "make: refusing to run 'npm ci' -- web/node_modules is a SYMLINK to $$(readlink web/node_modules)."; 		echo "      npm ci deletes node_modules first, which would wipe the SHARED install"; 		echo "      used by the main checkout and every other task worktree."; 		echo "      Run 'make web' (or npm ci) in the MAIN checkout instead, then re-run here."; 		exit 1; 	fi
	cd web && npm ci

.PHONY: web web-dev test-web lint-web
web: web/node_modules/.package-lock.json
	cd web && npm run build

web-dev: web/node_modules/.package-lock.json
	cd web && npm run dev

test-web: web/node_modules/.package-lock.json
	cd web && npm run test

lint-web: web/node_modules/.package-lock.json
	cd web && npm run check && npm run lint

# smoke is the browser smoke gate (Task SG1, extended by ui19): it builds the
# REAL client and the REAL binary, starts THREE gorged servers — one
# -spectator public (the mode that was broken when a null hand spread into
# the board killed every table page, and the one the omniscient-only unit
# fixtures cannot see), one -spectator omniscient, and one SEATED 1v1
# play-vs-bot server (-vsbot -humans 1, Task ui19) — on smoke ports in
# 8090-8099, and drives the headless-browser test at web/e2e against all
# three. It fails on any pageerror, any console.error, any failed request to
# the server's own origin, a page still in its loading state after a bounded
# wait, or a blank page that never mounts. It also asserts the SEATED layout
# invariants ui19 was added for: the seated player's own identity bar does
# not overlap their own hand fan, and the hand fan stays bounded by the
# board (never self-sizing straight off it). scripts/smoke.sh owns
# starting/stopping the set and removing their temp dirs on failure (a leaked
# gorged poisons the next run).
#
# Deliberately NOT part of `make test`. The Go suite must stay runnable with
# no browser and no live servers; threading smoke into the default tree would
# make `make test` need Playwright + three gorged on this shared box. CI and
# the merge gate run it explicitly, exactly like the other opt-in lanes
# (conformance, gc-gate, alloc-gate).
.PHONY: smoke
smoke:
	scripts/smoke.sh

.PHONY: lint
lint: lint-web
	@out=$$(gofmt -l $$($(GO_FILES)) 2>&1); \
		if [ -n "$$out" ]; then \
			echo "gofmt: files need formatting:"; echo "$$out"; exit 1; \
		fi
	go vet ./...
	go run ./cmd/gentypes -check

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(COVER_OUT) $(COVER_HTML)

# clean-cards is destructive and separate from clean: refetching the corpus is
# a multi-minute network operation.
.PHONY: clean-cards
clean-cards:
	rm -rf $(CARDS_DIR)

# clean-seats / clean-worktrees reclaim disk from finished agent work. Both
# are dry runs that only list what they would delete; pass APPLY=1 to delete.
# See scripts/cleanup.sh for exactly what is kept.
.PHONY: clean-seats clean-worktrees
clean-seats:
	@APPLY=$(APPLY) scripts/cleanup.sh seat-cache
clean-worktrees:
	@APPLY=$(APPLY) scripts/cleanup.sh worktrees

.PHONY: ledger
## ledger: rebuild the judge-lane issue ledger the agent dashboard renders
# Derived, never hand-maintained: the conformance lane's own -v output, AGENTS.md's
# approximations table, and the orchestrator's tracked issue files (.ds4/issues/*,
# top level only — inbox/ is the un-triaged drop zone). Writes .ds4/ledger.json
# (git-excluded). The lane runs cached (no -count=1), capped and under the heavy
# lock: see scripts/ledger-lane.sh.
ledger:
	scripts/ledger-lane.sh .ds4/lane-rules.txt
	go run ./cmd/ledger -lane .ds4/lane-rules.txt -out .ds4/ledger.json

# coverage writes the card-support tables -- .coverage/summary.md and
# .coverage/coverage.md -- into the gitignored .coverage/. They are generated,
# never committed: the `card coverage` workflow publishes them on its run page
# (job summary + artifact). Output is deterministic (no wall clock; every table
# sorted), so two runs over the same corpus and tree are byte-identical.
.PHONY: coverage
coverage: $(BIN_DIR)/forgec
	$(BIN_DIR)/forgec coverage -dir $(CARDS_DIR) -out .coverage
