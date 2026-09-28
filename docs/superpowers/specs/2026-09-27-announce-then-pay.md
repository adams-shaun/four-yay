# Announce then pay: the "select mana" prompt

Status: implementation specification. Amends
[`2026-09-24-cast-payment-plans.md`](2026-09-24-cast-payment-plans.md)
(amendment item 5, "D-MANUAL", is superseded for a human seat with the
Auto-pay preference off; see §9). Baseline: gorge main `671a3ad02`.

## 1. Outcome

A player announces a spell first and pays for it second, the way the rules
order a cast (CR 601.2a-h) and the way digital clients present it:

```text
Cast Doom Blade                                 [Cancel cast]
  Costs {1}{B}   Still owed {1}{B}   Pool: —
  Swamp        [B]
  Badlands     [B] [R]
  Birds        [W] [U] [B] [R] [G]
  [Auto-fill: tap Swamp, Swamp]   [Undo last tap]
```

Operator decisions (binding):

1. Manual tapping at priority and casting from a floating pool keep working
   exactly as today.
2. CAST is shown whenever the spell is castable from the pool **plus**
   untapped mana sources, and the test for that is the auto-pay planner's
   (`rules/payment_plan.go`, the published `PaymentActions`), not a new pricer.
3. With the seat's Auto-pay preference ON, CAST behaves as today (it submits
   the suggested plan). Only with the preference OFF does CAST open the
   select-mana prompt.
4. The prompt offers every available mana ability (per source, and per
   ability/colour where a source has several), **Auto-fill** (the planner's
   activations for what is still owed), **Cancel cast** (CR 733.1 reversal),
   **board highlighting** (payable sources glow and are clickable on the
   battlefield as well as in the panel) and **Undo last tap**. It shows the
   total cost, what is still owed and the current pool.

## 2. Rules frame

* CR 601.2a: the card moves to the stack when announced. 601.2b-c: modes,
  X, additional costs, then targets. 601.2e: the legality check. 601.2f: the
  total cost is locked. **601.2g: "If the total cost includes a mana payment,
  the player then has a chance to activate mana abilities (see rule 605)."**
  601.2h: the player pays the total cost. 601.2i: the spell becomes cast.
* CR 605.3a/605.3b: a player may activate a mana ability while paying a cost
  (601.2g); a triggered mana ability resolves immediately.
* CR 733.1: an action that is reversed is undone entirely; payments already
  made are cancelled, no abilities trigger and no effects apply as a result
  of the undone action, and the player may also reverse legal mana abilities
  activated while making it (unless their mana was spent on another mana
  ability that was not reversed).

The engine already implements 601.2g as a mid-cast `choose` decision
(`manaWindowAsk`, rules/cast.go). Until now a human reached it only by accident
(the offer gate prices casts against the floating pool, so a cast was offered
only after mana floated; D-MANUAL). This feature makes announce-then-pay a
deliberate route into that window and gives the window, for that route only,
the richer option set of §4.

## 3. The opt-in announce selector

### 3.1 Wire

```go
// decision.Intent: a third exclusive selector beside Choices and Payment.
Intent.Announce *AnnounceSelection // json:"announce,omitempty"

type AnnounceSelection struct {
    ActionID string // json:"action_id"; an offered PaymentAction's ID
}
```

`{seq, player, choices: [], announce: {action_id}}`. Validation
(`Decision.Validate` → `validateAnnounce`, before the legacy Min/Max rule):
priority decisions only; `choices` and `rest` empty; `payment` absent; the
action is in `Decision.PaymentActions` **and carries at least one plan**
(the planner's proof that pool plus untapped sources pay the cast — decision
2). A planless action (`unsupported`/`insufficient`) cannot be announced.

At `Engine.Submit` the extension is built on demand (as for `Payment`), the
selector is validated, and the engine independently re-plans the cast
(`ValidateCastAnnounce`: `PlanCastPayment` must still return a plan), so a
stale offer fails before any mutation and leaves the decision pending. The
host performs the same check before acknowledging (`Registry.SubmitIntent`,
the path `Payment` already takes), so malformed input never crashes a match.

The intent is recorded like every other; `DecisionMade.Text` gains a suffix
**only** for an announce: `priority:[];announce:<action id>`. Legacy and
planned encodings are byte-identical. Execution emits the same `Priority`
marker the planned route emits and enters the ordinary cast transaction
(`beginCastWithPayment` with the announce flag): targets, modes and
additional costs are asked exactly where they are today.

### 3.2 Who can send it

Only a seat that receives `PaymentActions`: an Auto Mana table's human seat
(`TableConfig.AutoMana`, default on) or a seat that opts in through
`seat.PaymentPlanConsumer`. `Decision.Options` and `PaymentActions` are not
changed in any way. No bot sends an announce, so no bot ever reaches the
announced window (§7).

## 4. The announced 601.2g window

The window is the same `choose` decision `manaWindowAsk` poses, posed under the
same conditions, with a different option list and one extra field **only when
the cast was announced** (`pendingCast.announced`). A cast begun any other way
(legacy option, planned selector, a bot) poses today's window byte for byte.

Differences from the legacy window:

* It is posed whenever the pool cannot pay the total cost, **even when no
  untapped source remains** (the legacy window then falls through to payment
  and reverses the cast). An announced caster who tapped the wrong source can
  still undo or cancel.
* `Prompt` is `Pay for <name>`; `Source` is the spell.

### 4.1 `Decision.ManaPayment`

```go
Decision.ManaPayment *ManaPaymentWindow // json:"mana_payment,omitempty"

type ManaPaymentWindow struct {
    Card     state.ObjID   // json:"card"      the spell being paid for
    Cost     PaymentCost   // json:"cost"      the total mana cost (601.2f), generic apart from {C}
    Owed     PaymentCost   // json:"owed"      what the floating pool does not yet cover
    Pool     ManaAmount    // json:"pool"      the payer's pool, W/U/B/R/G/C
    AutoFill []state.ObjID // json:"autofill,omitempty" sources Auto-fill would activate
}
```

`Owed` is exact for the V1 cost shapes an announce admits (generic, coloured
and {C} pips): each coloured/colourless pip is covered only by its own pool
slot, generic by whatever remains. The field is present only on the announced
window; every other decision serialises byte-identically.

### 4.2 Options

In this order (each `Index` equal to its position, as `ask` enforces):

| Kind | Present when | Fields | Answer |
|---|---|---|---|
| `mana` | per activatable (source, ability, colour) | `Obj` source, `Ability` index into the source's payment-window abilities, `ManaSymbol` the colour for a flattened choice, `Cost` the beyond-tap marker, `Label` `Add B` / `Add C C` / `Add any color` | activate exactly that ability (§4.3) |
| `autofill` | the planner pays what is owed from here | `Label` `Auto-fill: tap Swamp, Island` | run the planner's activations (§4.4) |
| `undo_tap` | the last in-window activation is reversible | `Obj` its source, `Label` `Undo tapping Swamp` | §5 |
| `done` | the pool pays only with a pay-life grant (K'rrik) | `Label` `Pay` | 601.2h with the grant, today's `done` |
| `cancel_cast` | always | `Label` `Cancel cast` | §6 |

Sources are the same set the legacy window offers (untapped, controlled,
at least one payment-window mana ability, not committed to Convoke/Conspire),
in the same order. A source's abilities are
`availableManaAbilitiesForWindow(p, id, false)` (instant-speed-only mana
abilities are withheld, CR 605.4, as today). Each ability becomes:

* one option **per colour** when its production is a finite choice the
  engine can make concrete without asking: an explicit `Combo` (the existing
  wheel flattener, `manaAbilityComboColours`) or, for a planner-tier
  (normal / last-resort) ability, `Any` / `Chosen` / `ColorIdentity`
  (`paymentPlanChoiceColours`) — a dual land's two basic abilities, Birds of
  Paradise's five colours, a gate's two, Command Tower's identity colours;
* otherwise one option labelled with `manaAbilityLabel`; answering it runs
  the ordinary path, which may pose its own sub-ask (a `Combo Any`
  allocation, a sacrifice choice) exactly as a manual activation does today.

The player therefore never guesses what a multi-ability source will make. The
kind is `mana` so the client's existing mana wheel and pip faces apply; the
board already highlights every option carrying an `Obj` (cardoptions.ts), so
the payable sources glow and are clickable on the battlefield with no new
client rule.

### 4.3 Activation

The answer resolves the named ability through the same calls the stage-1
mana wheel makes (`answerManaActivation`): `resolveManaAbilityRefOriginal`
with a `withProduced` copy for a flattened colour, else
`resolveManaAbilityRef`, in the payment-window form (cast = true). Then
`continueCast` re-enters `payCast`, which re-prices the window (601.2g is a
loop until the player is done) and, once the pool covers the cost, pays
(601.2h) and completes the cast with no extra click, as the legacy window and
planned route do.

### 4.4 Auto-fill

At ask time the planner is run over the pending cast's remaining cost
(`planPaymentCost` with `castPaymentMana(pc)` and the live pool); the option
appears only when it returns a plan with at least one activation and the pool
carries nothing the planner cannot model (`paymentPlanPoolOK`). The answer
re-plans at the identical state (the planner is deterministic and pure, so
replay re-derives the same plan), installs it as the cast's payment witness
and hands it to the existing executor (`manaWindowAsk`'s planned branch):
each step is revalidated before it runs and its production checked after;
any deviation stops automation and returns to this announced window carrying
`PaymentFallback`, exactly as a planned cast falls back.

## 5. Undo last tap

**Chosen mechanism: an engine-level, logged `undo_tap` option scoped to the
window, plus one appended event kind.** The host's undo was rejected for this
use: it is refused on any table with a second human seat, it rewinds by
replaying the whole log (and a rewind past a shuffle leaks upcoming draws),
and it rewinds whole intents, so a tap that posed a sub-ask would need two
presses. An in-window undo is an ordinary recorded intent, replays like any
other answer and works at every table.

Each activation made from the announced window (a `mana` answer, and each
Auto-fill step) records `{source, event mark, trigger-queue mark, normal}` on
the pending cast, where `normal` is the planner's tier classification of the
activated ability *at activation time* (the planner's own proof that the
ability's whole resolution is "tap, add the recorded mana", with no rider,
trigger or replacement on it or on anything that watches it — spec
2026-09-24 §3.2). The record is closed when the window is next posed. The
**last** record is reversible when all of these hold:

1. `normal` was true;
2. the events it produced are exactly one `Tap` of the source and one or more
   `ManaAdd` to the payer with positive amounts, and nothing else (no
   sub-decision, no life, no counters, no `ManaActivate` limit marker);
3. no triggered ability was queued while it resolved;
4. the source is still on the battlefield, tapped, controlled by the payer,
   and carries no stun counter;
5. the pool still holds every unit it added (per slot, with the same snow
   and typed tallies).

Only then is `undo_tap` offered. Answering it emits one `ManaUndo` event per
recorded `ManaAdd` (the first also names the source): Apply removes exactly
those units from the pool slot and its snow/typed tallies, and untaps the
source. Triggered abilities the reversal would queue are dropped (CR 733.1:
nothing triggers from an undone action). The record is popped, so the next
most recent reversible activation becomes undoable in turn.

`ManaUndo` is **appended** after `ChaosEnsues` (events.Kind is append-only;
no existing ordinal, hash or golden moves). It is deliberately not a negative
`ManaAdd`: several log scans (`manaSpentForCast`, the spend buckets of
`Count$...Spent` readers, cast provenance) count negative `ManaAdd` events
after the spell's `PutOnStack` as mana spent on it, which a refund is not. It
is not an `Untap` either: `Untap` is a trigger-interest kind and an
`R:Event$ Untap` replacement target; the reversal must neither trigger nor be
replaced. `ManaUndo` is in the zero-interest set of `trigger_eligibility.go`.
The `Tap` it reverses stays in the log (history is append-only); a reader that
scans for "tapped this turn" sees a tap that was reversed, which is the one
known imprecision and is confined to announced human casts.

## 6. Cancel cast

`cancel_cast` is always offered in the announced window. The answer first
reverses the reversible in-window activations from the most recent backwards,
stopping at the first that is not reversible (CR 733.1 "the player may also
reverse any legal mana abilities"; an irreversible one — a Treasure sacrificed,
life paid, a sub-ask answered — stays done and its mana stays floating until
the step ends, CR 500.4). It then calls the existing reversal
`abortCast(pc, "cast cancelled by its caster (CR 733.1)", false)`: the spell
returns to hand, a flipped face flips back, triggers of the proposal are
dropped, and the cast is not held out of the next offer (`suppress` false: a
voluntary cancel is not a no-progress loop). Nothing was paid: 601.2h has not
happened, and every non-mana cost an announced cast can carry (a fixed-count
sacrifice) is paid at 601.2h, after the window.

Cancelling a legal cast is a product decision (operator decision 4, the Arena
model); the engine applies CR 733.1's reversal procedure to it.

## 7. Bots, goldens and the byte-identical pins

Nothing reachable without an `Announce` intent changes:

* `Decision.Options` and `PaymentActions` are untouched (the announce is a
  selector over the existing extension).
* The legacy window's code path is unchanged; the announced branch runs only
  when `pendingCast.announced` is set, and only `Submit` with `Announce` sets
  it.
* `ManaUndo` is emitted only from the announced window.
* No bot, caretaker, botbench policy or tool submits an `Announce`.

Evidence: `TestHeads` (`rules/heads_test.go`) and
`TestConstructedDefaultIsByteIdentical` (`cmd/botbench`) pass with no golden
edit; `decision` pins that an intent and a decision without the new fields
serialise byte-identically.

## 8. Client flow (web)

* Auto-pay **on**: unchanged (CAST submits `payment` with the first plan).
* Auto-pay **off**, table advertises `auto_mana`: a hand card whose payment
  action has a plan shows **CAST**. Clicking it posts the legacy cast option
  when `base_option_index` is present (the pool already pays: today's route)
  and otherwise posts `announce`. The seat panel lists the same plan-only
  casts beside the legacy options.
* The announced window renders as the select-mana panel: title, total cost
  pips, still-owed pips, pool, one row per source (name + one pip button per
  `mana` option), Auto-fill (its sources also marked on the board), Undo last
  tap, Cancel cast, and Pay when offered. Sources are also clickable on the
  battlefield through the existing card-options mechanism.
* A table without `auto_mana` publishes no `PaymentActions`, so it keeps the
  float-first route; that is unchanged by this spec.

Accessible names: every button carries an aria-label naming the source and
the mana ("Tap Badlands for R"); the panel is a labelled region; it fits the
seat panel's 21rem/92% width at phone width.

## 9. Amendment to 2026-09-24 D-MANUAL

D-MANUAL ("float-first manual payment is accepted; no additive cast then pay
selector is added") is superseded **for a human seat whose Auto-pay preference
is off on an `auto_mana` table**: such a seat casts plan-payable spells by
announcing them (`Intent.Announce`) and paying in the 601.2g window. Floating
mana at priority and then casting remains available and unchanged. §8 of the
payment-plan spec ("Off is the legacy manual route ... Plan-only casts are not
shown") is amended accordingly.

## 10. Deferred

* Casts the planner does not plan (X, hybrid/Phyrexian, kicker and other
  optional/alternative costs, flashback and other zones, convoke/delve) are not
  announceable; they keep the float-first route. Widening announce to them
  needs a castability proof other than the planner, which decision 2 excludes.
* Undo reverses only "normal"-tier activations with no side effects; a
  Treasure, a painland's coloured ability, a Signet or a filter land is not
  undoable (Cancel still reverses everything reversible before it).
* Auto-fill uses the planner's single ranked plan; no alternative plans.
