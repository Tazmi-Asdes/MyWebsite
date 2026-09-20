package project

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPGitHubVerifier checks repository visibility with the GitHub REST API.
// The HTTP client is injected so tests can use httptest.Server and production
// callers can configure a timeout without introducing a global client.
type HTTPGitHubVerifier struct {
	baseURL string
	client  *http.Client
}

// NewGitHubVerifier constructs a standard-library GitHub verifier. It accepts
// either (baseURL, client), (client, baseURL), or just (baseURL). The client
// is used as supplied; a bounded default is used when omitted.
func NewGitHubVerifier(first any, rest ...any) *HTTPGitHubVerifier {
	var baseURL string
	var client *http.Client
	consume := func(value any) {
		switch typed := value.(type) {
		case string:
			if baseURL == "" {
				baseURL = typed
			}
		case *url.URL:
			if typed != nil && baseURL == "" {
				baseURL = typed.String()
			}
		case *http.Client:
			if typed != nil && client == nil {
				client = typed
			}
		}
	}
	consume(first)
	for _, value := range rest {
		consume(value)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPGitHubVerifier{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// NewGitHubHTTPVerifier is an explicit constructor for callers that prefer to
// put the injected client first.
func NewGitHubHTTPVerifier(client *http.Client, baseURL string) *HTTPGitHubVerifier {
	return NewGitHubVerifier(baseURL, client)
}

// Verify implements GitHubVerifier.
func (verifier *HTTPGitHubVerifier) Verify(ctx context.Context, repositoryURL string) error {
	if verifier == nil || verifier.client == nil || verifier.baseURL == "" {
		return ErrGitHubVerificationUnavailable
	}
	_, owner, repo, err := ParseGitHubRepositoryURL(repositoryURL)
	if err != nil {
		return ErrGitHubRepositoryInvalid
	}
	endpoint := strings.TrimRight(verifier.baseURL, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrGitHubVerificationUnavailable
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "MyWebsite")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := verifier.client.Do(request)
	if err != nil {
		return ErrGitHubVerificationUnavailable
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusNotFound:
		return ErrGitHubRepositoryInvalid
	case response.StatusCode != http.StatusOK:
		return ErrGitHubVerificationUnavailable
	}

	var body struct {
		Private *bool `json:"private"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&body); err != nil || body.Private == nil {
		return ErrGitHubVerificationUnavailable
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrGitHubVerificationUnavailable
	}
	if *body.Private {
		return ErrGitHubRepositoryInvalid
	}
	return nil
}

// VerifyRepository is a readable alias used by service adapters.
func (verifier *HTTPGitHubVerifier) VerifyRepository(ctx context.Context, repositoryURL string) error {
	return verifier.Verify(ctx, repositoryURL)
}
