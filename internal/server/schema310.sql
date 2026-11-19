-- Data-driven equipment. Catalog edits affect future purchases, not owned snapshots.
CREATE TABLE IF NOT EXISTS ship_module_catalog (
 id TEXT PRIMARY KEY CHECK(id ~ '^[A-Za-z0-9_-]{1,80}$'),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 kind TEXT NOT NULL CHECK(kind IN('extractor','cargo','scanner','scoop','drive')),
 rating TEXT NOT NULL CHECK(rating IN('C','B','A','S')),
 size INTEGER NOT NULL CHECK(size BETWEEN 1 AND 64),
 price BIGINT NOT NULL CHECK(price BETWEEN 1 AND 1000000000),
 effects JSONB NOT NULL CHECK(jsonb_typeof(effects)='object'),
 description TEXT NOT NULL DEFAULT '', enabled BOOLEAN NOT NULL DEFAULT TRUE,
 CHECK(kind<>'cargo' OR rating='C'), CHECK(kind<>'scoop' OR rating IN('C','A','S'))
);
CREATE TABLE IF NOT EXISTS ship_module_hulls (
 class TEXT PRIMARY KEY, capacity INTEGER NOT NULL CHECK(capacity BETWEEN 3 AND 256),
 mining_rating TEXT NOT NULL CHECK(mining_rating IN('C','B'))
);
CREATE TABLE IF NOT EXISTS ship_modules (
 id BIGSERIAL PRIMARY KEY, ship_id BIGINT NOT NULL REFERENCES ships(id) ON DELETE CASCADE,
 catalog_id TEXT NOT NULL REFERENCES ship_module_catalog(id),
 definition JSONB NOT NULL CHECK(jsonb_typeof(definition)='object'),
 installed BOOLEAN NOT NULL DEFAULT FALSE, purchased_price BIGINT NOT NULL DEFAULT 0 CHECK(purchased_price>=0)
);
CREATE INDEX IF NOT EXISTS ship_modules_ship ON ship_modules(ship_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS ship_modules_singleton ON ship_modules(ship_id,(definition->>'kind'),(CASE WHEN definition->>'kind'='extractor' THEN definition->'effects'->>'extractor' ELSE '' END)) WHERE installed AND definition->>'kind'<>'cargo';
CREATE TABLE IF NOT EXISTS station_module_stock (
 star INTEGER NOT NULL, station TEXT NOT NULL, catalog_id TEXT NOT NULL REFERENCES ship_module_catalog(id),
 quantity INTEGER NOT NULL CHECK(quantity>=0), price BIGINT NOT NULL CHECK(price BETWEEN 1 AND 1000000000),
 enabled BOOLEAN NOT NULL DEFAULT TRUE, PRIMARY KEY(star,station,catalog_id),
 FOREIGN KEY(star,station) REFERENCES stations(star,object_id) ON DELETE CASCADE
);
ALTER TABLE ships ADD COLUMN IF NOT EXISTS modules_initialized BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ships ADD COLUMN IF NOT EXISTS scoop_ready_at TIMESTAMPTZ NOT NULL DEFAULT '2000-01-01';

INSERT INTO ship_module_catalog(id,name,kind,rating,size,price,effects,description)
SELECT 'extractor-'||t.id||'-'||g.rating,t.name,'extractor',g.rating,2+g.ordinal,g.price,
 jsonb_build_object('extractor',t.id,'yield_bonus',g.ordinal), 'Один добытчик каждого подтипа. Для ресурса автоматически используется наиболее эффективный совместимый модуль.'
FROM (VALUES('universal','Универсальный добытчик'),('ore','Рудный бур'),('ice','Ледовый добытчик'),('gas','Газовый добытчик')) AS t(id,name)
CROSS JOIN (VALUES('C',0,800),('B',1,3500),('A',2,15000),('S',3,60000)) AS g(rating,ordinal,price)
ON CONFLICT DO NOTHING;
INSERT INTO ship_module_catalog(id,name,kind,rating,size,price,effects,description)
SELECT 'cargo-'||s.size||'-C','Грузовой расширитель','cargo','C',s.size,s.size*1200,
 jsonb_build_object('cargo_bonus',s.size*10),'Дополнительный трюм. Выпускается только в классе C.'
FROM (VALUES(1),(2),(3),(5),(8)) AS s(size) ON CONFLICT DO NOTHING;
INSERT INTO ship_module_catalog(id,name,kind,rating,size,price,effects,description)
SELECT 'scanner-'||g.rating,'Системный сканер','scanner',g.rating,g.size,g.price,
 jsonb_build_object('scan_percent',g.percent),'Сокращает интервал сканирования объектов системы.'
FROM (VALUES('C',1,1500,10),('B',2,6000,20),('A',3,24000,35),('S',5,90000,50)) AS g(rating,size,price,percent) ON CONFLICT DO NOTHING;
INSERT INTO ship_module_catalog(id,name,kind,rating,size,price,effects,description)
SELECT 'scoop-'||g.rating,'Топливозаборник','scoop',g.rating,g.size,g.price,
 jsonb_build_object('scoop_rate',g.rate),'Сбор звёздного ветра в открытом космосе. Один цикл раз в 10 секунд.'
FROM (VALUES('C',2,2500,2),('A',4,20000,5),('S',6,80000,10)) AS g(rating,size,price,rate) ON CONFLICT DO NOTHING;
INSERT INTO ship_module_catalog(id,name,kind,rating,size,price,effects,description)
SELECT 'drive-'||g.rating,'Ускоритель перехода','drive',g.rating,g.size,g.price,
 jsonb_build_object('jump_percent',g.percent),'Сокращает длительность гиперперехода, не меняя его стоимость в топливе.'
FROM (VALUES('C',2,3500,10),('B',3,10000,20),('A',4,35000,35),('S',6,110000,50)) AS g(rating,size,price,percent) ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION ship_module_initial_loadout() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO ship_modules(ship_id,catalog_id,definition,installed)
 SELECT NEW.id,c.id,(to_jsonb(c)-'enabled'-'id')||jsonb_build_object('catalog_id',c.id),TRUE FROM ship_module_catalog c
 WHERE c.id='cargo-1-C' OR c.id='extractor-'||NEW.extractor||'-'||COALESCE((SELECT mining_rating FROM ship_module_hulls WHERE class=NEW.class),'C');
 UPDATE ships SET modules_initialized=TRUE WHERE id=NEW.id;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS ship_module_initial_loadout ON ships;
CREATE TRIGGER ship_module_initial_loadout AFTER INSERT ON ships FOR EACH ROW EXECUTE FUNCTION ship_module_initial_loadout();

-- Apply the shared game version once; preserve operator edits on later restarts.
INSERT INTO game_release(id,version,migration) VALUES(1,'0.31.0',310)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=clock_timestamp()
 WHERE game_release.migration<excluded.migration;
