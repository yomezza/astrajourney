"""Deterministic fictitious values only; no backend, no account data."""
from datetime import datetime, timezone, timedelta
from math import sin, cos, pi

NOW = datetime(2026, 11, 1, 11, 30, tzinfo=timezone.utc)
ISO = NOW.isoformat()
STAR_NAMES = ["Эдем", "Вега-9", "Альтаир", "Полярис", "Антарес", "Денеб", "Сириус", "Арктур", "Эридан", "Электра", "Канопус", "Тау Кита", "Мира", "Регул", "Адара", "Процион", "Альдебаран", "Фомальгаут", "Лира", "Центавра"]
CLASSES = 'GAKFMBO'
REGIONS = [dict(id=i+1, name=name) for i,name in enumerate(["Кузни Сварога", "Туманность Эдема", "Предел Пилигрима", "Тихая Бездна", "Граница Атласа"])]
STARS=[]
for index in range(80):
    idx=index+1
    angle=index*2.39996323
    radius=24+index*5.2
    STARS.append(dict(id=idx,name=STAR_NAMES[index] if index < len(STAR_NAMES) else f"ED-{idx:04d}",
       **{'class':CLASSES[index%len(CLASSES)]}, region=1+index%5,
       galaxy='edem',x=round(cos(angle)*radius,1),y=round(sin(angle)*radius*.72,1),
       connections=[q for q in (idx-1,idx+1,idx+7) if 1<=q<=80]))
SITES=[
  {"id":101,"object_id":101,"name":"Эдем I","kind":"Каменистая","planet":1,"orbit":1.0,"quality":"Хорошее","concentration":42,"resources":["iron","silicon"],"quotas":{"iron":{"remaining":230,"capacity":500,"reset_at":ISO},"silicon":{"remaining":125,"capacity":400,"reset_at":ISO}}},
  {"id":102,"object_id":102,"name":"Эдем II","kind":"Садовый мир","planet":2,"orbit":1.9,"quality":"Высокое","concentration":68,"resources":["water","biomass"],"quotas":{"water":{"remaining":170,"capacity":250,"reset_at":ISO},"biomass":{"remaining":65,"capacity":200,"reset_at":ISO}}},
  {"id":103,"object_id":103,"name":"Рубиновый гигант","kind":"Газовый гигант","planet":3,"orbit":3.4,"quality":"Среднее","concentration":33,"resources":["gas"],"quotas":{"gas":{"remaining":700,"capacity":900,"reset_at":ISO}}},
  {"id":104,"object_id":104,"name":"Торговый маяк","kind":"Орбитальная станция","planet":4,"orbit":4.3,"quality":"Обычное","concentration":0,"resources":[],"quotas":{}}
]
USER={"id":7,"username":"Stellar_07","role":"admin","credits":185240,"ore":90,"is_bot":False}
PROFILE={"id":7,"username":"Stellar_07","level":8,"xp":86,"required_xp":100,"upgrade_cost":150,"company_id":3,"company":"Союз Ориона","company_tag":"ORION","company_flag":"✦","company_color":"#7ad4cb","company_flag_design":None,"company_role":1}
SPECS={"id":"agni","name":"Пилигрим","model":"pilgrim.svg","fuel":100,"cargo":80,"jump_seconds":6,"jump_fuel":3,"speed":1,"research_seconds":2}
SHIP={"id":13,"name":"Титанчик","class":"agni","spec":SPECS,"owner":7,"user_id":7,"star":1,"destination":None,"arrival":None,"departure":None,"path":[],"object_id":104,"position_mode":"docked","local_target":None,"local_started":None,"local_arrival":None,"fuel":74,"cargo":{"iron":17,"silicon":4},"capacity":80,"health":100,"shield":100,"level":2}
OTHER_SHIP={**SHIP,"id":14,"name":"Вектор-12","star":5,"cargo":{"iron":8},"fuel":88,"object_id":0,"position_mode":"system","class":"vesper","spec":{**SPECS,"name":"Веспер","model":"vesper.svg"}}
KNOWN=[{"star":idx,"received_at":(NOW-timedelta(minutes=idx*17)).isoformat(),"source":("purchased" if idx%5==0 else "shared" if idx%7==0 else "survey")} for idx in range(1,43)]
RESEARCH=[{"ship":13,"star":1,"scanned":5,"total":6,"seconds":2,"paused":False},{"ship":14,"star":5,"scanned":2,"total":7,"seconds":4,"paused":True}]
COMPANY={"id":3,"name":"Союз Ориона","tag":"ORION","owner":7,"owner_id":7,"flag":"✦","color":"#7ad4cb","credits":93240,"description":"Картография, исследования и логистика на границе Эдема."}
MEMBERS=[{"id":7,"username":"Stellar_07","company_role":1,"role_name":"Владелец"},{"id":12,"username":"Vega","company_role":2,"role_name":"Навигатор"},{"id":14,"username":"Atlas","company_role":3,"role_name":"Пилот"}]
RESOURCES=[{"id":code,"name":name,"kind":kind,"price":price} for code,name,kind,price in [("iron","Железо","Руда",5),("silicon","Кремний","Руда",9),("water","Вода","Жидкость",3),("gas","Газ","Газ",12),("biomass","Биомасса","Органика",7)]]
STATIONS=[{"star":1,"object_id":104,"name":"Торговый маяк","station_name":"Торговый маяк","owner_kind":"company","owner_tag":"ORION","owner_color":"#7ad4cb","founder_name":"Союз Ориона","fee_percent":0,"reputation":80,"goods":{},"active":True,"shipyard":True,"specialization":"Универсальная"}]
FLEET_CLASSES=[{"id":"agni","name":"Пилигрим","model":"pilgrim.svg","cargo":80,"fuel":100,"required_level":1,"jump_seconds":6,"jump_fuel":3,"price":2500}]
for class_id, name in [("wanderer","Странник"),("forge","Кузнец"),("ark","Ковчег"),("hermes","Гермес"),("vesper","Веспер"),("astra","Астра"),("argus","Аргус"),("crucible","Крусибл"),("titan","Титан"),("atlas","Атлас"),("leviathan","Левиафан"),("iris","Ирис"),("zephyr","Зефир"),("lightning","Молния")]:
    FLEET_CLASSES.append({"id":class_id,"name":name,"model":class_id+".svg","cargo":80,"fuel":100,"required_level":1,"jump_seconds":6,"jump_fuel":3,"price":2500})

# Demo-only colony data. Shape mirrors GET /api/planet/projects from Go ColonyList.
COLONY_PROJECTS = [
    {"id": 201, "surface": 3101, "sector": 12, "status": "funding", "progress": 0,
     "build_ticks": 12, "expires_at": (NOW + timedelta(days=2)).isoformat(),
     "star": 1, "planet": 2, "name": "Эдем II", "owner": "Stellar_07",
     "owner_user": 7, "owner_company": None,
     "initiator": 7, "required": {"credits": 5000, "iron": 120, "silicon": 60},
     "received": {"credits": 3400, "iron": 60, "silicon": 22}, "refund_percent": 100},
    {"id": 202, "surface": 3102, "sector": 7, "status": "building", "progress": 6,
     "build_ticks": 18, "expires_at": (NOW + timedelta(days=5)).isoformat(),
     "star": 3, "planet": 1, "name": "Альтаир I", "owner": "Союз Ориона",
     "owner_user": None, "owner_company": 3, "initiator": 12,
     "required": {"credits": 8000, "iron": 250},
     "received": {"credits": 8000, "iron": 250}, "refund_percent": 100},
]
COLONY_REFUNDS = []
# Notifications live only for this process; a deleted item must not return on refresh.
NOTICES = [
    {"id": 1, "kind": "research_complete", "message": "Исследование системы ED-0114 завершено",
     "level": "success", "created_at": ISO, "read": False},
]

def colony_detail(project_id):
    project = next((item for item in COLONY_PROJECTS if item['id'] == project_id), None)
    if project is None:
        return None
    return {"project": dict(project), "name": project['name'], "star": project['star'],
            "planet": project['planet'], "owner": project['owner'],
            "initiator_name": "Stellar_07" if project['initiator'] == 7 else "Vega",
            "contributions": [], "events": [], "can_manage": True, "can_contribute": True,
            "land_reason": "", "server_time": ISO}


def notifications():
    return {"count": len(NOTICES), "items": [dict(item) for item in NOTICES]}


def world():
    lanes=[]
    for s in STARS:
        if s['id'] < 80: lanes.append({'from':s['id'],'to':s['id']+1})
    return {"revision":1,"stars":STARS,"regions":REGIONS,"lanes":lanes,"galaxies":[{"id":"edem","name":"Эдем","opened":True,"systems":list(range(1,81)),"center":{"x":0,"y":0},"radius":800}]}

def api_data(path):
    if path in ('login','register','logout'):
        return {"ok":True}
    if path=='state':
        return {"date":"ЭД 0028 · 151 · 340","pulses":1930340,"tick_seconds":10,"user":USER,"tick":35140,"last_tick":ISO,"companies":[COMPANY],"ore_price":5}
    if path=='profile': return PROFILE
    if path=='expedition':
        return {"server_time":ISO,"job":None,"world_tick":35140,"station":True,"docked":True,"sites":SITES,"ship":SHIP,"system":STARS[0],"region":REGIONS[0]['name'],"surveyed":True,"credits":USER['credits'],"resources":RESOURCES,"offers":[],"market_credits":0,"capacity":80,"research":RESEARCH}
    if path=='fleet': return {"ships":[SHIP,OTHER_SHIP],"public_ships":[],"ship_limit":7,"server_time":ISO}
    if path=='gameplay': return {"server_time":ISO,"last_tick":ISO,"settings":{"tick_seconds":10,"rescue_cost":200,"company_cost":1000},"tick":35140,"pulses":1930340}
    if path=='workbench':
        return {"universe_revision":1,"research":RESEARCH,"known":KNOWN,"notes":[{"star":2,"object_id":None,"note":"Маршрут к торговым узлам","bookmark":True,"label":"Важное"}],
            "mining":[],"shipyards":[{"star":1,"station":104,"ship_class":"agni","price":2500,"quantity":3,"station_name":"Торговый маяк"}],
            "core_systems":[1,2,3],"portal_cores":[],"server_name":"Иггдрасиль","ship_classes":FLEET_CLASSES,"trade_profiles":[],"extractor_price":250,
            "reservation_policy":"full_refund","time":ISO,"stations":STATIONS,"orders":[],"data_offers":[{"id":1,"seller_name":"Navigator","price":350,"systems":[5,6,7],"new_systems":3}],
            "warehouses":[],"deliveries":[],"ledger":[{"id":1,"amount":2000,"balance":185240,"reason":"Продажа разведданных","created_at":ISO}],
            "members":MEMBERS,"roles":[{"id":1,"name":"Владелец","company_id":3,"permissions":{}},{"id":2,"name":"Навигатор","company_id":3,"permissions":{}}],
            "requests":[],"company_finance":[],"company_balance":93240,"company_owner":7,"company_storage":[],"projects":[],"project_contributions":[],"portal":[],"reputation":[],"population":[{"players":17,"bots":22}]}
    if path=='galaxy': return world()
    if path=='notifications': return notifications()
    if path=='planet/projects': return {"projects": [dict(p) for p in COLONY_PROJECTS], "refunds": [dict(r) for r in COLONY_REFUNDS]}
    if path=='server-status': return {"name":"Иггдрасиль","players":17,"bots":22,"online":True}
    if path=='companies': return [{**COMPANY,"members":len(MEMBERS),"founder":False,"starter":False}]
    if path=='market/global': return {"orders":[],"offers":[],"items":[],"total":0}
    if path=='logistics': return {"warehouses":[],"ships":[SHIP,OTHER_SHIP],"stations":STATIONS,"deliveries":[],"storage":[]}
    if path=='ideas': return {"items":[],"ideas":[],"total":0}
    if path.startswith('admin/'):
        return {"ok":True,"items":[],"users":[],"services":[],"history":[],"total":0}
    if path=='finance': return {"items":[],"ledger":[],"total":0}
    if path=='estimate':
        return {"path":[1],"remaining":1,"objects":6,"travel_seconds":0,"scan_seconds":12,"fuel":0,"cost":10,"return_fuel":6,"can_depart":True}
    if path=='system':
        return {"star":STARS[0],"sites":SITES,"region":REGIONS[0]['name'],"surveyed":True,"notes":[],"ports":[]}
    return None
