// Package mediastore is the bounded private file protocol; it never grants ACLs.
package mediastore

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yomezza/cosmosimulator/internal/platform/configuration"
	"github.com/yomezza/cosmosimulator/internal/platform/httpkit"
)

const Limit int64 = 8 << 20
const Header = "X-Astra-Service-Token"

type Receipt struct {
	ID           string `json:"id"`
	SHA256       string `json:"sha256"`
	InputSHA256  string `json:"input_sha256"`
	Size         int64  `json:"size"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SourceFormat string `json:"source_format"`
	Deleted      bool   `json:"deleted,omitempty"`
}
type Error struct{ Status int }

func (e *Error) Error() string { return "CDN private file operation failed" }

type Client struct {
	BaseURL, TokenFile string
	HTTP               *http.Client
}

func Defaults() *Client {
	base := configuration.Getenv("CDN_INTERNAL_URL")
	if base == "" {
		base = "http://127.0.0.1:8083"
	}
	token := configuration.Getenv("CDN_SERVICE_TOKEN_FILE")
	if token == "" {
		token = filepath.Join("data", "cdn", "state", "service.token")
	}
	return &Client{BaseURL: strings.TrimRight(base, "/"), TokenFile: token, HTTP: &http.Client{Transport: httpkit.InternalTransport(), Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func Token(file string) (string, error) {
	b, e := os.ReadFile(file)
	if e != nil || len(b) > 512 {
		return "", errors.New("CDN service token unavailable")
	}
	s := strings.TrimSpace(string(b))
	if len(s) != 64 {
		return "", errors.New("invalid CDN service token")
	}
	return s, nil
}
func Authorized(r *http.Request, token string) bool {
	got := r.Header.Get(Header)
	return len(token) == 64 && len(got) == len(token) && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) (Receipt, error) {
	var out Receipt
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return out, errors.New("invalid CDN internal URL")
	}
	token, err := Token(c.TokenFile)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/cdn/internal/v1/media/"+path, body)
	if err != nil {
		return out, err
	}
	req.Header.Set(Header, token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, errors.New("CDN media unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, &Error{resp.StatusCode}
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&out)
	if err != nil || out.ID != strings.ToLower(strings.SplitN(path, "?", 2)[0]) {
		return out, errors.New("invalid CDN receipt")
	}
	if method == "DELETE" && out.Deleted {
		return out, nil
	}
	hash, err := hex.DecodeString(out.SHA256)
	if err != nil || len(hash) != 32 || out.Size < 1 || out.Size > Limit || out.Width < 1 || out.Height < 1 || out.Width > 4096 || out.Height > 4096 || (out.SourceFormat != "png" && out.SourceFormat != "jpeg" && out.SourceFormat != "webp") {
		return out, errors.New("invalid CDN receipt")
	}
	return out, nil
}
