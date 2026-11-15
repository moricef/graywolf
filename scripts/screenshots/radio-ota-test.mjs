// Browser integration using simulated APIs; no Graywolf instance or radio required.
import { chromium } from 'playwright';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
const base = process.env.GW_OTA_TEST_BASE || 'http://127.0.0.1:5187';
const browser = await chromium.launch({headless:true, executablePath:process.env.CHROMIUM || '/usr/bin/chromium', args:['--no-sandbox']});
const context = await browser.newContext({viewport:{width:1150,height:950}});
const page = await context.newPage();
const errors=[]; page.on('pageerror',e=>errors.push(e.message));
let config={bridge:'',via:'',has_route_secret:false,normal:{frequency_hz:433775000,bandwidth_hz:125000,spreading_factor:12,coding_rate:5,power_dbm:2},transfer:{frequency_hz:434550000,bandwidth_hz:250000,spreading_factor:8,coding_rate:6,power_dbm:3}};
let checks=0;
let job=null, uploads=0, polls=0, saves=0, savedRouteKey='';
await page.route('**/api/**',async route=>{
 const req=route.request(), path=new URL(req.url()).pathname;
 let status=200,data={};
 if (path==='/api/kiss/available-serial-ports') data=[{path:'COM4',description:'Demonstration bridge'}];
 else if (path.endsWith('/settings')) {saves++; const body=req.postDataJSON();assert.equal(body.target_board,'qrp-labs-lightgateway-plus');assert.equal(body.bridge,'COM4');assert.equal(body.normal.frequency_hz,433775000);assert.equal(body.normal.coding_rate,5);savedRouteKey=body.route_secret || savedRouteKey;config={...body,has_route_secret:!!savedRouteKey};delete config.route_secret;data=config;}
 else if(path.endsWith('/preflight')) {checks++;assert.equal(new URL(req.url()).searchParams.get('replace_existing'),null);job={id:'preflight',state:'running',phase:'releasing_session',written:0,total:512,can_pause:true,options:{preflight_only:true}};status=202;data=job;}
 else if(path.endsWith('/firmware')) {uploads++; const query=new URL(req.url()).searchParams; assert.equal(query.get('expected_version'),'4.0.2RXT-OTA-B'); for(const name of ['replace_existing','require_offline','exercise_confirm_timeout'])assert.equal(query.get(name),'true');assert.equal(req.headers()['content-type'],'application/octet-stream');job={id:'simulated',state:'running',phase:'sending',written:256,total:512,can_pause:true};status=202;data=job;}
 else if(path.endsWith('/pause')) {job={...job,state:'paused',can_pause:false};data=job;}
 else if(path==='/api/radio-ota/DEMO') {polls++;data={controller:'F4MLV-2',signed_target:'DEMO-15',settings:config,job,engine_ready:true,engine_error:''};}
 else throw new Error('Unexpected API request '+path);
 await route.fulfill({status,json:data});
});
try {
 await page.goto(base+'/tests/radio-ota.html');
 await page.getByText('Firmware transfer is included in Graywolf. No Python or external scripts are required.',{exact:true}).waitFor();
 assert.equal(await page.getByLabel('Target board (required)',{exact:true}).inputValue(),'');
 await page.getByLabel('Target board (required)',{exact:true}).selectOption('qrp-labs-lightgateway-plus');
 await page.getByText('Software tests only; hardware validation still required.',{exact:true}).waitFor();
 await page.getByLabel('Controller USB port on the Graywolf host').fill('COM4');
 await page.getByLabel('Frequency (MHz)',{exact:true}).first().fill('433.775');
 await page.getByLabel('Coding rate',{exact:true}).first().selectOption('5');
 await page.getByLabel('Relay Callsign-SSID (empty for direct)',{exact:true}).fill('DEMO-10');
 await page.getByRole('button',{name:'Generate route key (initial setup)',exact:true}).click();
 const routeKey=page.locator('#ota-route-key');
 let generatedKey=await routeKey.inputValue();assert.match(generatedKey,/^[A-Za-z0-9_-]{43}$/);
 assert.equal(Buffer.from(generatedKey,'base64url').length,32);assert.equal(saves,0);assert.equal(uploads,0);
 page.once('dialog',d=>d.dismiss());
 await page.getByRole('button',{name:'Generate route key (initial setup)',exact:true}).click();
 assert.equal(await routeKey.inputValue(),generatedKey);
 page.once('dialog',d=>d.accept());
 await page.getByRole('button',{name:'Generate route key (initial setup)',exact:true}).click();
 const nextKey=await routeKey.inputValue();assert.notEqual(nextKey,generatedKey);generatedKey=nextKey;
 // A plain HTTP LAN origin may not expose clipboard.writeText. Manual copy remains usable.
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:undefined}));
 await page.getByRole('button',{name:'Copy route key',exact:true}).click();
 assert.equal(await routeKey.getAttribute('type'),'text');
 assert.deepEqual(await routeKey.evaluate(el=>[el.selectionStart,el.selectionEnd]),[0,43]);
 await page.getByRole('button',{name:'Hide route key',exact:true}).click();
 assert.equal(await routeKey.getAttribute('type'),'password');

 const frequency = page.getByLabel('Frequency (MHz)',{exact:true}).first();
 await frequency.fill('434,100');
 // An outside click must leave the form open and preserve its draft.
 await page.mouse.click(8,8);
 assert.equal(await frequency.inputValue(),'434,100');
 const other = await page.context().newPage();await other.goto('about:blank');await other.bringToFront();
 await page.waitForTimeout(1700);await page.bringToFront();await other.close();
 assert.equal(await frequency.inputValue(),'434,100');
 assert.equal(await routeKey.inputValue(),generatedKey);
 await page.getByRole('button',{name:'Close',exact:true}).click();
 // Reopen through a remount; drafts must survive in the same SPA session.
 await page.evaluate(() => window.reopenRadioOtaPreview());
 const reopened=page.getByLabel('Frequency (MHz)',{exact:true}).first();await reopened.waitFor();
 assert.equal(await reopened.inputValue(),'434,100');
 assert.equal(await page.locator('#ota-route-key').inputValue(),generatedKey);
 await reopened.fill('433.775');
 await page.getByLabel('Target uses a different service profile',{exact:true}).check();
 await page.locator('#ota-target_normal-frequency').fill('434.100');
 await page.locator('#ota-target_normal-bandwidth').selectOption('125000');
 await page.locator('#ota-target_normal-sf').selectOption('12');
 await page.locator('#ota-target_normal-cr').selectOption('5');
 await page.locator('#ota-target_normal-power').fill('20');
 await page.getByRole('button',{name:'Save OTA settings',exact:true}).click();
 await page.getByLabel('Application firmware (.bin)').waitFor();
 await page.getByRole('table',{name:'Saved radio settings'}).waitFor();
 assert.equal(savedRouteKey,generatedKey);assert.equal(saves,1);
 assert.equal(config.target_normal.frequency_hz,434100000);
 assert.equal(config.target_normal.spreading_factor,12);
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).click();
 assert.equal(await page.getByLabel('Frequency (MHz)',{exact:true}).first().inputValue(),'433.775');
 assert.equal(await page.getByLabel('Spreading factor',{exact:true}).first().inputValue(),'12');
 assert.equal(Number(await page.locator('#ota-target_normal-frequency').inputValue()),434.1);
 assert.equal(await page.locator('#ota-route-key').inputValue(),'');
 page.once('dialog',d=>d.dismiss());
 await page.getByRole('button',{name:'Generate route key (initial setup)',exact:true}).click();
 assert.equal(await page.locator('#ota-route-key').inputValue(),'');assert.equal(savedRouteKey,generatedKey);

 await page.getByLabel('Frequency (MHz)',{exact:true}).first().fill('434.123');
 await page.getByRole('button',{name:'Cancel settings changes',exact:true}).click();
 await page.getByLabel('Application firmware (.bin)').waitFor();
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).click();
 assert.equal(await page.getByLabel('Frequency (MHz)',{exact:true}).first().inputValue(),'433.775');
 await page.getByRole('button',{name:'Cancel settings changes',exact:true}).click();
 await page.getByLabel('Application firmware (.bin)').waitFor();
 await mkdir('scratch/ota-ui',{recursive:true});
 await page.screenshot({path:'scratch/ota-ui/graywolf-ota-ready.png',fullPage:true});
 await page.getByLabel('Expected firmware version',{exact:true}).fill('4.0.2RXT-OTA-B');
 await page.getByLabel('Application firmware (.bin)').setInputFiles({name:'firmware.bin',mimeType:'application/octet-stream',buffer:Buffer.alloc(512)});
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files[0]?.name),'firmware.bin');
 const retainedLabels=['Discard the previous inactive candidate','Require target offline (Wi-Fi off, no active IP interface)','Validation: test automatic rollback before confirming'];
 for(const label of retainedLabels)await page.getByLabel(label,{exact:true}).check();
 for(let i=0;i<3;i++){await page.evaluate(()=>window.refreshRadioOtaConversation());await page.waitForTimeout(120);}
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files[0]?.name),'firmware.bin','conversation refresh reset the file');
 assert.equal(await page.getByLabel('Expected firmware version',{exact:true}).inputValue(),'4.0.2RXT-OTA-B');
 for(const label of retainedLabels)assert.equal(await page.getByLabel(label,{exact:true}).isChecked(),true,'conversation refresh reset '+label);
 // The filename has a separate selectable field. Clicking/selecting it must
 // not reopen the native picker or clear File/flags/version.
 const filename=page.getByLabel('Selected firmware filename (select text to copy)',{exact:true});
 assert.equal(await filename.inputValue(),'firmware.bin');
 await filename.click();await filename.press('Control+A');
 assert.deepEqual(await filename.evaluate(el=>[el.selectionStart,el.selectionEnd]),[0,12]);
 assert.equal(await page.getByLabel('Expected firmware version',{exact:true}).inputValue(),'4.0.2RXT-OTA-B');
 // Some browsers report an empty change when a picker is cancelled.
 await page.getByLabel('Application firmware (.bin)').setInputFiles([]);
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files[0]?.name),'firmware.bin');
 assert.equal(await filename.inputValue(),'firmware.bin');
 for(const label of retainedLabels)assert.equal(await page.getByLabel(label,{exact:true}).isChecked(),true);
 // Restore defaults for the subsequent per-attempt options test.
 for(const label of retainedLabels)await page.getByLabel(label,{exact:true}).uncheck();
 await page.waitForTimeout(1800);
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files[0]?.name),'firmware.bin');
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).click();
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files[0]?.name),'firmware.bin');
 await page.getByRole('button',{name:'Cancel settings changes',exact:true}).click();
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files[0]?.name),'firmware.bin');

 page.once('dialog',d=>{assert.match(d.message(),/no firmware blocks or activation/);d.accept();});
 await page.getByRole('button',{name:'Check preparation',exact:true}).click();
 await page.getByText('Releasing the check session without discarding its checkpoint',{exact:true}).waitFor();
 assert.equal(checks,1);assert.equal(uploads,0);
 assert.equal(await page.getByRole('button',{name:'Update firmware',exact:true}).isDisabled(),true);
 job={...job,state:'ready',phase:'ready',can_pause:false,route_authenticated:true,session_released:true};
 await page.getByText('Route checked; no firmware transferred',{exact:true}).waitFor();
 assert.equal(await page.getByRole('progressbar').count(),0);

 page.on('dialog',d=>{assert.match(d.message(),/VALIDATION/);assert.match(d.message(),/128-byte blocks/);assert.match(d.message(),/Discard the inactive candidate/);d.accept();});
 for(const label of ['Discard the previous inactive candidate','Require target offline (Wi-Fi off, no active IP interface)','Validation: test automatic rollback before confirming']){const input=page.getByLabel(label,{exact:true});assert.equal(await input.isChecked(),false);await input.check();}
 await page.getByLabel('Application firmware (.bin)').setInputFiles({name:'firmware.bin',mimeType:'application/octet-stream',buffer:(()=>{const b=Buffer.alloc(512);b.write('4.0.2RXT-OTA-B',48);return b;})()});
 await page.getByRole('button',{name:'Update firmware',exact:true}).click();
 await page.getByText('Sending firmware · 50%',{exact:true}).waitFor();assert.equal(uploads,1);
 await page.getByRole('button',{name:'Update firmware',exact:true}).isDisabled().then(v=>assert.equal(v,true));
 await page.getByRole('button',{name:'Pause',exact:true}).click();
 await page.getByText(/Paused\. Allow/).waitFor();
 job={...job,state:'running',phase:'confirming',written:512,can_pause:false};
 await page.getByText('Starting and confirming new firmware · 99%',{exact:true}).waitFor();
 assert.equal(await page.getByRole('button',{name:'Pause',exact:true}).isDisabled(),true);
 job={...job,state:'running',phase:'waiting_rollback',written:512,can_pause:false};
 await page.getByText('Validation: withholding confirmation for rollback · 99%',{exact:true}).waitFor();
 job={...job,phase:'checking_installation'};
 await page.getByText('Reading back installed version, partition and SHA-256 · 99%',{exact:true}).waitFor();
 job={...job,state:'succeeded',written:512,post_install_verified:true};
 await page.getByText('Firmware updated, confirmed and installation verified · 100%',{exact:true}).waitFor();
 await page.screenshot({path:'scratch/ota-ui/graywolf-ota-confirmed.png',fullPage:true});
 // Legacy saved settings have no target_board: preserve their bridge/relay and
 // expose the firmware selector without trapping the operator in the editor.
 await page.getByRole('button',{name:'Close firmware update',exact:true}).click();
 config={...config,bridge:'/dev/ttyACM0',via:'F4MLV-10',target_board:''};job=null;
 await page.reload();
 await page.getByLabel('Application firmware (.bin)').waitFor();
 await page.getByText('Radio bridge: /dev/ttyACM0 · via F4MLV-10',{exact:true}).waitFor();
 await page.getByText('Select the target board in radio settings before checking or updating.',{exact:true}).waitFor();
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).click();
 await page.getByLabel('Target board (required)',{exact:true}).waitFor();
 assert.equal(await page.getByLabel('Application firmware (.bin)').count(),1);
 await page.getByRole('button',{name:'Cancel settings changes',exact:true}).click();
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).waitFor();
 assert.equal(await page.getByLabel('Target board (required)',{exact:true}).count(),0);
 await page.setViewportSize({width:800,height:480});
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).click();
 const close=page.getByRole('button',{name:'Close firmware update',exact:true});
 const bounds=await close.boundingBox();assert.ok(bounds.y>=0&&bounds.y+bounds.height<=480);
 await close.click();await page.getByRole('dialog').waitFor({state:'hidden'});assert.equal(await page.getByRole('dialog').count(),0);
 // A station without saved OTA settings must also remain closable, and Cancel
 // must return to the main view even though its configuration is incomplete.
 config={...config,bridge:'',via:'',target_board:''};
 await page.reload();
 await page.getByLabel('Application firmware (.bin)').setInputFiles({name:'remove-me.bin',mimeType:'application/octet-stream',buffer:Buffer.alloc(512)});
 await page.getByRole('button',{name:'Remove selected firmware',exact:true}).click();
 assert.equal(await page.getByLabel('Application firmware (.bin)').evaluate(el=>el.files.length),0);
 assert.equal(await page.getByLabel('Selected firmware filename (select text to copy)',{exact:true}).count(),0);
 await page.getByLabel('Target board (required)',{exact:true}).waitFor();
 assert.equal(await page.getByLabel('Application firmware (.bin)').count(),1);
 await page.getByRole('button',{name:'Cancel settings changes',exact:true}).click();
 await page.getByRole('button',{name:'View / edit radio settings',exact:true}).waitFor();
 await page.getByRole('button',{name:'Close firmware update',exact:true}).click();
 await page.getByRole('dialog').waitFor({state:'hidden'});assert.equal(await page.getByRole('dialog').count(),0);
 assert.ok(polls>=3);assert.deepEqual(errors,[]);
 assert.equal(await page.evaluate(()=>Object.keys(localStorage).some(k=>localStorage[k].includes('route_secret'))),false);
 console.log('PASS: parent conversation refresh preserves file, version and checkboxes; selectable filename and cancelled picker;  secure route key generation, copy fallback, explicit replacement, draft retention, saved secret hidden; saved station, binary upload, progress, pause, activation lock, confirmed success; no radio opened');
} finally {await browser.close();}
