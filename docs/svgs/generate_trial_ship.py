"""Generate the experimental Wayfarer hologram. Python standard library only."""
from math import sin, cos, pi
from pathlib import Path

def build():
    points=[]; edges=set()
    def add(points_new, loops):
        base=len(points); points.extend(points_new)
        for loop in loops:
            for i in range(len(loop)):
                a,b=base+loop[i],base+loop[(i+1)%len(loop)]
                edges.add(tuple(sorted((a,b))))
    def loft(sections,n=6):
        new=[]
        for x,y,z,w,h in sections:
            for i in range(n):
                a=2*pi*i/n+pi/n
                new.append((x+w*cos(a),y,z+h*sin(a)))
        loops=[]
        for j in range(len(sections)-1):
            for k in range(n):
                a=j*n+k; b=j*n+(k+1)%n
                loops.append((a,b,b+n,a+n))
        add(new,loops)
    def prism(poly,z0,z1):
        n=len(poly)
        p=[(x,y,z0) for x,y in poly]+[(x,y,z1) for x,y in poly]
        add(p,[(i,(i+1)%n,(i+1)%n+n,i+n) for i in range(n)])
    loft([(0,-4.7,0,.14,.16),(0,-3.5,0,.45,.32),(0,-2.1,0,.95,.55),(0,-.6,0,1.18,.70),(0,1.2,0,1.10,.64),(0,2.7,0,.82,.50),(0,3.8,0,.40,.28)],8)
    loft([(0,-2.1,.60,.2,.1),(0,-1.3,.76,.36,.12),(0,-.2,.68,.42,.12)],4)
    for s in (-1,1):
        prism([(s*.65,-1.3),(s*2.55,-.1),(s*2.15,2.1),(s*.72,1.15)],-.12,.08)
        loft([(s*1.75,-1,.10,.28,.26),(s*2.15,-.2,.15,.22,.22)])
        loft([(s*2.05,1,-.05,.30,.30),(s*2.05,2.65,-.05,.33,.33)])
        loft([(s*2.05,2.68,-.05,.24,.24),(s*2.05,3.12,-.05,.24,.24)])
        prism([(s*.25,2),(s*.85,3.3),(s*.45,3.55),(s*.05,2.35)],-.34,-.06)
    return points,sorted(edges)

def project(points,yaw,el=28):
    yaw=yaw*pi/180; el=el*pi/180
    return [(450+(x*cos(yaw)-y*sin(yaw))*49,
             280+((x*sin(yaw)+y*cos(yaw))*sin(el)-z*cos(el))*49)
            for x,y,z in points]

def path(points,edges):
    return ''.join('M%.1f %.1fL%.1f %.1f'%(points[a]+points[b]) for a,b in edges)

def generate():
    points,edges=build()
    frames=[path(project(points,-35+i*360/32),edges) for i in range(33)]
    assert frames[0]==frames[-1]
    values=';'.join(frames)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="900" height="560" viewBox="0 0 900 560" data-idle-viewbox="0 0 900 560" data-moving-viewbox="0 0 900 560" data-length="36" role="img" fill="none" aria-labelledby="wayfarer-title wayfarer-desc">
<title id="wayfarer-title">Wayfarer prototype</title><desc id="wayfarer-desc">Experimental three-dimensional wireframe hologram, rotating once every 16 seconds.</desc>
<defs><filter id="wayfarer-glow" x="-25%" y="-25%" width="150%" height="150%"><feGaussianBlur stdDeviation="1.2"/></filter>
<path id="wayfarer-body" d="{frames[0]}"><animate attributeName="d" values="{values}" dur="16s" repeatCount="indefinite" calcMode="linear"/></path></defs>
<g stroke-linejoin="round" stroke-linecap="round">
<use href="#wayfarer-body" stroke="#78e0c7" stroke-width="3.3" opacity=".26" filter="url(#wayfarer-glow)"/>
<use href="#wayfarer-body" stroke="#78e0c7" stroke-width="1.4" opacity=".92"/>
<use href="#wayfarer-body" stroke="#c7fff2" stroke-width="1.8" stroke-dasharray="16 190" opacity=".8"><animate attributeName="stroke-dashoffset" from="0" to="-206" dur="5s" repeatCount="indefinite"/></use>
</g>
<g class="engine-thrust" visibility="hidden" stroke="#a6ffe3"><use href="#wayfarer-body" opacity=".12" stroke-dasharray="4 18"><animate attributeName="stroke-dashoffset" from="0" to="-22" dur=".8s" repeatCount="indefinite"/></use></g>
</svg>'''

if __name__=="__main__":
    dest=Path(__file__).with_name("wayfarer-proto.svg")
    dest.write_text(generate(),encoding="utf-8")
    print(dest)
