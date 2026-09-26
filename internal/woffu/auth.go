package woffu

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Auth error types for classification
type AuthError struct {
	Kind    AuthErrorKind
	Detail  string
	Wrapped error
}

type AuthErrorKind int

const (
	ErrBadEmail    AuthErrorKind = iota // Email not found in Woffu
	ErrBadPassword                      // Wrong password
	ErrBadCompany                       // Company subdomain not found
	ErrNetwork                          // Network/connectivity issue
	ErrUnknown                          // Something else
)

func (e *AuthError) Error() string {
	return e.Detail
}

func (e *AuthError) Unwrap() error {
	return e.Wrapped
}

// Authenticate performs the full Woffu login flow and returns a bearer token.
// Returns typed AuthError for proper error classification.
func Authenticate(client *Client, companyClient *Client, email, password string) (string, error) {
	// Step 1: Check if email exists
	var newLogin woffuNewLogin
	err := client.doJSON("GET", "/svc/accounts/authorization/use-new-login?email="+url.QueryEscape(email), nil, nil, &newLogin)
	if err != nil {
		if hasHTTPStatus(err, http.StatusBadRequest, http.StatusNotFound) || strings.Contains(err.Error(), "UserNotFound") {
			return "", &AuthError{Kind: ErrBadEmail, Detail: fmt.Sprintf("email \"%s\" not found in Woffu", email), Wrapped: err}
		}
		if isTransientRequestError(err) {
			return "", &AuthError{Kind: ErrNetwork, Detail: "cannot connect to Woffu", Wrapped: err}
		}
		return "", &AuthError{Kind: ErrUnknown, Detail: err.Error(), Wrapped: err}
	}

	// Step 2: Get login configuration
	var loginConfig woffuLoginConfiguration
	err = client.doJSON("GET", "/svc/accounts/companies/login-configuration-by-email?email="+url.QueryEscape(email), nil, nil, &loginConfig)
	if err != nil {
		if hasHTTPStatus(err, http.StatusBadRequest, http.StatusNotFound) || strings.Contains(err.Error(), "UserNotFound") {
			return "", &AuthError{Kind: ErrBadEmail, Detail: fmt.Sprintf("email \"%s\" not found in Woffu", email), Wrapped: err}
		}
		if isTransientRequestError(err) {
			return "", &AuthError{Kind: ErrNetwork, Detail: "cannot connect to Woffu", Wrapped: err}
		}
		return "", &AuthError{Kind: ErrUnknown, Detail: err.Error(), Wrapped: err}
	}

	// Step 3: Get token with credentials
	formBody := fmt.Sprintf("grant_type=password&username=%s&password=%s", url.QueryEscape(email), url.QueryEscape(password))
	resp, err := client.doRaw(requestOptions{
		Method:      "POST",
		Path:        "/svc/accounts/authorization/token",
		Body:        strings.NewReader(formBody),
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		return "", &AuthError{Kind: ErrNetwork, Detail: "cannot connect to Woffu", Wrapped: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 400 || resp.StatusCode == 401 {
		// Steps 1-2 passed, so the email is valid. Any error here is a password issue.
		return "", &AuthError{Kind: ErrBadPassword, Detail: "wrong password", Wrapped: fmt.Errorf("status %d: %s", resp.StatusCode, string(body))}
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", &AuthError{
			Kind:    ErrNetwork,
			Detail:  "cannot connect to Woffu",
			Wrapped: fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body)),
		}
	}

	// Extract cookies
	var cookies []string
	for _, c := range resp.Cookies() {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	cookieHeader := fmt.Sprintf(`user-language="es"; woffu.lang=es; %s`, strings.Join(cookies, "; "))

	// Step 4: Get company-scoped token
	var tokenResp woffuGetToken
	err = companyClient.doJSON("GET", "/api/svc/accounts/authorization/users/token", nil, map[string]string{
		"Cookie": cookieHeader,
	}, &tokenResp)
	if err != nil {
		// Steps 1-3 reached Woffu, so the network works: an address that
		// doesn't resolve is a company subdomain that doesn't exist.
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return "", &AuthError{Kind: ErrBadCompany, Detail: "company domain not found", Wrapped: err}
		}
		if isTransientRequestError(err) {
			return "", &AuthError{Kind: ErrNetwork, Detail: "cannot connect to Woffu", Wrapped: err}
		}
		if hasHTTPStatus(err, http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden) {
			return "", &AuthError{Kind: ErrBadCompany, Detail: "company not accessible", Wrapped: err}
		}
		return "", &AuthError{Kind: ErrUnknown, Detail: err.Error(), Wrapped: err}
	}

	return tokenResp.Token, nil
}

func hasHTTPStatus(err error, statuses ...int) bool {
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	for _, status := range statuses {
		if statusErr.StatusCode == status {
			return true
		}
	}
	return false
}
