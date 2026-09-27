import type { Decision, Intent, Option, PaymentAction, PaymentPlan, View } from '../protocol';
import { fetchPending, postIntent, ApiError } from './api';
import { safeStorage } from './storage';
import type { SeatCtx } from './seat';
import { STOPPABLE_STEPS, actionables, decide, emptyPriorityWindow, isActionKind, type StopReason, type Stops, type TurnSide } from './autopilot';
import {
  applyPreset,
  defaultSettings,
  loadSettings,
  presetPatch,
  saveSettings,
  withChange,
  type PlaySettings,
  type PresetName,
  type StepStop,
  type StoppableStep,
} from './playsettings';
import { autoPassLogText, pushAutoPassLog, type AutoPassKind, type AutoPassLog } from './autolog';
import { loadYields, saveYields } from './yields';
import { clientBreadcrumbs } from './breadcrumbs';
import {
  emptyRemembered,
  loadRemembered,
  rememberable,
  rememberChoiceFor,
  rememberKey,
  saveRemembered,
  triggerPromptLabel,
  withRemember,
  withoutRemember,
  type RememberedStore,
} from './remembered';

/**
 * actedOption reports whether the posted `choices` (wire indices) contain at
 * least one real action on a priority decision — an option whose kind
 * passes isActionKind (autopilot.ts): neither pass, nor concede, nor
 * activate. The kind comes off the wire option itself, resolved by index; a
 * choice that names no option on the decision is not an action. This is the
 * arming test for pass-after-acting, and it is the per-choice counterpart of
 * `actionable`'s per-decision test — the SAME shared predicate (isActionKind),
 * so the two cannot drift apart again. The activate exclusion is the fix for
 * fb-3ab6d9da. The wire fact it rests on: in a PRIORITY decision, Kind
 * "activate" is only ever the tap-for-mana option (rules/legal.go's
 * availableManaAbilities loop — `add("activate", "Tap <name> for mana", id)`);
 * non-mana activated abilities are offered as Kind "ability" (legal.go's
 * ability loop), and the one other "activate" on the wire (cast.go's mid-cast
 * mana-source ask) sits on a non-priority decision, which the kind test above
 * already refuses. A mana tap therefore arms nothing: it is not "I am done
 * acting" — it is the prelude to acting in the NEXT window, the one where the
 * freshly floated mana makes the held spell affordable and the engine offers
 * the cast. Counting the tap as an action machine-passed exactly that window,
 * so the player never saw the spell become playable. An earlier version of
 * this comment claimed to mirror `actionable`'s kind test while inlining a
 * test that did not — now both call the one shared predicate.
 * (Moved here from actpass.ts, which prio3 deleted with the per-table keys.)
 */
export function actedOption(d: Decision, choices: number[]): boolean {
  if (d.kind !== 'priority') return false;
  return choices.some((i) => {
    const o = d.options.find((opt) => opt.index === i);
    return o !== undefined && isActionKind(o.kind);
  });
}

/**
 * SeatPanelState is everything a human seat answers with. It holds the
 * pending decision (adopted from view.decision, refreshed from /pending),
 * the user's picked options, the concede-confirmation and posted states,
 * and posts the intent. It is deliberately rules-ignorant (R-E4-2): the
 * options are the server's verbatim, the only selection constraints are the
 * decision's own min/max, and no option is ever chosen by position — the
 * primary button resolves its option by kind, never by index (R-E4-1), and
 * the concede option is the LAST one on the wire precisely so that a client
 * which defaulted to the last option would concede on the very first
 * priority window. This module never does that: nothing selects, preselects
 * or auto-submits an option it was not explicitly handed by a user click.
 */

/**
 * pickOption is the pure heart of the seat's selection logic: it applies one
 * click identified by the option's wire index to the current picked index set and returns the
 * resulting set. It is expressed ONLY in terms of the decision's own min/max
 * (applied elsewhere) and the option Group field, never in terms of what a
 * Group's members are (R-E4-2) — the panel never learns what a blocker is.
 *
 *  - A repeatable decision (`d.repeatable` — a CanRepeatModes$ Charm,
 *    CR 601.2b) is an ORDERED MULTISET over the distinct options: clicking an
 *    already-picked option appends another instance instead of toggling it
 *    off, so with CharmNum$ 3 over 2 legal modes the same mode can fill all
 *    three slots (the ask's Min may exceed the option count — exactly the
 *    shape where toggle-off would make the submit gate unreachable). The max
 *    is the only cap; a picked instance is removed through unpickOption.
 *  - Otherwise, if the clicked option is already picked, it is removed (toggle
 *    off).
 *  - Otherwise, if it carries a non-empty Group already represented in
 *    `picked`, that previously-picked group member is REPLACED by the new
 *    option: moving one blocker from attacker A to attacker B just works,
 *    and at most one option of a Group is ever held.
 *  - Otherwise it is appended.
 */
export function pickOption(d: Decision, index: number, picked: number[]): number[] {
  const opt = optionAt(d, index);
  if (opt === undefined) return [...picked];
  if (d.repeatable) {
    if (picked.length >= d.max) return [...picked];
    return [...picked, index];
  }
  const at = picked.indexOf(index);
  if (at >= 0) return picked.filter((i) => i !== index);
  const g = opt.group;
  if (g) {
    // The per-Group cap: groupLimit raises the exclusivity marker from "at
    // most one" to "at most N" (Decision.GroupLimit, Forge's EACH per-type
    // ChangeNum), and groupLimits raises ONE named group above even that
    // (Decision.GroupLimits, a per-defender attack ceiling). At the default
    // cap the group is already represented, so the pick REPLACES that member;
    // above it the pick appends until the cap is full, then replaces the
    // oldest member.
    const cap =
      d.groupLimits?.[g] ?? (d.groupLimit && d.groupLimit > 1 ? d.groupLimit : 1);
    const members = picked.filter((i) => optionAt(d, i)?.group === g);
    if (members.length >= cap) {
      // Replace: drop the oldest group member, keep the rest's click order,
      // and put the freshly picked option at the end.
      return picked.filter((i) => i !== members[0]).concat(index);
    }
  }
  return [...picked, index];
}

/**
 * unpickOption removes ONE instance of an option from a repeatable pick
 * multiset — the picked-so-far chips' remove affordance. The LAST occurrence
 * in click order goes (the click that added it is the most recent intent),
 * the other instances keep their order; an unpicked index is a no-op.
 */
export function unpickOption(index: number, picked: number[]): number[] {
  const at = picked.lastIndexOf(index);
  if (at < 0) return [...picked];
  return picked.filter((_, i) => i !== at);
}

/** primaryOf resolves the "primary" option by kind — pass/resolve — never by position (R-E4-1). */
export function primaryOf(d: Decision): Option | null {
  for (const o of d.options) {
    if (o.kind === 'pass' || o.kind === 'resolve') return o;
  }
  return null;
}

export function isConcede(o: Option): boolean {
  return o.kind === 'concede';
}

/** paymentActionForBase keeps the additive payment offer attached to its
 * legacy cast.  It deliberately compares the wire index, never display text:
 * labels are presentation and may change without changing the action. */
export function paymentActionForBase(d: Decision, index: number): PaymentAction | undefined {
  return d.payment_actions?.find((action) => action.base_option_index === index);
}

/** paymentPlanSummary is presentation only.  The offered witness remains the
 * value sent to the server; this string never participates in selection. */
export function paymentPlanSummary(plan: PaymentPlan): string {
  if (plan.activations.length === 0) return 'Use floating mana';
  const produced = plan.activations.flatMap((step) => step.produces.map((n, i) => n > 0 ? `${n > 1 ? n : ''}${['W', 'U', 'B', 'R', 'G', 'C'][i]}` : '').filter(Boolean));
  return `Tap ${plan.activations.length} ${plan.activations.length === 1 ? 'source' : 'sources'} for ${produced.join(' + ') || 'mana'}`;
}

/** optionAt resolves a wire option by its own index, never by array position (R-E4-1). */
function optionAt(d: Decision, index: number): Option | undefined {
  return d.options.find((o) => o.index === index);
}

/**
 * MANA_FOLLOW_UP_KINDS is the one eligibility rule for arming the card
 * follow-up expectation, measured from the engine: the only decision
 * resolveCardFollowUp can open (2-6 all-'mana' options on one object) is
 * posed by rules/mana_activation.go, and only two answers lead straight
 * into it for the SAME object:
 *
 * - `activate` -- activateManaFor, entered only from an `activate` option,
 *   whatever decision carries it: the priority window (rules/legal.go), the
 *   CR 601.2g mid-cast mana window, and the ward, cumulative-upkeep and
 *   unless-payment windows (all `choose` decisions). It poses the stage-1
 *   ability pick (`mana` options on the source) or goes straight to
 *   askManaColor (a Treasure's colour ask on the source).
 * - `mana` -- the stage-1 ability pick; answering an Any / Combo Any /
 *   Chosen ability poses askManaColor's stage-2 wheel on the same source
 *   (fb-e079def5: Talisman, Vivid Marsh).
 *
 * The rule reads option kinds, never the decision kind, because the same
 * `activate` shape appears on priority and on choose windows. Nothing else
 * reaches a same-object mana ask: an `ability` option goes on the stack (mana
 * abilities are offered only as `activate`), and target (`permanent`/
 * `player`), arrange (`card`), cast, sacrifice and mode answers do not arm.
 */
const MANA_FOLLOW_UP_KINDS: ReadonlySet<string> = new Set(['activate', 'mana']);

/**
 * followUpArm returns the expectation a hand post arms: the first answered
 * option whose kind can hand back a same-object mana ask
 * (MANA_FOLLOW_UP_KINDS) and that carries an obj. The obj is read from the
 * answered option itself (R-E4-1), never rebuilt from position. Anything
 * else returns null, which disarms.
 */
export function followUpArm(d: Decision, choices: number[]): { seq: number; obj: number } | null {
  for (const index of choices) {
    const option = optionAt(d, index);
    if (option !== undefined && MANA_FOLLOW_UP_KINDS.has(option.kind) && option.obj !== undefined) {
      return { seq: d.seq, obj: option.obj };
    }
  }
  return null;
}

/**
 * Tone is how loudly the panel presents its state, and it is resolved from
 * option KINDS alone — never a label, never a position (R-E4-1).
 *
 * `offered` is a window this seat may decline: the decision carries a `pass`
 * option, so doing nothing is a legal answer and the game moves on without
 * you. `initiative` is a decision the game is blocked on — a target, a
 * mulligan, a block assignment, a mode — where there is no pass and nothing
 * happens anywhere at the table until this seat answers. Those two deserve
 * different colours because they demand different things of the player, and
 * painting them alike is what made the old panel unreadable across a room.
 */
export type Tone = 'initiative' | 'offered' | 'idle';

export function toneOf(d: Decision | null): Tone {
  if (d === null) return 'idle';
  return d.options.some((o) => o.kind === 'pass') ? 'offered' : 'initiative';
}

/**
 * triggerOrderPermutation is the SHAPE contract every auto-order path shares
 * (fb-trigorder1 generalised the prio6 check): a trigger_order decision whose
 * min == max == options.length (the engine's permutation contract, Ruling U2)
 * with at least two options — a one-trigger ask is never posed. The check is
 * grounded in the real wire shape, measured on a live decision
 * (rules/trigger_queue.go's askTriggerOrder): each option is `kind: "trigger"`
 * with `label: "<source name>: <TriggerDescription>"`.
 */
export function triggerOrderPermutation(d: Decision): boolean {
  if (d.kind !== 'trigger_order') return false;
  if (d.min !== d.max || d.max !== d.options.length) return false;
  return d.options.length >= 2;
}

/**
 * identicalTriggerOrder reports whether a trigger_order decision's EVERY
 * option describes the same trigger — the same source name and the same
 * text (prio6) — on top of the shared triggerOrderPermutation shape
 * contract.
 *
 * Caveat, measured rather than assumed away: the label carries no target
 * information, so two identical-name/text triggers aimed at DIFFERENT
 * targets also read as identical. That is the brief's definition
 * deliberately — between two copies of the same trigger the order is
 * immaterial — and it is stated here so nobody mistakes the test for a
 * target-aware one.
 */
export function identicalTriggerOrder(d: Decision): boolean {
  if (!triggerOrderPermutation(d)) return false;
  const first = d.options[0].label;
  return d.options.every((o) => o.label === first);
}

/**
 * MulliganPhase names which half of the London round a `mulligan` decision is
 * in, so the seat panel can lay it out. The two halves are told apart by their
 * option KINDS — `keep`/`mulligan` in the first, `bottom` in the second — and
 * never by option count, position or label text (FL-101).
 *
 * A mulligan decision carrying any other kind returns null and falls back to
 * the generic option list, so an option this layout does not understand is
 * still reachable rather than silently dropped.
 */
export type MulliganPhase =
  | { phase: 'keep'; choices: Option[] }
  | { phase: 'bottom'; cards: Option[] }
  | null;

export function mulliganPhase(d: Decision | null): MulliganPhase {
  if (d === null || d.kind !== 'mulligan' || d.options.length === 0) return null;
  if (d.options.every((o) => o.kind === 'keep' || o.kind === 'mulligan')) {
    return { phase: 'keep', choices: d.options };
  }
  if (d.options.every((o) => o.kind === 'bottom')) {
    return { phase: 'bottom', cards: d.options };
  }
  return null;
}

/**
 * AUTO_PASS_CAP bounds how many priority windows auto may pass in an
 * unbroken run before it switches itself off. A runaway autopasser is not a
 * cosmetic bug: it hammers the server and it passes the game away in
 * silence. Forty is roughly two turn cycles of an uneventful four-seat
 * game — long enough that a normal quiet stretch never trips it, short
 * enough that a stuck loop is caught in seconds.
 */
export const AUTO_PASS_CAP = 40;

/**
 * AutoOffReason is why auto is no longer running, as distinct from
 * StopReason (why auto declined THIS window but stays armed). The two are
 * separate vocabularies because they need separate words on screen: one is
 * "waiting for you here", the other is "auto is off now".
 */
export type AutoOffReason = 'loop' | 'cap';

/**
 * AutoNote is the one line the panel shows about what auto is doing. It is
 * an enum-shaped value, never a string to print: autoNoteText turns it into
 * words, so no StopReason identifier can reach the screen.
 */
export type AutoNote =
  | { kind: 'off' }
  | { kind: 'paused' }
  | { kind: 'skip-off'; reason: AutoOffReason }
  | { kind: 'skipped'; count: number }
  | { kind: 'armed' }
  | { kind: 'passing'; count: number }
  /**
   * detail (fb-20260916T225211Z) carries the actionable option labels that
   * made a stop-set window stop-worthy — autoNoteText folds it into the
   * note text, so the player reads WHAT the window offered, not just that a
   * stop they set fired. Absent (or empty) for every other reason and for a
   * 'forced' stop with nothing to do — the base wording is complete there.
   */
  | { kind: 'waiting'; reason: StopReason; detail?: string }
  | { kind: 'stopped'; reason: AutoOffReason }
  | { kind: 'end-turn-armed' }
  | { kind: 'end-turn-passing'; count: number }
  | { kind: 'end-turn-stopped'; reason: StopReason | AutoOffReason }
  | { kind: 'skip-turn-armed' }
  | { kind: 'skip-turn-passing'; count: number }
  | { kind: 'skip-turn-stopped'; reason: StopReason | AutoOffReason }
  | { kind: 'resolve-all-armed' }
  | { kind: 'resolve-all-passing'; count: number }
  | { kind: 'resolve-all-stopped'; reason: StopReason | AutoOffReason }
  | { kind: 'act-passed'; count: number };

const WAITING_TEXT: Record<StopReason, string> = {
  'disabled': 'Auto is off.',
  'not-priority': 'Auto is waiting: this decision needs you, not a pass.',
  'unexpected-shape': 'Auto is waiting: it does not recognise this window.',
  'stop-set': 'Auto stopped here: you set a stop on this step.',
  'opponent-object': "Auto stopped here: an opponent's object is on the stack and you can respond.",
  'own-object': 'Auto stopped here: your own object is on the stack and you can respond.',
};

const OFF_TEXT: Record<AutoOffReason, string> = {
  'loop': 'Auto switched itself off: the same decision came back after it answered. Press the Auto switch to rearm it.',
  'cap': `Auto switched itself off after ${AUTO_PASS_CAP} passes in a row. Press the Auto switch to rearm it.`,
};

/**
 * RUN_*_TEXT re-words a stop reason for the one-shot runs, which are not
 * "waiting" — a stop ENDS an End Turn / hard-skip run. The wording is
 * reason-loyal (the same fact the Auto wording states), only re-anchored.
 */
const RUN_WAITING_TEXT: Record<StopReason, string> = {
  'disabled': 'the Auto switch is off.',
  'not-priority': 'this decision needs you, not a pass.',
  'unexpected-shape': 'it does not recognise this window.',
  'stop-set': 'you set a stop on this step.',
  'opponent-object': "an opponent's object is on the stack and you can respond.",
  'own-object': 'your own object is on the stack and you can respond.',
};
const RUN_OFF_TEXT: Record<AutoOffReason, string> = {
  'loop': 'the same decision came back after it answered.',
  'cap': `it passed ${AUTO_PASS_CAP} windows in a row.`,
};

/** autoNoteText renders an AutoNote as plain words. No enum identifier ever reaches the screen. */
export function autoNoteText(note: AutoNote): string {
  switch (note.kind) {
    case 'off':
      return 'Auto is off. You answer every window that offers you something to do.';
    case 'paused':
      return 'Undo paused automatic passing so it cannot re-answer the window you rewound to. Press the Auto switch (or apply a preset) to start it again.';
    case 'skip-off':
      return `${OFF_TEXT[note.reason]} Empty windows are no longer skipped either.`;
    case 'skipped':
      return note.count === 1
        ? 'Passed 1 window where you had nothing to do.'
        : `Passed ${note.count} windows where you had nothing to do.`;
    case 'armed':
      return 'Auto is on. It passes windows where you have nothing to do, and stops at your stops.';
    case 'passing':
      return note.count === 1
        ? 'Auto passed 1 priority window.'
        : `Auto passed ${note.count} priority windows.`;
    case 'waiting':
      // stop-set with actionable labels (fb-20260916T225211Z): the base line
      // alone read "you set a stop" without saying WHY the window was worth
      // stopping at — the exact gap the Deadly Rollick free-cast report is
      // about. The labels come from actionables(), the same predicate the
      // smart step rule consulted, so the note cannot name something the
      // stop did not actually stop for. Derived from the base string (the
      // trailing full stop is dropped, the clause spliced in) so the wording
      // stays in one place.
      if (note.reason === 'stop-set' && note.detail) {
        return `${WAITING_TEXT[note.reason].replace(/\.$/, '')} and you can act — ${note.detail}.`;
      }
      return WAITING_TEXT[note.reason];
    case 'stopped':
      return OFF_TEXT[note.reason];
    case 'end-turn-armed':
      return 'End Turn: passing the rest of this turn — it still stops for opponent plays. Esc cancels.';
    case 'end-turn-passing':
      return note.count === 1
        ? 'End Turn passed 1 priority window.'
        : `End Turn passed ${note.count} priority windows.`;
    case 'end-turn-stopped':
      return `End Turn stopped: ${
        note.reason in RUN_WAITING_TEXT
          ? RUN_WAITING_TEXT[note.reason as StopReason]
          : RUN_OFF_TEXT[note.reason as AutoOffReason]
      }`;
    case 'skip-turn-armed':
      return 'Skipping turn — Esc to stop.';
    case 'skip-turn-passing':
      return note.count === 1
        ? 'Skipping turn: passed 1 priority window.'
        : `Skipping turn: passed ${note.count} priority windows.`;
    case 'skip-turn-stopped':
      return `Skipping turn stopped: ${
        note.reason in RUN_WAITING_TEXT
          ? RUN_WAITING_TEXT[note.reason as StopReason]
          : RUN_OFF_TEXT[note.reason as AutoOffReason]
      }`;
    case 'resolve-all-armed':
      return 'Resolve All: passing the stack as it stands — a NEW opponent play or a decision that needs you stops it. Esc cancels.';
    case 'resolve-all-passing':
      return note.count === 1
        ? 'Resolve All passed 1 priority window.'
        : `Resolve All passed ${note.count} priority windows.`;
    case 'resolve-all-stopped':
      return `Resolve All stopped: ${
        note.reason in RUN_WAITING_TEXT
          ? RUN_WAITING_TEXT[note.reason as StopReason]
          : RUN_OFF_TEXT[note.reason as AutoOffReason]
      }`;
    case 'act-passed':
      return note.count === 1
        ? 'Passed 1 priority window after your action.'
        : `Passed ${note.count} priority windows after your action.`;
  }
}

/** runStopNote is a one-shot run's stopped note, worded in that run's own register. */
function runStopNote(mode: 'end-turn' | 'hard-skip' | 'resolve-all', reason: StopReason | AutoOffReason): AutoNote {
  const kind = mode === 'end-turn'
    ? 'end-turn-stopped'
    : mode === 'resolve-all'
    ? 'resolve-all-stopped'
    : 'skip-turn-stopped';
  return { kind, reason } as AutoNote;
}

/**
 * safeSessionStorage is sessionStorage under the same guard — the handle for
 * the YIELD store only (prio6), deliberately separate from the persisted
 * play settings' localStorage (whose ONE shared handle lives in
 * lib/storage.ts): a yield is session-scoped (review r2 — it
 * must die with the browser session, not outlive it the way a localStorage
 * record would).
 */
function safeSessionStorage(): Storage | null {
  try {
    return typeof sessionStorage === 'undefined' ? null : sessionStorage;
  } catch {
    return null;
  }
}

export class SeatPanelState {
  readonly table: string;
  readonly ctx: SeatCtx;
  private readonly storage: Storage | null;
  /** The YIELD store's own handle: sessionStorage, never the settings' localStorage (review r2). */
  private readonly yieldStorage: Storage | null;

  constructor(
    table: string,
    readonly match: number,
    ctx: SeatCtx,
    storage: Storage | null = safeStorage(),
    yieldStorage: Storage | null = safeSessionStorage(),
  ) {
    this.table = table;
    this.ctx = ctx;
    this.storage = storage;
    this.yieldStorage = yieldStorage;
    // The settings load here rather than at mount so the very first render
    // — and every test — sees the player's saved preferences. SSR and a
    // browser that refuses site data both pass null and get casual.
    this.settings = loadSettings(storage);
    // The remembered answers load here for the same reason (part B): the
    // first adopted decision can already be one the player asked the client
    // to remember. SSR gets the empty store — no answer ever fires there.
    this.remembered = loadRemembered(storage);
    // The yields seed from the per-GAME store (lib/yields.ts): memory first
    // (this tab already yielded something in THIS match), then sessionStorage
    // (a reload of the same match), else empty. The scope is table + match,
    // so a new match on the same table reads empty — a yield granted in game
    // N never auto-passes in game N+1 (review r2).
    this.yieldList = [...loadYields(table, match, yieldStorage)];
    clientBreadcrumbs.setPlay(this.settings, this.yieldList);
  }

  /** pending is the decision this seat must answer right now, or null when the game is waiting on someone else. */
  pending = $state<Decision | null>(null);
  /** picked holds the chosen option indices, in click order: for a permutation decision the ORDER is the answer (Chosen returns options in client order), so picks must never be reordered. */
  picked = $state<number[]>([]);
  /** postedSeq is the seq of the last intent the server accepted; while the pending decision has this seq the answer is in and the options stay hidden. */
  postedSeq = $state<number | null>(null);
  /**
   * followUpExpected is the ONE armed card-follow-up expectation, and this
   * state is its one home because it is the one place BOTH posting surfaces
   * meet: a hand click on a card's tile posts through click() exactly as a
   * click on the panel's own option button does, so arming here makes the
   * tile and the panel behave identically (fb-20260923T050205Z: a treasure
   * activated from the seat panel never armed it, so the colour ask fell
   * back to the panel's plain button list instead of raising the radial mana
   * wheel).
   *
   * The one rule: a HAND post whose answered option is of a kind that can
   * hand back a same-object mana ask (followUpArm / MANA_FOLLOW_UP_KINDS:
   * `activate` or `mana`) arms the expectation with that option's own obj
   * (R-E4-1 — never a rebuilt position). Machine posts (auto, the one-shot runs, the empty-window
   * floor, passClick/primaryClick, the auto-order and remembered-trigger
   * submits) arm nothing: they post pass/resolve answers, whose options carry
   * no obj, and they never reach a hand post's arming branch regardless.
   *
   * The decoder is not here — the owner (Table.svelte) receives each arm
   * through onFollowUpArm, mirrors it into its own $state and decodes it with
   * cardoptions.resolveCardFollowUp, whose kind gate (2-6 all-'mana' options
   * on the expected obj) is what keeps every other follow-up off the wheel.
   *
   * Deliberately a PLAIN field, not $state, and handed out through a callback
   * rather than read by the route's effect: reading the panel (a $derived in
   * Table) or any of its state from that effect adds a reactive edge that
   * re-ran it across the starting_player -> mulligan transition and tripped
   * SeatPanel's null-ctx read (agent-20260924T114750Z-ba517e3b), hanging the
   * seated page. The effect's dependencies stay exactly the route's own.
   */
  followUpExpected: { seq: number; obj: number } | null = null;
  /** onFollowUpArm is called with every hand post's arm (null disarms); the route mirrors it into its decode effect's own state. */
  onFollowUpArm: ((arm: { seq: number; obj: number } | null) => void) | null = null;
  /** confirming arms the concede option's required second confirmation (R-E4-1). */
  confirming = $state(false);
  /** error surfaces a rejected intent — never swallowed (a stale seq must be seen and recovered from, not silently dropped). */
  error = $state<string | null>(null);
  busy = $state(false);
  /** Auto-pay is intentionally local to this seat-panel instance.  Unlike
   * play settings it is not persisted, so a different seat or match starts
   * manual and toggling cannot send an engine intent. */
  autoPayMana = $state(false);
  /** Set from the table's explicit auto_mana capability. */
  autoManaAvailable = $state(false);

  setAutoManaAvailable(on: boolean) {
    this.autoManaAvailable = on;
    if (!on) this.autoPayMana = false;
  }

  setAutoPayMana(on: boolean) {
    this.autoPayMana = this.autoManaAvailable && on;
  }

  // ---- autopilot ------------------------------------------------------
  //
  // decide() (lib/autopilot) is pure and already tested; what lives here is
  // the LOOP around it, which is the dangerous half. A mis-firing
  // autopasser loses a game in silence, so every field below exists to make
  // it stop rather than to make it go.

  /**
   * settings is the player's play settings (playsettings.ts): the presets,
   * the per-step stop rules, the opponent-object rules and pass-after-acting
   * in ONE object — the source of truth for every machine decision this
   * state makes. `auto` is settings.autoPass, the stops are settings.steps,
   * actPass is settings.passAfterAct. It is loaded from the ONE global
   * localStorage key at construction (SSR has no storage and gets the
   * defaults, casual) and saved on every change, so a preference survives a
   * reload and a match boundary: begin() must not — and does not — reset it.
   */
  settings = $state<PlaySettings>(defaultSettings());

  /**
   * machinePaused is the runaway brake, and it is deliberately NOT a
   * settings change: the loop guard and the pass cap set it, and only the
   * player's own Auto switch clears it. Flipping the persisted autoPass from
   * a guard would have written a preference the player never chose — a
   * reload would then come back with auto off for good.
   *
   * rewind() (an UNDO) sets it too: rewinding is the player deliberately
   * taking the controls back, so the machine must not instantly re-answer
   * the restored window (auto, the empty-window floor, pass-after-acting,
   * the identical-trigger auto-order — all of them read this brake). The
   * pause is session-scoped like the runaway brake: the persisted
   * settings.autoPass survives, a reload comes back as the player left it,
   * and the same resume paths clear it (the pause-aware Auto switch — see
   * pressAuto — and a named preset). A one-shot run is NOT a resume path:
   * while the pause holds, startRun refuses to arm, so a run press can
   * never re-enable machine posting on the window the player just rewound
   * to.
   */
  machinePaused = $state(false);

  /**
   * skipEmpty is the empty-window floor, and it is ON by default -- the one
   * thing the panel does for the player without being asked. A priority
   * window whose only options are pass and concede asks nothing: there is no
   * action to take, so collecting a click there is pure friction between the
   * player and the next real decision. It is separate from `auto` because it
   * is a different promise: auto decides FOR you (a persisted preference),
   * whereas this only declines to interrupt you when there was nothing to
   * decide. Turning it off restores the old stop-at-every-window behaviour.
   */
  skipEmpty = $state(true);

  /** autoPassed counts every window auto has answered this session, so the pass is visible after the fact. */
  autoPassed = $state(0);

  /** emptySkipped counts the no-action windows the floor passed, kept apart from autoPassed so the panel never credits auto with a pass it did not make. */
  emptySkipped = $state(0);
  /** actPassed counts the windows the pass-after-acting preference answered, kept apart from autoPassed and emptySkipped for the same reason: each mechanism's passes are its own. */
  actPassed = $state(0);
  /** autoRun is the current unbroken run of machine passes; Auto and the one-shot runs share its hard cap. */
  autoRun = $state(0);
  /**
   * oneShot is the current one-shot run: 'end-turn' (End Turn — passes the
   * rest of THIS turn with the player's own rules minus the step stops),
   * 'hard-skip' (passes everything including opponent objects, MTGO F6),
   * 'resolve-all' (prio6: passes while the stack is non-empty — the objects
   * PRESENT at arm time never stop it, a NEW opponent object stops it per
   * the settings, and it ends when the stack is empty), or 'none'. All
   * three end on Escape, on any non-priority decision, on any other stop
   * verdict, and at the shared pass cap; end-turn and hard-skip also end
   * when the turn number changes or the step reaches cleanup.
   */
  oneShot = $state<'none' | 'end-turn' | 'hard-skip' | 'resolve-all'>('none');
  /** runPassed is the current one-shot's visible pass count. */
  runPassed = $state(0);
  /** the view turn the one-shot was armed at; a turn change ends an end-turn/hard-skip run. */
  private oneShotTurn: number | null = null;
  /**
   * resolveAllIds is the Resolve All run's arm-time stack — the ids of the
   * objects that were already on the stack when the run started (null for
   * every other run). decide() skips its stack rules for these ids, so the
   * run plays through the stack as it stands; a NEW opponent object (an id
   * not in the set) stops the run per the settings, exactly as the brief
   * specifies. Reset on every other run start and on begin().
   */
  private resolveAllIds: ReadonlySet<number> | null = null;
  /** note is what the panel says about automatic action, as a value — autoNoteText turns it into words. */
  note = $state<AutoNote>({ kind: 'off' });

  /**
   * autoLog is the client-local log of automatic passes (prio5): one line
   * per pass when settings.logAutoPasses is on, rendered by the transcript
   * after the engine's own lines. It is NOT the event log — nothing here is
   * sent to the server or folded into DvrState.events; it lives and dies
   * with this browser tab. See lib/autolog.ts for the shape and the cap.
   * The prio6 auto-order note is logged here too (unconditionally — it is a
   * decision the machine made for the player, not a pass under the
   * logAutoPasses switch).
   */
  autoLog = $state<AutoPassLog[]>([]);

  /**
   * yieldList is the game-scoped "always pass for this ability" set (prio6,
   * lib/yields.ts), as a reactive array of keys — seeded from the per-game
   * (table + match) store at construction (memory + sessionStorage; survives
   * a reload of the same match, but a new match on the same table starts
   * clean) and written through on every change. Reading it as a set is
   * the `yields` getter below.
   */
  yieldList = $state<string[]>([]);

  /**
   * arrangeOpen is the arrange popup's open flag (brief Job 4). It lives on
   * the shared state rather than on the SeatPanel component because a
   * component-local `$state` is undeclarable there (the component's own
   * `state` prop makes the rune ambiguous) and because both surfaces that
   * mount SeatPanel against one state object share it — only the board
   * surface renders the modal; the strip's copy shows the pointer note.
   */
  arrangeOpen = $state(false);

  /**
   * discardOpen is the discard-pick modal's open flag (fb-20260918T201739Z),
   * the twin of arrangeOpen: the same shared-state reasons (a component-local
   * `$state` is undeclarable in SeatPanel; the board and strip surfaces mount
   * against one state object) and the same reset sites — a new decision and
   * a match boundary both close it, so the modal can never outlive the ask
   * it belongs to.
   */
  discardOpen = $state(false);

  /**
   * autoOrderedSeq is the seq the identical-trigger auto-order last posted
   * for — the same loop guard autoActedSeq is for the pass paths, so a
   * rejected auto-order is never retried forever against a refusing server.
   */
  private autoOrderedSeq: number | null = null;

  /**
   * remembered is the player's remembered trigger answers (lib/remembered.ts,
   * fb-20260914T062319Z-88b4069a part B): one answered option index per full
   * prompt, so an optional trigger that recurs every turn is answered once
   * and auto-answered ever after. Loaded from the ONE global localStorage key
   * at construction (SSR and a browser that refuses site data pass null and
   * get the empty store) and written through on every change. The management
   * list (PlaySettingsPanel) reads and prunes it through
   * removeRemembered/clearRemembered.
   */
  remembered = $state<RememberedStore>(emptyRemembered());

  /**
   * rememberChoice is the remember checkbox on a trigger_optional prompt
   * (B4): when checked, the answer about to be posted is stored under the
   * prompt's key. Default OFF — remembering is an explicit act, never a
   * side effect of answering. Reset on every newly adopted decision, so a
   * checkbox left ticked on one ask never silently remembers the next,
   * different ask.
   */
  rememberChoice = $state(false);

  /**
   * searchFilter is the library-search picker's display-only filter text
   * (fb-20260916T181754Z): a case-insensitive substring over the search
   * options' card-name labels, consumed by lib/search.ts's searchOptions to
   * build the DISPLAY list. It is deliberately not part of the answer: it
   * never touches `picked`, the submit gate or the posted intent — the
   * search answer is the picked wire indexes in click order regardless of
   * what the list shows. Reset on every newly adopted decision, so a filter
   * typed for one ask can never silently narrow the next, different ask's
   * list (the same adopt-reset contract as rememberChoice).
   */
  searchFilter = $state('');

  /**
   * rememberedSeq is the seq the remembered-answer auto-reply last posted
   * for — the same loop guard autoOrderedSeq is, so a rejected auto-answer
   * is never retried forever against a refusing server.
   */
  private rememberedSeq: number | null = null;

  /**
   * passWait is the pending PACED pass (prio5): the machine decided to pass
   * decision `seq` by option `index`, classified as `kind`, and is waiting
   * settings.pacing's stepMs/resolveMs before actually posting — the visible
   * beat that makes skipped windows seen rather than felt. It is abandoned
   * (cancelPassWait) on any new view object, decision change (adopt), Escape,
   * a hand answer, a run cancel, the panel's destruction or the match
   * boundary; the timer itself (firePass) also re-validates the seq and
   * complete verdict before posting, so a stale pass is structurally
   * impossible. A wait of 0 ms never schedules: the pass posts immediately,
   * which is the pre-pacing path the tests rely on.
   */
  private passWait: { seq: number; index: number; kind: AutoPassKind; reason: string } | null = null;
  private passTimer: ReturnType<typeof setTimeout> | null = null;
  /** Latest view supplied to considerAuto; firePass re-derives against this exact view. */
  private currentView: View | null = null;
  /**
   * autoActedSeq is the seq auto last posted for. If a decision with that
   * seq is put in front of auto again, the answer did not take and auto
   * would post it forever: that is the loop guard, and it pauses the
   * machine (machinePaused), never the persisted preference.
   */
  private autoActedSeq: number | null = null;
  /**
   * actPassArmed is the one-shot token: set when this seat POSTS a hand
   * answer to a priority decision containing a real action, consumed by the
   * FIRST priority window considerAuto sees while neither auto nor a
   * one-shot run is live. It is deliberately not reactive state — it is only
   * ever read inside considerAuto, which the component runs once per
   * decision/view change, so a token minted by a hand answer is always seen
   * by the next decision's pass through the loop. It survives intermediate
   * NON-priority hand answers on purpose: the common cast flow is cast → the
   * spell's target decision (hand-answered) → the next priority window, and
   * disarming on the target answer would break exactly the flow the
   * preference exists to smooth.
   */
  private actPassArmed = false;
  /**
   * actPassActedSeq is the seq the armed token last answered — the same loop
   * guard autoActedSeq is for the other machine paths. In practice the token
   * is consumed before the post is even attempted, so a failed post leaves
   * nothing armed and no retry can start; the guard is the belt to that
   * braces: if the very same seq ever comes back armed again, it is not
   * answered a second time.
   */
  private actPassActedSeq: number | null = null;
  /** presetBackup holds the settings Ctrl+Shift+F replaced, so toggling back restores them exactly. Session-scoped: the backup is a convenience, not a preference. */
  private presetBackup: PlaySettings | null = null;

  /**
   * seqEpoch counts seq-space discards: begin() bumps it, so a rewind (and
   * a match boundary) invalidates every in-flight intent posted against the
   * old space. post() captures it before its await and re-checks after: the
   * server's response to a pre-rewind intent describes a seq space the
   * client discarded, and letting its bookkeeping through would mark the
   * RESTORED decision — which can carry the SAME seq — as already answered
   * (match.svelte.ts's liveEpoch guards the DVR half of exactly this race;
   * this is the panel-state half).
   */
  private seqEpoch = 0;

  /**
   * seqHigh is the highest decision seq this seat has adopted or posted in
   * the current seq space. adopt() refuses anything older: within one space
   * seqs only grow, so an older decision is a stale view re-offering an ask
   * already answered (the 2026-09-24 g2 freeze: the board SeatPanel's mount
   * re-adopted the auto-ordered trigger_order over the target ask the poll
   * had just delivered). begin() resets it, because a rewind or a match
   * boundary opens a new space where a lower seq is legitimate again.
   */
  private seqHigh = -1;

  /** auto is settings.autoPass: the persisted preference, ON by default (casual). Reading it is a read of settings. */
  get auto(): boolean {
    return this.settings.autoPass;
  }

  /** actPass is settings.passAfterAct, likewise persisted. */
  get actPass(): boolean {
    return this.settings.passAfterAct;
  }

  /** endTurn mirrors oneShot for the template and tests. */
  get endTurn(): boolean {
    return this.oneShot === 'end-turn';
  }

  /** hardSkip mirrors oneShot for the template, the chip and tests. */
  get hardSkip(): boolean {
    return this.oneShot === 'hard-skip';
  }

  /** resolveAll mirrors oneShot for the template, the chip and tests. */
  get resolveAll(): boolean {
    return this.oneShot === 'resolve-all';
  }

  /**
   * yields is the ReadonlySet view of yieldList that decide() and the stack
   * tile marker consume; the setter writes back and persists through the
   * per-table store.
   */
  get yields(): ReadonlySet<string> {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- a fresh ephemeral read view per access, never stored; the reactive source is yieldList
    return new Set(this.yieldList);
  }

  set yields(next: Iterable<string>) {
    this.yieldList = [...next];
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- a write-through copy into the non-reactive store layer (lib/yields.ts), never stored on the state
    saveYields(this.table, this.match, new Set(this.yieldList), this.yieldStorage);
    clientBreadcrumbs.setPlay(this.settings, this.yieldList);
  }

  /**
   * addYield is the stack tile menu's "Always pass for …" write path: one
   * key added for THIS GAME (persisted per table + match), and the current window
   * re-derived immediately — a yield the player just granted applies to the
   * decision that is pending right now, not only to the next one. A paced
   * pass already in flight needs no kick: firePass re-derives its verdict
   * with the fresh settings before it posts.
   */
  addYield(key: string) {
    if (this.yieldList.includes(key)) return;
    this.yields = [...this.yieldList, key];
    const view = this.currentView;
    if (view !== null) this.considerAuto(view);
  }

  /** clearYields is GAME OPTIONS' "Clear yields" action: the whole set for this game (this match), emptied and persisted. */
  clearYields() {
    this.yields = [];
  }

  /**
   * stops is the Set-shaped view of settings.steps that the phase track and
   * the stop grid read: a step is "stopped" when its rule is not 'off'. The
   * setter writes back — present becomes 'smart', absent 'off' — so the old
   * Set-based callers keep working; a 'forced' rule reads as set and is
   * rewritten to 'smart' by the same assignment (the Set shape cannot
   * express 'forced').
   */
  get stops(): Stops {
    const rules = this.settings.steps;
    const side = (r: Record<StoppableStep, StepStop>): Set<string> =>
      // eslint-disable-next-line svelte/prefer-svelte-reactivity -- a fresh ephemeral read view per access, never stored; the reactive source is settings.steps
      new Set(STOPPABLE_STEPS.filter((s) => r[s as StoppableStep] !== 'off'));
    return { yours: side(rules.yours), opponents: side(rules.opponents) };
  }

  set stops(next: Stops) {
    const side = (set: Set<string>): Record<StoppableStep, StepStop> => {
      const out = {} as Record<StoppableStep, StepStop>;
      for (const s of STOPPABLE_STEPS) out[s as StoppableStep] = set.has(s) ? 'smart' : 'off';
      return out;
    };
    this.applySettings(withChange(this.settings, {
      steps: { yours: side(next.yours), opponents: side(next.opponents) },
    }));
  }

  /** playMode is the status chip's value: the live one-shot beats the preset label. */
  get playMode(): 'end-turn' | 'skip-turn' | 'resolve-all' | PlaySettings['preset'] {
    if (this.oneShot === 'end-turn') return 'end-turn';
    if (this.oneShot === 'hard-skip') return 'skip-turn';
    if (this.oneShot === 'resolve-all') return 'resolve-all';
    return this.settings.preset;
  }

  /**
   * applySettings swaps the whole settings object and persists it. Every
   * settings change funnels through here (or patchSettings below) so the
   * global key never goes stale.
   */
  private applySettings(next: PlaySettings) {
    this.settings = next;
    saveSettings(this.storage, next);
    clientBreadcrumbs.setPlay(this.settings, this.yieldList);
  }

  /** patchSettings applies a partial change through withChange (which relabels the preset when the result matches one) and persists. */
  private patchSettings(patch: Partial<PlaySettings>) {
    this.applySettings(withChange(this.settings, patch));
  }

  /**
   * editSettings is the GAME OPTIONS editor's write path (prio4): a partial
   * change applied through withChange — so the preset relabels itself
   * Custom while the configuration matches none, and back to a named preset
   * when an edit is undone — and persisted. A settings edit ends a live
   * one-shot run (editing the rules is the player taking the controls) but
   * touches nothing else: the runaway brake still clears only on the Auto
   * switch or on applying a named preset that runs auto (applyNamedPreset
   * below), which is what its note tells the player to press.
   */
  editSettings(patch: Partial<PlaySettings>) {
    this.cancelRun(false);
    this.patchSettings(patch);
  }

  /**
   * applyNamedPreset is the GAME OPTIONS preset picker's and "Reset to
   * Casual" button's write path: the named preset applied through
   * withChange (presetPatch covers every field, so withChange relabels the
   * preset itself) PLUS the machine-side re-arm effects setAuto carries.
   * The runaway brake is a pause of the machine, not a setting — and
   * picking Casual or No tells after the brake tripped is the player asking
   * the machine to run again, so leaving machinePaused set would leave the
   * panel reading auto-pass on while considerAuto keeps refusing to act.
   * The cleared run counters and the armed/off note mirror setAuto exactly;
   * on full-control (autoPass false) the brake clear is harmless and the
   * note is off, exactly as setAuto(false) would leave it.
   */
  applyNamedPreset(id: PresetName) {
    this.cancelRun(false);
    this.machinePaused = false;
    this.autoRun = 0;
    this.autoActedSeq = null;
    this.patchSettings(presetPatch(id));
    this.note = this.auto ? { kind: 'armed' } : { kind: 'off' };
  }

  /**
   * pressAuto is the Auto switch's click path — every rendered Auto switch
   * (the seat panel's Auto/Manual toggle, GAME OPTIONS' Auto pass switch and
   * the live strip's paused status/resume chip) goes through this one method,
   * so they cannot drift. Its dual is the
   * undo pause: while machinePaused holds, the switch reads "Paused" and
   * pressing it STARTS the machine — setAuto(true) — instead of toggling the
   * persisted preference off. That is the resume the paused note promises:
   * with auto enabled (the default), pressing the switch leaves autoPass
   * exactly as it was and only lifts the brake; with auto off it turns auto
   * on, which is what pressing an Auto switch means. Unpaused it is the
   * ordinary toggle.
   */
  pressAuto() {
    this.setAuto(this.machinePaused ? true : !this.auto);
  }

  /** setAuto is the Auto/Manual control: a settings change (autoPass), persisted. Turning it on — or re-arming it while it is on — clears the runaway brake and the previous run so an old count never trips the cap. */
  setAuto(on: boolean) {
    this.cancelRun(false);
    this.machinePaused = false;
    this.autoRun = 0;
    this.autoActedSeq = null;
    this.patchSettings({ autoPass: on });
    this.note = on ? { kind: 'armed' } : { kind: 'off' };
  }

  /** setSkipEmpty is the empty-window floor's control. Turning it back on clears the run so an old count never trips the cap. */
  setSkipEmpty(on: boolean) {
    this.cancelRun();
    this.skipEmpty = on;
    this.autoRun = 0;
    this.autoActedSeq = null;
    if (!this.auto) this.note = { kind: 'off' };
  }

  /** setActPass is the pass-after-acting preference's control — a settings change (passAfterAct), persisted globally. Turning it OFF disarms a still-armed pass: a stale one-shot firing several windows after the player switched the preference off would be exactly the surprise the switch exists to prevent. Turning it ON arms nothing — only a future hand action does. */
  setActPass(on: boolean) {
    if (!on) {
      this.actPassArmed = false;
      // A paced act-pass waiting to post dies with the preference: the
      // player just said the machine should not answer the next window.
      this.cancelPassWait();
    }
    this.patchSettings({ passAfterAct: on });
  }

  /**
   * toggleStop flips one step's stop rule on one turn side and persists it.
   * The rule cycles 'off' → 'smart' → 'off' (a 'forced' rule goes straight
   * back to 'off' — the click always means "stop here" or "stop ignoring
   * here"); a step that grants no priority (untap, cleanup) is refused. The
   * two sides are separate records on purpose: stopping in your own combat
   * and stopping in an opponent's are different intentions, and a stop set
   * on one side must never mark the other.
   */
  toggleStop(step: string, side: TurnSide) {
    this.cancelRun();
    if (!(STOPPABLE_STEPS as readonly string[]).includes(step)) return;
    const rule = this.settings.steps[side][step as StoppableStep] ?? 'off';
    // The steps patch is a deep partial at runtime (withChange merges field-wise); the cast states that.
    const patch = { steps: { [side]: { [step as StoppableStep]: rule === 'off' ? 'smart' : 'off' } } } as unknown as Partial<PlaySettings>;
    this.patchSettings(patch);
  }

  /**
   * toggleFullControl is the Ctrl+Shift+F hotkey's action: swap the current
   * settings for the full-control preset, or — when full-control is already
   * the live preset — restore exactly what it replaced. The backup is
   * session-scoped and consumed by the restore; a second full-control press
   * without a backup restores the defaults rather than guessing.
   */
  toggleFullControl() {
    this.cancelRun(false);
    this.autoRun = 0;
    this.autoActedSeq = null;
    if (this.settings.preset === 'full-control') {
      const back = this.presetBackup;
      this.presetBackup = null;
      this.applySettings(back ?? defaultSettings());
    } else {
      this.presetBackup = this.settings;
      this.applySettings(applyPreset('full-control'));
    }
    this.note = this.auto ? { kind: 'armed' } : { kind: 'off' };
  }

  /**
   * stopActing is the shared guard exit for the loop guard and the pass cap.
   * Whichever mechanism was acting is the one stopped: the one-shot run if
   * one is live, otherwise the machine (paused, NOT un-preferenced),
   * otherwise the empty-window floor. All are runaway protections and all
   * have to be able to actually stop the thing that is running -- before
   * the floor existed, suspendAuto returned silently when auto was already
   * off, which would have left a wedged floor posting forever.
   */
  private stopActing(reason: AutoOffReason) {
    this.cancelPassWait();
    if (this.oneShot !== 'none') {
      const mode = this.oneShot;
      this.oneShot = 'none';
      this.autoRun = 0;
      this.autoActedSeq = null;
      this.note = runStopNote(mode, reason);
      return;
    }
    if (this.auto && !this.machinePaused) {
      this.suspendAuto(reason);
      return;
    }
    this.skipEmpty = false;
    this.autoRun = 0;
    this.autoActedSeq = null;
    this.note = { kind: 'skip-off', reason };
  }

  /**
   * startEndTurn arms the END TURN one-shot: every priority window for the
   * rest of THIS turn is passed, under decide()'s ordinary rules EXCEPT the
   * step stops, which the run ignores — pressing END TURN at your own main2
   * with a playable card is exactly the point of the button. The opponent-
   * object rules still apply: the run stops for an opponent's spell or
   * ability as the player's settings say. The run ends when the turn number
   * changes or the step reaches cleanup, on Escape, on any non-priority
   * decision, on any other stop verdict, and at the shared pass cap. The
   * current view is required because the press is itself consent.
   */
  startEndTurn(view: View) {
    this.startRun('end-turn', view);
  }

  /**
   * startHardSkip arms the hard skip (MTGO F6, Shift+Enter or shift-click
   * END TURN): EVERYTHING is passed for the rest of the turn, including
   * opponent objects — the strip shows a warning chip while it runs. The
   * same expiry and cap as End Turn apply.
   */
  startHardSkip(view: View) {
    this.startRun('hard-skip', view);
  }

  /**
   * startResolveAll arms the Resolve All one-shot (prio6): pass while the
   * stack is non-empty, playing through the objects ALREADY on it — the
   * arm-time stack ids are captured here and decide() skips its stack
   * rules for them. It stops on a NEW opponent object (an id not in the
   * baseline, judged by the settings — the brief's "an object id not
   * present when Resolve All was pressed"), on any non-priority decision,
   * on Escape, at the shared pass cap — and it ENDS when the stack is
   * empty, which is the run's own expiry (expireRun). A priority decision
   * is required and the stack must be non-empty: the button only shows in
   * that state, and the guard makes the state a fact, not an assumption.
   */
  startResolveAll(view: View) {
    if (view.stack.length === 0) return;
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- an arm-time id snapshot for the run's private baseline, never a reactive source
    this.startRun('resolve-all', view, new Set(view.stack.map((s) => s.id)));
  }

  private startRun(kind: 'end-turn' | 'hard-skip' | 'resolve-all', view: View, baseline: ReadonlySet<number> | null = null) {
    // While the undo pause holds, a run cannot arm: a run is the machine
    // passing on the player's behalf, and the pause exists precisely so the
    // machine does not answer the window the player just rewound to. The
    // pause clears only on the player's own resume paths (pressAuto, a named
    // preset); after that the buttons arm as usual. Refusing — rather than
    // arming a run that derivePass would hold — also keeps the paused note
    // on screen: an armed run chip would overwrite it with a note claiming
    // the machine is passing when the pause holds it back.
    if (this.busy || this.machinePaused) return;
    // Starting a run is the player taking the controls: any paced pass the
    // AUTO paths had pending dies here (r2 finding — the old auto wait used
    // to survive, post at its old deadline and count as autoPassed). The
    // effect re-runs considerAuto because oneShot changed, so the same
    // window is re-derived and re-paced under the run's own rules and
    // register — End turn / Skip turn / Resolve All, counted in runPassed.
    this.cancelPassWait();
    this.oneShot = kind;
    this.resolveAllIds = baseline;
    this.runPassed = 0;
    this.autoRun = 0;
    this.autoActedSeq = null;
    this.oneShotTurn = view.turn;
    this.note = kind === 'end-turn'
      ? { kind: 'end-turn-armed' }
      : kind === 'resolve-all'
      ? { kind: 'resolve-all-armed' }
      : { kind: 'skip-turn-armed' };
  }

  /** Any other pointer/key/answer hands control back immediately. When it actually fires (a run was live) it also clears an armed pass-after-acting token: the player took the controls back mid-run. */
  cancelRun(say = true) {
    if (this.oneShot === 'none') {
      // Even with no run live, a hand action or Escape must still abandon a
      // paced pass waiting to post (prio5): taking the controls back means
      // the machine does not answer this window after all.
      this.cancelPassWait();
      return;
    }
    this.actPassArmed = false;
    this.cancelPassWait();
    this.oneShot = 'none';
    this.autoRun = 0;
    this.autoActedSeq = null;
    if (say) this.note = this.auto ? { kind: 'armed' } : { kind: 'off' };
  }

  /**
   * handAnswer is the takeover step every human answering path runs before
   * it posts. A hand answer is NOT a request to stop auto-passing (prio3):
   * the persisted autoPass survives it, and Escape is the only key that
   * ends a run without a decision of its own. The one thing it does is end
   * a live one-shot run — taking the controls mid-run cancels it.
   */
  private handAnswer() {
    this.cancelPassWait();
    this.cancelRun();
  }

  /**
   * suspendAuto pauses the machine with a stated reason. Only the runaway
   * guards (loop, cap) reach it now — a hand answer and Escape no longer
   * flip the persisted preference. The pause clears on the player's next
   * Auto switch.
   */
  suspendAuto(reason: AutoOffReason) {
    if (!this.auto || this.machinePaused) return;
    this.machinePaused = true;
    this.cancelPassWait();
    this.autoRun = 0;
    this.autoActedSeq = null;
    this.note = { kind: 'stopped', reason };
  }

  /** onKeydown is the panel's key handler: Escape cancels the one-shot run — it does NOT flip the persisted autoPass (a panic key is not a settings change). */
  onKeydown(key: string) {
    if (key !== 'Escape') return;
    this.actPassArmed = false;
    this.cancelPassWait();
    this.cancelRun();
  }

  /**
   * expireRun ends the one-shot when its time is over — the turn changed or
   * the step reached cleanup for an end-turn/hard-skip run; the stack
   * emptied for a Resolve All run (its own expiry: there is nothing left to
   * resolve, so the run ends even while the seat still holds priority). It
   * is called from considerAuto (so a decision arriving after the expiry
   * never sees the run armed) and from the component's per-view effect (so
   * the chip drops even while no decision is pending for this seat).
   */
  expireRun(view: View) {
    if (this.oneShot === 'none') return;
    if (this.oneShot === 'resolve-all') {
      if (view.stack.length === 0) this.cancelRun(false);
      return;
    }
    if (view.turn !== this.oneShotTurn || view.step === 'cleanup') this.cancelRun(false);
  }

  /**
   * considerAuto is the whole autopilot loop, run once per decision/view
   * change. Order matters and every early return is a refusal to act:
   * nothing pending, in flight, already answered, seen before (loop), the
   * run cap, then and only then decide().
   */
  considerAuto(view: View) {
    // Every server projection is a complete new View object. Abandon a paced
    // candidate before every early return when that object changes, then let
    // the ordinary classification below decide whether the fresh view starts
    // a NEW full wait. Object identity is deliberately the boundary: unlike a
    // field stamp, it cannot omit a field decide() learns to read later.
    const viewChanged = this.currentView !== null && this.currentView !== view;
    this.currentView = view;
    if (viewChanged) this.cancelPassWait();
    const d = this.pending;
    if (d === null || this.busy || d.seq === this.postedSeq) return;

    // The identical-trigger auto-order (prio6): an answered-before-we-classify
    // path — if the pending decision IS an identical trigger_order and the
    // setting is on, it is submitted here and the run of this function ends
    // (post() set busy synchronously). It runs after the busy/posted guards
    // above and before expireRun, so a run that would otherwise stop on this
    // non-priority decision is cancelled by the submit path itself.
    if (this.maybeAutoOrderTriggers()) return;
    // The remembered-answer auto-reply gets the same second chance (B5): a
    // decision adopted while a post was still in flight returned false at
    // adopt() (busy), and this retry covers the rapid-successive-ask hole the
    // auto-order closes the same way. A decision that already auto-answered
    // (rememberedSeq) or has no remembered entry falls through to manual.
    if (this.maybeRememberedTrigger()) return;

    // A one-shot run ends the moment its turn is over, whether or not a
    // decision is pending (see expireRun).
    this.expireRun(view);

    // A paced pass already in flight for this exact View owns the pass. A
    // different View was cancelled above and therefore falls through to
    // re-derive and, if still passable, starts a new full wait.
    if (this.passWait !== null) return;

    const autoOn = this.auto && !this.machinePaused;

    // Pass after acting: the one-shot token, spent at the FIRST priority
    // window that arrives while neither auto nor a one-shot run is live
    // (with either of those live, their own rules govern and the token stays
    // dormant). It runs BEFORE the early return below, because a window with
    // actions on it is exactly the one this preference exists to pass, and
    // that window is precisely the one the empty-window floor never touches.
    // A machinePaused machine spends no token either (the undo pause) — the
    // guard is belt to begin()'s disarm-braces on the rewind path.
    if (!autoOn && !this.machinePaused && this.oneShot === 'none' && this.actPassArmed && d.kind === 'priority') {
      this.consumeActPass(view);
      return;
    }

    // Loop guard: we already answered this seq and here it is again. The
    // answer did not take, so posting it a second time is the start of an
    // unbounded retry against the server.
    if (this.autoActedSeq !== null && d.seq === this.autoActedSeq) {
      this.stopActing('loop');
      return;
    }

    const verdict = this.derivePass(view);
    if (verdict === null) return;
    if (verdict.act === 'stop') {
      // decide() remains the safety oracle. A one-shot run ENDS on every
      // stop verdict — there is no acknowledgement machinery any more,
      // because a run honours no step stops and the press itself moved
      // the window it was pressed on. Persistent Auto stays armed: the
      // player answers this window and Auto resumes after it — and with
      // hand answers no longer disarming Auto (prio3), no exception
      // token is needed or minted.
      this.autoRun = 0;
      if (this.oneShot !== 'none') {
        const mode = this.oneShot;
        this.oneShot = 'none';
        this.autoActedSeq = null;
        this.note = runStopNote(mode, verdict.reason);
      } else {
        // The stop-set note names the actionable option(s) (fb-20260916T225211Z):
        // a smart step stop fired because this window offered a real play —
        // say what the play is, so "why did it pause on my own priority" is
        // answered on the panel. A 'forced' stop with nothing to do, and every
        // other reason, carry no detail and keep the base wording.
        const labels = verdict.reason === 'stop-set' ? actionables(view, this.ctx.seat, d) : [];
        this.note = labels.length > 0
          ? { kind: 'waiting', reason: verdict.reason, detail: labels.join(', ') }
          : { kind: 'waiting', reason: verdict.reason };
      }
      return;
    }

    // The cap bounds an unbroken run of machine-made passes, and it bounds
    // the floor for the same reason it bounds auto and the one-shot runs: a
    // stuck window answered forever is a denial of service the player never
    // asked for.
    if (this.autoRun >= AUTO_PASS_CAP) {
      this.stopActing('cap');
      return;
    }

    this.dispatchPass(view, verdict.index, verdict.kind, verdict.reason);
  }

  /**
   * maybeAutoOrderTriggers is the trigger_order auto-order. The prio6 path
   * answers an identical pair — every option the same trigger
   * (identicalTriggerOrder) — when settings.autoOrderIdenticalTriggers is
   * on, with the note "Ordered N identical triggers automatically".
   * fb-trigorder1 adds the broader path: when settings.autoOrderAllTriggers
   * is on, ANY trigger_order satisfying the same shape contract
   * (triggerOrderPermutation) is auto-answered too, with the distinct note
   * "Ordered N triggers automatically" — auto-ordering DIFFERENT triggers is
   * a real game choice made on the player's behalf, and the note is what
   * makes it visible in the log. The submitted answer stays the OFFERED
   * order (d.options.map((o) => o.index)), exactly what the manual UI's
   * untouched answer submits, in both paths.
   *
   * Precedence: an identical pair answers through the identical path when
   * that setting is on, so the pinned prio6 note is unchanged for casual
   * (which turns both on); the broader path covers everything else.
   * Any other decision kind, a busy/posted state, the setting(s) off: false,
   * and the decision stays manual. A live one-shot run is cancelled first:
   * a non-priority decision ends a run, and the submit is the run's stop,
   * not the run's continuation. The autoOrderedSeq guard means a
   * server-rejected auto-order is never retried forever. The undo pause (and
   * the runaway brake — same brake, same reason) silences the auto-order
   * too: a restored trigger_order ask must sit pending for the player, not
   * be submitted by the machine the player just stopped. adopt() reaches
   * here before any considerAuto.
   */
  private maybeAutoOrderTriggers(): boolean {
    const d = this.pending;
    if (d === null || this.busy || d.seq === this.postedSeq) return false;
    if (this.autoOrderedSeq !== null && d.seq === this.autoOrderedSeq) return false;
    // The undo pause (and the runaway brake — same brake, same reason)
    // silences the auto-order too: a restored trigger_order ask must sit
    // pending for the player, not be submitted by the machine the player
    // just stopped. adopt() reaches here before any considerAuto.
    if (this.machinePaused) return false;
    if (!triggerOrderPermutation(d)) return false;
    const identical = identicalTriggerOrder(d);
    if (identical && this.settings.autoOrderIdenticalTriggers) {
      this.postTriggerOrder(d, `Ordered ${d.options.length} identical triggers automatically`);
      return true;
    }
    if (this.settings.autoOrderAllTriggers) {
      this.postTriggerOrder(d, `Ordered ${d.options.length} triggers automatically`);
      return true;
    }
    return false;
  }

  /** postTriggerOrder is the shared submit of both auto-order paths: cancel any live one-shot run (a non-priority decision ends a run), set the retry guard, log the note, post the offered order. */
  private postTriggerOrder(d: Decision, note: string) {
    if (this.oneShot !== 'none') this.cancelRun(); // the run's stop, noted in its own register
    this.autoOrderedSeq = d.seq;
    this.autoLog = pushAutoPassLog(this.autoLog, note, this.currentView?.turn ?? 0);
    void this.post(d.options.map((o) => o.index));
  }

  /**
   * derivePass is the shared classification used both when a window first
   * arrives and when a pacing timer fires. It returns null only when neither
   * persistent Auto, a one-shot run nor the empty-window floor owns the
   * current window. Keeping this decision in one function is the core safety
   * property: firePass cannot drift from considerAuto as decide() learns to
   * inspect more of View.
   */
  private derivePass(view: View):
    | { act: 'pass'; index: number; kind: Exclude<AutoPassKind, 'act'>; reason: string }
    | { act: 'stop'; reason: StopReason }
    | null {
    const d = this.pending;
    if (d === null) return null;
    // Auto-pay changes the witness used for an explicit cast only. It must
    // not alter Auto/Manual or empty-window pass policy.
    // The undo pause owns the whole classification: while it holds, neither
    // auto, nor the empty-window floor, nor a one-shot run passes anything.
    // It must gate HERE, before the autoOn split below, not only on the
    // floor branch: with autoPass ON, machinePaused already makes autoOn
    // false, so gating only the floor branch would fall through to decide()
    // with the real (auto-on) settings and the machine would pass anyway.
    if (this.machinePaused) return null;
    const autoOn = this.auto && !this.machinePaused;

    // The empty-window floor runs whether or not auto is on, so a Manual
    // seat is still not stopped at a window that asks nothing. With Auto on,
    // decide() owns the same shape and classifies it under Auto's counter.
    if (!autoOn && this.oneShot === 'none') {
      const index = this.skipEmpty ? emptyPriorityWindow(d, view, this.ctx.seat) : null;
      return index === null ? null : { act: 'pass', index, kind: 'empty', reason: 'empty-window' };
    }

    // A one-shot run feeds decide() its OWN settings: autoPass forced on,
    // step rules off, and — hard skip only — opponent/own object rules off.
    // Resolve All additionally passes the arm-time baseline, so decide()
    // skips its stack rules for the objects the run set out to resolve
    // through, and both runs still honour the game's yields.
    const verdict = decide({
      decision: d,
      view,
      seat: this.ctx.seat,
      settings: this.oneShot !== 'none' ? this.runSettings(this.oneShot) : this.settings,
      yields: this.yields,
      baselineStack: this.oneShot === 'resolve-all' ? (this.resolveAllIds ?? undefined) : undefined,
      // The one-shot runs are the player's explicit machine-plays-my-turn
      // consent (fb-20260917T231311Z-e392fcc0): they bypass the own-turn
      // main-phase floor. Persistent Auto keeps the default (the floor
      // applies); ffwd never reaches here.
      skipOwnTurnFloor: this.oneShot !== 'none',
      autoManaAvailable: this.autoManaAvailable,
    });
    if (verdict.act === 'stop') return verdict;
    const kind: Exclude<AutoPassKind, 'act'> = this.oneShot === 'end-turn'
      ? 'end-turn'
      : this.oneShot === 'hard-skip'
      ? 'hard-skip'
      : this.oneShot === 'resolve-all'
      ? 'resolve-all'
      : 'auto';
    return { ...verdict, kind, reason: 'no-stop-rule' };
  }

  /**
   * paceMs is the beat before an automatic pass posts: resolveMs while the
   * view's stack is non-empty (something is resolving — the thing the player
   * is being skipped past), else stepMs (a bare step boundary). A value of
   * 0 (or less) posts immediately — the pre-pacing path, which is what the
   * full-control preset ships and what the tests drive.
   */
  private paceMs(view: View): number {
    return view.stack.length > 0 ? this.settings.pacing.resolveMs : this.settings.pacing.stepMs;
  }

  /**
   * dispatchPass is the ONE exit every automatic pass goes through — auto,
   * the empty-window floor, pass-after-acting and both one-shot runs. At 0 ms
   * it counts, logs and posts synchronously (today's path); at a paced
   * setting it schedules the candidate seq/index/kind for firePass to
   * re-derive before acting. The loop-guard
   * seq is recorded HERE, at dispatch: a cancelled wait (cancelPassWait)
   * un-records it again, so an abandoned pass never looks answered and the
   * loop guard can never trip on a pass that was never posted.
   */
  private dispatchPass(view: View, index: number, kind: AutoPassKind, reason: string) {
    const d = this.pending;
    if (d === null) return;
    // This is the single exit for every machine pass. Carry its reason to
    // postPass, so a cancelled pacing wait is never reported as an action.
    const ms = this.paceMs(view);
    if (kind !== 'act') this.autoActedSeq = d.seq;
    if (ms <= 0) {
      this.postPass(kind, index, view, reason);
      return;
    }
    if (this.passTimer !== null) clearTimeout(this.passTimer);
    this.passWait = { seq: d.seq, index, kind, reason };
    this.passTimer = setTimeout(() => this.firePass(), ms);
  }

  /**
   * firePass is the paced pass's firing edge. The scheduled verdict is only
   * a candidate: after the seq guard, the exact classification used by
   * considerAuto is re-run with the latest view, settings and run mode. It
   * posts only when that fresh verdict is still a pass from the same machine
   * path and names the same wire index. Otherwise it drops the candidate and
   * sends the fresh state through considerAuto, which may stop or start a new
   * full wait. Log text is also made here, from the state actually passed.
   */
  private firePass() {
    const w = this.passWait;
    this.passTimer = null;
    this.passWait = null;
    if (w === null) return;
    const d = this.pending;
    const view = this.currentView;
    // A scheduled candidate is not an answered decision. Release its loop
    // marker even when the seq guard rejects it; postPass restores it only
    // for a freshly authorized post.
    if (this.autoActedSeq === w.seq) this.autoActedSeq = null;
    if (d === null || view === null || d.seq !== w.seq || this.busy || d.seq === this.postedSeq) return;

    const fresh = w.kind === 'act' ? this.deriveActPass(view) : this.derivePass(view);
    if (fresh?.act === 'pass' && fresh.index === w.index && fresh.kind === w.kind) {
      if (w.kind !== 'act') this.autoActedSeq = w.seq;
      this.postPass(w.kind, w.index, view, fresh.reason);
      return;
    }

    // The view/settings/run changed the answer. Let the ordinary path apply
    // its stop note or schedule a fresh, fully paced candidate.
    this.considerAuto(view);
  }

  /** One actual automatic post: count it and, if enabled NOW, log the current view. */
  private postPass(kind: AutoPassKind, index: number, view: View, reason: string) {
    // This is the actual post edge for every automatic pass. Recording here
    // means a cancelled pacing wait is not misreported as an action.
    clientBreadcrumbs.record('auto_pass', { reason, mode: kind, seq: this.pending?.seq ?? null, choice: index });
    this.countPass(kind);
    if (this.settings.logAutoPasses) {
      this.autoLog = pushAutoPassLog(this.autoLog, autoPassLogText(kind, view, this.ctx.seat), view.turn);
    }
    void this.post([index]);
  }

  /**
   * countPass is the shared counter/note edge for one ACTUAL pass (immediate
   * or just fired), split by the machine path that made it — the same split
   * the panel has always reported.
   */
  private countPass(kind: AutoPassKind) {
    this.autoRun += 1;
    if (kind === 'end-turn' || kind === 'hard-skip' || kind === 'resolve-all') {
      this.runPassed += 1;
      this.note = kind === 'end-turn'
        ? { kind: 'end-turn-passing', count: this.runPassed }
        : kind === 'resolve-all'
        ? { kind: 'resolve-all-passing', count: this.runPassed }
        : { kind: 'skip-turn-passing', count: this.runPassed };
    } else if (kind === 'auto') {
      this.autoPassed += 1;
      this.note = { kind: 'passing', count: this.autoPassed };
    } else if (kind === 'empty') {
      this.emptySkipped += 1;
      this.note = { kind: 'skipped', count: this.emptySkipped };
    } else {
      this.actPassed += 1;
      this.note = { kind: 'act-passed', count: this.actPassed };
    }
  }

  /**
   * cancelPassWait abandons a pending paced pass, if one is pending, and
   * un-records its loop-guard seq (the answer was never posted). Every
   * takeover edge calls it: a new decision (adopt), Escape, any hand
   * answer, a run cancel, the runaway brakes, the match boundary, and the
   * seat panel's own destruction (the component's onDestroy).
   */
  cancelPass() {
    this.cancelPassWait();
  }

  private cancelPassWait() {
    if (this.passTimer !== null) {
      clearTimeout(this.passTimer);
      this.passTimer = null;
    }
    const w = this.passWait;
    this.passWait = null;
    if (w === null) return;
    if (this.autoActedSeq === w.seq) this.autoActedSeq = null;
    if (this.actPassActedSeq === w.seq) this.actPassActedSeq = null;
  }

  /**
   * runSettings is the settings a one-shot run feeds decide(): the player's
   * own settings with autoPass forced on (the press is the consent), every
   * step rule off (End Turn ignores step stops), and — hard skip only —
   * every opponent-object rule 'never' and ownObjects 'never' (it passes
   * everything, MTGO F6). Everything else — the opponent-object rules for a
   * plain End Turn — stands as the player set it.
   */
  private runSettings(kind: 'end-turn' | 'hard-skip' | 'resolve-all'): PlaySettings {
    const off = {} as Record<StoppableStep, StepStop>;
    for (const s of STOPPABLE_STEPS) off[s as StoppableStep] = 'off';
    const s: PlaySettings = {
      ...this.settings,
      preset: 'custom',
      autoPass: true,
      steps: { yours: { ...off }, opponents: { ...off } },
    };
    if (kind === 'hard-skip') {
      s.opponentSpell = 'never';
      s.opponentAbility = 'never';
      s.opponentTrigger = 'never';
      s.ownObjects = 'never';
    }
    return s;
  }

  /**
   * consumeActPass spends the armed pass-after-acting token on ONE priority
   * window and is the only place the token ever posts. The invariants, in
   * order:
   *
   *  - the token is consumed FIRST, whatever happens after: it is one-shot,
   *    and a stale armed pass firing several windows later is forbidden;
   *  - the loop guard refuses a seq the token already answered, so a failed
   *    post can never retry (in practice the consumption above already
   *    guarantees this; the guard is the belt to that braces);
   *  - decide() stays the safety oracle, consulted with enabled: true and
   *    the player's own stops. Its pass verdict's index ALWAYS points at the
   *    pass option the shape check found — this method is structurally
   *    incapable of posting anything but that pass, and never a concede;
   *  - on a stop verdict (a stop the player set on this step/side, or an
   *    opponent-controlled stack object) the window is surfaced exactly as
   *    it would have been without the preference: no post, no note change.
   *    The token is still spent — that window was the one shot.
   *
   * The pass is counted and worded under the preference's own name
   * (actPassed / act-passed), never auto's.
   */
  private consumeActPass(view: View) {
    const d = this.pending;
    if (d === null) return;
    this.actPassArmed = false;
    if (this.actPassActedSeq !== null && d.seq === this.actPassActedSeq) return;
    this.actPassActedSeq = d.seq;
    // The armed token is itself the consent: the preference fires whether or
    // not the master switch is on (a manual seat with the preference on —
    // the mode this preference exists for).
    const verdict = this.deriveActPass(view);
    if (verdict === null) return;
    this.dispatchPass(view, verdict.index, verdict.kind, verdict.reason);
  }

  /** Re-derive a paced pass-after-acting candidate after its one-shot token was consumed. */
  private deriveActPass(view: View): { act: 'pass'; index: number; kind: 'act'; reason: string } | null {
    const d = this.pending;
    const autoOn = this.auto && !this.machinePaused;
    if (d === null || !this.actPass || autoOn || this.oneShot !== 'none') return null;
    const verdict = decide({ decision: d, view, seat: this.ctx.seat, settings: { ...this.settings, autoPass: true }, yields: this.yields, autoManaAvailable: this.autoManaAvailable });
    return verdict.act === 'pass' ? { ...verdict, kind: 'act', reason: 'no-stop-rule' } : null;
  }

  /** begin resets the seat across a match boundary. (The component keys the panel by match, so a new match is a fresh instance — this is belt and braces.) The settings are a property of the PLAYER, not of the match: auto (autoPass), the stops and pass-after-acting all survive begin() untouched. */
  begin() {
    // A new seq space invalidates every in-flight intent posted against the
    // old one (see seqEpoch; post() re-checks after its await). Relinquish
    // that post's busy lock here rather than waiting for a response that may
    // be delayed forever: the restored window belongs to the new epoch and
    // must be answerable immediately.
    this.seqEpoch += 1;
    this.seqHigh = -1;
    this.busy = false;
    this.cancelPassWait();
    this.pending = null;
    this.picked = [];
    this.postedSeq = null;
    this.confirming = false;
    this.error = null;
    this.oneShot = 'none';
    this.oneShotTurn = null;
    this.machinePaused = false;
    this.autoRun = 0;
    this.runPassed = 0;
    this.autoPassed = 0;
    // An armed pass-after-acting token is a one-shot about a window that no
    // longer exists once the match does; the PREFERENCE persists across the
    // boundary in the settings, only the pending token clears.
    this.actPassArmed = false;
    this.actPassActedSeq = null;
    this.actPassed = 0;
    // The floor is a preference, not an opt-in, so it comes back on across a
    // match boundary the way it starts: on. Only its runaway guards or the
    // player's own switch turn it off.
    this.skipEmpty = true;
    this.emptySkipped = 0;
    this.autoActedSeq = null;
    this.autoLog = [];
    this.autoOrderedSeq = null;
    this.rememberedSeq = null;
    this.resolveAllIds = null;
    this.currentView = null;
    this.arrangeOpen = false;
    this.discardOpen = false;
    this.rememberChoice = false;
    this.note = { kind: 'off' };
  }

  /**
   * rewind discards every action tied to the old seq tail, including a
   * pending decision, one-shot run and paced pass timer. Persistent play
   * settings survive just as they do across begin().
   *
   * An UNDO is the player deliberately taking the controls back — unlike a
   * hand answer, which is not a stop request (prio3), rewinding says "stop
   * answering for me". So beyond begin()'s discard, rewind PAUSES the
   * machine (machinePaused — the runaway brake, never the persisted
   * preference): without it the autopilot loop re-runs considerAuto against
   * the restored decision, re-derives the same pass verdict it made before
   * the undo (begin() cleared every loop-guard seq) and posts it — the
   * player's undo undone by their own client. While the pause holds, NOTHING
   * machine-side answers the restored window: not auto, not the empty-window
   * floor, not pass-after-acting (its token was disarmed anyway), not the
   * identical-trigger auto-order. The restored decision sits pending until
   * the player answers it or clicks UNDO again — which is exactly what makes
   * multi-step undoing possible. Remembered optional-trigger answers, added
   * alongside this work, are machine answers too and obey the same brake.
   * The pause clears on the player's own
   * resume paths: the pause-aware Auto switch (pressAuto — while paused it
   * starts the machine rather than toggling the preference off) and a named
   * preset. A one-shot run is not a resume path: startRun refuses to arm
   * while the pause holds.
   */
  /**
   * shouldPoll is the /pending poll's gate, here rather than in the component
   * so the rule is testable and stated once.
   *
   * It is deliberately NOT "only when the panel has nothing to answer". That
   * rule left the worse half of the failure it was written for wide open: a
   * decision the server has already moved past stays on screen forever,
   * because view.decision — the only other source — is embedded ONLY at the
   * exact head seq, and the effect reading it re-runs only when a new view
   * object is assigned. Once the view stops updating, the stale ask is never
   * cleared and the poll that would replace it is the very thing switched
   * off. Measured on the live demo: the panel held a trigger_order ask while
   * the server was asking this seat to choose a target.
   *
   * Polling with a decision displayed cannot take one out from under the
   * player: adopt() ignores an answer for the seq already shown, so a pick in
   * progress survives, and only a different seq (or a 409, meaning the server
   * asks this seat nothing) replaces it.
   *
   * `busy` still holds it off, so this panel's own in-flight intent and a
   * poll cannot race to define the current ask. That wait is bounded by the
   * intent request's own deadline (STATE_TIMEOUT in ./api).
   */
  get shouldPoll(): boolean {
    return !this.busy;
  }

  rewind() {
    this.begin();
    this.machinePaused = true;
    this.note = { kind: 'paused' };
  }

  /**
   * adoptView synchronises with the parent's view: the seat-scoped view at
   * head carries the decision asked of this seat (and only this seat's).
   * A null decision means the wait is on someone else, or the game is
   * resolving. A decision whose seq we already answered is ignored — a
   * stale view must not re-open answered options.
   */
  adoptView(d: Decision | null) {
    this.adopt(d);
  }

  private adopt(d: Decision | null) {
    if (d === null) {
      this.cancelPassWait();
      this.pending = null;
      return;
    }
    if (this.postedSeq !== null && d.seq === this.postedSeq) return;
    if (this.pending?.seq === d.seq) return;
    if (d.seq < this.seqHigh) return;
    this.seqHigh = d.seq;
    this.cancelPassWait();
    this.pending = d;
    this.postedSeq = null;
    this.picked = [];
    this.confirming = false;
    // The arrange popup, when open, belongs to the PREVIOUS ask: a new
    // decision closes it, so its edit state can never be presented as (or
    // submitted for) an ask the player has not answered — the two-tab path
    // where seat's next arrange ask arrives while this one sits open. The
    // popup's own modal also resets on a seq change (ArrangeModal), so a
    // future mount site that forgets to close still cannot reuse edits.
    this.arrangeOpen = false;
    // The discard-pick modal is the same kind of ask-local surface: a new
    // decision closes it, so its picked view can never be presented as (or
    // submitted for) the next ask.
    this.discardOpen = false;
    // The identical-trigger auto-order runs at ADOPT, not only in
    // considerAuto: the decision frame can arrive while no view change
    // follows it, and the submit must not depend on the next effect tick.
    // The remembered-answer auto-reply runs at the same hook, after the same
    // guards (busy/postedSeq), with its own rememberedSeq loop guard; the
    // checkbox is reset FIRST so a tick left on the previous ask can never
    // remember this, different ask.
    this.rememberChoice = false;
    // The search filter belongs to the PREVIOUS ask just as much (the
    // library-search picker's display filter): a new decision starts with an
    // empty filter, so a narrowing typed for one library can never hide
    // cards of the next one.
    this.searchFilter = '';
    if (!this.maybeAutoOrderTriggers()) this.maybeRememberedTrigger();
  }

  /**
   * maybeRememberedTrigger is the remembered-answer auto-reply (B5). When
   * the pending decision is a trigger_optional whose full-prompt key has a
   * remembered entry, the remembered option is posted through the ordinary
   * post() path — a normal visible submit, never a silent state write — with
   * an autoLog note naming what was answered. Any other kind, no entry, a
   * busy/posted state, or a seq the guard already answered: false, and the
   * decision stays manual as today. A live one-shot run is cancelled first,
   * exactly as maybeAutoOrderTriggers does (a non-priority decision ends a
   * run; the submit is the run's stop, not its continuation).
   */
  private maybeRememberedTrigger(): boolean {
    const d = this.pending;
    if (d === null || this.busy || d.seq === this.postedSeq) return false;
    if (this.rememberedSeq !== null && d.seq === this.rememberedSeq) return false;
    // A remembered answer is still a machine answer. In particular, a
    // rewind can restore the same optional-trigger prompt whose remembered
    // choice was just posted; the undo brake must leave that ask to the
    // player exactly as it leaves auto-pass and trigger auto-order.
    if (this.machinePaused) return false;
    if (!rememberable(d.kind)) return false;
    const choice = rememberChoiceFor(this.remembered, d.kind, d.prompt);
    if (choice === null) return false;
    // The stored index must still be an option of THIS decision: a remembered
    // answer for a prompt the engine now offers differently is not applied.
    if (!d.options.some((o) => o.index === choice)) return false;
    if (this.oneShot !== 'none') this.cancelRun(); // the run's stop, noted in its own register
    this.rememberedSeq = d.seq;
    this.autoLog = pushAutoPassLog(
      this.autoLog,
      `Answered optional trigger from a remembered choice — ${triggerPromptLabel(d.prompt)}`,
      this.currentView?.turn ?? 0,
    );
    void this.post([choice]);
    return true;
  }

  /**
   * rememberAnswer stores the answer the seat is about to post for a
   * trigger_optional decision under the prompt's key (B4). Only ever called
   * from click()'s post-on-click path with rememberChoice checked, so the
   * choice is a real user answer, never a machine one. The write goes
   * through setRemembered so the store and the localStorage key move
   * together.
   */
  private rememberAnswer(d: Decision, choice: number) {
    if (!d.options.some((o) => o.index === choice)) return;
    this.setRemembered(
      withRemember(this.remembered, rememberKey(d.kind, d.prompt), choice, triggerPromptLabel(d.prompt), Date.now()),
    );
  }

  /** setRemembered swaps the remembered store and persists it — the single write path, so the key never goes stale. */
  private setRemembered(next: RememberedStore) {
    this.remembered = next;
    saveRemembered(this.storage, next);
  }

  /** removeRemembered is the management list's per-entry delete (B4): one key forgotten and persisted. */
  removeRemembered(key: string) {
    this.setRemembered(withoutRemember(this.remembered, key));
  }

  /** clearRemembered is the management list's clear-all (B4): the whole store emptied and persisted. */
  clearRemembered() {
    this.setRemembered(emptyRemembered());
  }

  primary(): Option | null {
    return this.pending !== null && this.pending.seq !== this.postedSeq ? primaryOf(this.pending) : null;
  }

  /** passOption is the dedicated HUD action, resolved by kind and carrying its own wire index. */
  get passOption(): Option | null {
    const d = this.active;
    if (d?.kind !== 'priority') return null;
    return d.options.find((o) => o.kind === 'pass') ?? null;
  }

  /** concedeOption is rendered by the page-level quiet control, never in the action list. */
  get concedeOption(): Option | null {
    const d = this.active;
    return d?.options.find(isConcede) ?? null;
  }

  /**
   * active is the decision this seat must answer right now — pending, and
   * not already posted. The board indexes THIS to mark cards with options
   * and to hang each card's options on its tile, and it is exactly the
   * decision the seat panel surfaces, so the board and the panel cannot
   * disagree about what this seat is being asked (and, once answered, both
   * drop it together).
   */
  get active(): Decision | null {
    return this.pending !== null && this.pending.seq !== this.postedSeq ? this.pending : null;
  }

  /**
   * showSubmit renders the commit button for every decision a single click
   * cannot answer — which is exactly the complement of click()'s post-on-click
   * shape (min == max == 1, where the click IS the answer and a submit button
   * would be a redundant second control).
   *
   * The gate used to be `d.max > 1`, which deadlocked a Min 0 / Max 1
   * decision: declare-attackers on a 1v1 board with exactly one creature able
   * to attack. click() correctly declined to post it (min is 0, so a click is
   * a selection and not an answer), the pick sat in `picked`, and no button
   * was ever drawn to commit it or to decline — the seat could select its
   * attacker and then had no way forward at all. Attacking with two creatures
   * was fine, which is why it went unseen. KAttackers also carries no `pass`
   * or `resolve` option, so the primary-by-kind button (R-E4-1) is not there
   * to fall back on.
   */
  get showSubmit(): boolean {
    const d = this.pending;
    return d !== null && d.seq !== this.postedSeq && !(d.min === 1 && d.max === 1);
  }

  /** canSubmit gates the submit button on the decision's OWN min/max — the only selection constraints the client may enforce (R-E4-2). */
  get canSubmit(): boolean {
    const d = this.pending;
    return d !== null && this.picked.length >= d.min && this.picked.length <= d.max;
  }

  /** click handles one option click. A single-required-option decision posts immediately (the click IS the answer); a multi-pick toggles into `picked` for submit. The concede option never posts on the first click (R-E4-1). Passing `holdPriority` (Ctrl held) skips the pass-after-acting arming for this one action. */
  click(index: number, opts?: { holdPriority?: boolean }) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy) return;
    const opt = optionAt(d, index);
    if (opt === undefined) return;
    // Card/ordinary-option clicks share this path.  Auto-pay changes only
    // this one submission; it never changes the pending decision or an
    // already in-flight request.
    if (this.autoPayMana && opt.kind === 'cast') {
      const action = paymentActionForBase(d, index);
      const plan = action?.plans[0];
      if (action !== undefined && plan !== undefined) {
        this.handAnswer();
        this.submitPayment(action, plan, opts?.holdPriority ?? false);
        return;
      }
    }
    if (isConcede(opt)) {
      // Concede is not an action: it never arms pass-after-acting, and an
      // ALREADY armed token must die here — the seat's next priority window
      // would otherwise be passed for a player who is no longer in the game.
      // It also ends a live one-shot run. It does NOT flip the persisted
      // autoPass: conceding is a move, not a settings change.
      this.actPassArmed = false;
      this.cancelRun();
      if (this.confirming) void this.post([index]);
      else this.confirming = true;
      return;
    }
    this.handAnswer();
    this.confirming = false;
    if (d.min === 1 && d.max === 1) {
      // The remember checkbox (B4): a trigger_optional answered with the box
      // ticked stores the chosen index under the prompt's key BEFORE the
      // post, so the store records what the player answered, not whether the
      // server accepted it — an answer that stores then rejects is still the
      // player's answer to this prompt, and the next identical ask offers the
      // checkbox again to overwrite it.
      if (d.kind === 'trigger_optional' && this.rememberChoice) this.rememberAnswer(d, index);
      void this.post([index], opts?.holdPriority ?? false, undefined, true);
      return;
    }
    this.picked = pickOption(d, index, this.picked);
  }

  /**
   * toggle adds or removes one option from `picked` and never posts. click()
   * posts straight away on a min==max==1 decision because there the click IS
   * the answer, which is right for a priority option and wrong for bottoming:
   * bottoming one card has exactly that shape and is irreversible, so those
   * cards toggle and the player commits with the submit button.
   */
  toggle(index: number) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy) return;
    if (optionAt(d, index) === undefined) return;
    this.handAnswer();
    this.confirming = false;
    this.picked = pickOption(d, index, this.picked);
  }

  /**
   * unpick removes ONE instance of an option from the picked multiset of a
   * repeatable decision (a CanRepeatModes$ Charm) — the picked-so-far chips'
   * remove affordance. On a non-repeatable decision it is inert: toggle()
   * already owns removal there.
   */
  unpick(index: number) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy || !d.repeatable) return;
    if (optionAt(d, index) === undefined) return;
    this.handAnswer();
    this.confirming = false;
    this.picked = unpickOption(index, this.picked);
  }

  /**
   * setPicked replaces the picked set wholesale, in the given order — the
   * arrange surface's write path (brief Job 4): the popup's final keep order
   * IS the answer, so it must be writable as an order, not rebuilt click by
   * click. Every index must be an option of the pending decision and no
   * index may repeat — anything else is a programming error in the surface
   * that called it, not a player answer, and is refused rather than posted.
   * Like toggle, it never posts; the caller submits through the ordinary
   * submit(), whose min/max gate stays the only posting constraint.
   */
  setPicked(indices: readonly number[]) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy) return;
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- a local dedup scratch for one call, never stored on the state
    const seen = new Set<number>();
    for (const i of indices) {
      if (!Number.isInteger(i) || seen.has(i) || optionAt(d, i) === undefined) return;
      seen.add(i);
    }
    this.handAnswer();
    this.confirming = false;
    this.picked = [...indices];
  }

  /** Submit both ordered piles for a Restable arrange ask; keep the legacy
   * submit() path for ordinary asks and the inline offered-order answer. */
  submitArrange(keep: readonly number[], rest: readonly number[]) {
    const d = this.pending;
    if (d === null || d.kind !== 'arrange' || !d.restable || d.seq === this.postedSeq || this.busy) return;
    if (keep.length < d.min || keep.length > d.max || keep.length + rest.length !== d.options.length) return;
    // The two lists must partition the offered options, as the server's
    // Decision.Validate requires. Never send a malformed UI answer.
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local validation scratch, never stored in reactive state
    const seen = new Set<number>();
    for (const i of [...keep, ...rest]) {
      if (!Number.isInteger(i) || seen.has(i) || optionAt(d, i) === undefined) return;
      seen.add(i);
    }
    this.handAnswer();
    // `hand=true` because this IS a hand post: the popup's final keep order
    // is the player's own answer, and its options (the library cards the
    // arrange walker offers) carry objs, so it follows the one arming rule
    // exactly as click() and submit() do — a hand post whose answered
    // options carry an `obj` arms. An arrange follow-up is not the mana
    // wheel, so cardoptions.resolveCardFollowUp's kind gate keeps it off;
    // routing it here keeps the rule consistent rather than carving an
    // exception the next hand-post path would have to rediscover.
    void this.post([...keep], false, [...rest], true);
  }

  /** passClick posts only the pass-by-kind option, using its own wire index. */
  passClick() {
    const d = this.pending;
    const pass = this.passOption;
    if (d === null || pass === null || this.busy) return;
    this.handAnswer();
    void this.post([pass.index]);
  }

  /** primaryClick posts the primary-by-kind option directly. `holdPriority` (Ctrl held) skips the pass-after-acting arming for this one action. */
  primaryClick(holdPriority = false) {
    const d = this.pending;
    const p = d ? primaryOf(d) : null;
    if (d === null || p === null || d.seq === this.postedSeq || this.busy) return;
    this.handAnswer();
    void this.post([p.index], holdPriority);
  }

  /** confirmConcede posts the armed concede option — the second, explicit confirmation. It does not flip the persisted autoPass; it does kill an armed pass-after-acting token and a live run. */
  confirmConcede() {
    const d = this.pending;
    if (d === null || !this.confirming || this.busy) return;
    const concede = d.options.find(isConcede);
    if (!concede) return;
    this.actPassArmed = false;
    this.cancelPassWait();
    this.cancelRun();
    void this.post([concede.index]);
  }

  /** submit posts the picked set — gated on min/max; a rejected answer is recovered from, never treated as impossible. `holdPriority` (Ctrl held) skips the pass-after-acting arming for this one action. */
  submit(holdPriority = false) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy) return;
    if (this.picked.length < d.min || this.picked.length > d.max) return;
    this.handAnswer();
    void this.post([...this.picked], holdPriority, undefined, true);
  }

  /** submitPayment posts exactly one offered witness.  Looking it up again on
   * the current pending decision makes stale buttons inert after a seq swap,
   * and busy makes double-clicks inert while the request is in flight. */
  submitPayment(action: PaymentAction, plan: PaymentPlan, holdPriority = false) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy || d.kind !== 'priority') return;
    const offered = d.payment_actions?.find((candidate) => candidate.id === action.id);
    const offeredPlan = offered?.plans.find((candidate) => candidate.id === plan.id);
    // The button closes over the offered objects for one decision.  Refusing
    // a previous decision's otherwise-identical IDs prevents a delayed click
    // from attaching that witness to a new Seq.
    if (offered === undefined || offered !== action || offeredPlan === undefined || offeredPlan !== plan) return;
    this.handAnswer();
    void this.post([], holdPriority, undefined, true, { action_id: offered.id, plan: offeredPlan });
  }

  /**
   * continueEmpty is the Pending tray's empty-answer safety net (the Squadron
   * Hawk fail-to-find soft-lock; see lib/prompt stuckDecision): a pending
   * decision with NO options and Min 0 has exactly one legal answer, the
   * empty one, and no picker can offer it. It posts that answer through the
   * ordinary setPicked + submit gate. Anything else — a decision with options,
   * or a positive Min over nothing — is not answerable this way and is left
   * alone.
   */
  continueEmpty() {
    const d = this.pending;
    if (d === null || d.options.length > 0 || d.min !== 0) return;
    this.setPicked([]);
    this.submit();
  }

  private async post(choices: number[], holdPriority = false, rest?: number[], hand = false, payment?: Intent['payment']) {
    const d = this.pending;
    if (d === null || this.busy) return;
    this.busy = true;
    this.error = null;
    const epoch = this.seqEpoch;
    const options = choices.map((index) => ({ index, kind: d.options.find((option) => option.index === index)?.kind ?? 'unknown' }));
    clientBreadcrumbs.record('intent_sent', { decision_kind: d.kind, seq: d.seq, choices: options });
    try {
      await postIntent(this.table, this.match, { seq: d.seq, player: d.player, choices, ...(rest?.length ? { rest } : {}), ...(payment ? { payment } : {}) } satisfies Intent, this.ctx);
      // A rewind (or match boundary) landed while the post was in flight:
      // the response describes a seq space the client discarded. Touch
      // nothing — the restored decision, which can carry the SAME seq, must
      // not read as answered, and no error may surface for a post the
      // player's own undo superseded. The rewind path itself re-based the
      // panel (pending was cleared, the restored decision re-adopted).
      if (epoch !== this.seqEpoch) return;
      this.postedSeq = d.seq;
      // The armed card-follow-up expectation (fb-20260923T050205Z) lives on
      // the ACCEPTED hand post: a card action can hand the server a follow-up
      // decision for the same object (a treasure's activate -> its colour
      // ask; a multi-ability mana source's stage-1 pick -> its stage-2
      // wheel), and the seat panel's own option buttons post through exactly
      // this path, so arming here covers the panel exactly as the tile path
      // used to. The obj is the answered option's OWN wire obj (R-E4-1,
      // never a rebuilt position), and only an `activate`/`mana` answer arms
      // (followUpArm); every other answer disarms instead, so a stale
      // expectation can never survive into an unrelated window. Only a hand
      // post arms: the machine paths post a pass/resolve index and their
      // options carry no obj regardless.
      if (hand) {
        this.followUpExpected = followUpArm(d, choices);
        this.onFollowUpArm?.(this.followUpExpected);
      }
      // The hand answers that can carry a real action are click()'s post-on-click
      // (min == max == 1) and submit()'s multi-pick commit; both funnel through
      // here, so the pass-after-arming test lives on the ACCEPTED post — a
      // rejected intent never arms, and the gates are the preference itself
      // and the hold-priority modifier: with actPass off nothing is ever
      // armed, and a Ctrl-held action (hold priority) skips the arming for
      // that one post. passClick/primaryClick post only pass/resolve options,
      // the machine paths (auto, the one-shot runs, the empty-window floor,
      // the act-pass pass itself) post only the pass verdict's index, and a
      // non-priority decision fails the kind test, so none of them arm.
      // A mana tap (Kind "activate" on a priority window) fails the kind
      // test too — arming on the tap machine-passed the very window the
      // freshly floated mana unlocked (fb-3ab6d9da).
      // Concede never reaches here as an action: click() returns before
      // posting it once and confirmConcede posts a concede kind, which the
      // test rejects.
      if (this.actPass && !holdPriority && actedOption(d, choices)) this.actPassArmed = true;
      // If a new decision was adopted while the intent was in flight (a
      // rapid successive ask), keep it; only drop the decision we answered.
      if (this.pending?.seq === d.seq) this.pending = null;
      this.picked = [];
      this.confirming = false;
    } catch (e) {
      // Record every server refusal before deciding whether its seq space is
      // still current. A rewind must keep its UI recovery silent, but the
      // rejected request is exactly the breadcrumb a feedback report needs.
      const message = e instanceof Error ? e.message : String(e);
      const stale = epoch !== this.seqEpoch;
      clientBreadcrumbs.record('intent_rejected', { decision_kind: d.kind, seq: d.seq, message, stale });
      // A rejection against a discarded seq space (a rewind landed
      // mid-flight) is not an error the player can act on — the undo already
      // moved the game. Stay silent; the restored decision is pending.
      if (stale) return;
      // Surfaced, not swallowed: the intent was rejected (a stale seq, a
      // race, a refusal) and the game is exactly where it was — recover by
      // adopting the CURRENT decision rather than wedging on the stale one.
      this.picked = [];
      this.confirming = false;
      this.error = message;
      void this.refreshPending();
    } finally {
      // busy belongs to the post's seq epoch. A rewind can already have
      // released the old lock and a hand answer can have acquired a NEW one;
      // the old promise settling must not clear that new post's lock.
      if (epoch === this.seqEpoch) this.busy = false;
    }
  }

  /** refreshPending re-reads the current decision from /pending: the recovery path after a rejection, and the not-yet-viewed case at mount. A 409 conflict IS the normal "nothing pending" answer (the wait is on someone else), not an error. */
  async refreshPending() {
    try {
      const d = await fetchPending(this.table, this.match, this.ctx);
      this.adopt(d);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        this.adopt(null);
        return;
      }
      if (this.error === null) this.error = e instanceof Error ? e.message : String(e);
    }
  }
}
