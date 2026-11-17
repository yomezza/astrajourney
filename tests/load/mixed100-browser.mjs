import {createRequire} from 'node:module';
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {performance} from 'node:perf_hooks';
const {chromium}=createRequire(new URL('../ui/package.json',import.meta.url))('playwright');
const input=JSON.parse(readFileSync(0,'utf8'));
if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(input.base))throw Error('Owned loopback Web required');
let stopping=false;
process.once('SIGINT',()=>{stopping=true;});
process.once('SIGTERM',()=>{stopping=true;});
const result={harness_version:2,started_at:new Date().toISOString(),module_openings:[],page_errors:[],http_errors:[],requests:0,websockets:0,ready:false};
const save=()=>writeFileSync(input.output+'/browser.json',JSON.stringify({...result,finished_at:new Date().toISOString()},null,2));
let browser;
try{
 browser=await chromium.launch({handleSIGINT:false,handleSIGTERM:false,...(process.env.CHROMIUM_EXECUTABLE_PATH?{executablePath:process.env.CHROMIUM_EXECUTABLE_PATH}:{}),args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
 const context=await browser.newContext({viewport:{width:1440,height:1000},serviceWorkers:'block'});
 await context.addCookies([{name:'session',value:input.cookie,url:input.base,httpOnly:true,sameSite:'Lax'}]);
 const page=await context.newPage();page.setDefaultTimeout(30000);
 page.on('pageerror',error=>{if(result.page_errors.length<30)result.page_errors.push(error.message);});
 page.on('request',()=>{result.requests++;});
 page.on('websocket',()=>{result.websockets++;});
 page.on('response',response=>{if(response.status()>=400&&result.http_errors.length<100)result.http_errors.push({path:new URL(response.url()).pathname,status:response.status()});});
 await page.addInitScript(()=>{window.__mixedProbe={long_tasks:0,long_task_ms:0,long_task_max_ms:0};try{new PerformanceObserver(list=>{for(const entry of list.getEntries()){window.__mixedProbe.long_tasks++;window.__mixedProbe.long_task_ms+=entry.duration;window.__mixedProbe.long_task_max_ms=Math.max(window.__mixedProbe.long_task_max_ms,entry.duration);}}).observe({type:'longtask',buffered:true});}catch{}});
 const began=performance.now();await page.goto(input.base,{waitUntil:'domcontentloaded'});
 await page.waitForFunction(()=>document.getElementById('startup-screen')?.hidden&&!document.getElementById('game')?.hidden,{},{timeout:60000});
 result.initial_ready_ms=performance.now()-began;result.ready=true;save();
 mkdirSync(input.output+'/previews',{recursive:true});await page.screenshot({path:input.output+'/previews/browser-ready.png'});
 const modules=['market','tasks','profile','studies','notifications','galaxy'];let i=0;
 while(!stopping){
  const id=modules[i++%modules.length];
  const start=performance.now();
  try{
   const button=page.locator(`[data-open="${id}"]`).first();
   if(await button.count()){
    await button.evaluate(el=>{for(let ancestor=el.parentElement;ancestor;ancestor=ancestor.parentElement)if(ancestor.tagName==='DETAILS')ancestor.open=true;});
    await button.click();
    await page.waitForFunction(id=>{const el=document.getElementById('window-'+id);return el?.open&&!el.classList.contains('module-pending');},id,{timeout:35000});
    result.module_openings.push({id,ms:performance.now()-start,at:new Date().toISOString(),ok:true});
   }
   if(i%3===0)await page.evaluate(()=>{for(const dialog of document.querySelectorAll('dialog[open]'))dialog.querySelector('.window-actions button:last-child')?.click();});
  }catch(error){result.module_openings.push({id,ms:performance.now()-start,ok:false,error:error.message.slice(0,220)});}
  result.performance=await page.evaluate(()=>({long_tasks:window.__mixedProbe,resource_entries:performance.getEntriesByType('resource').length}));save();
  for(let j=0;j<20&&!stopping;j++)await new Promise(resolve=>setTimeout(resolve,1000));
 }
 await page.screenshot({path:input.output+'/previews/browser-final.png'}).catch(()=>{});
}catch(error){result.fatal=error.message;}
finally{save();await browser?.close();}
