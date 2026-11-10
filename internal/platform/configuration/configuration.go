// Package configuration selects exactly one application configuration source.
// Initialize is called before starting each service or maintenance command.
package configuration

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/pelletier/go-toml/v2"
)

const FileVariable = "CONFIG_FILE"

type Service string

const (
	Cosmos Service = "cosmos"
	Planet Service = "planet"
	Chat   Service = "chat"
)

// Source is an immutable TOML snapshot or an environment lookup. File mode never
// falls back to environment values, including for omitted or empty parameters.
type Source struct {
	values      map[string]string
	environment func(string) string
}

func (s *Source) Getenv(name string) string {
	if s.environment != nil {
		return s.environment(name)
	}
	return s.values[name]
}

// Load reads CONFIG_FILE from the supplied environment. An explicit bad path or
// invalid document is an error, never a reason to silently select another source.
func Load(getenv func(string) string, service Service) (*Source, error) {
	if service != Cosmos && service != Planet && service != Chat {
		return nil, errors.New("unknown configuration service")
	}
	path := getenv(FileVariable)
	if path == "" {
		return &Source{environment: getenv}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read CONFIG_FILE: %w", err)
	}
	// Windows editors and PowerShell 5 can write UTF-8 with a leading BOM.
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var document document
	if err = toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&document); err != nil {
		// Decoder messages can include the source line, a DSN or a password. Only
		// report positions, never the raw error or the document's contents.
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) && len(strict.Errors) > 0 {
			row, column := strict.Errors[0].Position()
			return nil, fmt.Errorf("unknown TOML configuration field at line %d, column %d", row, column)
		}
		var decode *toml.DecodeError
		if errors.As(err, &decode) {
			row, column := decode.Position()
			return nil, fmt.Errorf("invalid TOML configuration at line %d, column %d (check syntax and value types)", row, column)
		}
		return nil, errors.New("invalid TOML configuration")
	}
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("cannot resolve CONFIG_FILE directory")
	}
	values := document.values(service)
	for _, name := range []string{"PLANET_LOG_FILE", "CHAT_ERASURE_JOURNAL", "NATS_PASSWORD_FILE", "NATS_CA_FILE"} {
		if value := values[name]; value != "" && !filepath.IsAbs(value) {
			values[name] = filepath.Join(base, value)
		}
	}
	// Arrays are translated to the existing application's comma-separated input.
	// Reject ambiguous entries instead of silently interpreting one item as two.
	for _, list := range [][]string{document.Cosmos.TrustedProxies, document.Planet.AllowedOrigins, document.Chat.AllowedOrigins} {
		for _, item := range list {
			if strings.Contains(item, ",") {
				return nil, errors.New("TOML origin/proxy array entries must not contain commas")
			}
		}
	}
	if document.NATS != nil {
		for _, item := range document.NATS.Servers {
			if strings.Contains(item, ",") {
				return nil, errors.New("TOML NATS server entries must not contain commas")
			}
		}
	}
	return &Source{values: values}, nil
}

var active atomic.Pointer[Source]

// Initialize installs one snapshot for this process. Configuration changes take
// effect on restart; the OS environment is not changed or populated with secrets.
func Initialize(service Service) error {
	source, err := Load(os.Getenv, service)
	if err != nil {
		return err
	}
	active.Store(source)
	if source.environment != nil {
		log.Print("Configuration source: environment")
	} else {
		log.Print("Configuration source: TOML (CONFIG_FILE)")
	}
	return nil
}

// Getenv keeps the existing service defaults and validators usable with either
// source. Before process initialization it preserves environment-based embedding.
func Getenv(name string) string {
	if source := active.Load(); source != nil {
		return source.Getenv(name)
	}
	return os.Getenv(name)
}
