import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Decision, Option, View } from '../protocol';
import { ApiError } from './api';
import { rememberKey } from './remembered';
import { SeatPanelState, autoNoteText } from './seatpanel.svelte';
import { clientBreadcrumbs } from './breadcrumbs';

// The UNDO pause (fb-20260914T063523Z): pressing UNDO rewinds the board to
// the decision point of the player's last action, and the autopilot machine
// — auto-pass, the empty-window floor, pass-after-acting, the
// identical-trigger auto-order, remembered optional-trigger answers — must
// NOT instantly re-answer that restored window. rewind() sets machinePaused
// (the runaway brake, never the
// persisted preference); only the player's own resume paths clear it.
// The server half (host/undo.go, one intent per click) is already correct
// and untouched; these tests hold the client half.

const { postIntentMock, fetchPendingMock } = vi.hoisted(() => ({ postIntentMock: vi.fn(), fetchPendingMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
  fetchPending: fetchPendingMock,
}));

const ctx = { seat: 0, token: 'tok' };

const pass = (i: number): Option => ({ index: i, kind: 'pass', label: 'Pass priority', player: 0 });
const concede = (i: number): Option => ({ index: i, kind: 'concede', label: 'Concede', player: 0 });
const cast = (i: number): Option => ({ index: i, kind: 'cast', label: `Cast ${i}`, player: 0 });
const trigger = (i: number, label: string, obj: number): Option => ({ index: i, kind: 'trigger', label, obj, player: 0 });

/** quiet is a priority window with nothing to do: pass and concede only. */
const quiet = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [pass(0), concede(1)] });

/** live is a priority window where the player has a real action. */
const live = (seq: number): Decision =>
  ({ seq, player: 0, kind: 'priority', prompt: 'You have priority.', min: 1, max: 1, options: [cast(0), pass(1), concede(2)] });

/** triggerOrder is the measured wire shape of a trigger_order decision (mirrors seatpanel.prio6.test.ts). */
const triggerOrder = (seq: number, labels: [string, string]): Decision => ({
  seq,
  player: 0,
  kind: 'trigger_order',
  prompt: 'Order your simultaneous triggered abilities.',
  min: 2,
  max: 2,
  options: labels.map((label, i) => trigger(i, label, 81 + i)),
});
const IDENTICAL: [string, string] = ['Blood Artist: loses 1 life', 'Blood Artist: loses 1 life'];
const OPTIONAL_PROMPT = 'Put this optional triggered ability on the stack? — Bloodghast: Whenever a land enters, Bloodghast may return from the graveyard';
const triggerOptional = (seq: number): Decision => ({
  seq,
  player: 0,
  kind: 'trigger_optional',
  prompt: OPTIONAL_PROMPT,
  min: 1,
  max: 1,
  options: [
    { index: 0, kind: 'yes', label: 'Yes — return Bloodghast', player: 0 },
    { index: 1, kind: 'no', label: 'No', player: 0 },
  ],
});
const choose = (seq: number): Decision => ({
  seq,
  player: 0,
  kind: 'choose',
  prompt: 'Choose one or two cards.',
  min: 1,
  max: 2,
  options: [
    { index: 0, kind: 'card', label: 'Card 0', obj: 90, player: 0 },
    { index: 1, kind: 'card', label: 'Card 1', obj: 91, player: 0 },
  ],
});

const view = (step = 'draw', active = 0, turn = 2, stack: { id: number; controller: number; kind?: string }[] = []): View =>
  ({ active, step, turn, stack }) as unknown as View;

/** armedSeat is casual (auto ON — the default) with every stop off and zero pacing, so machine passes post synchronously. */
function armedSeat(): SeatPanelState {
  const p = new SeatPanelState('t1', 1, ctx, null);
  p.stops = { yours: new Set(), opponents: new Set() };
  p.setAuto(true);
  p.settings = { ...p.settings, pacing: { stepMs: 0, resolveMs: 0 } };
  return p;
}

/** manualSeat is casual with auto off — the empty-window floor is the only machine path left. */
function manualSeat(): SeatPanelState {
  const p = armedSeat();
  p.setAuto(false);
  return p;
}

function deferred() {
  let resolve!: () => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<void>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function settle(predicate: () => boolean, maxTicks = 200): Promise<void> {
  for (let i = 0; i < maxTicks; i++) {
    if (predicate()) return;
    await Promise.resolve();
  }
  throw new Error(`settle: condition still false after ${maxTicks} microtask ticks`);
}

async function drain(ticks = 20): Promise<void> {
  for (let i = 0; i < ticks; i++) await Promise.resolve();
}

const seqs = () => postIntentMock.mock.calls.map((c) => (c[2] as { seq: number }).seq);

beforeEach(() => {
  postIntentMock.mockReset();
  fetchPendingMock.mockReset();
  postIntentMock.mockResolvedValue(undefined);
});

describe('the undo pause', () => {
  it('rewind pauses the machine, surfaces the paused note, and leaves the persisted autoPass untouched', () => {
    const p = armedSeat();
    p.adoptView(quiet(1));
    p.considerAuto(view());
    p.rewind();
    expect(p.machinePaused).toBe(true);
    expect(p.note.kind).toBe('paused');
    expect(autoNoteText(p.note)).toContain('Undo paused automatic passing');
    // The preference is the player's, not the match's: a reload comes back
    // with auto exactly as it was.
    expect(p.auto).toBe(true);
    expect(p.settings.autoPass).toBe(true);
  });

  it('with auto ON, a rewind leaves the restored decision PENDING until the player answers by hand', async () => {
    const p = armedSeat();
    // The pre-undo game: the player casts by hand (seq 1), then auto passes
    // a quiet window (seq 2).
    p.adoptView(live(1));
    p.click(0);
    await settle(() => p.postedSeq === 1);
    p.adoptView(quiet(2));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 2);
    expect(seqs()).toEqual([1, 2]);

    // UNDO: the board rewinds to the seq-2 decision point (same seq, a fresh
    // object from the rewind snapshot — as the real adopt path delivers it).
    p.rewind();
    p.adoptView(quiet(2));
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 2); // nothing new posts
    expect(p.machinePaused).toBe(true);
    expect(p.pending?.seq).toBe(2);
    expect(p.active?.seq).toBe(2); // the window is live for the player

    // The player answers it by hand — the only post that happens.
    p.click(0); // pass
    await settle(() => postIntentMock.mock.calls.length === 3);
    expect(seqs()[2]).toBe(2);
    // A hand answer while paused does NOT clear the brake (only the Auto
    // switch or a preset resumes the machine).
    expect(p.machinePaused).toBe(true);
  });

  it('the empty-window floor does not pass a restored empty window while paused; the floor resumes when the pause lifts', async () => {
    const p = manualSeat(); // auto off, skipEmpty on (the default) — the floor is the machine path that fires even with auto off
    p.adoptView(quiet(5));
    p.rewind();
    p.adoptView(quiet(5));
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 0);
    expect(p.pending?.seq).toBe(5);
    expect(p.emptySkipped).toBe(0);

    // Resume: the Auto switch clears the brake, and the machine paths run again.
    p.setAuto(true);
    expect(p.machinePaused).toBe(false);
    p.considerAuto(view());
    await settle(() => p.postedSeq === 5);
    expect(seqs()).toEqual([5]);
  });

  it('maybeAutoOrderTriggers does not auto-submit a restored identical trigger-order ask while paused; it resumes after the brake clears', async () => {
    const p = armedSeat();
    p.adoptView(triggerOrder(7, IDENTICAL)); // pre-undo: the machine ordered it
    await settle(() => p.postedSeq === 7);
    expect(seqs()).toEqual([7]);

    p.rewind();
    p.adoptView(triggerOrder(7, IDENTICAL));
    await settle(() => postIntentMock.mock.calls.length === 1); // no auto-submit, at adopt or after
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 1);
    expect(p.pending?.seq).toBe(7);
    expect(p.autoLog.some((n) => n.text.includes('identical triggers'))).toBe(false);

    // Resume by applying a named preset: the ask is then auto-ordered as before.
    p.applyNamedPreset('casual');
    expect(p.machinePaused).toBe(false);
    p.considerAuto(view());
    await settle(() => p.postedSeq === 7);
    expect(seqs()).toEqual([7, 7]);
  });

  it('a remembered optional-trigger choice does not answer the restored ask while paused', async () => {
    const p = armedSeat();
    p.remembered = {
      version: 1,
      entries: [{ key: rememberKey('trigger_optional', OPTIONAL_PROMPT), choice: 0, label: 'Bloodghast', savedAt: 1 }],
    };

    // Before the undo, the saved answer owns the ask.
    p.adoptView(triggerOptional(8));
    await settle(() => p.postedSeq === 8);
    expect(seqs()).toEqual([8]);

    // Rewind clears the old seq guards, but the brake must still stop this
    // machine-answer sibling at both adopt and considerAuto.
    p.rewind();
    p.adoptView(triggerOptional(8));
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 1);
    expect(p.pending?.seq).toBe(8);
    expect(p.machinePaused).toBe(true);

    // The ordinary resume path restores remembered-answer automation too.
    p.pressAuto();
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 2);
    expect(seqs()).toEqual([8, 8]);
  });

  it('a lost pre-rewind post does not keep the restored window busy: PASS posts immediately', async () => {
    const p = armedSeat();
    const lost = deferred();
    postIntentMock.mockReturnValueOnce(lost.promise).mockResolvedValueOnce(undefined);

    p.adoptView(live(1));
    p.click(0); // the old-space intent never settles
    expect(p.busy).toBe(true);
    expect(postIntentMock).toHaveBeenCalledTimes(1);

    p.rewind();
    p.adoptView(live(1)); // restored decision: SAME seq, fresh object
    expect(p.busy).toBe(false);
    expect(p.active?.seq).toBe(1);
    p.passClick();

    // Do not release `lost` before this assertion: rewind itself must release
    // the stale busy lock. Waiting for the abandoned response can wedge the
    // restored decision forever in a real browser.
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    await settle(() => p.postedSeq === 1);
    expect(p.busy).toBe(false);
    expect(p.pending).toBeNull();
    expect(p.machinePaused).toBe(true);
  });

  it('a late success from the old epoch cannot clear or mutate a new in-flight post', async () => {
    const p = armedSeat();
    const oldPost = deferred();
    const newPost = deferred();
    postIntentMock.mockReturnValueOnce(oldPost.promise).mockReturnValueOnce(newPost.promise);

    p.adoptView(live(1));
    p.click(0);
    p.rewind();
    const restored = choose(1);
    p.adoptView(restored);
    p.toggle(0);
    p.submit();
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    expect(p.busy).toBe(true);
    expect(p.pending).toBe(restored);
    expect(p.picked).toEqual([0]);

    oldPost.resolve();
    await drain();
    // All four values belong to the restored epoch/new post. The old
    // completion may touch none of them — especially not busy, or a second
    // click could overlap the still-live new request.
    expect(p.busy).toBe(true);
    expect(p.error).toBeNull();
    expect(p.pending).toBe(restored);
    expect(p.picked).toEqual([0]);
    expect(p.postedSeq).toBeNull();

    newPost.resolve();
    await settle(() => p.postedSeq === 1 && !p.busy);
    expect(p.pending).toBeNull();
    expect(p.picked).toEqual([]);
  });

  it('a late rejection from the old epoch remains a stale breadcrumb without mutating the restored post', async () => {
    const record = vi.spyOn(clientBreadcrumbs, 'record');
    const p = armedSeat();
    const oldPost = deferred();
    const newPost = deferred();
    postIntentMock.mockReturnValueOnce(oldPost.promise).mockReturnValueOnce(newPost.promise);

    p.adoptView(live(1));
    p.click(0);
    p.rewind();
    const restored = choose(1);
    p.adoptView(restored);
    p.toggle(1);
    p.submit();
    expect(postIntentMock).toHaveBeenCalledTimes(2);
    expect(p.busy).toBe(true);

    oldPost.reject(new Error('old seq rejected'));
    await drain();
    expect(record).toHaveBeenCalledWith('intent_rejected', {
      decision_kind: 'priority', seq: 1, message: 'old seq rejected', stale: true,
    });
    expect(p.busy).toBe(true);
    expect(p.error).toBeNull();
    expect(p.pending).toBe(restored);
    expect(p.picked).toEqual([1]);
    expect(p.postedSeq).toBeNull();
    expect(fetchPendingMock).not.toHaveBeenCalled();

    newPost.resolve();
    await settle(() => p.postedSeq === 1 && !p.busy);
    expect(p.pending).toBeNull();
    expect(p.picked).toEqual([]);
    record.mockRestore();
  });

  it('two consecutive undos each walk back one of the player\'s own intents, and the machine posts nothing in between', async () => {
    const p = armedSeat();
    // Player's intent #1: the seq-1 cast, by hand. Machine's: the seq-2 pass.
    p.adoptView(live(1));
    p.click(0);
    await settle(() => p.postedSeq === 1);
    p.adoptView(quiet(2));
    p.considerAuto(view());
    await settle(() => p.postedSeq === 2);
    expect(seqs()).toEqual([1, 2]);

    // UNDO #1 rewinds to the seq-2 decision point; the machine stays silent;
    // the player answers by hand (intent #2).
    p.rewind();
    p.adoptView(quiet(2));
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 2);
    p.click(0);
    await settle(() => postIntentMock.mock.calls.length === 3);
    expect(seqs()).toEqual([1, 2, 2]);

    // UNDO #2 rewinds to the seq-1 decision point; again the machine stays
    // silent and the window waits for the player.
    p.rewind();
    p.adoptView(live(1));
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 3);
    expect(p.pending?.seq).toBe(1);
    expect(p.active?.seq).toBe(1);
    p.click(0);
    await settle(() => postIntentMock.mock.calls.length === 4);
    expect(seqs()).toEqual([1, 2, 2, 1]);
  });

  it('the pause-aware Auto switch resumes without touching the enabled preference (the real r2 break)', () => {
    // r2 finding: the paused note told the default auto-ON player to press
    // the Auto switch, but the switch's onclick was setAuto(!autoPass) —
    // with the persisted default that DISABLED auto, cleared the brake and
    // the empty-window floor instantly passed the restored window. The
    // pause-aware switch (pressAuto) must instead call setAuto(true): the
    // brake lifts and the enabled preference survives untouched.
    const p = armedSeat(); // autoPass true — the Casual default
    p.rewind();
    expect(p.machinePaused).toBe(true);
    p.pressAuto();
    expect(p.machinePaused).toBe(false);
    expect(p.settings.autoPass).toBe(true);
    expect(p.auto).toBe(true);
    expect(p.note.kind).toBe('armed');
  });

  it('the pause-aware Auto switch on a paused Manual seat turns auto on, and the machine runs again', async () => {
    const p = manualSeat();
    p.adoptView(quiet(9));
    p.rewind();
    p.adoptView(quiet(9));
    p.considerAuto(view());
    await settle(() => postIntentMock.mock.calls.length === 0); // paused: nothing posts
    p.pressAuto(); // the switch reads Paused; pressing it starts the machine
    expect(p.machinePaused).toBe(false);
    expect(p.settings.autoPass).toBe(true);
    p.considerAuto(view());
    await settle(() => p.postedSeq === 9);
    expect(seqs()).toEqual([9]);
  });

  it('unpaused, pressAuto is the ordinary toggle the switches always were', () => {
    const p = armedSeat();
    p.pressAuto();
    expect(p.settings.autoPass).toBe(false);
    p.pressAuto();
    expect(p.settings.autoPass).toBe(true);
  });

  it('both resume paths clear the pause: the Auto switch and applying a named preset', async () => {
    const viaSwitch = armedSeat();
    viaSwitch.rewind();
    expect(viaSwitch.machinePaused).toBe(true);
    viaSwitch.setAuto(true);
    expect(viaSwitch.machinePaused).toBe(false);
    expect(viaSwitch.note.kind).toBe('armed');

    const viaPreset = armedSeat();
    viaPreset.rewind();
    viaPreset.applyNamedPreset('casual');
    expect(viaPreset.machinePaused).toBe(false);
    expect(viaPreset.note.kind).toBe('armed');
  });

  it('while the pause holds a one-shot run cannot arm: the machine answers nothing on the restored window (r2 finding)', async () => {
    // r2 finding: startRun used to lift the pause — an unauthorized resume
    // path (End Turn / Hard Skip / Resolve All could re-enable machine
    // posting on the just-restored window). Now the arm is REFUSED while
    // the pause holds: the run never arms, the paused note stays on screen
    // (an armed run chip would overwrite it with a note claiming the
    // machine is passing), and nothing posts. After the player resumes,
    // the buttons arm as usual.
    const p = armedSeat();
    p.adoptView(quiet(3));
    p.rewind();
    p.adoptView(quiet(3));
    p.startEndTurn(view('main1', 0, 2));
    expect(p.oneShot).toBe('none');
    expect(p.machinePaused).toBe(true);
    expect(p.note.kind).toBe('paused');
    p.considerAuto(view('main1', 0, 2));
    await settle(() => postIntentMock.mock.calls.length === 0);
    expect(p.pending?.seq).toBe(3);

    // Resume, and the run arms and passes the restored window as before.
    p.pressAuto();
    p.startEndTurn(view('main1', 0, 2));
    expect(p.oneShot).toBe('end-turn');
    p.considerAuto(view('main1', 0, 2));
    await settle(() => p.postedSeq === 3);
    expect(p.endTurn).toBe(true);
  });

  it('a paced pass in flight when the rewind frame lands never fires into the new seq space', async () => {
    vi.useFakeTimers();
    try {
      const p = armedSeat();
      p.settings = { ...p.settings, pacing: { stepMs: 200, resolveMs: 400 } };
      p.adoptView(quiet(1));
      p.considerAuto(view());
      await vi.advanceTimersByTimeAsync(199); // the paced pass is mid-beat, not yet posted
      expect(postIntentMock).not.toHaveBeenCalled();
      p.rewind();
      p.adoptView(quiet(1)); // the restored window, same seq
      await vi.advanceTimersByTimeAsync(10_000);
      expect(postIntentMock).not.toHaveBeenCalled();
      expect(p.pending?.seq).toBe(1); // still the player's to answer
      expect(p.machinePaused).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  // The /pending half of the 2026-09-26 undo wedge (demo g3): a poll issued
  // in the pre-rewind seq space describes that space. Landing after rewind()
  // -- which opened a new space and dropped the seqHigh fence -- it
  // re-adopted the discarded ask (1526) over the restored one (1519), and the
  // fence then refused 1519 for good: every answer carried 1526 and the
  // server 409'd it. Either order of the stale read and the restored view
  // wedged.
  for (const lands of ['before', 'after'] as const) {
    it(`a /pending read in flight across the rewind cannot re-adopt the discarded ask (it lands ${lands} the restored view)`, async () => {
      const p = armedSeat();
      p.adoptView(choose(1526));
      let answer!: (d: Decision) => void;
      fetchPendingMock.mockReturnValueOnce(new Promise<Decision>((resolve) => { answer = resolve; }));
      const poll = p.refreshPending(); // read against the pre-undo server

      p.rewind();
      if (lands === 'after') p.adoptView(live(1519)); // the restored seat view
      answer(choose(1526));
      await poll;
      if (lands === 'before') p.adoptView(live(1519));

      expect(p.pending).toMatchObject({ seq: 1519, kind: 'priority' });
      p.click(1); // pass, by hand
      await settle(() => postIntentMock.mock.calls.length === 1);
      expect(seqs()).toEqual([1519]);
    });
  }

  it('a /pending 409 from the pre-rewind space does not clear the restored ask', async () => {
    const p = armedSeat();
    p.adoptView(choose(1526));
    let refuse!: (e: unknown) => void;
    fetchPendingMock.mockReturnValueOnce(new Promise<Decision>((_, reject) => { refuse = reject; }));
    const poll = p.refreshPending();

    p.rewind();
    p.adoptView(live(1519));
    refuse(new ApiError(409, 'conflict', 'host: no decision pending for seat 0'));
    await poll;

    expect(p.pending?.seq).toBe(1519);
    expect(p.active?.seq).toBe(1519);
    expect(p.error).toBeNull();
  });
});
