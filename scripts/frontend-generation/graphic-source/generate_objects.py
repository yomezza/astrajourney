"""Production object SVG generator corresponding to scripts/generate_object_holograms.py (v0.23.0)."""
from pathlib import Path
import math,hashlib
from ship_projection import raw_projection,project,path_for

class Wire:
 def __init__(self):self.points=[];self.edges=[]
 def line(self,points,closed=False):
  start=len(self.points);self.points.extend(points)
  self.edges.extend((start+i,start+i+1) for i in range(len(points)-1))
  if closed:self.edges.append((len(self.points)-1,start))
 def ring(self,r,z=0,n=24):self.line([(r*math.cos(i*2*math.pi/n),r*math.sin(i*2*math.pi/n),z) for i in range(n)],True)

def xyz(lat,lon,r=2):
 lat,lon=math.radians(lat),math.radians(lon)
 return (r*math.cos(lat)*math.cos(lon),r*math.cos(lat)*math.sin(lon),r*math.sin(lat))

def planet(kind):
 m=Wire();n=16 if kind=='icy' else 24
 for lat in (-60,-30,0,30,60):m.line([xyz(lat,i*360/n) for i in range(n)],True)
 for lon in range(0,180,30):m.line([xyz(i*15-90,lon if i<=12 else lon+180) for i in range(13)])
 if kind=='garden':
  for coastline in [[(58,12),(30,5),(15,35),(-8,20),(-30,43),(-12,63),(20,55),(48,35)],[(40,150),(15,120),(-8,137),(-42,128),(-50,165),(-20,180),(8,174)]]:
   m.line([xyz(a,b,2.035) for a,b in coastline],True)
 elif kind=='gas':
  for lat in (-48,-38,-13,13,38,48):m.line([xyz(lat,i*15,2.02) for i in range(24)],True)
  m.line([(2.75*math.cos(i*math.pi/16),2.75*math.sin(i*math.pi/16),.22*math.cos(i*math.pi/16)) for i in range(32)],True)
 elif kind=='rocky':
  for lat,lon in [(20,15),(-25,120),(45,210)]:m.line([xyz(lat+9*math.sin(i*math.pi/5),lon+12*math.cos(i*math.pi/5),2.02) for i in range(10)],True)
 elif kind=='lava':
  for lon in (15,130,250):m.line([xyz(lat,lon+(lat%19)*1.7,2.04) for lat in range(-75,76,15)])
 elif kind=='desert':
  for lat in (-12,12,42):m.line([xyz(lat+5*math.sin(i*.7),i*15,2.025) for i in range(24)],True)
 else:
  for lon in (45,165,285):m.line([xyz(lat,lon+(8 if (lat//15)%2 else -8),2.03) for lat in range(-75,76,15)])
 return m

def station():
 m=Wire()
 for r in (1.4,1.8):
  for z in (-.28,.28):m.ring(r,z,24)
 for i in range(12):
  a=i*math.pi/6;c,s=math.cos(a),math.sin(a)
  m.line([(1.4*c,1.4*s,-.28),(1.8*c,1.8*s,-.28),(1.8*c,1.8*s,.28),(1.4*c,1.4*s,.28)],True)
 for a in (0,math.pi/2,math.pi,math.pi*1.5):
  for z in (-.12,.12):m.line([(.2*math.cos(a)-.1,.2*math.sin(a)-.1,z),(2.7*math.cos(a)-.1,2.7*math.sin(a)-.1,z),(2.7*math.cos(a)+.1,2.7*math.sin(a)+.1,z),(.2*math.cos(a)+.1,.2*math.sin(a)+.1,z)],True)
 for r,z in [(.45,-.8),(.65,-.35),(.65,.35),(.35,1.1)]:m.ring(r,z,8)
 for i in range(8):
  a=i*math.pi/4;m.line([(r*math.cos(a),r*math.sin(a),z) for r,z in [(.45,-.8),(.65,-.35),(.65,.35),(.35,1.1)]])
 return m

def field(ice):
 m=Wire()
 for j in range(7):
  a=j*2.4;x,y=math.cos(a)*(1+j*.22),math.sin(a)*(1+j*.22);z=math.sin(j)*.35;r=.24+(j%3)*.12
  pts=[(x+r*math.cos(i*math.pi/3),y+r*math.sin(i*math.pi/3),z+(.08*math.sin(i+j) if not ice else 0)) for i in range(6)]
  m.line(pts,True)
  for i,p in enumerate(pts):
   m.line([p,(x+.08,y-.03,z+r*(2 if ice else 1.05))]);m.line([p,(x-.06,y+.06,z-r*(1.6 if ice else .8))])
 return m

def star():
 m=planet('gas')
 for j in range(8):
  a=j*math.pi/4;x,y=2.5*math.cos(a),2.5*math.sin(a)
  m.line([(x,y,0),(x*1.18,y*1.18,.5),(x*1.05,y*1.05,.8)])
 return m

def blackhole():
 m=Wire()
 for r in (1.0,1.14,1.35,1.8,2.3,2.8):m.ring(r,0,32)
 for j in range(8):
  a=j*math.pi/4
  m.line([(r*math.cos(a+r*.6),r*math.sin(a+r*.6),.12*math.sin(r*3+a)) for r in [1.1+i*.14 for i in range(13)]])
 m.line([(1.1*math.cos(i*math.pi/16),0,1.1*math.sin(i*math.pi/16)) for i in range(32)],True)
 return m

MODELS={
 'icy':(lambda:planet('icy'),'#8ec6ee'), 'rocky':(lambda:planet('rocky'),'#b0b1b3'),
 'garden':(lambda:planet('garden'),'#8bcf9e'), 'gas':(lambda:planet('gas'),'#d9b889'),
 'desert':(lambda:planet('desert'),'#d7bb83'), 'lava':(lambda:planet('lava'),'#e58c72'),
 'station':(station,'#7cc8d2'), 'asteroids':(lambda:field(False),'#bfb29a'),
 'ice-field':(lambda:field(True),'#9fd7eb'), 'black-hole':(blackhole,'#b59ad4')}
for letter,color in {'o':'#96baff','b':'#bdd5ff','a':'#edf1ff','f':'#fff0c4','g':'#ffdb78','k':'#f5ad65','m':'#ee8277'}.items():MODELS['star-'+letter]=(star,color)

def render(name,build,color):
 m=build()
 raw=[raw_projection(m.points,-35+i*360/32,28)[0] for i in range(33)]
 sx=max(max(x for x,y in f)-min(x for x,y in f) for f in raw)
 sy=max(max(y for x,y in f)-min(y for x,y in f) for f in raw)
 scale=min(650/sx,370/sy)*(1 if name in ('gas','station','black-hole') else .86)
 frames=[project(m.points,-35+i*360/32,28,fixed_scale=scale)[0] for i in range(33)]
 paths=[path_for(m.edges,f) for f in frames]
 assert paths[0]==paths[-1]
 return f'''<svg xmlns="http://www.w3.org/2000/svg" width="900" height="560" viewBox="0 0 900 560" fill="none" role="img" aria-labelledby="title" data-object="{name}"><title id="title">{name} hologram</title><desc>Transparent wireframe object. Full 3D orbit in 16 seconds.</desc><defs><filter id="glow" x="-25%" y="-25%" width="150%" height="150%"><feGaussianBlur stdDeviation="1.1"/></filter><path id="body" d="{paths[0]}"><animate attributeName="d" values="{";".join(paths)}" dur="16s" repeatCount="indefinite"/></path></defs><g stroke="{color}" stroke-linecap="round" stroke-linejoin="round"><use href="#body" opacity=".24" stroke-width="3" filter="url(#glow)"/><use href="#body" opacity=".85" stroke-width="1.3"/><use href="#body" opacity=".72" stroke-width="1.9" stroke-dasharray="10 210"><animate attributeName="stroke-dashoffset" from="0" to="-220" dur="5s" repeatCount="indefinite"/></use></g></svg>'''
if __name__=='__main__':
 dest=Path(__file__).resolve().parents[1]/'embedded-original-assets';dest.mkdir(parents=True,exist_ok=True)
 for name,(build,color) in MODELS.items():
  out=render(name,build,color)
  p=dest/('object-'+name+'.svg');p.write_text(out,encoding='utf-8');b=out.encode()
  sha=hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()
  print(p.name,len(b),sha)
