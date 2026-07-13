package auth

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func solveCaptcha(t *testing.T, challenge Challenge) int {
	t.Helper()
	for number := 0; number <= challenge.MaxNumber; number++ {
		if sha256Hex(challenge.Salt+strconv.Itoa(number)) == challenge.Challenge {
			return number
		}
	}
	t.Fatal("no proof-of-work solution found")
	return 0
}

func encodeCaptchaSolution(challenge Challenge, number int) string {
	payload, _ := json.Marshal(captchaSolution{
		Algorithm: challenge.Algorithm,
		Challenge: challenge.Challenge,
		Number:    number,
		Salt:      challenge.Salt,
		Signature: challenge.Signature,
	})
	return base64.StdEncoding.EncodeToString(payload)
}

func TestCaptchaVerifySolution(t *testing.T) {
	captcha := &Captcha{secret: []byte("captcha-test-secret")}
	now := time.Unix(1_700_000_000, 0)

	challenge, err := captcha.CreateChallenge(now)
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	number := solveCaptcha(t, challenge)
	valid := encodeCaptchaSolution(challenge, number)

	if _, err := captcha.verifySolution(valid, now); err != nil {
		t.Fatalf("valid solution rejected: %v", err)
	}

	if _, err := captcha.verifySolution("", now); err != ErrCaptchaRequired {
		t.Fatalf("empty payload: want ErrCaptchaRequired, got %v", err)
	}

	wrongNumber := encodeCaptchaSolution(challenge, (number+1)%(challenge.MaxNumber+1))
	if _, err := captcha.verifySolution(wrongNumber, now); err != ErrCaptchaInvalid {
		t.Fatalf("wrong number: want ErrCaptchaInvalid, got %v", err)
	}

	// A challenge signed with a different secret must not verify.
	forged := &Captcha{secret: []byte("a-different-secret")}
	forgedChallenge, _ := forged.CreateChallenge(now)
	forgedSolution := encodeCaptchaSolution(forgedChallenge, solveCaptcha(t, forgedChallenge))
	if _, err := captcha.verifySolution(forgedSolution, now); err != ErrCaptchaInvalid {
		t.Fatalf("forged signature: want ErrCaptchaInvalid, got %v", err)
	}

	if _, err := captcha.verifySolution(valid, now.Add(captchaTTL+time.Minute)); err != ErrCaptchaInvalid {
		t.Fatalf("expired solution: want ErrCaptchaInvalid, got %v", err)
	}

	if _, err := captcha.verifySolution("not valid base64!!!", now); err != ErrCaptchaInvalid {
		t.Fatalf("malformed payload: want ErrCaptchaInvalid, got %v", err)
	}
}

func TestNewCaptchaDisabledWithoutSecret(t *testing.T) {
	if NewCaptcha("", nil) != nil {
		t.Fatal("captcha must be disabled when no secret is configured")
	}
}
