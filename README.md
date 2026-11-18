# Plagiarism Checker Plus for Go

Official client for the [Plagiarism Checker Plus API](https://plagiarismcheckerplus.com/integrations/api). Go 1.21+, standard library only.

The API needs a paid plan. A key on the free plan returns `403 plan_required`.

```sh
go get github.com/pcp-tech/plagiarism-checker-plus-go
```

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

	created, err := client.CreateScan(ctx, essayText, []string{pcp.ToolPlagiarism, pcp.ToolAIDetect}, "essay-draft-1")
	var apiErr *pcp.Error
	if errors.As(err, &apiErr) && apiErr.Code == pcp.CodeTextTooShort {
		log.Fatal("Send at least 30 words for plagiarism, 50 for AI detection.")
	} else if err != nil {
		log.Fatal(err)
	}

	scan, err := client.WaitForScan(ctx, created.ID, pcp.WaitOptions{})
	if err != nil {
		log.Fatal(err)
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

## Methods

| Method | API call |
|---|---|
| `CreateScan(ctx, text, tools, name)` | `POST /scans` |
| `GetScan(ctx, id, includeText)` | `GET /scans/{id}` |
| `ListScans(ctx, page, limit)` | `GET /scans` |
| `GetAccount(ctx)` | `GET /account` |
| `WaitForScan(ctx, id, WaitOptions{})` | Polls until `completed` or `failed` |

`WaitForScan` starts at 2 seconds, grows by 1.5× up to 10 seconds, waits for `Retry-After` on `429`, and returns `ErrScanTimeout` after 5 minutes by default.

Failed calls return `*pcp.Error` with `Status`, `Code` (such as `plan_required`, `text_too_short`, `doc_too_long`, `insufficient_words`) and `RetryAfter`.

Run the tests with `go test ./...`.

## Error handling

Failed calls raise `*plagiarismcheckerplus.Error`. It carries the HTTP status and the API error code:

| Code | Status | Meaning |
|---|---|---|
| `plan_required` | 403 | The key's plan has no API access. The API needs a paid plan. |
| `text_too_short` | 400 | Plagiarism needs at least 30 words, AI detection at least 50. Nothing is charged. |
| `doc_too_long` | 400 | The text is longer than the plan allows per check. |
| `insufficient_words` | 402 | Not enough words left on the account. |

Other codes (for example on `401`, `404`, `429` or `503`) come through as the raw string the API sends. On `429` the client honours `Retry-After`. `WaitForScan` raises `plagiarismcheckerplus.ErrScanTimeout` when it runs out of time. A scan whose status is `failed` is returned, not raised, so check `status`.

## Notes

- **An AI score is a likelihood, not proof** that someone used AI. A similarity score does not decide on its own whether text is plagiarism. Review the result yourself, and do not use either score alone to accuse anyone.
- **The API needs a paid plan.** A key on the free plan returns `403 plan_required`.
- Each tool you run is billed at the full word count. The rate limit is 60 requests a minute per key.
- Plagiarism Checker API overview: https://plagiarismcheckerplus.com/integrations/api
- AI Detector API overview: https://plagiarismcheckerplus.com/ai-detector-api
- API reference (try requests in the browser): https://app.plagiarismcheckerplus.com/api-docs
- Create an API key: https://app.plagiarismcheckerplus.com/settings/api
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
