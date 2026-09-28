import { mount } from 'svelte';
import '../app.css';
import TranscriptScrollFixture from './Transcript.scroll.fixture.svelte';

mount(TranscriptScrollFixture, { target: document.querySelector('#fixture')! });
