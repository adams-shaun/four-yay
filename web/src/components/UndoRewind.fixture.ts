import { mount } from 'svelte';
import '../app.css';
import UndoRewindFixture from './UndoRewind.fixture.svelte';

// The whole fixture is the Svelte component: see UndoRewind.fixture.svelte.
mount(UndoRewindFixture, { target: document.getElementById('fixture')! });
