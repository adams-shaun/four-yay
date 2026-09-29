/** Table-local arm state/callback; spectators and standalone tiles have none. */
export const REPEAT_ARM = Symbol('repeat-arm');
export interface RepeatArmContext {
  candidate: () => { source: number; stackId: number | null; sourceName: string } | null;
  disabled: () => boolean;
  arm: (target: number) => void;
}
