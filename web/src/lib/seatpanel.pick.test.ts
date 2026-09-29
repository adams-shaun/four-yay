import { describe, expect, it, vi } from 'vitest';
import type { Decision, Option } from '../protocol';
import { SeatPanelState, pickOption, unpickOption } from './seatpanel.svelte';

// Hermetic post stub: the pick-N tests below assert what is (not) posted.
const { postIntentMock } = vi.hoisted(() => ({ postIntentMock: vi.fn() }));
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  postIntent: postIntentMock,
}));

const ctx = { seat: 0, token: 'tok' };

// pickOption is the seat's pure selection logic, expressed entirely in terms
// of the decision's own Option.Group field (R-E4-2): the panel never finds
// out what a Group's members are, only that a non-empty Group is an
// exclusivity marker. These tests therefore build the decision by hand and
// assert nothing about blockers, creatures or combat.

const block = (index: number, obj: number, group?: string): Option => ({
  index,
  kind: 'block',
  label: `option ${index}`,
  obj,
  player: 0,
  ...(group ? { group } : {}),
});

// A block-style decision: one blocker (obj 5) is offered against two
// attackers (groups share "blocker:5"), a second blocker (obj 6) against one.
const d: Decision = {
  seq: 3,
  player: 0,
  kind: 'blockers',
  prompt: 'declare blockers',
  min: 0,
  max: 4,
  options: [
    block(0, 5, 'blocker:5'), // Bear blocks Alpha
    block(1, 5, 'blocker:5'), // Bear blocks Beta
    block(2, 6, 'blocker:6'), // Wolf blocks Alpha
  ],
};

describe('pickOption — exclusivity by Group', () => {
  it('picking two options of one Group never holds both; the second REPLACES the first', () => {
    // pick Bear blocks Alpha
    expect(pickOption(d, 0, [])).toEqual([0]);
    // move Bear to Beta: Alpha (index 0) is dropped, Beta takes its place
    expect(pickOption(d, 1, [0])).toEqual([1]);
  });

  it('a replacement preserves the click order of the other picks and appends the new one', () => {
    // pick Wolf (2), then Bear against Alpha (0)
    expect(pickOption(d, 2, [])).toEqual([2]);
    expect(pickOption(d, 0, [2])).toEqual([2, 0]);
    // now move Bear to Beta: index 0 is replaced by index 1, Wolf (2) stays put
    expect(pickOption(d, 1, [2, 0])).toEqual([2, 1]);
  });

  it('re-picking an already-picked option toggles it off, even inside a group', () => {
    expect(pickOption(d, 0, [0])).toEqual([]);
    expect(pickOption(d, 1, [1, 2])).toEqual([2]);
  });

  it('different Groups coexist; at most one option per Group is ever held', () => {
    // one option from each group
    expect(pickOption(d, 0, [])).toEqual([0]);
    expect(pickOption(d, 2, [0])).toEqual([0, 2]);
    // adding another member of a represented group replaces that member only
    expect(pickOption(d, 1, [0, 2])).toEqual([2, 1]);
  });

  it('an out-of-range index is a no-op (never panics, never mutates)', () => {
    expect(pickOption(d, 99, [0])).toEqual([0]);
  });

  it('options with no Group are never exclusive (R-E4-2: only the Group contract applies)', () => {
    const ungrouped: Decision = {
      seq: 4,
      player: 0,
      kind: 'choose',
      prompt: 'bottom 2',
      min: 0,
      max: 2,
      options: [
        { index: 0, kind: 'bottom', label: 'A', obj: undefined, player: 0 },
        { index: 1, kind: 'bottom', label: 'B', obj: undefined, player: 0 },
      ],
    };
    expect(pickOption(ungrouped, 0, [])).toEqual([0]);
    expect(pickOption(ungrouped, 1, [0])).toEqual([0, 1]);
  });
});

// A repeatable modal ask (a CanRepeatModes$ Charm, CR 601.2b): the answer is
// an ORDERED MULTISET over the distinct options, so the ask's Min may exceed
// the option count — CharmNum$ 3 over 2 legal modes. The review-r2 wedge:
// toggle-off on a re-click made len(picked) == min unreachable, so a human
// seat could never enable the submit button on the engine's own shape.
const repeatableModes = (seq: number, min = 3, max = 3, repeatable = true): Decision => ({
  seq,
  player: 0,
  kind: 'modes',
  prompt: 'Choose three. You may choose the same mode more than once.',
  min,
  max,
  repeatable,
  options: [
    { index: 0, kind: 'mode', label: 'deals 1 damage to each creature', obj: undefined, player: 0 },
    { index: 1, kind: 'mode', label: 'deals 2 damage to each opponent', obj: undefined, player: 0 },
  ],
});

describe('pickOption — repeatable decisions', () => {
  it('re-picking an already-picked option APPENDS another instance, never toggles off', () => {
    const d = repeatableModes(9);
    expect(pickOption(d, 1, [])).toEqual([1]);
    expect(pickOption(d, 1, [1])).toEqual([1, 1]);
    expect(pickOption(d, 0, [1, 1])).toEqual([1, 1, 0]);
  });

  it('the max caps the multiset; a full pick is a no-op', () => {
    const d = repeatableModes(9);
    expect(pickOption(d, 1, [1, 1, 1])).toEqual([1, 1, 1]);
    expect(pickOption(d, 0, [0, 1, 1])).toEqual([0, 1, 1]);
  });

  it('a min lower than the option count still fills to min by repeating', () => {
    const d = repeatableModes(9, 2, 5);
    expect(pickOption(d, 0, [])).toEqual([0]);
    expect(pickOption(d, 0, [0])).toEqual([0, 0]);
  });
});

describe('unpickOption — removing one instance of a repeatable pick', () => {
  it('removes the LAST occurrence and keeps the other instances in click order', () => {
    expect(unpickOption(1, [1, 0, 1])).toEqual([1, 0]);
    expect(unpickOption(0, [1, 0, 1])).toEqual([1, 1]);
  });

  it('an unpicked index is a no-op (never panics, never mutates)', () => {
    expect(unpickOption(2, [1, 0, 1])).toEqual([1, 0, 1]);
    expect(unpickOption(1, [])).toEqual([]);
  });
});

// The seat panel's own state on the engine's real shape: the submit gate must
// be reachable on a repeatable ask whose Min exceeds the option count.
describe('SeatPanelState — a repeatable modal ask is answerable', () => {
  it('three clicks of one mode reach canSubmit on Min 3 over 2 options; a chip click removes one', () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView(repeatableModes(9));
    p.click(1);
    p.click(1);
    expect(p.canSubmit).toBe(false);
    expect(p.picked).toEqual([1, 1]);
    p.click(1);
    expect(p.picked).toEqual([1, 1, 1]);
    expect(p.canSubmit).toBe(true);
    // The picked-so-far chips' remove affordance: one instance goes, and the
    // gate re-locks until the slot is refilled.
    p.unpick(1);
    expect(p.picked).toEqual([1, 1]);
    expect(p.canSubmit).toBe(false);
    p.click(0);
    expect(p.picked).toEqual([1, 1, 0]);
    expect(p.canSubmit).toBe(true);
  });

  it('unpick is inert on a non-repeatable decision (toggle already owns removal there)', () => {
    const p = new SeatPanelState('t1', 1, ctx, null);
    p.adoptView(repeatableModes(9, 0, 2, false));
    p.click(1);
    p.unpick(1);
    expect(p.picked).toEqual([1]);
  });
});

describe('SeatPanelState — the pick-N hotkeys', () => {
  it('pickHotkey answers option n of a non-priority decision and ignores priority windows', () => {
    const p = new SeatPanelState('t', 1, ctx, null);
    p.adoptView(d); // the blockers decision above: min 0, max 4, so a pick toggles, never posts
    expect(p.pickHotkey(1)).toBe(true);
    expect(p.picked).toEqual([0]);
    expect(p.pickHotkey(9)).toBe(false); // no ninth option
    const prio: Decision = { seq: 4, player: 0, kind: 'priority', prompt: 'p', min: 1, max: 1, options: [{ index: 0, kind: 'pass', label: 'Pass', player: 0 }] };
    p.adoptView(prio);
    expect(p.pickHotkey(1)).toBe(false);
  });
});

describe('pickHotkey — digit N is the Nth rendered row', () => {
  const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };

  it('numbers a non-priority list without its separately drawn pass/resolve button', async () => {
    postIntentMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
    const dec: Decision = { seq: 20, player: 0, kind: 'choose', prompt: 'Pay {1}?', min: 1, max: 1,
      options: [{ index: 0, kind: 'pass', label: 'Decline', player: 0 }, { index: 1, kind: 'choose', label: 'Pay', player: 0 }] };
    const p = new SeatPanelState('t', 1, ctx, null);
    p.adoptView(dec);
    // The only row is "Pay" (Decline is the footer button): 2 names nothing.
    expect(p.pickHotkey(2)).toBe(false);
    expect(p.pickHotkey(1)).toBe(true);
    await settle();
    expect(postIntentMock).toHaveBeenCalledWith('t', 1, { seq: 20, player: 0, choices: [1] }, ctx);
  });

  it('numbers the announced mana-payment window as its panel draws it', async () => {
    postIntentMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
    const dec: Decision = { seq: 21, player: 0, kind: 'choose', prompt: 'Pay for Bolt', min: 1, max: 1,
      mana_payment: { card: 9, cost: { generic: 0, mana: [0, 0, 0, 1, 0, 0] }, owed: { generic: 0, mana: [0, 0, 0, 1, 0, 0] }, pool: [0, 0, 0, 0, 0, 0] },
      options: [
        { index: 0, kind: 'cancel_cast', label: 'Cancel', player: 0 },
        { index: 1, kind: 'mana', label: 'Add R', obj: 3, player: 0 },
      ] };
    const p = new SeatPanelState('t', 1, ctx, null);
    p.adoptView(dec);
    // Row 1 is the Mountain's R; Cancel cast is the last button (row 2).
    expect(p.pickHotkey(1)).toBe(true);
    await settle();
    expect(postIntentMock).toHaveBeenCalledWith('t', 1, { seq: 21, player: 0, choices: [1] }, ctx);
  });

  it('still picks on a generic target list that lists every option', async () => {
    postIntentMock.mockReset();
    postIntentMock.mockResolvedValue(undefined);
    const dec: Decision = { seq: 22, player: 0, kind: 'target', prompt: 'Choose a target', min: 1, max: 1,
      options: [{ index: 0, kind: 'player', label: 'Seat 1', player: 0 }, { index: 1, kind: 'obj', label: 'Grizzly Bears', obj: 7, player: 0 }] };
    const p = new SeatPanelState('t', 1, ctx, null);
    p.adoptView(dec);
    expect(p.pickHotkey(2)).toBe(true);
    await settle();
    expect(postIntentMock).toHaveBeenCalledWith('t', 1, { seq: 22, player: 0, choices: [1] }, ctx);
  });
});
