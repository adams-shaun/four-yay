# Client table UI rework — design

Date: 2026-09-28. Status: design approved in an interactive session; each
sub-project below gets its own implementation plan before any code.

Supersedes the layout parts of `2026-09-06-client-design-system.md` (tokens,
quadrant layout). Keeps its "felt vs instrument" split and its rule that
saturated colour belongs to mana, cards and seats.

## Why

Players and the operator find the in-game table hard to read and unpolished:

- The decision prompt floats over the middle of the board and covers it
  (feedback `20260927T212721Z-1f9fc4b0`, `20260921T022723Z-96e6db91`).
- Prompt text leaks engine syntax (`Pay Sac<1/Creature.Other/another creature>`).
- Two stacked rows of all-caps controls (phase track, then
  AUTO/ACTIONS/PASS/END TURN/UNDO/DONE) make the next action hard to find.
- Seat identity is split across a corner pill, the rail table and the board
  border.
- There is no layout for 5–8 seats: `seatCorner` (`web/src/lib/seattable.ts`)
  indexes only four corners, so seats 5+ render unpositioned although the
  server accepts 8 (`host/table.go`).
- Vertical space is wasted: a fixed hand band and fixed card sizes leave
  empty felt on both boards.

## Principles

1. **Configurable by default.** Anything that can reasonably be a user
   setting is one. A stated preference is the default, not a constraint.
2. **Client-side only.** Layouts, profiles and keymaps live in the browser
   (localStorage) and are never sent to the server. Sharing is by file export.
3. **No drag-and-drop required.** Every layout choice has a control in the
   Layout drawer. Dragging (the centre bar, a floating prompt) is a shortcut.
4. **One visual grammar for every seat.** The viewer's template renders every
   board, so an opponent's creatures are always where the viewer expects them.
5. **Protocol-independent UI.** Components read one normalized client model,
   so a move to the Manabrew protocol replaces an adapter, not the UI.

## Research summary

A survey of 51 screenshots across Arena, MTGO, Forge (desktop and Android),
XMage, Cockatrice, Untap, Manabrew, NeoForge, phase-rs, argentum-engine,
Magic Workstation, Duels, Tabletop Simulator, Hearthstone, Runeterra and
Marvel Snap (screenshots are third-party and not committed). What we take:

| From | Idea |
|---|---|
| argentum-engine | Rows mirrored about a centre bar carrying phase and turn |
| Arena | One large contextual pass button in a fixed place, naming the next step; `×N` stacks of identical permanents |
| Manabrew | Empty slot outlines, zone piles per seat, minimal chrome, one priority cluster |
| MTGO (Commander) | Opponents as compact tiles with a header per seat; own board kept large |
| NeoForge | Seat header bars with big life; switching opponent focus |
| Forge / MTGO | Clickable phase stops |

What we avoid: XMage's arrow spaghetti (arrows only for the pending or
hovered item) and shrink-until-cut-off; Forge's free docking with no lock;
Arena's missing log and stack; prompts floating over the board; engine
syntax in prompt text.

Overflow survey: every client stacks identical permanents first. Past that,
Arena, Forge and XMage shrink (disliked at the extremes); MTGO and argentum
overlap. Nobody grows regions dynamically.

## Mockups

Standalone HTML mockups were built and reviewed during the session:

- **v2**: board, entity overlays, prompt dock, zone-move animation, edit mode.
- **v3**: config-driven layouts, centre splitter, 2/4/6/8 seats, Layout drawer.

They use Scryfall card art, so they are not committed. The decisions they
settled are written down in this spec.

## Visual language

| Token | Value | Meaning |
|---|---|---|
| felt | `#1c1915`, lit toward the centre | the board |
| instrument | `#14171c` / `#1b1f26` | rail, panels, prompt |
| ink | `#ece6da`, dim `#a8a195`, faint `#6f695f` | text |
| gilt | `#d4ad62` | you: your seat, your priority, primary action, selected option |
| verdigris | `#6fb7ae` | a legal choice (offered target, playable card, blocker candidate) |
| ember | `#e0533f` | attacking, damage |

Every other hue comes from cards, mana symbols and seat colours.

Typography: **Cormorant Garamond** 600/700 is the game's voice (prompt
titles, seat names, life totals, turn label). **IBM Plex Sans** is the UI.
**IBM Plex Mono** is used only for log ordinals and key captions.

Entity overlays are kept and restyled:

- P/T pill bottom-right, tinted when modified.
- Counter pills top-left (`+1/+1 ×2`, `Loyalty 3`).
- Tapped cards turn landscape while their pills stay upright.
- Summoning sickness is shown as a hatch.
- Identical-permanent piles carry a gilt `×N`.

## Sub-projects

Each one ships on its own and needs its own plan. They are listed in
dependency order.

### 0. Client model adapter and transitions

There is one normalized client model, `ClientView` + `PendingDecision` +
`Transition[]`, and every component reads only that.

- **Gorge adapter.** Today's stream sends `snapshot`, `event` and `decision`
  frames (`web/src/lib/stream.ts`). Explicit events (`MoveZone`, `Draw`,
  `PutOnStack`, damage, life and counters) map directly to transitions.
  Objects keep their `ObjID` across zone moves (`events/apply.go` `MoveZone`).
- **Manabrew adapter** (future). The Manabrew protocol
  (docs.manabrew.app/protocol) sends full `gameView` snapshots and typed
  prompts, with no event log; its `display` channel is still work-in-progress.
  Transitions come from comparing the previous and next snapshot by card `id`,
  which stays stable across zones, hidden cards included. The comparison emits
  moved A→B, tapped/untapped, damage, counters, life, and stack push/pop.
- **Transitions feed the animation queue (sub-project 5) and the log.** Under
  Manabrew, until `display` lands, the log is built from these transitions.
- **Replay/DVR.** Under Manabrew the client keeps its own ring of snapshots,
  because the protocol has no log.

### 1. Flow profiles and keymap

This extends `web/src/lib/profiles.ts` and `playsettings.ts`; it does not
replace them.

- **Flow profile** = everything that affects game flow: autopass rules,
  per-step stops (yours and opponents'), pacing and pause timing, trigger
  auto-ordering, pass-after-act, and breakpoints.
- **Breakpoints, first cut.** Pause when:
  1. something targets you or a permanent you control;
  2. a card on the name watchlist is cast or its ability goes on the stack;
  3. creatures attack *you* (other players' combats are skipped);
  4. the stack reaches N items.

  They are evaluated client-side in `autopilot.ts` as a pure function of
  (view, profile) that returns "stop, because X". Under Manabrew, the
  autopilot still answers each `chooseAction` itself. `pass.until` and
  `exhaustStack` are used only when no breakpoint could fire.
  - Breakpoints stop persistent Auto, the one-shot runs and fast-forward.
    Each one fires once for the thing it caught, and stays stopped on that
    same window however often it is re-checked.
  - A yield, or the Resolve All baseline, exempts the top object from the
    top-object breakpoints.
  - Arming a one-shot run (End Turn, Hard Skip, Resolve All) on a paused
    window acknowledges that pause, so the run proceeds.
  - The Manual seat's empty-window auto-skip honours breakpoints too.
- **Keymap.** One global keymap. Every action can have 0–2 bindings, matched
  by physical key code as today. The editor warns on conflicts rather than
  blocking them. There is a reset to defaults and a `?` cheat-sheet overlay.
  Today's bindings stay as the defaults. New actions:

  | Group | Actions |
  |---|---|
  | Priority | Undo; pass until my turn; pass until stack empty |
  | Decisions | Pick option 1–9; confirm/done; attack with all; no blocks; auto-pay |
  | Profiles | Flow profile 1–9; layout profile 1–9 or next/prev |
  | View | Toggle log; toggle stacking; zoom card under cursor; open grave/exile of the hovered seat |

  Sub-project 1 wires the priority, decision (pick 1–9, confirm) and
  profile actions, plus the cheat sheet. Actions that need UI from later
  sub-projects arrive with that UI:
  - attack with all, no blocks and auto-pay come with sub-project 4
    (prompts);
  - toggle log, toggle stacking, layout 1–9, zoom card and open grave/exile
    come with sub-projects 2 and 3.

  Binding rules:
  - Pick 1–9 answers the Nth numbered row in on-screen order. Sub-project 4
    numbers every renderer's rows by rendered order (`lib/prompts/order.ts`),
    so library search, name pick and the payment window answer the digit
    too; priority windows, which are not numbered, refuse it.
  - Numpad Enter counts as Enter, and Shift+Space also passes. Alt variants
    of the default Space/Enter chords are dropped, since they are OS and
    browser chords; Alt remains a usable modifier for bindings the player
    defines. Escape can be bound only to cancel-run.
- **Storage and migration**:
  - The existing keys are kept, not renamed. `gorge.playsettings.v1` and
    `gorge.playsettings.profiles.v1` move to settings blob version 3, in
    place.
  - Blob versions 1 and 2 load with breakpoints off.
  - The keymap is new, at `gorge.keymap.v1`. It stores only the overrides of
    the defaults.
  - Renaming keys was rejected: it would orphan saved settings, which is the
    same reason the v1→v2 bump kept its key.
  - Profile names `__proto__`, `constructor` and `prototype` are refused.
- **Export/import.** One JSON file per library:
  `{kind: 'gorge-flow' | 'gorge-layout' | 'gorge-keymap', version, items}`.
  Imports are validated with the store's own `validate`. A name clash gets the
  suffix " (2)".

### 2. Layout profiles

A separate named library, `gorge.layouts.v1`, which absorbs today's
`layoutsettings.ts`. A layout profile is one serialisable config:

- **Table.**
  - `split`: the opponents' share of the board height, set by dragging the
    centre bar or with a slider. Double-click resets it.
  - Opponent arrangement:
    - **Columns**, one row.
    - **Grid**: two rows once there are more than three opponents.
    - **Focus**: one opponent full-size. The others are *side strips*
      (header plus a creatures-only row); clicking a strip focuses it.
  - Opponent orientation: **Mirrored** (default; cards always face the viewer)
    or **Same as mine**.
- **Board template.** Regions for creatures, lands and other permanents. Each
  region has:
  - a row (1–3);
  - a width weight;
  - an anchor to fill from: start, centre or end;
  - an order: entry order (default for creatures), name (default for lands)
    or power.

  Ties break by entry order, so equal keys never reshuffle. Only entry order
  never moves existing cards; the drawer says so.
- **Cards.**
  - Stack identical permanents: on by default, and toggleable from the
    actions bar and by hotkey.
  - When a row is full: **overlap** (default), **scroll** or **wrap**. A wrap
    that runs out of rows falls back to overlap, so no permanent is ever
    hidden.
  - Art tiles below a width threshold.
  - How much of the hand is visible.
- **Panels.** The stack/log rail on the left, right or hidden. Log shown or
  hidden. Prompt placement: rail dock or floating, with the floating position
  saved.
- **Presets.** Duel, Duel 3 rows, Commander 4, 6 players grid, 8 players focus.

**Sizing rules** (these replace per-zone scale):

- Card height per seat panel = (rows box height − gaps) ÷ rows. It is then
  capped so the fullest row fits with at most ~40% overlap.
- **All opponents share the smallest opponent card size**, so the table reads
  evenly. The viewer's own board is sized separately.
- Below the art-tile threshold a card renders as an art crop with a name strip
  and keeps its P/T and counter pills. This is a first-class rendering, not a
  fallback.
- The viewer's side is its rows plus a peeking hand row, all from one row
  unit. There is no fixed hand band.
- The zone piles column (command, library, graveyard, exile) is sized from
  the panel height. It is hidden when cards are art tiles and in focus side
  strips, where the header counts replace it.

Seat-count layouts replace `seatCorner`/`CELL`/`OUTER`/`FACING`/`CORNER`,
which are four duplicated corner maps, with one arrangement function.

### 3. Board and entity visuals

- **Seat box** for the viewer: avatar, serif name, big life total, hand,
  library, grave and exile counts, and mana pool.
- **Header bar** per opponent: avatar, name, turn badge, counts and life.
- **Target and attack states.** A seat box or bar glows verdigris when it is
  targetable and ember when it is attacked.
- **Centre strip.**
  - Turn label and phase track, with your stops dotted.
  - Pills naming the active flow profile and layout profile; clicking one
    opens its drawer.
  - It is also the splitter.
- **One action button**, gilt, in a fixed corner. It names what passing does
  ("Pass — Move to combat", "Let Rhystic Study resolve"). It becomes
  "Waiting" while a prompt needs an answer. Secondary buttons above it: Undo,
  Until my turn, End turn.
- **Card preview.** A hover popover beside the card, replacing a permanent
  preview panel.
- **Arrows.**
  - Drawn only for the pending decision, the hovered stack item, or declared
    combat.
  - Verdigris for a choice being made, ember for attacks.
  - Under Manabrew, a target's `intent` (damage/destroy/bounce/…) may pick the
    arrow's colour or icon.

### 4. Prompt system

One renderer per decision kind, replacing the generic branch in
`SeatPanel.svelte` and `lib/seatpanel.svelte.ts`.

**Anatomy:**
- The source card's art as a banner (rail dock) or spine (floating).
- A source line, e.g. "Mira's Rhystic Study · triggered ability".
- A serif title that asks the question: "Choose two", "Pay {1} for Rhystic
  Study?", "Declare blockers".
- **One plain-language line** saying what happens.
- Numbered options (`1`–`9` keys).
- A footer with live state ("2 of 2 chosen", "you'd take 5 (17 → 12)") and the
  primary and secondary buttons.

**Placement:** docked at the top of the rail by default, next to the stack it
concerns, so the board stays clear. Floating is a layout setting; the prompt
is dragged by its grip, and the position is saved in the layout profile.

**Kinds:**
- `priority` is the action button and does not use the dock.
- `target` offers chips with art thumbnails, highlights legal targets on the
  board, and draws an arrow on hover.
- `modes` is a multi-select with a count. `choose` and `replacement` are
  single-select.
- `attackers` and `blockers` are assigned by clicking the board, and the dock
  summarises the assignments.
- `mulligan`, `arrange`, `trigger_order`, `trigger_optional`,
  `commander_zone` and `starting_player` each get a renderer following the
  same anatomy.

**Text quality is a server contract.** A prompt title or description must
never contain engine syntax such as `Sac<…>`. The gorge side should add a test
that rejects raw cost or filter syntax in `decision.prompt`. Under Manabrew
this text comes from `presentation.title/description/text`.

### 5. Motion

- **Zone moves animate as a flight** from the source to the destination, with
  the destination pile's count bumping when the card lands. The mockup showed
  hand → stack, stack → graveyard, battlefield → graveyard and
  library → hand.
- **Damage shows a floating number and a short shake.** Life changes tick.
- **A queue plays transitions in order.** A batch (a board wipe, a
  mass-return) is staggered, and the queue has a length cap: past it, the
  batch collapses to one group flight.
- **Speed is user-set:** off, fast or normal. `prefers-reduced-motion` forces
  off.
- **Motion never blocks input.** A decision arriving mid-animation finishes
  the queue at once.

## Out of scope

- Server or protocol changes, apart from the prompt-text contract test.
- Deck builder and lobby screens.
- Free-form placement of individual cards; regions are rule-based.
- Share codes (export and import go through a file first).

## Risks

- **Two stores in flight.** Flow profiles and layout profiles both migrate
  existing localStorage keys, so each migration needs a test on a real
  pre-migration blob.
- **Measured layout costs time.** Re-measuring on every splitter frame
  re-renders the board in the mockup; production should update only CSS
  variables while dragging.
- **The large files get larger.** `SeatPanel.svelte` (1431 lines),
  `seatpanel.svelte.ts` (2300+) and `PlaySettingsPanel.svelte` (985) would
  grow; sub-projects 1 and 4 must split them, not extend them.
- **Manabrew's `display` channel may change** what sub-project 0 needs to
  derive. The adapter boundary contains that change.
