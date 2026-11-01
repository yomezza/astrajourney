"""Canonical fictitious object registry for the UI mirror.

Every map, research record and trade port resolves its star/object IDs here.
Do not copy this registry into a second frontend-specific mock list.
"""
from datetime import datetime, timezone, timedelta
from math import cos, sin

NOW = datetime(2026, 11, 1, 11, 30, tzinfo=timezone.utc)
ISO = NOW.isoformat()
NAMES = ['Эдем','Вега-9','Альтаир','Полярис','Антарес','Денеб','Сириус','Арктур',
         'Эридан','Электра','Канопус','Тау Кита','Мира','Регул','Адара',
         'Процион','Альдебаран','Фомальгаут','Лира','Центавра']
REGIONS = [dict(id=i+1, name=name) for i, name in enumerate(
    ['Кузни Сварога','Туманность Эдема','Предел Пилигрима','Тихая Бездна','Граница Атласа'])]
STAR_CLASSES = 'GAKFMBO'
MASS = {'O': 22.0, 'B': 6.0, 'A': 1.9, 'F': 1.25, 'G': 1.0, 'K': 0.78, 'M': 0.38}
STARS = []
for i in range(80):
    code = STAR_CLASSES[i % len(STAR_CLASSES)]
    angle = i * 2.39996323
    radius = 28 + i * 5.0
    mass = MASS[code]
    STARS.append(dict(id=i+1, name=NAMES[i] if i < len(NAMES) else f'ED-{i+1:04d}',
                      galaxy='edem', region=i%5+1, **{'class': code},
                      mass=mass, luminosity=round(mass**3.5, 3),
                      x=round(cos(angle)*radius, 1), y=round(sin(angle)*radius*.72, 1)))
STAR_BY_ID = {star['id']: star for star in STARS}
RESOURCE_INFO = [('iron','Железо','Руда',5,'Металл для строительства и производства.'),
                 ('silicon','Кремний','Руда',9,'Основа электронных компонентов.'),
                 ('water','Вода','Жидкость',3,'Вода для колоний и производства.'),
                 ('gas','Газ','Газ',12,'Газовое сырьё для реакторов.'),
                 ('biomass','Биомасса','Органика',7,'Органическое сырьё.'),
                 ('copper','Медь','Руда',8,'Проводники и электросистемы.')]
RESOURCES = [dict(id=k, name=n, kind=kind, price=price, description=description)
             for k,n,kind,price,description in RESOURCE_INFO]
KNOWN = [dict(star=i, received_at=(NOW-timedelta(minutes=i*17)).isoformat(),
              source=('purchased' if i%5==0 else 'shared' if i%7==0 else 'survey'))
         for i in range(2,49)]
KNOWN_IDS = {item['star'] for item in KNOWN}
PORT_STARS = {1, 3, 5, 11, 19}

def _site(id, name, kind, planet, orbit, resources=(), details=False):
    result = dict(id=id, object_id=id, name=name, kind=kind, planet=planet,
                  orbit=orbit, quality='Хорошее', concentration=45,
                  resources=list(resources), quotas={})
    if details:
        result['details'] = {'id': planet, 'kind': kind}
    return result

def system_sites(star_id):
    """Stable object IDs; station IDs are also used as market IDs."""
    if star_id not in STAR_BY_ID:
        return []
    if star_id == 1:
        sites = [_site(101,'Эдем I','Каменистая',1,1.0,['iron','silicon'],True),
                 _site(102,'Эдем II','Садовый мир',2,1.9,['water','biomass'],True),
                 _site(103,'Рубиновый гигант','Газовый гигант',3,3.4,['gas'],True),
                 _site(104,'Торговый маяк','Орбитальная станция',4,4.3),
                 _site(105,'Пояс Эдема','Пояс астероидов',101,5.3,['iron','copper']),
                 _site(106,'Ледяное поле','Ледяное поле',102,6.1,['water'])]
        return sites
    star = STAR_BY_ID[star_id]
    base = star_id*1000
    sites = [_site(base+1, f"{star['name']} I", 'Каменистая', 1, 1.1, ['iron'], True),
             _site(base+2, f"{star['name']} II", 'Садовый мир' if star_id%3==0 else 'Ледяная', 2, 2.2, ['water'], True),
             _site(base+3, f"Пояс {star['name']}", 'Пояс астероидов', 101, 3.8, ['iron','copper'])]
    if star_id in PORT_STARS:
        sites.append(_site(base+4, f"Маяк {star['name']}", 'Орбитальная станция', 3, 4.8))
    return sites

SYSTEM_SITES = {star['id']: system_sites(star['id']) for star in STARS}
STATIONS = []
for star_id in sorted(PORT_STARS):
    site = next(s for s in SYSTEM_SITES[star_id] if s['kind']=='Орбитальная станция')
    STATIONS.append(dict(star=star_id, object_id=site['id'], name=site['name'],
                         station_name=site['name'], owner_kind='company', owner_tag='ORION',
                         owner_color='#7ad4cb', founder_name='Союз Ориона', fee_percent=2,
                         reputation=80, goods={}, active=True, shipyard=star_id==1,
                         specialization='Универсальная' if star_id==1 else 'Промышленная'))
STATION_BY_KEY = {(s['star'], s['object_id']): s for s in STATIONS}
OFFERS = {}
for port in STATIONS:
    star_id, station_id = port['star'], port['object_id']
    OFFERS[(star_id, station_id)] = [dict(resource=r['id'], stock=95+star_id*3+j*55,
          buy=r['price']+3+(star_id+j)%4, sell=max(1,r['price']-1+(star_id+j)%2))
          for j, r in enumerate(RESOURCES)]
RESERVATIONS = []

def galaxy():
    return dict(revision=1, name='Edem', stars=STARS, regions=REGIONS,
                lanes=[{'from':i,'to':i+1} for i in range(1,80)] +
                      [{'from':i,'to':i+7} for i in range(1,73,7)],
                galaxies=[dict(id='edem',name='Эдем',opened=True,radius=700,territories=[])],
                founders=[],black_holes={})

def system(star_id, scanned=0):
    star = STAR_BY_ID.get(star_id)
    if not star:
        return None
    sites = SYSTEM_SITES[star_id]
    if star_id not in KNOWN_IDS and scanned:
        sites=sites[:max(0,scanned)]
    elif star_id not in KNOWN_IDS:
        sites=[]
    return dict(star=star, sites=sites, region=REGIONS[star['region']-1]['name'],
                surveyed=star_id in KNOWN_IDS, notes=[], ports=[])

def market_ports():
    return [dict(star=station['star'], station=station['object_id'], name=station['name'],
                 specialization=station['specialization'], offers=OFFERS[(station['star'],station['object_id'])])
            for station in STATIONS]

def export(ship_id=1):
    """Read-only projection consumed by the embedded approved research prototype."""
    return dict(revision=1, stars=STARS, regions=REGIONS, known=KNOWN,
                ship_star=ship_id, sites=SYSTEM_SITES[ship_id],
                stations=STATIONS, resources=RESOURCES, research=[])
