// Isolated browser fixture; the test intercepts every API request.
import '../src/app.css';
import { mount } from 'svelte';
import Kiss from '../src/routes/Kiss.svelte';
mount(Kiss, {target: document.getElementById('app')});
