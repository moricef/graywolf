<script>
  import { onMount } from 'svelte';
  import RadioOtaModal from '../src/components/messages/remote_actions/RadioOtaModal.svelte';
  let open=$state(true);
  // Message-store refreshes replace the conversation object while retaining
  // the same station key. The real Messages drawer passes this getter chain.
  let conversation=$state.raw({key:'DEMO',sequence:0});
  onMount(()=>{
    const refresh=()=>{conversation={key:conversation.key,sequence:conversation.sequence+1};};
    window.refreshRadioOtaConversation=refresh;
    return ()=>{if(window.refreshRadioOtaConversation===refresh)delete window.refreshRadioOtaConversation;};
  });
</script>
<RadioOtaModal bind:open target={conversation.key} credential={{signed_target:'DEMO-15'}} />
