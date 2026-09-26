package woffu

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientRetriesTransientGET(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < 3 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	client := NewWoffuClient(server.URL)
	client.retryWait = func(time.Duration) {}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := client.doJSON(http.MethodGet, "/probe", nil, nil, &response); err != nil {
		t.Fatalf("GET after transient failures: %v", err)
	}
	if !response.OK {
		t.Fatal("expected decoded success response")
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}

func TestClientDoesNotRetryMutatingRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := NewCompanyClient(server.URL)
	client.retryWait = func(time.Duration) {}
	if err := client.doJSON(http.MethodPost, "/api/signs", map[string]bool{"sign": true}, nil, nil); err == nil {
		t.Fatal("expected POST to fail")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1; mutating requests must never be retried", got)
	}
}

func TestAuthenticateCachedKeepsTokenOnTransientProbeFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var probeRequests atomic.Int32
	var loginRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users":
			probeRequests.Add(1)
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		default:
			loginRequests.Add(1)
			http.Error(w, "login should not be called", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	email := "morning@example.com"
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix())))
	wantToken := "header." + payload + ".signature"
	saveCachedToken(email, server.URL, wantToken)
	t.Cleanup(func() { ClearCachedToken(email, server.URL) })

	client := NewWoffuClient(server.URL)
	companyClient := NewCompanyClient(server.URL)
	client.retryWait = func(time.Duration) {}
	companyClient.retryWait = func(time.Duration) {}
	gotToken, err := AuthenticateCached(client, companyClient, email, "password")
	if err != nil {
		t.Fatalf("AuthenticateCached: %v", err)
	}
	if gotToken != wantToken {
		t.Fatalf("token = %q, want cached token", gotToken)
	}
	if got := probeRequests.Load(); got != 3 {
		t.Fatalf("probe requests = %d, want 3", got)
	}
	if got := loginRequests.Load(); got != 0 {
		t.Fatalf("login requests = %d, want 0", got)
	}
}

func TestAuthenticateCachedRetriesTransientLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/svc/accounts/authorization/use-new-login":
			_, _ = w.Write([]byte(`{"useNewLogin":true,"companyId":1}`))
		case "/svc/accounts/companies/login-configuration-by-email":
			_, _ = w.Write([]byte(`{"woffuLogin":true}`))
		case "/svc/accounts/authorization/token":
			if tokenRequests.Add(1) == 1 {
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok"})
			w.WriteHeader(http.StatusFound)
		case "/api/svc/accounts/authorization/users/token":
			_, _ = w.Write([]byte(`{"token":"fresh-token"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewWoffuClient(server.URL)
	companyClient := NewCompanyClient(server.URL)
	client.retryWait = func(time.Duration) {}
	companyClient.retryWait = func(time.Duration) {}
	token, err := AuthenticateCached(client, companyClient, "new@example.com", "password")
	if err != nil {
		t.Fatalf("AuthenticateCached: %v", err)
	}
	if token != "fresh-token" {
		t.Fatalf("token = %q, want fresh-token", token)
	}
	if got := tokenRequests.Load(); got != 2 {
		t.Fatalf("token requests = %d, want 2", got)
	}
}

// fakeLogin serves the first three login steps; the company-token step is
// answered by companyToken.
func fakeLogin(t *testing.T, tokenRequests *atomic.Int32, companyToken http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/svc/accounts/authorization/use-new-login":
			_, _ = w.Write([]byte(`{"useNewLogin":true,"companyId":1}`))
		case "/svc/accounts/companies/login-configuration-by-email":
			_, _ = w.Write([]byte(`{"woffuLogin":true,"domain":"acme.woffu.com"}`))
		case "/svc/accounts/authorization/token":
			tokenRequests.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok"})
			w.WriteHeader(http.StatusFound)
		case "/api/svc/accounts/authorization/users/token":
			companyToken(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAuthenticateCachedDoesNotRepeatLoginOnUnknownErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var tokenRequests atomic.Int32
	server := fakeLogin(t, &tokenRequests, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected", http.StatusBadRequest)
	})

	client := NewWoffuClient(server.URL)
	companyClient := NewCompanyClient(server.URL)
	client.retryWait = func(time.Duration) {}
	companyClient.retryWait = func(time.Duration) {}
	_, err := AuthenticateCached(client, companyClient, "someone@example.com", "password")
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Kind != ErrUnknown {
		t.Fatalf("err = %v, want an ErrUnknown AuthError", err)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("password sent %d times, want 1: unknown errors must not repeat the login", got)
	}
}

// dnsFailure is a transport for which every host fails to resolve.
type dnsFailure struct{}

func (dnsFailure) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, &net.DNSError{Err: "no such host", Name: r.URL.Hostname(), IsNotFound: true}
}

func TestAuthenticateReportsACompanyThatDoesNotExist(t *testing.T) {
	var tokenRequests atomic.Int32
	server := fakeLogin(t, &tokenRequests, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the company host should not have been reached")
	})

	client := NewWoffuClient(server.URL)
	client.retryWait = func(time.Duration) {}
	companyClient := NewCompanyClient("https://typo.woffu.invalid")
	companyClient.retryWait = func(time.Duration) {}
	companyClient.httpClient = &http.Client{Transport: dnsFailure{}}

	_, err := Authenticate(client, companyClient, "someone@example.com", "password")
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Kind != ErrBadCompany {
		t.Fatalf("err = %v, want an ErrBadCompany AuthError", err)
	}
}
