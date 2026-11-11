// Package natscore provides authenticated Core NATS transport, not game state.
package natscore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/nats-io/nats.go"
	"net"
	"strings"
	"time"
)

// Cancel the actual socket even during initial INFO/TLS/auth reads, so shutdown
// does not wait for connect_timeout * server count on unreachable/broken peers.
type natsDialer struct {
	ctx     context.Context
	timeout time.Duration
}
type natsSocket struct {
	net.Conn
	stop func() bool
}

func (c *natsSocket) Close() error { c.stop(); return c.Conn.Close() }
func (d natsDialer) Dial(network, address string) (net.Conn, error) {
	c, e := (&net.Dialer{Timeout: d.timeout}).DialContext(d.ctx, network, address)
	if e != nil {
		return nil, e
	}
	return &natsSocket{Conn: c, stop: context.AfterFunc(d.ctx, func() { c.Close() })}, nil
}
func ErrorCode(e error) string {
	var verify *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(e, &verify) || errors.As(e, &unknown) || errors.As(e, &host) || errors.As(e, &invalid) || errors.Is(e, nats.ErrTLS) || errors.Is(e, nats.ErrSecureConnRequired) || errors.Is(e, nats.ErrSecureConnWanted) {
		return "tls_verification_failed"
	}
	if errors.Is(e, nats.ErrAuthorization) || errors.Is(e, nats.ErrAuthExpired) || errors.Is(e, nats.ErrAuthRevoked) {
		return "authentication_failed"
	}
	if errors.Is(e, nats.ErrPermissionViolation) {
		return "authorization_failed"
	}
	if errors.Is(e, nats.ErrSlowConsumer) {
		return "slow_consumer"
	}
	return "network_unavailable"
}

// Connect has no reconnect loop or subscriptions: its owner chooses its exact
// subjects and lifecycle. All sockets, including handshake reads, are cancellable.
func Connect(ctx context.Context, c Config, tlsConfig *tls.Config, password string, onError nats.ErrHandler, onClose nats.ConnHandler) (*nats.Conn, error) {
	options := []nats.Option{nats.UserInfo(c.Username, password), nats.NoReconnect(), nats.IgnoreDiscoveredServers(), nats.ReconnectBufSize(0), nats.Timeout(c.ConnectTimeout), nats.FlusherTimeout(c.ConnectTimeout), nats.PingInterval(5 * time.Second), nats.MaxPingsOutstanding(2), nats.SetCustomDialer(natsDialer{ctx, c.ConnectTimeout}), nats.Name("cosmos-" + c.ServiceIdentity + "-core"), nats.ErrorHandler(onError), nats.ClosedHandler(onClose)}
	if c.UsesTLS() {
		if tlsConfig == nil {
			return nil, errors.New("NATS TLS configuration is required")
		}
		options = append(options, nats.Secure(tlsConfig.Clone()))
	}
	return nats.Connect(strings.Join(c.Servers, ","), options...)
}
