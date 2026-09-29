import { mount } from 'svelte';
import '../../app.css';
import PriorityOptionsFixture from './PriorityOptions.fixture.svelte';

// The fixture reads ?case itself, so the case is a plain URL value rather than
// a reactive prop referenced in a one-time initializer.
mount(PriorityOptionsFixture, { target: document.querySelector('#fixture')! });
