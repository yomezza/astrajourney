package administration

import (
	"context"
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
	"github.com/yomezza/cosmosimulator/internal/platform/mediastore"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"io"
	"net/http"
	"strconv"
	"time"
)

func (a *Service) cdnCard(ctx context.Context) ServiceCard {
	card := ServiceCard{ID: "cdn", Name: "Доставка графики", Status: "unreachable", Info: []ServiceMetric{}}
	card.NATS.State = "unknown"
	req, err := http.NewRequestWithContext(ctx, "GET", mediastore.Defaults().BaseURL+"/cdn/api/v1/health/ready", nil)
	if err != nil {
		card.Status = "configuration_error"
		return card
	}
	client := http.Client{Transport: httpkit.InternalTransport(), Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return card
	}
	defer response.Body.Close()
	var h struct {
		Service, Version, Status string
		Ready                    bool
		Revisions                []string
		NATS                     natscore.Status
		SyncError                string `json:"sync_error"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&h) != nil || h.Service != "astra-journey-cdn" {
		card.Status = "not_ready"
		return card
	}
	card.Status = "not_ready"
	if h.Ready && response.StatusCode == 200 {
		card.Status = "ready"
	}
	card.NATS = h.NATS
	card.Info = []ServiceMetric{{"Версия", h.Version}, {"Пакеты графики", strconv.Itoa(len(h.Revisions))}}
	return card
}
