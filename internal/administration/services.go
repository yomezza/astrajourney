package administration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type ServiceMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type ServiceCard struct {
	NATS   natscore.Status `json:"nats"`
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Status string          `json:"status"`
	Reason string          `json:"reason,omitempty"`
	Info   []ServiceMetric `json:"info"`
}
type ServicesOptions struct {
	NATSStatus     func() natscore.Status
	Version        string
	Started        time.Time
	PlanetURL      *url.URL // Validated deployment config; never an HTTP query parameter.
	PlanetEnabled  func() bool
	ExpectedSchema int
}
type planetHealth struct {
	NATS        natscore.Status `json:"nats"`
	Service     string          `json:"service"`
	Version     string          `json:"version"`
	Stage       string          `json:"stage"`
	Status      string          `json:"status"`
	Ready       bool            `json:"gameplay_ready"`
	Multiplayer bool            `json:"multiplayer_ready"`
	Schema      int             `json:"schema_version"`
	Instance    string          `json:"runtime_instance"`
	Fence       int64           `json:"runtime_fence"`
}

func (a *Service) Services(o ServicesOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := a.Auth.User(w, r)
		if !ok {
			return
		}
		if user.Role != "admin" {
			httpkit.Fail(w, 403, "Требуется роль администратора")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2500*time.Millisecond)
		defer cancel()
		cosmic := a.cosmicCard(ctx, o)
		planet := a.planetCard(ctx, o)
		httpkit.Reply(w, 200, map[string]any{"checked_at": time.Now().UTC(), "services": []ServiceCard{cosmic, planet, a.chatCard(ctx)}})
	}
}
func (a *Service) cosmicCard(ctx context.Context, o ServicesOptions) ServiceCard {
	pool := a.DB.Stat()
	card := ServiceCard{ID: "cosmic", Name: "Космическая служба", Status: "ready", Info: []ServiceMetric{
		{"Версия", o.Version}, {"Время работы, с", strconv.FormatInt(int64(time.Since(o.Started).Seconds()), 10)},
		{"Платформа", runtime.GOOS + "/" + runtime.GOARCH}, {"Go", runtime.Version()},
		{"Соединения БД: занято / максимум", fmt.Sprintf("%d / %d", pool.AcquiredConns(), pool.MaxConns())},
	}}
	if o.NATSStatus != nil {
		card.NATS = o.NATSStatus()
	} else {
		card.NATS.State = "unknown"
	}
	began := time.Now()
	var schema string
	if err := a.DB.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		card.Status = "not_ready"
		card.Reason = "database_unavailable"
	} else {
		card.Info = append(card.Info, ServiceMetric{"Схема БД", schema}, ServiceMetric{"Ответ БД, мс", strconv.FormatInt(time.Since(began).Milliseconds(), 10)})
	}
	return card
}
func (a *Service) planetCard(ctx context.Context, o ServicesOptions) ServiceCard {
	card := ServiceCard{ID: "planet", Name: "Планетарная служба", Status: "disabled", Reason: "planetary_disabled", Info: []ServiceMetric{{"Ожидаемая схема", strconv.Itoa(o.ExpectedSchema)}}}
	card.NATS.State = "unknown"
	if o.PlanetEnabled == nil || !o.PlanetEnabled() {
		return card
	}
	card.Status = "unreachable"
	card.Reason = "runtime_unavailable"
	if o.PlanetURL == nil {
		card.Status = "configuration_error"
		card.Reason = "planetary_configuration_error"
		return card
	}
	card.Info = append(card.Info, ServiceMetric{"Внутренний адрес", o.PlanetURL.String()})
	target := *o.PlanetURL
	target.Path = "/planet/api/v1/health/ready"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return card
	}
	// Never forward the browser's cookie or follow an endpoint redirect.
	client := http.Client{Transport: httpkit.InternalTransport(), Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	began := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return card
	}
	defer response.Body.Close()
	card.Info = append(card.Info, ServiceMetric{"HTTP readiness", strconv.Itoa(response.StatusCode)}, ServiceMetric{"Ответ службы, мс", strconv.FormatInt(time.Since(began).Milliseconds(), 10)})
	var health planetHealth
	card.Status = "not_ready"
	if json.NewDecoder(io.LimitReader(response.Body, 32768)).Decode(&health) != nil || (health.Service != "astra-journey-planet" && health.Service != "cosmosimulator-planet") {
		card.Reason = "planetary_health_invalid"
		return card
	}
	card.NATS = health.NATS
	if card.NATS.State == "" {
		card.NATS.State = "unknown"
	}
	card.Reason = knownReason(health.Status)
	card.Info = append(card.Info, ServiceMetric{"Версия", health.Version}, ServiceMetric{"Этап", health.Stage}, ServiceMetric{"Схема службы", strconv.Itoa(health.Schema)})
	if response.StatusCode != 200 || health.Status != "ready" || !health.Ready {
		return card
	}
	var same bool
	err = a.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM planet_runtime_leases WHERE id=1 AND owner_id=$1 AND fence=$2 AND expires_at>clock_timestamp()) AND EXISTS(SELECT 1 FROM planet_schema_state WHERE id=1 AND version=$3)`, health.Instance, health.Fence, o.ExpectedSchema).Scan(&same)
	if err != nil {
		card.Reason = "database_unavailable"
		return card
	}
	if !same || health.Schema != o.ExpectedSchema {
		card.Status = "mismatch"
		card.Reason = "planetary_database_mismatch"
		return card
	}
	card.Status = "ready"
	card.Reason = ""
	card.Info = append(card.Info, ServiceMetric{"Подключение к игровой БД", "Подтверждено"}, ServiceMetric{"Поколение runtime", strconv.FormatInt(health.Fence, 10)})
	if health.Multiplayer {
		card.Info = append(card.Info, ServiceMetric{"Мультиплеер", "Доступен"})
	}
	return card
}
func knownReason(reason string) string {
	switch reason {
	case "database_unavailable", "schema_missing", "schema_incompatible", "lease_in_use", "lease_lost", "starting", "stopping":
		return reason
	default:
		return "runtime_unavailable"
	}
}
