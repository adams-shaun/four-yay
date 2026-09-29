import { defaultKeymap, loadKeymap, saveKeymap, withBinding, withoutBinding, type Binding, type KeyAction, type Keymap } from './keymap';
import { safeStorage } from './storage';

/**
 * KeymapStore is the reactive shell over lib/keymap (the pure model), the
 * same split layoutsettings.svelte.ts uses. `capturing` is true while the
 * editor waits for a chord: the table's hotkey listener must stand down then,
 * or the chord being recorded would also fire.
 */
export class KeymapStore {
  #storage: Storage | null;
  current = $state<Keymap>(defaultKeymap());
  capturing = $state(false);

  constructor(storage: Storage | null | undefined = undefined) {
    this.#storage = storage !== undefined ? storage : safeStorage();
    this.current = loadKeymap(this.#storage);
  }

  #set(k: Keymap) {
    this.current = k;
    saveKeymap(this.#storage, k);
  }

  add(a: KeyAction, b: Binding) { this.#set(withBinding(this.current, a, b)); }
  remove(a: KeyAction, i: number) { this.#set(withoutBinding(this.current, a, i)); }
  reset() { this.#set(defaultKeymap()); }
  replace(k: Keymap) { this.#set(k); }
}

export const keymapStore = new KeymapStore();
