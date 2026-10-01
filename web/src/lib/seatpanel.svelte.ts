import type { Decision, Intent, Option, PaymentAction, PaymentPlan, View } from '../protocol';
import { fetchPending, postIntent, ApiError } from './api';
import { safeStorage } from './storage';
import type { SeatCtx } from './seat';
import { checkBreakpoints, type BreakpointHit } from './breakpoints';
import { STOPPABLE_STEPS, actionables, decide, emptyPriorityWindow, passDiagnostics, type StopReason, type Stops, type TurnSide } from './autopilot';
import {
  applyPreset,
  cloneBreakpoints,
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
import { loadYields, saveYields, stackYieldKey } from './yields';
import {
  emptyStore,
  hasProfile,
  listProfiles,
  loadProfiles,
  normaliseName,
  saveProfiles,
  storeApply,
  storeDelete,
  storeRename,
  storeSave,
  storeSetActive,
  type ProfileStore,
} from './profiles';
import { exportFlow, importFlow } from './transfer';
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

import { actedOption, actedPayment, answersByToggle, followUpArm, identicalTriggerOrder, isConcede, optionAt, paymentActionForBase, pickOption, primaryOf, triggerOrderPermutation, unpickOption } from './prompts/decision';
import { MAX_DIGIT, renderedOrder } from './prompts/order';
import { attackAllPicks, noAttackAllowed, noBlocksAllowed } from './prompts/combat';
import { manaWindow, windowAction } from './announcepay';
import { AUTO_PASS_CAP, type AutoNote, type AutoOffReason, runStopNote } from './prompts/autonote';

// The pure decision helpers and the Auto note vocabulary live in
// lib/prompts/ (UI rework sub-project 4); they are re-exported here so every
// existing importer of this module keeps working.
export * from './prompts/decision';
export * from './prompts/autonote';


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
    // The saved profiles load here too, from their own key — a corrupt profile
    // blob yields the empty store and never touches the settings blob.
    this.profiles = loadProfiles(storage);
    this.activeProfileName = this.profiles.lastActive;
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
   * manual and toggling cannot send an engine intent.  It is also the ONE
   * auto-pay input to the autopilot (derivePass/deriveActPass hand it to
   * emptyPriorityWindow and decide(); spec §8): while it is on, a plan-bearing
   * payment action is a real play and Auto resolves the seat's own spell.
   * Toggling it never re-runs that classification by itself. */
  autoPayMana = $state(false);
  /** Set from the table's explicit auto_mana capability.  It only makes the
   * auto-pay switch available; it never reaches the pass policy, so a player
   * who leaves the switch off plays exactly as on a capability-less table. */
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
   * profiles is the player's saved configurations (lib/profiles.ts): a map of
   * named PlaySettings plus an order and the name last applied. It is loaded
   * from its OWN localStorage key at construction (SSR has no storage and
   * gets the empty store) and saved on every write. It deliberately does NOT
   * live inside `settings`: that blob's format is pinned, and the active
   * profile identity belongs to the profile store, not the settings object.
   */
  profiles = $state<ProfileStore>(emptyStore());

  /**
   * activeProfileName is the profile last applied and not since edited away
   * from, or null. Kept in component state (mirrored into the store's
   * lastActive) so the panel can label "(modified)" the moment the player
   * edits while a profile is active — the profile itself is only rewritten by
   * an explicit Save, never by an edit.
   */
  activeProfileName = $state<string | null>(null);

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
   * dockCount is how many prompt docks are mounted against this seat (UI
   * rework spec §4). While one is, it is the answer surface for every
   * non-priority decision: the ACTIONS strip points at it instead of drawing
   * a second copy, and does not pop open for it. Display-only.
   */
  dockCount = $state(0);

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

  /**
   * bpFired maps a breakpoint key (lib/breakpoints) to the decision seq it
   * first stopped at. decide() re-stops that same seq on every re-derive and
   * passes later windows for the same key. Session-scoped: begin() clears it.
   * Only the first live hit per window is recorded; a second matching key
   * pauses the next window, though arming a run on that window acknowledges
   * every live key (acknowledgeBreakpoints).
   */
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- private loop bookkeeping decide() reads, never rendered; the note is the reactive surface
  private bpFired = new Map<string, number>();

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
    // A named preset is not a profile: the active-profile identity clears.
    this.activeProfileName = null;
    this.persistProfiles(storeSetActive(this.profiles, null));
  }

  /** profileNames is the saved profiles' names in pinned order (never map iteration order). */
  get profileNames(): string[] {
    return listProfiles(this.profiles);
  }

  /**
   * profileModified reports whether the live settings differ from the stored
   * copy of the active profile — the panel's "(modified)" marker. Editing
   * while a profile is active does NOT auto-save: only an explicit Save
   * rewrites the entry.
   */
  get profileModified(): boolean {
    if (this.activeProfileName === null) return false;
    const saved = this.profiles.profiles[this.activeProfileName];
    if (saved === undefined) return false;
    return JSON.stringify(saved) !== JSON.stringify(this.settings);
  }

  /** persistProfiles swaps the whole profile store and persists it to its own key. */
  private persistProfiles(next: ProfileStore) {
    this.profiles = next;
    saveProfiles(this.storage, next);
  }

  /**
   * saveProfile stores the LIVE settings under a name (explicit Save only) and
   * marks that profile active. A rejected name (empty, too long) is a no-op
   * returning false so the panel can report it; the entry is deep-cloned, so
   * later edits cannot mutate the saved copy.
   */
  saveProfile(name: string): boolean {
    const clean = normaliseName(name);
    if (clean === null) return false;
    this.persistProfiles(storeSave(this.profiles, clean, this.settings));
    this.activeProfileName = clean;
    return true;
  }

  /**
   * applyProfile applies a saved profile through withChange (so its preset
   * label is re-derived from the configuration, normally 'custom') with the
   * SAME machine-side re-arm effects as applyNamedPreset: a profile that runs
   * auto must clear the runaway brake and reset the run counters, or the panel
   * would read auto-pass on while considerAuto keeps refusing to act.
   */
  applyProfile(name: string): boolean {
    const s = storeApply(this.profiles, name);
    if (s === null) return false;
    this.cancelRun(false);
    this.machinePaused = false;
    this.autoRun = 0;
    this.autoActedSeq = null;
    // withChange relabels the preset by deep-equality against the named
    // presets; storeApply already returns the earned label, but the call also
    // normalises a stored label that drifted from its configuration.
    this.applySettings(withChange(s, {}));
    this.activeProfileName = name;
    this.persistProfiles(storeSetActive(this.profiles, name));
    this.note = this.auto ? { kind: 'armed' } : { kind: 'off' };
    return true;
  }

  /** deleteProfile removes a saved profile; deleting the active one clears the active identity. */
  deleteProfile(name: string): boolean {
    if (!hasProfile(this.profiles, name)) return false;
    this.persistProfiles(storeDelete(this.profiles, name));
    if (this.activeProfileName === name) this.activeProfileName = null;
    return true;
  }

  /** exportProfilesText is the Export button's payload: every saved flow profile, in order. */
  exportProfilesText(): string {
    return exportFlow(this.profiles);
  }

  /** importProfilesText merges a file's profiles in (never overwriting) and returns the result in plain words. */
  importProfilesText(text: string): string {
    const got = importFlow(text, this.profiles);
    if ('error' in got) return got.error;
    if (got.added.length > 0) this.persistProfiles(got.store);
    const skipped = got.skipped > 0 ? ` ${got.skipped} could not be read and ${got.skipped === 1 ? 'was' : 'were'} skipped.` : '';
    return got.added.length === 0 ? `Nothing imported.${skipped}` : `Imported ${got.added.join(', ')}.${skipped}`;
  }

  /**
   * renameProfile renames in place (order preserved). A rejected new name, an
   * absent old name, or a name already taken is a no-op returning false; the
   * active identity follows the rename.
   */
  renameProfile(oldName: string, newName: string): boolean {
    const next = storeRename(this.profiles, oldName, newName);
    if (next === this.profiles) return false;
    this.persistProfiles(next);
    if (this.activeProfileName === oldName) this.activeProfileName = normaliseName(newName);
    return true;
  }

  /**
   * cycleProfile steps the profile list by one (the Ctrl+Shift+]/[ hotkeys),
   * wrapping. The cycle runs over the NAMED PRESETS plus saved profiles — the
   * presets are the first three entries, in PRESET_LIST order — so a player
   * with no saved profiles can still hotkey between the shipped presets.
   * Returns the new label, or null when there is nothing to step to.
   */
  cycleProfile(delta: number, presetIds: readonly PresetName[]): string | null {
    const names: string[] = [...presetIds];
    const saved = listProfiles(this.profiles);
    const current = this.activeProfileName;
    let index: number;
    if (current !== null && saved.includes(current)) {
      index = presetIds.length + saved.indexOf(current);
    } else {
      index = presetIds.indexOf(this.settings.preset as PresetName);
      if (index < 0) index = 0;
    }
    const size = names.length + saved.length;
    if (size === 0) return null;
    const next = (((index + delta) % size) + size) % size;
    if (next < names.length) {
      this.applyNamedPreset(names[next] as PresetName);
      return names[next];
    }
    const name = saved[next - names.length];
    this.applyProfile(name);
    return name;
  }

  /** applyProfileAt applies entry i (0-based) of the cycle's list — presets first, then saved profiles — for the profile-N hotkeys. */
  applyProfileAt(i: number, presetIds: readonly PresetName[]): string | null {
    const saved = listProfiles(this.profiles);
    if (i < 0 || i >= presetIds.length + saved.length) return null;
    if (i < presetIds.length) {
      this.applyNamedPreset(presetIds[i]);
      return presetIds[i];
    }
    const name = saved[i - presetIds.length];
    this.applyProfile(name);
    return name;
  }

  /**
   * pickHotkey answers option n (1-based) of a pending decision exactly as
   * clicking the row numbered n would (the pick-N hotkeys): a toggle on the
   * layouts that answer by toggle-then-commit (answersByToggle), a click()
   * everywhere else. The row is resolved through renderedOrder
   * (lib/prompts/order.ts) — the same list every renderer numbers its rows
   * from — so the digit on screen and the key always name the same option,
   * including the sorted/filtered library search, the name pick and the
   * select-mana window. It refuses (returns false, so the key is not
   * consumed) where nothing is numbered: priority windows, a digit past the
   * rendered rows, a posted or in-flight answer.
   */
  pickHotkey(n: number): boolean {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy) return false;
    const order = renderedOrder(d, { filter: this.searchFilter });
    if (order === null || n < 1 || n > MAX_DIGIT) return false;
    const index = order[n - 1];
    if (index === undefined) return false;
    if (answersByToggle(d)) this.toggle(index);
    else this.click(index);
    return true;
  }

  /**
   * attackWithAll is the "attack with all" answer (keymap action and the
   * attackers prompt's button): it SELECTS one pairing per creature that can
   * attack (attackAllPicks) and leaves the commit to the player. False when
   * the pending decision is not a declare-attackers ask.
   */
  attackWithAll(): boolean {
    const d = this.pending;
    if (d === null || d.kind !== 'attackers' || d.seq === this.postedSeq || this.busy) return false;
    const picks = attackAllPicks(d);
    if (picks.length === 0) return false;
    this.setPicked(picks);
    return true;
  }

  /**
   * declareNone commits the empty combat declaration ("No blocks", "No
   * attack") straight away. It refuses when the empty answer is not one the
   * client may send: a forced block or attack, or a positive min.
   */
  declareNone(): boolean {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy) return false;
    if (!noBlocksAllowed(d) && !noAttackAllowed(d)) return false;
    this.setPicked([]);
    this.submit();
    return true;
  }

  /** noBlocks is the keymap's "no blocks": declareNone on a declare-blockers ask only. */
  noBlocks(): boolean {
    return this.pending?.kind === 'blockers' && this.declareNone();
  }

  /**
   * autoPay is the keymap's "auto-pay": the select-mana window's Auto-fill
   * option (announce-then-pay §4), posted through the ordinary click path.
   * False outside that window, so the key is not consumed.
   */
  autoPay(): boolean {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy || manaWindow(d) === null) return false;
    const fill = windowAction(d, 'autofill');
    if (fill === null) return false;
    this.click(fill.index);
    return true;
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
   * without a backup restores the defaults rather than guessing. Both ways
   * carry the CURRENT breakpoints: they are the player's, not the preset's
   * (playsettings Breakpoints), so neither the preset nor a backup taken
   * before an edit made in full control may replace them.
   */
  toggleFullControl() {
    this.cancelRun(false);
    this.autoRun = 0;
    this.autoActedSeq = null;
    const breakpoints = cloneBreakpoints(this.settings.breakpoints);
    if (this.settings.preset === 'full-control') {
      const back = this.presetBackup;
      this.presetBackup = null;
      this.applySettings({ ...(back ?? defaultSettings()), breakpoints });
    } else {
      this.presetBackup = this.settings;
      this.applySettings({ ...applyPreset('full-control'), breakpoints });
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

  /**
   * acknowledgeBreakpoints is arming a run on the window a breakpoint
   * stopped: the player's acknowledgement of that pause. decide() keeps a
   * hit live for the seq it fired at, so without this the run would stop
   * again on the very window it was pressed on and the button would look
   * dead. Only the FIRST live hit per window was recorded, so re-marking just
   * the recorded keys is not enough: a second rule live on the same window
   * (targets-me with stack-depth) would stop the run at once. So every hit
   * live at the pending seq is marked as fired one seq earlier — decide()
   * then reads each as already fired, and later windows stay passed too —
   * with the same skipTop inputs decide() will use for the run (the Resolve
   * All baseline or a yield). A window no breakpoint stopped acknowledges
   * nothing: a pause the player has not seen yet still stops the run.
   */
  private acknowledgeBreakpoints(view: View, baseline: ReadonlySet<number> | null) {
    const seq = this.pending?.seq;
    if (seq === undefined || ![...this.bpFired.values()].includes(seq)) return;
    for (const [key, at] of this.bpFired) if (at === seq) this.bpFired.set(key, seq - 1);
    const top = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
    const skipTop = top !== null && ((baseline?.has(top.id) ?? false) || this.yields.has(stackYieldKey(top)));
    // Each pass marks one more key not-live; there are at most four rules, so this ends.
    for (;;) {
      const hit = checkBreakpoints({ view, seat: this.ctx.seat, bp: this.settings.breakpoints, fired: this.bpFired, seq, skipTop });
      if (hit === null) return;
      this.bpFired.set(hit.key, seq - 1);
    }
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
    this.acknowledgeBreakpoints(view, baseline);
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
      // stop verdict: a run honours no step stops, and the one pause it
      // can meet on the window it was pressed on — a breakpoint — was
      // acknowledged when the run was armed (acknowledgeBreakpoints), so the
      // press itself moves that window. Persistent Auto stays armed: the
      // player answers this window and Auto resumes after it — and with
      // hand answers no longer disarming Auto (prio3), no exception
      // token is needed or minted.
      this.autoRun = 0;
      if (verdict.reason === 'breakpoint' && verdict.hit) this.bpFired.set(verdict.hit.key, d.seq);
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
        // other reason, carry no detail and keep the base wording. The labels
        // read the same auto-pay preference decide() was handed, so a stop a
        // planned cast made is named as that cast.
        const labels = verdict.reason === 'stop-set' ? actionables(view, this.ctx.seat, d, this.autoPayMana) : [];
        this.note = verdict.reason === 'breakpoint' && verdict.hit
          ? { kind: 'waiting', reason: 'breakpoint', detail: verdict.hit.detail }
          : labels.length > 0
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
    | { act: 'stop'; reason: StopReason; hit?: BreakpointHit }
    | null {
    const d = this.pending;
    if (d === null) return null;
    // Auto-pay changes which witness an explicit cast uses; it is not a pass
    // policy of its own, so there is deliberately no auto-pay guard here (the
    // blanket one that held every window carrying a plan was removed by
    // 4757be4e9, squashed into 7022042e6). It reaches this classification
    // only as the seat PREFERENCE handed to the one shared actionable test
    // (spec §8): while it is on, a plan-bearing payment action is a real play,
    // so neither the floor nor decide() passes a window whose only play is a
    // plan-only cast. The table capability (autoManaAvailable) never reaches
    // here: with the preference off this is exactly the capability-less policy.
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
      const index = this.skipEmpty ? emptyPriorityWindow(d, view, this.ctx.seat, this.autoPayMana) : null;
      if (index === null) return null;
      // The floor honours breakpoints too: a pause the player set outranks
      // a window that merely asks nothing. Same inputs decide() uses (no run
      // baseline here — no run is armed — so only a yield skips the top).
      const top = view.stack.length > 0 ? view.stack[view.stack.length - 1] : null;
      const hit = checkBreakpoints({
        view,
        seat: this.ctx.seat,
        bp: this.settings.breakpoints,
        fired: this.bpFired,
        seq: d.seq,
        skipTop: top !== null && this.yields.has(stackYieldKey(top)),
      });
      if (hit !== null) return { act: 'stop', reason: 'breakpoint', hit };
      return { act: 'pass', index, kind: 'empty', reason: 'empty-window' };
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
      // The seat's auto-pay PREFERENCE, never the table capability (spec §8).
      autoPayMana: this.autoPayMana,
      breakpointsFired: this.bpFired,
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
      const decision = this.pending;
      const diagnostics = decision?.kind === 'priority'
        ? {
            ...passDiagnostics(decision, view, this.ctx.seat, `${kind}: ${reason}`, this.yields, this.autoPayMana),
            // View is the server's seat-redacted projection. Keep a bounded
            // detached copy so later updates cannot rewrite this pass-time state.
            view: JSON.parse(JSON.stringify(view)) as View,
          }
        : undefined;
      this.autoLog = pushAutoPassLog(this.autoLog, autoPassLogText(kind, view, this.ctx.seat), view.turn, undefined, diagnostics);
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
    const verdict = decide({ decision: d, view, seat: this.ctx.seat, settings: { ...this.settings, autoPass: true }, yields: this.yields, autoPayMana: this.autoPayMana, breakpointsFired: this.bpFired });
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
    this.bpFired.clear();
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

  /** submitAnnounce posts the announce-then-pay selector for one offered cast
   * (docs/superpowers/specs/2026-09-27-announce-then-pay.md
   * §3): the engine begins the cast and poses the "select mana" window. Like
   * submitPayment it re-resolves the action on the current decision, so a
   * stale button is inert after a seq swap. */
  submitAnnounce(action: PaymentAction, holdPriority = false) {
    const d = this.pending;
    if (d === null || d.seq === this.postedSeq || this.busy || d.kind !== 'priority') return;
    const offered = d.payment_actions?.find((candidate) => candidate.id === action.id);
    if (offered === undefined || offered !== action) return;
    this.handAnswer();
    void this.post([], holdPriority, undefined, true, undefined, { action_id: offered.id });
  }

  /** castAction is the one CAST route for an offered cast (a hand card's
   * CAST shortcut, the panel's cast row): Auto-pay ON submits the suggested
   * plan, as before; OFF posts the legacy cast when the pool already pays
   * (base_option_index) and otherwise announces the cast so the player picks
   * the mana (announce-then-pay spec §8). A plan-less action (ManaBrew only —
   * the native planner never publishes one, rules/payment_plan.go appends an
   * action only with a plan) has no plan to submit in either mode, so it
   * falls back to announce-then-pay under Auto-pay ON too. Never a
   * synthesized plan. */
  castAction(action: PaymentAction, holdPriority = false) {
    if (this.autoPayMana) {
      const plan = action.plans[0];
      if (plan !== undefined) {
        this.submitPayment(action, plan, holdPriority);
        return;
      }
      // ManaBrew carries no payment plan; announce is its supported route to
      // the select-mana window, in both Auto-pay modes.
      this.submitAnnounce(action, holdPriority);
      return;
    }
    if (action.base_option_index !== undefined && action.base_option_index !== null) {
      this.click(action.base_option_index, { holdPriority });
      return;
    }
    this.submitAnnounce(action, holdPriority);
  }

  private async post(choices: number[], holdPriority = false, rest?: number[], hand = false, payment?: Intent['payment'], announce?: Intent['announce']) {
    const d = this.pending;
    if (d === null || this.busy) return;
    this.busy = true;
    this.error = null;
    const epoch = this.seqEpoch;
    const options = choices.map((index) => ({ index, kind: d.options.find((option) => option.index === index)?.kind ?? 'unknown' }));
    clientBreadcrumbs.record('intent_sent', { decision_kind: d.kind, seq: d.seq, choices: options });
    try {
      await postIntent(this.table, this.match, { seq: d.seq, player: d.player, choices, ...(rest?.length ? { rest } : {}), ...(payment ? { payment } : {}), ...(announce ? { announce } : {}) } satisfies Intent, this.ctx);
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
      // (min == max == 1), submit()'s multi-pick commit and submitPayment()'s
      // planned cast; all funnel through here, so the pass-after-arming test
      // lives on the ACCEPTED post — a
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
      // A planned cast (submitPayment: the seat-panel/hot-strip plan button,
      // the hand CAST shortcut, and click()'s auto-pay branch for a legacy
      // cast) posts no choices, only its payment selection; actedPayment
      // counts it exactly as actedOption counts the same cast's legacy
      // option, under the same gates: accepted posts only, the preference on,
      // and Ctrl (holdPriority) exempt (spec §8).
      if (this.actPass && !holdPriority && (actedOption(d, choices) || actedPayment(d, payment))) this.actPassArmed = true;
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
    // A read issued before a rewind (or match boundary) describes the seq
    // space begin() discarded, exactly like an in-flight post (seqEpoch). Its
    // decision must not be adopted into the fresh space, where the seqHigh
    // fence has been reset and would then refuse the restored, lower-seq ask
    // for good; nor may its 409 clear that restored ask.
    const epoch = this.seqEpoch;
    try {
      const d = await fetchPending(this.table, this.match, this.ctx);
      if (epoch !== this.seqEpoch) return;
      this.adopt(d);
    } catch (e) {
      if (epoch !== this.seqEpoch) return;
      if (e instanceof ApiError && e.status === 409) {
        this.adopt(null);
        return;
      }
      if (this.error === null) this.error = e instanceof Error ? e.message : String(e);
    }
  }
}
