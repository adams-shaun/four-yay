import { mount, unmount } from 'svelte';
import '../app.css';
import KeymapEditor from './KeymapEditor.svelte';
import KeyCheatSheet from './KeyCheatSheet.svelte';
import { KeymapStore } from '../lib/keymap.svelte';
import { clickedOutside } from '../lib/modals';

/**
 * KeysUi fixture mounts the REAL Keys editor inside a root that runs the
 * table's own outside-click test (Table.svelte's closeOptionsOutside uses
 * the same clickedOutside), so a click inside the editor that re-renders its
 * own target is proven to stay inside. window.__openSheet mounts the `?`
 * cheat sheet, open, whose onClose unmounts it and counts:
 *
 *  - window.__store: the editor's KeymapStore (storage null: nothing leaks);
 *  - window.__outside: outside clicks the root's window listener saw;
 *  - window.__unmountEditor: tears the editor down mid-capture;
 *  - window.__sheetCloses: how many times the sheet asked to close.
 */
interface FixtureWindow {
  __store: KeymapStore;
  __outside: number;
  __unmountEditor: () => void;
  __sheetCloses: number;
  __openSheet: () => void;
}
const win = window as unknown as FixtureWindow;

const root = document.querySelector('#root')!;
const store = new KeymapStore(null);
win.__store = store;
win.__outside = 0;
window.addEventListener('click', (e) => {
  if (clickedOutside(e, root)) win.__outside += 1;
});
// Table.svelte's window keydown runs before the sheet's (it is registered
// first) and always consumes Escape for the Options panel; the sheet must
// still close on it.
window.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') e.preventDefault();
});
const editor = mount(KeymapEditor, { target: root, props: { store } });
win.__unmountEditor = () => unmount(editor);

win.__sheetCloses = 0;
win.__openSheet = () => {
  const sheet = mount(KeyCheatSheet, {
    target: document.querySelector('#sheet')!,
    props: {
      open: true,
      keymap: store.current,
      onClose: () => {
        win.__sheetCloses += 1;
        void unmount(sheet);
      },
    },
  });
};
