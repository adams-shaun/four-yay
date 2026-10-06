import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { CardView, Decision, Option, PlayerView, PotentialAction } from '../protocol';
import HandFan from '../components/HandFan.svelte';
import { laterByObj, optionsByObj, optionsByPlayer, type CardOptions } from './cardoptions';

// The report fb-20261006T070024Z-ff7470d7: seat 0 held Gurmag Angler ({6}{B},
// Delve) in hand with 6 cards in the graveyard and a black source untapped,
// but the priority window offered only mana taps. Delve is reachable (float B,
// then the live cast reappears with the delve prompt), but the hand card
// renderered NOTHING for the plan-less cast — the engine withholds a payment
// plan from every Convoke/Improvise/Delve "contribution" cast, so no payment
// action exists to put a CAST shortcut on the card, and the hand card's only
// other affordance gate was the LIVE option list, which was empty.
//
// This suite renders HandFan itself (not OptionPicker): the hand card is what
// the player looks at, and HandFan is the component the route actually mounts
// (Table.svelte -> HandFan). The prior round's test rendered OptionPicker and
// therefore passed while the hand card stayed bare.

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

const card = (id: number, name: string): CardView => ({
  id, name, types: 'Creature', mana_cost: '{6}{B}',
  printing: { name }, token: '', tapped: false, power: 5, toughness: 5,
  damage: 0, attacking: false, controller: 0, owner: 0, summon_sick: false,
});

const player = (hand: CardView[]): PlayerView => ({
  seat: 0, name: 'You', life: 40, lost: false, library_size: 60, hand_size: hand.length,
  graveyard_size: 6, hand, battlefield: [], graveyard: [], exile: [],
  completed_dungeons: 0,
  pool: {}, command: [], commanders: [], commander_casts: [],
});

const BOARD_W = 1180;
const HAND = player([card(CARD, 'Gurmag Angler')]);

function bundle(d: Decision, potential: PotentialAction[], handIds: number[]): CardOptions {
  return {
    byObj: optionsByObj(d),
    byPlayer: optionsByPlayer(d),
    picked: [],
    tone: 'offered',
    later: laterByObj(d, potential, handIds),
    post: vi.fn(),
  };
}

describe('plan-less hand cast later affordance', () => {
  it('shows a cast affordance on the hand card when no live option or payment action exists', () => {
    const d = decision();
    // Precondition: this exactly mirrors the report. The cast is in
    // potential_actions, the object is in hand, and neither the decision's
    // options nor its payment actions offer it — so without the later index
    // the hand card would have no affordance at all.
    expect(cast.kind).toBe('cast');
    expect([CARD]).toContain(cast.obj);
    expect(d.options.some((o) => o.kind === 'cast' && o.obj === CARD)).toBe(false);
    expect(d.payment_actions?.some((a) => a.cast.object === CARD)).not.toBe(true);

    const options = bundle(d, [cast], [CARD]);
    expect(options.later?.get(CARD)).toEqual([cast]);

    const { html } = render(HandFan, { props: { player: HAND, width: BOARD_W, options, open0: CARD } });
    expect(html).toContain('Cast Gurmag Angler (tap other mana first)');
    expect(html).toContain('aria-disabled="true"');
    expect(html).toContain('menu__item--later');
    // The count badge is the affordance that was missing entirely.
    expect(html).toContain('aria-haspopup="menu"');
  });

  it('does not index a cast for an object outside the viewer hand', () => {
    const d = decision();
    expect(d.options.some((o) => o.kind === 'cast' && o.obj === CARD)).toBe(false);
    expect(laterByObj(d, [cast], [CARD + 1])).toBeUndefined();
  });

  it('keeps a live cast offer on the existing live-option path', () => {
    const liveCast: Option = { index: 2, kind: 'cast', label: cast.label!, player: 0, obj: CARD };
    const d = decision([tap, pass, liveCast]);
    // Precondition: the live option really is present, so this is the
    // control case and the later index must stay out of the way.
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
    // And the hand card renders the CAST shortcut path, never a later row.
    const options = bundle(d, [cast], [CARD]);
    const payment = d.payment_actions![0];
    const { html } = render(HandFan, { props: { player: HAND, width: BOARD_W, options, paymentActions: [payment] } });
    expect(html).toContain('data-payment-card="plan-1"');
    expect(html).not.toContain('menu__item--later');
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
