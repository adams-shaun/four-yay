# Dumb-choice census for the heuristic bots

Status: measurement only, stopped early by the operator (a label-based regret
study now covers the question). The planned paired A/B runs were NOT run, so
this report makes no win-rate claim. Nothing in the engine or bots changed.

## Method

Throwaway harness (not committed; archived at
`/mnt/sata/gorge-training/bot-sanity/census2/`, raw output `census.log`).
`bench.PlayGame` over the 15 constructed repo decks, every ordered pair, 2
seeds, same policy on both seats: 420 games per policy (23, 20, 10, 41 hit the
turn/intent cap). The decision hook reads the live engine state just before
each answer is submitted and tests each class's definition. Two-player games
only. Decisions per policy: bot 182,278; lethal-pressure 178,115; sb-tactical
163,756; sb-heuristic 310,364. Most decisions are pass-only or mana-only
priority stops, so "per 1000 decisions" understates; the opportunity column
(`% of opp`) is the more honest rate.

## Results (occurrences / opportunities)

Opportunity is class-specific (see definitions). `n/a` means the class had no
hit and I do not trust the zero (see caveats).

| Class | bot | lethal-pressure | sb-tactical | sb-heuristic |
|---|---|---|---|---|
| C11 hostile effect (damage/removal/Counter/Discard/LoseLife) aimed at own target | 353 / 2606 (13.5%) | 339 / 2540 (13.4%) | 175 / 2289 (7.7%) | 1118 / 2258 (49.5%) |
| C7 beneficial-looking effect (Pump, PutCounter, Attach, Animate...) aimed at an opponent target while an own target existed | 120 / 922 (13.0%) | 123 / 872 (14.1%) | 0 / 711 | 85 / 38451 |
| C3b losing block (blocker dies, attacker survives) when unblocked damage would not have been lethal | 105 / 645 blocks (16.3%) | 0 / 416 | 38 / 534 (7.1%) | no block decisions seen |
| C3c same, and life would stay >= 8 | 31 (4.8%) | 0 | 13 (2.4%) | n/a |
| C5 attacker that every possible blocker kills without dying (not all-in lethal) | 99 / 4781 (2.1%) | 15 / 4637 (0.3%) | 100 / 4639 (2.2%) | 201 / 3929 (5.1%) |
| C2a pay life at life <= 4 (fetchland, Phyrexian) | 32 / 611 (5.2%) | 30 / 608 (4.9%) | 33 / 580 (5.7%) | 16 / 638 (2.5%) |
| C2b sacrifice cost not the cheapest permanent | 7 / 151 | 8 / 152 | 15 / 111 (13.5%) | 20 / 153 (13.1%) |
| C1a discard a land with < 4 lands in play, a nonland kept | 7 / 312 | 7 / 307 | 20 / 501 (4.0%) | 10 / 400 |
| C1b discard best spell while keeping a land with >= 4 in play | 13 / 312 (4.2%) | 13 / 307 | 2 / 501 | 4 / 400 |
| C4 unblocked damage lethal though a surviving assignment existed | 2 / 859 | 4 / 706 | 0 / 663 | 29 / 491 (5.9%) |
| C10 creature stays home though opponent controls no creature | 18 / 1377 (1.3%) | 17 / 1908 | 0 / 671 | 0 |
| C9a passed priority, empty stack, mana floating | 1413 / 118781 (1.2%) | 1393 / 116117 | 103 / 111274 | 247 / 104096 |
| C9b ability with no visible state change (approximate, see caveats) | 377 / 1983 | 366 / 1906 | 157 / 1635 | 38122 / 39911 |
| C6a cast creature while holding a castable wipe; C6b cast a wipe with own board ahead | 0 | 0 | 0 | 0 |
| C8a / C8b end turn at main 2 with castable permanent spell / unused land drop | 0 / 7223 | 0 / 7043 | 0 / 6709 | 0 / 6337 |

Class definitions: C11 is the target decision's `TargetEffect` (Damage,
Removal destroy/exile/graveyard/sacrifice, or API Counter/Discard/LoseLife)
with the chosen option controlled by the deciding seat. C7 is API in
{Pump, PutCounter, Protection, Regenerate, Attach, Untap, Animate}, chosen
target an opponent's, an own option available, and the card's Oracle text
lacking "gets -" / "-1/-1" / tap wording. C3 uses layer-derived power/toughness
and keywords (deathtouch, first strike, indestructible) of each blocker and
attacker; "unnecessary" means life minus all unblocked damage plus the damage
of every losing-blocked attacker is still above 0 (C3b) or at least 8 (C3c).
C5 skips required attackers, evasive attackers (menace, unblockable-like) and
attacks where the declared total power is lethal. C6 detects wraths by an
Oracle regex.

## Top cards per class (false-positive check)

- C11: Wasteland (172 for bot), Swords to Plowshares (65), Counter on
  Mausoleum Wanderer (54), Fatal Push, Path to Exile, Spell Pierce. The
  counter-own-spell cases are new relative to the earlier self-hit report.
  The one case with an opposing target available per policy (C11b) is 1.
- C7: Aspect of Hydra (31), Vines of Vastwood (25), Rancor (20), Reckless
  Charge (18), Karn the Great Creator animate (10). Oblivion Stone's PutCounter
  is a false positive (it targets with fate counters deliberately). The pump
  and Rancor hits look like real mistakes. sb-tactical has none.
- C3b: tokens and small creatures blocking Wurmcoil Engine, Spectral Sailor
  blocking Sunspear Shikari. Many are chump blocks with life well above lethal.
- C5: Monastery Swiftspear (prowess and tricks make this a false positive
  risk), Goblin Bushwhacker, Elemental Tokens, mana creatures.
- C2a: dominated by fetchlands at life <= 4 (shuffle value is often needed);
  genuinely risky only when life is 1-2.
- C9a: mainly mana creatures and lands tapped at priority; only bot and
  lethal-pressure do it (1.2%), sb-tactical and sb-heuristic almost never.

## Decidability from Decision / Board

| Class | Decidable from the decision and board? | Notes |
|---|---|---|
| C11 | Yes, cheaply: `TargetEffect` plus option `Player`, at the priority stage only via a clone dry run or a published count | Proven earlier: guard removes 98%, small positive effect for bot and lethal-pressure. Extend the rule from Damage/Removal to Counter/Discard/LoseLife. |
| C7 | Mostly: needs the effect sign. `TargetEffect.API` alone cannot separate a pump from Dismember or a fate counter; I used Oracle text, which is not on the decision | A published `Beneficial` bit would make it cheap. Rate (13-14%) is as high as C11. |
| C3b/c | Yes, from Board creature table (power, toughness, keywords) at the blockers decision | Needs a rule for tricks, first strike and multi-block. Possibly valuable to chump when life is low; guard only when comfortably safe (C3c). |
| C5 | Partly: needs the defender's untapped creatures and open mana/tricks | False positives from haste/prowess/tricks; low rate for the best bots. |
| C2a | Yes (life, cost kind) | Low rate; fetchlands are a poor target for a guard. |
| C2b | Needs a value function for permanents | Low rate; no clear guard. |
| C1a/C1b | Yes (hand, lands in play) | Rare (0.04-0.12 per 1000 decisions), probably not worth a layer. |
| C4 | Yes | Essentially absent for the three good bots (0-4 cases). sb-heuristic 5.9% is out of scope. |
| C10 | Yes | 1.3%, and the cards are non-attackers (Pithing Needle, Mishra's Factory); a false positive class. |
| C9a | Yes (pool and stack) | High count but not demonstrably harmful. |
| C9b | No (my detector is unreliable) | See caveats. |

## Ranked recommendation (measurement only, no A/B behind it)

1. Add to the shared guard layer: C11 (already proven), extended to Counter,
   Discard and LoseLife hostile APIs, which account for roughly a quarter of
   bot's own-target casts (Counter on Mausoleum Wanderer, Spell Pierce).
2. Strong candidate, needs the engine to publish a beneficial/hostile bit or an
   allow-list: C7 (13-14% for bot and lethal-pressure, 0% for sb-tactical, so a
   proven bot has already solved it).
3. Candidate with an A/B first: C3c (unneeded chump blocks at comfortable
   life). Low rate (4.8% of bot's blocks) so the expected gain is small.
4. Reject or defer: C1, C2, C4, C5, C10 (rare or false-positive prone), C9a
   (not shown harmful), C6, C8 (no occurrences measured).

The paired A/B for the top two or three was not run, so none of these
recommendations rests on a win-rate result.

## Caveats and known measurement defects

- Single-seat-count, two-player, constructed decks only; 2 seeds per ordered
  pair; no confidence intervals for rates (counts are large only for C11, C7,
  C9).
- C8a/C8b, C6a/C6b show zero everywhere. I could not confirm the detectors
  fire (they might be correct: the bots play out their turn in main 1, and
  wipes in these decks are rare), so treat them as "not found", not "absent".
  C3 for lethal-pressure shows 0 losing blocks against 105 for bot; it uses
  fewer block decisions (416 vs 645) but a zero is surprising, so it may be a
  genuinely different block policy or a detector gap. Not investigated.
- C9b is unreliable: sb-heuristic's 95% (equip on Bonesplitter, Leonin
  Scimitar) means the before/after signature misses attach changes or compares
  too early, and Jace the Mind Sculptor changes only the library order. Do not
  quote C9b.
- Dumb is judged by simple static rules; the evaluation report's operator
  caveat stands: such actions can be right in context and the engine must not
  filter anything. Filters belong in a decorator for the heuristic policies
  only, never in search or trained bots.
- The label-based regret study is the better arbiter for classes C3, C5, C7.
