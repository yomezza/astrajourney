package natscore

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Config struct {
	Enabled                                         bool
	ServiceIdentity, Username, PasswordFile, CAFile string
	Password                                        string `json:"-"`
	Servers                                         []string
	ConnectTimeout, ReconnectWait                   time.Duration
}

func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{ServiceIdentity: getenv("NATS_SERVICE_IDENTITY"), Username: getenv("NATS_USERNAME"), Password: getenv("NATS_PASSWORD"), PasswordFile: getenv("NATS_PASSWORD_FILE"), CAFile: getenv("NATS_CA_FILE"), ConnectTimeout: 2 * time.Second, ReconnectWait: time.Second}
	if c.ServiceIdentity == "" {
		c.ServiceIdentity = c.Username
	}
	var err error
	if v := getenv("NATS_ENABLED"); v != "" {
		c.Enabled, err = strconv.ParseBool(v)
		if err != nil {
			return c, errors.New("invalid NATS_ENABLED boolean")
		}
	}
	if v := getenv("NATS_SERVERS"); v != "" {
		for _, server := range strings.Split(v, ",") {
			c.Servers = append(c.Servers, strings.TrimSpace(server))
		}
	}
	for _, v := range []struct {
		name  string
		value *time.Duration
	}{{"NATS_CONNECT_TIMEOUT", &c.ConnectTimeout}, {"NATS_RECONNECT_WAIT", &c.ReconnectWait}} {
		if raw := getenv(v.name); raw != "" {
			d, e := time.ParseDuration(raw)
			if e != nil {
				return c, errors.New("invalid NATS interval")
			}
			*v.value = d
		}
	}
	return c, nil
}
func (c Config) Validate(identity string, required bool) error {
	if required && !c.Enabled {
		return errors.New("NATS requires enabled=true")
	}
	if !c.Enabled {
		return nil
	}
	if (identity != "chat" && identity != "planet" && identity != "cosmos") || c.ServiceIdentity != identity || c.Username != identity {
		return errors.New("NATS service_identity and username must match the service (cosmos/planet/chat)")
	}
	if len(c.Servers) < 1 || len(c.Servers) > 3 {
		return errors.New("NATS requires 1-3 explicit servers")
	}
	scheme := ""
	for _, server := range c.Servers {
		u, e := url.Parse(server)
		if e != nil || (u.Scheme != "tls" && u.Scheme != "nats") || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return errors.New("NATS servers must be nats://host:port or tls://host:port without credentials or URL extras")
		}
		if scheme != "" && scheme != u.Scheme {
			return errors.New("NATS servers must use one transport scheme; mixed nats/tls is not supported")
		}
		scheme = u.Scheme
		h, p, e := net.SplitHostPort(u.Host)
		port, pe := strconv.Atoi(p)
		if e != nil || pe != nil || port < 1 || port > 65535 || h == "" || strings.ContainsAny(h, "* >\t\r\n%") {
			return errors.New("invalid NATS host/port")
		}
	}
	if c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > 10*time.Second || c.ReconnectWait < 250*time.Millisecond || c.ReconnectWait > 30*time.Second {
		return errors.New("NATS connect_timeout must be 100ms-10s and reconnect_wait 250ms-30s")
	}
	if (c.Password == "") == (c.PasswordFile == "") {
		return errors.New("NATS requires exactly one of password or password_file")
	}
	if c.PasswordFile != "" && !filepath.IsAbs(c.PasswordFile) {
		return errors.New("NATS password_file must be absolute (TOML paths resolve against CONFIG_FILE)")
	}
	if c.Password != "" && !validPassword(c.Password) {
		return errors.New("invalid NATS password content")
	}
	if scheme == "tls" && !filepath.IsAbs(c.CAFile) {
		return errors.New("NATS TLS requires an absolute ca_file (TOML paths resolve against CONFIG_FILE)")
	}
	if scheme == "nats" && c.CAFile != "" {
		return errors.New("NATS ca_file applies only to tls:// servers")
	}

	return nil
}

// TLS is selected explicitly by the URL. TLS profiles never fall back to plaintext.
func (c Config) UsesTLS() bool {
	return len(c.Servers) > 0 && strings.HasPrefix(c.Servers[0], "tls://")
}
func validPassword(p string) bool {
	return len(p) > 0 && len(p) <= 4096 && utf8.ValidString(p) && !strings.ContainsAny(p, "\x00\r\n")
}
func readNATSFile(path string, limit int64, secret bool) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("NATS file unavailable; check password_file/ca_file and service access")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("NATS file must be a regular file within its size limit")
	}
	if secret {
		if e = checkNATSSecretPermissions(path, info); e != nil {
			return nil, e
		}
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("cannot read bounded NATS file")
	}
	return b, nil
}

// Local material is validated before any broker connection; remote identity
// failures are classified separately by the transport, without logging secrets.
func (c Config) Credentials() (*tls.Config, string, error) {
	if (c.Password == "") == (c.PasswordFile == "") {
		return nil, "", errors.New("NATS requires exactly one of password or password_file")
	}
	password := c.Password
	if c.PasswordFile != "" {
		b, e := readNATSFile(c.PasswordFile, 4096, true)
		if e != nil {
			return nil, "", e
		}
		b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
		password = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	}
	if !validPassword(password) {
		return nil, "", errors.New("invalid NATS password content")
	}
	if !c.UsesTLS() {
		return nil, password, nil
	}
	ca, e := readNATSFile(c.CAFile, 1<<20, false)
	if e != nil {
		return nil, "", e
	}
	roots := x509.NewCertPool()
	count := 0
	for rest := ca; len(bytes.TrimSpace(rest)) > 0; {
		block, tail := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, "", errors.New("NATS ca_file must contain CA certificates in PEM format")
		}
		rest = tail
		cert, e := x509.ParseCertificate(block.Bytes)
		now := time.Now()
		if e != nil || !cert.IsCA || !cert.BasicConstraintsValid || now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return nil, "", errors.New("NATS ca_file contains an invalid or expired CA")
		}
		roots.AddCert(cert)
		count++
	}
	if count == 0 {
		return nil, "", errors.New("NATS ca_file has no CA certificates")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, password, nil
}
