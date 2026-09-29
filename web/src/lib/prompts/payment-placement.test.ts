import { describe, expect, it } from 'vitest';
import type { Decision } from '../../protocol';
import { defaultProfile } from '../layoutprofile';
import { LAYOUTS_KEY, loadLibrary } from '../layoutlibrary';
import { effectivePlacement } from './dock';

const payment = {
  seq: 2, player: 0, kind: 'choose', prompt: 'Pay for a spell', min: 1, max: 1,
  options: [{ index: 0, kind: 'mana', label: 'Add U', obj: 10, player: 0 }],
  mana_payment: { card: 9, cost: { generic: 0, mana: [0, 1, 0, 0, 0, 0] }, owed: { generic: 0, mana: [0, 1, 0, 0, 0, 0] }, pool: [0, 0, 0, 0, 0, 0], autofill: [] },
} as Decision;
const target = { seq: 3, player: 0, kind: 'target', prompt: 'Choose target', min: 1, max: 1, options: [{ index: 0, kind: 'player', label: 'Opponent' }] } as Decision;

describe('payment dock on upgrading persisted layouts', () => {
  it('loads the shipped table-default library and routes its payment to the rail without rewriting its chosen layout', () => {
    const current = defaultProfile();
    current.panels.prompt.placement = 'table';
    delete (current.panels as { railWidth?: number }).railWidth; // shipped before rail resizing
    const stored = { version: 1, current, profiles: { Mine: current }, order: ['Mine'], active: 'Mine' };
    const storage = { getItem: (key: string) => key === LAYOUTS_KEY ? JSON.stringify(stored) : null } as Storage;
    const lib = loadLibrary(storage);
    expect(lib.current.panels.prompt.placement).toBe('table'); // precondition: old default survived validation
    expect(lib.profiles.Mine.panels.prompt.placement).toBe('table');
    expect(lib.current.panels.railWidth).toBe(defaultProfile().panels.railWidth);
    expect(effectivePlacement(lib.current.panels.prompt, payment)).toBe('rail');
    expect(effectivePlacement(lib.profiles.Mine.panels.prompt, payment)).toBe('rail');
    expect(effectivePlacement(lib.current.panels.prompt, target)).toBe('table');
  });

  it('preserves explicit bottom and floating placement for payment and does not move other questions', () => {
    const p = defaultProfile().panels.prompt;
    expect(effectivePlacement({ ...p, placement: 'dock-bottom' }, payment)).toBe('rail-bottom');
    expect(effectivePlacement({ ...p, placement: 'float' }, payment)).toBe('floating');
    expect(effectivePlacement({ ...p, placement: 'table' }, target)).toBe('table');
  });
});
