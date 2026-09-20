package project

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPGitHubVerifierMapsResponsesAndHeaders(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		want     error
		wantPath string
	}{
		{name: "public", status: http.StatusOK, body: `{"private":false}`},
		{name: "private", status: http.StatusOK, body: `{"private":true}`, want: ErrGitHubRepositoryInvalid},
		{name: "missing", status: http.StatusNotFound, body: `{}`, want: ErrGitHubRepositoryInvalid},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, want: ErrGitHubVerificationUnavailable},
		{name: "malformed", status: http.StatusOK, body: `{"private":false`, want: ErrGitHubVerificationUnavailable},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var gotPath string
			var gotAccept, gotAgent, gotVersion string
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				gotPath = request.URL.Path
				gotAccept = request.Header.Get("Accept")
				gotAgent = request.Header.Get("User-Agent")
				gotVersion = request.Header.Get("X-GitHub-Api-Version")
				response.WriteHeader(testCase.status)
				_, _ = response.Write([]byte(testCase.body))
			}))
			defer server.Close()

			verifier := NewGitHubVerifier(server.URL, server.Client())
			err := verifier.Verify(context.Background(), "https://github.com/acme/demo.git/")
			if testCase.want == nil {
				if err != nil {
					t.Fatalf("Verify() error = %v", err)
				}
			} else if !errors.Is(err, testCase.want) {
				t.Fatalf("Verify() error = %v, want %v", err, testCase.want)
			}
			if gotPath != "/repos/acme/demo.git" {
				t.Fatalf("request path = %q", gotPath)
			}
			if gotAccept != "application/vnd.github+json" || gotAgent != "MyWebsite" || gotVersion != "2022-11-28" {
				t.Fatalf("request headers = accept %q agent %q version %q", gotAccept, gotAgent, gotVersion)
			}
		})
	}
}

func TestParseGitHubRepositoryURLCanonicalization(t *testing.T) {
	canonical, owner, repo, err := ParseGitHubRepositoryURL(" https://GitHub.com/acme/demo.git/ ")
	if err != nil || canonical != "https://github.com/acme/demo.git" || owner != "acme" || repo != "demo.git" {
		t.Fatalf("ParseGitHubRepositoryURL() = %q %q %q %v", canonical, owner, repo, err)
	}
	for _, value := range []string{
		"http://github.com/acme/demo",
		"https://github.com/acme/demo?tab=readme",
		"https://github.com/acme/demo#readme",
		"https://user@github.com/acme/demo",
		"https://github.com:443/acme/demo",
		"https://github.com/acme/demo/extra",
		"https://github.com/acme/../demo",
	} {
		if _, _, _, err := ParseGitHubRepositoryURL(value); !errors.Is(err, ErrValidationFailed) {
			t.Errorf("ParseGitHubRepositoryURL(%q) error = %v, want validation_failed", value, err)
		}
	}
}

func TestHTTPGitHubVerifierRejectsInvalidURLWithoutRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	verifier := NewGitHubVerifier(server.URL, server.Client())
	if err := verifier.Verify(context.Background(), "https://github.com/acme/demo/extra"); !errors.Is(err, ErrGitHubRepositoryInvalid) {
		t.Fatalf("Verify() error = %v", err)
	}
	if called {
		t.Fatal("invalid URL made an HTTP request")
	}
	if strings.TrimSpace(server.URL) == "" {
		t.Fatal("httptest server URL unexpectedly empty")
	}
}
