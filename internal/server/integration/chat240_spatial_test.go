package integration

import (
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/account"
	"github.com/yomezza/cosmosimulator/internal/chat"
	"github.com/yomezza/cosmosimulator/internal/chatintegration"
	"testing"
)

func TestChat240SpatialPresence(t *testing.T) {
	t.Setenv("CHAT_ENABLED", "true")
	f, c, _, surface, site, ship := prepare180(t)
	base, stop := startPlanet150(t, f.db)
	defer stop()
	ticket := ticket180(t, f, c, op180(t), site, ship)
	conn, welcome := ws180(t, base, c, map[string]any{"ticket": ticket["ticket"]})
	defer conn.CloseNow()
	if welcome["message_type"] != "welcome" {
		t.Fatal(welcome)
	}
	s := &chat.Service{DB: f.db}
	u, e := s.Authenticate(f.ctx, account.TokenHash(c.Value))
	if e != nil {
		t.Fatal(e)
	}
	lease, e := s.Claim(f.ctx, u, chat.UUID())
	if e != nil {
		t.Fatal(e)
	}
	sector := f.integer(`SELECT current_sector FROM planet_visits WHERE user_id=$1`, u.ID)
	send := func(target, kind string) error {
		raw, _ := json.Marshal(map[string]any{"kind": kind, "target": target, "body": "Spatial message"})
		_, e := s.Execute(f.ctx, u, lease, chat.Command{Type: "message.send", Request: chat.UUID(), Payload: raw})
		return e
	}
	if e = send(fmt.Sprintf("%d/%d", surface, sector), "sector"); e != nil {
		t.Fatal(e)
	}
	if e = send(fmt.Sprint(surface), "planet"); e != nil {
		t.Fatal(e)
	}
	if e = send(fmt.Sprintf("%d/%d", surface, sector+1), "sector"); e == nil || e.Error() != "scope_changed" {
		t.Fatal("old scope", e)
	}
	outsider, _ := f.signup("chat_spatial_outsider")
	v, e := s.Authenticate(f.ctx, account.TokenHash(outsider.Value))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.History(f.ctx, v, chat.Target{Kind: "planet", Target: fmt.Sprint(surface)}, 0, ""); e == nil {
		t.Fatal("spatial history leaked into space")
	}
	tx, e := f.db.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	ready, e := chatintegration.PrepareSurface(f.ctx, tx, surface)
	if e != nil || ready {
		t.Fatal("early spatial cleanup receipt", e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.History(f.ctx, u, chat.Target{Kind: "planet", Target: fmt.Sprint(surface)}, 0, ""); e == nil {
		t.Fatal("retiring planet still accessible")
	}
	for i := 0; i < 3; i++ {
		if e = s.Maintain(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if f.integer(`SELECT count(*) FROM chat_channels WHERE surface_id=$1`, surface) != 0 {
		t.Fatal("spatial channels survived")
	}
	if f.integer(`SELECT count(*) FROM chat_surface_cleanup_receipts WHERE surface_id=$1`, surface) != 1 {
		t.Fatal("no cleanup receipt")
	}
	if f.integer(`SELECT count(*) FROM planet_surfaces WHERE id=$1`, surface) != 1 {
		t.Fatal("Chat changed canonical geography")
	}
}
