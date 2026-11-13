/* Astra Journey / autonomous animated SVG models for the Original C handoff.
   These are new demo wireframes, NOT copied or required production assets.
   True 3D yaw: x = cos(lat)*sin(lon+theta), z = cos(lat)*cos(lon+theta).
   theta runs monotonically through 2π; 180° is a distinct half turn,
   360° coincides with 0° and loops without a mid-cycle snap. */
(() => {
  'use strict';
  const NS='http://www.w3.org/2000/svg', TAU=Math.PI*2;
  const palette={star:'#ffc77e',garden:'#89d8b2',rocky:'#9ebdc7',icy:'#9dd2eb',gas:'#e9c48d',volcanic:'#e8a087',asteroid:'#c2a989',icefield:'#a3d4e7',station:'#89d4df',blackhole:'#bda3e6'};
  const periods={star:9.2,garden:13.6,rocky:15.2,icy:15.8,gas:12.2,volcanic:14.7,asteroid:18.5,icefield:18,station:14,blackhole:17};
  const active=new Set();
  let activeFrame=0;
  function el(name,attr={}){const x=document.createElementNS(NS,name);for(const [k,v] of Object.entries(attr))x.setAttribute(k,String(v));return x;}
  const round=v=>(Math.round(v*10)/10).toFixed(1);
  function point(lat,lon,yaw,scale=1){
    const a=lon+yaw, x=Math.cos(lat)*Math.sin(a),z=Math.cos(lat)*Math.cos(a);
    return [180+x*91*scale,111+Math.sin(lat)*87*scale-z*24*scale,z];
  }
  function path(points){return points.map((p,i)=>(i?'L':'M')+round(p[0])+' '+round(p[1])).join(' ');}
  function meridian(lon,yaw){return path(Array.from({length:33},(_,k)=>point(-Math.PI/2+0.001+k*(Math.PI-.002)/32,lon,yaw)));}
  function parallel(lat,yaw){return path(Array.from({length:65},(_,k)=>point(lat,k*TAU/64,yaw)));}
  function signature(yaw,type){
    // Deliberately asymmetric longitudes make 180° different from 0°.
    const preset={star:[[.2,.9,.23],[-.48,3.4,.14]],garden:[[.22,1.2,.39],[-.3,3.6,.28],[.52,5.1,.20]],rocky:[[.3,.8,.17],[-.53,2.7,.24]],icy:[[.48,1.7,.32],[-.36,4.2,.22]],gas:[[.12,.4,.2],[-.28,2.4,.17],[.55,5.1,.15]],volcanic:[[.3,1,.18],[-.4,3,.34]],station:[[.2,1.5,.25]],blackhole:[[.25,1.4,.16]],asteroid:[[.25,1.6,.20]],icefield:[[.2,2.4,.24]]}[type]||[];
    return preset.map(([la,lo,rad])=>path(Array.from({length:19},(_,i)=>{
      const a=i*TAU/18;
      return point(la+Math.sin(a)*rad*.8,lo+Math.cos(a)*rad*1.2/Math.max(.2,Math.cos(la)),yaw,.98);
    }))).join(' ');
  }
  function ring(yaw,scale=1.35){return path(Array.from({length:65},(_,i)=>{
    const a=i*TAU/64+yaw;
    const x=Math.cos(a)*scale, z=Math.sin(a)*scale;
    return [180+x*91,111+z*19];
  }));}
  function animate(time){
    activeFrame=0;
    const reduce=window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
    for(const model of active){
      if(!model.svg.isConnected){active.delete(model);continue;}
      const theta=reduce?0:(time/(model.period*1000)%1)*TAU;
      model.grid.setAttribute('d', model.lats.map(la=>parallel(la,theta)).join(' ')+model.lons.map(lo=>meridian(lo,theta)).join(' '));
      model.detail.setAttribute('d',signature(theta,model.type));
      if(model.orbit)model.orbit.setAttribute('d',ring(theta,model.type==='station'?1.22:1.35));
    }
    if(active.size)activeFrame=requestAnimationFrame(animate);
  }
  function create(type,title){
    const color=palette[type]||'#9fb6bf',svg=el('svg',{viewBox:'0 0 360 224',width:'360',height:'224',role:'img','aria-label':title+' — непрерывная 3D-проекция с полным оборотом 360°','data-yaw':'360-full-cycle'});
    svg.classList.add('holo-svg');
    const bg=el('circle',{cx:180,cy:111,r:102,fill:color,opacity:'.035'}),rim=el('circle',{cx:180,cy:111,r:103,stroke:color,'stroke-width':1,opacity:'.47',fill:'none'});
    const grid=el('path',{fill:'none',stroke:color,'stroke-width':'.65',opacity:'.41'}),detail=el('path',{fill:'none',stroke:color,'stroke-width':'1.6',opacity:'.85'});
    svg.append(bg,rim,grid,detail);
    let orbit=null;
    if(['gas','station','blackhole','asteroid','icefield'].includes(type)){
      orbit=el('path',{fill:'none',stroke:color,'stroke-width':'1.2',opacity:'.68'});if(['asteroid','icefield'].includes(type))orbit.setAttribute('stroke-dasharray','4 4');svg.append(orbit);
    }
    if(type==='blackhole')svg.append(el('circle',{cx:180,cy:111,r:30,fill:'#111d26',stroke:color,'stroke-width':2}));
    if(type==='star')svg.append(el('circle',{cx:180,cy:111,r:96,fill:'none',stroke:color,'stroke-width':3,opacity:'.25'}));
    for(const [y,text] of [[13,'EDEM / ASTROGRAPHIC PROJECTION'],[216,'360° / CONTINUOUS YAW']]){
      const tx=el('text',{x:9,y,fill:color,'font-size':8,'font-family':'monospace',opacity:'.7'});tx.textContent=text;svg.append(tx);
    }
    const model={svg,type,grid,detail,orbit,period:periods[type]||13,lats:[-.92,-.58,-.28,0,.28,.58,.92],lons:[0,.7,1.4,2.1,2.8,3.5,4.2,4.9,5.6]};
    active.add(model);
    if(!activeFrame)activeFrame=requestAnimationFrame(animate);
    return svg;
  }
  window.AstraGuideHologram=Object.freeze({create});
})();