import { safeStorage } from '../storage';

/**
 * pref.ts is the client-side protocol setting: which wire a seated table
 * plays over. Native (gorge's own stream) is the default; ManaBrew is the
 * opt-in. It is a property of the player's browser, never sent to the
 * server, and read once when a table mounts (a change applies on the next
 * table load).
 */

export type WireProtocol = 'native' | 'manabrew';

export const PROTOCOL_KEY = 'gorge.protocol.v1';

export const PROTOCOLS: { value: WireProtocol; label: string }[] = [
  { value: 'native', label: 'Native' },
  { value: 'manabrew', label: 'ManaBrew' },
];

export function loadProtocol(store: Storage | null = safeStorage()): WireProtocol {
  try {
    return store?.getItem(PROTOCOL_KEY) === 'manabrew' ? 'manabrew' : 'native';
  } catch {
    return 'native';
  }
}

export function saveProtocol(p: WireProtocol, store: Storage | null = safeStorage()): void {
  try {
    if (p === 'native') store?.removeItem(PROTOCOL_KEY);
    else store?.setItem(PROTOCOL_KEY, p);
  } catch {
    // A browser refusing site data keeps the default; storage.ts surfaces that.
  }
}

/**
 * activeWire records which wire the mounted table actually plays over (the
 * route sets it; a failed ManaBrew probe leaves it 'native'), so the
 * setting can tell a pending change from the live one.
 */
export const activeWire: { current: WireProtocol | null } = { current: null };
