package plagiarismcheckerplus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func sign(secret string, ts int64, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.%s", ts, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature(t *testing.T) {
	const secret = "whsec_test_secret"
	const body = `{"id":"evt_1","type":"scan.completed","data":{}}`
	const ts int64 = 1800000000
	at := func(offset int64) time.Time { return time.Unix(ts+offset, 0) }
	header := fmt.Sprintf("t=%d,v1=%s", ts, sign(secret, ts, body))
	rotated := fmt.Sprintf("t=%d,v1=%s,v1=%s", ts, sign("other", ts, body), sign(secret, ts, body))

	cases := []struct {
		name      string
		secret    string
		header    string
		body      string
		now       time.Time
		tolerance time.Duration
		want      bool
	}{
		{"valid", secret, header, body, at(10), DefaultWebhookTolerance, true},
		{"rotation", secret, rotated, body, at(0), DefaultWebhookTolerance, true},
		{"wrong secret", "whsec_wrong", header, body, at(0), DefaultWebhookTolerance, false},
		{"changed body", secret, header, body + " ", at(0), DefaultWebhookTolerance, false},
		{"too old", secret, header, body, at(301), DefaultWebhookTolerance, false},
		{"too new", secret, header, body, at(-301), DefaultWebhookTolerance, false},
		{"custom tolerance", secret, header, body, at(500), 600 * time.Second, true},
		{"missing t", secret, "v1=" + sign(secret, ts, body), body, at(0), DefaultWebhookTolerance, false},
		{"missing v1", secret, fmt.Sprintf("t=%d", ts), body, at(0), DefaultWebhookTolerance, false},
		{"empty header", secret, "", body, at(0), DefaultWebhookTolerance, false},
	}
	for _, c := range cases {
		if got := VerifyWebhookSignatureAt(c.secret, c.header, []byte(c.body), c.now, c.tolerance); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if WebhookSignatureHeader != "PCP-Signature" {
		t.Errorf("header name %q", WebhookSignatureHeader)
	}
}
