import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { Decision, Option, PotentialAction } from '../protocol';
import OptionPicker from '../components/OptionPicker.svelte';
import { laterByObj, optionsByObj, tileOptions } from './cardoptions';

const CARD = 28;
const cast: PotentialAction = { kind: 'cast', obj: CARD, label: 'Cast Gurmag Angler' };
const tap: Option = { index: 0, kind: 'activate', label: 'Activate Underground Sea for mana', player: 0, obj: 4 };
const pass: Option = { index: 1, kind: 'pass', label: 'Pass priority', player: 0 };
const decision = (options: Option[] = [tap, pass]): Decision => ({
  seq: 953,
  player: 0,
  kind: 'priority',
  prompt: 'priority',
  min: 1,
  max: 1,
  options,
});

function tileFor(d: Decision, potential: PotentialAction[], handIds: number[]) {
  return tileOptions({
    byObj: optionsByObj(d),
    byPlayer: new Map(),
    picked: [],
    tone: 'offered',
    later: laterByObj(d, potential, handIds),
    post: vi.fn(),
  }, CARD);
}

describe('plan-less hand cast later affordance', () => {
  it('shows the hand cast as a disabled prerequisite row when no live option or payment action exists', () => {
    const d = decision();
    // Preconditions mirror the report: the cast is in potential_actions, the
    // object is in hand, and neither the options nor payment actions offer it.
    expect(cast.kind).toBe('cast');
    expect([CARD]).toContain(cast.obj);
    expect(d.options.some((o) => o.kind === 'cast' && o.obj === CARD)).toBe(false);
    expect(d.payment_actions?.some((a) => a.cast.object === CARD)).not.toBe(true);

    const later = laterByObj(d, [cast], [CARD]);
    expect(later?.get(CARD)).toEqual([cast]);
    const tile = tileFor(d, [cast], [CARD]);
    expect(tile?.later).toEqual([cast]);
    const html = render(OptionPicker, {
      props: { tileOptions: tile!, subject: 'for Gurmag Angler', open0: true, collapseTapActions: true },
    }).body;
    expect(html).toContain('Cast Gurmag Angler (tap other mana first)');
    expect(html).toContain('aria-disabled="true"');
  });

  it('does not index a cast for an object outside the viewer hand', () => {
    const d = decision();
    expect(d.options.some((o) => o.kind === 'cast' && o.obj === CARD)).toBe(false);
    expect(laterByObj(d, [cast], [CARD + 1])).toBeUndefined();
  });

  it('keeps a live cast offer on the existing live-option path', () => {
    const liveCast: Option = { index: 2, kind: 'cast', label: cast.label!, player: 0, obj: CARD };
    const d = decision([tap, pass, liveCast]);
    expect(d.options.some((o) => o.kind === 'cast' && o.obj === CARD)).toBe(true);
    expect(laterByObj(d, [cast], [CARD])).toBeUndefined();
  });

  it('does not duplicate a cast already represented by a payment action', () => {
    const d = decision();
    d.payment_actions = [{
      id: 'plan-1',
      cast: { object: CARD, face: 0, origin: 'hand' },
      label: 'Cast Gurmag Angler',
      plans: [],
    }];
    expect(d.options.some((o) => o.kind === 'cast' && o.obj === CARD)).toBe(false);
    expect(d.payment_actions.some((a) => a.cast.object === CARD)).toBe(true);
    expect(laterByObj(d, [cast], [CARD])).toBeUndefined();
  });

  it('keeps land drops and stations excluded even for hand cards', () => {
    const d = decision();
    const potentials: PotentialAction[] = [
      { kind: 'play_land', obj: CARD, label: 'Play Gurmag Angler' },
      { kind: 'station', obj: CARD, label: 'Station Gurmag Angler' },
    ];
    expect(laterByObj(d, potentials, [CARD])).toBeUndefined();
  });
});
