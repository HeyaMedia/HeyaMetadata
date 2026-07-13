package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Captcha issues and verifies Altcha-style proof-of-work challenges: the client
// brute-forces the number whose SHA-256(salt+number) equals a signed challenge
// hash. It is self-hosted (no third party) and enabled only when a signing
// secret is configured — a nil *Captcha means the feature is off and callers
// should skip verification.
type Captcha struct {
	secret []byte
	redis  *redis.Client
}

// Sentinel errors surfaced to the HTTP layer.
var (
	ErrCaptchaRequired = errors.New("captcha solution required")
	ErrCaptchaInvalid  = errors.New("captcha solution is invalid")
)

const (
	captchaAlgorithm = "SHA-256"
	// Kept modest so the browser solves it in well under a second on the main
	// thread; combined with rate limiting this is enough to price out bots.
	captchaMaxNumber = 50000
	captchaTTL       = 10 * time.Minute
)

// NewCaptcha returns a verifier, or nil when disabled (no secret / no Redis).
func NewCaptcha(secret string, client *redis.Client) *Captcha {
	if strings.TrimSpace(secret) == "" || client == nil {
		return nil
	}
	return &Captcha{secret: []byte(secret), redis: client}
}

// Challenge is the Altcha-compatible challenge sent to the client.
type Challenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	MaxNumber int    `json:"maxnumber"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

type captchaSolution struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	Number    int    `json:"number"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

func sha256Hex(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (c *Captcha) sign(challenge string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(challenge))
	return hex.EncodeToString(mac.Sum(nil))
}

// CreateChallenge issues a fresh signed challenge with an embedded expiry.
func (c *Captcha) CreateChallenge(now time.Time) (Challenge, error) {
	saltBytes := make([]byte, 12)
	if _, err := rand.Read(saltBytes); err != nil {
		return Challenge{}, fmt.Errorf("generate captcha salt: %w", err)
	}
	numberBytes := make([]byte, 4)
	if _, err := rand.Read(numberBytes); err != nil {
		return Challenge{}, fmt.Errorf("generate captcha number: %w", err)
	}
	number := int(binary.BigEndian.Uint32(numberBytes) % (captchaMaxNumber + 1))
	// Altcha embeds an expiry in the salt as a query suffix; the whole salt
	// string is part of the hashed input on both sides.
	salt := hex.EncodeToString(saltBytes) + "?expires=" + strconv.FormatInt(now.Add(captchaTTL).Unix(), 10)
	challenge := sha256Hex(salt + strconv.Itoa(number))
	return Challenge{
		Algorithm: captchaAlgorithm,
		Challenge: challenge,
		MaxNumber: captchaMaxNumber,
		Salt:      salt,
		Signature: c.sign(challenge),
	}, nil
}

// Verify validates a base64 solution and consumes it (single-use via Redis).
func (c *Captcha) Verify(ctx context.Context, payload string, now time.Time) error {
	solution, err := c.verifySolution(payload, now)
	if err != nil {
		return err
	}
	// Claim the challenge so a valid solution can be used exactly once.
	ok, err := c.redis.SetNX(ctx, captchaKey(solution.Challenge), "1", captchaTTL).Result()
	if err != nil {
		return fmt.Errorf("record captcha single-use: %w", err)
	}
	if !ok {
		return ErrCaptchaInvalid
	}
	return nil
}

// verifySolution checks signature authenticity, the proof-of-work, and expiry —
// everything except the Redis single-use claim (kept pure for testing).
func (c *Captcha) verifySolution(payload string, now time.Time) (captchaSolution, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return captchaSolution{}, ErrCaptchaRequired
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return captchaSolution{}, ErrCaptchaInvalid
	}
	var solution captchaSolution
	if err := json.Unmarshal(raw, &solution); err != nil {
		return captchaSolution{}, ErrCaptchaInvalid
	}
	if solution.Algorithm != captchaAlgorithm || solution.Challenge == "" || solution.Signature == "" {
		return captchaSolution{}, ErrCaptchaInvalid
	}
	// Authenticity: the challenge must be one we signed.
	expectedSig := c.sign(solution.Challenge)
	if subtle.ConstantTimeCompare([]byte(expectedSig), []byte(solution.Signature)) != 1 {
		return captchaSolution{}, ErrCaptchaInvalid
	}
	// Proof-of-work: the number must hash to the challenge.
	if sha256Hex(solution.Salt+strconv.Itoa(solution.Number)) != solution.Challenge {
		return captchaSolution{}, ErrCaptchaInvalid
	}
	if err := captchaExpiry(solution.Salt, now); err != nil {
		return captchaSolution{}, err
	}
	return solution, nil
}

func captchaExpiry(salt string, now time.Time) error {
	index := strings.IndexByte(salt, '?')
	if index < 0 {
		return nil
	}
	values, err := url.ParseQuery(salt[index+1:])
	if err != nil {
		return ErrCaptchaInvalid
	}
	expires := values.Get("expires")
	if expires == "" {
		return nil
	}
	unix, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return ErrCaptchaInvalid
	}
	if now.After(time.Unix(unix, 0)) {
		return ErrCaptchaInvalid
	}
	return nil
}

func captchaKey(challenge string) string {
	return "heya:metadata:v2:captcha:" + challenge
}
