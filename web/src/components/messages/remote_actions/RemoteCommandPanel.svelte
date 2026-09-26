<script>
  import { Button, toast } from '@chrissnell/chonky-ui';
  import RemoteCommandCredentialModal from './RemoteCommandCredentialModal.svelte';
  import { remoteCommandsApi } from '../../../lib/remote_actions/api.js';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';
  import { refreshNow } from '../../../lib/messagesTransport.js';

  let { target, channel = 0 } = $props();
  let modalOpen = $state(false);
  let sending = $state('');
  const credential = $derived(remoteActionsStore.commandCredFor(target));

  const controls = [
    { command: 'EM=ON', label: 'Eco mode on' },
    { command: 'EM=OFF', label: 'Eco mode off' },
    { command: 'TX=ON', label: 'TX on' },
    { command: 'TX=OFF', label: 'TX off', dangerous: true },
    { command: 'COMMIT', label: 'Save config' },
  ];

  async function send(control) {
    if (!credential || sending) return;
    if (control.dangerous && !window.confirm(`Send ${control.command} to ${target}?`)) return;
    sending = control.command;
    try {
      const body = { command: control.command };
      if (channel) body.channel = channel;
      const { data, error } = await remoteCommandsApi.send(target, body);
      if (error) {
        toast(`Command failed: ${error.error ?? error.message ?? error}`, 'error');
        return;
      }
      await remoteActionsStore.loadCommandCreds();
      refreshNow();
      toast(`${control.command} queued . counter ${data?.counter ?? '?'}`, 'success');
    } finally {
      sending = '';
    }
  }
</script>

<section class="remote-control">
  <div class="heading">
    <div>
      <h3>CA2RXU authenticated control</h3>
      {#if credential}
        <p>{credential.name} . counter {credential.last_counter}</p>
      {:else}
        <p>No key installed for {target}</p>
      {/if}
    </div>
    <Button variant="ghost" size="sm" onclick={() => (modalOpen = true)}>
      {credential ? 'Key settings' : 'Install key'}
    </Button>
  </div>
  {#if credential}
    <div class="controls">
      {#each controls as control (control.command)}
        <Button
          variant={control.dangerous ? 'danger' : 'secondary'}
          size="sm"
          disabled={!!sending}
          onclick={() => send(control)}
        >{sending === control.command ? 'Queueing…' : control.label}</Button>
      {/each}
    </div>
    <p class="note">Queued means Graywolf accepted the APRS message. It does not prove that the remote command was applied.</p>
  {/if}
</section>

<RemoteCommandCredentialModal bind:open={modalOpen} {target} {credential} />

<style>
  .remote-control { display: flex; flex-direction: column; gap: 10px; padding: 12px; margin-bottom: 12px; border: 1px solid var(--color-border); border-radius: var(--radius); background: var(--color-surface-raised, var(--color-surface)); }
  .heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
  h3 { margin: 0; font-size: 13px; text-transform: uppercase; letter-spacing: 0.05em; }
  .heading p, .note { margin: 3px 0 0; font-size: 11px; color: var(--color-text-muted); }
  .controls { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 6px; }
</style>
