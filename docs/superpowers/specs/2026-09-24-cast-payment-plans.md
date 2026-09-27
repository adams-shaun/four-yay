# Optional payment plans on cast actions

Status: implementation specification, ready for agentctl intake. Amended
2026-09-26 (below); the amendment wins wherever it and older text disagree.
Scope: one suggested payment plan per supported cast; client selection per action.
Source inspection baseline: gorge `b4e52200d56210a08fdddc6b4a7ffc8e2236c477`;
amendment re-verified against gorge main `6c711fece`.

## Amendment 2026-09-26 (operator decisions after the auto-pay audit)

Five audits of the shipped feature (gaps, mana census, engine research, A/B
mirror, corpus fuzz; 2026-09-26) found defects and scope questions. The
operator decided the following. The body of this spec has been edited to
match; the hardening work is the wave in
[`docs/superpowers/plans/autopay-v1-hardening/`](../plans/autopay-v1-hardening/README.md).

1. **No planner versioning.** The newest planner is always V1 and the wire
   codec `version` stays `1`. There is no frozen V1, no versioned successor
   and no per-match planner-version pin. A planner or offer change is treated
   like any engine change: a planned-intent log recorded before the change may
   diverge on replay, exactly as AGENTS.md already says of engine changes for
   feedback repro. The DecisionMade payment-suffix golden still pins the
   *encoding* (on a unique-plan fixture), not the planner. Former §4 freeze
   rules are withdrawn (§4, §7).
2. **Costly sources are a last resort; only life needs confirmation** (the MTG
   Arena model). Sources are classified into three tiers (§3.2): *normal*,
   *last resort* and *deferred*. A plan may use a last-resort source only when
   no plan built from normal sources exists, and last-resort plans rank by
   Arena-calibrated cost weights (§5). Every last-resort step discloses its
   consequence in the witness (§4). Bots use last-resort plans automatically.
   For a human, a plan whose consequences are sacrifice, damage, doesn't-untap
   or return-to-hand submits on one click but its summary must name the
   consequence ("sacrifices Treasure", "you take 2"); a plan that pays life
   requires an explicit confirmation before it is submitted (§8). The deferred
   tier (sacrifice or tap *another* permanent, discard/exile/counter costs,
   mana-costed filters and Signets, dynamic amounts, reflected/imprint
   production, `RestrictValid$`, production-altering effects) stays manual-only
   and is not scheduled.
3. **CR 302.6** (summoning-sick creatures' `{T}` mana abilities) is fixed in
   the shared mana-ability gate by the already-queued ticket
   `cr302-6-sick-mana` (head regeneration approved for that ticket only). The
   planner inherits the fix; no planner ticket re-plans it.
4. **Web UX is the shipped mode switch** (commit `7022042e6` on main; the
   branch commits "keep auto-pay toggle in control strip" `ae0aef4ef`,
   "keep auto-pay out of autopass policy" `4757be4e9` / merge "decouple
   auto-pay from autopass" `6675497a7`, and "add auto-pay shortcuts to hand
   cards" `57de23759` live on `wt/automana-feature-flag` and are not ancestors
   of main, but their changes are present in main's tree -- inferred to have
   been squashed into `7022042e6`). §8 now describes it. One behaviour is fixed:
   while the seat's auto-pay preference is on, the empty-window floor and
   Auto's actionable test must not skip a window whose only real play is a
   plan-only cast, and own-spell auto-resolution is keyed on the seat
   preference, not on the table's `auto_mana` capability.
5. **D-MANUAL: float-first manual payment is accepted.** A cast that is absent
   from `Options` until mana floats is paid manually by floating mana at
   priority and then casting (CR 601.2 order differs from the planned route,
   which activates inside the 601.2g window). No additive "cast then pay"
   selector is added.

## 1. Outcome and decisions

A player can select a spell together with a concrete plan for paying its mana
cost. The engine supplies the plan and executes the selected mana abilities at
the normal payment stage. The client may use suggestions for one cast and pay
manually for the next, including within the same turn.

For a spell costing `{1}{U}{B}`, the UI can display:

```text
Cast X
  Suggested: Island -> U, Swamp -> B, Mountain -> R
  Future alternative: Island -> U, Swamp -> B, Badlands -> R
```

The Mountain plan ranks ahead of the otherwise equivalent Badlands plan because
it preserves the source that can produce two colors. The planner returns zero
or one plan per cast. The wire uses a list so a later release can return
alternatives without multiplying the top-level cast actions.

A plan normally uses only *normal* sources. When no such plan exists, the
planner may offer one that also uses *last-resort* sources (a Treasure, a
painland's coloured ability, Mana Vault); each such step names its consequence
and the plan is ranked by what it costs (§3.2, §5).

The accepted product decisions are:

* Selection belongs to an individual cast intent. There is no game-wide engine
  auto-payment mode and no toggle-change event.
* The client owns an `Auto-pay mana` preference, initially off, changeable while
  a priority decision is pending. It chooses whether a click includes a plan.
* Enabling the preference never casts, passes priority, or taps by itself.
* A selected plan names exact sources and ability/color choices, and every
  consequence beyond tapping (sacrifice, life, damage, doesn't-untap,
  return-to-hand). The engine cannot silently replace it with its current
  favorite plan, and never spends a consequence the plan did not name.
* Only life payments need an explicit human confirmation (amendment item 2).
* Ordinary manual decisions and intents keep their existing meaning. A missing
  plan means that automatic payment is unavailable for that action, not that
  the action or card is unsupported by gorge.
* All actual activation, payment and game-state changes use the existing rules
  continuations and events.Apply path.

This spec supersedes the earlier conversational proposal for a fixed per-game
`auto_v1` configuration. It also refines the illustrative `choices:[7]` plus
`payment_plan_id` request: the additive selector below is necessary for casts
which are absent from the legacy option list until mana is floated.

## 2. Existing implementation and the compatibility boundary

Read these paths before editing; symbols, not the inspected line numbers, are
the navigation anchors because other tickets are landing continuously:

| Concern | Current source |
|---|---|
| Client choice vocabulary and validation | `decision/decision.go`: Option, Decision, Intent, Validate |
| Priority actions and hypothetical mana offers | `rules/legal.go`: legalActionsPriced, PotentialActions |
| Shared cast feasibility | `rules/mana.go`: offerCastableUsing; `rules/statics.go`: manaFeasibleDescriptor |
| Source alternatives | `rules/mana_available.go`: windowManaUnit, windowManaAlt, windowManaUnits |
| Existing source search | `rules/unless_payment.go`: unlessManaReachable |
| Cast transaction | `rules/cast.go`: beginCast, continueCast, manaWindowAsk, payCast |
| Real mana activation | `rules/mana_activation.go`: availableManaAbilitiesForWindow, activateManaFor, resolveManaAbilityRef* |
| Restricted pool and payment provenance | `rules/stack.go`: paymentDescriptor, manaAvailableFor, payManaDescriptorForSpent |
| Pool solver | `rules/mana.go`: resolveManaWith, manaPayment |
| Submit and event boundary | `rules/engine.go`: ask, Submit, decisionMadeText |
| Copying | `rules/clone.go`, `events/log.go`, `view/view.go`, `host/action.go`, `host/humanseat.go` |
| Replay and recovery | `replay/`, `host/viewat.go`, `host/undo.go`, `host/feedback.go`, `internal/testutil/feedback/` |
| Browser submission | `web/src/lib/seatpanel.svelte.ts`, `web/src/lib/api.ts`, `web/src/components/SeatPanel.svelte` |

The ordinary offer gate prices casts against the current pool. The potential
action walk uses hypothetical mana, but its aggregate bound is not an executable
payment plan: it can lose source exclusivity and activation details. Neither
AvailableMana nor PotentialActions is sufficient evidence that a cast can be
paid by a particular set of sources.

Appending newly funded casts to Decision.Options would change legacy bot
selection and recorded indices even if no client selected a plan. Therefore:

1. Preserve Decision.Options, its ordering, and every legacy Index exactly.
2. Add Decision.PaymentActions, a separate optional extension of the SAME
   priority decision, with one wrapper per supported cast and a nested plan list.
3. A wrapper references BaseOptionIndex when the same cast already exists in
   Options. Otherwise it is selectable only using the new payment selector.
4. The new UI groups a wrapper with its matching ordinary cast; it never shows
   two identical cast buttons just because both forms exist.
5. Existing seats that read only Options continue to receive the same choices.
   The hosted bot's opt-in payment wrapper (`seat.Bot.EnableAutoPayMana`,
   `-bot-auto-mana`) consumes the extension; the policies themselves are
   unchanged.

Generate the extension only for a real pending priority decision. Inspecting it
must emit no event, consume no RNG, mutate no game field, or change Seq. No client
request may change which legacy options the pending decision contains.

(Amended.) The extension is built lazily, only for a consumer that reads it: an
AutoMana human seat's projection, an auto-pay bot or tool seat, or a Submit that
carries a Payment selector. A priority ask no longer builds it eagerly; eager
building cost 53-60% of all engine CPU in bot games (gaps audit, botbench
profile) while almost no seat reads it. The built value is cached on the
pending decision and is exactly what an eager build would have produced.

## 3. Required first-release scope

### 3.1 Casts

V1 supports ordinary casts from the acting player's hand, on the current main
face, with a known fixed mana cost containing generic, W/U/B/R/G and true
colorless requirements. It supports ordinary fixed cost raises/reductions by
using the engine's composed cost, not printed mana value. Targeted spells are
included when the cost is independent of target choice; the player still
chooses targets normally before mana activation.

V1 does not suggest plans for X, hybrid, Phyrexian or snow costs; additional
non-mana costs (except a spell ability's own fixed-count mandatory sacrifice,
below); kicker or other optional/alternative casting methods; alternate
faces; casts from graveyard/exile/command; target-dependent pricing; convoke,
improvise, delve or similar contributions; or mana-spent-sensitive spell riders
such as converge, sunburst, or a bonus tied to mana provenance. These remain
manual. Detection is semantic and conservative, not a list of card names.

(Amended.) The audit found each of the following receiving plans; all are
excluded, and the planner must price the same composed cost the cast path
charges, not the printed `ManaCost` alone:

* A spell ability's own `Cost$` non-mana parts (`Sac<>`, `Discard<>`,
  `PayLife<>`, `Exile<>`, `tapXType<>` …), which the cast path folds with
  `withSpellAbilityExtras` (`rules/cast.go`), and a cost static's non-mana
  extra folded at `beginCast` (Soul Immolation's `Blight<X>` shape).

  (Amended.) One non-mana shape IS admitted: a spell ability's own
  **fixed-count mandatory sacrifice** (`Sac<N/Spec>` with a literal N, the
  Bone Splinters / Village Rites / Vicious Betrayal shape). The V1 witness
  describes the mana half only; the sacrifice is answered by the player
  through the ordinary in-flow cost `choose` after the plan is submitted, so
  the witness needs no element for it and the plan never picks the victim.
  The shape check is `paymentPlanNonManaAdmissible`
  (`rules/payment_plan.go`), which admits the cost only when its mana half is
  V1-clean and its sole non-mana field is fixed-count Sac parts.
  Variable-count `Sac<X/…>` (the announced count binds the cast's X) and
  `Sac<All/…>` (which parses as an Unknown part) stay manual, as does a
  sacrifice combined with any other non-mana part. At submit time
  `ValidateCastPayment` re-runs the offer gate's own sacrifice-feasibility
  check (`nonManaCastable`) and rejects a plan whose candidate has left the
  battlefield, so the seat falls back to the manual window instead of
  committing an unpayable cast.
* Contributions announced at CR 601.2b: Convoke, Improvise and Delve, whether
  printed or granted to the spell on the stack (`hasCastConvoke` /
  `hasCastImprovise` read the stack-zone derivation; Inspiring Statuary grants
  Improvise).
* Announcement-time optional or modal costs: Spree and Tiered (`ModeCost$`),
  Gift, Replicate, Multikicker and Squad.
* Every mana-spent reader, not only converge/sunburst/`CastTotalManaSpent`:
  `ConditionManaSpent$`, `Count$Adamant…`, `Count$EachSpentToCast…`,
  `Count$TotalManaSpent`, `ManaSpentBy`, even where the engine does not yet
  evaluate the reader.

A face that merely *mentions* Sunburst (Engineered Explosives' own keyword) is
not a sunburst grant, and a `ValidTarget$` cost static applies only to a cast
that targets: an untargeted spell is not declined because of an opponent's Syr
Elenora. These two conservative over-reaches are narrowed in the planner only;
the shared pay-time capture gates they reuse are not changed.

Do not accidentally automate an optional cost or omit a mandatory cost because
the base printed mana cost belongs to the supported subset. If a normal cast
itself is supported but an alternative method is not, only offer a plan for the
normal method, provided the existing cast flow can commit that method explicitly.

### 3.2 Mana

Producers are untapped, controlled battlefield sources with a legal
payment-window mana ability (printed, or a CR 305.6 intrinsic including one
granted by a basic-land-type change such as Urborg's). Ordinary floating
unrestricted mana is usable before activating more sources. Apply summoning
sickness (CR 302.6, via the shared gate), haste, phasing, CantActivate,
`CheckSVar$` and payment-window timing restrictions through the existing
legality helpers.

The same permanent may be used only once even when it has several abilities.
The plan records the exact ability and selected production. `Add R G` produces
both; `Add R or G` produces one choice. Do not infer choices from labels.

(Amended.) Every candidate ability is classified, semantically and by one
rules-owned function, into exactly one tier. The classification reads the
ability's cost, production and every parameter, its `SubAbility$` chain, and
the triggers/replacements that can act on its tap or its mana. Unknown
parameters fail closed to *deferred*.

**Normal.** The whole resolution is "tap, add the recorded mana". Cost is
exactly `{T}`. The ability has no `SubAbility$`, no `Condition*`/
`ConditionCheckSVar$` gate on its production, no target anywhere in its chain,
no special-production parameter (`TriggersWhenSpent$`, `AddsCounters$`,
`AddsKeywords*$`, `PersistentMana$`, `UnlessCost$` …) and no `RestrictValid$`;
its parameters are within an allowlist the planner models (production and
amount, presentation keys, and the activation gates the window already
evaluates). Production is one of: fixed symbols with a literal amount
(including multi-mana production, surplus left floating); `Any` with a literal
amount (one alternative per colour); `Combo <colours>` with amount 1 (one
alternative per listed colour); `Chosen` / `Combo … Chosen` resolved against
the source's recorded as-enters colour; `ColorIdentity` / `Combo ColorIdentity`
resolved against the controller's commander colour identity (no commander, no
alternatives). No trigger or replacement on the source itself, and none on
another object whose filter can match this source's tap or mana, touches the
tap or the production. Creatures are normal (ranked later, §5).

**Last resort.** Exactly the normal shape except that the ability, or the
source's own static text, adds one or more of these consequences, each fully
determined before activation:

| Consequence | Shape (semantic) | Examples |
|---|---|---|
| `sacrifice` | cost part `Sac<1/CARDNAME>` (with or without `{T}`); the source is the only legal choice | Treasure, Gold, Lotus Petal, Black Lotus, Eldrazi Spawn/Scion, Chromatic Star |
| `life:N` | cost part `PayLife<N>`, literal N | Mana Confluence, horizon lands, Fiery Islet |
| `damage:N` | a `SubAbility$` that is exactly `DB$ DealDamage \| Defined$ You \| NumDmg$ <literal>` and nothing else; or the source's own `T:Mode$ Taps \| ValidCard$ Card.Self` trigger whose effect is exactly that | Ancient Tomb, painland and Talisman coloured abilities, Tarnished Citadel, City of Brass |
| `no_untap` | the source's own `R:Event$ Untap \| ValidCard$ Card.Self` "doesn't untap during your untap step" replacement | Mana Vault, Grim Monolith, Basalt Monolith |
| `return_to_hand` | cost part `Return<1/CARDNAME>`, or the source's own delayed self-return rider (Undiscovered Paradise's hidden-keyword `Pump Defined$ Self`) | Undiscovered Paradise |

**Deferred (manual only, not scheduled).** Everything else, including:
sacrificing or tapping *another* permanent (Ashnod's Altar, Phyrexian Tower,
Springleaf Drum); discard, exile, counter or energy costs (Lion's Eye Diamond,
Gemstone Mine, storage lands, Spirit Guides); mana-costed abilities (Signets,
filter lands, Cabal Coffers, Devotees); dynamic amounts (Gaea's Cradle, Tron
lands, Priest of Titania, Everflowing Chalice); reflected, imprinted or
remembered production (Chrome Mox, Mox Amber, Fellwar Stone, Exotic Orchard);
`RestrictValid$` production (Cavern of Souls, Eldrazi Temple, Powerstone,
Mishra's Workshop); conditional or special production (River of Tears,
Gemstone Caverns, Pyromancer's Goggles, Primal Amulet); riders other than the
last-resort shapes (Witch Engine's targeted control change, Cryptolith
Fragment's each-player life loss, Mox Poison, Runecarved Obelisk's counter,
Rainbow Vale); `Combo Any` and multi-unit Combo allocations; granted or foreign
mana abilities (Chromatic Lantern, Cryptolith Rite); hand and graveyard mana
abilities; and any source that another object's tap/mana trigger or
`ProduceMana` replacement can match (Wild Growth's land, Utopia Sprawl's land,
Crypt Ghast's Swamps, Mana Flare, Mana Reflection's controller's sources,
Nyxbloom Ancient, Forbidden Orchard). Rituals and other spells that add mana
are out of scope (casting them is a separate action).

Only a *global* effect whose scope the planner cannot prove declines every
plan of the affected player, with the diagnostic reason
`unsupported:global_mana_effect` naming the object. A `ManaConvert` static that
reaches the payer (Celestial Dawn) is such an effect: the planner prices with
the ordinary solver, which does not apply the conversion. `Untap` and
`LoseMana` replacements on other objects alter neither a tap nor a production
and are ignored; the substring match on the event name is withdrawn. A
trigger or replacement on source S affects only S's tier. An opponent's Mana
Vault, City of Brass or Claustrophobia never suppresses another player's plan.

The pool solver must preserve snow/typed/persistent/restricted provenance. For
V1 it is acceptable to decline planning for a payment whose available pool or
sources carry provenance the planner cannot model exactly. It is never acceptable
to strip the metadata and spend the mana as ordinary mana. Do not broaden the
existing restriction grammar as part of this feature.

An unsupported producer elsewhere on the battlefield does not by itself block
a plan made entirely from eligible sources. Conversely, an unmodelled global
effect that can alter a selected activation must cause fallback. Eligibility
and fallback diagnostics must explain which of these cases occurred.

### 3.3 Deferred work

Activated non-mana abilities, ward/unless/upkeep/combat payments, multiple
suggested plans, explicit source-lock preferences, strategic (next-turn or
opponent-aware) planning, automatic hybrid/Phyrexian life elections for the
spell's own cost, the deferred source tier of §3.2, and mtg-kernel/XMage
adapters are follow-ups. Do not add debt rows to AGENTS.md; report any further
exclusions in the ticket report and commit message under the repository's
closing-register rule.

## 4. Additive protocol contract

Implement shared data-only types in `decision`, below rules in the dependency
graph. Regenerate TypeScript using the existing generator. Names below are the
contract; repository-style Go names and JSON casing are specified here.

```go
// Fields on existing types, all optional on the wire.
Decision.PaymentActions []PaymentAction  // json:"payment_actions,omitempty"
Intent.Payment          *PaymentSelection // json:"payment,omitempty"
Decision.PaymentFallback *PaymentFallback // json:"payment_fallback,omitempty"

type PaymentAction struct {
    ID              string         // json:"id"
    Cast            PlannedCast    // json:"cast"
    BaseOptionIndex *int           // json:"base_option_index,omitempty"
    Label           string         // json:"label"
    Plans           []PaymentPlan  // json:"plans"
}

type PlannedCast struct {
    Object state.ObjID // json:"object"
    Face   int         // json:"face"
    Origin string      // json:"origin"; "hand" in V1
}

type PaymentSelection struct {
    ActionID string      // json:"action_id"
    Plan     PaymentPlan // json:"plan"; exact selected offered witness
}
```

PaymentPlan must have `version` (1), `id`, `cost` (structured resolved mana
requirement), ordered `activations`, `pool_spend`, and `pool_after`. Each
activation contains `source`, `source_zone_seq`, `ability`, and `produces`.
`ability` is a stable printed/intrinsic identity, not an index into a filtered
temporary list; define a discriminated identity for printed versus intrinsic
abilities and pin its codec. `source_zone_seq` identifies the latest zone entry
event for that incarnation (use the existing equivalent if one exists; define
the genesis sentinel). Production and pool quantities use nonnegative integers
and the fixed six-symbol order W/U/B/R/G/C. Cost stores generic separately from C.
Amounts must fit the existing engine types, with checked addition/multiplication.

(Amended.) A last-resort step carries its disclosed consequence. The field is
additive and optional on the wire; it is absent on every normal step:

```go
PaymentActivation.Consequence *PaymentConsequence // json:"consequence,omitempty"

type PaymentConsequence struct {
    Sacrifice    bool   // json:"sacrifice,omitempty"      the source itself
    Life         uint32 // json:"life,omitempty"           life paid as a cost
    Damage       uint32 // json:"damage,omitempty"         damage to its controller
    NoUntap      bool   // json:"no_untap,omitempty"       doesn't untap in its controller's untap step
    ReturnToHand bool   // json:"return_to_hand,omitempty" returns to its owner's hand
}
```

A present consequence must set at least one field (a zero-valued consequence
is rejected, so absent and "nothing" have one spelling); quantities obey the
existing bounds. The consequence is part of the witness: validation compares
it, and the plan identity binds it. To keep every existing V1 identity
byte-identical, the canonical plan encoding (codec note
`docs/superpowers/plans/cast-payment-plans/payment-plan-v1-codec.md`) gains a
trailer only when at least one step carries a consequence: after `pool_after`,
`string("consequences")`, then per activation in execution order `u32(flags)`
(bit 0 sacrifice, bit 1 no_untap, bit 2 return_to_hand), `u32(life)`,
`u32(damage)`. The base encoding's length is fixed by its activation count, so
the trailer's presence is unambiguous. Clients derive "needs confirmation"
from any step with `life > 0`; there is no separate flag.

Display labels may describe cards but never authorize an action or influence
identity/ranking. Internally the admitted action must resolve to the same
ordinary cast descriptor beginCast uses; the client supplies no script or SVar.

A manual submission is unchanged:

```json
{"seq":123,"player":0,"choices":[7]}
```

A planned submission uses an exclusive alternative selector:

```text
{seq:123, player:0, choices:[], payment:{action_id:<offered ID>, plan:<offered plan>}}
```

`choices` must be empty and `rest` absent/empty when Payment is present. The
new selector is valid only for priority. Decision.Validate must recognize this
case before applying the legacy Min/Max count to Choices. It validates the
actor, Seq, selector exclusivity, membership in PaymentActions, plan version,
ID and exact witness equality. Everything else continues through legacy
validation. In particular, an empty Choices without Payment remains subject
to today's normal bounds. A planned action without a plan cannot be selected.

Use domain-separated SHA-256 IDs over a documented canonical, map-free encoding
of version, decision Seq, acting player, cast identity and (for a plan) its
complete execution witness. Exclude labels, BaseOptionIndex, preferred rank and
the ID field itself. Use full lowercase hex digests. A change of list order or
display name cannot change a plan's identity. Neither IDs nor client-supplied
amounts are proof of legality; rules validates the witness independently.

(Amended.) The engine publishes only version 1, and the newest planner is
always version 1: there is no frozen planner, no versioned successor and no
per-match version pin. A planner or offer change is an engine change; §7 states
the replay consequence. A replay never means "select element zero from the
current planner". Plan bytes travel with the recorded intent, and unknown
versions are rejected. The codec itself changes only additively (the
consequence trailer above), and each change is pinned by the identity golden
(`decision.TestPaymentPlanIdentityIsIndependentOfPresentation`) and the
DecisionMade golden (`rules.TestPaymentPlanDecisionMadeGolden`).

Reject oversized lists, duplicate/reused sources, unknown ability variants,
negative/overflowing quantities, forged production, a mismatched cast, a stale
incarnation, and any extra mutable payload outside the declared contract. Check
size before expensive hashing or search. Set named V1 constants: at most 64
activation steps and 65,536 planner search nodes per cast. Hitting a supported
resource bound is a planning limit, not a declaration that the spell is illegal.

## 5. Planner and offer construction

Introduce a pure rules-side API along these lines (internal types are allowed):

```text
planCastPayment(player, exactCast, version) ->
    {ready(plan), unsupported(reason), insufficient, search_limit}
validateCastPayment(player, exactCast, witness) -> error
```

One rules-owned implementation computes the effective cost and collects eligible
source alternatives. Reuse/refactor the existing feasibility and pool solver;
do not create a second approximation of cost modifiers or mana restrictions in
view, host or the browser. Existing membership helpers need audit: the current
windowManaUnits deliberately omits some production shapes and unlessManaReachable
returns only a bool. Returning its successful branch is a starting point, not
automatic proof that the new plan is executable.

The search operates over exclusive source alternatives and evaluates complete
pool payment with the existing solver. It must backtrack when assigning a dual
land to one color would strand a later pip. Do not greedily satisfy pips and
assume the remainder is payable. Do not enumerate permutations of equivalent
activation sequences; canonicalize independent activations by source/ability
identity. Search complexity is bounded by counters, never elapsed time.

(Amended.) The search is two-phase and rank-aware:

* **Phase 1** uses normal sources only. If it finds a complete plan, that is
  the offer; last-resort sources are never considered.
* **Phase 2** runs only when phase 1 proves no complete plan exists
  (`insufficient`, not `search_limit`). It adds last-resort alternatives. A
  plan whose summed `life` and `damage` consequences would reduce the caster's
  life total to 0 or less is never offered (the planner does not kill its own
  caster; CR 119.4 already forbids paying more life than you have).
* Interchangeable units are grouped into classes (same alternatives, same
  production vectors, tier, consequence and creature flag) and the search
  chooses a *count* per class, not a subset, so thirty Mountains are one class.
  Coloured requirements are solved by exact backtracking before generic is
  filled; branches are pruned by branch-and-bound on the monotone prefix of the
  rank key below. The node budget and the `search_limit` outcome remain. The
  PP-26 boards (8+8, 10+10, 12+12 basics plus typed duals) must return the
  0-dual plan without reaching the limit.
* The zone-entry sequence of a candidate (`source_zone_seq`) is resolved from
  one index per query, not one backwards scan of the log per alternative.

Rank complete plans by this deterministic lexicographic integer tuple, lower
first (amended; the former first key, fewest sources, is demoted):

1. **Irreversible cost** (0 in phase 1): the sum over steps of sacrifice-self
   non-creature 10, sacrifice-self creature 20, doesn't-untap 25, 3 per point
   of life paid or damage taken, return-to-hand 8. These Arena-calibrated
   weights make two Treasures (20) cheaper than Mana Vault (25), which is
   cheaper than three Treasures (30), and damage cheaper than a sacrifice.
   Because the key is per ability, a source's painless ability always beats
   its painful one for the same need (a painland's `{C}` pays generic).
2. Fewest creature sources activated (a creature tap is still normal, so any
   all-normal plan that taps a creature beats any last-resort plan).
3. Fewest newly activated sources (a sufficient floating pool gives zero).
4. Least surplus newly produced mana after paying the cost.
5. Least flexibility consumed: over the chosen sources, the sum of the number
   of distinct mana types (W/U/B/R/G/C) each source's eligible alternatives can
   produce (not the number of alternatives).
6. **Hand reserve**: prefer the plan whose untapped normal remainder best
   covers the colour demand of the acting player's own hand. Demand per colour
   is the largest number of that colour's pips on any single nonland card in
   hand other than the card being cast (hybrid pips count for both colours).
   Coverage per colour is `min(remainder sources able to produce it, demand)`;
   compare coverage vectors in descending demand order (ties in WUBRG order),
   larger first. It reads only the acting player's own hand.
7. **Remainder diversity**: more distinct colours still producible by the
   untapped normal remainder first.
8. The typed canonical witness, compared numerically step by step (source
   object ID, ability kind, face, index, intrinsic name, produced vector,
   consequence), then by length. The former `fmt.Sprint` string compare (which
   sorted object 10 before object 9) is withdrawn.

Use no hidden opponent information or RNG. This is a bounded heuristic, not a
claim of optimal play. If the node limit is reached after a complete plan was
found, returning the best complete plan found is allowed and must be deterministic;
record the limit in diagnostic counters. Without a complete plan, return
search_limit. Never return a partially funded action.

Build candidate cast identities using the existing legal-action walk, retaining
timing, origin, cast prohibitions and mandatory-target feasibility checks.
Refactor a discovery/pricing callback or reuse its hypothetical discovery path
as a candidate superset, then require an exact complete plan for admission.
Do not create a second hand-only cast-legality implementation. The hypothetical
walk alone cannot admit PaymentActions.

PaymentActions is deterministic, grouped by exact cast identity, with zero or
one plan per action in V1. If the ordinary cast already exists, set BaseOptionIndex;
if not, leave it absent. The extension remains within the same decision Seq.
Cache read-only results at the decision boundary if needed, without changing
events, RNG, observers or any game's rules state. Cache keys must include the
exact decision/position and be invalidated across clone, rewind and continuation.

(Amended.) Publication is lazy (§2): `Engine.EnsurePaymentActions` builds and
caches the extension on the pending priority decision on first request; Submit
builds it on demand when a Payment selector arrives and none is cached, so
replay never depends on a live cache. The builder reuses the pending
decision's `Options` for `BaseOptionIndex` and one hypothetical candidate walk
for all candidates, instead of one full legal-action walk per candidate.

Diagnostics: the pure query result keeps its three outcomes
(`unsupported`, `insufficient`, `search_limit`) and adds a machine-readable
detail (for example `shape:contribution`, `source:deferred`,
`global_mana_effect`). An engine-side statistics sink that tools and tests can
attach counts outcomes, details, nodes, offered actions, planned submissions
and fallbacks by reason. It emits no event, is not cloned into another engine,
and is never published to another seat.

## 6. Validation and execution

At Submit, validate the full selection before appending an intent or consuming
the pending decision. Membership validation is followed by rules validation of
the current cast legality, source incarnations, abilities, production, cost and
pool witness. Failure leaves pending decision, events, head, state, RNG and
intent count unchanged. Host submission must perform the applicable validation
before acknowledging an invalid plan; malformed user input must not become a
match crash when the match goroutine later calls Submit. Reuse the host's existing
locking/ownership discipline; an HTTP reader never runs the engine.

Store an immutable copy of the selected witness on the cast continuation, then
enter the ordinary cast path. Do not pre-tap at priority or create a parallel
spell-resolution implementation. Targets and other normal announcements still
happen where they do today. No synthetic public mana-choice intents are required
for the deterministic steps that the selected plan already authorizes.

At the normal mana-activation stage, revalidate the whole remaining plan and
resolved cost before the first activation. Activate through the existing mana
ability path, honoring production choice without a redundant color prompt when
the witness already specifies it. All taps, mana production/spend, source
attribution and later cast consequences use the existing events/handlers.
Do not implement execution by directly writing Tapped, Pool or Life, or by
emitting a tap plus nominal mana in place of executing the ability.

After each activation, check actual production and outstanding continuation.
Do not continue blindly if it posed a replacement/other decision or changed a
remaining source. Preserve the normal rules timing for triggers, priority and
state-based actions; automatic execution grants no extra priority window.

If final cost/source validation fails before activation, retain the normal cast
continuation and expose its existing manual payment choices, with
PaymentFallback `{plan_id, reason}`. Required reason vocabulary:
`cost_changed`, `source_changed`, `production_changed`, `choice_required`.
An already illegal cast takes the engine's existing illegal-cast reversal path.
Do not advertise that a manual window can recover an illegal cast.

If a real activation unexpectedly interrupts a plan, suspend through the normal
decision machinery. Cancel automatic execution of its remaining steps and
continue manually when that interruption resolves. Preserve completed legal
activations and floating mana under the existing transaction/reversal rules;
do not roll them back using an ad hoc snapshot. Never substitute another source,
silently add activations, or spend unannounced life/sacrifices. Version-one
eligibility should make this exceptional, but the executor must handle it safely.

(Amended.) A cast-time announcement answered before the payment window (a
sacrifice, discard or delve pick) is not a decision the planned activation
posed: only a genuinely pending decision interrupts a plan, and priority is
never granted while a planned cast is still in progress (ticket
`autopay-exec-harden`, in flight). Each remaining step is revalidated before it
runs and its actual production is checked after it runs.

Last-resort steps execute through the same ordinary mana-ability path with
their full activation cost; the witness is what authorizes the consequence, so
no extra prompt is posed:

* `sacrifice`: the `Sac<1/CARDNAME>` cost has exactly one legal candidate, the
  source, and the existing mana-cost continuation settles a forced sacrifice
  without an ask. Typed producer provenance (a Treasure's tag) is preserved.
* `life:N`: paid by the ordinary cost settle (`LifeChange`), never by writing
  `Life`. Revalidate before activation that the payer can still pay it.
* `damage:N`, `no_untap`, `return_to_hand`: nothing extra is paid; the rider,
  trigger or replacement behaves exactly as it does for a manual activation.
* Before each step, the step's consequence is re-derived with its alternative;
  a changed consequence is `production_changed`, and a plan whose remaining
  life/damage would now be lethal falls back with `cost_changed` before any
  further activation.

The engine does not know whether a human confirmed a life payment: submitting a
selector *is* the consent. Human confirmation is the client's precondition for
submitting a life-paying plan (§8). An external agent or bot that submits one
has consented by submitting. A hosted *caretaker* standing in for a human seat
does not auto-select a life-paying plan (it falls back to the ordinary option
list for that decision).

## 7. Replay, logging and privacy

The selected witness is part of the existing persisted Intent. Add a canonical
payment suffix to DecisionMade.Text ONLY for planned submissions, incorporating
the action and plan IDs (which bind the witness). Freeze that encoding with a
golden. Manual decisionMadeText and existing events.Kind ordinals stay untouched.
Ordinary mana/cast events still describe the actual effects. A receipt UI may
derive execution from those events; the plan preview is not proof of execution.

Normal replay reconstructs the same priority decision and validates the recorded
V1 witness. Exercise Replay, ReplayTo, Engine.Clone at a target ask with a pending
plan, host restart, undo and feedback/repro. No original client setting is needed.
Reconstruction must not depend on an in-memory offer cache that existed only in
the live process. A change of the client's toggle requires no replay metadata.

(Amended; replay stance.) Replay rebuilds the offer with the planner in the
binary doing the replay and accepts a recorded selector only if that planner
offers the same witness byte-for-byte. Therefore a planned-intent log recorded
before any change to eligibility, ranking, search or codec may DIVERGE on
replay, restart, undo or feedback repro, exactly like a log recorded before any
other engine change (AGENTS.md "Reproduce a feedback report": an engine change
since the report was filed is a real, expected cause). No log migration, pin
or compatibility shim is provided. Tests that need a stable planned log record
it in-test (`cmd/repro` `TestPaymentPlanAuditFeedbackCaptureReplaysPlannedIntents`
does), and the only committed planned-play goldens pin the encoding on a
unique-plan fixture. The repo-deck chain heads (`TestHeads`) come from legacy
bots that submit no plans and must not move because of planner work.

Audit all copies of Decisions, Options, Intents and pending casts. Deep-copy new
mutable slices at ownership boundaries (host pending, view projection, human
parking, engine cloning and persisted intent admission). Caller mutation after
submission or projection must not alter either the live plan or its logged copy.
Do not apply a shared-cache pointer to two engines after cloning.

PaymentActions and pending PaymentFallback are visible only to the acting seat
where its ordinary decision is visible. Their cast identities can expose its
hand. Other seats/public spectators must not receive them, including through
events, error text or diagnostics. Follow existing omniscient-view policy.
Plan execution reveals only what the ordinary activation/cast flow reveals.
Hashing a hidden plan is not a replacement for redaction.

## 8. Browser and external-agent behavior

(Amended to the shipped mode-switch UX, commit `7022042e6`.)

**Availability and preference.** The auto-pay control exists only when the
table advertises the `auto_mana` capability (`TableInfo.auto_mana`;
`cmd/gorged -auto-mana`, default on). It appears as a switch in the seat
panel's controls and as a mode chip in the hot-button control strip
(`SeatPanelState.autoPayMana`, `setAutoPayMana`). It starts off, is local to
that seat panel instance (not persisted; a different seat or match starts
off), is unavailable to spectators, and toggling it never posts an intent.

**On is a mode, not an extra choice.** With the preference on:

* A cast that has an offered plan is presented once, as a cast carrying its
  first plan: in the seat panel's payment-action group with a source summary
  (`paymentPlanSummary`) on the button and its tooltip, as a `CAST` shortcut on
  the hand card (`HandFan`), and in the hot-button strip. A legacy cast option
  whose `BaseOptionIndex` names a payment action submits that plan when clicked
  (`SeatPanelState.click`). Plan-only casts (no legacy option) are shown the
  same way.
* Manual mana taps are hidden under auto-pay only when every play the window
  can reach is reachable without them (amended by
  `aph-web-manual-only-plays`; one shared rule, `web/src/lib/manualmana.ts`,
  for the option list, the board badges and the hot strip, on a priority
  decision only). A play is reachable without them when it is a visible
  option or a cast its own offered plan pays. So the plain `Activate … for
  mana` taps stay visible while the window can reach any play the engine
  offers only once mana floats: a potential play the decision does not offer
  and no plan pays -- a mana-costed ability, max-speed granted ability, Room
  unlock, morph turn-face-up or specialize, and every cast the planner does
  not plan (X, flashback, kicker/optional and alternative costs) -- an
  offered non-cast action whose cost may carry mana, or a payment action
  with no plan. A mana activation that costs more than a bare tap (the
  `Option.Cost` marker: Lion's Eye Diamond, a Treasure, Mana Confluence) is a
  play of its own and is never hidden. To pay a planned cast by hand in a
  window whose taps are hidden, the player switches the preference off. There
  is no `Pay manually` button (the dead branch that renders it only when the
  preference is off inside an on-only block is removed).
* A cast whose plan is unavailable shows "Suggested payment is unavailable;
  use the manual mana controls". A `PaymentFallback` on the manual window is
  displayed with its reason.
* Last-resort plans: the summary names every consequence ("sacrifices
  Treasure", "you take 2", "Mana Vault doesn't untap", "returns Undiscovered
  Paradise to hand", "pay 1 life"). A plan with no life step submits on one
  click from every entry point. A plan with any `life` step requires an
  explicit confirmation naming the life amount, from every entry point (list
  button, legacy cast click, hand shortcut, hot strip); the confirmation is
  not suppressible by the preference, and nothing is posted until it is
  confirmed. Dismissing it posts nothing.

**Off is the legacy manual route.** With the preference off, the priority list,
board badges and hand fan are exactly the legacy/manual ones. Plan-only casts
are not shown; a player pays for such a cast by floating mana at priority and
then casting (D-MANUAL, amendment item 5). There is no one-cast "Cast with
suggested mana" action while off.

Keep one card/action group for a cast even when it has both a legacy option and
a PaymentAction. The UI honors Plans order and supports rendering multiple
entries in a fixture, though the engine produces one in V1. Use IDs for selection,
not row numbers or labels. Do not post all listed plans.

Toggling after a submission affects future submissions only. Double-clicks,
in-flight requests, refreshed decisions and stale HTTP responses must not attach
an old plan to a new Seq (`submitPayment` re-resolves the action and plan on the
current pending decision by identity). Reject/adopt a fresh decision using
existing stale-intent handling.

**Auto-pass (the one behavioural fix).** Auto-pay changes which witness an
explicit cast uses; it does not otherwise change Auto/Manual policy. But while
the preference is on, a plan-bearing payment action is a real play: the
empty-window floor (`emptyPriorityWindow`) and Auto's actionable/step/own-turn
floor tests must treat a priority decision that carries a payment action with
at least one plan as actionable, independent of whether
`potential_actions` also lists the card. While the preference is off, both
behave exactly as on a table without the capability. Own-spell resolution
under normal Auto (the `decide()` own-object branch that passes on payment-plan
tables) is keyed on the seat preference, not on the table capability: a player
who never turns auto-pay on keeps the historical own-stack stops. Merely
toggling must not auto-submit a cast or release an explicitly held priority
window.

An external agent receives the same extension and submits the same Payment
selector. No special engine object, privileged planner call or game-wide mode is
needed. A small test seat that deliberately chooses the first plan demonstrates
this; changing the production bot's strategy is out of scope.

## 9. Acceptance criteria

Every row requires an automated assertion at the narrowest useful layer. Each
implementer reports test names/commands and maps them to these IDs. Shared setup
must exercise the real offer/Submit/activation/payment paths where specified.

| ID | Required assertion |
|---|---|
| PP-01 | With an empty pool and Island/Swamp/Mountain, a supported `{1}{U}{B}` cast appears in PaymentActions; its source plan is complete. Options and its indices remain the legacy list. |
| PP-02 | A cast also payable from floating mana groups with its ordinary option via BaseOptionIndex; it is not duplicated in the UI. Sufficient pool yields zero activations. |
| PP-03 | Mountain is preferred to Badlands in the opening example; the latter remains untapped after the selected plan executes. Fixed order and repeated runs yield identical IDs/witnesses. |
| PP-04 | A dual source is not counted twice. A cast requiring two mana is withheld when only one single-output dual exists. A case requiring backtracking finds a valid complete assignment. |
| PP-05 | True colorless and generic remain distinct. A colored source cannot pay `{C}`; it can pay generic. Fixed multi-output production leaves the correct surplus. |
| PP-06 | Simple dork/rock production works; tapped, sick-without-haste (CR 302.6, via the shared gate), phased, wrong-controller and activation-prohibited sources are excluded. A sick source with haste is admitted when otherwise legal. |
| PP-07 | Forbidden timing, absent mandatory targets and CantBeCast continue to suppress planned casts even with abundant mana. The engine computes the effective fixed taxed/reduced cost. |
| PP-08 | X, hybrid/Phyrexian/snow, additional/alternative costs (including a spell ability's `Cost$` Sac/Discard/PayLife/Exile/tapXType parts and a cost static's non-mana extra), Spree/Tiered, Gift, Replicate/Multikicker/Squad, printed or granted Convoke/Improvise/Delve, target-dependent pricing and every mana-spent reader (converge, sunburst, `CastTotalManaSpent`, `ConditionManaSpent$`, `Count$Adamant`, `Count$EachSpentToCast`, `Count$TotalManaSpent`, `ManaSpentBy`) receive no plan. Their legacy offers/asks remain unchanged. |
| PP-09 | Deferred-tier producers (§3.2) never fund a plan; riders outside the last-resort shapes (targets, control change, each-player life loss, poison, counters), conditional and special production are deferred. Interference is scoped per source: an opponent's Mana Vault, City of Brass or Claustrophobia leaves a basic-land plan unchanged; a source matched by another object's tap/mana trigger or `ProduceMana` replacement is deferred; only an unprovable global effect (a `ManaConvert` static reaching the payer, an unscoped effect-created `ProduceMana` replacement) declines with `global_mana_effect`. No restricted pool metadata is erased. |
| PP-10 | Inspecting/reinspecting offers changes no log, RNG, state or Seq. Node/size limits terminate deterministically; no partial plan is offered. The query outcome distinguishes unsupported, insufficient and search-limit, with a detail; the statistics sink counts outcomes, details, nodes, offers, planned submissions and fallbacks by reason without an event. |
| PP-11 | Planned casts require empty Choices/Rest, the proper actor/Seq/kind, and an offered action/plan. Unknown version, ID, ability, forged quantities, duplicate source, changed incarnation and mixed selectors reject before mutation. Legacy validation still holds. |
| PP-12 | The real Submit flow asks for targets before activating the selected mana sources. The ordinary target answer resumes the stored plan without extra tap/color asks for a supported source. |
| PP-13 | The exact listed sources/abilities/colors execute; ordinary mana activation markers, production, spend, cast provenance and final pool are correct. No extra priority pass or stack object is introduced by payment. |
| PP-14 | A controlled post-offer cost/source change invalidates execution before tapping and opens the appropriate manual/reversal path. An injected activation interruption cancels remaining automation, preserves completed legal effects, and never substitutes sources. |
| PP-15 | Plan payload survives JSON intent persistence, Replay and ReplayTo, cloning at a target ask, host restart and undo. Tampering with the saved witness rejects/diverges. Plan selection affects the chain-bound DecisionMade encoding; all manual encodings/goldens remain identical. |
| PP-16 | Mutating returned offer slices, a submitted witness or a clone cannot change the live game, another view or recorded intent. A new Seq never accepts a previous offer. |
| PP-17 | Opponents/public spectators cannot see PaymentActions or fallback details. HTTP table/seat credential fencing holds for the new selector, and invalid submissions do not crash a match. |
| PP-18 | The auto-pay switch and hot-strip chip appear only with the table capability, toggle on/off/on during one turn without issuing intents, and reset per seat panel. While on, clicks include Payment and manual mana taps are hidden; while off, the list is the legacy one and plan-only casts are not shown. Pending submitted plans are unaffected by later toggling (exactly one post, carrying the plan chosen at click time). |
| PP-19 | UI groups base/planned casts, renders a multi-plan fixture by stable IDs, handles stale responses (no post for an old Seq after adopting a new one) and double submissions, shows fallback. With the preference on, neither the empty-window floor nor Auto's actionable tests pass a window whose only play is a plan-only cast; with it off, auto-pass is exactly the capability-less policy, including own-spell stops. |
| PP-20 | A test Seat chooses the extension through the normal host API and completes a deterministic replayable game, including a two-seat run on dual-land repo decks (e.g. `ur-delver`, `uw-control`) in which every planned cast reaches the stack (never back in hand after its Submit). Another Seat ignores the extension and retains the legacy action/intent behavior. |
| PP-21 | The corpus is present and existing TestHeads, repo-deck ratchets and focused conformance gates pass without golden updates. Fixed-seed hosted planned games finish without new errors/livelocks and replay identically. |
| PP-22 | `Add {X} or {Y}` (Combo), recorded-colour (Chosen) and commander colour-identity producers are normal sources: Selesnya Guildgate pays {W}; Thriving Isle with R recorded pays {R} (without a recorded colour only its fixed ability); Command Tower pays {G} for a GW commander and nothing without a commander. Execution poses no colour ask and a witness naming a colour outside the set is rejected. |
| PP-23 | Last-resort sources fund a plan only when no normal plan exists, ranked by the §5 weights (2 Treasures before Mana Vault before 3 Treasures; damage before sacrifice; a painland pays generic with `{C}`). Each step carries its consequence; a lethal plan is never offered. Submit executes sacrifice/life/damage/no-untap/return-to-hand steps through the ordinary path with no extra ask (Treasure leaves play, life moves by a `LifeChange`), and a changed consequence falls back before activation. |
| PP-24 | The web client names every consequence in the plan summary; a life-paying plan posts nothing until an explicit confirmation from every entry point; other last-resort plans submit on one click. A hosted caretaker for a human seat does not auto-select a life-paying plan; an auto-pay bot does. |
| PP-25 | Ranking follows §5: a creature rock is not tapped when basics suffice; a damage land is not tapped when three Islands suffice; for {1}{G} over Forest, Forest, Forest, Island the Island stays untapped (remainder diversity); with a {G} card in hand, {1}{W} over Forest, Island, Plains taps Island and Plains (hand reserve) and an opponent's hand never changes the plan ID; object 10 does not sort before object 9. |
| PP-26 | 8+8, 10+10 and 12+12 basics plus typed duals at {6}{U}, {8}{U}, {9}{U} return a 0-dual plan with reason "" well under the node budget; a 36-source 12-generic board returns a plan; on a 20-source mixed board the plan equals an exhaustive oracle's best under the §5 rank. |
| PP-27 | Lazy publication: offers built on demand equal the former ask-time offers on fixed seeds; non-consuming seats never trigger the builder; every consumer (AutoMana human projection, hosted auto-pay bots and caretakers, botbench `bot-auto-pay`, `cardfuzz -autopay`, `paymirror`) still receives offers; replay/undo/restart/feedback rebuild on Submit; botbench JSON is byte-identical and CPU drops to within 15% of a no-publication build. |
| PP-28 | Standing sweeps: `cmd/paymirror` over constructed, commander and random formats reports every planned cast equivalent except named, storied findings, with no `cost_changed`, `choice_required` or `cast_aborted_before_payment` caused by an excluded shape; `cmd/cardfuzz -autopay all` shows no `planrev`/`planfb` signature caused by an excluded shape or rider, and planned casts replay byte-identically. |

Tests must use authored minimal IR fixtures or the gitignored corpus, never
committed Forge script text. Test helpers must not bypass the very validation
or execution being asserted. Any unexpected red committed baseline must be
investigated under AGENTS.md; do not fix another session's uncommitted files.

## 10. Verification and release evidence

Use focused tests for each slice. At integration, run the repository's required
gates, plus tests covering all PP rows. Suggested gate commands (adapt package
selection to actual changes, preserving the coverage intent):

```sh
go test ./decision ./events ./rules ./view ./seat ./replay ./host ./host/httpapi ./cmd/repro -run 'Test.*PaymentPlan' -count=1
go test ./rules -run 'TestHeads|TestEveryRepoDeck|TestRepoDeckGamesReplayExactly' -count=1
make conformance
make sim
go run ./cmd/gentypes -check
git diff --check
```

Use the web package's actual package.json scripts for its focused tests, type
checks and lint. Names of the new tests should contain PaymentPlan so the first
command executes them; identify all selected tests and verify none were skipped
for a missing corpus. `make sim` is a legacy-regression gate, not evidence that
a planned cast was exercised: add the explicit plan-selecting test-seat lane.

Measure offer-generation cost over a fixed set of ordinary and broad-source
boards, search nodes, complete-plan hit rate, unsupported/limit reasons, external
decision counts and finished replayable games. Compare the same snapshots with
and without the extension for offer overhead; compare a manual execution of the
same selected payment with planned execution for state equivalence. Do not claim
a policy win-rate or mtg-kernel speedup from this feature's smoke evidence.

Release report must contain the PP-to-test matrix, commands/results, corpus pin,
source commit, supported/excluded shapes, deterministic search limits, observed
offer cost/fallback counts and the exact manual golden check. No production-bot
promotion or change of planner ranking is hidden in the release.

(Amended.) The 2026-09-26 audits showed that the curated suite missed every
executor and planner defect the ordinary bot games exposed. Every ticket that
changes the planner, the offer or the planned executor therefore also runs the
standing sweeps, which exist on main:

```sh
# A/B mirror: planned route versus a float-then-cast manual route, per planned cast
go run ./cmd/paymirror -dir .cards -games 300 -seed 1000 -seats 2,4 -formats constructed,random -workers 4 -out <scratch>/m1
go run ./cmd/paymirror -dir .cards -games 150 -seed 2000 -seats 2,4 -formats commander -workers 4 -out <scratch>/m2
# corpus fuzz with every production seat auto-paying; -verify replays each game
go run ./cmd/cardfuzz -dir .cards -games 1000 -batch 1000 -workers 4 -seed 26092600 -autopay all \
  -state <scratch>/f.json -failures <scratch>/f.jsonl -stats <scratch>/f-stats.json
# source census (env-gated; writes census.csv, classes.md, findings.md, killswitch.md, summary.txt)
GORGE_AUTOPAY_CENSUS=<scratch>/census go test ./rules -run TestAutopayManaCensus -count=1 -v
```

A ticket states which sweep is its real acceptance and quotes the before/after
counts in its report.

## 11. Queue breakdown and landing order

(Amended 2026-09-26.) The original six tickets below have all landed. The
hardening work that encodes the amendment is the wave in
[`docs/superpowers/plans/autopay-v1-hardening/`](../plans/autopay-v1-hardening/README.md):
its README holds the queue order, the dependency DAG, the file/function
collision map, which tickets change offers, and the intake commands. Tickets
already queued from the audits (`cr302-6-sick-mana`, `offstack-mana-*`,
`mana-*`, `ap-*`, the general engine tickets) and the in-flight
`autopay-exec-harden` fix are not re-planned there; wave tickets that touch the
planned executor in `rules/cast.go` depend on `autopay-exec-harden`.

The historical first-release breakdown follows.

Companion briefs live in `docs/superpowers/plans/cast-payment-plans/`. Each is
written for direct `agentctl issue add --brief-file` intake, not a triage task.
Use these IDs and dependencies (or rename all consistently before intake):

| Ticket | Deliverable | Depends on |
|---|---|---|
| payplan-01-contract | Data types, canonical identity, validation and copy boundaries | none |
| payplan-02-planner | Pure bounded planner and dormant offer builder | payplan-01-contract |
| payplan-03-execution | Submit, continuation, event commitment and replay | payplan-02-planner |
| payplan-04-host | Publish supported offers, host admission, projection, restart/undo/feedback | payplan-03-execution |
| payplan-05-client | Toggle, grouped actions, selection, auto-pass and fallback UX | payplan-04-host |
| payplan-06-acceptance | External-seat exercise, regression gates, coverage/performance report | payplan-05-client |

Keep the offer builder dormant on normal engine/host decisions until submission
and replay can consume it. Ticket 03 can exercise the builder via focused tests;
ticket 04 enables publication in live pending decisions with all transport and
privacy paths present. No landed stage may advertise an unexecutable action.

Each ticket makes its changes in its own agent-worktree.sh worktree, rebases on
its merged dependencies, stages explicit paths, passes relevant gates, lands,
and removes its worktree. Do not dispatch shared-source edits concurrently.
No ticket may grow the AGENTS.md approximation register, replace the existing
payment engine, commit corpus scripts, or bind demo ports 8080/8081.

The expected implementation effort for this bounded release is approximately
8-12 engineer-days, not a deadline or an agent runtime budget. Planning and
continuation correctness dominate; multiple plans and more payment shapes require
their own acceptance criteria. A ticket is complete only when its assigned
criteria pass; the feature is complete after payplan-06 closes the full matrix.

## References

* mtg-kernel PaymentPlan and backtracking solver, inspected commit `5472539`:
  https://github.com/adams-shaun/mtg-kernel/blob/54725398f9fac767c8ee2cef1be6b36cd3656ca2/mtg-kernel/src/mana.rs
* XMage separates pool spending and mana activation; its human path has special
  care for spells whose result depends on spent colors, inspected commit `7bbfb31`:
  https://github.com/magefree/mage/blob/7bbfb31587b33f070d5d5efd153ef0d0217afffa/Mage.Server.Plugins/Mage.Player.Human/src/mage/player/human/HumanPlayer.java
* Gorge's earlier experiment motivates considering complete plans rather than
  searching isolated taps: `docs/superpowers/reports/2026-09-24-pn22-mana-tap-search.md`.

These are design references. Do not port their runtime dependencies or bypass
gorge's event and privacy boundaries to reproduce their implementation details.
