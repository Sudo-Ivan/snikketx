# web-portal

Snikket web portal package in the snikketx monorepo. Builds to
`ghcr.io/sudo-ivan/snikketx/web-portal`.

## Development

```
cd web-portal
cp example.env .env
pip install -r requirements.txt
pip install -r build-requirements.txt
make
quart run
```

Config keys are listed in `example.env`. Optional Python-based env init via
`SNIKKET_WEB_PYENV` (see `example.env.py`).
