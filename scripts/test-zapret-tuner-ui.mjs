// Real Chromium + unmodified LuCI rpc.js, with deterministic network/RPC states.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {s6Root,s6ReadSources,s6InitialState,s6OpenFixture,s6SetTheme,s6Themes} from './fixtures/s6-luci-harness.mjs';

const {chromium}=await import(process.env.NADKOP_PLAYWRIGHT_MODULE||'/path/to/playwright-env/node_modules/playwright/index.mjs');
const output=path.resolve(process.env.TUNER_UI_OUTPUT||path.join(s6Root,'build/zapret-tuner/ui'));
await fs.mkdir(output,{recursive:true});
const sources=await s6ReadSources();
const browser=await chromium.launch({executablePath:process.env.NADKOP_CHROMIUM||'/run/current-system/sw/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
const results=[];
const args=['--filter-tcp=80,443','--dpi-desync=multisplit','--dpi-desync-split-pos=2'];
const job=(status='running')=>({id:'a'.repeat(32),status,revision:'s6-r1',wan:'eth0',targets:['blocked.example'],protocols:['https'],
 strategies:[{id:'multisplit',title:'Splitting',args}],startedAt:1793740000,completed:status==='completed'?5:1,total:14,
 restored:!['running','restoring','queued','recovery_failed'].includes(status),winner:status==='completed'?'multisplit':'',message:{running:'Checking without a strategy',restoring:'Removing temporary resources',completed:'Strategy passed two checks',unneeded:'Sites are reachable without a strategy',no_strategy:'No strategy passed the check',recovery_failed:'Failed to remove temporary resources',cancelled:'Check cancelled'}[status]||status,
 results:[{host:'blocked.example',protocol:'https',stage:'baseline',round:1,ok:false,code:'timeout',delayMs:4000,queued:false},
 {host:'blocked.example',protocol:'https',stage:'strategy',strategy:'multisplit',round:1,ok:true,code:'ok',delayMs:45,httpStatus:200,queued:true}]});
const b=(f,name)=>f.page.getByRole('button',{name,exact:true});
const tick=f=>f.page.evaluate(()=>window.s6poll[1]());
async function test(name,run,initial=null,context){
 const state=s6InitialState();state.config.wanInterface='eth0';state.config.zapretArgs=['--filter-tcp=443','--dpi-desync=fake'];
 const tuner={current:initial,start:0,cancel:0,select:0};
 const f=await s6OpenFixture(browser,sources,{state,context,rpcHandler(method,p){
  if(method!=='zaprettune')return;
  if(p.action==='status')return {job:structuredClone(tuner.current)};
  if(p.action==='start'){tuner.start++;tuner.current=job();return {job:structuredClone(tuner.current)};}
  if(p.action==='cancel'){tuner.cancel++;tuner.current=job('restoring');return {ok:true};}
  if(p.action==='select'){tuner.select++;if(tuner.current?.status!=='completed'||!tuner.current.restored)return {error:'Unverified strategy'};return {args,wan:'eth0'};}
  throw new Error('Unexpected tune action '+p.action);
 }});
 let error;
 try{
  f.page.setDefaultTimeout(10000);
  await f.s6WaitCall('zaprettune');
  await f.page.waitForFunction(()=>!document.querySelector('.nk-tuner button:nth-child(3)').disabled);
  await f.page.getByRole('tab',{name:'Settings',exact:true}).click();
  await run(f,tuner);await f.s6CleanErrors();
 }catch(e){error=String(e.stack||e);}
 results.push({name,status:error?'FAIL':'PASS',error});
 await fs.writeFile(path.join(output,name+'.json'),JSON.stringify({result:results.at(-1),ui:await f.s6Snapshot(),calls:f.log},null,2)+'\n');
 console.log((error?'FAIL ':'PASS ')+name+(error?'\n'+error:''));
 await f.s6Close();if(context)await context.close();
}
try{
 await test('real_progress_cancel_and_rerun',async(f,t)=>{
  const manual=await f.page.getByLabel('Zapret strategy',{exact:true}).inputValue();
  await f.page.getByLabel('Tuning sites',{exact:true}).fill('blocked.example');
  await b(f,'Start tuning').click();await f.page.waitForFunction(()=>document.querySelector('.nk-tuner progress').value===1);
  assert.equal(await b(f,'Start tuning').isDisabled(),true);assert.equal(await b(f,'Cancel tuning').isEnabled(),true);
  assert.equal(await f.page.getByLabel('Zapret strategy',{exact:true}).inputValue(),manual);
  assert.equal(f.state.writes.length,0);assert.equal(f.state.actions.length,0);
  assert.equal(await b(f,'Apply the verified strategy to settings').isVisible(),false);
  await b(f,'Cancel tuning').click();await f.page.waitForFunction(()=>document.querySelector('.nk-tuner').textContent.includes('Cleanup is not confirmed yet'));
  assert.equal(await b(f,'Start tuning').isDisabled(),true);
  t.current=job('cancelled');await tick(f);assert.equal(await b(f,'Start tuning').isEnabled(),true);
  await b(f,'Start tuning').click();await f.s6WaitCall('zaprettune',6);assert.equal(t.start,2);
 });
 await test('confirmed_requires_explicit_selection_then_save_apply',async(f,t)=>{
  const original=(await f.s6Snapshot()).editor.zapretArgs;
  await b(f,'Apply the verified strategy to settings').click();
  assert.deepEqual((await f.s6Snapshot()).editor.zapretArgs,original);
  await f.page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();assert.equal(t.select,0);
  await b(f,'Apply the verified strategy to settings').click();
  await b(f,'Replace in draft').click();await f.page.waitForFunction(()=>window.s6editor.zapretArgs.includes('--dpi-desync=multisplit'));
  assert.equal(t.select,1);assert.equal(f.state.writes.length,0);assert.equal(f.state.actions.length,0);
  assert.deepEqual((await f.s6Snapshot()).editor.zapretArgs,args);
  await b(f,'Save and apply').click();await f.s6WaitCall('service');await f.s6Ready();
  assert.deepEqual(f.state.actions,['restart']);assert.deepEqual(f.state.writes[0].config.zapretArgs,args);
 },job('completed'));
 for(const terminal of ['unneeded','no_strategy','inconclusive','failed','cancelled','recovery_failed'])await test('terminal_'+terminal,async f=>{
  assert.equal(await b(f,'Apply the verified strategy to settings').isVisible(),false);
  assert.equal(f.state.writes.length,0);assert.equal(f.state.actions.length,0);
  assert.equal(await b(f,'Start tuning').isEnabled(),terminal!=='recovery_failed');
 },job(terminal));
 await test('reopen_reattaches_without_mutation',async(f,t)=>{
  await f.page.evaluate(async()=>{document.querySelector('main .container').replaceChildren();await window.s6mount();});
  await f.page.getByRole('tab',{name:'Settings',exact:true}).click();
  await f.page.waitForFunction(()=>document.querySelector('.nk-tuner').textContent.includes('Checking without a strategy'));
  assert.equal(t.start,0);assert.equal(await b(f,'Cancel tuning').isEnabled(),true);assert.equal(f.state.writes.length,0);
 },job());
 for(const kind of ['abort','http503','truncated','ubus'])await test('lost_response_'+kind,async(f,t)=>{
  f.s6Fault('zaprettune',{kind});await tick(f);
  assert.match(await f.page.locator('.nk-tuner [role=status]').textContent(),/may still be running/);
  assert.equal(await b(f,'Start tuning').isDisabled(),true);
  assert.equal(await f.page.getByRole('button',{name:'Apply the verified strategy to settings',exact:true,includeHidden:true}).isDisabled(),true);
  assert.equal(await b(f,'Cancel tuning').isEnabled(),true);
  await b(f,'Cancel tuning').click();assert.equal(t.cancel,1);
  assert.equal(f.s6Calls('zaprettune').findLast(c=>c.params.action==='cancel').params.job,'');
 },job());
 await test('poll_preserves_focus_and_details',async(f,t)=>{
  const details=f.page.locator('.nk-tune-results').locator('..');await details.locator('summary').click();await details.locator('summary').focus();
  await tick(f);assert.equal(await details.evaluate(el=>el.open),true);assert.equal(await details.locator('summary').evaluate(el=>el===document.activeElement),true);
  t.current.completed++;t.current.results.push({...t.current.results[1],round:2});await tick(f);
  assert.equal(await details.evaluate(el=>el.open),true);assert.equal(await details.locator('summary').evaluate(el=>el===document.activeElement),true);
 },job());
 await test('poll_does_not_blur_site_field',async f=>{
  const sites=f.page.getByLabel('Tuning sites',{exact:true});
  await sites.fill('typing.example');await sites.focus();
  await tick(f);await tick(f);
  assert.equal(await sites.evaluate(el=>el===document.activeElement),true,'background status poll must not disable and blur the sites field');
  assert.equal(await sites.inputValue(),'typing.example');
 },null);
 for(const payload of [{},{job:{id:'bad',status:'completed'}},{job:{...job(),status:'unknown-state'}}])await test('malformed_state_'+Object.keys(payload).length+'_'+(payload.job?.id||'empty')+'_'+(payload.job?.status||'empty'),async f=>{
  f.s6Fault('zaprettune',{payload});await tick(f);
  assert.match(await f.page.locator('.nk-tuner [role=status]').textContent(),/may still be running/);
  assert.equal(await b(f,'Start tuning').isDisabled(),true);assert.equal(await b(f,'Cancel tuning').isEnabled(),true);
 },job());
 for(const theme of Object.keys(s6Themes))for(const scheme of ['light','dark'])for(const width of [390,1360])await test('layout_'+theme+'_'+scheme+'_'+width,async f=>{
  await s6SetTheme(f,theme,scheme);await f.page.locator('.nk-tuner summary').click();
  assert.ok((await f.s6Snapshot()).overflow<=1);
  for(const control of await f.page.locator('.nk-tuner button:visible').all()){const bounds=await control.boundingBox();assert.ok(bounds.x>=0&&bounds.x+bounds.width<=width+1,JSON.stringify(bounds));}
  await f.page.screenshot({path:path.join(output,theme+'-'+scheme+'-'+width+'.png'),fullPage:true});
 },job('completed'),await browser.newContext({viewport:{width,height:950},colorScheme:scheme,serviceWorkers:'block',isMobile:width<500,hasTouch:width<500}));
}finally{
 await browser.close();await fs.writeFile(path.join(output,'results.json'),JSON.stringify({total:results.length,pass:results.filter(x=>x.status==='PASS').length,results},null,2)+'\n');
 if(results.some(x=>x.status==='FAIL'))process.exitCode=1;
}
