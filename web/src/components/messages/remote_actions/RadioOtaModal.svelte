<script>
  import { untrack, tick } from 'svelte';
  import { rememberOtaDraft, otaDraft, discardOtaDraft, frequencyHz } from '../../../lib/remote_actions/radioOtaDrafts.js';
  import { Button, Input, Modal, Icon } from '@chrissnell/chonky-ui';
  import { otaRequest, otaProgress, otaPhase, otaStartPath, otaPreflightPath, otaImageDetails, generateRouteSecret, otaBoards, otaBoard } from '../../../lib/remote_actions/radioOta.js';
  let { open = $bindable(false), target, credential } = $props();
  // A prop getter can depend on the whole parent conversation object. Buffer
  // primitive identity/open values so replacing that object for an incoming
  // APRS message cannot retrigger the initialization effect for the same call.
  const station = $derived(target);
  const showing = $derived(open);
  let imageInput=$state(null);

  let cfg = $state(null), job = $state(null), firmware = $state(null);
  let err = $state(''), busy = $state(false), ready = $state(false), engineError = $state('');
  let showRouteKey = $state(false), routeKeyNotice = $state('');
  let controller = $state(''), signedTarget = $state(''), imageDetails = $state(null);
  let options = $state({expected_version:'', replace_existing:false, require_offline:false, exercise_confirm_timeout:false});
  let frequencies = $state({normal:'',transfer:'',target_normal:''});
  let generation = 0;
  let stablePorts=$state([]);
  const bridgePorts=$derived([...stablePorts,...ports.filter(p=>!/^\/dev\/tty(?:ACM|USB)/.test(p.path))]);
  let editing = $state(false), ports = $state([]), loaded = $state(false);
  const running = $derived(job?.state === 'running');
  const separateService=$derived(!!cfg?.target_normal && ['frequency_hz','bandwidth_hz','spreading_factor','coding_rate','power_dbm'].some(k=>cfg.target_normal[k]!==cfg.normal[k]));
  const base = $derived(`/radio-ota/${encodeURIComponent(target)}`);
  const emptyProfile = () => ({ frequency_hz: 0, bandwidth_hz: 125000, spreading_factor: 7, coding_rate: 6, power_dbm: 2 });

  async function refresh(destination, first = false, requestGeneration = generation) {
    try {
      const data = await otaRequest(`/radio-ota/${encodeURIComponent(destination)}`);
      if (destination !== target || !open || requestGeneration !== generation) return;
      controller = data.controller || ''; signedTarget = data.signed_target || credential?.signed_target || target;
      stablePorts=data.stable_ports || [];
      job = data.job; ready = data.engine_ready; engineError = data.engine_error;
      if (first) {
        showRouteKey = false; routeKeyNotice = '';
        cfg = { ...data.settings, target_board: data.settings.target_board || '', relay_recipient:data.settings.relay_recipient || '', route_secret: '', normal: data.settings.normal.frequency_hz ? data.settings.normal : emptyProfile(),
          transfer: data.settings.transfer.frequency_hz ? data.settings.transfer : emptyProfile() };
        frequencies = {normal: cfg.normal.frequency_hz ? String(cfg.normal.frequency_hz / 1000000) : '', transfer: cfg.transfer.frequency_hz ? String(cfg.transfer.frequency_hz / 1000000) : '',target_normal:cfg.target_normal?.frequency_hz ? String(cfg.target_normal.frequency_hz / 1000000):''};
        editing = !cfg.bridge; loaded = true;
      }
    } catch (e) { if (destination === target && open) err = e.message; }
  }
  $effect(() => {
    const destination = station;
    if (!showing || !destination) return;
    // Only opening or changing station starts a new read. Editing fields must
    // not make this effect reload the saved settings.
    const requestGeneration = ++generation;
    untrack(() => {
      showRouteKey = false; routeKeyNotice = '';
      loaded = false; cfg = null; job = null; firmware = null; imageDetails = null; err = '';
      options = {expected_version:'', replace_existing:false, require_offline:false, exercise_confirm_timeout:false};
      const draft = otaDraft(destination);
      if (draft) { cfg = draft.settings; frequencies = draft.frequencies; editing = true; loaded = true; }
      refresh(destination, !draft, requestGeneration);
    });
    otaRequest('/kiss/available-serial-ports').then(p => { if (destination === target && requestGeneration === generation) ports = p; }).catch(() => {});
    const timer = setInterval(() => refresh(destination, false, requestGeneration), 1500);
    return () => {
      clearInterval(timer);
      untrack(() => {
        if (cfg && editing) rememberOtaDraft(destination, $state.snapshot(cfg), $state.snapshot(frequencies));
      });
      ++generation;
    };
  });
  function generateRouteKey() {
    if (busy || running || !cfg?.via) return;
    if ((cfg.has_route_secret || cfg.route_secret) && !window.confirm('Generate a different route key for initial provisioning? For a firmware update of an existing route, keep its existing key. Saving a new key here does not send it to any device. The relay and target must be provisioned with that same key before this route can be used.')) return;
    err = ''; routeKeyNotice = '';
    try {
      cfg.route_secret = generateRouteSecret();
      showRouteKey = false;
      routeKeyNotice = 'Initial provisioning key generated. Before deploying this route, provision the relay and target with this same key and save it in Graywolf. No device has been changed.';
    } catch (e) { err = e.message; }
  }
  async function copyRouteKey() {
    if (busy || running || !cfg?.route_secret) return;
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(cfg.route_secret);
      routeKeyNotice = 'Route key copied.';
    } catch {
      showRouteKey = true;
      await tick();
      document.getElementById('ota-route-key')?.select();
      routeKeyNotice = 'Press Ctrl+C to copy the selected route key.';
    }
  }
  async function cancelSettings() {
    discardOtaDraft(target);
    await refresh(target, true);
    editing = false;
  }
  async function save() {
    busy = true; err = '';
    try {
      const body = { ...cfg, normal: {...cfg.normal, frequency_hz: frequencyHz(frequencies.normal)},
        transfer: {...cfg.transfer, frequency_hz: frequencyHz(frequencies.transfer)} };
      body.target_normal=cfg.target_normal?{...cfg.target_normal,frequency_hz:frequencyHz(frequencies.target_normal)}:null;
      cfg = { ...await otaRequest(`${base}/settings`, {method: 'PUT', body}), route_secret: '' };
      discardOtaDraft(target); editing = false; showRouteKey = false; routeKeyNotice = '';
    }
    catch (e) { err = e.message; } finally { busy = false; }
  }
  function restoreFirmwareInput(input) {
    if (!firmware || !input || input.files?.length) return;
    try { const transfer=new DataTransfer(); transfer.items.add(firmware); input.files=transfer.files; }
    catch { /* The selected File stays authoritative even without DataTransfer. */ }
  }
  function removeFirmware() {
    firmware=null;imageDetails=null;err='';
    if(imageInput)imageInput.value='';
  }
  async function selectFirmware(event) {
    const file = event.currentTarget.files?.[0] || null;
    if (!file) { restoreFirmwareInput(event.currentTarget); return; }
    firmware = file; imageDetails = null; err = '';
    try {
      const details = await otaImageDetails(file);
      if (firmware === file) imageDetails = details;
    } catch (e) { if (firmware === file) err = e.message; }
  }
  async function checkPreparation() {
    if (!otaBoard(cfg?.target_board) || !firmware || !imageDetails || !options.expected_version.trim() || running || !ready) return;
    if (!window.confirm(`Check the route to ${target} using controller USB ${cfg.bridge}? Controller / relay service: ${cfg.normal.frequency_hz / 1000000} MHz. Target service: ${(cfg.target_normal || cfg.normal).frequency_hz / 1000000} MHz. Graywolf will configure the selected route roles with signed commands if necessary. These roles are retained for later updates. The image is uploaded to Graywolf for local checks only; no firmware blocks or activation will be sent. ${separateService ? 'Coordination firmware v1 is required. The relay temporarily joins the target; authenticated closure then restores its own service.' : 'After the HMAC check, allow at least 65 seconds for session release and another normal-state check.'} The relay is unavailable for APRS traffic during preparation as well as during the update. Existing image checkpoints are preserved.`)) return;
    busy = true; err = '';
    try { job = await otaRequest(otaPreflightPath(base, options), {method:'POST',body:firmware,binary:true}); }
    catch(e){err=e.message;}finally{busy=false;}
  }
  async function start() {
    if (!otaBoard(cfg?.target_board) || !firmware || !imageDetails || !options.expected_version.trim() || running || !ready) return;
    const mode = options.exercise_confirm_timeout ? 'VALIDATION: withhold confirmation for 190 seconds, observe automatic rollback, reactivate and confirm.' : 'Activate and immediately confirm the new firmware.';
    const replacement = options.replace_existing ? 'Discard the inactive candidate and its resume point first.' : 'Keep the existing resume point.';
    if (!window.confirm(`Update ${target}, signed identity ${signedTarget}, controller ${controller}, target board ${otaBoard(cfg.target_board).label}, with ${firmware.name} (${options.expected_version.trim()})?\n${imageDetails.sha256 || 'SHA-256 will be calculated by Graywolf after upload.'}\n128-byte blocks; uncompressed image.\nController USB: ${cfg.bridge}. Controller / relay service: ${cfg.normal.frequency_hz / 1000000} MHz; target service: ${(cfg.target_normal || cfg.normal).frequency_hz / 1000000} MHz; transfer: ${cfg.transfer.frequency_hz / 1000000} MHz. Relay: ${cfg.via || 'direct'}; RF recipient: ${cfg.relay_recipient || cfg.via || 'none'}.\nGraywolf prepares and rechecks this route before sending firmware. Route roles are retained for subsequent updates. The relay is unavailable for APRS traffic during the operation.\n${mode}\n${replacement}\n${options.require_offline ? 'Require target Wi-Fi off and no active IP interface.' : 'Target offline check not requested.'}`)) return;
    busy = true; err = '';
    try { job = await otaRequest(otaStartPath(base, options), { method: 'POST', body: firmware, binary: true }); }
    catch (e) { err = e.message; } finally { busy = false; }
  }
  async function pause() {
    busy = true; err = '';
    try { job = await otaRequest(`${base}/pause`, {method: 'POST'}); }
    catch (e) { err = e.message; } finally { busy = false; }
  }
</script>

<Modal class="radio-ota-modal" bind:open onInteractOutside={(event) => event.preventDefault()}>
  <Modal.Header>
    <h3>Firmware update over LoRa · {target}</h3>
    <Modal.Close aria-label="Close firmware update"><Icon name="x" size="lg" /></Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="ota">
      <p>Firmware transfer is included in Graywolf. No Python or external scripts are required.</p>
      <p>Controller: {controller || 'loading…'} · RF recipient: {target} · Signed station: {signedTarget || 'loading…'}. The installed control key is used.</p>
      {#if err}<p role="alert" class="error">{err}</p>{/if}
      {#if loaded && !ready}<p role="alert" class="error">{engineError}</p>{/if}
      {#if cfg}
        <div class="settings-heading">
          <span>Radio bridge: {cfg.bridge || 'not configured'} · {cfg.via ? `via ${cfg.via}` : 'direct'}</span>

        </div>
        {#if editing}
          <Button variant="secondary" size="sm" disabled={running || busy} onclick={cancelSettings}>Cancel settings changes</Button>
          <fieldset disabled={running || busy}>
            <legend>Save once for this station</legend>
            <label for="ota-target-board">Target board (required)</label>
            <select id="ota-target-board" bind:value={cfg.target_board} required>
              <option value="" disabled>Select the target board</option>
              {#each otaBoards as board}<option value={board.value}>{board.label}</option>{/each}
            </select>
            {#if otaBoard(cfg.target_board)}<p>{otaBoard(cfg.target_board).validation}</p>{/if}
            <p>The target model is independent of the controller connected by USB. Select the exact OTA firmware variant; a chip-family check cannot distinguish Heltec from QRP Labs.</p>
            <label for="ota-bridge">Controller USB port on the Graywolf host</label>
            <input id="ota-bridge" list="ota-ports" bind:value={cfg.bridge} placeholder="COM4 or /dev/serial/by-id/…" />
            <datalist id="ota-ports">{#each bridgePorts as port}<option value={port.path}>{port.description || port.path}</option>{/each}</datalist>
            <label for="ota-via">Relay Callsign-SSID (empty for direct)</label>
            <Input id="ota-via" bind:value={cfg.via} maxlength={8} />
            {#if cfg.via}
              <label for="ota-relay-recipient">Relay RF recipient (blank = relay Callsign-SSID)</label>
              <Input id="ota-relay-recipient" bind:value={cfg.relay_recipient} maxlength={9} />
              <p>Use the relay’s on-air tactical name here if it differs. Its authenticated-control key must already exist in Graywolf with the relay Callsign-SSID as signed identity.</p>
              <label for="ota-route-key">Shared route key {cfg.has_route_secret ? '(saved; leave blank to keep)' : ''}</label>
              <input id="ota-route-key" type={showRouteKey ? 'text' : 'password'} bind:value={cfg.route_secret} autocomplete="new-password" spellcheck="false" />
              <div class="buttons">
                <Button variant="secondary" size="sm" disabled={running || busy} onclick={generateRouteKey}>Generate route key (initial setup)</Button>
                <Button variant="secondary" size="sm" disabled={running || busy || !cfg.route_secret} onclick={() => showRouteKey = !showRouteKey}>{showRouteKey ? 'Hide route key' : 'Show route key'}</Button>
                <Button variant="secondary" size="sm" disabled={running || busy || !cfg.route_secret} onclick={copyRouteKey}>Copy route key</Button>
              </div>
              {#if routeKeyNotice}<p role="status">{routeKeyNotice}</p>{/if}
              <p><strong>Firmware update on an existing route:</strong> reuse the key already installed in the relay and target. Enter it once in Graywolf, or leave this field blank to keep the saved key. Do not generate a new key for each update.</p>
              <p><strong>Initial setup of a new route:</strong> Generate creates a 43-character key. Provision that same key in the relay and target before deploying them, and save it in Graywolf. Generating or saving here does not send the key over radio or configure any device.</p>
            {/if}
            <label><input type="checkbox" checked={!!cfg.target_normal} onchange={e=>{cfg.target_normal=e.currentTarget.checked?emptyProfile():null;frequencies.target_normal='';}} disabled={running || busy} />Target uses a different service profile</label>
            <div class="profiles">
              {#each [['normal', 'Controller / relay service profile'], ...(cfg.target_normal ? [['target_normal','Target service profile']]:[]), ['transfer', 'Temporary transfer profile']] as [name, title]}
                <fieldset><legend>{title}</legend>
                  <label for={`ota-${name}-frequency`}>Frequency (MHz)</label>
                  <input id={`ota-${name}-frequency`} type="text" inputmode="decimal" bind:value={frequencies[name]} placeholder="433.775" required />
                  <label for={`ota-${name}-bandwidth`}>Bandwidth (kHz)</label>
                  <select id={`ota-${name}-bandwidth`} bind:value={cfg[name].bandwidth_hz}>
                    {#each [7800,10400,15600,20800,31250,41700,62500,125000,250000,500000] as hz}<option value={hz}>{hz / 1000}</option>{/each}
                  </select>
                  <label for={`ota-${name}-sf`}>Spreading factor</label>
                  <select id={`ota-${name}-sf`} bind:value={cfg[name].spreading_factor}>
                    {#each [7,8,9,10,11,12] as sf}<option value={sf}>SF{sf}</option>{/each}
                  </select>
                  <label for={`ota-${name}-cr`}>Coding rate</label>
                  <select id={`ota-${name}-cr`} bind:value={cfg[name].coding_rate}>
                    {#each [5,6,7,8] as cr}<option value={cr}>4/{cr}</option>{/each}
                  </select>
                  <label for={`ota-${name}-power`}>Power (dBm)</label>
                  <input id={`ota-${name}-power`} type="number" min="2" max="20" bind:value={cfg[name].power_dbm} required />
                </fieldset>
              {/each}
            </div>
            <p>Existing keys are reused. Different service profiles require coordination firmware v1 on the controller, relay and target. The relay temporarily joins the target and returns to its own service after the operation. Permanent radio settings are not changed. Select the controller USB port, never the target port.</p>
            <Button onclick={save} disabled={busy || !otaBoard(cfg.target_board)}>Save OTA settings</Button>
          </fieldset>
        {/if}
        {#if !editing}
          <table class="saved-profiles" aria-label="Saved radio settings">
            <thead><tr><th scope="col">Profile</th><th scope="col">Frequency</th><th scope="col">LoRa</th></tr></thead>
            <tbody>
              {#each [['normal', 'Controller / relay service'], ...(cfg.target_normal ? [['target_normal','Target service']]:[]), ['transfer', 'Transfer']] as [name, title]}
                <tr><th scope="row">{title}</th><td>{cfg[name].frequency_hz / 1000000} MHz</td>
                  <td>BW{cfg[name].bandwidth_hz / 1000} · SF{cfg[name].spreading_factor} · 4/{cfg[name].coding_rate} · {cfg[name].power_dbm} dBm</td></tr>
              {/each}
            </tbody>
          </table>
          <Button variant="secondary" disabled={running || busy} onclick={() => editing = true}>View / edit radio settings</Button>
        {/if}
          {#if !cfg.target_board}<p role="status">Select the target board in radio settings before checking or updating.</p>{/if}
          {#if editing}<p role="status">Save OTA settings before checking or updating. You can select the firmware below while editing.</p>{/if}
          <label for="ota-image">Application firmware (.bin)</label>
          <input id="ota-image" type="file" accept=".bin" bind:this={imageInput} disabled={running || busy} onchange={selectFirmware} oncancel={(event)=>restoreFirmwareInput(event.currentTarget)} />
          {#if firmware}
            <label for="ota-selected-filename">Selected firmware filename (select text to copy)</label>
            <input id="ota-selected-filename" type="text" readonly value={firmware.name} spellcheck="false" />
            <p>Copy text from this field without reopening the file picker. Enter the radio firmware version below, without the filename prefix or .bin extension.</p>
            <Button variant="secondary" size="sm" disabled={running || busy} onclick={removeFirmware}>Remove selected firmware</Button>
          {/if}
          <p>Target board: {otaBoard(cfg.target_board)?.label || 'not selected'}. Select the matching OTA-compatible application image, not a merged flash image.</p>
          {#if !editing}<p>{otaBoard(cfg.target_board)?.validation}</p>{/if}
          <p>Laboratory reference: about 63 minutes to transfer and verify 1.78 MB with 160-byte blocks. A whole image at 128 bytes has not yet been timed. The relay is unavailable for APRS traffic during the operation. The roughly 71-minute laboratory cycle included deliberate rollback; it is not a guaranteed normal-update duration.</p>
          {#if imageDetails}<p>Image descriptor: {imageDetails.descriptor} · {imageDetails.size} bytes · 128-byte blocks · uncompressed</p><p class="hash">SHA-256: {imageDetails.sha256 || 'calculated by Graywolf after upload (browser has no WebCrypto)'}</p>{/if}
          <fieldset disabled={running || busy}>
            <legend>This update only</legend>
            <label for="ota-expected-version">Expected firmware version</label>
            <input id="ota-expected-version" type="text" maxlength="40" bind:value={options.expected_version} placeholder="Version reported by the candidate, e.g. 4.0.2RXT-OTA-B" required />
            <p>Use the firmware version announced by the candidate after reboot. The image descriptor may contain only the ESP-IDF version.</p>
            <label><input type="checkbox" bind:checked={options.replace_existing} /> Discard the previous inactive candidate</label>
            <p>Destructive: removes its saved resume point. Leave unchecked to resume the same image.</p>
            <label><input type="checkbox" bind:checked={options.require_offline} /> Require target offline (Wi-Fi off, no active IP interface)</label>
            <label><input type="checkbox" bind:checked={options.exercise_confirm_timeout} /> Validation: test automatic rollback before confirming</label>
            <p>Normal updates confirm immediately. Validation deliberately withholds confirmation for 190 seconds, checks rollback, then reactivates and confirms the candidate.</p>
          </fieldset>
          {#if job}
            {#if !job.options?.preflight_only}<progress max="100" value={otaProgress(job)} aria-label="Firmware update progress"></progress>{/if}
            <p aria-live="polite">{otaPhase(job)}{job.options?.preflight_only ? '' : ` · ${otaProgress(job)}%`}</p>
            <p>Target board: {otaBoard(job.target_board)?.label || 'not recorded'}</p>
            <p>Attempt: {job.options?.preflight_only ? 'route check only' : job.options?.exercise_confirm_timeout ? 'rollback validation' : 'normal'} · {job.block_size || 128}-byte blocks · expected {job.expected_version || 'not recorded'}</p>
            {#if job.roles_changed?.length}<p>Route roles saved on: {job.roles_changed.join(', ')}. These changes remain in the devices.</p>{/if}
            {#if job.route_proof}<p>Authenticated initial firmware: {job.route_proof.version} · active 0x{job.route_proof.running.toString(16)} · boot 0x{job.route_proof.boot.toString(16)}</p>{/if}
            {#if job.final}<p>Installed: {job.final.version} · active 0x{job.final.running.toString(16)} · boot 0x{job.final.boot.toString(16)} · pending: {String(job.final.pending)}</p>{/if}
            {#if job.sha256}<p class="hash">Image SHA-256: {job.sha256}</p>{/if}
          {/if}
          <div class="buttons">
            <Button variant="secondary" onclick={checkPreparation} disabled={editing || !cfg.bridge || !otaBoard(cfg.target_board) || !firmware || !imageDetails || !options.expected_version.trim() || !ready || busy || running}>Check preparation</Button>
            <Button onclick={start} disabled={editing || !cfg.bridge || !otaBoard(cfg.target_board) || !firmware || !imageDetails || !options.expected_version.trim() || !ready || busy || running}>Update firmware</Button>
            {#if running}<Button variant="secondary" onclick={pause} disabled={!job.can_pause || busy}>Pause</Button>{/if}
          </div>
          <p>A route check preserves the inactive candidate and sends no firmware blocks. {separateService ? 'The temporary rendezvous requires coordination firmware v1, including in the candidate image. Hardware validation is still pending.' : 'Session release adds at least 65 seconds to the radio exchanges.'} The relay is unavailable for APRS traffic during preparation. Selected route roles remain saved on the devices. Every update rechecks the route.</p>
          <p>Closing this window leaves the transfer running on the Graywolf host. Keep Graywolf and its radio bridge powered on.</p>
      {:else}<p>Loading station settings…</p>{/if}
    </div>
  </Modal.Body>
  <Modal.Footer><Button variant="secondary" onclick={() => open = false}>Close</Button></Modal.Footer>
</Modal>

<style>
 :global(.radio-ota-modal) {width:min(760px,96vw);max-height:calc(100dvh - 24px)}
 :global(.radio-ota-modal > .modal-header) {gap:12px;padding:12px 16px}
 :global(.radio-ota-modal > .modal-header h3) {margin:0;font-size:14px}
 :global(.radio-ota-modal > .modal-body) {padding:16px;overflow-y:auto;min-height:0}
 :global(.radio-ota-modal > .modal-footer) {padding:10px 16px}

 .ota {display:flex;flex-direction:column;gap:10px;max-width:700px}
 .ota p {font-size:12px;margin:0;color:var(--color-text-muted)}
 .ota .error {color:var(--color-danger,#cf4040)}
 fieldset {display:flex;flex-direction:column;gap:6px;border:1px solid var(--color-border);padding:12px;min-width:0}
 legend,label {font-size:12px} input,select {padding:7px;background:var(--color-surface);color:var(--color-text);border:1px solid var(--color-border);border-radius:4px;min-width:0}
 .profiles {display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:8px}
 .settings-heading,.buttons {display:flex;align-items:center;gap:10px;flex-wrap:wrap;font-size:12px}
 .saved-profiles {width:100%;border-collapse:collapse;font-size:12px}.saved-profiles th,.saved-profiles td {text-align:left;padding:7px;border-bottom:1px solid var(--color-border)}
 progress {width:100%;height:20px}.hash {overflow-wrap:anywhere}
 @media(max-width:500px){.profiles{grid-template-columns:1fr}}
</style>
