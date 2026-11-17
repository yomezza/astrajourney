// Additional public media-delivery burst during the running 100-player test.
// No extra account/session is created. This checks HTTP/hash/cache contracts,
// not 100 browser renderers or a production network.
import {writeFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {performance} from 'node:perf_hooks';
const [base,output]=process.argv.slice(2);
if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(base)||!output)throw Error('Owned loopback Web URL and output path required');
const result={started_at:new Date().toISOString(),virtual_clients:100,planet_clients:30,cold_requests:[],revalidations:[]};
const mapResponse=await fetch(base+'/cdn/api/v1/asset-map',{signal:AbortSignal.timeout(10000)});
if(!mapResponse.ok)throw Error('Asset map unavailable');
const map=await mapResponse.json();
const common=['/favicon.svg','/ui/icons/profile.svg','/ui/icons/sidebar-collapse.svg','/pilgrim.svg','/object-station.svg'];
const planet=['/planet-pixelart-v1/assets/terrain_grass.png','/planet-pixelart-v1/assets/water.png','/planet-pixelart-v1/assets/landing_module.png','/planet-pixelart-v1/assets/explorer_casual.png','/planet-pixelart-v1/assets/rock_large.png','/planet-pixelart-v1/assets/tree_green_medium.png'];
for(const key of [...common,...planet])if(!map.urls[key])throw Error('Published media key unavailable: '+key);
await Promise.all(Array.from({length:100},async(_,client)=>{
 await new Promise(resolve=>setTimeout(resolve,client*10));
 const keys=[...common,...(client%10<3?planet:[])];
 await Promise.all(keys.map(async key=>{
  const began=performance.now();
  try{
   const response=await fetch(base+map.urls[key],{signal:AbortSignal.timeout(15000)});
   const bytes=Buffer.from(await response.arrayBuffer());
   const hash=createHash('sha256').update(bytes).digest('hex');
   result.cold_requests.push({client,key,status:response.status,ms:performance.now()-began,bytes:bytes.length,hash_ok:hash===map.urls[key].split('/')[3],cache_control:response.headers.get('cache-control'),etag:response.headers.get('etag')});
  }catch(error){result.cold_requests.push({client,key,ms:performance.now()-began,error:error.message});}
 }));
}));
for(const key of [...common,...planet]){
 const prior=result.cold_requests.find(value=>value.key===key&&value.status===200);
 if(!prior?.etag)continue;
 const began=performance.now();
 const response=await fetch(base+map.urls[key],{headers:{'If-None-Match':prior.etag},signal:AbortSignal.timeout(15000)});
 result.revalidations.push({key,status:response.status,bytes:(await response.arrayBuffer()).byteLength,ms:performance.now()-began});
}
result.finished_at=new Date().toISOString();
writeFileSync(output,JSON.stringify(result,null,2)+'\n');
const failed=result.cold_requests.filter(value=>value.status!==200||!value.hash_ok||!value.cache_control?.includes('immutable'));
console.log(JSON.stringify({requests:result.cold_requests.length,failed:failed.length,bytes:result.cold_requests.reduce((sum,value)=>sum+(value.bytes||0),0),revalidations:result.revalidations.map(value=>value.status)}));
if(failed.length||result.revalidations.some(value=>value.status!==304))process.exitCode=1;
