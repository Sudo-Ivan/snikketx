package prosody

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

const (
	defaultTimeout = 30 * time.Second
	maxErrorBody   = 64 << 10
	maxIQBody      = 8 << 20
)

// Client talks to the HTTP APIs of a single Prosody virtual host. It is safe
// for concurrent use. Authenticated calls take the caller's bearer token, an
// empty token issues the request unauthenticated.
type Client struct {
	// Endpoint is the base URL of the Prosody HTTP interface without a
	// trailing slash.
	Endpoint string
	// Domain is the XMPP virtual host. It is forced as the Host header on
	// every request so Prosody routes to the right virtual host.
	Domain string
	// Version is reported as the software version during OAuth 2.0 client
	// registration.
	Version string
	// HTTP is the transport used for every request.
	HTTP *http.Client

	// registerMu serialises dynamic client registration so concurrent
	// logins share one set of credentials.
	registerMu sync.Mutex

	mu              sync.Mutex
	clientID        string
	clientSecret    string
	credentialsPath string
}

// New returns a Client for the given Prosody endpoint and virtual host.
func New(endpoint, domain, version string) *Client {
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Domain:   domain,
		Version:  version,
		HTTP:     &http.Client{Timeout: defaultTimeout},
	}
}

func (c *Client) loginEndpoint() string {
	return c.Endpoint + "/oauth2/token"
}

func (c *Client) revokeEndpoint() string {
	return c.Endpoint + "/oauth2/revoke"
}

func (c *Client) registerClientEndpoint() string {
	return c.Endpoint + "/oauth2/register"
}

func (c *Client) restEndpoint() string {
	return c.Endpoint + "/rest"
}

// adminEndpoint builds a mod_admin_api URL, percent encoding every path
// segment so URL unsafe characters inside a segment cannot escape it.
func (c *Client) adminEndpoint(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = url.PathEscape(segment)
	}
	return c.Endpoint + "/admin_api/" + strings.Join(escaped, "/")
}

func (c *Client) publicEndpoint(subpath string) string {
	return c.Endpoint + "/register_api" + subpath
}

func (c *Client) xep227Endpoint(subpath string) string {
	return c.Endpoint + "/xep227" + subpath
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// newRequest builds a request with the virtual host forced into the Host
// header and the bearer token applied when one was supplied.
func (c *Client) newRequest(ctx context.Context, method, endpoint, token string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Host = c.Domain
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// requestJSON sends payload as a JSON body and returns the raw response. A nil
// payload sends no body at all. The caller owns the response body.
func (c *Client) requestJSON(ctx context.Context, method, endpoint, token string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := c.newRequest(ctx, method, endpoint, token, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient().Do(req)
}

// callJSON sends payload as JSON and decodes a successful response into out.
// A nil out discards the response body.
func (c *Client) callJSON(ctx context.Context, method, endpoint, token string, payload, out any) error {
	resp, err := c.requestJSON(ctx, method, endpoint, token, payload)
	if err != nil {
		return err
	}
	defer closeBody(resp)

	if err := errorFromResponse(resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// errorFromResponse turns a non success response into an APIError when the
// body carries a structured error envelope, and into an HTTPError otherwise.
// It consumes the body only when the response failed.
func errorFromResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))

	var envelope struct {
		Error *APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error != nil {
		if envelope.Error.Condition != "" || envelope.Error.Type != "" {
			return envelope.Error
		}
	}

	return &HTTPError{
		Status:  resp.StatusCode,
		Message: resp.Status,
		Body:    string(body),
	}
}

// decodeJSONBody decodes a response body that has already been checked for
// errors.
func decodeJSONBody(resp *http.Response, out any) error {
	return json.NewDecoder(resp.Body).Decode(out)
}

// closeBody drains and closes a response body so the connection can be reused.
func closeBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()
}

// restIQRequest is the JSON form of an IQ stanza accepted by mod_rest.
type restIQRequest struct {
	Kind    string          `json:"kind"`
	Type    string          `json:"type"`
	To      string          `json:"to"`
	Ping    bool            `json:"ping,omitempty"`
	Version json.RawMessage `json:"version,omitempty"`
}

// jsonIQCall posts a JSON encoded IQ to mod_rest and returns the raw response
// together with its status. The response body is fully read and closed.
func (c *Client) jsonIQCall(ctx context.Context, token string, req restIQRequest) (int, []byte, error) {
	resp, err := c.requestJSON(ctx, http.MethodPost, c.restEndpoint(), token, req)
	if err != nil {
		return 0, nil, err
	}
	defer closeBody(resp)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIQBody))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// xmlIQCall posts a serialised IQ stanza to mod_rest and returns the inner XML
// of a result reply. Error replies are returned as an xmpp.IQError.
func (c *Client) xmlIQCall(ctx context.Context, token, payload string) ([]byte, error) {
	req, err := c.newRequest(ctx, http.MethodPost, c.restEndpoint(), token, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xmpp+xml")
	req.Header.Set("Accept", "application/xmpp+xml")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &HTTPError{
			Status:  resp.StatusCode,
			Message: resp.Status,
			Body:    string(body),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIQBody))
	if err != nil {
		return nil, err
	}
	return xmpp.ExtractIQReply(body)
}

// isNotFound reports whether err is a missing item, either an IQ error with an
// item-not-found condition or an HTTP 404.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if iqErr, ok := errors.AsType[*xmpp.IQError](err); ok {
		return iqErr.Status == http.StatusNotFound
	}
	if httpErr, ok := errors.AsType[*HTTPError](err); ok {
		return httpErr.Status == http.StatusNotFound
	}
	return false
}
