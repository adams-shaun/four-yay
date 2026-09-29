# Cross-engine oracle audit — plan (2026-09-29)

Status: plan only, no code. Continues the release-by-release auditor scan
cycles from [`2026-09-27-oracle-text-card-audit.md`](2026-09-27-oracle-text-card-audit.md)
("the W-series audit" below), adding a second source of ground truth.

## 1. Problem this closes

The W-series audit gets its expected outcomes from Oracle text plus the CR,
authored by an LLM seat that never reads the Forge script. That is sound and
already found three real defects (Purphoros, Relic Vial look-back, Sidar
Jabari's command zone) in a 25-card pilot. Its cost is authoring time: a
human/CR-literate seat has to derive every expectation by hand, one card at a
time, and ambiguous CR corners (look-back, layers, timestamp ordering) are
exactly where the author is most likely to get the *expectation* wrong too,
not just where gorge is likely to get the *engine* wrong.

A second engine that runs the **same Forge script** removes that authoring
step for the bulk of cards: instead of a seat deriving "what should happen"
from prose, drive both engines from the identical setup and diff the
outcome. Disagreement is now binary and free per card; a human only reads
the ones that disagree. This does not replace the W-series audit — Oracle
text stays the arbiter of who is right — but it multiplies its throughput
and catches script-translation bugs (§3 of the audit doc's triage table)
that an Oracle-only author would have to already suspect to test for.

## 2. Candidate oracles

| Candidate | What it is | Fidelity to forgescript | Availability here | License risk |
|---|---|---|---|---|
| **Forge itself** (`forge-game` + `forge-ai`) | The Java engine the scripts are written for. Already cloned read-only at `/home/sadams/projects/ref/forge` (`forge-ai` @ `2ccbbb0`, read for P§2.8). | Highest possible: it is not a reimplementation, it is the reference interpreter for the exact script syntax gorge's compiler targets. | Runnable headless today (forge-game supports an AI-vs-AI simulation match without the desktop UI; this needs a short spike to confirm the exact headless entry point on this checkout). | GPL-3.0, same as the card scripts. Never build it into anything committed; run it exactly like the mbdelta pattern — external process, test-only, driven as a black box, never checked into gorge. |
| **ManaBrew** | Independent Rust reimplementation, already wired as a black-box oracle via `/mnt/sata/gorge-training/mbdelta` (test-only, never committed) and `host/manabrewhttp` (the committed, permanent wire). | Lower: it is a second *interpretation* of the same script corpus, so it can share gorge's bug if both read the ambiguous script clause the same wrong way — it is a check on the *engine*, not on forgescript fidelity specifically. | Already running; the harness already exists. | AGPL. "Disregard the license" (2026-09-29 operator) permits reading it; never committed into gorge, same as Forge. |
| **XMage** | Java engine, mtg-kernel's own oracle (CP7 benchmarked against it) and gorge's own predecessor bridge (`mtgplay`'s XMage bridge, which gorge replaces per this repo's own charter). | Comparable in principle to Forge, but XMage does not execute Forge's script format — its card logic is hand-ported Java per card, so a disagreement never localizes to a specific script line the way Forge/gorge can. | Not cloned here; would need fetching. Some institutional familiarity exists from the old bridge (in `mtgplay`, which this repo never imports and should not be read for this either — gorge's isolation rule stands). | GPL. Same never-commit rule would apply. |

**Recommendation: Forge as the primary oracle, ManaBrew as the existing
secondary.** Forge is the only candidate whose disagreement is diagnostic
down to "which script line did gorge's compiler or interpreter read
differently from Forge's own interpreter for this exact text" — which is
literally the audit's stated question ("leverage another established game
engine to serve as oracle for auditing proper handling of forgescript").
ManaBrew stays in play for the general engine-behavior deltas it already
covers (mbdelta's existing scope: wire shape, prompt volume, error codes);
it is not swapped out, it answers a different question. XMage is not worth
fetching for this: it cannot localize to a script line, and gorge already
has two better-fitting engines available.

## 3. Harness shape (test-only, never committed — same rule as mbdelta)

Lives at `/mnt/sata/gorge-training/forgedelta`, its own local git repo,
never pushed, never checked into gorge. Mirrors mbdelta's proven shape
rather than inventing a new one:

1. **Scenario source: the W-series audit's own JSON files**
   (`rules/testdata/oracle/<family>/*.json`, §6 of the audit doc). They are
   already engine-neutral (named cards/refs/ops, no script tokens), already
   reviewed against Oracle+CR, and already have a `setup`/`steps`/`expect`
   shape a driver can walk. Reusing them means the two audits share one
   scenario corpus instead of forking a second format.
2. **Two drivers, one scenario format**, same split as mbdelta's
   `harness/mbclient`:
   - gorge driver: the existing `rules` package runner (`oracle_audit_test.go`)
     already executes these files; it just needs to emit its per-step
     observable state as data instead of only pass/fail, so the comparator
     (§4) can read it.
   - Forge driver: a small headless Go↔Forge bridge, analogous to
     `mbdelta/mbdriver` (Rust↔stdio there; here it would shell out to a
     headless Forge sim process reading a scripted opening hand/battlefield
     and a scripted line of play). This is new work; the exact headless
     entry point needs a spike first (§5, ticket 1) since this checkout of
     Forge has not been driven headless before.
   - Setup fidelity risk: Forge's own opening-hand/zone-placement API may
     not accept an arbitrary contrived board the way gorge's logged-setup
     seam does. Expect the spike to find a subset of `setup` shapes Forge
     can accept (e.g. hand + battlefield, not mid-stack states) — scope the
     first wave to scenarios within that subset.
3. **Comparator**: per scenario, run both engines to each `expect` checkpoint
   and diff the observable set (§6 of the audit doc: zone, pt, keywords,
   life, counters, stack_size, etc. — both engines already expose enough to
   answer these). A mismatch is filed exactly like a W-series divergence:
   triaged under the same four-verdict table (§7 of the audit doc), with a
   fifth possible verdict, **"Forge disagrees with the scenario's own
   expectation"** — which sends the scenario back to Oracle+CR review before
   touching either engine, since now three sources (gorge, Forge, the
   scenario author) disagree and the scenario may be the one that is wrong.
4. **Never a merge gate.** Like mbdelta, this harness informs ticket filing.
   It does not run in CI and Forge is never a dependency of anything
   committed.

## 4. What this adds to the existing wave plan

The W-series audit's wave table (§10 there) already schedules ~150 cards
(wave 1) through ~500 (wave 5). This plan does not add waves; it changes
**how a wave's scenarios get their pass/fail signal** for any family where a
scenario's `setup` fits what the Forge driver can accept (§3, setup fidelity
risk):

- Waves already written (pilot, and whatever of wave 1 lands before the
  Forge driver exists) get a **retroactive second opinion**: replay their
  existing JSON through the new comparator once it exists. This is pure
  upside — zero new authoring cost, and any scenario where Forge disagrees
  with both gorge and the scenario itself is a strong signal the scenario
  needs a second human read.
- Waves not yet authored can, for the subset of card families the Forge
  driver's setup dialect covers, skip the "cheap-seat author + review-tier
  reviewer" two-role step (§5 of the audit doc) for the *base* scenario:
  the comparator's own agreement/disagreement with gorge is the first-pass
  filter, and a human (or reviewer seat) only reads scenarios where the two
  engines disagree, or where the family's setup is out of the driver's
  reach and the original two-role process still applies. This should cut
  wave 2-5's authoring cost roughly by the disagreement rate — expect this
  to still be a minority of cards (the pilot's Oracle-only divergence rate
  was 6%), so most of the savings is in review-seat time, not author-seat
  time, and the author role does not disappear.

## 5. Rollout tickets

1. **`forge-oracle-spike`** (P2, hand or one strong seat, not the local
   tier — this needs judgment about what Forge's headless API can and
   cannot express). Spike only, no gorge changes: confirm a headless Forge
   entry point exists on `/home/sadams/projects/ref/forge`'s checked-out
   commit, drive one pilot scenario (Fatal Push, the audit doc's own
   worked example) end to end, and report which `setup`/`steps`/`expect`
   shapes it can and cannot accept. Lives entirely under
   `/mnt/sata/gorge-training/forgedelta`; touches nothing in gorge.
2. **`forge-oracle-driver`** (P2, depends on 1). Build the Go↔Forge driver
   and comparator per §3, scoped to whatever setup subset the spike found.
   Done means: the pilot's 64 scenarios run through both engines and the
   comparator's verdicts on the 4 known-divergent ones match the audit
   doc's own triage (§8 there) — i.e., the comparator finds the same three
   defects without being told about them in advance.
3. **`forge-oracle-retro-wave0`** (P3, depends on 2). Replay every
   already-landed W-series wave through the comparator; file one ticket per
   new disagreement the Oracle-only pilot missed.
4. Wave 1 onward: fold the comparator into the ticket shape described in
   §4 above, starting with whichever wave-1 family (§10 of the audit doc)
   the spike found best setup coverage for.

## 6. Open question for the operator

Same shape as the W-series audit's own §9: is quoting a short Forge error
message or log line in a ticket's triage notes (never the script itself,
never committed) acceptable, the same way the audit doc already asks about
quoting short Oracle-text clauses? Recommendation: yes, under the same
fair-use-scale reasoning — flagging for confirmation, not blocking rollout.
