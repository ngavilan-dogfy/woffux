package woffu

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	headers    map[string]string
	retryWait  func(time.Duration)
}

// HTTPStatusError preserves the response status so callers can distinguish
// an expired/revoked token from a temporary Woffu outage without parsing an
// error string.
type HTTPStatusError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("request %s %s returned %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

func NewWoffuClient(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		retryWait:  time.Sleep,
		headers: map[string]string{
			"Accept":          "application/json, text/plain, */*",
			"Accept-Language": "es,es-ES;q=0.9",
			"Cache-Control":   "no-cache",
			"Pragma":          "no-cache",
			"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
			"Cookie":          `user-language="es"; woffu.lang=es`,
			"Origin":          baseURL,
			"Referer":         baseURL + "/v2/login",
		},
	}
}

func NewCompanyClient(companyURL string) *Client {
	companyURL = strings.TrimRight(companyURL, "/")
	return &Client{
		baseURL:    companyURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		retryWait:  time.Sleep,
		headers: map[string]string{
			"Accept":          "application/json, text/plain, */*",
			"Accept-Language": "es,es-ES;q=0.9",
			"Cache-Control":   "no-cache",
			"Pragma":          "no-cache",
			"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
			"Cookie":          `user-language="es"; woffu.lang=es`,
			"Referer":         companyURL + "/v2",
		},
	}
}

type requestOptions struct {
	Method      string
	Path        string
	Body        io.Reader
	ContentType string
	Headers     map[string]string
	Target      any
}

func (c *Client) doJSON(method, path string, body any, headers map[string]string, target any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	return c.do(requestOptions{
		Method:      method,
		Path:        path,
		Body:        bodyReader,
		ContentType: "application/json",
		Headers:     headers,
		Target:      target,
	})
}

func (c *Client) do(opts requestOptions) error {
	// Reads are safe to retry. Mutating requests, especially the sign toggle,
	// must remain single-shot because a lost response does not mean the action
	// was not applied by Woffu.
	attempts := 1
	if opts.Method == http.MethodGet && opts.Body == nil {
		attempts = 3
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = c.doOnce(opts)
		if err == nil || attempt == attempts || !isTransientRequestError(err) {
			return err
		}
		c.waitBeforeRetry(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return err
}

func (c *Client) doOnce(opts requestOptions) error {
	url := c.baseURL + opts.Path

	req, err := http.NewRequest(opts.Method, url, opts.Body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", opts.Method, url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPStatusError{
			Method:     opts.Method,
			URL:        url,
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	if opts.Target != nil {
		if err := json.Unmarshal(respBody, opts.Target); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}

func (c *Client) waitBeforeRetry(delay time.Duration) {
	if c.retryWait != nil {
		c.retryWait(delay)
		return
	}
	time.Sleep(delay)
}

func isTransientRequestError(err error) bool {
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode >= 500
	}

	var networkErr net.Error
	return errors.As(err, &networkErr)
}

// doRaw performs a request and returns the raw response (for cookie extraction).
func (c *Client) doRaw(opts requestOptions) (*http.Response, error) {
	url := c.baseURL + opts.Path

	req, err := http.NewRequest(opts.Method, url, opts.Body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}

	// Use a separate client that doesn't follow redirects
	noRedirectClient := &http.Client{
		Timeout: c.httpClient.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s %s: %w", opts.Method, url, err)
	}

	return resp, nil
}
