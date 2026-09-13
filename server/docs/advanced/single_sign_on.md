# Single sign-on

SnikketX can delegate sign-in to an external OIDC identity provider
(PocketID, Keycloak and similar) at two levels:

- **Portal sign-on**: users sign in to the web portal with the identity
  provider. On first login an XMPP account is provisioned automatically
  and the portal issues app bootstrap links instead of a password.
- **XMPP sign-on**: OAuth-capable clients such as Badinage authenticate
  with SASL OAUTHBEARER (XEP-0493), so no XMPP password exists at all.

Both can be used together or independently.

## Portal sign-on (OIDC)

Configure the provider details in `snikket.conf`:

```
SNIKKET_WEB_OIDC_ISSUER=https://id.example.com
SNIKKET_WEB_OIDC_CLIENT_ID=snikket-portal
SNIKKET_WEB_OIDC_CLIENT_SECRET=secret
SNIKKET_WEB_OIDC_REDIRECT_URL=https://chat.example.com/auth/oidc/callback
```

Optional claims tuning:

```
SNIKKET_WEB_OIDC_USERNAME_CLAIM=preferred_username
SNIKKET_WEB_OIDC_SCOPES="openid profile email"
```

Issuer and redirect URL must be https. The redirect URL must be
registered as an allowed callback at the provider.

### Account provisioning

On first sign-in the portal creates the XMPP account through a dedicated
service account with the `prosody:admin` role:

```
SNIKKET_WEB_SERVICE_ADDRESS=portal-service@DOMAIN
SNIKKET_WEB_SERVICE_PASSWORD=secret
```

Create it with `prosodyctl adduser` and grant the role with
`prosodyctl shell` (`roles.grant("portal-service@DOMAIN",
"prosody:admin")`) or the server admins list. Without a service
account, single sign-on sign-ins are refused because no account could
be provisioned or verified.

### Identity binding

Provisioned accounts are bound to the provider subject (`sub` claim) at
creation time. A later sign-in whose localpart matches an existing
account but whose subject differs is refused, so a password-created
account can never be claimed through OIDC. Operators who want to adopt
an existing account can bind it manually.

OIDC accounts carry no usable XMPP password. Apps are connected from the
"Connect an app" page via one-time bootstrap links. Portal-side password
and two-factor pages are hidden for these sessions.

## XMPP sign-on (SASL OAUTHBEARER)

Enable XEP-0493 sign-on for capable clients:

```
SNIKKET_TWEAK_OAUTH=1
SNIKKET_OAUTH_DISCOVERY_URL=https://id.example.com/.well-known/openid-configuration
SNIKKET_OAUTH_VALIDATION_ENDPOINT=https://id.example.com/api/oidc/userinfo
SNIKKET_OAUTH_USERNAME_FIELD=preferred_username
SNIKKET_OAUTH_SCOPE="openid profile"
```

This loads `mod_auth_snikket`, an auth provider that wraps the normal
password backend and additionally advertises `OAUTHBEARER`. Existing
password clients keep working unchanged. On a failed bearer token the
server relays `SNIKKET_OAUTH_DISCOVERY_URL` in the RFC 7628 error
document, which is how Badinage learns where to start the OAuth flow.

Requirements on the identity provider:

- An OIDC/OAuth discovery document at the configured URL.
- Dynamic client registration (RFC 7591), because Badinage registers
  itself on first use.
- A userinfo endpoint that returns the configured username field
  (`preferred_username` when the `profile` scope is granted).

When LDAP auth is enabled, the OAuth layer wraps the LDAP backend and
PLAIN stays enabled for the simple bind.

## Badinage web client

The bundled web client at `/chat` supports the OAuth flow out of the
box: it probes `OAUTHBEARER`, follows the discovery URL and completes
the authorization code flow with PKCE. With `SNIKKET_TWEAK_OAUTH=1` set,
users see a "Sign in with SSO" style flow and never need an XMPP
password.
