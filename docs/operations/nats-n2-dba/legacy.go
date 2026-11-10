//go:build ignore

// Isolated A2 compatibility test of the existing Chat Service legacy SQL path.
// Uses PostgreSQL chat_runtime role, but NEVER LISTEN. No runtime Go is changed.
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/yomezza/cosmosimulator/internal/chat"
    "github.com/yomezza/cosmosimulator/internal/platform/postgres"
)

func main() {
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" { panic("test DSN required") }
    ctx,cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()
    cfg,_,err := postgres.Config(dsn,"nats_dba_test")
    if err!=nil { panic(err) }
    if !strings.HasSuffix(cfg.ConnConfig.Database,"_test") {
        panic("refusing non-test database")
    }
    admin,err := pgxpool.NewWithConfig(ctx,cfg)
    if err!=nil { panic(err) }
    defer admin.Close()
    var uid int64
    err=admin.QueryRow(ctx,
        "INSERT INTO users(username,password_hash) VALUES('n2_dba_legacy','test') RETURNING id",
    ).Scan(&uid)
    if err!=nil { panic(err) }
    const token = "n2-dba-legacy-test-token-hash"
    _,err=admin.Exec(ctx,"INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 hour')",token,uid)
    if err!=nil { panic(err) }
    var hw int64
    if err=admin.QueryRow(ctx,"SELECT coalesce(max(id),0) FROM chat_outbox").Scan(&hw);err!=nil { panic(err) }
    cfgRestricted:=cfg.Copy()
    cfgRestricted.AfterConnect=func(ctx context.Context,c *pgx.Conn)error {
        _,e:=c.Exec(ctx,"SET ROLE chat_runtime")
        return e
    }
    runtime,err:=pgxpool.NewWithConfig(ctx,cfgRestricted)
    if err!=nil { panic(err) }
    defer runtime.Close()
    s:=&chat.Service{DB:runtime}
    u,err:=s.Authenticate(ctx,token)
    if err!=nil || u.ID!=uid { panic(fmt.Sprintf("restricted authenticate: %v",err)) }
    lease,err:=s.Claim(ctx,u,chat.UUID())
    if err!=nil { panic(fmt.Sprintf("restricted lease claim: %v",err)) }
    defer s.Release(context.Background(),lease)
    before,_,stampBefore,err:=s.Changes(ctx,u,hw)
    if err!=nil {panic(fmt.Sprintf("legacy Changes BEFORE: %v",err))}
    _,err=admin.Exec(ctx,"INSERT INTO chat_outbox(event_kind,target_user_id) VALUES('dba.d15.targeted',$1)",uid)
    if err!=nil {panic(err)}
    next,_,stampAfter,err:=s.Changes(ctx,u,before)
    if err!=nil || stampAfter==stampBefore || next<=before {
        panic(fmt.Sprintf("legacy Changes AFTER: next=%d before=%d err=%v match=%v",next,before,err,stampAfter==stampBefore))
    }
    payload,_:=json.Marshal(map[string]string{"kind":"global","body":"N2 DBA old Chat SQL compatibility"})
    result,err:=s.Execute(ctx,u,lease,chat.Command{Type:"message.send",Request:chat.UUID(),Payload:payload})
    if err!=nil { panic(fmt.Sprintf("restricted legacy Execute: %v",err)) }
    history,err:=s.History(ctx,u,chat.Target{Kind:"global"},0,"")
    if err!=nil || len(history)<1 {panic(fmt.Sprintf("legacy History: %v count %d",err,len(history)))}
    fmt.Printf("D15 legacy PASS: restricted auth, lease, Changes, message.send, History; user=%d; result_bytes=%d\n",uid,len(result))
}
