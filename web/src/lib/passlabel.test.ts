import { describe, expect, it } from 'vitest';
import type { CardView, PlayerView, StackView, View } from '../protocol';
import { nextStep, passLabel } from './passlabel';
import { seatGlow } from './seatglow';
import type { CardOptions } from './cardoptions';

const player = (seat: number, battlefield: CardView[] = []): PlayerView => ({
  seat, name: `P${seat}`, life: 20, lost: false, library_size: 50, hand_size: 7, graveyard_size: 0,
  hand: [], battlefield, graveyard: [], exile: [], completed_dungeons: 0, pool: {}, available: {}, command: [], commanders: [], commander_casts: [],
} as PlayerView);
const view = (step: string, stack: StackView[] = [], players = [player(0), player(1)]): View => ({
  viewer: 0, visibility: 'seat', active: 0, priority: 0, turn: 3, round: 2, step, phase: 'x', over: false, draw: false, winner: null, players, stack, pending: [],
} as unknown as View);

describe('passLabel — the gilt button names what passing does', () => {
  it('with a stack, passing lets the top object resolve', () => {
    const stack = [{ id: 9, kind: 'spell', name: 'Bolt', text: '', controller: 1, targets: [], optional: false }, { id: 10, kind: 'trigger', name: 'Rhystic Study', text: '', controller: 1, targets: [], optional: false }] as StackView[];
    expect(passLabel({ view: view('main1', stack), passAvailable: true, prompt: false, waitingOn: null })).toEqual({ title: 'Pass', sub: 'Let Rhystic Study resolve', passes: true });
  });

  it('with an empty stack, passing moves to the next step, and at the end passes the turn', () => {
    expect(passLabel({ view: view('main1'), passAvailable: true, prompt: false, waitingOn: null }).sub).toBe('Move to Beginning of combat');
    expect(passLabel({ view: view('end'), passAvailable: true, prompt: false, waitingOn: null }).sub).toBe('Pass the turn');
    expect(nextStep('cleanup')).toBeNull();
    expect(nextStep('nonsense')).toBeNull();
  });

  it('is Waiting while a prompt needs an answer, or while another seat acts', () => {
    expect(passLabel({ view: view('main1'), passAvailable: false, prompt: true, waitingOn: null })).toEqual({ title: 'Waiting', sub: 'Answer the prompt', passes: false });
    expect(passLabel({ view: view('main1'), passAvailable: false, prompt: false, waitingOn: 'Mira' }).sub).toBe('for Mira');
  });
});

describe('seatGlow — verdigris for a choice, ember for an attack', () => {
  it('an attacked seat glows ember; an offered player glows verdigris', () => {
    const attacker = { id: 5, name: 'Bear', types: 'Creature', attacking: true, attacking_player: 1 } as CardView;
    const v = view('declare-attackers', [], [player(0, [attacker]), player(1)]);
    expect(seatGlow(v, null, 1)).toEqual({ target: false, attacked: true });
    expect(seatGlow(v, null, 0)).toEqual({ target: false, attacked: false });
    const opts = { byObj: new Map(), byPlayer: new Map([[0, []]]), picked: [], tone: 'initiative', post: () => {} } as unknown as CardOptions;
    expect(seatGlow(v, opts, 0)).toEqual({ target: true, attacked: false });
  });
});
