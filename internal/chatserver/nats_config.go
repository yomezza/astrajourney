package chatserver

import (
	"crypto/tls"
	"github.com/yomezza/cosmosimulator/internal/platform/natscore"
)

// NATSConfig preserves the Chat embedding API; transport security is shared.
type NATSConfig natscore.Config

func loadNATSConfig(getenv func(string) string) (NATSConfig, error) {
	c, e := natscore.LoadConfig(getenv)
	return NATSConfig(c), e
}
func (c NATSConfig) Validate(mode string) error {
	return natscore.Config(c).Validate("chat", mode == "b1")
}
func (c NATSConfig) credentials() (*tls.Config, string, error) {
	return natscore.Config(c).Credentials()
}
