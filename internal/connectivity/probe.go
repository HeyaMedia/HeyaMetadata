package connectivity

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	connectAndHandshakeTimeout = 5 * time.Second
	probeTimeout               = 10 * time.Second
	probeBodyLimit             = 4 << 10
	probeHeaderLimit           = 64 << 10
)

type TLSInfo struct {
	LeafSHA256 string   `json:"leaf_sha256"`
	SANs       []string `json:"sans"`
	SelfSigned bool     `json:"self_signed"`
}

type ProbeError struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type Result struct {
	ObservedIP string      `json:"observed_ip"`
	Reachable  bool        `json:"reachable"`
	Verified   bool        `json:"verified"`
	LatencyMS  int64       `json:"latency_ms"`
	TLS        *TLSInfo    `json:"tls"`
	Error      *ProbeError `json:"error"`
}

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

// Prober performs exactly one literal-IP TCP/TLS/HTTP exchange. dialContext
// is injectable for classification tests but production always uses net.Dialer.
type Prober struct {
	dialContext dialContextFunc
	now         func() time.Time
}

func NewProber() *Prober {
	dialer := &net.Dialer{}
	return &Prober{dialContext: dialer.DialContext, now: time.Now}
}

func (prober *Prober) Probe(ctx context.Context, address netip.Addr, port int, challenge string) (result Result) {
	started := prober.now()
	result = Result{ObservedIP: address.String()}
	defer func() {
		result.LatencyMS = prober.now().Sub(started).Milliseconds()
	}()

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	phaseCtx, phaseCancel := context.WithTimeout(ctx, connectAndHandshakeTimeout)
	defer phaseCancel()

	target := net.JoinHostPort(address.String(), fmt.Sprintf("%d", port))
	plain, err := prober.dialContext(phaseCtx, "tcp", target)
	if err != nil {
		result.Error = classifyDialError(err)
		return result
	}
	defer plain.Close()

	tlsConnection := tls.Client(plain, &tls.Config{
		InsecureSkipVerify: true, // Reachability is independent of ACME state.
		MinVersion:         tls.VersionTLS12,
	})
	if err := tlsConnection.HandshakeContext(phaseCtx); err != nil {
		if isTimeout(err) || errors.Is(phaseCtx.Err(), context.DeadlineExceeded) {
			result.Error = &ProbeError{Code: "timeout", Detail: "no TCP/TLS response within 5s"}
		} else {
			result.Error = &ProbeError{Code: "tls_handshake", Detail: sanitizeError(err)}
		}
		return result
	}
	phaseCancel()

	state := tlsConnection.ConnectionState()
	if len(state.PeerCertificates) > 0 {
		result.TLS = describeCertificate(state.PeerCertificates[0])
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConnection.SetDeadline(deadline)
	}

	hostHeader := address.String()
	if address.Is6() {
		hostHeader = "[" + hostHeader + "]"
	}
	request := "GET /api/connectivity/probe HTTP/1.1\r\n" +
		"Host: " + hostHeader + "\r\n" +
		"Accept: application/json\r\n" +
		"Connection: close\r\n\r\n"
	if _, err := io.WriteString(tlsConnection, request); err != nil {
		result.Error = classifyHTTPError(ctx, err)
		return result
	}

	limited := &io.LimitedReader{R: tlsConnection, N: probeHeaderLimit + probeBodyLimit + 1}
	response, err := http.ReadResponse(bufio.NewReaderSize(limited, 4096), &http.Request{Method: http.MethodGet})
	if err != nil {
		result.Error = classifyHTTPError(ctx, err)
		return result
	}
	defer response.Body.Close()
	result.Reachable = true

	if response.StatusCode != http.StatusOK {
		result.Error = &ProbeError{Code: "http_error", Detail: fmt.Sprintf("probe returned HTTP %d", response.StatusCode)}
		return result
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, probeBodyLimit+1))
	if err != nil {
		result.Error = classifyHTTPError(ctx, err)
		return result
	}
	if len(body) > probeBodyLimit {
		result.Error = &ProbeError{Code: "http_error", Detail: "probe response exceeds 4096 bytes"}
		return result
	}
	var payload struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		result.Error = &ProbeError{Code: "http_error", Detail: "probe returned invalid JSON"}
		return result
	}
	if len(payload.Challenge) != len(challenge) || subtle.ConstantTimeCompare([]byte(payload.Challenge), []byte(challenge)) != 1 {
		result.Error = &ProbeError{Code: "challenge_mismatch", Detail: "probe challenge did not match"}
		return result
	}
	result.Verified = true
	return result
}

func describeCertificate(certificate *x509.Certificate) *TLSInfo {
	digest := sha256.Sum256(certificate.Raw)
	sans := append([]string(nil), certificate.DNSNames...)
	for _, address := range certificate.IPAddresses {
		sans = append(sans, address.String())
	}
	sans = append(sans, certificate.EmailAddresses...)
	for _, uri := range certificate.URIs {
		sans = append(sans, uri.String())
	}
	slices.Sort(sans)
	sans = slices.Compact(sans)
	if sans == nil {
		sans = []string{}
	}
	selfSigned := bytes.Equal(certificate.RawIssuer, certificate.RawSubject) &&
		certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature) == nil
	return &TLSInfo{LeafSHA256: hex.EncodeToString(digest[:]), SANs: sans, SelfSigned: selfSigned}
}

func classifyDialError(err error) *ProbeError {
	switch {
	case isTimeout(err):
		return &ProbeError{Code: "timeout", Detail: "no TCP response within 5s"}
	case errors.Is(err, syscall.ECONNREFUSED):
		return &ProbeError{Code: "connection_refused", Detail: "TCP connection was refused"}
	default:
		return &ProbeError{Code: "timeout", Detail: sanitizeError(err)}
	}
}

func classifyHTTPError(ctx context.Context, err error) *ProbeError {
	if isTimeout(err) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &ProbeError{Code: "timeout", Detail: "probe exceeded its 10s budget"}
	}
	return &ProbeError{Code: "http_error", Detail: sanitizeError(err)}
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

// sanitizeError keeps operational detail useful without reflecting targets,
// request bodies, or challenges into logs and API responses.
func sanitizeError(err error) string {
	detail := strings.ReplaceAll(err.Error(), "\r", " ")
	detail = strings.ReplaceAll(detail, "\n", " ")
	if len(detail) > 240 {
		detail = detail[:240]
	}
	return detail
}
