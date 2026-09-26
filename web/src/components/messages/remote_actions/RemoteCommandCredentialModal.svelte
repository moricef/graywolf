<script>
  import { Button, Icon, Input, Modal, toast } from '@chrissnell/chonky-ui';
  import { remoteCommandCredsApi } from '../../../lib/remote_actions/api.js';
  import { remoteActionsStore } from '../../../lib/remote_actions/store.svelte.js';

  let { open = $bindable(false), target, credential = null } = $props();

  let name = $state('');
  let secret = $state('');
  let saving = $state(false);
  let err = $state('');
  let prevOpen = false;

  $effect(() => {
    if (open && !prevOpen) {
      name = credential?.name ?? `${target} control`;
      secret = '';
      err = '';
    }
    prevOpen = open;
  });

  const isEdit = $derived(credential?.id != null);

  async function save() {
    err = '';
    if (!name.trim()) {
      err = 'Name required';
      return;
    }
    if (!isEdit && !secret.trim()) {
      err = 'Secret key required';
      return;
    }
    saving = true;
    try {
      const body = { name: name.trim(), target_call: target };
      if (secret.trim()) body.secret_b64url = secret.trim();
      const result = isEdit
        ? await remoteCommandCredsApi.update(credential.id, body)
        : await remoteCommandCredsApi.create(body);
      if (result.error) {
        err = result.error.error ?? result.error.message ?? 'Save failed';
        return;
      }
      await remoteActionsStore.loadCommandCreds();
      toast(isEdit ? 'Control credential updated' : 'Control credential installed', 'success');
      open = false;
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!credential?.id || !window.confirm(`Remove the control key for ${target}?`)) return;
    const { error } = await remoteCommandCredsApi.remove(credential.id);
    if (error) {
      err = error.error ?? error.message ?? 'Delete failed';
      return;
    }
    await remoteActionsStore.loadCommandCreds();
    toast('Control credential removed', 'success');
    open = false;
  }
</script>

<Modal bind:open>
  <Modal.Header>
    <h3 class="modal-title">Authenticated control . {target}</h3>
    <Modal.Close aria-label="Close"><Icon name="x" size="lg" /></Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="form">
      <div class="field">
        <label for="rc-name">Name</label>
        <Input id="rc-name" bind:value={name} />
      </div>
      <div class="field">
        <label for="rc-secret">43-character Base64URL key</label>
        <textarea
          id="rc-secret"
          class="secret"
          rows="2"
          bind:value={secret}
          autocomplete="new-password"
          placeholder={isEdit ? '(leave blank to keep current key)' : 'paste the key installed on the iGate'}
        ></textarea>
        {#if isEdit && secret.trim()}
          <p class="warning">Replacing the key resets Graywolf's sender counter. Install the same new key on the iGate.</p>
        {/if}
      </div>
      {#if err}<p class="err" role="alert">{err}</p>{/if}
    </div>
  </Modal.Body>
  <Modal.Footer>
    {#if isEdit}<Button variant="danger" onclick={remove}>Remove</Button>{/if}
    <span class="spacer"></span>
    <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" disabled={saving} onclick={save}>{isEdit ? 'Save' : 'Install'}</Button>
  </Modal.Footer>
</Modal>

<style>
  .modal-title { margin: 0; font-size: 14px; font-weight: 600; }
  .form { display: flex; flex-direction: column; gap: 12px; min-width: min(420px, 80vw); }
  .field { display: flex; flex-direction: column; gap: 4px; }
  .field label { font-size: 11px; font-weight: 700; letter-spacing: 0.5px; text-transform: uppercase; color: var(--color-text-dim); }
  .secret { font-family: var(--font-mono); font-size: 13px; padding: 6px 8px; border: 1px solid var(--color-border); border-radius: var(--radius); background: var(--color-surface); color: var(--color-text); resize: vertical; }
  .warning { margin: 0; color: var(--color-warning, #d89b2b); font-size: 12px; }
  .err { margin: 0; color: var(--color-danger); font-size: 0.875rem; }
  .spacer { flex: 1; }
</style>
