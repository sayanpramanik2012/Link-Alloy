package engine

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

const maxTLSInspectionBytes = 64 << 10
const tlsInspectionTimeout = 500 * time.Millisecond

var errHelloInspected = errors.New("client hello inspection finished")

// helloRecorder is an offline view of the application's initial TLS bytes.
// crypto/tls validates the ClientHello framing. It cannot send alerts, complete
// a handshake, or close the application's real socket through this wrapper.
// Every consumed byte is forwarded unchanged before bridging the remaining data.
type helloRecorder struct {
	net.Conn
	prefix []byte
}

func (r *helloRecorder) Read(p []byte) (int, error) {
	remaining := maxTLSInspectionBytes - len(r.prefix)
	if remaining <= 0 {
		return 0, io.EOF
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := r.Conn.Read(p)
	r.prefix = append(r.prefix, p[:n]...)
	return n, err
}
func (*helloRecorder) Write(p []byte) (int, error) { return len(p), nil }
func (*helloRecorder) Close() error                { return nil }

func inspectTLSService(conn net.Conn) ([]byte, string) {
	recorder := &helloRecorder{Conn: conn}
	if err := conn.SetReadDeadline(time.Now().Add(tlsInspectionTimeout)); err != nil {
		return nil, ""
	}
	defer conn.SetReadDeadline(time.Time{})
	var service string
	// This parser is disconnected from the network's write path. Abort before
	// certificates/key exchange, then relay the untouched original ClientHello.
	parser := tls.Server(recorder, &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		service = serviceDomain(hello.ServerName)
		return nil, errHelloInspected
	}})
	_ = parser.Handshake()
	return recorder.prefix, service
}

func serviceDomain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return ""
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return ""
			}
		}
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	return domain
}
