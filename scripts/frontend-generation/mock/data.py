"""Deterministic fictitious values only; no backend, no account data."""
from datetime import datetime, timezone, timedelta
from math import sin, cos, pi

NOW = datetime(2026, 11, 1, 11, 30, tzinfo=timezone.utc)
ISO = NOW.isoformat()
from mock import registry as objects
NOW = objects.NOW
ISO = objects.ISO
REGIONS = objects.REGIONS
STARS = objects.STARS
SITES = objects.SYSTEM_SITES[1]
KNOWN = objects.KNOWN
RESOURCES = objects.RESOURCES
STATIONS = objects.STATIONS

USER={"id":7,"username":"Stellar_07","role":"admin","credits":185240,"ore":90,"is_bot":False}
PROFILE={"id":7,"username":"Stellar_07","level":8,"xp":86,"required_xp":100,"upgrade_cost":150,"company_id":3,"company":"Союз Ориона","company_tag":"ORION","company_flag":"✦","company_color":"#7ad4cb","company_flag_design":None,"company_role":1}
SPECS={"id":"agni","name":"Пилигрим","model":"pilgrim.svg","fuel":100,"cargo":80,"jump_seconds":6,"jump_fuel":3,"speed":1,"research_seconds":2}
SHIP={"id":13,"name":"Титанчик","class":"agni","spec":SPECS,"owner":7,"user_id":7,"star":1,"destination":None,"arrival":None,"departure":None,"path":[],"object_id":104,"position_mode":"docked","local_target":None,"local_started":None,"local_arrival":None,"fuel":74,"cargo":{"iron":17,"silicon":4},"capacity":80,"health":100,"shield":100,"level":2}
OTHER_SHIP={**SHIP,"id":14,"name":"Вектор-12","star":5,"cargo":{"iron":8},"fuel":88,"object_id":0,"position_mode":"system","class":"vesper","spec":{**SPECS,"name":"Веспер","model":"vesper.svg"}}
RESEARCH=[{"ship":13,"star":1,"scanned":5,"total":6,"seconds":2,"paused":False},{"ship":14,"star":5,"scanned":2,"total":7,"seconds":4,"paused":True}]
COMPANY={"id":3,"name":"Союз Ориона","tag":"ORION","owner":7,"owner_id":7,"flag":"✦","color":"#7ad4cb","credits":93240,"description":"Картография, исследования и логистика на границе Эдема."}
MEMBERS=[{"id":7,"username":"Stellar_07","company_role":1,"role_name":"Владелец"},{"id":12,"username":"Vega","company_role":2,"role_name":"Навигатор"},{"id":14,"username":"Atlas","company_role":3,"role_name":"Пилот"}]
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
    {"id": 1, "kind": "research_complete", "message": "Исследование системы ED-0048 завершено",
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
    return objects.galaxy()

PLAYER_ORDERS = [
    {"id":301,"star":1,"station":104,"resource":"iron","quantity":40,
     "price":8,"seller":12,"seller_name":"Vega"},
    {"id":302,"star":3,"station":3004,"resource":"silicon","quantity":25,
     "price":13,"seller":14,"seller_name":"Atlas"},
]

def api_data(path):
    if path in ('login','register','logout'):
        return {"ok":True}
    if path=='state':
        return {"date":"ЭД 0028 · 151 · 340","pulses":1930340,"tick_seconds":10,"user":USER,"tick":35140,"last_tick":ISO,"companies":[COMPANY],"ore_price":5}
    if path=='profile': return PROFILE
    if path=='expedition':
        return {"server_time":ISO,"job":None,"world_tick":35140,"station":True,"docked":True,"sites":SITES,"ship":SHIP,"system":STARS[0],"region":REGIONS[0]['name'],"surveyed":True,"credits":USER['credits'],"resources":RESOURCES,"offers":objects.OFFERS[(1,104)],"market_credits":92500,"capacity":80,"research":RESEARCH}
    if path=='fleet': return {"ships":[SHIP,OTHER_SHIP],"public_ships":[],"ship_limit":7,"server_time":ISO}
    if path=='gameplay': return {"server_time":ISO,"last_tick":ISO,"settings":{"tick_seconds":10,"rescue_cost":200,"company_cost":1000},"tick":35140,"pulses":1930340}
    if path=='workbench':
        return {"universe_revision":1,"research":RESEARCH,"known":KNOWN,"notes":[{"star":2,"object_id":None,"note":"Маршрут к торговым узлам","bookmark":True,"label":"Важное"}],
            "mining":[],"shipyards":[{"star":1,"station":104,"ship_class":"agni","price":2500,"quantity":3,"station_name":"Торговый маяк"}],
            "core_systems":[1,2,3],"portal_cores":[],"server_name":"Иггдрасиль","ship_classes":FLEET_CLASSES,"trade_profiles":[],"extractor_price":250,
            "reservation_policy":"full_refund","time":ISO,"stations":STATIONS,"orders":PLAYER_ORDERS,"data_offers":[{"id":1,"seller":12,"seller_name":"Navigator","star":1,"station":104,"region":1,"created_at":ISO,"price":350,"systems":[5,6,7],"new_systems":3}],
            "warehouses":[],"deliveries":[],"ledger":[{"id":1,"amount":2000,"balance":185240,"reason":"Продажа разведданных","created_at":ISO}],
            "members":MEMBERS,"roles":[{"id":1,"name":"Владелец","company_id":3,"permissions":{}},{"id":2,"name":"Навигатор","company_id":3,"permissions":{}}],
            "requests":[],"company_finance":[],"company_balance":93240,"company_owner":7,"company_storage":[],"projects":[],"project_contributions":[],"portal":[],"reputation":[],"population":[{"players":17,"bots":22}]}
    if path=='galaxy': return world()
    if path=='notifications': return notifications()
    if path=='planet/projects': return {"projects": [dict(p) for p in COLONY_PROJECTS], "refunds": [dict(r) for r in COLONY_REFUNDS]}
    if path=='server-status': return {"name":"Иггдрасиль","players":17,"bots":22,"online":True}
    if path=='companies': return [{**COMPANY,"members":len(MEMBERS),"founder":False,"starter":False}]
    if path=='market/global': return {'ports': objects.market_ports(), 'reservations': objects.RESERVATIONS}
    if path=='logistics': return {"warehouses":[],"ships":[SHIP,OTHER_SHIP],"stations":STATIONS,"deliveries":[],"storage":[]}
    if path=='ideas': return {"items":[],"ideas":[],"total":0}
    if path.startswith('admin/'):
        return {"ok":True,"items":[],"users":[],"services":[],"history":[],"total":0}
    if path=='finance': return {"items":[],"ledger":[],"total":0}
    if path=='estimate':
        return {"path":[1],"remaining":1,"objects":6,"travel_seconds":0,"scan_seconds":12,"fuel":0,"cost":10,"return_fuel":6,"can_depart":True}
    if path=='system': return objects.system(1, scanned=5)
    return None
