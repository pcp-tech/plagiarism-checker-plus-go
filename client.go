// Package plagiarismcheckerplus is the official Go client for the Plagiarism
// Checker Plus API: plagiarism checks with sources and AI writing detection.
//
// Get a free API key at https://app.plagiarismcheckerplus.com/developers.
// Test keys (pcp_test_...) are free and unlimited and return sample results
// marked Sandbox; free accounts also get 5,000 live API words once. Pricing:
// https://plagiarismcheckerplus.com/api-pricing.
package plagiarismcheckerplus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the production API.
	DefaultBaseURL = "https://app.plagiarismcheckerplus.com/api/public/v1"
	// Version of this library.
	Version = "1.1.0"

	// CodePlanRequired is no longer returned: free accounts get a free API
	// trial and free test keys.
	//
	// Deprecated: kept so existing code still compiles.
	CodePlanRequired = "plan_required"
	CodeTextTooShort = "text_too_short"
	CodeDocTooLong   = "doc_too_long"
	// CodeInsufficientWords (402): not enough words across all sources.
	// Error.Details has WordsNeeded, WordsAvailable, Balances and BillingURL.
	CodeInsufficientWords = "insufficient_words"
	// CodeTrialExhausted (402): the free API trial cannot cover the request.
	// Error.Details has WordsNeeded, TrialRemaining, TrialTotal and BillingURL.
	CodeTrialExhausted = "trial_exhausted"
	// CodeKeyCapReached (402): the key's monthly word cap would be exceeded.
	// Error.Details has WordsNeeded, MonthlyWordCap and UsedThisMonth.
	CodeKeyCapReached      = "key_cap_reached"
	CodeInvalidRequest     = "invalid_request"
	CodeInvalidJSON        = "invalid_json"
	CodeUnauthorized       = "unauthorized"
	CodeInvalidAPIKey      = "invalid_api_key"
	CodeKeyRevoked         = "key_revoked"
	CodeEmailUnverified    = "email_unverified"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeRateLimited        = "rate_limited"
	CodeWorkersUnavailable = "workers_unavailable"
	CodeInternalError      = "internal_error"

	ToolPlagiarism = "plagiarism"
	ToolAIDetect   = "ai_detect"
)

// ErrorDetails holds the detail fields some errors carry (402 errors in
// particular). Fields an error does not send are zero.
type ErrorDetails struct {
	WordsNeeded    int    `json:"wordsNeeded"`
	WordsAvailable int    `json:"wordsAvailable"`
	BillingURL     string `json:"billingUrl"`
	TrialRemaining int    `json:"trialRemaining"`
	TrialTotal     int    `json:"trialTotal"`
	MonthlyWordCap int    `json:"monthlyWordCap"`
	UsedThisMonth  int    `json:"usedThisMonth"`
	// Balances: what each source could still pay (insufficient_words).
	Balances *struct {
		APIPlanRemaining    int `json:"apiPlanRemaining"`
		AppMonthlyRemaining int `json:"appMonthlyRemaining"`
		AppBonusBalance     int `json:"appBonusBalance"`
		PurchasedWords      int `json:"purchasedWords"`
		TrialRemaining      int `json:"trialRemaining"`
	} `json:"balances"`
}

// Error is a non-2xx response. Code is the API's error code, such as insufficient_words.
type Error struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration // zero when not sent
	Details    ErrorDetails
	Body       []byte
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("plagiarism checker plus: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("plagiarism checker plus: %d: %s", e.Status, e.Message)
}

// ErrScanTimeout is returned by WaitForScan when the timeout passes.
var ErrScanTimeout = errors.New("plagiarism checker plus: scan did not finish in time")

// Client calls the API. Create one with NewClient.
type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	// Sleep waits between polls in WaitForScan; replaceable in tests.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewClient returns a client for the production API.
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Sleep:      sleepContext,
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ParseRetryAfter reads a Retry-After header (delta-seconds or HTTP date).
func ParseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// CreateScanResponse is the 202 body of POST /scans.
type CreateScanResponse struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	WordCount    int      `json:"wordCount"`
	WordsCharged int      `json:"wordsCharged"`
	Tools        []string `json:"tools"`
	// Sandbox is true for scans made with a test key.
	Sandbox bool `json:"sandbox"`
}

// ScanSummary is one scan in a list.
type ScanSummary struct {
	ID           string   `json:"id"`
	Name         *string  `json:"name"`
	Status       string   `json:"status"` // pending, processing, completed or failed
	WordCount    int      `json:"wordCount"`
	WordsCharged int      `json:"wordsCharged"`
	Tools        []string `json:"tools"`
	CreatedAt    string   `json:"createdAt"`
	CompletedAt  *string  `json:"completedAt"`
	// Sandbox is true for scans made with a test key (pcp_test_...):
	// fixed sample results, nothing checked or charged.
	Sandbox bool `json:"sandbox"`
}

// Source is a page that matched part of the text.
type Source struct {
	URL             string  `json:"url"`
	Domain          string  `json:"domain"`
	MatchPercentage float64 `json:"matchPercentage"`
	MatchedText     string  `json:"matchedText"`
	Excerpt         *string `json:"excerpt"`
}

// Sentence is a per-sentence AI verdict. Probability is a likelihood, not proof.
type Sentence struct {
	Text        string  `json:"text"`
	Probability float64 `json:"probability"`
	Offset      int     `json:"offset"`
	Length      int     `json:"length"`
}

// ScanDetail is the body of GET /scans/{id}.
type ScanDetail struct {
	ScanSummary
	Text     *string `json:"text,omitempty"`
	Progress *struct {
		Percent int `json:"percent"`
		Tools   []struct {
			Tool    string `json:"tool"`
			Status  string `json:"status"`
			Percent int    `json:"percent"`
		} `json:"tools"`
	} `json:"progress,omitempty"`
	Results *struct {
		Plagiarism *struct {
			Score         float64  `json:"score"`
			UniqueSources int      `json:"uniqueSources"`
			Sources       []Source `json:"sources"`
		} `json:"plagiarism"`
		AIDetection *struct {
			Score     float64    `json:"score"`
			Summary   *string    `json:"summary"`
			Sentences []Sentence `json:"sentences"`
		} `json:"aiDetection"`
	} `json:"results,omitempty"`
}

// ScanList is the body of GET /scans.
type ScanList struct {
	Data       []ScanSummary `json:"data"`
	Pagination struct {
		Page       int `json:"page"`
		Limit      int `json:"limit"`
		Total      int `json:"total"`
		TotalPages int `json:"totalPages"`
	} `json:"pagination"`
}

// AccountTrial is the one-time free API trial (5,000 words) of a free account.
type AccountTrial struct {
	WordsTotal     int `json:"wordsTotal"`
	WordsUsed      int `json:"wordsUsed"`
	WordsRemaining int `json:"wordsRemaining"`
}

// AccountBalances is what live API requests can draw on, in charging order.
type AccountBalances struct {
	APIPlan *struct {
		Plan              string  `json:"plan"`
		Name              string  `json:"name"`
		Status            string  `json:"status"`
		WordsLimit        int     `json:"wordsLimit"`
		WordsUsed         int     `json:"wordsUsed"`
		WordsRemaining    int     `json:"wordsRemaining"`
		PeriodEnd         *string `json:"periodEnd"`
		CancelAtPeriodEnd bool    `json:"cancelAtPeriodEnd"`
	} `json:"apiPlan"`
	AppPlan *struct {
		Plan             string `json:"plan"`
		MonthlyRemaining int    `json:"monthlyRemaining"`
		BonusBalance     int    `json:"bonusBalance"`
	} `json:"appPlan"`
	PurchasedWords int `json:"purchasedWords"`
	TrialRemaining int `json:"trialRemaining"`
	TotalAvailable int `json:"totalAvailable"`
}

// AccountAutoTopup is the auto top-up status.
type AccountAutoTopup struct {
	Enabled           bool     `json:"enabled"`
	ThresholdWords    *int     `json:"thresholdWords"`
	TopupWords        *int     `json:"topupWords"`
	MonthlyCapUSD     *float64 `json:"monthlyCapUsd"`
	SpentThisMonthUSD float64  `json:"spentThisMonthUsd"`
	LastStatus        *string  `json:"lastStatus"`
}

// AccountKey describes the calling key.
type AccountKey struct {
	Mode               string `json:"mode"` // "live" or "test"
	MonthlyWordCap     *int   `json:"monthlyWordCap"`
	WordsUsedThisMonth int    `json:"wordsUsedThisMonth"`
}

// Account is the body of GET /account.
type Account struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Plan  string `json:"plan"`
	// Words are the app plan's words.
	Words struct {
		MonthlyLimit     int `json:"monthlyLimit"`
		MonthlyUsed      int `json:"monthlyUsed"`
		MonthlyRemaining int `json:"monthlyRemaining"`
		BonusBalance     int `json:"bonusBalance"`
	} `json:"words"`
	// Trial is nil on paid app plans, which never use it.
	Trial     *AccountTrial     `json:"trial"`
	Balances  *AccountBalances  `json:"balances"`
	AutoTopup *AccountAutoTopup `json:"autoTopup"`
	Key       *AccountKey       `json:"key"`
	// BillingURL is where to buy API words or choose an API plan.
	BillingURL string `json:"billingUrl"`
	Limits     struct {
		MaxDocumentWords  *int `json:"maxDocumentWords"`
		RequestsPerMinute int  `json:"requestsPerMinute"`
	} `json:"limits"`
	MonthlyResetAt *string `json:"monthlyResetAt"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	endpoint := strings.TrimRight(c.BaseURL, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "plagiarism-checker-plus-go/"+Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &Error{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode), Body: raw,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"))}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				ErrorDetails
			} `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil {
			apiErr.Code = envelope.Error.Code
			apiErr.Details = envelope.Error.ErrorDetails
			if envelope.Error.Message != "" {
				apiErr.Message = envelope.Error.Message
			}
		}
		return apiErr
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// CreateScan submits text. tools: ToolPlagiarism and/or ToolAIDetect. name may be empty.
func (c *Client) CreateScan(ctx context.Context, text string, tools []string, name string) (*CreateScanResponse, error) {
	body := map[string]any{"text": text, "tools": tools}
	if name != "" {
		body["name"] = name
	}
	var out CreateScanResponse
	if err := c.do(ctx, http.MethodPost, "/scans", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetScan reads a scan and its results.
func (c *Client) GetScan(ctx context.Context, id string, includeText bool) (*ScanDetail, error) {
	var query url.Values
	if includeText {
		query = url.Values{"include_text": {"true"}}
	}
	var out ScanDetail
	if err := c.do(ctx, http.MethodGet, "/scans/"+url.PathEscape(id), query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListScans lists scans, newest first. Zero page or limit uses the API default.
func (c *Client) ListScans(ctx context.Context, page, limit int) (*ScanList, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var out ScanList
	if err := c.do(ctx, http.MethodGet, "/scans", query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAccount returns the plan, word balances, free API trial, auto top-up
// status and the calling key's mode and monthly cap.
func (c *Client) GetAccount(ctx context.Context) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodGet, "/account", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitOptions tunes WaitForScan. Zero values use the defaults.
type WaitOptions struct {
	Timeout     time.Duration // default 5 minutes
	Interval    time.Duration // default 2 seconds
	MaxInterval time.Duration // default 10 seconds
	IncludeText bool
}

// WaitForScan polls until the scan is completed or failed. The delay grows by
// 1.5x up to MaxInterval, and a 429 waits for Retry-After.
func (c *Client) WaitForScan(ctx context.Context, id string, opts WaitOptions) (*ScanDetail, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	if opts.MaxInterval <= 0 {
		opts.MaxInterval = 10 * time.Second
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	deadline := time.Now().Add(opts.Timeout)
	delay := opts.Interval
	for {
		wait := delay
		scan, err := c.GetScan(ctx, id, opts.IncludeText)
		if err == nil {
			if scan.Status == "completed" || scan.Status == "failed" {
				return scan, nil
			}
			delay = time.Duration(float64(delay) * 1.5)
			if delay > opts.MaxInterval {
				delay = opts.MaxInterval
			}
		} else {
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
				return nil, err
			}
			if apiErr.RetryAfter > 0 {
				wait = apiErr.RetryAfter
			}
		}
		if time.Now().Add(wait).After(deadline) {
			return nil, ErrScanTimeout
		}
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}
