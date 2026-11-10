// Package natsfixture creates disposable, loopback-only TLS/auth NATS processes.
// It never accepts a remote broker URL or touches the owner's installed service.
package natsfixture

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const Subject = "cosmos.v1.signal.chat.outbox.changed"

type Material struct{ CAFile, CertFile, KeyFile, PasswordFile, Password string }

func PKI(t *testing.T, wrongName bool) Material {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "N3 disposable CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	serverKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "N3 disposable broker"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(12 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	if wrongName {
		cert.IPAddresses = nil
		cert.DNSNames = []string{"wrong-name.invalid"}
	}
	der, e := x509.CreateCertificate(rand.Reader, cert, ca, &serverKey.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalPKCS8PrivateKey(serverKey)
	if e != nil {
		t.Fatal(e)
	}
	m := Material{CAFile: filepath.Join(dir, "ca.pem"), CertFile: filepath.Join(dir, "server.pem"), KeyFile: filepath.Join(dir, "server.key"), PasswordFile: filepath.Join(dir, "chat.password")}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		t.Fatal(e)
	}
	m.Password = base64.RawURLEncoding.EncodeToString(secret)
	for path, b := range map[string][]byte{m.CAFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), m.CertFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), m.KeyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), m.PasswordFile: []byte(m.Password + "\n")} {
		if e = os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return m
}

type Options struct{ WrongName, DenyPublish, DenySubscribe bool }
type Broker struct {
	stderr bytes.Buffer
	Material
	URL, ConfigFile, LogFile string
	cmd                      *exec.Cmd
	done                     chan error
	binary                   string
	t                        *testing.T
	OperatorPassword         string
}

func New(t *testing.T, options Options) *Broker {
	t.Helper()
	binary := os.Getenv("TEST_NATS_SERVER_BINARY")
	if binary == "" {
		var e error
		binary, e = exec.LookPath("nats-server")
		if e != nil {
			t.Skip("TEST_NATS_SERVER_BINARY required for real TLS Core NATS tests")
		}
	}
	info, e := os.Stat(binary)
	if e != nil || !info.Mode().IsRegular() {
		t.Fatal("invalid test NATS binary")
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if port == 4222 || port == 8222 {
		t.Fatal("reserved production port selected")
	}
	m := PKI(t, options.WrongName)
	dir := t.TempDir()
	random := make([]byte, 32)
	if _, e = rand.Read(random); e != nil {
		t.Fatal(e)
	}
	b := &Broker{Material: m, URL: "tls://127.0.0.1:" + strconv.Itoa(port), ConfigFile: filepath.Join(dir, "server.json"), LogFile: filepath.Join(dir, "server.log"), binary: binary, t: t, OperatorPassword: base64.RawURLEncoding.EncodeToString(random)}
	hash := func(password string) string {
		v, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if e != nil {
			t.Fatal(e)
		}
		return string(v)
	}
	allowPub, allowSub := []string{Subject}, []string{Subject}
	if options.DenyPublish {
		allowPub = []string{"n3.denied"}
	}
	if options.DenySubscribe {
		allowSub = []string{"n3.denied"}
	}
	user := map[string]any{"user": "chat", "password": hash(m.Password), "permissions": map[string]any{"publish": map[string]any{"allow": allowPub}, "subscribe": map[string]any{"allow": allowSub}}}
	operator := map[string]any{"user": "n3_test_operator", "password": hash(b.OperatorPassword), "permissions": map[string]any{"publish": map[string]any{"allow": []string{">"}}, "subscribe": map[string]any{"allow": []string{">"}}}}
	config := map[string]any{"server_name": "n3-disposable", "host": "127.0.0.1", "port": port, "tls": map[string]any{"cert_file": m.CertFile, "key_file": m.KeyFile, "min_version": "1.2", "timeout": 2}, "authorization": map[string]any{"users": []any{user, operator}}, "log_file": b.LogFile}
	var configJSON bytes.Buffer
	encoder := json.NewEncoder(&configJSON)
	encoder.SetEscapeHTML(false) // NATS config lexer does not accept JSON \u003e for ACL >.
	if e = encoder.Encode(config); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(b.ConfigFile, configJSON.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(b.Stop)
	b.Start()
	return b
}
func (b *Broker) Start() {
	b.t.Helper()
	if b.cmd != nil {
		b.t.Fatal("owned broker already running")
	}
	b.stderr.Reset()
	b.cmd = exec.Command(b.binary, "-c", b.ConfigFile)
	b.cmd.Stderr = &b.stderr
	if e := b.cmd.Start(); e != nil {
		b.t.Fatal("cannot start owned NATS")
	}
	b.done = make(chan error, 1)
	cmd := b.cmd
	go func() { b.done <- cmd.Wait() }()
	address := b.URL[len("tls://"):]
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-b.done:
			b.cmd = nil
			message := strings.ReplaceAll(strings.ReplaceAll(b.stderr.String(), b.Password, "[redacted]"), b.OperatorPassword, "[redacted]")
			message = regexp.MustCompile(`\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}`).ReplaceAllString(message, "[redacted-hash]")
			b.t.Fatalf("owned NATS exited before readiness: %s", message)
		default:
		}
		c, e := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if e == nil {
			c.Close()
			return
		}
		if time.Now().After(deadline) {
			b.Stop()
			b.t.Fatal("owned NATS readiness timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func (b *Broker) Stop() {
	if b.cmd == nil {
		return
	}
	b.cmd.Process.Kill()
	select {
	case <-b.done:
	case <-time.After(3 * time.Second):
		b.t.Error("owned NATS stop timeout")
	}
	b.cmd = nil
}
