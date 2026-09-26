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
	ErrBadEmail        AuthErrorKind = iota // Email not found in Woffu
	ErrBadPassword                          // Wrong password
	ErrBadCompany                           // Company subdomain not found
	ErrNetwork                              // Network/connectivity issue
	ErrUnknown                              // Something else
	ErrNoPasswordLogin                      // The company turned Woffu passwords off (single sign-on only)
)

func (e *AuthError) Error() string {
	return e.Detail
}

func (e *AuthError) Unwrap() error {
	return e.Wrapped
}

// Account is what Woffu says about an email before anyone signs in.
type Account struct {
	// CompanyURL is the company's own Woffu address (https://acme.woffu.com),
	// or "" when Woffu didn't say.
	CompanyURL string
	// PasswordLogin is false when the company turned Woffu passwords off
	// and signs in only through single sign-on.
	PasswordLogin bool
	// SSO is true when the company also (or only) signs in through a
	// single sign-on provider, named in SSOProvider when Woffu names it.
	SSO         bool
	SSOProvider string
}

// LookupAccount asks Woffu whether an email has an account and how its
// company signs in: the same two questions Woffu's login page asks while you
// type your email. No password is sent.
func LookupAccount(client *Client, email string) (*Account, error) {
	// Step 1: Check if email exists
	var newLogin woffuNewLogin
	err := client.doJSON("GET", "/svc/accounts/authorization/use-new-login?email="+url.QueryEscape(email), nil, nil, &newLogin)
	if err != nil {
		return nil, classifyLookupError(err, email)
	}

	// Step 2: Get login configuration
	var loginConfig woffuLoginConfiguration
	err = client.doJSON("GET", "/svc/accounts/companies/login-configuration-by-email?email="+url.QueryEscape(email), nil, nil, &loginConfig)
	if err != nil {
		return nil, classifyLookupError(err, email)
	}
	provider := ""
	if loginConfig.ProviderName != nil {
		provider = strings.TrimSpace(*loginConfig.ProviderName)
	}
	return &Account{
		CompanyURL: companyURLFromDomain(loginConfig.Domain),
		// Only an explicit "false" turns passwords off: a field Woffu stops
		// sending must not make a wrong password look like a policy.
		PasswordLogin: loginConfig.WoffuLogin == nil || *loginConfig.WoffuLogin,
		SSO:           loginConfig.OpenIDLogin || provider != "",
		SSOProvider:   provider,
	}, nil
}

func classifyLookupError(err error, email string) error {
	if hasHTTPStatus(err, http.StatusBadRequest, http.StatusNotFound) || strings.Contains(err.Error(), "UserNotFound") {
		return &AuthError{Kind: ErrBadEmail, Detail: fmt.Sprintf("email \"%s\" not found in Woffu", email), Wrapped: err}
	}
	if isTransientRequestError(err) {
		return &AuthError{Kind: ErrNetwork, Detail: "cannot connect to Woffu", Wrapped: err}
	}
	return &AuthError{Kind: ErrUnknown, Detail: err.Error(), Wrapped: err}
}

// companyURLFromDomain turns the domain Woffu reports ("acme.woffu.com",
// or just "acme") into the company's address. Anything that isn't a Woffu
// host is ignored: the session cookie is sent there next.
func companyURLFromDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	d = strings.TrimSuffix(d, "/")
	if d == "" || strings.ContainsAny(d, "/@: ") {
		return ""
	}
	if !strings.Contains(d, ".") {
		d += ".woffu.com"
	}
	if !strings.HasSuffix(d, ".woffu.com") {
		return ""
	}
	return "https://" + d
}

// Authenticate performs the full Woffu login flow and returns a bearer token.
// Returns typed AuthError for proper error classification.
func Authenticate(client *Client, companyClient *Client, email, password string) (string, error) {
	account, err := LookupAccount(client, email)
	if err != nil {
		return "", err
	}
	return SignIn(client, companyClient, account, email, password)
}

// SignIn sends the password and returns a bearer token for the company.
// account comes from LookupAccount.
func SignIn(client *Client, companyClient *Client, account *Account, email, password string) (string, error) {
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
		wrapped := fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
		if account != nil && !account.PasswordLogin {
			return "", &AuthError{Kind: ErrNoPasswordLogin, Detail: "your company signs in to Woffu through single sign-on only", Wrapped: wrapped}
		}
		return "", &AuthError{Kind: ErrBadPassword, Detail: "wrong password", Wrapped: wrapped}
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
