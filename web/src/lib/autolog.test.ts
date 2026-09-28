import { describe, expect, it } from 'vitest';
import type { Decision, View } from '../protocol';
import { passDiagnostics } from './autopilot';
import { AUTO_LOG_CAP, pushAutoPassLog, type AutoPassDiagnostics, type AutoPassLog } from './autolog';

describe('advanced autopass diagnostics', () => {
  it('captures the verdict, evaluated option signals, yield hit, and bounded pass snapshot', () => {
    const decision = {
      kind: 'priority', min: 1, max: 1,
      options: [
        { index: 0, kind: 'cast', label: 'Cast Counterspell', player: 0 },
        { index: 1, kind: 'pass', label: 'Pass', player: 0 },
      ],
    } as unknown as Decision;
    const top = { id: 9, controller: 1, kind: 'spell', name: 'Threat', text: 'Deal damage' };
    const view = { viewer: 0, turn: 7, step: 'main1', active: 0, players: [], stack: [top] } as unknown as View;
    const diagnostics: AutoPassDiagnostics = {
      ...passDiagnostics(decision, view, 0, 'auto: no-stop-rule', new Set(['1:Threat:Deal damage'])),
      view: JSON.parse(JSON.stringify(view)) as View,
    };

    expect(decision.options.some((option) => option.kind === 'cast')).toBe(true);
    expect(view.stack).toHaveLength(1);
    expect(diagnostics).toMatchObject({
      verdict: 'auto: no-stop-rule',
      optionKinds: ['cast', 'pass'],
      actionableOptions: ['Cast Counterspell'],
      castableAfterTap: [],
      respondableOption: true,
      yieldsHit: true,
    });
    expect(diagnostics.view).toEqual(view);
    let notes: AutoPassLog[] = Array.from({ length: AUTO_LOG_CAP }, (_, i) => ({ id: i, turn: i, text: `pass ${i}` }));
    notes = pushAutoPassLog(notes, 'latest', 99, AUTO_LOG_CAP, diagnostics);
    expect(notes).toHaveLength(AUTO_LOG_CAP);
    expect(notes[0].text).toBe('pass 1');
    expect(notes.at(-1)?.diagnostics?.view.stack[0].name).toBe('Threat');
  });
});
