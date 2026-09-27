import { describe, expect, it, vi } from 'vitest';
import { render } from 'svelte/server';
import type { Decision, Option, PotentialAction } from '../protocol';
import {
  laterByObj,
  laterLabel,
  optionsByObj,
  postSingleAction,
  singleTapOptionOf,
  tileOptions,
  tileOptionsMany,
  wheelFace,
  type CardOptions,
} from './cardoptions';
import OptionPicker from '../components/OptionPicker.svelte';

// fb-20260923T033148Z-877b8f8f: "mount doom, phyrexian tower -- can only play
// tap for mana, not the other abilities". The engine offers a mana-costed
// ability only once its mana floats, so an untapped Mount Doom's live option
// list was its one mana activation; a one-option card acts directly, so the
// click that looked for "{1}{B}{R}, {T}: 1 damage to each opponent" tapped
// Mount Doom for mana and spent the {T} that ability needs. The seat's own
// potential_actions already carried the ability (rules
// TestPotentialActionsMountDoomDamageAbility); the tile now reads it.

const DOOM = 19;

const opt = (over: Partial<Option>): Option => ({ index: 0, kind: 'pass', label: 'Pass priority', player: 0, ...over });

const priority = (options: Option[], kind = 'priority'): Decision => ({
  seq: 2501, player: 0, kind, prompt: 'priority', min: 1, max: 1, options,
});

const doomMana = opt({ index: 7, kind: 'activate', label: 'Activate Mount Doom for mana', obj: DOOM, cost: 'PayLife<1> T' } as Partial<Option>);
const doomDamage: PotentialAction = { kind: 'ability', obj: DOOM, ability: 1, label: 'Mount Doom: Mount Doom deals 1 damage to each opponent.' };
const handCast: PotentialAction = { kind: 'cast', obj: 5, label: 'Cast Lightning Bolt' };
const otherAbility: PotentialAction = { kind: 'ability', obj: 30, ability: 0, label: 'Grim Monolith: Untap this artifact.' };

function bundle(d: Decision, potential: PotentialAction[], post = vi.fn()): CardOptions {
  return {
    byObj: optionsByObj(d),
    byPlayer: new Map(),
    picked: [],
    tone: 'offered',
    later: laterByObj(d, potential),
    post,
  };
}

describe('laterByObj', () => {
  it('puts a potential ability on the tile whose only live option is its mana tap', () => {
    const d = priority([doomMana, opt({ index: 8, kind: 'pass' })]);
    const m = laterByObj(d, [doomDamage, handCast, otherAbility]);
    expect(m?.get(DOOM)).toEqual([doomDamage]);
    // A hand cast and an ability on a card with no live option stay where
    // they were (the auto-pass stop note): no new badge appears anywhere.
    expect(m?.has(5)).toBe(false);
    expect(m?.has(30)).toBe(false);
  });

  it('drops an ability the decision already offers live (mana floated)', () => {
    const live = opt({ index: 3, kind: 'ability', obj: DOOM, ability: 1, label: doomDamage.label! });
    expect(laterByObj(priority([doomMana, live]), [doomDamage])).toBeUndefined();
  });

  it('is empty outside a priority window', () => {
    expect(laterByObj(priority([doomMana], 'choose'), [doomDamage])).toBeUndefined();
    expect(laterByObj(null, [doomDamage])).toBeUndefined();
    expect(laterByObj(priority([doomMana]), undefined)).toBeUndefined();
  });
});

describe('a tile with later abilities never acts directly', () => {
  it('Mount Doom: the lone mana option is not a direct action', () => {
    const post = vi.fn();
    const b = bundle(priority([doomMana]), [doomDamage], post);
    const tile = tileOptions(b, DOOM)!;
    expect(tile.list).toEqual([doomMana]);
    expect(tile.later).toEqual([doomDamage]);
    expect(singleTapOptionOf(tile)).toBeNull();
    postSingleAction(tile, true);
    expect(post).not.toHaveBeenCalled();
  });

  it('a card without later abilities keeps its direct action', () => {
    const post = vi.fn();
    const b = bundle(priority([doomMana]), [], post);
    const tile = tileOptions(b, DOOM)!;
    expect(tile.later).toBeUndefined();
    postSingleAction(tile, true);
    expect(post).toHaveBeenCalledWith(7, true, false);
  });

  it('a collapsed stack shows each later row once', () => {
    const a = opt({ index: 1, kind: 'activate', obj: 40, label: 'Activate Mishra\'s Factory for mana' });
    const c = opt({ index: 2, kind: 'activate', obj: 41, label: 'Activate Mishra\'s Factory for mana' });
    const anim = (obj: number): PotentialAction => ({ kind: 'ability', obj, ability: 1, label: 'Mishra\'s Factory: becomes a creature.' });
    const b = bundle(priority([a, c]), [anim(40), anim(41)]);
    const tile = tileOptionsMany(b, [40, 41])!;
    expect(tile.later).toHaveLength(1);
    expect(singleTapOptionOf(tile)).toBeNull();
  });
});

describe('OptionPicker with later abilities', () => {
  it('renders a count badge, not the direct tap icon', () => {
    const tile = tileOptions(bundle(priority([doomMana]), [doomDamage]), DOOM)!;
    const { body } = render(OptionPicker, { props: { tileOptions: tile, subject: 'for Mount Doom', collapseTapActions: true } });
    expect(body).not.toContain('data-single-action');
    expect(body).toContain('aria-label="2 actions for Mount Doom"');
  });

  it('lists the later ability as a disabled row that says why', () => {
    const tile = tileOptions(bundle(priority([doomMana]), [doomDamage]), DOOM)!;
    const { body } = render(OptionPicker, { props: { tileOptions: tile, subject: 'for Mount Doom', open0: true, collapseTapActions: true } });
    expect(body).toContain('data-wire-index="7"');
    expect(body).toContain('data-later-ability="1"');
    expect(body).toContain('aria-disabled="true"');
    expect(body).toContain(laterLabel(doomDamage));
    expect(laterLabel(doomDamage)).toBe('Mount Doom: Mount Doom deals 1 damage to each opponent. (tap other mana first)');
  });
});

// aph-web-manual-only-plays: rules.PotentialActions now also projects the
// float-gated special actions and max-speed granted abilities. Avishkar
// Raceway is the Mount Doom shape again: its live option is the mana tap, and
// its max-speed "{3}, {T}, Discard a card: Draw a card." (kind "granted") is
// offered only once {3} floats -- a direct tap would spend the {T} it needs.
describe('laterByObj with the widened projection kinds', () => {
  const RACEWAY = 61;
  const racewayMana = opt({ index: 4, kind: 'activate', label: 'Activate Avishkar Raceway for mana', obj: RACEWAY });
  const racewayDraw: PotentialAction = { kind: 'granted', obj: RACEWAY, label: 'Avishkar Raceway: Draw a card.' };

  it('a float-gated granted ability is a later row on the tile whose live option is its tap', () => {
    const b = bundle(priority([racewayMana]), [racewayDraw]);
    const tile = tileOptions(b, RACEWAY)!;
    expect(tile.later).toEqual([racewayDraw]);
    expect(singleTapOptionOf(tile)).toBeNull();
  });

  it('a granted ability the decision already offers live is not repeated', () => {
    const live = opt({ index: 5, kind: 'granted', label: racewayDraw.label!, obj: RACEWAY, svar: 'ABDraw' } as Partial<Option>);
    expect(laterByObj(priority([racewayMana, live]), [racewayDraw])).toBeUndefined();
  });

  it('unlock, turn face up and specialize are later rows too, but only on a tile the decision already offers something', () => {
    const others: PotentialAction[] = [
      { kind: 'unlock', obj: RACEWAY, label: 'Unlock Prop Room' },
      { kind: 'turn_face_up', obj: RACEWAY, label: 'Turn face up ({G})' },
      { kind: 'specialize', obj: RACEWAY, mode: '1', label: 'Specialize as White Form ({1})' },
    ];
    expect(laterByObj(priority([racewayMana]), others)?.get(RACEWAY)).toEqual(others);
    // On an object with no live option (a face-down morph, a locked Room)
    // nothing is indexed: no new badge appears on a tile that had none.
    const elsewhere = others.map((a) => ({ ...a, obj: 62 }));
    expect(laterByObj(priority([racewayMana]), elsewhere)).toBeUndefined();
  });

  it('a cast, a land drop and a station never become later rows', () => {
    const m = laterByObj(priority([racewayMana]), [
      { kind: 'cast', obj: RACEWAY, label: 'Cast Raceway' },
      { kind: 'play_land', obj: RACEWAY, label: 'Play Raceway' },
      { kind: 'station', obj: RACEWAY, label: 'Station Raceway' },
    ]);
    expect(m).toBeUndefined();
  });

  it('OptionPicker renders the granted later row with its reason, and the tile is a count badge', () => {
    const tile = tileOptions(bundle(priority([racewayMana]), [racewayDraw]), RACEWAY)!;
    const closed = render(OptionPicker, { props: { tileOptions: tile, subject: 'for Avishkar Raceway', collapseTapActions: true } }).body;
    expect(closed).not.toContain('data-single-action');
    expect(closed).toContain('aria-label="2 actions for Avishkar Raceway"');
    const open = render(OptionPicker, { props: { tileOptions: tile, subject: 'for Avishkar Raceway', open0: true, collapseTapActions: true } }).body;
    expect(open).toContain('aria-disabled="true"');
    expect(open).toContain('Avishkar Raceway: Draw a card. (tap other mana first)');
    expect(open).not.toContain('undefined');
  });
});

// fb-20260924T180813Z-bbe4fd8f: Phyrexian Tower's stage-1 wheel is now
// labelled "Add C" / "Sacrifice 1 creature: Add BB" by the engine; the
// generic first-word face read "Add" on both buttons.
describe('wheelFace for mana options', () => {
  const mana = (label: string) => wheelFace({ kind: 'mana', label });
  it('tells Phyrexian Tower\'s two abilities apart', () => {
    expect(mana('Add C')).toBe('C');
    expect(mana('Sacrifice 1 creature: Add BB')).toBe('Sac BB');
  });
  it('renders the other production shapes', () => {
    expect(mana('Add B or R')).toBe('B/R');
    expect(mana('Add U, B or R')).toBe('U/B/R');
    expect(mana('Pay 1 life: Add G')).toBe('Pay G');
    expect(mana('Add any color')).toBe('any');
    expect(mana('Add chosen color')).toBe('chosen');
  });
});
