package administration

import (
	"context"
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"io"
	"net/http"
	"time"
)

func (a *Service) webCard(ctx context.Context) ServiceCard {
	card := ServiceCard{ID: "web", Name: "Веб-интерфейс", Status: "unreachable", Info: []ServiceMetric{}}
	card.NATS.State = "unknown"
	base := configuration.Getenv("WEB_INTERNAL_URL")
	if base == "" {
		base = "http://127.0.0.1:8083"
	}
	request, e := http.NewRequestWithContext(ctx, "GET", base+"/web/api/v1/health/ready", nil)
	if e != nil {
		card.Status = "configuration_error"
		return card
	}
	client := http.Client{Transport: httpkit.InternalTransport(), Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(request)
	if e != nil {
		return card
	}
	defer response.Body.Close()
	var h struct {
		Service, Version, Status, Revision string
		Ready                              bool
		NATS                               natscore.Status
		API                                string `json:"api_contract"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&h) != nil || h.Service != "astra-journey-web" {
		card.Status = "not_ready"
		return card
	}
	card.Status = "not_ready"
	if h.Ready && response.StatusCode == 200 {
		card.Status = "ready"
	}
	card.NATS = h.NATS
	if card.NATS.State == "" {
		card.NATS.State = "unknown"
	}
	card.Info = []ServiceMetric{{"Версия", h.Version}, {"Пакет интерфейса", h.Revision}, {"Контракт API", h.API}}
	return card
}
