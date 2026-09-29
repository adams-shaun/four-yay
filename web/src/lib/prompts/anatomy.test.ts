import { describe, expect, it } from 'vitest';
import type { CardView, Decision, Option, PlayerView, StackView, View } from '../../protocol';
import { countWord, promptAnatomy, promptSource, selectionStatus, submitLabel } from './anatomy';
import { attackAllPicks, attackLines, blockLines, damageText, noBlocksAllowed, unblockedDamage } from './combat';

const card = (id: number, name: string, controller: number, extra: Partial<CardView> = {}): CardView => ({
  id, name, types: 'Creature — Bear', printing: { name } as CardView['printing'], token: '', tapped: false,
  power: 2, toughness: 2, damage: 0, attacking: false, controller, owner: controller, summon_sick: false, ...extra,
} as CardView);
const player = (seat: number, name: string, battlefield: CardView[] = [], life = 20): PlayerView => ({
  seat, name, life, lost: false, library_size: 40, hand_size: 0, graveyard_size: 0, hand: [], battlefield,
  graveyard: [], exile: [], pool: {}, completed_dungeons: 0, command: [], commanders: [], commander_casts: [],
} as unknown as PlayerView);
const view = (players: PlayerView[], stack: StackView[] = []): View => ({
  viewer: 0, visibility: 'seat', turn: 3, round: 2, step: 'main1', phase: 'main1', active: 0, priority: 0,
  over: false, draw: false, winner: null, players, stack, pending: [],
} as unknown as View);
const opt = (index: number, kind: string, label: string, extra: Partial<Option> = {}): Option => ({ index, kind, label, player: 0, ...extra });
const dec = (kind: string, options: Option[], extra: Partial<Decision> = {}): Decision => ({ seq: 1, player: 0, kind, prompt: 'p', min: 1, max: 1, options, ...extra });

describe('promptSource', () => {
  it('names a triggered ability by its controller: "Mira\'s Rhystic Study · triggered ability"', () => {
    const v = view([player(0, 'Ari'), player(1, 'Mira')], [{ id: 90, kind: 'trigger', name: 'Rhystic Study', text: '', controller: 1, source: 44, targets: [], optional: false } as StackView]);
    expect(promptSource(dec('choose', [], { source: 44 }), v)?.line).toBe("Mira's Rhystic Study · triggered ability");
  });

  it('names the viewer\'s own spell by type: "Your Lightning Bolt · instant you\'re casting"', () => {
    const bolt = card(7, 'Lightning Bolt', 0, { types: 'Instant' });
    const v = view([player(0, 'Ari'), player(1, 'Mira')], [{ id: 7, kind: 'spell', name: 'Lightning Bolt', text: '', controller: 0, targets: [], card: bolt, optional: false } as StackView]);
    const s = promptSource(dec('target', [], { source: 7 }), v);
    expect(s?.line).toBe("Your Lightning Bolt · instant you're casting");
    expect(s?.card?.name).toBe('Lightning Bolt');
  });

  it('is null for an unknown or absent source', () => {
    expect(promptSource(dec('choose', []), view([player(0, 'Ari')]))).toBeNull();
    expect(promptSource(dec('choose', [], { source: 999 }), view([player(0, 'Ari')]))).toBeNull();
  });
});

describe('promptAnatomy titles and plain lines', () => {
  const v = view([player(0, 'Ari'), player(1, 'Mira')]);

  it('asks the question by kind', () => {
    expect(promptAnatomy(dec('target', [opt(0, 'player', 'Mira', { player: 1 })]), v).title).toBe('Choose a target');
    expect(promptAnatomy(dec('target', [], { min: 2, max: 2 }), v).title).toBe('Choose two targets');
    expect(promptAnatomy(dec('modes', [opt(0, 'mode', 'A'), opt(1, 'mode', 'B'), opt(2, 'mode', 'C')], { min: 2, max: 2 }), v).title).toBe('Choose two');
    expect(promptAnatomy(dec('attackers', [], { min: 0, max: 3 }), v).title).toBe('Declare attackers');
    expect(promptAnatomy(dec('blockers', [], { min: 0, max: 3 }), v).title).toBe('Declare blockers');
    expect(promptAnatomy(dec('trigger_order', [opt(0, 'trigger', 'A'), opt(1, 'trigger', 'B')], { min: 2, max: 2 }), v).title).toBe('Order your triggers');
    expect(promptAnatomy(dec('starting_player', [opt(0, 'player', 'Ari')], { prompt: 'You won the toss. Choose who takes the first turn.' }), v).title).toBe('Who plays first?');
  });

  it('keeps a choose prompt as the question', () => {
    expect(promptAnatomy(dec('choose', [opt(0, 'x', 'Red')], { prompt: 'Choose a colour of mana' }), v).title).toBe('Choose a colour of mana');
  });

  it('reads target_effect damage as the plain line', () => {
    const a = promptAnatomy(dec('target', [], { target_effect: { api: 'DealDamage', damage: { amount: 3 } } } as Partial<Decision>), v);
    expect(a.plain).toBe('Deals 3 damage to the target. Click a highlighted card or player, or pick below.');
  });

  it('puts an optional trigger\'s description on the plain line', () => {
    const a = promptAnatomy(dec('trigger_optional', [opt(0, 'yes', 'Yes'), opt(1, 'no', 'No')], { prompt: 'Put this optional triggered ability on the stack? — Draw a card' }), v);
    expect(a.title).toBe('Use this optional ability?');
    expect(a.plain).toBe('Draw a card.');
  });

  it('never repeats the title as the plain line', () => {
    const a = promptAnatomy(dec('choose', [opt(0, 'x', 'A'), opt(1, 'x', 'B')], { prompt: 'Choose 1 option.', min: 1, max: 1 }), v);
    expect(a.plain).toBeNull();
  });
});

describe('footer words', () => {
  it('counts a multi-pick and stays silent on a single click', () => {
    expect(selectionStatus(dec('modes', [], { min: 2, max: 2 }), 1)).toBe('1 of 2 chosen');
    expect(selectionStatus(dec('attackers', [], { min: 0, max: 3 }), 2)).toBe('2 chosen · up to 3');
    expect(selectionStatus(dec('choose', []), 0)).toBeNull();
    expect(submitLabel(dec('blockers', [], { min: 0, max: 2 }))).toBe('Confirm blocks');
    expect(submitLabel(dec('choose', [], { min: 2, max: 2 }))).toBe('Choose 2');
    expect(countWord(2)).toBe('two');
    expect(countWord(12)).toBe('12');
  });
});

describe('combat summaries', () => {
  const thragtusk = card(30, 'Thragtusk', 1, { power: 5, toughness: 3, attacking: true, attacking_player: 0 });
  const confidant = card(10, 'Dark Confidant', 0, { power: 2, toughness: 1 });
  const elves = card(11, 'Llanowar Elves', 0, { power: 1, toughness: 1 });
  const v = view([player(0, 'Ari', [confidant, elves], 17), player(1, 'Mira', [thragtusk])]);
  const blocks = dec('blockers', [
    opt(0, 'block', 'Dark Confidant blocks Thragtusk', { obj: 10, attacker: 30, group: 'b10' }),
    opt(1, 'block', 'Llanowar Elves blocks Thragtusk', { obj: 11, attacker: 30, group: 'b11' }),
  ], { min: 0, max: 2 });

  it('says what an unblocked attacker would do: "you\'d take 5 (17 → 12)"', () => {
    expect(damageText(unblockedDamage(blocks, v, []))).toBe("you'd take 5 (17 → 12)");
    expect(unblockedDamage(blocks, v, [0])).toBeNull();
  });

  it('summarises each attacker with its blockers', () => {
    expect(blockLines(blocks, v, [])).toEqual([{ attacker: 30, head: 'Thragtusk 5/3 attacking you', detail: 'Unblocked', blocked: false }]);
    expect(blockLines(blocks, v, [0, 1])[0].detail).toBe('Blocked by Dark Confidant and Llanowar Elves');
  });

  it('refuses no-blocks when a block is forced', () => {
    expect(noBlocksAllowed(blocks)).toBe(true);
    expect(noBlocksAllowed({ ...blocks, options: [{ ...blocks.options[0], required: true }] })).toBe(false);
  });

  it('attack with all picks one defender per creature, required pairings first, capped by max', () => {
    const attack = dec('attackers', [
      opt(0, 'attacker', 'a', { obj: 10, player: 1 }),
      opt(1, 'attacker', 'b', { obj: 10, player: 2 }),
      opt(2, 'attacker', 'c', { obj: 11, player: 1 }),
      opt(3, 'attacker', 'd', { obj: 12, player: 2, required: true }),
    ], { min: 0, max: 2 });
    expect(attackAllPicks(attack)).toEqual([3, 0]);
    expect(attackAllPicks({ ...attack, max: 5 })).toEqual([3, 0, 2]);
    expect(attackLines(attack, v, [0])).toEqual(['Dark Confidant → Mira']);
  });
});
