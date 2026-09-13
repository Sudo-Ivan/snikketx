// Package oidc implements the relying-party side of OpenID Connect
// authentication for the portal: discovery, the authorization code flow with
// PKCE, ID token verification against the provider JWKS and derivation of an
// XMPP localpart from the identity claims.
package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// defaultTimeout bounds every call to the identity provider.
	defaultTimeout = 15 * time.Second
	// maxDocBody caps discovery, JWKS and token endpoint replies.
	maxDocBody = 1 << 20

	// discoveryTTL is how long the provider metadata stays cached.
	discoveryTTL = time.Hour
	// jwksTTL is how long a cached key stays trusted.
	jwksTTL = time.Hour
	// jwksCooldown is the minimum delay between JWKS refetches, so an
	// attacker presenting tokens with random key ids cannot turn the portal
	// into a fetch loop against the provider.
	jwksCooldown = 30 * time.Second

	// maxLocalpartLen bounds a derived XMPP localpart.
	maxLocalpartLen = 64
)

// Config carries the static OIDC client settings.
type Config struct {
	// Issuer is the provider issuer URL, e.g. https://id.example.com.
	Issuer string
	// ClientID and ClientSecret are the client credentials registered at
	// the provider.
	ClientID     string
	ClientSecret string
	// RedirectURL is the portal callback registered at the provider.
	RedirectURL string
	// UsernameClaim names the claim used for the XMPP localpart.
	UsernameClaim string
	// Scopes are the requested scopes.
	Scopes []string
}

// discovery is the subset of the provider metadata the flow uses.
type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// tokenReply is the wire form of a token endpoint response.
type tokenReply struct {
	AccessToken      string `json:"access_token"`
	IDToken          string `json:"id_token"`
	TokenType        string `json:"token_type"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// jwksDoc is the JSON form of a JSON Web Key set.
type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey is one JSON Web Key. Only the RSA and EC fields are used.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Crv string `json:"crv"`
}

// Provider is a configured OIDC relying party. It is safe for concurrent use.
type Provider struct {
	cfg  Config
	http *http.Client

	mu     sync.Mutex
	disc   *discovery
	discAt time.Time
	keys   map[string]crypto.PublicKey
	keysAt time.Time
}

// New returns a Provider for the given configuration. A nil httpClient uses a
// default client with TLS verification enabled and a sane timeout.
func New(cfg Config, httpClient *http.Client) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Provider{cfg: cfg, http: httpClient}
}

// wellKnownURL is the OIDC discovery document location of the issuer.
func (p *Provider) wellKnownURL() string {
	return strings.TrimRight(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
}

// discover returns the cached provider metadata, fetching it when stale.
func (p *Provider) discover(ctx context.Context) (*discovery, error) {
	p.mu.Lock()
	cached, at := p.disc, p.discAt
	p.mu.Unlock()
	if cached != nil && time.Since(at) < discoveryTTL {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.wellKnownURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetch discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery endpoint answered %s", resp.Status)
	}
	var doc discovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocBody)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("oidc: decode discovery document: %w", err)
	}

	issuer := strings.TrimRight(doc.Issuer, "/")
	if issuer == "" || issuer != strings.TrimRight(p.cfg.Issuer, "/") {
		return nil, errors.New("oidc: discovery issuer does not match the configured issuer")
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, errors.New("oidc: discovery document misses authorization or token endpoint")
	}
	doc.Issuer = issuer

	p.mu.Lock()
	p.disc, p.discAt = &doc, time.Now()
	p.mu.Unlock()
	return &doc, nil
}

// AuthorizationURL builds the authorize endpoint URL for a code flow with
// PKCE. state and nonce are generated by the caller and echoed back later.
func (p *Provider) AuthorizationURL(ctx context.Context, state, nonce, codeChallenge string) (string, error) {
	disc, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(disc.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("oidc: bad authorization endpoint: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Exchange trades an authorization code for tokens at the token endpoint.
// The client secret travels as HTTP Basic credentials.
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier string) (string, string, error) {
	disc, err := p.discover(ctx)
	if err != nil {
		return "", "", err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(p.cfg.ClientID, p.cfg.ClientSecret)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("oidc: token exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var reply tokenReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocBody)).Decode(&reply); err != nil {
		return "", "", fmt.Errorf("oidc: decode token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		detail := reply.Error
		if reply.ErrorDescription != "" {
			detail = strings.TrimSpace(detail + ": " + reply.ErrorDescription)
		}
		if detail == "" {
			detail = "status " + resp.Status
		}
		return "", "", fmt.Errorf("oidc: token endpoint refused the exchange: %s", detail)
	}
	return reply.AccessToken, reply.IDToken, nil
}

// Claims is the verified claim set of an identity.
type Claims struct {
	// Subject is the stable provider account id.
	Subject string
	// Raw carries every claim for flexible username mapping.
	Raw map[string]any
}

// VerifyIDToken validates the raw ID token: signature against the provider
// JWKS, issuer, audience, expiry, issued-at and the nonce minted at the start
// of the flow. It returns the claim set.
func (p *Provider) VerifyIDToken(ctx context.Context, rawIDToken, nonce string) (*Claims, error) {
	disc, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rawIDToken) == "" {
		return nil, errors.New("oidc: token response carried no id_token")
	}
	if disc.JWKSURI == "" {
		return nil, errors.New("oidc: provider publishes no JWKS endpoint")
	}

	parser := jwt.NewParser(
		// Only asymmetric signature methods are accepted; the symmetric
		// HS* family would verify against the client secret and is not
		// meaningful for JWKS based validation.
		jwt.WithValidMethods([]string{
			"RS256", "RS384", "RS512",
			"PS256", "PS384", "PS512",
			"ES256", "ES384", "ES512",
		}),
		jwt.WithIssuer(disc.Issuer),
		jwt.WithAudience(p.cfg.ClientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(time.Minute),
	)

	claims := jwt.MapClaims{}
	token, err := parser.ParseWithClaims(rawIDToken, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return p.keyFor(ctx, disc, kid)
	})
	if err != nil {
		return nil, fmt.Errorf("oidc: id token rejected: %w", err)
	}
	if token == nil || !token.Valid {
		return nil, errors.New("oidc: id token rejected")
	}

	gotNonce, _ := claims["nonce"].(string)
	if subtle.ConstantTimeCompare([]byte(gotNonce), []byte(nonce)) != 1 {
		return nil, errors.New("oidc: id token nonce mismatch")
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, errors.New("oidc: id token carries no subject")
	}
	return &Claims{Subject: sub, Raw: map[string]any(claims)}, nil
}

// Userinfo fetches the claim set through the userinfo endpoint. It is the
// fallback for providers that return no ID token; state and PKCE still bind
// the response to the browser flow.
func (p *Provider) Userinfo(ctx context.Context, accessToken string) (*Claims, error) {
	disc, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	if disc.UserinfoEndpoint == "" {
		return nil, errors.New("oidc: provider publishes no userinfo endpoint")
	}
	if accessToken == "" {
		return nil, errors.New("oidc: token response carried no access token")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, disc.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: userinfo request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: userinfo endpoint answered %s", resp.Status)
	}

	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocBody)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("oidc: decode userinfo: %w", err)
	}
	sub, _ := raw["sub"].(string)
	if sub == "" {
		return nil, errors.New("oidc: userinfo carries no subject")
	}
	return &Claims{Subject: sub, Raw: raw}, nil
}

// keyFor resolves the JWKS key for a token header kid, refetching the key set
// when the kid is unknown and the cache is old enough.
func (p *Provider) keyFor(ctx context.Context, disc *discovery, kid string) (crypto.PublicKey, error) {
	p.mu.Lock()
	cached, at := p.keys, p.keysAt
	p.mu.Unlock()

	if key, ok := cached[kid]; ok && time.Since(at) < jwksTTL {
		return key, nil
	}
	if time.Since(at) > jwksCooldown {
		if err := p.fetchJWKS(ctx, disc.JWKSURI); err != nil {
			return nil, err
		}
		p.mu.Lock()
		cached, at = p.keys, p.keysAt
		p.mu.Unlock()
	}
	key, ok := cached[kid]
	if !ok {
		return nil, errors.New("oidc: id token signed with an unknown key")
	}
	return key, nil
}

// fetchJWKS downloads the provider key set and caches every usable key.
func (p *Provider) fetchJWKS(ctx context.Context, jwksURI string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: fetch JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: JWKS endpoint answered %s", resp.Status)
	}

	var doc jwksDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocBody)).Decode(&doc); err != nil {
		return fmt.Errorf("oidc: decode JWKS: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kid == "" {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		key, err := k.publicKey()
		if err != nil {
			continue
		}
		keys[k.Kid] = key
	}

	p.mu.Lock()
	p.keys, p.keysAt = keys, time.Now()
	p.mu.Unlock()
	return nil
}

// publicKey converts a JWK into a crypto.PublicKey.
func (k jwkKey) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, err
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		if exponent <= 0 || exponent > 1<<31-1 {
			return nil, errors.New("oidc: bad RSA exponent")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}, nil
	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("oidc: unsupported EC curve %q", k.Crv)
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}
		y, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
	default:
		return nil, fmt.Errorf("oidc: unsupported key type %q", k.Kty)
	}
}

// Localpart derives the XMPP localpart from the verified claims. The
// configured claim wins; the email localpart is the fallback. Anything that
// does not reduce to a safe nodeprep style localpart is rejected.
func (p *Provider) Localpart(claims *Claims) (string, error) {
	raw, _ := claims.Raw[p.cfg.UsernameClaim].(string)
	if strings.TrimSpace(raw) == "" && p.cfg.UsernameClaim != "email" {
		raw, _ = claims.Raw["email"].(string)
	}
	return LocalpartFromClaim(raw)
}

// LocalpartFromClaim reduces a claim value to a safe XMPP localpart. A
// value carrying an "@" is cut at the first one so email style claims map to
// their localpart. The result must fit a conservative nodeprep charset.
func LocalpartFromClaim(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(value, '@'); i >= 0 {
		// The part after the @ is dropped, but only when it looks like a
		// domain; anything else marks the claim as unusable rather than
		// silently mapping it onto someone else's localpart.
		for j := i + 1; j < len(value); j++ {
			c := value[j]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
				return "", fmt.Errorf("oidc: username %q cannot be used as an XMPP localpart", value)
			}
		}
		value = value[:i]
	}
	if value == "" {
		return "", errors.New("oidc: identity carries no usable username claim")
	}
	if len(value) > maxLocalpartLen {
		return "", fmt.Errorf("oidc: username %q is too long for an XMPP localpart", value)
	}
	var prev byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return "", fmt.Errorf("oidc: username %q cannot be used as an XMPP localpart", value)
		}
		if c == '.' && prev == '.' {
			return "", fmt.Errorf("oidc: username %q cannot be used as an XMPP localpart", value)
		}
		prev = c
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") ||
		strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") ||
		strings.HasPrefix(value, "_") || strings.HasSuffix(value, "_") {
		return "", fmt.Errorf("oidc: username %q cannot be used as an XMPP localpart", value)
	}
	return value, nil
}

// PKCEVerifier returns a fresh high entropy code verifier.
func PKCEVerifier() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// PKCEChallenge returns the S256 code challenge of a verifier.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
