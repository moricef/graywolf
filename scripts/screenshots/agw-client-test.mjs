// Browser test with simulated APIs. No live radio settings are changed.
import {chromium} from 'playwright';
import assert from 'node:assert/strict';
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM||'/usr/bin/chromium',args:['--no-sandbox']});
const page=await browser.newPage({viewport:{width:1200,height:1000}});
const errors=[];page.on('pageerror',e=>errors.push(e.message));
let rows=[],saved;
await page.route('**/api/**',async route=>{
 const req=route.request(),path=new URL(req.url()).pathname;
 let data=[];
 if(path==='/api/channels')data=[{id:7,name:'AGW VHF',enabled:true,mode:'aprs',backing:{modem:false,kiss_tnc:true,summary:'external',health:'connected'}}];
 else if(path==='/api/kiss'&&req.method()==='POST') {saved=req.postDataJSON();rows=[{...saved,id:1,enabled:true,state:'connected'}];data=rows[0];}
 else if(path==='/api/kiss')data=rows;
 else if(path==='/api/kiss/1'&&req.method()==='PUT') {saved=req.postDataJSON();rows=[{...saved,id:1,enabled:true,state:'connected'}];data=rows[0];}
 await route.fulfill({json:data});
});
async function choose(label,option){await page.getByLabel(label,{exact:true}).click();await page.getByRole('option',{name:option,exact:true}).click();}
try{
 await page.goto((process.env.GW_AGW_TEST_BASE||'http://127.0.0.1:5188')+'/tests/agw-client.html');
 await page.getByRole('button',{name:/Add/}).click();
 await choose('Type','TCP Client');
 await choose('Protocol','AGWPE (APRS client)');
 assert.equal(await page.getByLabel('Remote Port',{exact:true}).inputValue(),'8000');
 assert.equal(await page.getByLabel('Mode',{exact:true}).isDisabled(),true);
 await page.getByLabel('Remote Host',{exact:true}).fill('127.0.0.1');
 await page.getByLabel('AGWPE radio port',{exact:true}).fill('2');
 await page.getByRole('button',{name:'Create',exact:true}).click();
 await page.getByText('AGWPE Client',{exact:true}).waitFor();
 assert.equal(saved.tcp_protocol,'agwpe');assert.equal(saved.agw_port,1);assert.equal(saved.remote_port,8000);assert.equal(saved.mode,'tnc');assert.equal(saved.allow_connected_mode,false);
 await page.getByRole('button',{name:/Edit/}).click();
 assert.equal(await page.getByLabel('AGWPE radio port',{exact:true}).inputValue(),'2');
 assert.equal(await page.getByLabel('Remote Port',{exact:true}).inputValue(),'8000');
 await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.getByText('AGWPE Client',{exact:true}).waitFor();
 assert.equal(saved.agw_port,1);
 assert.deepEqual(errors,[]);
 console.log('AGWPE client browser test passed: creation, protocol defaults, port numbering, edit roundtrip.');
}finally{await browser.close();}
