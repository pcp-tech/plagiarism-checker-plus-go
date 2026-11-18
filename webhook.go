package plagiarismcheckerplus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	// WebhookSignatureHeader carries the signature of every webhook delivery.
	WebhookSignatureHeader = "PCP-Signature"
	// DefaultWebhookTolerance is how old a signed timestamp may be before it is rejected.
	DefaultWebhookTolerance = 300 * time.Second
)

// VerifyWebhookSignature checks a PCP-Signature header,
// t=<unix seconds>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>,
// against the raw request body (read it before any JSON decoding). It uses
// the current time and DefaultWebhookTolerance.
func VerifyWebhookSignature(secret, header string, body []byte) bool {
	return VerifyWebhookSignatureAt(secret, header, body, time.Now(), DefaultWebhookTolerance)
}

// VerifyWebhookSignatureAt is VerifyWebhookSignature with an explicit current
// time and tolerance. It returns false for a missing or malformed header, a
// wrong signature, or a timestamp more than tolerance away from now.
// Signatures are compared in constant time.
func VerifyWebhookSignatureAt(secret, header string, body []byte, now time.Time, tolerance time.Duration) bool {
	if secret == "" || header == "" {
		return false
	}
	var timestamp int64 = -1
	var signatures [][]byte
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "t":
			if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= 0 && !strings.HasPrefix(value, "+") {
				timestamp = n
			}
		case "v1":
			if sig, err := hex.DecodeString(value); err == nil && len(sig) > 0 {
				signatures = append(signatures, sig)
			}
		}
	}
	if timestamp < 0 || len(signatures) == 0 {
		return false
	}
	age := now.Sub(time.Unix(timestamp, 0))
	if age < 0 {
		age = -age
	}
	if age > tolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	expected := mac.Sum(nil)
	for _, sig := range signatures {
		if hmac.Equal(sig, expected) {
			return true
		}
	}
	return false
}
