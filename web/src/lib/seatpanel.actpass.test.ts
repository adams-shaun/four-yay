import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option, View } from '../protocol';
import { SeatPanelState, autoNoteText } from './seatpanel.svelte';

// The "pass after acting" preference (settings.passAfterAct): after this
// seat POSTS a hand answer to a priority decision carrying a real action,
// its NEXT priority window is passed once. "Real action" is actedOption's
// kind test: an option whose kind is neither pass, concede nor activate —
// the activate exclusion (fb-3ab6d9da) is what keeps a mana tap from arming
// the pass that would machine-pass the very window the floated mana just
// unlocked. decide() is the safety oracle and is tested in autopilot.test.ts;
// what these tests hold down is the token's lifecycle — what arms it, what
// consumes it, what clears it, and that the machine can only ever post a pass.
const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };

const cast = (i: number): Option => ({ index: i, kind: 'cast', label: `Cast ${i}`, player: 0 });
const ability = (i: number): Option => ({ index: i, kind: 'ability', label: `Ability ${i}`, player: 0 });
const activate = (i: number): Option => ({ index: i, kind: 'activate', label: `Tap source ${i} for mana`, player: 0 });
const pass = (i: number): Option => ({ index: i, kind: 'pass', label: 'Pass priority', player: 0 });
const concede = (i: number): Option => ({ index: i, kind: 'concede', label: 'Concede', player: 0 });
const target = (i: number): Option => ({ index: i, kind: 'target', label: `Target ${i}`, player: 0 });

/** quiet is a priority window with nothing to do: pass and concede only. */
const quiet = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [pass(0), concede(1)] });

/** live is a priority window where the player has a real action. */
const live = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [cast(0), pass(1), concede(2)] });

/** tappable is the pre-tap priority window (fb-3ab6d9da): a mana tap, a pass and a concede — no real action. */
const tappable = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [activate(0), pass(1), concede(2)] });

/** abilityWindow is a priority window whose only real action is a non-mana activated ability (Kind "ability"). */
const abilityWindow = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [ability(0), pass(1), concede(2)] });

/** choose is a non-priority decision — a target ask, the shape a cast hands back. */
const targetAsk = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'target', prompt: 'Choose a target.', min: 1, max: 1, options: [target(0), target(1)] });

const mulligan = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'mulligan', prompt: 'Keep?', min: 1, max: 1, options: [pass(0), concede(1)] });

/** multi is a multi-pick priority window (max > 1), answered with submit(). */
const multi = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 0, max: 2, options: [cast(0), ability(1), pass(2), concede(3)] });

const view = (step = 'draw', active = 0, stack: { id: number; controller: number }[] = []): View =>
  ({ active, step, turn: 2, stack }) as unknown as View;

/**
 * floatReadyView is a view whose seat 0 carries a projected potential action
 * (rv2c: the server's own legal-offer walk priced against the hypothetical
 * tapped-out pool — here, the Lava-Spike-after-the-Mountain shape as one
 * projected cast). It is what makes a mana-only tap window STOP-worthy
 * (castableAfterTap true) instead of empty-floor'd, so the tap tests exercise
 * the player's own click on a window that genuinely surfaces.
 */
const floatReadyView = (step = 'main1'): View =>
  ({
    active: 0,
    step,
    turn: 2,
    stack: [],
    players: [{
      seat: 0, name: 'P0', life: 20, lost: false, library_size: 40, hand_size: 1,
      graveyard_size: 0, battlefield: [], graveyard: [], exile: [],
      completed_dungeons: 0,
      pool: {},
      hand: [{ id: 7, name: 'Lava Spike', types: 'Instant', mana_cost: 'R', printing: {}, token: '' }],
      potential_actions: [{ kind: 'cast', obj: 7, label: 'Cast Lava Spike' }],
    }],
  }) as unknown as View;

function fakeStorage(): Storage {
  const store = new Map<string, string>();
  return {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
    key: (i: number) => [...store.keys()][i] ?? null,
    get length() {
      return store.size;
    },
  } as unknown as Storage;
}

async function settle(predicate: () => boolean, maxTicks = 200): Promise<void> {
  for (let i = 0; i < maxTicks; i++) {
    if (predicate()) return;
    await Promise.resolve();
  }
  throw new Error(`settle: condition still false after ${maxTicks} microtask ticks`);
}

/** manual is a seat with the preference ON, auto off and no stops set. Pacing is zeroed so machine passes post synchronously (the pre-prio5 path); pacing itself is tested in seatpanel.pacing.test.ts. */
function manual(storage: Storage | null = null): SeatPanelState {
  const p = new SeatPanelState('t1', 1, ctx, storage);
  p.stops = { yours: new Set(), opponents: new Set() };
  p.setAuto(false);
  p.setActPass(true);
  p.settings = { ...p.settings, pacing: { stepMs: 0, resolveMs: 0 } };
  return p;
}

/** arm posts one cast by hand so the token is armed for what follows. */
async function arm(p: SeatPanelState) {
  p.adoptView(live(1));
  p.click(0);
  await settle(() => p.postedSeq === 1);
  postIntentMock.mockClear();
}

describe('pass after acting — the preference and its persistence', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('is ON on a fresh seat (casual), and a fresh browser (no saved value) loads casual', () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    expect(p.actPass).toBe(true);
    expect(p.actPassed).toBe(0);
  });

  it('setActPass writes the ONE global settings key, shared by every seat in the browser', () => {
    const storage = fakeStorage();
    const p = new SeatPanelState('t1', 1, ctx, storage);
    p.setActPass(false);
    expect(p.actPass).toBe(false);
    expect(JSON.parse(storage.getItem('gorge.playsettings.v1') as string).passAfterAct).toBe(false);

    // a second seat in the same browser shares the player's settings — a
    // preference is a property of the player, not of the table or the seat.
    const other = new SeatPanelState('t1', 1, { seat: 1, token: 'tok' }, storage);
    expect(other.actPass).toBe(false);

    p.setActPass(true);
    const reloaded = new SeatPanelState('t1', 1, ctx, storage);
    expect(reloaded.actPass).toBe(true);
  });

  it('the preference survives a match boundary like the rest of the settings; the armed token and counter do not', () => {
    const p = manual();
    p.actPassed = 3;
    p.begin();
    expect(p.actPass).toBe(true); // persisted preference, not a per-match opt-in
    expect(p.actPassed).toBe(0);
  });
});

describe('pass after acting — what arms the token', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('THE REPORTED FLOW, end to end: cast click arms → the spell’s target decision is hand-answered → the next priority window is machine-passed exactly once, with its own note', async () => {
    const p = manual();
    // 1. the cast: a hand click on a live priority window posts the cast…
    p.adoptView(live(1));
    p.considerAuto(view()); // no token yet — the window surfaces as today
    expect(postIntentMock).not.toHaveBeenCalled();
    p.click(0);
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);

    // 2. the spell's target decision: non-priority, hand-answered by the
    // player. The token must survive it — and it must not ANSWER it.
    p.adoptView(targetAsk(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // never machine-answered
    p.click(0); // the target — a hand answer
    await settle(() => p.postedSeq === 2);
    expect(postIntentMock).toHaveBeenCalledTimes(2);

    // 3. the next priority window: machine-passed once, exactly once.
    p.adoptView(live(3));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 3);
    expect(postIntentMock).toHaveBeenCalledTimes(3);
    expect(postIntentMock.mock.calls[2][2].choices).toEqual([1]); // the PASS option's wire index
    expect(p.actPassed).toBe(1);
    expect(p.autoPassed).toBe(0); // never credited to auto
    expect(p.emptySkipped).toBe(0); // nor to the empty-window floor
    expect(autoNoteText(p.note)).toBe('Passed 1 priority window after your action.');

    // 4. exactly once: the window AFTER that is the player's again.
    p.adoptView(live(4));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(3);
    expect(p.actPassed).toBe(1);
  });

  it('a multi-pick submit arms too (the priority window the submit button answers)', async () => {
    const p = manual();
    p.adoptView(multi(1));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled(); // max > 1: the click toggles, it does not post
    p.click(0); // pick the cast
    expect(p.picked).toEqual([0]);
    p.submit();
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock.mock.calls[0][2].choices).toEqual([0]);

    p.adoptView(live(2));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 2);
    expect(p.actPassed).toBe(1);
  });

  it('a multi-pick submit of only passes does NOT arm', async () => {
    const p = manual();
    p.adoptView(multi(1));
    p.click(2); // the pass option
    p.submit();
    await settle(() => p.postedSeq === 1);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the submit only; no machine pass
  });

  it('Ctrl held while submitting a cast skips the pass-after-acting arming for that one action', async () => {
    const p = manual();
    p.adoptView(live(1));
    p.click(0, { holdPriority: true }); // the cast, Ctrl held: hold priority
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);

    // the NEXT window is the player's — nothing was armed
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.actPassed).toBe(0);
  });

  it('Ctrl held while submitting a multi-pick commit also skips the arming; the NEXT cast without Ctrl arms normally', async () => {
    const p = manual();
    p.adoptView(multi(1));
    p.click(0);
    p.submit(true); // Ctrl held
    await settle(() => p.postedSeq === 1);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the submit only; nothing armed
    expect(p.actPassed).toBe(0);

    // without Ctrl the same action arms as always
    p.click(0);
    await settle(() => p.postedSeq === 2);
    p.adoptView(live(3));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 3);
    expect(p.actPassed).toBe(1);
  });

  it.each([
    ['the dedicated pass button', (p: SeatPanelState) => p.passClick()],
    ['the primary (pass/resolve) button', (p: SeatPanelState) => p.primaryClick()],
  ])('%s posts its pass but does not arm', async (_name, act) => {
    const p = manual();
    p.adoptView(multi(1));
    act(p);
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the pass itself
    postIntentMock.mockClear();
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled(); // nothing was armed
  });

  it('a toggle without submit does not arm', async () => {
    const p = manual();
    p.adoptView(multi(1));
    p.toggle(0);
    await settle(() => p.busy === false);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('conceding does not arm', async () => {
    const p = manual();
    p.adoptView(live(1));
    p.click(2); // arms the confirmation
    expect(p.confirming).toBe(true);
    p.confirmConcede();
    await settle(() => p.postedSeq === 1);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the concede only
  });

  it('the empty-window floor’s own pass does not arm', async () => {
    const p = manual();
    p.adoptView(quiet(1));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 1);
    expect(p.emptySkipped).toBe(1);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the floor only; the live window is the player's
  });

  it('a hand action does NOT arm while the preference is off', async () => {
    const p = manual();
    p.setActPass(false);
    p.adoptView(live(1));
    p.click(0);
    await settle(() => p.postedSeq === 1);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the cast only
  });

  it('a rejected cast never arms: the next window is the player’s', async () => {
    const p = manual();
    postIntentMock.mockRejectedValueOnce(new Error('stale seq'));
    fetchPendingMock.mockResolvedValue(live(1));
    p.adoptView(live(1));
    p.click(0);
    await settle(() => p.busy === false && p.error !== null);
    p.adoptView(live(1)); // the same decision comes back
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the failed cast; no machine pass
  });
});

describe('pass after acting — arming boundaries: a mana tap is not an action (fb-3ab6d9da)', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('posting a mana tap (the priority-window activate option) does NOT arm — the next live window is the player\'s', async () => {
    const p = manual();
    // The pre-tap window: the hand holds a card castable once the untapped
    // source floats (the reported Lava-Spike-after-the-Mountain shape), so the
    // window surfaces instead of being floor-passed, and the engine offers a
    // tap for the source. The player taps for mana by hand.
    p.adoptView(tappable(1));
    p.considerAuto(floatReadyView()); // nothing armed yet: the window surfaces
    expect(postIntentMock).not.toHaveBeenCalled();
    p.click(0); // the tap — the exact click the reported flow makes
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(postIntentMock.mock.calls[0][2].choices).toEqual([0]);

    // The NEXT priority window — the one where the floated mana makes the
    // held spell affordable and the engine offers the cast — must be the
    // player's. Before the fix this window was machine-passed.
    p.adoptView(live(2));
    p.considerAuto(floatReadyView());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the tap only; nothing armed
    expect(p.actPassed).toBe(0);
  });

  it('posting a cast still arms (the boundary the tap fix must not dull)', async () => {
    const p = manual();
    await arm(p);
    p.adoptView(live(2));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 2);
    expect(p.actPassed).toBe(1);
    expect(postIntentMock.mock.calls[0][2].choices).toEqual([1]); // the pass index
  });

  it('posting an ability still arms — non-mana activated abilities are Kind "ability", not "activate"', async () => {
    const p = manual();
    p.adoptView(abilityWindow(1));
    p.click(0); // the ability — a real action
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    p.adoptView(live(2));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 2);
    expect(p.actPassed).toBe(1);
    expect(postIntentMock.mock.calls[1][2].choices).toEqual([1]);
  });

  it('the Ctrl-held skip still skips (the existing boundary survives the new exclusion)', async () => {
    const p = manual();
    p.adoptView(live(1));
    p.click(0, { holdPriority: true }); // the cast, Ctrl held: hold priority
    await settle(() => p.postedSeq === 1);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    p.adoptView(live(2));
    p.considerAuto(floatReadyView()); // nothing was armed — the window is the player's
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.actPassed).toBe(0);
  });
});

describe('pass after acting — where the token is spent and where it is not', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('a stop the player set on the current step/side is honoured: no post, token consumed without acting', async () => {
    const p = manual();
    p.stops = { yours: new Set(['main1']), opponents: new Set() };
    await arm(p);
    p.adoptView(live(4));
    p.considerAuto(view('main1', 0));
    expect(postIntentMock).not.toHaveBeenCalled();
    expect(autoNoteText(p.note)).toBe('Auto is off. You answer every window that offers you something to do.');

    // consumed: the NEXT window, away from the stop, is the player's again
    p.adoptView(live(5));
    p.considerAuto(view('draw', 0));
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('an opponent-controlled stack object is honoured: no post, token consumed without acting', async () => {
    const p = manual();
    await arm(p);
    p.adoptView(live(4));
    p.considerAuto(view('main1', 0, [{ id: 9, controller: 1 }]));
    expect(postIntentMock).not.toHaveBeenCalled();
    p.adoptView(live(5));
    p.considerAuto(view('draw', 0));
    expect(postIntentMock).not.toHaveBeenCalled(); // consumed, not deferred
  });

  it('a shape decide() does not understand is honoured: no post, token consumed', async () => {
    const p = manual();
    await arm(p);
    p.adoptView({ ...live(4), min: 0 });
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
    p.adoptView(live(5));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('a non-priority decision neither consumes the token nor is answered by it — the token fires at the NEXT priority window', async () => {
    const p = manual();
    await arm(p);
    p.adoptView(mulligan(4));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
    p.adoptView(live(5));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 5);
    expect(p.actPassed).toBe(1);
  });

  it('a failed machine pass never retries: the token is spent, the window surfaces to the player', async () => {
    const p = manual();
    await arm(p);
    postIntentMock.mockRejectedValueOnce(new Error('stale seq'));
    fetchPendingMock.mockResolvedValue(live(3));
    p.adoptView(live(3));
    p.considerAuto(view());
    await settle(() => p.busy === false && p.error !== null);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.actPassed).toBe(1); // counted when posted; the rejection surfaces as an error

    p.adoptView(live(3)); // the same seq comes back
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // no retry
    p.adoptView(live(4));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // and the next window is the player's — one-shot
  });
});

describe('pass after acting — dormant while a machine run is live', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('while Auto runs, its own rules govern and the token stays dormant: the pass is Auto’s, not the preference’s', async () => {
    const p = manual();
    p.setAuto(true);
    // Arm the token through a hand answer — which no longer disarms auto.
    p.adoptView(live(1));
    p.click(0);
    await settle(() => p.postedSeq === 1);
    expect(p.auto).toBe(true);
    postIntentMock.mockClear();

    // Auto's next window is answered by AUTO — counted and worded as Auto's;
    // the armed token stays dormant while the machine runs.
    p.adoptView(quiet(2));
    p.considerAuto(view('main1', 0));
    await settle(() => p.postedSeq === 2);
    expect(postIntentMock).toHaveBeenCalledTimes(1);
    expect(p.autoPassed).toBe(1);
    expect(p.actPassed).toBe(0);
    expect(autoNoteText(p.note)).toBe('Auto passed 1 priority window.');

    // Auto paused by hand via the switch; the still-armed token now spends
    // itself on the next live window.
    p.setAuto(false);
    p.adoptView(live(3));
    p.considerAuto(view('draw', 0));
    await settle(() => p.postedSeq === 3);
    expect(p.actPassed).toBe(1);
    expect(autoNoteText(p.note)).toBe('Passed 1 priority window after your action.');
  });

  it('while End Turn runs the token stays dormant, and spends itself after the run ends', async () => {
    const p = manual();
    p.stops = { yours: new Set(['draw']), opponents: new Set() };
    await arm(p); // armed, manual mode
    p.adoptView(quiet(2));
    p.startEndTurn(view('draw', 0)); // the run takes the wheel before the token is spent
    p.considerAuto(view('draw', 0));
    await settle(() => p.postedSeq === 2);
    expect(p.runPassed).toBe(1);
    expect(p.actPassed).toBe(0); // dormant, not consumed

    // The run ends at the player's stop… wait — a run ignores step stops;
    // the stop that ends it here is the non-priority ask below.
    p.adoptView(mulligan(3));
    p.considerAuto(view('draw', 0));
    expect(p.endTurn).toBe(false);
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the run's pass only
    expect(p.actPassed).toBe(0); // not consumed by the stop either

    p.adoptView(quiet(4));
    p.considerAuto(view('draw', 0));
    await settle(() => p.postedSeq === 4);
    expect(p.actPassed).toBe(1);
    expect(autoNoteText(p.note)).toBe('Passed 1 priority window after your action.');
  });

  it('turning the preference off disarms a still-armed token', async () => {
    const p = manual();
    await arm(p);
    p.setActPass(false);
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
  });
});

describe('pass after acting — what clears an armed token', () => {
  beforeEach(() => {
    postIntentMock.mockReset();
    fetchPendingMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
  });

  it('begin() clears it', async () => {
    const p = manual();
    await arm(p);
    p.begin();
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('Escape clears it, even in manual mode where auto’s own guards would skip', async () => {
    const p = manual();
    await arm(p);
    p.onKeydown('Escape');
    p.adoptView(live(2));
    p.considerAuto(view());
    expect(postIntentMock).not.toHaveBeenCalled();
  });

  it('conceding clears it (click() path, both clicks)', async () => {
    const p = manual();
    await arm(p);
    p.adoptView(live(2));
    p.click(2); // the concede option: first click arms the confirmation…
    expect(p.confirming).toBe(true);
    p.click(2); // …the second posts it — and must clear the armed token
    await settle(() => p.postedSeq === 2);
    p.adoptView(live(3));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the concede post only; no machine pass
  });

  it('confirmConcede() clears it too', async () => {
    const p = manual();
    await arm(p);
    p.adoptView(live(2));
    p.click(2); // arms the confirmation
    expect(p.confirming).toBe(true);
    p.confirmConcede(); // the explicit second confirmation
    await settle(() => p.postedSeq === 2);
    p.adoptView(live(3));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the concede post only; no machine pass
  });

  it('answering by hand while a one-shot run is live clears it (the cancel fired, the pass did not re-arm)', async () => {
    const p = manual();
    await arm(p);
    p.adoptView(live(2));
    p.startEndTurn(view()); // the run takes the wheel before considerAuto spends the token
    p.passClick(); // a human click during the run cancels it…
    await settle(() => p.postedSeq === 2);
    expect(p.endTurn).toBe(false);
    p.adoptView(live(3));
    p.considerAuto(view());
    expect(postIntentMock).toHaveBeenCalledTimes(1); // the passClick only; the token went with the cancel
  });
});
