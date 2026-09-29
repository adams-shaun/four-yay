import type { TurnSide } from './autopilot';
import type { PlaySettings, StepStop, StoppableStep } from './playsettings';

/** nextStop is one step-stop cell's three-state cycle: Off → Smart → Always → Off. */
export function nextStop(rule: StepStop): StepStop {
  return rule === 'off' ? 'smart' : rule === 'smart' ? 'forced' : 'off';
}

/** stopPatch is one cell's change as a withChange patch (deep-merged into steps field-wise). */
export function stopPatch(step: StoppableStep, side: TurnSide, rule: StepStop): Partial<PlaySettings> {
  return { steps: { [side]: { [step]: rule } } } as unknown as Partial<PlaySettings>;
}

/** stopWord names each cell state in plain words — the state is text, never colour alone. */
export function stopWord(rule: StepStop): string {
  return rule === 'off' ? 'Off' : rule === 'smart' ? 'Smart' : 'Always';
}

/** stopGlyph marks each cell state with a shape beside its word. */
export function stopGlyph(rule: StepStop): string {
  return rule === 'off' ? '·' : rule === 'smart' ? '◐' : '●';
}

/**
 * clampMs parses one pacing input as an integer clamped to [0, 10000]
 * (fb-20260917T004341Z: 0 keeps the instant-post path; above 10s a
 * wedged-looking client is a footgun we don't ship). Returns null for a
 * non-numeric/empty input — the caller then leaves the current value
 * unchanged.
 */
export function clampMs(raw: string): number | null {
  const n = Number.parseInt(raw, 10);
  if (Number.isNaN(n)) return null;
  return Math.min(10000, Math.max(0, n));
}
