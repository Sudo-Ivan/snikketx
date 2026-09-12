# ravenguard

RavenGuard edge configs. `ravenguard.toml` is the production config,
`ravenguard.dev.toml` is for local development.

Intentional differences in `ravenguard.dev.toml`:

- listen: HTTP only on :8080, no :80/:443 ACME listeners
- tls: `mode = "off"` instead of `acme` (no TLS-ALPN-01 locally)
- ratelimit / detect: disabled
- protect: looser limits (per-client 32, ban after 20 strikes, 1m ban TTL,
  write method cost 1)
- challenge: lower difficulty and `env_probe = "off"`, plus a dev-only secret
- blocklists / allowlists: slower reload interval (60s)
- ui: "SnikketX Dev" brand and `test_mode = true`
- logging: `level = "debug"`

Both files ship placeholder values. In production, override
`admin.bootstrap_user` and `challenge.secret`; never run the committed values.
