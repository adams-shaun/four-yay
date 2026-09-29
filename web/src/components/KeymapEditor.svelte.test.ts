import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import KeymapEditor from './KeymapEditor.svelte';
import KeyCheatSheet from './KeyCheatSheet.svelte';
import { KeymapStore } from '../lib/keymap.svelte';
import { defaultKeymap, withBinding } from '../lib/keymap';

// Svelte's SSR output carries the scoped-style hash on every styled element,
// so a key cap renders as `<kbd class="svelte-…">`, not a bare `<kbd>`.
const KBD = '<kbd class="svelte-[a-z0-9]+">';

describe('KeymapEditor', () => {
  it('lists every group and shows default chords as key caps', () => {
    const h = render(KeymapEditor, { props: { store: new KeymapStore(null) } }).html;
    for (const g of ['Priority', 'Decisions', 'Profiles', 'View']) expect(h).toContain(`>${g}<`);
    expect(h).toMatch(new RegExp(`data-key-action="pass"[\\s\\S]*?${KBD}Space</kbd>`));
    expect(h).toMatch(new RegExp(`data-key-action="toggle-full-control"[\\s\\S]*?${KBD}Ctrl\\+Shift\\+F</kbd>`));
    expect(h).toContain('Reset all keys');
  });

  it('warns on a conflict', () => {
    const s = new KeymapStore(null);
    s.replace(withBinding(defaultKeymap(), 'undo', { code: 'Space', ctrl: false, shift: false, alt: false }));
    const h = render(KeymapEditor, { props: { store: s } }).html;
    expect(h).toContain('Also used by: Pass priority once');
  });
});

describe('KeyCheatSheet', () => {
  it('renders nothing closed, a labelled dialog open', () => {
    expect(render(KeyCheatSheet, { props: { open: false, keymap: defaultKeymap(), onClose: () => {} } }).html).not.toContain('role="dialog"');
    const h = render(KeyCheatSheet, { props: { open: true, keymap: defaultKeymap(), onClose: () => {} } }).html;
    expect(h).toContain('role="dialog"');
    expect(h).toContain('Keyboard shortcuts');
    expect(h).toMatch(new RegExp(`${KBD}Space</kbd>`));
  });
});
