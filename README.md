# Plagiarism Checker Plus for Go

Official client for the [Plagiarism Checker Plus API](https://plagiarismcheckerplus.com/integrations/api). Go 1.21+, standard library only.

Get a free API key in the [developer portal](https://app.plagiarismcheckerplus.com/developers). Test keys are free and unlimited, and every free account gets 2,000 live API words once, with no card. See [API pricing](https://plagiarismcheckerplus.com/api-pricing).

## Install

```sh
go get github.com/pcp-tech/plagiarism-checker-plus-go
```

## Quick start

Get a free API key at https://app.plagiarismcheckerplus.com/developers (test keys are free and unlimited).

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	pcp "github.com/pcp-tech/plagiarism-checker-plus-go"
)

func main() {
	ctx := context.Background()
	client := pcp.NewClient(os.Getenv("PCP_API_KEY"))

	// 50+ words for AI detection.
	created, err := client.CreateScan(ctx, essayText, []string{pcp.ToolPlagiarism, pcp.ToolAIDetect}, "essay-draft-1")
	var apiErr *pcp.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case pcp.CodeTextTooShort:
			log.Fatal("Send at least 30 words for plagiarism, 50 for AI detection.")
		case pcp.CodeTrialExhausted:
			log.Fatalf("Free API trial used up. Buy words or choose an API plan: %s", apiErr.Details.BillingURL)
		case pcp.CodeInsufficientWords:
			log.Fatalf("Need %d words, %d available. Top up: %s", apiErr.Details.WordsNeeded, apiErr.Details.WordsAvailable, apiErr.Details.BillingURL)
		case pcp.CodeKeyCapReached:
			log.Fatalf("This key reached its monthly cap of %d words.", apiErr.Details.MonthlyWordCap)
		}
	}
	if err != nil {
		log.Fatal(err)
	}

	scan, err := client.WaitForScan(ctx, created.ID, pcp.WaitOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if scan.Sandbox {
		fmt.Println("Test key: these are sample results.")
	}
	if scan.Results == nil {
		log.Fatalf("scan ended with status %s", scan.Status)
	}
	if p := scan.Results.Plagiarism; p != nil {
		fmt.Printf("Matched: %.1f%%\n", p.Score)
		for _, s := range p.Sources {
			fmt.Println(s.URL)
		}
	}
	// A likelihood, not proof that the text was written by AI.
	if a := scan.Results.AIDetection; a != nil {
		fmt.Printf("AI likelihood: %.1f%%\n", a.Score)
	}
}
```

## Test keys, free trial and pricing

- **Test keys** (`pcp_test_...`) are free and unlimited on every plan, including free. They never run a check: `CreateScan` returns as usual and `GetScan` returns `completed` with fixed sample results in the same shape as a real scan, marked `Sandbox: true`. Use them to build and test your integration.
- **Free API trial**: each free account gets 2,000 API words once for live keys (`pcp_...`), with no card. When the trial cannot cover a request, the API returns `402 trial_exhausted` and nothing is charged.
- **Paid app plans** (Student, Personal, Team) use their plan words for API calls.
- **API plans**: Starter $25/month (300,000 words), Growth $99/month (1,500,000 words), Scale $399/month (7,500,000 words).
- **Pay as you go**: words from $4.50 (50,000 words, $0.09 per 1,000 words). Purchased words never expire.

A scan costs its word count once per tool: 800 words checked for plagiarism and AI costs 1,600 words. Prices: [API pricing](https://plagiarismcheckerplus.com/api-pricing). Keys, usage, billing and webhooks: [developer portal](https://app.plagiarismcheckerplus.com/developers). `GetAccount` returns what is left in `trial` and `balances`, plus `autoTopup` and this key's `key.mode` and `key.monthlyWordCap`.

The rate limit is 60 requests a minute per key on paid app plans and for accounts with an API plan or purchased API words, and 10 a minute otherwise (free trial, test keys on the free plan).

## Webhooks

Instead of polling, register an https endpoint in the [developer portal](https://app.plagiarismcheckerplus.com/developers) to receive `scan.completed` and `scan.failed` for checks made with live keys. Each delivery carries

```
PCP-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>
```

`VerifyWebhookSignature(secret, header, rawBody)` checks it with the endpoint's signing secret (`whsec_...`). Pass the raw request body, before JSON parsing. It compares in constant time and rejects timestamps more than 300 seconds from now (the tolerance is configurable). Use `VerifyWebhookSignatureAt(secret, header, rawBody, now, tolerance)` to set the time and tolerance yourself.

```go
http.HandleFunc("/webhooks/pcp", func(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body) // raw body, before JSON decoding
	if err != nil || !pcp.VerifyWebhookSignature(os.Getenv("PCP_WEBHOOK_SECRET"), r.Header.Get(pcp.WebhookSignatureHeader), raw) {
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}
	var event struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Scan struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"scan"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		http.Error(w, "Bad payload", http.StatusBadRequest)
		return
	}
	switch event.Type {
	case "scan.completed":
		log.Printf("Scan done: %s", event.Data.Scan.ID)
	case "scan.failed":
		log.Printf("Scan failed: %s", event.Data.Scan.ID)
	}
	w.WriteHeader(http.StatusOK)
})
```

An event can be delivered more than once (retries). Use its `id` to ignore repeats.

## Methods

| Method | API call |
|---|---|
| `CreateScan(ctx, text, tools, name)` | `POST /scans` |
| `GetScan(ctx, id, includeText)` | `GET /scans/{id}` |
| `ListScans(ctx, page, limit)` | `GET /scans` |
| `GetAccount(ctx)` | `GET /account` |
| `WaitForScan(ctx, id, WaitOptions{})` | Polls until `completed` or `failed` |
| `VerifyWebhookSignature(secret, header, body)` | Checks a webhook's `PCP-Signature` (package function, no API call) |

`WaitForScan` starts at 2 seconds, grows by 1.5x up to 10 seconds, waits for `Retry-After` on `429`, and returns `ErrScanTimeout` after 5 minutes by default.

Failed calls return `*pcp.Error` with `Status`, `Code` (such as `trial_exhausted`, `insufficient_words`, `text_too_short`), `RetryAfter`, `Details` (detail fields such as `WordsNeeded` and `BillingURL`) and the raw `Body`.

`Account` has `Trial`, `Balances`, `AutoTopup` and `Key`; scans have `Sandbox`.

Run the tests with `go test ./...`.

## Error codes

Failed calls return `*plagiarismcheckerplus.Error`. It carries the HTTP status and the API error code (constants such as `CodeTrialExhausted`):

| Code | Status | Meaning |
|---|---|---|
| `invalid_request`, `invalid_json` | 400 | The request body or parameters are not valid. |
| `text_too_short` | 400 | Plagiarism needs at least 30 words, AI detection at least 50. Nothing is charged. |
| `doc_too_long` | 400 | The text is longer than the account allows per check. |
| `unauthorized`, `invalid_api_key`, `key_revoked` | 401 | Missing, unknown or revoked API key. |
| `trial_exhausted` | 402 | The free API trial (2,000 words) cannot cover this request. Includes `wordsNeeded`, `trialRemaining`, `trialTotal` and `billingUrl`. Buy words or choose an API plan; test keys stay free. Nothing is charged. |
| `insufficient_words` | 402 | Not enough words across all sources. Includes `wordsNeeded`, `wordsAvailable`, `balances` and `billingUrl`. Nothing is charged. |
| `key_cap_reached` | 402 | This key's monthly word cap would be exceeded. Includes `wordsNeeded`, `monthlyWordCap` and `usedThisMonth`. Raise the cap in the developer portal. |
| `email_unverified` | 403 | Verify the account's email address first. |
| `forbidden` | 403 | The key cannot access this resource. |
| `not_found` | 404 | No such scan for this key. |
| `rate_limited` | 429 | Too many requests. The client exposes `Retry-After` as `RetryAfter`. |
| `internal_error` | 500 | Something went wrong on our side. |
| `workers_unavailable` | 503 | Checks are temporarily unavailable. Retry later. |
| `plan_required` | 403 | **Deprecated, no longer returned.** Free accounts now get a free API trial and free test keys. `CodePlanRequired` is kept (marked deprecated) so existing code still compiles. |

`WaitForScan` returns `plagiarismcheckerplus.ErrScanTimeout` when it runs out of time. A scan whose status is `failed` is returned, not as an error, so check `Status`.

## Notes

- **An AI score is a likelihood, not proof** that someone used AI. A similarity score does not decide on its own whether text is plagiarism. Review the result yourself, and do not use either score alone to accuse anyone.
- Test-key scans carry `"sandbox": true` and contain sample data, not a real check.
- Each tool you run is billed at the full word count.
- Plagiarism Checker API overview: https://plagiarismcheckerplus.com/integrations/api
- AI Detector API overview: https://plagiarismcheckerplus.com/ai-detector-api
- API pricing: https://plagiarismcheckerplus.com/api-pricing
- API reference (try requests in the browser): https://app.plagiarismcheckerplus.com/api-docs
- Developer portal (keys, usage, billing, webhooks): https://app.plagiarismcheckerplus.com/developers
- The OpenAPI spec this client follows is in [`spec/openapi.json`](spec/openapi.json).

## Other languages

| Language | Repository | Install |
|---|---|---|
| JavaScript / TypeScript | [plagiarism-checker-plus-js](https://github.com/pcp-tech/plagiarism-checker-plus-js) | `npm install plagiarism-checker-plus` |
| Python | [plagiarism-checker-plus-python](https://github.com/pcp-tech/plagiarism-checker-plus-python) | `pip install plagiarism-checker-plus` |
| PHP | [plagiarism-checker-plus-php](https://github.com/pcp-tech/plagiarism-checker-plus-php) | `composer require plagiarismcheckerplus/sdk` |
| Ruby | [plagiarism-checker-plus-ruby](https://github.com/pcp-tech/plagiarism-checker-plus-ruby) | `gem install plagiarism_checker_plus` |
| **Go** (this repository) | [plagiarism-checker-plus-go](https://github.com/pcp-tech/plagiarism-checker-plus-go) | `go get github.com/pcp-tech/plagiarism-checker-plus-go` |
| Java | [plagiarism-checker-plus-java](https://github.com/pcp-tech/plagiarism-checker-plus-java) | Maven `com.plagiarismcheckerplus:sdk` |
| C# / .NET | [plagiarism-checker-plus-dotnet](https://github.com/pcp-tech/plagiarism-checker-plus-dotnet) | `dotnet add package PlagiarismCheckerPlus` |

## License

MIT. Copyright (c) 2026 Plagiarism Checker Plus.
