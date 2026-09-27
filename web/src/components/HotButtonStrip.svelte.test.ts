import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { Decision, PlayerView, SeatInfo, View } from '../protocol';
import { SeatPanelState } from '../lib/seatpanel.svelte';
import HotButtonStrip from './HotButtonStrip.svelte';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  postIntent: vi.fn(),
  fetchPending: vi.fn(),
}));
vi.mock('../lib/images', () => ({ images: { url: () => new Promise<string | null>(() => {}), offline: () => false } }));

const ctx = { seat: 0, token: 'tok' };
const seats: SeatInfo[] = [{ name: 'Ari', deck: 'deck', colour: '#e5484d', human: true }];
const player: PlayerView = {
  seat: 0, name: 'Ari', life: 20, lost: false, library_size: 53, hand_size: 0,
  graveyard_size: 0, hand: [], battlefield: [], graveyard: [], exile: [], pool: {},
  command: [], commanders: [], commander_casts: [],
};
const baseView: View = {
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null,
  players: [player], stack: [], pending: [],
};

function strip(decision: Decision): string {
  const state = new SeatPanelState('t1', 1, ctx, null);
  state.skipEmpty = false;
  state.adoptView(decision);
  return render(HotButtonStrip, {
    props: { view: { ...baseView, decision }, seats, state, ctx, table: 't1', match: 1 },
  }).html;
}

/** stripOnSeat renders with view.active set to `active` (default the base's own turn). */
function stripOnSeat(active: number, decision: Decision): string {
  const state = new SeatPanelState('t1', 1, ctx, null);
  state.skipEmpty = false;
  state.adoptView(decision);
  return render(HotButtonStrip, {
    props: { view: { ...baseView, active, decision }, seats, state, ctx, table: 't1', match: 1 },
  }).html;
}

/** stripState renders with a caller-prepared state, for cases strip() cannot express (no decision at all, or an answer already posted). */
function stripState(state: SeatPanelState, decision: Decision | null): string {
  return render(HotButtonStrip, {
    props: { view: { ...baseView, decision }, seats, state, ctx, table: 't1', match: 1 },
  }).html;
}

const option = (index: number, kind: string, label: string) => ({ index, kind, label, player: 0 });

describe('HotButtonStrip — server options regrouped into one instrument', () => {
  it('renders six fixed action slots including Resolve All and UNDO, with PASS enabled only by a pass option', () => {
    const priority: Decision = {
      seq: 1, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
      options: [option(7, 'cast', 'Cast spell'), option(42, 'pass', 'Pass priority')],
    };
    const html = strip(priority);
    expect([...html.matchAll(/data-hot-tab="/g)]).toHaveLength(6);
    expect(html).toMatch(/data-hot-tab="undo"[^>]*aria-disabled="false"/);
    expect(html).toContain('data-undo');
    expect(html).toMatch(/data-hot-tab="pass"[^>]*aria-disabled="false"/);
    expect(html).toContain('data-pass-action');

    const choose: Decision = {
      seq: 2, player: 0, kind: 'choose', prompt: 'Choose', min: 0, max: 2,
      options: [option(7, 'choose', 'First'), option(19, 'choose', 'Second')],
    };
    const unavailable = strip(choose);
    expect(unavailable).toMatch(/data-hot-tab="pass"[^>]*aria-disabled="true"/);
    // The button is always present now -- it is a transport control, not a
    // menu that appears only when it has something in it. Unavailable means
    // DISABLED, which is what keeps R-E4-2 true: the strip never offers an
    // action the wire did not send.
    expect(unavailable).toMatch(/data-pass-action[^>]*disabled/);

    // SeatPanel owns polling and Skip Empty. It must remain mounted when the
    // wire offers only pass, even though ACTIONS itself is unavailable.
    const emptyPriority: Decision = {
      ...priority, seq: 3, options: [option(42, 'pass', 'Pass priority')],
    };
    const empty = strip(emptyPriority);
    expect(empty).toMatch(/data-hot-tab="actions"[^>]*aria-disabled="true"/);
    expect(empty).toContain('data-seat-panel');
  });

  it('disables UNDO when another human seat would need to consent', () => {
    const priority: Decision = {
      seq: 1, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
      options: [option(42, 'pass', 'Pass priority')],
    };
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.adoptView(priority);
    const html = render(HotButtonStrip, {
      props: {
        view: { ...baseView, decision: priority },
        seats: [...seats, { name: 'Bo', deck: 'deck2', colour: '#22c55e', human: true }],
        state, ctx, table: 't1', match: 1,
      },
    }).html;
    expect(html).toMatch(/data-hot-tab="undo"[^>]*aria-disabled="true"[^>]*disabled/);
  });

  it('uses one contextual slot with a stable DONE label', () => {
    const choose: Decision = {
      seq: 2, player: 0, kind: 'choose', prompt: 'Choose', min: 0, max: 2,
      options: [option(7, 'choose', 'First'), option(19, 'choose', 'Second')],
    };
    const picking = strip(choose);
    expect([...picking.matchAll(/data-hot-tab="done"/g)]).toHaveLength(1);
    expect(picking).toContain('aria-label="Done picking"');
    expect(picking).toContain('>DONE</span>');

    const attackers: Decision = {
      ...choose, seq: 3, kind: 'attackers', prompt: 'Declare attackers',
      options: [option(7, 'attacker', 'Attack Ari')],
    };
    const attacking = strip(attackers);
    expect([...attacking.matchAll(/data-hot-tab="done"/g)]).toHaveLength(1);
    expect(attacking).toContain('aria-label="Done selecting attackers"');
    expect(attacking).toContain('>DONE</span>');
  });

  it('does not expose a separate Done submit for a one-pick decision', () => {
    const target: Decision = {
      seq: 4, player: 0, kind: 'target', prompt: 'Choose a target', min: 1, max: 1,
      options: [option(17, 'target', 'Target Ari')],
    };
    const html = strip(target);
    expect(html).toMatch(/data-hot-tab="done"[^>]*aria-disabled="true"/);
    expect(html).toMatch(/data-done-action[^>]*disabled/);
  });

  it('keeps AUTO in a fixed status slot and leaves settings out of the action strip', () => {
    const priority: Decision = {
      seq: 1, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
      options: [option(7, 'cast', 'Cast spell'), option(42, 'pass', 'Pass priority')],
    };
    const html = strip(priority);
    expect(html).toContain('data-play-mode="casual"');
    expect(html).toContain('title="Auto: Casual"');
    expect(html).toContain('data-auto-status');
    expect(html).not.toContain('data-hot-tab="options"');
    expect(html).not.toContain('data-settings-panel');
  });
});

describe('HotButtonStrip — the ACTIONS tab projects the seat tone', () => {
  // toneOf resolves from option KINDS only (R-E4-1): initiative is a decision
  // with no pass option — the game is blocked on this seat; offered is a
  // window this seat may decline. The tab must carry that state machine-
  // readably in data-awaiting, with the dropdown closed.
  const target: Decision = {
    seq: 5, player: 0, kind: 'target', prompt: 'Choose a target', min: 1, max: 1,
    options: [option(17, 'target', 'Target Ari')],
  };
  const priority: Decision = {
    seq: 6, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
    options: [option(7, 'cast', 'Cast spell'), option(42, 'pass', 'Pass priority')],
  };

  it('awaits initiative when the game is blocked on this seat (no pass option)', () => {
    expect(strip(target)).toMatch(/data-awaiting="initiative"/);
  });

  it('awaits offered on a window this seat may act in or pass', () => {
    expect(strip(priority)).toMatch(/data-awaiting="offered"/);
  });

  it('is idle with no pending decision', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.skipEmpty = false;
    const html = stripState(state, null);
    expect(html).toMatch(/data-awaiting="idle"/);
    expect(html).not.toMatch(/data-awaiting="initiative"/);
    expect(html).not.toMatch(/data-awaiting="offered"/);
  });

  it('drops the affordance once the answer is posted (answered or in flight)', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.skipEmpty = false;
    state.adoptView(target);
    state.postedSeq = target.seq; // logic.active is null while the posted seq matches
    const html = stripState(state, target);
    expect(html).toMatch(/data-awaiting="idle"/);
    expect(html).not.toMatch(/data-awaiting="initiative"/);
  });
});

describe('HotButtonStrip — the status chip', () => {
  const priority: Decision = {
    seq: 1, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
    options: [option(7, 'cast', 'Cast spell'), option(42, 'pass', 'Pass priority')],
  };

  it('shows the settings preset by default, machine-readably in data-play-mode', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.setAutoManaAvailable(true);
    const html = stripState(state, priority);
    expect(html).toMatch(/data-play-mode="casual"/);
    expect(html).toContain('title="Auto: Casual"');
    expect(html).toContain('>AUTO</span>');
    expect(html).toContain('data-auto-pay-toggle');
    expect(html).toContain('AUTO MANA');
  });

  it('shows the undo pause and its resume control in the always-visible live-strip chip', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    // Table handles a rewind frame by calling this before adopting the
    // restored view. The nested strip SeatPanel hides its autobar, so the
    // status chip itself must expose the pause; otherwise the live game still
    // says Casual while its machine silently refuses to run.
    state.rewind();
    state.adoptView(priority);
    const html = stripState(state, priority);
    expect(html).toMatch(/data-play-mode="paused"/);
    expect(html).toContain('data-auto-note');
    expect(html).toContain('>AUTO</span>');
    expect(html).toContain('Press the Auto switch (or apply a preset)');
    expect(html).not.toMatch(/data-play-mode="casual"/);
  });

  it('END TURN names the live run, and the hard skip names its warning', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.setActPass(false);
    state.adoptView(priority);
    state.startEndTurn({ ...baseView, decision: priority } as unknown as View);
    const run = stripState(state, priority);
    expect(run).toMatch(/data-play-mode="end-turn"/);
    expect(run).toContain('title="Auto: END TURN"');

    state.startHardSkip({ ...baseView, decision: priority } as unknown as View);
    const skip = stripState(state, priority);
    expect(skip).toMatch(/data-play-mode="skip-turn"/);
    expect(skip).toContain('title="Auto: Skipping turn — Esc to stop"');
  });

  it('shows Custom once the settings no longer match a named preset', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.setActPass(false);
    state.setAuto(false); // casual with autoPass off is no preset
    const html = stripState(state, priority);
    expect(html).toMatch(/data-play-mode="custom"/);
    expect(html).toContain('title="Auto: Custom"');
  });
});

// END TURN is a one-shot to the end of the CURRENT turn, so it only makes
// sense on a turn the seat owns: on the opponent's turn the button must be
// disabled with a truthful title, while PASS (and the keyboard hard-skip)
// keep working — passing priority on the opponent's turn is real Magic.
describe('HotButtonStrip — END TURN is gated to the seat\'s own turn', () => {
  const priority: Decision = {
    seq: 1, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
    options: [option(7, 'cast', 'Cast spell'), option(42, 'pass', 'Pass priority')],
  };

  it('disables END TURN on the opponent\'s turn with a title that names the gate, not a missing pass option', () => {
    const html = stripOnSeat(1, priority);
    expect(html).toMatch(/data-end-turn[^>]*aria-disabled="true"/);
    expect(html).toMatch(/data-end-turn[^>]*disabled/);
    expect(html).toContain('End Turn is for your own turn');
    expect(html).not.toContain('End Turn needs a pass option');
  });

  it('arms END TURN exactly as before on the seat\'s own turn', () => {
    const html = stripOnSeat(0, priority);
    expect(html).toMatch(/data-end-turn[^>]*aria-disabled="false"/);
  });

  it('keeps PASS enabled on the opponent turn (passing priority there is real Magic)', () => {
    const html = stripOnSeat(1, priority);
    expect(html).toMatch(/data-pass-action[^>]*aria-disabled="false"/);
  });

  it('the hard-skip machinery still arms on an opponent turn (Shift+Enter path is not turn-gated)', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.skipEmpty = false;
    state.adoptView(priority);
    state.startHardSkip({ ...baseView, active: 1 } as unknown as View);
    expect(state.oneShot).toBe('hard-skip');
    expect(state.playMode).toBe('skip-turn');
  });
});

// Prio6: Resolve All owns a fixed slot so stack changes cannot shift the
// action strip. It is enabled only in a pending stack priority window.
describe('HotButtonStrip — Resolve All (prio6)', () => {
  const option = (index: number, kind: string, label: string) => ({ index, kind, label, player: 0 });
  const priorityOnStack: Decision = {
    seq: 1, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
    options: [option(42, 'pass', 'Pass priority'), option(43, 'concede', 'Concede')],
  };

  it('is visible with a non-empty stack and a pending priority decision, enabled by a pass option', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.skipEmpty = false;
    state.adoptView(priorityOnStack);
    const html = render(HotButtonStrip, {
      props: {
        view: { ...baseView, decision: priorityOnStack, stack: [{ id: 9, controller: 1, kind: 'spell', name: 'Bolt', text: '', targets: [], optional: false }] },
        seats, state, ctx, table: 't1', match: 1,
      },
    }).html;
    expect(html).toContain('data-resolve-all');
    expect(html).toMatch(/data-resolve-all[^>]*aria-disabled="false"/);
  });

  it('keeps its fixed slot disabled on an empty stack or non-priority decision', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.skipEmpty = false;
    state.adoptView(priorityOnStack);
    const emptyStack = render(HotButtonStrip, {
      props: { view: { ...baseView, decision: priorityOnStack, stack: [] }, seats, state, ctx, table: 't1', match: 1 },
    }).html;
    expect(emptyStack).toMatch(/data-resolve-all[^>]*aria-disabled="true"/);

    const target: Decision = { seq: 2, player: 0, kind: 'target', prompt: 'T', min: 1, max: 1, options: [option(7, 'target', 'T Ari')] };
    state.adoptView(target);
    const nonPriority = render(HotButtonStrip, {
      props: {
        view: { ...baseView, decision: target, stack: [{ id: 9, controller: 1, kind: 'spell', name: 'Bolt', text: '', targets: [], optional: false }] },
        seats, state, ctx, table: 't1', match: 1,
      },
    }).html;
    expect(nonPriority).toMatch(/data-resolve-all[^>]*aria-disabled="true"/);
  });
});

describe('HotButtonStrip — Auto Mana action availability', () => {
  it('keeps ACTIONS available when a payment plan is the only visible action', () => {
    const state = new SeatPanelState('t1', 1, ctx, null);
    state.setAutoManaAvailable(true);
    state.setAutoPayMana(true);
    const priority: Decision = {
      seq: 91, player: 0, kind: 'priority', prompt: 'Priority', min: 1, max: 1,
      options: [option(7, 'cast', 'Cast Test Spell'), option(42, 'pass', 'Pass priority')],
      payment_actions: [{
        id: 'pay-test', cast: { object: 8, face: 0, origin: 'hand' }, base_option_index: 7,
        label: 'Cast Test Spell', plans: [{ id: 'plan', version: 1, cost: { generic: 0, mana: [0, 1, 0, 0, 0, 0] }, activations: [], pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0] }],
      }],
    };
    state.adoptView(priority);
    const html = stripState(state, priority);
    expect(html).toMatch(/data-hot-tab="actions"[^>]*aria-disabled="false"/);
    expect(html).not.toContain('No action is offered by this decision.');
  });
});
