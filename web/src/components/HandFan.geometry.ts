import { mount } from 'svelte';
import { IMAGE_KEY } from '../lib/images';
import type { CardView, Option, PlayerView } from '../protocol';
import type { CardOptions } from '../lib/cardoptions';
import '../app.css';
import HandFan from './HandFan.svelte';

/**
 * Geometry fixture for HandFan — the browser half of the hand-card peek
 * (fb-20260916T024357Z-9005ad6a). It mounts the REAL component (not a CSS
 * replica) into a fixed-size, overflow:hidden board-stage equivalent, so the
 * measured rects are what a real hover/focus produces against the real clip.
 * SSR markup alone cannot prove clipping or :hover geometry.
 *
 * The stage is the only box on the page (see the <style> in the .html):
 * position: relative + overflow: hidden, exactly the clipping contract
 * Table.svelte's .board gives the real fan. HandFan's .handtrack is absolute
 * bottom:0 inside it, so the stage's overflow is the clip that cuts the
 * fan's lowered half — which is the behaviour under test.
 *
 * Card art is settled before mount the way CardDetail.geometry.ts settles it:
 * each card's exact-name key under the versioned IMAGE_KEY namespace is
 * seeded with the empty string, the resolver's stored "known no image", so no
 * same-origin /art request is ever fired and every face renders the blank.
 * The blank does not affect the geometry — the .card box sizes itself from
 * width + aspect-ratio 63/88, and that box is what is measured.
 *
 * Seven cards over a 560px room engages the horizontal overlap (natural width
 * 788 > 560, step ≈ 76 < 104): the fan genuinely overlaps, so the hovered
 * card's z-order over its neighbours is observable through elementFromPoint
 * in the band two cards share. The first card is the one driven, because
 * sibling painting order puts its RIGHT neighbour on top of it at rest —
 * only the hover raise's z-index can put it back on top there, which makes
 * the assertion discriminate.
 */

const names = [
  'Squire',
  'Vanguard of Rose',
  'Bearscape',
  'Wall of Vines',
  'Oakenform',
  'Trained Pronghorn',
  'Sporemound',
];

names.forEach((n) => localStorage.setItem(`${IMAGE_KEY}${n}`, ''));

const hand: CardView[] = names.map((name, i) => ({
  id: i + 1,
  name,
  types: 'Creature — Human Soldier',
  mana_cost: '1 W',
  printing: { name },
  token: '',
  tapped: false,
  power: 2,
  toughness: 2,
  damage: 0,
  attacking: false,
  keywords: [],
  counters: {},
  controller: 0,
  owner: 0,
  summon_sick: false,
}));

// Card 1 carries ONE option (a direct `.action-icon` affordance), card 2
// THREE (a count badge whose menu must be opened before its `.menu__item`
// controls exist), and card 3 a payment shortcut. The keyboard fixture can Tab from a focused face onto
// BOTH kinds of action control and measure that the card STAYS raised — the
// raise must follow any focused descendant of the card, not just the face
// (fb-20260916T024357Z-9005ad6a). The bundle is hand-built, not derived from
// a Decision, because the fixture has no server; the shape is what the wire
// sends and what lib/cardoptions groups by.
const options: CardOptions = {
  byObj: new Map<number, Option[]>([
    [1, [{ index: 0, kind: 'cast', label: 'Cast Squire', obj: 1, player: 0 }]],
    [2, [
      { index: 1, kind: 'target', label: 'Squire', obj: 2, player: 0 },
      { index: 2, kind: 'target', label: 'Vanguard of Rose', obj: 2, player: 0 },
      { index: 3, kind: 'target', label: 'Bearscape', obj: 2, player: 0 },
    ]],
  ]),
  byPlayer: new Map<number, Option[]>(),
  picked: [],
  tone: 'offered',
  post: () => {},
};

const player: PlayerView = {
  seat: 0,
  name: 'You',
  life: 40,
  lost: false,
  library_size: 60,
  hand_size: hand.length,
  graveyard_size: 0,
  completed_dungeons: 0,
  hand,
  battlefield: [],
  graveyard: [],
  exile: [],
  pool: {},
  command: [],
  commanders: [],
  commander_casts: [],
};

mount(HandFan, {
  target: document.querySelector('#stage')!,
  props: {
    player, width: 560, options,
    paymentActions: [{
      id: 'pay-bearscape', cast: { object: 3, face: 0, origin: 'hand' },
      label: 'Cast Bearscape',
      plans: [{ id: 'plan', version: 1, cost: { generic: 0, mana: [0, 0, 0, 0, 0, 0] }, activations: [], pool_spend: [0, 0, 0, 0, 0, 0], pool_after: [0, 0, 0, 0, 0, 0] }],
    }],
  },
});
