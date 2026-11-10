#!/usr/bin/env python3
"""N2 DBA: isolated PostgreSQL 17 tests, never a game deployment. Reproducible CI.
All SQL writes are restricted to freshly initialized nats_dba_test* schemas
in a database whose name ends in _test.
"""
import hashlib
import json
import os
import pathlib
import statistics
import subprocess
import sys
import time
import traceback

import psycopg

ROOT = pathlib.Path(__file__).resolve().parents[3]
HERE = pathlib.Path(__file__).resolve().parent
RESULTS = HERE / "results"
RESULTS.mkdir(exist_ok=True)
UP = ROOT / "docs/ideas/nats-event-bus/dba/sql/notify-up.example.sql"
DOWN = ROOT / "docs/ideas/nats-event-bus/dba/sql/notify-down.example.sql"
DSN = os.environ["TEST_DATABASE_URL"]
SCHEMA = "nats_dba_test"
SECOND = "nats_dba_test_b"
CHANNEL = "cosmos_chat_" + hashlib.md5(SCHEMA.encode()).hexdigest()
OTHER_CHANNEL = "cosmos_chat_" + hashlib.md5(SECOND.encode()).hexdigest()
STATUSES = {f"D{i:02d}": {"status": "NOT RUN", "reason": "not attempted"} for i in range(1, 27)}
EXTRAS = {"database": "", "version": "", "run": "GitHub Actions disposable PostgreSQL 17", "guard": {}, "bench": {}}


def require_test():
    with psycopg.connect(DSN, autocommit=True) as c:
        db, ver = c.execute("SELECT current_database(),version()").fetchone()
        if not db.endswith("_test"):
            raise RuntimeError("Refusing non-test database")
        EXTRAS["database"], EXTRAS["version"] = db, ver
        for schema in (SCHEMA, SECOND):
            count = c.execute(
                "SELECT count(*) FROM pg_namespace WHERE nspname=%s", (schema,)
            ).fetchone()[0]
            if count != 1:
                raise RuntimeError("Test schema missing: " + schema)
        return db


def connect(schema=SCHEMA, role=None):
    if schema not in (SCHEMA, SECOND):
        raise RuntimeError("Non-test schema")
    c = psycopg.connect(DSN, autocommit=True)
    c.execute("SET search_path TO " + schema + ",pg_catalog")
    if c.execute("SELECT current_schema()").fetchone()[0] != schema:
        c.close()
        raise RuntimeError("search_path did not select test schema")
    if role is not None:
        if role != "chat_runtime":
            raise RuntimeError("Only restricted role accepted")
        c.execute("SET ROLE chat_runtime")
    return c


def sqlfile(path, schema=SCHEMA, should_succeed=True):
    cmd = [
        "psql", "-X", "-q", "--single-transaction", "-v", "ON_ERROR_STOP=1",
        DSN, "-c", "SET search_path TO " + schema + ", pg_catalog",
        "-c", "SET lock_timeout='2s'; SET statement_timeout='10s'",
        "-f", str(path)
    ]
    run = subprocess.run(cmd, capture_output=True, text=True, timeout=35)
    if should_succeed and run.returncode:
        raise RuntimeError("psql file failed: " + run.stderr[-1200:])
    if not should_succeed and not run.returncode:
        raise AssertionError("psql unexpectedly accepted conflicting function")
    return {"exit": run.returncode, "stderr": run.stderr[-1200:], "stdout": run.stdout[-1200:]}


def listen(schema=SCHEMA):
    c = connect(schema)
    channel = CHANNEL if schema == SCHEMA else OTHER_CHANNEL
    c.execute('LISTEN "' + channel + '"')
    return c


def recv(c, timeout=1.0):
    return next(c.notifies(timeout=timeout, stop_after=1), None)


def no_signal(c, seconds=0.16):
    return recv(c, seconds) is None


def signal(c, seconds=2.0):
    n = recv(c, seconds)
    if n is None or n.payload != "":
        raise AssertionError("Expected empty NOTIFY after COMMIT, got " + repr(n))
    return n


def put(c, kind, schema=SCHEMA):
    return c.execute(
        "INSERT INTO chat_outbox(event_kind) VALUES(%s) RETURNING id,created_at",
        (kind,)
    ).fetchone()


def case(cid, fn):
    start = time.perf_counter()
    try:
        detail = fn() or {}
        STATUSES[cid] = {
            "status": "PASS", "elapsed_seconds": round(time.perf_counter() - start, 3),
            "evidence": detail,
        }
        print(cid, "PASS", detail, flush=True)
    except Exception as exc:
        STATUSES[cid] = {
            "status": "FAIL", "elapsed_seconds": round(time.perf_counter() - start, 3),
            "reason": repr(exc), "traceback": traceback.format_exc()[-1800:],
        }
        print(cid, "FAIL", repr(exc), flush=True)


def notrun(cid, reason):
    STATUSES[cid] = {"status": "NOT RUN", "reason": reason}


def run_guard():
    with connect() as c:
        original = c.execute(
            "SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON p.pronamespace=n.oid "
            "WHERE n.nspname=current_schema() AND p.proname='chat_outbox_notify_statement_v1'"
        ).fetchone()[0]
        if original:
            raise AssertionError("Expected clean dedicated DB schema")
        # Explicitly try a foreign function with the reserved signature.
        c.execute(
            "CREATE FUNCTION chat_outbox_notify_statement_v1() RETURNS trigger "
            "LANGUAGE plpgsql AS $foreign$ BEGIN RETURN NULL; END $foreign$"
        )
        before = c.execute(
            "SELECT prosrc FROM pg_proc WHERE oid='chat_outbox_notify_statement_v1()'::regprocedure"
        ).fetchone()[0]
        try:
            conflict = sqlfile(UP, should_succeed=False)
            after = c.execute(
                "SELECT prosrc FROM pg_proc WHERE oid='chat_outbox_notify_statement_v1()'::regprocedure"
            ).fetchone()[0]
            if before != after:
                raise AssertionError("Foreign trigger function overwritten")
        finally:
            c.execute("DROP FUNCTION chat_outbox_notify_statement_v1() RESTRICT")
        sqlfile(UP)
        sqlfile(UP)  # idempotence
        fn = c.execute(
            "SELECT p.prosecdef, p.proconfig, pg_get_userbyid(p.proowner) "
            "FROM pg_proc p WHERE p.oid='chat_outbox_notify_statement_v1()'::regprocedure"
        ).fetchone()
        if fn[0] or fn[1] != ["search_path=pg_catalog"]:
            raise AssertionError("Function security changed: " + repr(fn))
        EXTRAS["guard"] = {
            "foreign_function_preserved": True, "idempotent_up": True,
            "prosecdef": fn[0], "proconfig": fn[1], "function_owner": fn[2],
            "expected_trigger": c.execute(
                "SELECT pg_get_triggerdef(oid) FROM pg_trigger "
                "WHERE tgrelid='chat_outbox'::regclass AND tgname='chat_outbox_wakeup_a2'"
            ).fetchone()[0]
        }
        return EXTRAS["guard"]


def d01():
    with connect() as w, listen() as l, connect() as observer:
        w.execute("BEGIN")
        rid, _ = put(w, "dba.d01")
        assert observer.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0] == 0
        assert no_signal(l), "NOTIFY before commit"
        w.execute("COMMIT")
        signal(l)
        assert observer.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0] == 1
        return {"committed_id": rid}


def d02():
    with connect() as w, listen() as l, connect() as observer:
        w.execute("BEGIN")
        rid, _ = put(w, "dba.d02.rollback")
        w.execute("ROLLBACK")
        assert no_signal(l, 0.3)
        assert observer.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0] == 0
        return {"rolled_back_id": rid}


def d03():
    with connect() as w, listen() as l:
        w.execute("BEGIN")
        for i in range(3):
            put(w, "dba.d03." + str(i))
        w.execute("COMMIT")
        signal(l)
        assert no_signal(l, 0.35), "Multiple identical PG NOTIFY delivered for one tx"
        return {"insert_statements": 3, "delivered_notifies": 1}


def d04():
    with connect() as w, listen() as l:
        n = w.execute("INSERT INTO chat_outbox(event_kind) "
            "SELECT 'dba.d04' FROM generate_series(1,100)").rowcount
        signal(l)
        assert no_signal(l, 0.25)
        assert n == 100
        return {"rows": n, "notifies": 1}


def d05():
    with connect() as w, listen() as l:
        before = w.execute("SELECT count(*) FROM chat_outbox").fetchone()[0]
        w.execute("INSERT INTO chat_outbox(event_kind) "
                  "SELECT 'dba.d05' WHERE FALSE")
        signal(l)
        after = w.execute("SELECT count(*) FROM chat_outbox").fetchone()[0]
        assert after == before
        return {"rows_inserted": 0, "false_wakeup": 1}


def d06():
    with connect() as w, listen() as a, listen() as b:
        put(w, "dba.d06")
        x, y = signal(a), signal(b)
        assert x.channel == y.channel == CHANNEL
        return {"listeners": 2, "channel": CHANNEL}


def late_test(delay):
    with connect() as a, connect() as b, connect() as check:
        a.execute("BEGIN")
        low, created = put(a, "dba.late." + str(delay))
        high, _ = put(b, "dba.higher." + str(delay))
        assert low < high
        water = check.execute("SELECT max(id) FROM chat_outbox").fetchone()[0]
        assert water >= high
        t0 = time.monotonic()
        if delay:
            time.sleep(delay)
        age = time.monotonic() - t0
        a.execute("COMMIT")
        ids = {r[0] for r in check.execute(
            "SELECT id FROM chat_outbox WHERE id>%s "
            "OR created_at >= clock_timestamp()-interval '60 seconds'",
            (water,)
        )}
        if delay:
            assert age > 60 and low not in ids
        else:
            assert low in ids
        return {"lower_id": low, "higher_id": high,
                "visible_highwater": water, "lower_returned": low in ids,
                "hold_seconds": round(age, 3), "created_at": created.isoformat()}


def d09():
    # PG17 transaction_timeout is a test-only session setting, not a production recommendation.
    with connect() as checker:
        x = connect()
        try:
            x.execute("SET transaction_timeout='3s'")
            x.execute("BEGIN")
            rid, _ = put(x, "dba.d09.timeout")
            time.sleep(3.8)
            failed = False
            try:
                x.execute("COMMIT")
            except psycopg.Error:
                failed = True
            assert failed, "PostgreSQL did not enforce transaction_timeout"
            assert checker.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0] == 0
            return {"transaction_timeout": "3s", "timed_out_id": rid}
        finally:
            x.close()


def d10():
    with connect() as w, connect() as l:
        l.execute("BEGIN")
        l.execute('LISTEN "' + CHANNEL + '"')
        l.execute("COMMIT")
        before = l.execute("SELECT max(id) FROM chat_outbox").fetchone()[0]
        rid, _ = put(w, "dba.d10")
        signal(l)
        assert rid > before
        return {"listen_committed_before_snapshot": True, "id": rid}


def d11():
    with listen() as l:
        assert l is not None
    with connect() as w:
        rid, _ = put(w, "dba.d11.missed")
    with listen() as l:
        assert no_signal(l), "Reconnected listener replayed old notification"
        found = l.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0]
        assert found == 1
        return {"reconnect_initial_scan_found_id": rid}


def d12():
    with connect() as w, listen() as l:
        l.execute("UNLISTEN *")
        rid, _ = put(w, "dba.d12.not_listening")
        assert no_signal(l, 0.2)
        found = l.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0]
        assert found == 1
        l.execute('LISTEN "' + CHANNEL + '"')
        newer, _ = put(w, "dba.d12.resubscribe")
        signal(l)
        return {"missed_by_unlisten": rid, "resubscribed": newer}


def d13():
    with connect() as c:
        before = c.execute("SELECT COALESCE(max(id),0) FROM chat_outbox").fetchone()[0]
        c.execute("INSERT INTO chat_outbox(event_kind) "
                  "SELECT 'dba.d13.burst' FROM generate_series(1,2300)")
        maxid, cutoff = c.execute(
            "SELECT COALESCE(max(id),0),clock_timestamp()-interval '60 seconds' FROM chat_outbox"
        ).fetchone()
        cursor = before
        ids = []
        sizes = []
        while True:
            page = c.execute(
                "SELECT id FROM chat_outbox WHERE id>%s AND id<=%s "
                "AND (id>%s OR created_at>=%s) ORDER BY id LIMIT 1000",
                (cursor, maxid, before, cutoff)
            ).fetchall()
            sizes.append(len(page))
            ids.extend(x[0] for x in page)
            if not page or len(page) < 1000:
                break
            cursor = page[-1][0]
        if len(ids) != 2300 or len(set(ids)) != 2300:
            raise AssertionError(f"Pagination dropped or duplicated: {len(ids)}")
        return {"fetched": len(ids), "page_sizes": sizes}


def d14():
    with connect(role="chat_runtime") as c, connect() as admin, listen() as l:
        grants = admin.execute(
          "SELECT has_table_privilege('chat_runtime','chat_outbox','INSERT'), "
          "has_table_privilege('chat_runtime','chat_outbox','TRIGGER'), "
          "has_schema_privilege('chat_runtime',current_schema(),'CREATE'), "
          "has_function_privilege('chat_runtime',"
          "'chat_outbox_notify_statement_v1()','EXECUTE')"
        ).fetchone()
        if grants != (True, False, False, False):
            raise AssertionError("Unexpected restricted grants: " + repr(grants))
        rid, _ = put(c, "dba.d14.runtime")
        signal(l)
        try:
            c.execute("UPDATE users SET credits=credits+1 WHERE false")
        except psycopg.errors.InsufficientPrivilege:
            denied = True
        else:
            denied = False
        assert denied
        fn = admin.execute(
            "SELECT prosecdef FROM pg_proc WHERE oid='chat_outbox_notify_statement_v1()'::regprocedure"
        ).fetchone()[0]
        assert fn is False
        return {"chat_runtime_insert_id": rid, "privileges": list(grants),
                "users_write_denied": denied, "security_definer": fn}


def d15():
    # Existing Chat Service: Authenticate, Claim, Changes, Execute, History
    # through the restricted pg role while A2 trigger is installed. No LISTEN.
    proc = subprocess.run(
        ["go", "run", "docs/operations/nats-n2-dba/legacy.go"],
        cwd=ROOT, capture_output=True, text=True, timeout=140, env=os.environ.copy()
    )
    if proc.returncode != 0:
        raise RuntimeError("Legacy Chat A2 compatibility failed: " + proc.stderr[-1700:])
    assert "D15 legacy PASS" in proc.stdout
    return {"native_go": proc.stdout.strip(), "runtime_role": "chat_runtime",
            "listening": False}


def d21_partial():
    with connect() as c:
        rid = c.execute("INSERT INTO chat_outbox(event_kind,created_at) "
                        "VALUES('dba.d21.old',clock_timestamp()-interval '26 hours') "
                        "RETURNING id").fetchone()[0]
        affected = c.execute(
            "DELETE FROM chat_outbox WHERE created_at<clock_timestamp()-interval '1 day'"
        ).rowcount
        assert c.execute("SELECT count(*) FROM chat_outbox WHERE id=%s", (rid,)).fetchone()[0] == 0
        return {"old_event_purged": rid, "deleted_rows": affected,
                "not_tested": "full client bootstrap after retention"}


def explain(c, sql, params, filename):
    raw = c.execute("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + sql, params).fetchone()[0]
    parsed = json.loads(raw) if isinstance(raw, str) else raw
    (RESULTS / filename).write_text(json.dumps(parsed,ensure_ascii=False,indent=2))
    plan = parsed[0]
    return {"execution_ms": plan["Execution Time"],
            "planning_ms": plan["Planning Time"],
            "node": plan["Plan"]["Node Type"],
            "actual_rows": plan["Plan"]["Actual Rows"],
            "shared_hit": plan["Plan"].get("Shared Hit Blocks", 0),
            "shared_read": plan["Plan"].get("Shared Read Blocks", 0)}


def d22():
    with connect() as c:
        c.execute("INSERT INTO chat_outbox(event_kind,created_at) "
                  "SELECT 'dba.d22.bulk',clock_timestamp()-((g % 86400)*interval '1 second') "
                  "FROM generate_series(1,1000000) g")
        c.execute("ANALYZE chat_outbox")
        cnt, hw, cutoff = c.execute(
            "SELECT count(*),max(id)-50000,clock_timestamp()-interval '60 seconds' FROM chat_outbox"
        ).fetchone()
        q1 = ("SELECT id,event_kind,channel_id,target_user_id,reference_id "
              "FROM chat_outbox WHERE id>%s OR created_at>=%s ORDER BY id LIMIT 1000")
        q2 = ("SELECT * FROM ("
              "SELECT id,event_kind,channel_id,target_user_id,reference_id FROM chat_outbox WHERE id>%s "
              "UNION ALL "
              "SELECT id,event_kind,channel_id,target_user_id,reference_id FROM chat_outbox "
              "WHERE id<=%s AND created_at>=%s) e ORDER BY id LIMIT 1000")
        p1 = explain(c,q1,(hw,cutoff),"explain-or.json")
        p2 = explain(c,q2,(hw,hw,cutoff),"explain-union.json")
        # Exact SQL predicate and column list of Chat 0.24.7
        # internal/chat/dispatch_reader.go:OutboxPage.
        q3 = ("SELECT id,event_kind,channel_id::text,target_user_id,reference_id::text,created_at "
              "FROM chat_outbox WHERE id>%s AND id<=%s "
              "AND (id>%s OR created_at>=%s) ORDER BY id LIMIT 1000")
        p3 = explain(c,q3,(0,hw+50000,hw,cutoff),"explain-n1-outboxpage.json")
        ids1 = [r[0] for r in c.execute(q1,(hw,cutoff)).fetchall()]
        ids2 = [r[0] for r in c.execute(q2,(hw,hw,cutoff)).fetchall()]
        ids3 = [r[0] for r in c.execute(q3,(0,hw+50000,hw,cutoff)).fetchall()]
        assert ids1==ids2==ids3 and len(ids1)==1000, "OR/UNION/N1 exact page differ"
        inds = c.execute("SELECT indexname,indexdef FROM pg_indexes "
                         "WHERE schemaname=current_schema() AND tablename='chat_outbox' "
                         "ORDER BY indexname").fetchall()
        result={"rows":cnt,"highwater":hw,"or":p1,"union":p2,"n1_outboxpage":p3,
                "indexes": [{"name":n,"definition":d} for n,d in inds]}
        EXTRAS["explain"] = result
        return result


def d24_partial():
    with connect() as blocker, connect() as w, connect() as monitor:
        blocker.execute('LISTEN "' + CHANNEL + '"')
        blocker.execute("BEGIN")
        start = monitor.execute("SELECT pg_notification_queue_usage()").fetchone()[0]
        for i in range(320):
            put(w, "dba.d24." + str(i))
        middle = monitor.execute("SELECT pg_notification_queue_usage()").fetchone()[0]
        blocker.execute("COMMIT")
        end = monitor.execute("SELECT pg_notification_queue_usage()").fetchone()[0]
        return {"committed_notifications": 320, "start_queue": start,
                "during_blocked_listener_tx": middle, "after": end,
                "not_tested": "8GB queue saturation / COMMIT failure (unsafe for shared runners)"}


def d26():
    sqlfile(UP,SECOND)
    try:
        with connect() as a, connect(SECOND) as b, listen() as la, listen(SECOND) as lb:
            assert CHANNEL != OTHER_CHANNEL
            ida, _ = put(a,"dba.d26.a")
            signal(la)
            assert no_signal(lb, 0.2)
            idb, _ = put(b,"dba.d26.b")
            signal(lb)
            assert no_signal(la, 0.2)
            assert a.execute("SELECT count(*) FROM chat_outbox WHERE event_kind='dba.d26.b'").fetchone()[0] == 0
            return {"channel_a":CHANNEL,"channel_b":OTHER_CHANNEL,
                    "schema_a_row":ida,"schema_b_row":idb}
    finally:
        sqlfile(DOWN,SECOND)


def time_inserts(label, n=250):
    with connect() as c:
        inserts, commits = [], []
        for i in range(n):
            c.execute("BEGIN")
            st=time.perf_counter_ns()
            put(c, "dba.bench." + label)
            middle=time.perf_counter_ns()
            c.execute("COMMIT")
            end=time.perf_counter_ns()
            inserts.append((middle-st)/1e6)
            commits.append((end-middle)/1e6)
        return {
            "count":n,
            "median_insert_ms":round(statistics.median(inserts),4),
            "median_commit_ms":round(statistics.median(commits),4),
            "p95_insert_ms":round(sorted(inserts)[int(n*.95)-1],4),
            "p95_commit_ms":round(sorted(commits)[int(n*.95)-1],4),
            "sum_insert_ms":round(sum(inserts),3),
            "sum_commit_ms":round(sum(commits),3),
        }


def d25_and_bench():
    with connect() as c:
        before = c.execute("SELECT count(*) FROM chat_outbox").fetchone()[0]
        privileges = c.execute("SELECT relacl FROM pg_class "
                               "WHERE oid='chat_outbox'::regclass").fetchone()[0]
    EXTRAS["bench"]["a2_before"] = time_inserts("a2_first")
    sqlfile(DOWN)
    sqlfile(DOWN)
    with connect() as c:
        assert c.execute("SELECT to_regprocedure('chat_outbox_notify_statement_v1()')").fetchone()[0] is None
        assert c.execute("SELECT count(*) FROM pg_trigger WHERE tgrelid='chat_outbox'::regclass "
                         "AND tgname='chat_outbox_wakeup_a2'").fetchone()[0] == 0
        assert c.execute("SELECT relacl FROM pg_class WHERE oid='chat_outbox'::regclass").fetchone()[0] == privileges
    EXTRAS["bench"]["no_trigger"] = time_inserts("baseline")
    with connect() as w, listen() as l:
        put(w, "dba.down.no_signal")
        assert no_signal(l,0.3)
    sqlfile(UP)
    sqlfile(UP)
    EXTRAS["bench"]["a2_after"] = time_inserts("a2_second")
    with connect() as w, listen() as l:
        put(w, "dba.up.signal_restored")
        signal(l)
    sqlfile(DOWN)
    with connect() as c:
        assert c.execute("SELECT count(*) FROM chat_outbox").fetchone()[0] >= before
        assert c.execute("SELECT relacl FROM pg_class WHERE oid='chat_outbox'::regclass").fetchone()[0] == privileges
    (RESULTS / "insert-commit-bench.json").write_text(
        json.dumps(EXTRAS["bench"],ensure_ascii=False,indent=2))
    return {"rollback_twice":True,"up_rerun":True,"messages_preserved":True,
            "privileges_preserved":True,"bench":EXTRAS["bench"]}


def main():
    require_test()
    try:
        EXTRAS["guard"] = run_guard()
    except Exception as exc:
        EXTRAS["fatal"] = "Preflight or UP failed: " + repr(exc)
        for d in STATUSES:
            notrun(d,"Preflight failed: " + repr(exc))
        return
    for cid, fn in [
       ("D01",d01),("D02",d02),("D03",d03),("D04",d04),
       ("D05",d05),("D06",d06),("D07",lambda:late_test(0)),
       ("D08",lambda:late_test(70.7)),("D09",d09),("D10",d10),
       ("D11",d11),("D12",d12),("D13",d13),("D14",d14),("D15",d15),
    ]:
        case(cid, fn)
    for cid, why in [
       ("D16","End-to-end company role change with A2 trigger and live WS requires N2 Go adapter"),
       ("D17","End-to-end corporation deletion/cleanup with A2 trigger requires N2 Go adapter"),
       ("D18","Planet sector transition and N2 WS adapter not implemented"),
       ("D19","Private/invite WS fanout with N2 adapter not implemented"),
       ("D20","Session/WS lease with N2 adapter not implemented"),
       ("D23","A2/B1 modes do not exist in Chat 0.24.7; full 50/100/200/256 comparison impossible"),
    ]:
        notrun(cid,why)
    # Partial D21/D24 evidence does not count as complete acceptance.
    try:
        EXTRAS["d21_partial"] = d21_partial()
        notrun("D21","Retention SQL DELETE tested, but full fresh WS bootstrap after >1-day retention not tested")
    except Exception as exc:
        STATUSES["D21"]={"status":"FAIL","reason":repr(exc)}
    case("D22",d22)
    try:
        EXTRAS["d24_partial"] = d24_partial()
        notrun("D24","320 commits with long LISTEN tx measured; 8GB NOTIFY queue saturation and COMMIT failure not tested safely")
    except Exception as exc:
        STATUSES["D24"]={"status":"FAIL","reason":repr(exc)}
    case("D26",d26)
    case("D25",d25_and_bench)


if __name__=="__main__":
    try:
        main()
    except Exception as exc:
        EXTRAS["fatal"] = repr(exc)
        traceback.print_exc()
    finally:
        out={"cases":STATUSES,"extra":EXTRAS,
             "totals":{s:sum(v["status"]==s for v in STATUSES.values())
                       for s in ("PASS","FAIL","NOT RUN")}}
        target=RESULTS/"summary.json"
        target.write_text(json.dumps(out,ensure_ascii=False,indent=2))
        print("SUMMARY",json.dumps(out["totals"]),flush=True)
        print("ARTIFACT",str(target),flush=True)
    sys.exit(1 if EXTRAS.get("fatal") or any(v["status"]=="FAIL" for v in STATUSES.values()) else 0)
