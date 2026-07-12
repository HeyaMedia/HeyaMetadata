// Package fingerprint generates and compares raw Chromaprint fingerprints.
package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	Algorithm        = "chromaprint"
	AlgorithmVersion = "chromaprint-raw/v1"
	maxPreviewBytes  = 8 << 20
	MinOverlapItems  = 80
	MaxBitError      = 0.25
	EdgeTrimItems    = 16
)

type Fingerprint struct {
	Hashes   []uint32
	Duration float64
}

type MatchResult struct {
	Match    bool    `json:"match"`
	BitError float64 `json:"bit_error"`
	Overlap  int     `json:"overlap"`
	Offset   int     `json:"offset"`
}

type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

type Calculator struct {
	path   string
	client *http.Client
}

func NewCalculator(path string) *Calculator {
	if path == "" {
		path, _ = exec.LookPath("fpcalc")
	}
	return &Calculator{path: path, client: &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return validatePreviewURL(req.URL)
		},
	}}
}

func (c *Calculator) Available() bool {
	if c == nil || c.path == "" {
		return false
	}
	_, err := exec.LookPath(c.path)
	return err == nil
}

func (c *Calculator) Version(ctx context.Context) string {
	if !c.Available() {
		return "unavailable"
	}
	out, err := exec.CommandContext(ctx, c.path, "-version").CombinedOutput() //nolint:gosec // configured executable
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "unknown"
	}
	value := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(value) > 255 {
		value = value[:255]
	}
	return value
}

func (c *Calculator) FromURL(ctx context.Context, rawURL string) (Fingerprint, error) {
	if !c.Available() {
		return Fingerprint{}, errors.New("fpcalc not available")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("parse preview URL: %w", err)
	}
	if err := validatePreviewURL(parsed); err != nil {
		return Fingerprint{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Fingerprint{}, err
	}
	req.Header.Set("User-Agent", "HeyaMetadata/dev (https://github.com/HeyaMedia/HeyaMetadata)")
	resp, err := c.client.Do(req)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("fetch preview: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		statusErr := fmt.Errorf("preview fetch returned HTTP %d", resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return Fingerprint{}, &PermanentError{Err: statusErr}
		}
		return Fingerprint{}, statusErr
	}
	tmp, err := os.CreateTemp("", "heya-fingerprint-*.audio")
	if err != nil {
		return Fingerprint{}, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	n, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, maxPreviewBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return Fingerprint{}, fmt.Errorf("download preview: %w", copyErr)
	}
	if closeErr != nil {
		return Fingerprint{}, fmt.Errorf("close preview: %w", closeErr)
	}
	if n == 0 {
		return Fingerprint{}, &PermanentError{Err: errors.New("empty preview body")}
	}
	if n > maxPreviewBytes {
		return Fingerprint{}, &PermanentError{Err: fmt.Errorf("preview exceeds %d byte cap", maxPreviewBytes)}
	}
	return c.fromFile(ctx, name)
}

type fpcalcOutput struct {
	Duration    float64       `json:"duration"`
	Fingerprint []json.Number `json:"fingerprint"`
}

func (c *Calculator) fromFile(ctx context.Context, name string) (Fingerprint, error) {
	cmd := exec.CommandContext(ctx, c.path, "-raw", "-json", name) //nolint:gosec // configured executable and private temp file
	out, err := cmd.Output()
	if err != nil {
		return Fingerprint{}, fmt.Errorf("fpcalc: %w", err)
	}
	var parsed fpcalcOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return Fingerprint{}, fmt.Errorf("decode fpcalc output: %w", err)
	}
	if len(parsed.Fingerprint) == 0 {
		return Fingerprint{}, &PermanentError{Err: errors.New("fpcalc returned an empty fingerprint")}
	}
	hashes := make([]uint32, len(parsed.Fingerprint))
	for i, raw := range parsed.Fingerprint {
		value, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil {
			return Fingerprint{}, fmt.Errorf("decode fingerprint hash %d: %w", i, err)
		}
		hashes[i] = uint32(uint64(value))
	}
	return Fingerprint{Hashes: hashes, Duration: parsed.Duration}, nil
}

// SourceChecksum is stable when a provider rotates a signed query string for
// the same preview object. It is safe to retain; the URL itself is not.
func SourceChecksum(provider, trackID, rawURL string) string {
	parsed, _ := url.Parse(rawURL)
	if parsed != nil {
		parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
		rawURL = parsed.String()
	}
	sum := sha256.Sum256([]byte(provider + "\x00" + trackID + "\x00" + rawURL))
	return hex.EncodeToString(sum[:])
}

func validatePreviewURL(value *url.URL) error {
	if value == nil || value.Scheme != "https" || value.User != nil {
		return &PermanentError{Err: errors.New("preview URL must be credential-free HTTPS")}
	}
	host := strings.ToLower(value.Hostname())
	allowed := host == "audio-ssl.itunes.apple.com" || host == "audio.itunes.apple.com" ||
		strings.HasSuffix(host, ".mzstatic.com") || strings.HasSuffix(host, ".dzcdn.net")
	if !allowed || net.ParseIP(host) != nil {
		return &PermanentError{Err: fmt.Errorf("preview host %q is not allowed", host)}
	}
	return nil
}

func Pack(hashes []uint32) []byte {
	out := make([]byte, len(hashes)*4)
	for i, hash := range hashes {
		binary.LittleEndian.PutUint32(out[i*4:], hash)
	}
	return out
}

func Unpack(data []byte) []uint32 {
	out := make([]uint32, len(data)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	return out
}

func Match(a, b Fingerprint) MatchResult {
	left, right := trim(a.Hashes), trim(b.Hashes)
	if len(left) < MinOverlapItems || len(right) < MinOverlapItems {
		return MatchResult{}
	}
	best, found := MatchResult{}, false
	for offset := -(len(right) - 1); offset <= len(left)-1; offset++ {
		li, ri := 0, 0
		if offset > 0 {
			li = offset
		} else {
			ri = -offset
		}
		overlap := min(len(left)-li, len(right)-ri)
		if overlap < MinOverlapItems {
			continue
		}
		difference := 0
		for i := 0; i < overlap; i++ {
			difference += bits.OnesCount32(left[li+i] ^ right[ri+i])
		}
		ber := float64(difference) / float64(32*overlap)
		if !found || ber < best.BitError {
			best, found = MatchResult{BitError: ber, Overlap: overlap, Offset: offset}, true
		}
	}
	best.Match = found && best.BitError <= MaxBitError
	return best
}

func trim(values []uint32) []uint32 {
	if len(values)-2*EdgeTrimItems >= MinOverlapItems {
		return values[EdgeTrimItems : len(values)-EdgeTrimItems]
	}
	return values
}
