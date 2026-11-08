import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { s6Root, s6ReadSources, s6InitialState, s6OpenFixture, s6SetTheme, s6Themes } from './fixtures/s6-luci-harness.mjs';

const {chromium}=await import(process.env.NADKOP_PLAYWRIGHT_MODULE||'/path/to/playwright-env/node_modules/playwright/index.mjs');
const browser=await chromium.launch({executablePath:process.env.NADKOP_CHROMIUM||'/run/current-system/sw/bin/chromium',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']});
const sources=await s6ReadSources(),results=[];
try {
 for(const theme of Object.keys(s6Themes))for(const width of [1360,390]) {
  const state=s6InitialState();
  state.status.runtimeChecks.push(...['unknown','pending','success','error'].map((value,index)=>({id:'action-'+index,title:'Operation '+index,state:value,detail:'Safe description of operation '+index})));
  const fixture=await s6OpenFixture(browser,sources,{state});let error;
  try {
   await fixture.page.setViewportSize({width,height:950});await s6SetTheme(fixture,theme);
   await fixture.page.getByRole('tab',{name:'Diagnostics',exact:true}).click();
   for(const [index,label] of ['No data','In progress','Completed','Error'].entries()) {
    const line=fixture.page.locator('.nk-runtime-checks>li').nth(index+1);
    assert.match(await line.textContent(),new RegExp(label));
    const summary=line.locator('summary');await summary.focus();await summary.press('Enter');
    assert.equal(await line.locator('p').isVisible(),true);
   }
   assert.ok((await fixture.s6Snapshot()).overflow<=1);await fixture.s6CleanErrors();
  }catch(e){error=String(e.stack||e);}
  await fixture.s6Close();results.push({theme,width,status:error?'FAIL':'PASS',error});
 }
}finally{await browser.close();}
const out=path.join(s6Root,'build/improvements-X/x2-implementation');await fs.mkdir(out,{recursive:true});await fs.writeFile(path.join(out,'diagnostics-ui.json'),JSON.stringify(results,null,2)+'\n');
console.log(JSON.stringify(results,null,2));if(results.some(r=>r.error))process.exitCode=1;
