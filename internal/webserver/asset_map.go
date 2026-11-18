package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/yomezza/cosmosimulator/internal/assetpack"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

type mapFetch struct {
	done       chan struct{}
	generation uint64
	content    *httpkit.StaticContent
	err        error
}

func (s *Service) assetMap(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	content, err := s.assetMapContent(ctx)
	if err != nil {
		httpkit.Fail(w, 503, "Каталог графики недоступен")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	content.ServeHTTP(w, r)
}

// Share a refresh without holding the cache mutex during HTTP, compression or
// response writes. Closing done publishes one result to every waiting request.
func (s *Service) assetMapContent(ctx context.Context) (*httpkit.StaticContent, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.cache.Lock()
		generation := s.generation.Load()
		if s.cache.content != nil && time.Now().Before(s.cache.until) && s.cache.generation == generation {
			content := s.cache.content
			s.cache.Unlock()
			return content, nil
		}
		if pending := s.cache.loading; pending != nil {
			s.cache.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending.done:
				if pending.err != nil {
					if errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
						// A different browser owned this fetch. Its cancellation
						// must not cancel requests that still have their own budget.
						continue
					}
					return nil, pending.err
				}
				if pending.generation == s.generation.Load() {
					return pending.content, nil
				}
				continue
			}
		}
		pending := &mapFetch{done: make(chan struct{}), generation: generation}
		s.cache.loading = pending
		s.cache.Unlock()

		content, err := s.fetchAssetMap(ctx)
		s.cache.Lock()
		pending.content, pending.err = content, err
		if err == nil && generation == s.generation.Load() {
			s.cache.content = content
			s.cache.until = time.Now().Add(30 * time.Second)
			s.cache.generation = generation
		}
		s.cache.loading = nil
		close(pending.done)
		s.cache.Unlock()
		if err != nil {
			return nil, err
		}
		if generation == s.generation.Load() {
			return content, nil
		}
		// An asset activation/reconnect signal arrived while fetching. Fetch the
		// current map instead of publishing the response from an older generation.
	}
}

func (s *Service) fetchAssetMap(ctx context.Context) (*httpkit.StaticContent, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.config.CDNURL+"/cdn/api/v1/asset-map", nil)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil {
		return nil, err
	}
	var contract struct {
		Schema   int    `json:"schema_version"`
		Contract string `json:"client_contract"`
	}
	if response.StatusCode != 200 || len(body) > 2<<20 || json.Unmarshal(body, &contract) != nil || contract.Schema != assetpack.Schema || contract.Contract != assetpack.Contract {
		return nil, errors.New("invalid CDN asset map")
	}
	return httpkit.NewStaticContent(body), nil
}
