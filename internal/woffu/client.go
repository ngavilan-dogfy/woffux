package woffu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	headers    map[string]string
}

func NewWoffuClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
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
	return &Client{
		baseURL:    strings.TrimRight(companyURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
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
		return fmt.Errorf("request %s %s returned %d: %s", opts.Method, url, resp.StatusCode, string(respBody))
	}

	if opts.Target != nil {
		if err := json.Unmarshal(respBody, opts.Target); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
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
