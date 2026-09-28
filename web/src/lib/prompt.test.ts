import { describe, expect, it } from 'vitest';
import type { CardView, Decision, PlayerView, StackView, View } from '../protocol';
import { promptContext, promptContextText, shapeOf, sourceCause, sourceNameOf } from './prompt';

const player = (hand: CardView[]): PlayerView => ({
  seat: 0, name: 'Ari', life: 20, lost: false, library_size: 40, hand_size: hand.length,
  graveyard_size: 0, hand, battlefield: [], graveyard: [], exile: [], pool: {},
  completed_dungeons: 0,
  command: [], commanders: [], commander_casts: [],
});
const card = (id: number, name: string): CardView => ({
  id, name, types: 'Instant', printing: { name }, token: `#${id}`, tapped: false,
  power: 0, toughness: 0, damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});
const view = (stack: (Pick<StackView, 'id' | 'name'> & Partial<StackView>)[] = [], hand: CardView[] = []): View => ({
  viewer: 0, visibility: 'seat', turn: 1, round: 1, step: 'main1', phase: 'main1',
  active: 0, priority: 0, over: false, draw: false, winner: null,
  players: [player(hand)], stack: stack.map((s) => ({ ...s, kind: s.kind ?? 'spell', text: '', controller: 0, targets: [], optional: false })), pending: [],
});
const decision = (over: Partial<Decision>): Decision => ({
  seq: 1, player: 0, kind: 'target', prompt: 'p', min: 1, max: 1, options: [], ...over,
});

describe('shapeOf — the response shape in the UI’s own words', () => {
  it('an ordering ask says Order N, including the scry/surveil Min-0 shape', () => {
    expect(shapeOf(decision({ kind: 'arrange', min: 5, max: 5 }))).toBe('Order 5');
    expect(shapeOf(decision({ kind: 'arrange', min: 0, max: 3 }))).toBe('Order up to 3');
    expect(shapeOf(decision({ kind: 'trigger_order', min: 2, max: 2 }))).toBe('Order 2');
  });

  it('count asks name the unit and the range', () => {
    expect(shapeOf(decision({ kind: 'target', min: 1, max: 1 }))).toBe('Pick 1 target');
    expect(shapeOf(decision({ kind: 'choose', min: 0, max: 2 }))).toBe('Pick up to 2 choices');
    expect(shapeOf(decision({ kind: 'modes', min: 1, max: 3 }))).toBe('Pick 1–3 modes');
    expect(shapeOf(decision({ kind: 'blockers', min: 0, max: 4 }))).toBe('Pick up to 4 blocks');
    expect(shapeOf(decision({ kind: 'discard', min: 2, max: 2 }))).toBe('Pick 2 cards');
    expect(shapeOf(decision({ kind: 'unknown-kind', min: 1, max: 1 }))).toBe('Pick 1 option');
  });

  it('priority and mulligan carry no shape line (their surfaces already state it)', () => {
    expect(shapeOf(decision({ kind: 'priority' }))).toBeNull();
    expect(shapeOf(decision({ kind: 'mulligan' }))).toBeNull();
  });

  // fb-20260914T062319Z-88b4069a part A: an optional trigger is a yes/no on an
  // OPTIONAL ability, and the generic "Pick 1 option" fallback read as a
  // mandatory selection. The shape line names both facts.
  it('an optional-trigger ask says the ability is optional — a yes/no, never "Pick 1 option"', () => {
    expect(shapeOf(decision({ kind: 'trigger_optional', min: 1, max: 1 }))).toBe('Optional ability — choose Yes or No');
  });
});

describe('sourceNameOf — the prompt’s source, resolved from the view', () => {
  it('prefers the stack (a resolving spell or ability carries its display name)', () => {
    expect(sourceNameOf(decision({ source: 9 }), view([{ id: 9, name: 'Lightning Bolt' }]))).toBe('Lightning Bolt');
  });

  it('falls back to any visible zone card', () => {
    expect(sourceNameOf(decision({ source: 42 }), view([], [card(42, 'Bear')]))).toBe('Bear');
  });

  it('the cause reads the stack kind: spell, trigger or ability; anything else is omitted', () => {
    const stack = (id: number, kind: string): View => ({
      ...view(),
      stack: [{ id, kind, name: 'Bolt', text: '', controller: 0, targets: [], optional: false }],
    });
    expect(sourceCause(decision({ source: 9 }), stack(9, 'spell'))).toBe('a resolving spell');
    expect(sourceCause(decision({ source: 9 }), stack(9, 'trigger'))).toBe('a triggered ability');
    expect(sourceCause(decision({ source: 9 }), stack(9, 'ability'))).toBe('an activated ability');
    // Ability objects have minted stack IDs: Decision.source and
    // StackView.source both identify the originating permanent.
    expect(sourceCause(decision({ source: 9 }), view([{ id: 99, source: 9, name: 'Bolt', kind: 'trigger' }]))).toBe('a triggered ability');
    expect(sourceNameOf(decision({ source: 9 }), view([{ id: 99, source: 9, name: 'Bolt', kind: 'trigger' }]))).toBe('Bolt');
    // not on the stack: no cause fact on the wire — omitted, never guessed
    expect(sourceCause(decision({ source: 42 }), view([], [card(42, 'Bear')]))).toBeNull();
    expect(sourceCause(decision({}), view())).toBeNull();
  });

  it('a source the view cannot see, and no source, resolve null (the line is omitted, never guessed)', () => {
    expect(sourceNameOf(decision({ source: 77 }), view([{ id: 9, name: 'Bolt' }]))).toBeNull();
    expect(sourceNameOf(decision({}), view([{ id: 9, name: 'Bolt' }]))).toBeNull();
    expect(sourceNameOf(decision({ source: 0 }), view([{ id: 9, name: 'Bolt' }]))).toBeNull();
  });
});

describe('promptContext — the context line', () => {
  it('renders every known fact, cause included', () => {
    const withStack = view([{ id: 9, name: 'Lightning Bolt', kind: 'spell', text: '', controller: 0, targets: [], optional: false }]);
    const ctx = promptContext(decision({ source: 9, min: 1, max: 1 }), withStack);
    expect(promptContextText(ctx)).toBe('From Lightning Bolt (a resolving spell) · Pick 1 target');
  });

  it('omits whichever fact is unknown, and the line itself when neither is', () => {
    expect(promptContextText(promptContext(decision({ kind: 'mulligan', source: 9 }), view([{ id: 9, name: 'Lightning Bolt' }])))).toBe('From Lightning Bolt (a resolving spell)');
    expect(promptContextText(promptContext(decision({ kind: 'priority' }), view()))).toBeNull();
  });
});

// fb-20260924T023233Z-ae628f55: the target is mandatory now, but the
// optional trigger's effect can still be declined at resolution.
const sageTarget = () => decision({
  seq: 1107, kind: 'target', source: 204, prompt: 'Choose a target for Reclamation Sage',
  min: 1, max: 1, options: [{ index: 0, kind: 'permanent', label: 'Sol Ring (You)', obj: 62, player: 0 }],
});
const sageView = (over: Partial<StackView> = {}): View => ({
  ...view(), stack: [{
    id: 204, kind: 'trigger', name: 'Reclamation Sage', text: '', controller: 0,
    targets: [], optional: true, decider: 0, ...over,
  }],
});

describe('promptContext — resolution-time opt-out', () => {
  const ordinaryLine = 'From Reclamation Sage (a triggered ability) · Pick 1 target';
  const hint = 'you choose whether the effect happens when it resolves';

  it('names the later choice for this seat on the captured target-ask shape', () => {
    const d = sageTarget();
    const v = sageView();
    expect(d).toMatchObject({ kind: 'target', source: 204, min: 1, max: 1 });
    expect(v.stack[0]).toMatchObject({ id: 204, kind: 'trigger', optional: true, decider: v.viewer });
    const ctx = promptContext(d, v);
    expect(ctx.declinable).toBe(hint);
    expect(promptContextText(ctx)).toBe(`${ordinaryLine} · ${hint}`);
  });

  it('does not promise a decline when a different seat decides', () => {
    const ctx = promptContext(sageTarget(), sageView({ decider: 1 }));
    expect(ctx.declinable).toBeNull();
    expect(promptContextText(ctx)).toBe(ordinaryLine);
  });

  it('does not promise a decline for a non-optional trigger or absent decider', () => {
    for (const stack of [sageView({ optional: false }), sageView({ decider: null }), sageView({ decider: undefined })]) {
      const ctx = promptContext(sageTarget(), stack);
      expect(ctx.declinable).toBeNull();
      expect(promptContextText(ctx)).toBe(ordinaryLine);
    }
  });

  it('does not guess when the source is not on the stack', () => {
    const ctx = promptContext(sageTarget(), sageView({ id: 205 }));
    expect(ctx.declinable).toBeNull();
    expect(promptContextText(ctx)).toBe('Pick 1 target');
  });

  it('does not repeat the optional-trigger shape line at the resolution ask', () => {
    const ctx = promptContext(decision({ ...sageTarget(), kind: 'trigger_optional' }), sageView());
    expect(ctx.declinable).toBeNull();
    expect(promptContextText(ctx)).toBe('From Reclamation Sage (a triggered ability) · Optional ability — choose Yes or No');
  });
});
