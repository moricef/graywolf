<script>
  import { onMount } from 'svelte';
  import { Button, Box } from '@chrissnell/chonky-ui';
  import PageHeader from '../components/PageHeader.svelte';

  let links = $state([]);
  let loading = $state(true);
  let error = $state('');
  let updatedAt = $state(null);
  let pollTimer;

  onMount(() => {
    loadLinks();
    pollTimer = setInterval(loadLinks, 5000);
    return () => clearInterval(pollTimer);
  });

  async function loadLinks() {
    try {
      const response = await fetch('/api/rxt/links', { credentials: 'same-origin' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const body = await response.json();
      links = Array.isArray(body) ? body : [];
      updatedAt = new Date();
      error = '';
    } catch (err) {
      error = `Unable to load RXT telemetry: ${err?.message || err}`;
    } finally {
      loading = false;
    }
  }

  function age(iso) {
    const timestamp = Date.parse(iso);
    if (!Number.isFinite(timestamp)) return '—';
    const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
    return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
  }

  function number(value, digits = 0) {
    const n = Number(value);
    return Number.isFinite(n) ? n.toFixed(digits) : '—';
  }

  function positionState(link) {
    if (link.from_position && link.to_position) return 'drawable';
    const missing = [];
    if (!link.from_position) missing.push(link.from);
    if (!link.to_position) missing.push(link.to);
    return `missing ${missing.join(', ')}`;
  }
</script>

<PageHeader
  title="RXT Telemetry"
  subtitle="Recent LoRa reception measurements from the configured iGate"
>
  <Button onclick={loadLinks} disabled={loading}>Refresh</Button>
</PageHeader>

{#if error}
  <div class="error" role="alert">{error}</div>
{/if}

<Box>
  <div class="summary">
    <span><strong>{links.length}</strong> active link{links.length === 1 ? '' : 's'}</span>
    <span><strong>{links.filter((link) => link.from_position && link.to_position).length}</strong> drawable</span>
    {#if updatedAt}<span>Updated {updatedAt.toLocaleTimeString()}</span>{/if}
  </div>
</Box>

<div class="telemetry-block">
  {#if loading && links.length === 0}
    <Box><div class="empty">Loading RXT telemetry…</div></Box>
  {:else if links.length === 0}
    <Box><div class="empty">No recent RXT links.</div></Box>
  {:else}
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Age</th>
            <th>Link</th>
            <th class="numeric">RSSI</th>
            <th class="numeric">SNR</th>
            <th class="numeric">FO</th>
            <th class="numeric">TTH</th>
            <th>Map</th>
            <th>Packet</th>
          </tr>
        </thead>
        <tbody>
          {#each links as link (`${link.from}>${link.to}`)}
            <tr>
              <td class="nowrap">{age(link.observed_at)}</td>
              <td class="link"><strong>{link.from}</strong><span> → </span><strong>{link.to}</strong></td>
              <td class="numeric nowrap">{number(link.rssi_dbm)} dBm</td>
              <td class="numeric nowrap">{number(link.snr_db, 2)} dB</td>
              <td class="numeric nowrap">{number(link.fo_hz)} Hz</td>
              <td class="numeric nowrap">{number(link.tth_ms)} ms</td>
              <td>
                <span class:drawable={link.from_position && link.to_position} class="position">
                  {positionState(link)}
                </span>
              </td>
              <td class="packet">{link.packet || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<p class="footnote">
  One row is retained per directed link for 30 minutes. New observations replace
  older observations of the same link. A link is drawn on the Live Map only when
  both stations have known positions.
</p>

<style>
  .error {
    margin-bottom: 12px;
    padding: 10px 12px;
    color: var(--color-danger, #ff6b6b);
    border: 1px solid currentColor;
    border-radius: 6px;
  }
  .summary {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 24px;
    color: var(--color-text-dim);
    font-size: 13px;
  }
  .summary strong { color: var(--color-text); }
  .telemetry-block { margin-top: 12px; }
  .empty { color: var(--color-text-dim); text-align: center; padding: 24px; }
  .table-scroll {
    overflow-x: auto;
    border: 1px solid var(--color-border);
    border-radius: 6px;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
    background: var(--color-surface);
  }
  th, td {
    padding: 9px 10px;
    border-bottom: 1px solid var(--color-border);
    text-align: left;
    vertical-align: top;
  }
  th {
    color: var(--color-text-dim);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
  }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr:hover { background: var(--color-surface-hover, rgba(127, 127, 127, 0.08)); }
  .numeric { text-align: right; }
  .nowrap, .link { white-space: nowrap; }
  .packet {
    min-width: 280px;
    max-width: 520px;
    font-family: var(--font-mono, monospace);
    overflow-wrap: anywhere;
  }
  .position { color: var(--color-text-dim); white-space: nowrap; }
  .position.drawable { color: #2fbf71; }
  .footnote {
    margin: 10px 2px 0;
    color: var(--color-text-dim);
    font-size: 12px;
    line-height: 1.5;
  }
</style>
