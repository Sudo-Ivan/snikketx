package prosody

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// xep227Stores is the set of account stores exported and imported through
// mod_xep227.
const xep227Stores = "roster,vcard,pep,pep_data"

// GetPublicInviteByID resolves an invitation token without authentication.
func (c *Client) GetPublicInviteByID(ctx context.Context, id string) (*PublicInviteInfo, error) {
	endpoint := c.publicEndpoint("/invite/" + url.PathEscape(id))

	var invite PublicInviteInfo
	if err := c.callJSON(ctx, http.MethodGet, endpoint, "", nil, &invite); err != nil {
		return nil, err
	}
	return &invite, nil
}

// RegisterWithToken creates an account from an invitation token and returns
// the JID of the new account.
func (c *Client) RegisterWithToken(ctx context.Context, inviteToken, username, password string) (string, error) {
	payload := map[string]any{
		"username": username,
		"password": password,
		"token":    inviteToken,
	}

	var reply struct {
		JID string `json:"jid"`
	}
	if err := c.callJSON(ctx, http.MethodPost, c.publicEndpoint("/register"), "", payload, &reply); err != nil {
		return "", err
	}
	return reply.JID, nil
}

// ExportAccountData returns the account data of the authenticated user as a
// XEP-0227 document. An empty string means the server had nothing to export.
func (c *Client) ExportAccountData(ctx context.Context, token string) (string, error) {
	endpoint := c.xep227Endpoint("/export?stores=" + url.QueryEscape(xep227Stores))

	req, err := c.newRequest(ctx, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer closeBody(resp)

	if err := errorFromResponse(resp); err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNoContent {
		return "", nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIQBody))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ImportAccountData loads a XEP-0227 document into the authenticated account.
func (c *Client) ImportAccountData(ctx context.Context, token, userXML string) error {
	endpoint := c.xep227Endpoint("/import?stores=" + url.QueryEscape(xep227Stores))

	req, err := c.newRequest(ctx, http.MethodPut, endpoint, token, strings.NewReader(userXML))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer closeBody(resp)

	return errorFromResponse(resp)
}
