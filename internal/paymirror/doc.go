// Package paymirror is an A/B differential check for cast payment plans
// (docs/superpowers/specs/2026-09-24-cast-payment-plans.md §10: "compare a
// manual execution of the same selected payment with planned execution for
// state equivalence").
//
// At a priority decision where a seat submits a planned cast (Intent.Payment),
// Check/CheckLive run:
//
//   - A: the planned submit, then every follow-up decision of the cast
//     transaction (targets, modes, cost choices, trigger targets/order, ...)
//     answered by a deterministic Answerer and recorded, up to the next
//     priority decision (or game end). CheckLive runs A on the live engine;
//     Check runs it on a clone.
//   - B, float_then_cast: a clone taken before A's submit activates each
//     planned source through the ordinary priority "activate" option (the
//     witness's exact source; a mana-ability wheel or colour ask is answered
//     with the option producing the witness's Produces), chooses the
//     ordinary cast option, and replays A's recorded answers mapped by option
//     identity. This is the only manual route for a plan-only cast, which the
//     legacy offer gate withholds until its mana is floated.
//   - B, base_option_window: when the action names BaseOptionIndex, a second
//     clone chooses Options[BaseOptionIndex] (proving the index names the
//     cast) and pays any remaining planned sources in the cast's CR 601.2g
//     mana window.
//   - control (CheckLive, Options.Control): a third clone replays A's exact
//     intent and answers. It separates Engine.Clone fidelity from payment
//     route differences; routes are then compared clone-to-clone.
//
// # Equivalence
//
// Two engines are equivalent when all of the following hold:
//
//  1. State: a reflective structural walk of the whole rules.Engine,
//     including unexported fields and all of state.Game (every object's zone,
//     position, controller/owner, tapped, counters, damage, attachments,
//     face and cast provenance flags, targets, X, mana-spent tallies; every
//     player's life, counters, pool with snow/typed/persistent/restricted
//     provenance; stack; turn/step/active/priority/pass count; per-turn
//     tallies; pending triggers; the RNG position; delayed triggers;
//     continuous effects; replacement and cast continuations) finds no
//     difference outside the exclusions below. Shared compiled corpus data
//     (*cards.Card/Face/SA, Game.Tokens) compares by identity. Map entries
//     are paired by a canonical key rendering, never by iteration order.
//  2. Pending decision: the same kind, player, prompt, options and payment
//     actions, with Seq, the Seq-bound action/plan IDs and each activation's
//     SourceZoneSeq (a log position, offset after the fork by the route's
//     extra decision events) masked.
//  3. Events: the multiset of game events each side logged since the fork is
//     equal, excluding the route's own bookkeeping (DecisionAsk,
//     DecisionMade, priority pass-count resets) and every event's Seq. The
//     order is reported (EventOrderDiffers) but not required: the float
//     route legitimately taps before the cast begins.
//  4. Float route only: when a planned activation's cost MOVES its source
//     (Lotus Petal, a Treasure, an Eldrazi Spawn: a sacrifice), the three
//     state fields that record when that move happened relative to the
//     spell's own move to the stack -- this turn's zone-entry list
//     G.Entered, the spell's PreStackEnteredLen boundary into it, and the
//     spell's damageSourceLKI snapshots of the planned sources -- are
//     compared as order, not as game facts: the entry lists must be the
//     same multiset, and only then are the other two masked (diff.go's
//     floatReorder). Run A pays after the spell moved; the float route
//     before it.
//
// Two games that both ended inside the transaction are equivalent when their
// outcome (winner/draw and the set of losers) is equal.
//
// Run A is also checked against its own contract, independently of B:
//
//   - AInvariant: at the priority boundary no cast transaction is pending and
//     no choose flow is armed (CR 601.2: no priority while casting).
//   - AWitness: each planned source's activation (its first tap, or its
//     first zone change when its cost moves it without a tap) is followed,
//     after the rest of its own cost (its sacrifice, the payer's life), by
//     exactly its witness Produces (skipped when A fell back to the manual
//     window).
//   - ASideEffects: damage a planned source dealt while producing mana.
//
// # Exclusions
//
// Every excluded rules.Engine field, with why it may legitimately differ
// between two equivalent engines (TestExclusionTableNamesEngineFields keeps
// the table naming real fields):
//
//	L                                  the event log: compared semantically (3.); Seq/decision
//	                                   events shift by the manual route's extra decisions
//	pending                            compared by (2.) with Seq-bound identities masked
//	derivedMemo*, derivedKW/Types/     per-walk Derived memo and scratch, keyed by an epoch
//	Depth/PTFrames, boardStaticsCache, that advances with every ask; Clone copies none
//	activeStaticsCache, mayPlaysCache
//	staticContinuous/Epoch/Version/    layer/static rebuild caches keyed by log length and
//	Objs/QueueBuf, activeBuf/Epoch/    continuousVersion
//	Version/Depth/Objs, renames/*,
//	layer4Types/types*, sbaQuiet/Unquiet
//	ascend, storied                    incremental arena scans ("a pure cache, zero = rescan")
//	turnsTaken/Epoch, turnStartTurns/  TurnChange census caches keyed by log length
//	Epoch
//	continuousVersion                  cache-invalidation counter over e.continuous (which is
//	                                   compared); clones restart it at zero
//	triggerEventMasks, triggerObject-  immutable-syntax lookup caches keyed by face pointer
//	Masks, trigZones/Ep, trigFaceZones,
//	phaseSpecs
//	replZones/Ep                       replacement-walk zone summaries, validated on every use
//	paymentPlanCarriers/Objs/Events/   payment-plan interference carrier memo keyed by the
//	Valid                              object-arena size and log length; Clone copies none
//	legalOptBuf, manaAbBuf, manaLabels, reused scratch buffers
//	intentBuf, sbaIDBuf, foreachBuf
//	loop, askCount                     intent-stream watchdog and ask counter: they count the
//	                                   route's decisions, not the game
//	legalActionWalks                   legalActionsPriced's diagnostic call counter: the float
//	                                   route re-prices offers against a hypothetical floating
//	                                   pool the control route never opens, so it counts the
//	                                   route's work, not the game
//	ManaAbilityHook, paymentStats      harness-only observers (rules/clone.go): Clone copies
//	                                   neither, and cmd/cardfuzz installs the hook on its live
//	                                   run A, so the control would read "<func> vs nil"
//
// A route is Unmirrorable when the manual route cannot be driven at all. It
// is Expected (RouteResult.Expected) when the reason is a known limit of the
// float route rather than evidence about run A -- floating put only the
// float's own triggered abilities on the stack, which a sorcery-speed cast
// cannot be made over -- and cmd/cardfuzz records no failure for a cast whose
// only unmirrorable routes are expected (Report.ExpectedUnmirrorable).
//
// Not excluded, and therefore compared: everything else, including the log-
// derived engine tallies rules reads later (manaExpended, tappedTurn,
// triggerFireCount, castAborts, suppressedCast, inertHeldOut, sacrificedLKI,
// ...), so a route that changes what a later rule reads is a mismatch.
package paymirror
