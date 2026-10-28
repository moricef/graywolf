<script>
  import { onMount } from 'svelte';
  import { Button, Input, Toggle } from '@chrissnell/chonky-ui';
  import { api } from '../lib/api.js';
  import { toasts } from '../lib/stores.js';
  import { channelsStore, start as startChannelsStore } from '../lib/stores/channels.svelte.js';
  import PageHeader from '../components/PageHeader.svelte';
  import FormField from '../components/FormField.svelte';
  import ChannelListbox from '../lib/components/ChannelListbox.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let config = $state(null);
  let snapshot = $state(null);
  let error = $state('');
  let saving = $state(false);
  let resetOpen = $state(false);
  let channels = $derived(channelsStore.list);
  async function refresh() {
    try { snapshot = await api.get('/rf/dx'); }
    catch (e) { error = e.message; }
  }
  onMount(() => {
    const stop = startChannelsStore();
    api.get('/dx/config').then((value) => { config = value; }).catch((e) => { error = e.message; });
    refresh();
    const timer = setInterval(refresh, 5000);
    return () => { clearInterval(timer); if (typeof stop === 'function') stop(); };
  });
  async function save() {
    saving = true; error = '';
    try {
      const body = { ...config };
      for (const key of ['channel', 'window_minutes', 'min_interval_minutes', 'status_interval_minutes', 'capabilities_interval_minutes', 'min_distance_km', 'min_trigger_km']) body[key] = Number(body[key]);
      config = await api.put('/dx/config', body);
      toasts.success('DX and station report settings saved');
      await refresh();
    } catch (e) { error = e.message; }
    finally { saving = false; }
  }
  async function send(kind) {
    error = '';
    try { await api.post(`/dx/reports/${kind}`, {}); toasts.success('Station report sent'); }
    catch (e) { error = e.message; }
  }
  async function resetRecords() {
    try { await api.delete('/dx/record'); await refresh(); toasts.success('All-time DX records cleared'); }
    catch (e) { error = e.message; }
    resetOpen = false;
  }
</script>

<PageHeader title="DX / Status" subtitle="Direct RF DX detection and station reports" />
{#if error}<p class="error" role="alert">{error}</p>{/if}

<section class="panel" data-testid="dx-results">
  <h2>Current DX</h2>
  {#if !snapshot}<p>Loading…</p>
  {:else if !snapshot.position_available}<p>Set your station position in GPS or an enabled fixed-position beacon to calculate DX.</p>
  {:else if !snapshot.channels?.length}<p>No qualifying direct RF position in the configured window.</p>
  {:else}
    {#each snapshot.channels as dx (dx.channel)}
      <p><strong>Channel {dx.channel}: {dx.callsign} — {Math.round(dx.distance_km)} km — {Math.round(dx.bearing_deg) % 360}°</strong><br />Received {new Date(dx.received_at).toLocaleString()}</p>
    {/each}
  {/if}
  <h3>Max ever</h3>
  {#each snapshot?.max_ever || [] as record (record.channel)}
    <p>Channel {record.channel}: <strong>{record.callsign} — {Math.round(record.distance_km)} km</strong> · {new Date(record.received_at).toLocaleString()}</p>
  {:else}<p>No all-time DX record yet.</p>{/each}
  <Button onclick={() => resetOpen = true} disabled={!snapshot?.max_ever?.length}>Clear max ever</Button>
</section>

{#if config}
  <form onsubmit={(event) => { event.preventDefault(); save(); }}>
    <section class="panel">
      <h2>DX detection</h2>
      <p>A DX reception must contain a station position received directly by RF, without a used relay hop. Internet, third-party, object/item and 0,0 positions are excluded.</p>
      <div class="fields">
        <FormField label="Minimum distance (km)" id="dx-min-distance"><Input id="dx-min-distance" type="number" min="0" max="21000" bind:value={config.min_distance_km} /></FormField>
        <FormField label="Window (minutes)" id="dx-window" hint="A farther reception remains DX until its own window expires, even if that station later moves closer."><Input id="dx-window" type="number" min="1" max="1440" bind:value={config.window_minutes} /></FormField>
      </div>
      <FormField label="Ignore stations" id="dx-ignore" hint="Exact callsign-SSID values separated by spaces or commas. These stations still count in iGate statistics."><Input id="dx-ignore" bind:value={config.ignored_calls} placeholder="F4MLV-2 F4MLV-15" /></FormField>
      <Toggle label="Immediate DX status notifications" bind:checked={config.notify_enabled} />
      <p>Optional. Requires status reports and “Include DX” below. A new farther station can trigger immediately; repeated reports for the same DX station respect the minimum interval. Expiration alone never triggers a transmission.</p>
      <div class="fields">
        <FormField label="Minimum trigger distance (km)" id="dx-trigger"><Input id="dx-trigger" type="number" min="0" max="21000" bind:value={config.min_trigger_km} /></FormField>
        <FormField label="Minimum notification interval (minutes)" id="dx-trigger-interval"><Input id="dx-trigger-interval" type="number" min="1" max="1440" bind:value={config.min_interval_minutes} /></FormField>
      </div>
    </section>

    <section class="panel">
      <h2>Station reports</h2>
      <p>These reports have their own timers and are independent of position beacons.</p>
      <Toggle label="Periodic status report" bind:checked={config.status_enabled} />
      <div class="fields">
        <FormField label="Status text" id="dx-status-text" hint="APRS status packet (>), optionally followed by the current DX."><Input id="dx-status-text" bind:value={config.status_text} maxlength="150" /></FormField>
        <FormField label="Status interval (minutes)" id="dx-status-interval"><Input id="dx-status-interval" type="number" min="1" max="1440" bind:value={config.status_interval_minutes} /></FormField>
      </div>
      <Toggle label="Periodic iGate capabilities report" bind:checked={config.capabilities_enabled} />
      <FormField label="Capabilities interval (minutes)" id="dx-capabilities-interval" hint="APRS capability packet: <IGATE,MSG_CNT=…,LOC_CNT=…,DIR_CNT=…,RF_CNT=… . Default: once per hour."><Input id="dx-capabilities-interval" type="number" min="1" max="1440" bind:value={config.capabilities_interval_minutes} /></FormField>
      <Toggle label="Include DX in reports" bind:checked={config.include_dx} />
      <FormField label="Receive channel" id="dx-channel" hint="Statistics and DX come from this channel. RF reports also transmit on this channel."><ChannelListbox id="dx-channel" bind:value={config.channel} valueType="number" {channels} allowNone={true} noneLabel="Select a receive channel" /></FormField>
      <FormField label="Report destination" id="dx-send-path">
        <select id="dx-send-path" bind:value={config.send_path}><option value="is_only">APRS-IS only</option><option value="rf">RF only</option><option value="both">RF + APRS-IS</option></select>
      </FormField>
      <FormField label="RF path" id="dx-path" hint="Leave empty for a direct RF report. APRS-IS reports use TCPIP*."><Input id="dx-path" bind:value={config.path} placeholder="WIDE1-1,WIDE2-1" /></FormField>
      <div class="actions"><Button type="submit" variant="primary" disabled={saving}>Save</Button><Button type="button" onclick={() => send('status')} disabled={!config.channel}>Send status now</Button><Button type="button" onclick={() => send('capabilities')} disabled={!config.channel}>Send capabilities now</Button></div>
      <p>Save settings before sending a report. Directed ?DX queries return the current DX in a one-shot reply on the incoming transport.</p>
    </section>
  </form>
{/if}

<section class="panel">
  <h2>iGate statistics</h2>
  <p>Distinct stations over the last 30 minutes; MSG_CNT accumulates APRS-IS message handoffs to RF since startup. LOC_CNT follows Graywolf's direct-only reachability rule and equals DIR_CNT.</p>
  {#each snapshot?.statistics || [] as stats (stats.channel)}
    <p>Channel {stats.channel}: <code>MSG_CNT={stats.msg_cnt} LOC_CNT={stats.loc_cnt} DIR_CNT={stats.dir_cnt} RF_CNT={stats.rf_cnt}</code></p>
  {:else}<p>No recent RF stations or downlinked messages.</p>{/each}
</section>

<ConfirmDialog bind:open={resetOpen} title="Clear max ever?" description="Clear the saved all-time DX records. Current window receptions remain visible." confirmLabel="Clear records" onConfirm={resetRecords} />

<style>
  .panel { border:1px solid var(--border-color);border-radius:8px;padding:18px;margin:18px 0;max-width:1050px; }
  h2 { font-size:18px;margin:0 0 14px; } h3 { font-size:15px;margin-top:20px; }
  p { font-size:13px;color:var(--text-secondary);line-height:1.6; } strong { color:var(--text-primary); }
  .fields { display:grid;grid-template-columns:1fr 1fr;gap:18px; }
  .actions { display:flex;flex-wrap:wrap;gap:10px;margin-top:18px; }
  select { width:100%;padding:10px;color:var(--text-primary);background:var(--bg-primary);border:1px solid var(--border-color); }
  .error { color:var(--color-danger); }
  @media(max-width:700px){.fields{grid-template-columns:1fr}}
</style>
