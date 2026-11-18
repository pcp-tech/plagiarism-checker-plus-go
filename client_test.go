package plagiarismcheckerplus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reply struct {
	status  int
	headers map[string]string
	body    string
}

func server(t *testing.T, replies []reply, seen *[]*http.Request, bodies *[]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*seen = append(*seen, r)
		*bodies = append(*bodies, string(raw))
		next := replies[0]
		replies = replies[1:]
		for k, v := range next.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(next.status)
		_, _ = w.Write([]byte(next.body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("pcp_test")
	c.BaseURL = srv.URL
	return c
}

func TestCreateScan(t *testing.T) {
	var seen []*http.Request
	var bodies []string
	c := server(t, []reply{{202, nil, `{"id":"s1","status":"processing"}`}}, &seen, &bodies)
	out, err := c.CreateScan(context.Background(), "text", []string{ToolPlagiarism}, "draft")
	if err != nil || out.ID != "s1" {
		t.Fatalf("got %v, %v", out, err)
	}
	if seen[0].Method != http.MethodPost || seen[0].URL.Path != "/scans" {
		t.Fatalf("bad request %s %s", seen[0].Method, seen[0].URL.Path)
	}
	if seen[0].Header.Get("Authorization") != "Bearer pcp_test" {
		t.Fatal("missing bearer token")
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(bodies[0]), &body)
	if body["text"] != "text" || body["name"] != "draft" {
		t.Fatalf("bad body %v", body)
	}
}

func TestQueryParameters(t *testing.T) {
	var seen []*http.Request
	var bodies []string
	c := server(t, []reply{{200, nil, `{"id":"s1"}`}, {200, nil, `{"data":[]}`}}, &seen, &bodies)
	if _, err := c.GetScan(context.Background(), "s1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListScans(context.Background(), 2, 5); err != nil {
		t.Fatal(err)
	}
	if seen[0].URL.RawQuery != "include_text=true" || !strings.HasSuffix(seen[0].URL.Path, "/scans/s1") {
		t.Fatalf("bad getScan url %s", seen[0].URL)
	}
	if seen[1].URL.RawQuery != "limit=5&page=2" {
		t.Fatalf("bad listScans query %s", seen[1].URL.RawQuery)
	}
}

func TestErrorCodeAndDetails(t *testing.T) {
	var seen []*http.Request
	var bodies []string
	c := server(t, []reply{{402, nil, `{"error":{"code":"insufficient_words","message":"Not enough API words","wordsNeeded":900,"wordsAvailable":100,"balances":{"purchasedWords":100},"billingUrl":"https://app.plagiarismcheckerplus.com/developers/billing"}}`}}, &seen, &bodies)
	_, err := c.CreateScan(context.Background(), "text", []string{ToolPlagiarism}, "")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 402 || apiErr.Code != CodeInsufficientWords {
		t.Fatalf("got %v", err)
	}
	d := apiErr.Details
	if d.WordsNeeded != 900 || d.WordsAvailable != 100 || d.BillingURL == "" || d.Balances == nil || d.Balances.PurchasedWords != 100 {
		t.Fatalf("bad details %+v", d)
	}
	if apiErr.Message != "Not enough API words" {
		t.Fatalf("bad message %q", apiErr.Message)
	}
	if CodeTrialExhausted != "trial_exhausted" || CodeKeyCapReached != "key_cap_reached" || CodePlanRequired != "plan_required" {
		t.Fatal("bad code constants")
	}
}

func TestAccountAndSandbox(t *testing.T) {
	var seen []*http.Request
	var bodies []string
	c := server(t, []reply{
		{200, nil, `{"id":"u1","plan":"free","trial":{"wordsTotal":2000,"wordsUsed":500,"wordsRemaining":1500},"balances":{"apiPlan":null,"appPlan":null,"purchasedWords":0,"trialRemaining":1500,"totalAvailable":1500},"key":{"mode":"test","monthlyWordCap":null,"wordsUsedThisMonth":0},"limits":{"maxDocumentWords":1000,"requestsPerMinute":10}}`},
		{200, nil, `{"id":"sbx_1","status":"completed","sandbox":true}`},
	}, &seen, &bodies)
	acct, err := c.GetAccount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acct.Trial == nil || acct.Trial.WordsRemaining != 1500 || acct.Balances == nil || acct.Balances.TotalAvailable != 1500 || acct.Key == nil || acct.Key.Mode != "test" || acct.Limits.RequestsPerMinute != 10 {
		t.Fatalf("bad account %+v", acct)
	}
	scan, err := c.GetScan(context.Background(), "sbx_1", false)
	if err != nil || !scan.Sandbox {
		t.Fatalf("got %+v, %v", scan, err)
	}
}

func TestWaitHonoursRetryAfter(t *testing.T) {
	var seen []*http.Request
	var bodies []string
	c := server(t, []reply{
		{200, nil, `{"id":"s1","status":"processing"}`},
		{429, map[string]string{"Retry-After": "3"}, `{"error":{"code":"rate_limited","message":"Slow down"}}`},
		{200, nil, `{"id":"s1","status":"completed"}`},
	}, &seen, &bodies)
	var waits []time.Duration
	c.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	scan, err := c.WaitForScan(context.Background(), "s1", WaitOptions{Interval: time.Second})
	if err != nil || scan.Status != "completed" {
		t.Fatalf("got %v, %v", scan, err)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 3*time.Second {
		t.Fatalf("bad waits %v", waits)
	}
}

func TestWaitTimesOut(t *testing.T) {
	var seen []*http.Request
	var bodies []string
	c := server(t, []reply{{200, nil, `{"id":"s1","status":"processing"}`}}, &seen, &bodies)
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	_, err := c.WaitForScan(context.Background(), "s1", WaitOptions{Timeout: 10 * time.Millisecond, Interval: time.Second})
	if !errors.Is(err, ErrScanTimeout) {
		t.Fatalf("got %v", err)
	}
}
