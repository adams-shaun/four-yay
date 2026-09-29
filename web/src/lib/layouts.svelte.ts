import {
  addProfile,
  applyAt,
  applyNamed,
  applyPreset,
  cycled,
  cycleList,
  labelOf,
  loadLibrary,
  removeNamed,
  renameNamed,
  saveAs,
  saveLibrary,
  withCurrent,
  type CycleEntry,
  type LayoutLibrary,
} from './layoutlibrary';
import {
  clamp,
  cloneProfile,
  compactRows,
  defaultSplit,
  defaultRailWidth,
  RAIL_MAX,
  RAIL_MIN,
  SPLIT_MAX,
  SPLIT_MIN,
  type LayoutProfile,
  type PresetId,
  type Region,
  type RegionKey,
} from './layoutprofile';
import { safeStorage } from './storage';

/**
 * layouts.svelte.ts is the reactive shell over the pure layout library: ONE
 * store every board component reads (`layoutStore.profile`), so a drawer
 * change or a hotkey re-renders the table with no event plumbing. Every
 * write persists the library. It replaces the old layoutsettings store.
 *
 * The prompt's placement is read by the decision-prompt surfaces (lane C):
 * `layoutStore.prompt` is the live `{placement, x, y}` and `setPromptPosition`
 * saves a dragged floating prompt's position.
 */
export class LayoutStore {
  #storage: Storage | null;
  lib = $state<LayoutLibrary>(loadLibrary(null));
  /** drawerOpen is whether the Layout drawer is showing (not persisted). */
  drawerOpen = $state(false);

  constructor(storage: Storage | null | undefined = undefined) {
    this.#storage = storage !== undefined ? storage : safeStorage();
    this.lib = loadLibrary(this.#storage);
  }

  /** profile is the live working copy. */
  get profile(): LayoutProfile {
    return this.lib.current;
  }
  get label(): string {
    return labelOf(this.lib);
  }
  get names(): string[] {
    return [...this.lib.order];
  }
  get entries(): CycleEntry[] {
    return cycleList(this.lib);
  }
  get prompt(): LayoutProfile['panels']['prompt'] {
    return this.lib.current.panels.prompt;
  }

  #commit(next: LayoutLibrary): void {
    this.lib = next;
    saveLibrary(this.#storage, next);
  }

  /** edit applies a mutation to a copy of the working copy and saves it. */
  edit(mut: (p: LayoutProfile) => void): void {
    const p = cloneProfile(this.lib.current);
    mut(p);
    p.regions = compactRows(p.regions);
    this.#commit(withCurrent(this.lib, p));
  }

  setRegion(k: RegionKey, patch: Partial<Region>): void {
    this.edit((p) => {
      p.regions[k] = { ...p.regions[k], ...patch };
    });
  }

  setSplit(split: number): void {
    this.edit((p) => {
      p.table.split = Math.round(clamp(split, SPLIT_MIN, SPLIT_MAX) * 100) / 100;
    });
  }

  resetSplit(seats: number): void {
    this.setSplit(defaultSplit(seats));
  }

  setRailWidth(width: number): void {
    this.edit((p) => {
      p.panels.railWidth = Math.round(clamp(width, RAIL_MIN, RAIL_MAX) * 100) / 100;
    });
  }

  resetRailWidth(seats: number): void {
    this.setRailWidth(defaultRailWidth(seats));
  }

  toggleStacking(): void {
    this.edit((p) => {
      p.cards.stacking = !p.cards.stacking;
    });
  }

  setPromptPosition(x: number, y: number): void {
    this.edit((p) => {
      p.panels.prompt = { ...p.panels.prompt, x: clamp(x, 0, 1), y: clamp(y, 0, 1) };
    });
  }

  applyPreset(id: PresetId): void {
    this.#commit(applyPreset(this.lib, id));
  }
  applyNamed(name: string): void {
    this.#commit(applyNamed(this.lib, name));
  }
  /** applyAt applies the n-th (1-based) profile of the cycle list; false when there is none. */
  applyAt(n: number): boolean {
    const next = applyAt(this.lib, n - 1);
    if (next === null) return false;
    this.#commit(next);
    return true;
  }
  cycle(delta: number): void {
    this.#commit(cycled(this.lib, delta));
  }
  saveAs(name: string): void {
    this.#commit(saveAs(this.lib, name));
  }
  add(name: string, p: LayoutProfile): void {
    this.#commit(addProfile(this.lib, name, p));
  }
  remove(name: string): void {
    this.#commit(removeNamed(this.lib, name));
  }
  rename(from: string, to: string): void {
    this.#commit(renameNamed(this.lib, from, to));
  }
  replace(lib: LayoutLibrary): void {
    this.#commit(lib);
  }
}

/** layoutStore is the one store instance every component reads. */
export const layoutStore = new LayoutStore();
