from collections import defaultdict
from math import cos,pi,radians,sin
WIDTH,HEIGHT=900,560
class Mesh:
 def __init__(self):self.points=[];self.faces=[]
 def loft(self,sections,sides=6,part='hull'):
  start=len(self.points)
  for x,y,z,width,height in sections:
   for i in range(sides):
    angle=2*pi*i/sides+pi/sides
    self.points.append((x+width*cos(angle),y,z+height*sin(angle)))
  faces=[list(range(start,start+sides))]
  for j in range(len(sections)-1):
   for i in range(sides):
    a=start+j*sides+i;b=start+j*sides+(i+1)%sides
    faces.append([a,b,b+sides,a+sides])
  last=start+(len(sections)-1)*sides
  faces.append(list(range(last,last+sides)))
  self.add_faces(faces,start,part)
 def prism(self,polygon,bottom,top,part='wing'):
  start,count=len(self.points),len(polygon)
  self.points.extend((x,y,bottom) for x,y in polygon)
  self.points.extend((x,y,top) for x,y in polygon)
  faces=[list(range(start,start+count)),list(range(start+count,start+count*2))]
  for i in range(count):
   a=start+i;b=start+(i+1)%count
   faces.append([a,b,b+count,a+count])
  self.add_faces(faces,start,part)
 def add_faces(self,faces,start,part):
  centre=tuple(sum(p[i] for p in self.points[start:])/(len(self.points)-start) for i in range(3))
  for face in faces:
   a,b,c=(self.points[index] for index in face[:3])
   u,v=[b[i]-a[i] for i in range(3)],[c[i]-a[i] for i in range(3)]
   normal=(u[1]*v[2]-u[2]*v[1],u[2]*v[0]-u[0]*v[2],u[0]*v[1]-u[1]*v[0])
   fc=tuple(sum(self.points[index][i] for index in face)/len(face) for i in range(3))
   if sum(normal[i]*(fc[i]-centre[i]) for i in range(3))<0:normal=tuple(-n for n in normal)
   self.faces.append((face,normal,part))
 def edges(self):
  edges=defaultdict(list)
  for face,normal,part in self.faces:
   for i,a in enumerate(face):
    b=face[(i+1)%len(face)]
    edges[tuple(sorted((a,b)))].append((normal,part))
  return edges

def pilgrim(dense=False):
 m=Mesh();sections=[(0,-4.1,.12,.12,.16),(0,-2.6,.12,.65,.43),(0,-1,.12,1,.55),(0,1.6,.12,1.03,.52),(0,3.1,.12,.73,.40)]
 if dense:sections=[sections[0],sections[1],(0,-1.8,.12,.82,.49),sections[2],(0,.1,.12,1.01,.54),(0,.9,.12,1.02,.53),sections[3],(0,2.4,.12,.87,.46),sections[4]]
 m.loft(sections,sides=8 if dense else 6)
 for sign in (-1,1):
  m.prism([(sign*.8,-.8),(sign*2.25,.5),(sign*2.25,2.3),(sign*.8,1.8)],-.12,.12)
  pods=[(sign*1.9,-.35,0,.40,.42),(sign*1.9,2.8,0,.40,.42)]
  if dense:pods.insert(1,(sign*1.9,1.0,0,.40,.42))
  m.loft(pods,sides=8 if dense else 6,part='pods')
  m.loft([(sign*1.9,2.83,0,.31,.32),(sign*1.9,3.25,0,.31,.32)],sides=6,part='engine')
 m.loft([(0,-2.6,.60,.25,.15),(0,-1.4,.73,.40,.18)],sides=4,part='canopy')
 return m

def raw_projection(points,yaw,elevation):
 yaw,elevation=radians(yaw),radians(elevation)
 xy=[(x*cos(yaw)-y*sin(yaw),(x*sin(yaw)+y*cos(yaw))*sin(elevation)-z*cos(elevation)) for x,y,z in points]
 view=(sin(yaw)*cos(elevation),cos(yaw)*cos(elevation),sin(elevation))
 return xy,view

def path_for(edges,xy):
 return ''.join(f'M{xy[a][0]:.1f} {xy[a][1]:.1f}L{xy[b][0]:.1f} {xy[b][1]:.1f}' for a,b in edges)

def project(points,yaw,elevation,fixed_scale=None):
 xy,view=raw_projection(points,yaw,elevation)
 xs,ys=[p[0] for p in xy],[p[1] for p in xy]
 scale=fixed_scale or min(700/(max(xs)-min(xs)),380/(max(ys)-min(ys)))
 cx,cy=(min(xs)+max(xs))/2,(min(ys)+max(ys))/2
 return [(WIDTH/2+(x-cx)*scale,HEIGHT/2+(y-cy)*scale) for x,y in xy],view
