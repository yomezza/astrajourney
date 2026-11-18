import assert from "node:assert/strict";
import {test} from "node:test";
import {RefreshQueue} from "../../web/features/chat/refresh.js";
import {ChatClient} from "../../web/features/chat/transport.js";

test("chat refresh coalesces bursts and never overlaps requests", async () => {
 let release, entered;
 const started=new Promise(resolve=>{entered=resolve;});
 let calls=0, active=0, peak=0;
 const errors=[];
 const queue=new RefreshQueue(async()=>{
  calls++;peak=Math.max(peak,++active);
  if(calls===1)await new Promise(resolve=>{release=resolve;entered();});
  active--;
 },error=>errors.push(error),{interval:0,delay:0,jitter:()=>0});
 const pending=queue.run();await started;
 for(let i=0;i<100;i++)queue.schedule();
 release();await pending;queue.close();
 assert.equal(calls,2);assert.equal(peak,1);assert.deepEqual(errors,[]);
});

test("closing an old chat session cancels its queued refresh", async()=>{
 let release, entered;
 const started=new Promise(resolve=>{entered=resolve;});let calls=0;
 const queue=new RefreshQueue(async()=>{calls++;await new Promise(resolve=>{release=resolve;entered();});},()=>{}, {interval:0,jitter:()=>0});
 const pending=queue.run();await started;queue.schedule();queue.close();release();await pending;
 assert.equal(calls,1);
});

test("chat conditional reads share requests and never serve cached data on revoked access",async t=>{
 const original=globalThis.fetch;t.after(()=>{globalThis.fetch=original;});
 let requests=0, denied=false;
 globalThis.fetch=async(_url,options)=>{
  requests++;
  if(denied)return new Response(JSON.stringify({error:"access_denied"}),{status:403});
  if(options.headers["If-None-Match"])return new Response(null,{status:304});
  return new Response(JSON.stringify([{id:"1",body:"hello"}]),{headers:{ETag:'W/"view"'}});
 };
 const client=new ChatClient(()=>{});t.after(()=>client.close());
 const [a,b]=await Promise.all([client.get("history",{kind:"global"}),client.get("history",{kind:"global"})]);
 assert.deepEqual(a,b);assert.equal(requests,1);
 a[0].body="local change";
 assert.equal((await client.get("history",{kind:"global"}))[0].body,"hello");
 denied=true;
 await assert.rejects(client.get("history",{kind:"global"}),{message:"access_denied"});
 assert.equal(client.views.size,0);
});
