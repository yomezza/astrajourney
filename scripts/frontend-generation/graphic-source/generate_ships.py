"""Generate exact Cosmosimulator v0.23.0 spacecraft assets from upstream parametric model recipe.
Source: scripts/generate_ship_holograms.py, docs/design/ship-holograms/generate.py.
"""
import sys,math
from pathlib import Path
from ship_projection import Mesh,pilgrim,raw_projection,path_for

def engine(mesh,x,front,rear,width=.35,height=.38,z=0):
 mesh.loft([(x,front,z,width,height),(x,rear,z,width,height)],part='pods')
 mesh.loft([(x,rear+.03,z,width*.76,height*.76),(x,rear+.42,z,width*.76,height*.76)],part='engine')

def wanderer():
 m=Mesh()
 m.loft([(0,-4.8,0,.08,.12),(0,-2.7,.05,.40,.30),(0,-.6,.08,.65,.42),(0,2.7,.08,.53,.36),(0,3.4,.08,.35,.28)])
 m.loft([(0,-2.6,.43,.20,.12),(0,-.9,.55,.30,.14)],sides=4,part='canopy')
 for sign in (-1,1):
  m.prism([(sign*.45,-.9),(sign*2.15,1.6),(sign*1.65,2.2),(sign*.45,.9)],-.1,.08)
  engine(m,sign*1.28,1.15,3.5,.25,.29)
  m.loft([(sign*.44,-3.2,.1,.04,.05),(sign*.44,-1.5,.1,.04,.05)],sides=4,part='sensor')
 return m

def forge():
 m=Mesh()
 m.loft([(0,-2.9,.1,.55,.50),(0,-1.3,.1,1.15,.75),(0,1.8,.1,1.15,.75),(0,3,.1,.80,.60)],sides=8)
 m.loft([(0,-1.8,.85,.3,.18),(0,-.6,.93,.48,.20)],sides=4,part='canopy')
 for sign in (-1,1):
  m.prism([(sign*.8,-1.7),(sign*2.0,-2.9),(sign*2.15,1.5),(sign*.8,1.8)],-.3,.05)
  m.loft([(sign*1.75,-3.4,-.15,.24,.28),(sign*1.75,-.1,-.15,.42,.42)],sides=4,part='tool')
  m.loft([(sign*1.75,-4.1,-.15,.10,.12),(sign*1.75,-3.5,-.15,.44,.44)],sides=8,part='tool')
  engine(m,sign*1.85,.5,3.25,.45,.47)
 return m

def ark():
 m=Mesh()
 m.loft([(0,-4.2,.2,.32,.40),(0,-2.9,.2,.78,.70),(0,3.8,.2,.78,.70),(0,4.5,.2,.55,.58)],sides=4)
 m.loft([(0,-3.5,.85,.32,.13),(0,-2.4,.95,.45,.15)],sides=4,part='canopy')
 for sign in (-1,1):
  m.prism([(sign*.5,-2.7),(sign*2.3,-2.3),(sign*2.3,3.2),(sign*.5,3.4)],-.40,-.10)
  for y in (-2.3,-.5,1.3):m.loft([(sign*1.4,y,.25,.76,.70),(sign*1.4,y+1.5,.25,.76,.70)],sides=4,part='cargo')
  engine(m,sign*1.3,3.15,4.5,.46,.48)
 return m

def hermes():
 m=Mesh()
 m.loft([(0,-4.7,.05,.09,.12),(0,-2.7,.05,.48,.35),(0,-.2,.05,.85,.50),(0,1.8,.05,.73,.46),(0,3.4,.05,.45,.34)],sides=8)
 m.loft([(0,-2.7,.48,.23,.12),(0,-1.0,.65,.40,.17)],sides=4,part='canopy')
 for sign in (-1,1):
  m.prism([(sign*.4,-1.6),(sign*2.45,1.7),(sign*2.1,2.9),(sign*.5,1.3)],-.14,.12)
  engine(m,sign*1.55,.7,3.6,.32,.34)
  m.prism([(sign*.35,1.2),(sign*.95,3.0),(sign*.55,3.5)],.25,.48,part='fin')
 engine(m,0,2.7,4.2,.33,.36,-.12)
 return m

def scout(tier):
 m=Mesh()
 m.loft([(0,-4.7-tier*.3,.1,.10,.12),(0,-2.6,.1,.40+tier*.12,.32),(0,.3,.1,.65+tier*.10,.48),(0,3.4,.1,.50,.38)])
 m.loft([(0,-2.8,.50,.22,.13),(0,-1.1,.67,.38,.18)],sides=4,part='canopy')
 for sign in (-1,1):
  if tier==1:
   m.loft([(sign*.85,-4.3,.05,.07,.08),(sign*.85,-.6,.05,.14,.15)],sides=4,part='sensor')
   m.prism([(sign*.4,-.5),(sign*2.1,.8),(sign*1.7,2.1),(sign*.4,1)],-.1,.14)
  elif tier==2:
   m.prism([(sign*.35,-2.5),(sign*2.5,1.5),(sign*2.0,2.4),(sign*.5,.7)],-.15,.15)
   m.loft([(sign*2.1,-.7,.12,.18,.25),(sign*2.1,1.8,.12,.18,.25)],part='sensor')
  else:
   m.prism([(sign*.45,-2),(sign*2.35,-3.4),(sign*2.1,-.9),(sign*.55,.2)],-.15,.17)
   m.loft([(sign*1.0,-4.8,.2,.08,.1),(sign*1.0,-1.7,.2,.28,.3)],part='sensor')
  engine(m,sign*(1.2+tier*.15),.9,3.8,.25+tier*.03,.34)
 if tier==3:
  sides=12
  for i in range(sides):
   a,b=2*math.pi*i/sides,2*math.pi*(i+1)/sides
   m.prism([(2.1*math.cos(a),-.5+2.1*math.sin(a)),(2.1*math.cos(b),-.5+2.1*math.sin(b)),(1.93*math.cos(b),-.5+1.93*math.sin(b)),(1.93*math.cos(a),-.5+1.93*math.sin(a))],.45,.58,part='sensor')
 return m

def miner(tier):
 m=Mesh()
 m.loft([(0,-3.5,.25,.5,.5),(0,-1.6,.25,1.2,.85),(0,2.8,.25,1.2,.85),(0,4,.25,.8,.65)],sides=8)
 m.loft([(0,-2.5,1.1,.36,.2),(0,-.5,1.3,.5,.26)],sides=4,part='canopy')
 for sign in (-1,1):
  m.prism([(sign*.7,-1.8),(sign*2.7,-2.7),(sign*2.8,2.4),(sign*.7,2.9)],-.4,-.05)
  for z in ((-.25,) if tier==1 else (-.65,.75)):
   m.loft([(sign*2.25,-3.7,z,.15,.18),(sign*2.25,-.1,z,.45,.40)],sides=4,part='tool')
   m.loft([(sign*2.25,-4.6,z,.04,.05),(sign*2.25,-3.7,z,.48,.45)],sides=6,part='tool')
  for y in ((-.4,) if tier==1 else (-1.2,.8)):
   m.loft([(sign*1.35,y,.2,.55,.6),(sign*1.35,y+1.4,.2,.55,.6)],sides=4,part='cargo')
  engine(m,sign*2.2,1.6,4.0,.48,.50)
 if tier==2:
  engine(m,0,3.4,4.6,.55,.50,-.15)
  m.loft([(0,.3,1.5,.3,.35),(0,1.4,1.5,.3,.35)],sides=6,part='sensor')
 return m

def freighter(tier):
 m=Mesh()
 m.loft([(0,-5.8,.4,.3,.4),(0,-4.4,.4,.8,.7),(0,4.6,.4,.8,.7),(0,5.5,.4,.6,.55)],sides=4)
 m.loft([(0,-5.0,1.1,.35,.16),(0,-3.6,1.3,.55,.2)],sides=4,part='canopy')
 for sign in (-1,1):
  m.prism([(sign*.6,-3.8),(sign*2.65,-3.5),(sign*2.65,4.7),(sign*.6,4.9)],-.55,-.22)
  for y in (-3.8,-1.8,.2,2.2):m.loft([(sign*1.65,y,.5,.8,.75),(sign*1.65,y+1.7,.5,.8,.75)],sides=4,part='cargo')
  engine(m,sign*1.8,4.0,5.4,.52,.55)
 if tier==2:
  for y in (-2.8,-.8,1.2):m.loft([(0,y,1.7,.70,.55),(0,y+1.7,1.7,.70,.55)],sides=4,part='cargo')
  engine(m,0,4.5,6.0,.65,.65,-.1)
 return m

def courier(tier):
 m=Mesh()
 m.loft([(0,-5.0-tier*.3,.05,.07,.1),(0,-2.8,.05,.5,.4),(0,.0,.05,.9,.55),(0,3.2,.05,.65,.45)],sides=8)
 m.loft([(0,-2.8,.55,.25,.13),(0,-1.0,.75,.4,.2)],sides=4,part='canopy')
 for sign in (-1,1):
  if tier==1:wing=[(sign*.4,-.2),(sign*2.2,-1.8),(sign*2.5,1.8),(sign*.6,1.1)]
  elif tier==2:wing=[(sign*.3,-2.8),(sign*2.8,.4),(sign*2.5,2.7),(sign*1.8,1.2),(sign*.5,.4)]
  else:wing=[(sign*.35,-3.3),(sign*1.35,-1.3),(sign*2.9,.5),(sign*2.4,2.8),(sign*.6,1.8)]
  m.prism(wing,-.16,.12)
  engine(m,sign*(1.5+tier*.2),-.3,3.8,.3+tier*.03,.4)
  m.loft([(sign*.7,-.6,-.45,.3,.3),(sign*.7,1.8,-.45,.3,.3)],sides=4,part='cargo')
 if tier>=2:engine(m,0,2.5,4.7,.4,.42,-.1)
 if tier==3:
  for sign in (-1,1):m.prism([(sign*.45,.4),(sign*.9,2.8),(sign*.4,3.8)],.5,1.05,part='fin')
 return m

MODELS={
 'pilgrim':('Pilgrim',pilgrim,'#71d8bb','#c5ffe9',28,22),
 'wanderer':('Wanderer',wanderer,'#79decb','#badfff',26,24),
 'forge':('Forge',forge,'#7bd9ad','#e6c080',30,40),
 'ark':('Ark',ark,'#93d8ae','#d9e9a8',28,60),
 'hermes':('Hermes',hermes,'#74e7d0','#cefff6',25,32),
 'vesper':('Vesper',lambda:scout(1),'#7edfc6','#bce8ff',28,28),
 'astra':('Astra',lambda:scout(2),'#75ddc9','#9bd6ff',27,34),
 'argus':('Argus',lambda:scout(3),'#7ae3bd','#e6f9ce',30,42),
 'crucible':('Crucible',lambda:miner(1),'#8dddab','#f0d293',30,48),
 'titan':('Titan',lambda:miner(2),'#91dca5','#ffd395',32,60),
 'atlas':('Atlas',lambda:freighter(1),'#89d9b7','#d0e7a2',28,72),
 'leviathan':('Leviathan',lambda:freighter(2),'#83d9ad','#e4ecc3',30,92),
 'iris':('Iris',lambda:courier(1),'#77dfcf','#c3ecff',26,40),
 'zephyr':('Zephyr',lambda:courier(2),'#74e7cf','#b7fff2',27,48),
 'lightning':('Lightning',lambda:courier(3),'#81ebc8','#e4fff3',28,58),
}

def render(model_id,settings):
 name,build,color,accent,elevation,length=settings
 mesh=build();edges=mesh.edges();important={'engine','canopy','tool','sensor'}
 body=[edge for edge,faces in edges.items() if faces[0][1] not in important]
 details=[edge for edge,faces in edges.items() if faces[0][1] in important]
 plume=Mesh();particle_points=[];particle_edges=[];adjacency={}
 for face,_,part in mesh.faces:
  if part!='engine':continue
  for a,b in zip(face,face[1:]+face[:1]):
   adjacency.setdefault(a,set()).add(b);adjacency.setdefault(b,set()).add(a)
 unseen=set(adjacency)
 while unseen:
  component,pending=set(),[unseen.pop()]
  while pending:
   vertex=pending.pop()
   if vertex in component:continue
   component.add(vertex);pending.extend(adjacency[vertex]-component)
  unseen-=component
  rear=max(mesh.points[i][1] for i in component)
  ring=[mesh.points[i] for i in component if abs(mesh.points[i][1]-rear)<1e-6]
  x=sum(p[0] for p in ring)/len(ring)
  z=sum(p[2] for p in ring)/len(ring)
  width=(max(p[0] for p in ring)-min(p[0] for p in ring))/2
  height=(max(p[2] for p in ring)-min(p[2] for p in ring))/2
  plume.loft([(x,rear+.03,z,width*.8,height*.8),(x,rear+1.0,z,width*.5,height*.5),(x,rear+2.4,z,.025,.025)],part='thrust')
  for offset in (-1,0,1):
   start=len(particle_points)
   particle_points.extend([(x+offset*width*.5,rear+.15,z),(x+offset*(width+.35),rear+4.2,z+offset*.18)])
   particle_edges.append((start,start+1))
 raw_particles=[raw_projection(particle_points,-35+i*360/48,elevation)[0] for i in range(49)]
 raw_frames=[raw_projection(mesh.points,-35+i*360/48,elevation)[0] for i in range(49)]
 raw_plumes=[raw_projection(plume.points,-35+i*360/48,elevation)[0] for i in range(49)]
 centers=[((min(x for x,_ in f)+max(x for x,_ in f))/2,(min(y for _,y in f)+max(y for _,y in f))/2) for f in raw_frames]
 extent_x=max(abs(x-cx) for body_frame,thrust_frame,(cx,cy) in zip(raw_frames,[a+b for a,b in zip(raw_plumes,raw_particles)],centers) for x,y in body_frame+thrust_frame)
 extent_y=max(abs(y-cy) for body_frame,thrust_frame,(cx,cy) in zip(raw_frames,[a+b for a,b in zip(raw_plumes,raw_particles)],centers) for x,y in body_frame+thrust_frame)
 footprint=.80+.20*length/92
 scale=min(415/extent_x,235/extent_y)*footprint
 def project_frames(raw):
  return [[(450+(x-cx)*scale,280+(y-cy)*scale) for x,y in f] for f,(cx,cy) in zip(raw,centers)]
 frames,thrust_frames=project_frames(raw_frames),project_frames(raw_plumes)
 particle_frames=project_frames(raw_particles)
 assert all(math.isfinite(x) and math.isfinite(y) and 5<x<895 and 5<y<555 for f in frames+thrust_frames+particle_frames for x,y in f)
 def viewport(all_frames):
  rx=max(abs(x-450) for f in all_frames for x,y in f)/footprint+18
  ry=max(abs(y-280) for f in all_frames for x,y in f)/footprint+18
  rx=max(rx,ry*900/560);ry=rx*560/900
  return f'{450-rx:.2f} {280-ry:.2f} {2*rx:.2f} {2*ry:.2f}'
 idle_view=viewport(frames)
 moving_view=viewport(frames+thrust_frames+particle_frames)
 definitions=[]
 for part,lines,projected in (('body',body,frames),('details',details,frames),('thrust',list(plume.edges()),thrust_frames),('particles',particle_edges,particle_frames)):
  paths=[path_for(lines,frame) for frame in projected]
  assert paths[0]==paths[-1]
  definitions.append(f'<path id="{model_id}-{part}" d="{paths[0]}"><animate attributeName="d" values="{";".join(paths)}" dur="16s" repeatCount="indefinite" calcMode="linear"/></path>')
 return f'''<svg xmlns="http://www.w3.org/2000/svg" width="900" height="560" viewBox="{idle_view}" data-idle-viewbox="{idle_view}" data-moving-viewbox="{moving_view}" data-length="{length}" role="img" fill="none" aria-labelledby="{model_id}-title {model_id}-description">
<title id="{model_id}-title">{name} hologram</title>
<desc id="{model_id}-description">Transparent wireframe spacecraft. Full spatial orbit in 16 seconds, soft green glow and moving light pulses.</desc>
<defs><filter id="{model_id}-glow" x="-25%" y="-25%" width="150%" height="150%"><feGaussianBlur stdDeviation="1.2"/></filter>{"".join(definitions)}</defs>
<g fill="none" stroke-linecap="round" stroke-linejoin="round">
<use href="#{model_id}-body" stroke="{color}" stroke-width="3.4" opacity=".26" filter="url(#{model_id}-glow)"/>
<use href="#{model_id}-body" stroke="{color}" stroke-width="1.2" opacity=".72"/>
<use href="#{model_id}-details" stroke="{color}" stroke-width="3" opacity=".22" filter="url(#{model_id}-glow)"/>
<use href="#{model_id}-details" stroke="{accent}" stroke-width="1.6" opacity=".96"/>
<use href="#{model_id}-body" stroke="#cefff0" stroke-width="1.9" stroke-dasharray="16 190" opacity=".8"><animate attributeName="stroke-dashoffset" from="0" to="-206" dur="5s" repeatCount="indefinite"/></use>
<g class="engine-thrust" visibility="hidden">
<use href="#{model_id}-thrust" stroke="{color}" stroke-width="3" opacity=".3" filter="url(#{model_id}-glow)"/>
<use href="#{model_id}-thrust" stroke="#a6ffe3" stroke-width="1.1" stroke-dasharray="6 8"><animate attributeName="stroke-dashoffset" from="0" to="-14" dur=".8s" repeatCount="indefinite"/></use>
<use class="engine-particles" href="#{model_id}-particles" stroke="{color}" stroke-width="8" stroke-dasharray="1 23" opacity=".55" filter="url(#{model_id}-glow)"><animate attributeName="stroke-dashoffset" from="0" to="-24" dur=".65s" repeatCount="indefinite"/></use>
<use class="engine-particles" href="#{model_id}-particles" stroke="#d2fff0" stroke-width="4.5" stroke-dasharray="1 23" opacity=".95"><animate attributeName="stroke-dashoffset" from="0" to="-24" dur=".65s" repeatCount="indefinite"/></use>
<animate attributeName="opacity" values=".7;1;.7" dur="1.2s" repeatCount="indefinite"/>
</g></g></svg>
'''

if __name__=='__main__':
 import hashlib
 dest=Path(__file__).resolve().parents[1]/'embedded-original-assets'
 dest.mkdir(parents=True,exist_ok=True)
 for model_id,settings in MODELS.items():
  result=render(model_id,settings)
  p=dest/(model_id+'.svg');p.write_text(result,encoding='utf-8')
  raw=result.encode();sha=hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest()
  print(p.name,p.stat().st_size,sha)
