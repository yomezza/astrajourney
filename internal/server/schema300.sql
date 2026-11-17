-- New founder placeholders never rename or appropriate a player's corporation.
DO $$ DECLARE ordinal INTEGER; stable_key TEXT; display_name TEXT; candidate_tag TEXT; suffix INTEGER; BEGIN
 FOR ordinal IN 3..4 LOOP
  stable_key:='founder-0'||ordinal;
  IF NOT EXISTS(SELECT 1 FROM companies WHERE founder_key=stable_key) THEN
   candidate_tag:='F0'||ordinal;suffix:=0;
   WHILE EXISTS(SELECT 1 FROM companies WHERE tag=candidate_tag) LOOP
    suffix:=suffix+1;
    IF suffix>65535 THEN RAISE EXCEPTION 'No free temporary founder tag'; END IF;
    candidate_tag:=upper(lpad(to_hex(suffix),4,'0'));
   END LOOP;
   display_name:='Корпорация-основатель 0'||ordinal;suffix:=1;
   WHILE EXISTS(SELECT 1 FROM companies WHERE name=display_name) LOOP
    suffix:=suffix+1;display_name:='Корпорация-основатель 0'||ordinal||' · '||suffix;
   END LOOP;
   INSERT INTO companies(name,tag,color,flag,founder,founder_key)
    VALUES(display_name,candidate_tag,CASE WHEN ordinal=3 THEN '#d3b47d' ELSE '#86bfa3' END,CASE WHEN ordinal=3 THEN '◈' ELSE '⬡' END,TRUE,stable_key);
  END IF;
 END LOOP;
END $$;

-- A release bump is applied once. Operator edits survive ordinary restarts.
INSERT INTO game_release(id,version,migration) VALUES(1,'0.30.0',300)
 ON CONFLICT(id) DO UPDATE SET version=excluded.version,migration=excluded.migration,updated_at=clock_timestamp()
 WHERE game_release.migration<excluded.migration;
