---
title: Reverse proxies
subtitle: Running SnikketX behind a reverse proxy
---

{{< lead >}}
The default SnikketX setup assumes that there is no other HTTP/HTTPS server
running. If you already have another web server running for example, you will
need to instruct it to forward SnikketX traffic to SnikketX. This page provides
guides and example configuration to help you with this.
{{< /lead >}}

{{< panel style="warning" >}}
**A quick note about non-HTTP services:** SnikketX includes a number of non-HTTP
services which cannot be routed through a HTTP reverse proxy. This includes
XMPP, STUN and TURN. The documentation here applies to redirecting the HTTP
and HTTPS ports (80 and 443) through a reverse proxy only.
{{< /panel >}}

# Certificates

It is important to get certificates correct when deploying SnikketX behind a reverse
proxy. SnikketX needs to obtain certificates from Let's Encrypt in order to secure
the non-HTTP services it provides. Be careful that your reverse proxy does not
intercept requests from Let's Encrypt that are intended for the SnikketX service.

The reverse proxy will generally need its own certificates, which can be obtained
in the usual manner using certbot or another ACME client on your host system (basically,
however you normally would obtain certificates for a website/service on your setup).

# Configuration

## SnikketX

First we need to tell SnikketX to use alternative ports, so that it doesn't conflict
with the primary web server/proxy that will be forwarding the traffic. This can be
done by adding the following lines to /etc/snikket/snikket.conf:

```
SNIKKET_TWEAK_HTTP_PORT=5080
SNIKKET_TWEAK_HTTPS_PORT=5443
```

You can choose any alternative ports that you would prefer, but the rest of this
documentation will assume you use the ports given in this example.

In the next step, you need to configure the web server to forward traffic to
SnikketX on these ports. Follow the section below according to which web server
you are using.

## Web servers

Each web server is different, so here we provide some example configuration snippets
for the most common servers. Feel free to contribute any that you would like to see
included!

### Nginx

```
server {
  # Accept HTTP connections
  listen 80;
  listen [::]:80;

  server_name chat.example.com;
  server_name groups.chat.example.com;
  server_name share.chat.example.com;

  location / {
      proxy_pass http://localhost:5080/;
      proxy_set_header      Host              $host;
      proxy_set_header      X-Forwarded-For   $proxy_add_x_forwarded_for;

      # This is the maximum size of uploaded files in SnikketX
      client_max_body_size 104857616; # 100MB + 16 bytes
  }
}

server {
  # Accept HTTPS connections
  listen [::]:443 ssl ipv6only=on;
  listen 443 ssl;
  ssl_certificate /path/to/certificate.pem;
  ssl_certificate_key /path/to/key.pem;

  server_name chat.example.com;
  server_name groups.chat.example.com;
  server_name share.chat.example.com;

  location / {
      proxy_pass https://localhost:5443/;
      proxy_set_header      Host              $host;
      proxy_set_header      X-Forwarded-For   $proxy_add_x_forwarded_for;
      # REMOVE THIS IF YOU CHANGE `localhost` TO ANYTHING ELSE ABOVE
      proxy_ssl_verify      off;
      proxy_set_header      X-Forwarded-Proto https;
      proxy_ssl_server_name on;

      # This is the maximum size of uploaded files in SnikketX
      client_max_body_size 104857616; # 100MB + 16 bytes

      # For BOSH and WebSockets
      proxy_set_header Connection $http_connection;
      proxy_set_header Upgrade $http_upgrade;
      proxy_read_timeout 900s;

  }
}
```

**Note:** You may modify the first server block to include a redirect to HTTPS
instead of proxying plain-text HTTP traffic. When doing that, take care to
proxy `.well-known/acme-challenge` even in plain text to allow SnikketX to
obtain certificates.

### apache2

**Note**: enable the needed apache2 mods, the parameter upgrade=websocket and timeout is only needed if you want to enable connections from webclients like [converse.js](https://conversejs.org) :
`a2enmod ssl proxy proxy_http`


```
<VirtualHost *:80>

        ServerName  chat.example.com
        ServerAlias groups.chat.example.com
        ServerAlias share.chat.example.com

        ProxyPreserveHost On

        ProxyPass           / http://127.0.0.1:5080/
        ProxyPassReverse    / http://127.0.0.1:5080/

</VirtualHost>

<VirtualHost *:443>

        ServerName  chat.example.com
        ServerAlias groups.chat.example.com
        ServerAlias share.chat.example.com

        SSLEngine On
        SSLProxyEngine On
        ProxyPreserveHost On

        SSLCertificateFile /path/to/certificatefolder/cert.pem
        SSLCertificateKeyFile /path/to/certificatefolder/privkey.pem
        SSLCertificateChainFile /path/to/certificatefolder/chain.pem

        ProxyPass           / https://127.0.0.1:5443/ upgrade=websocket timeout=900
        ProxyPassReverse    / https://127.0.0.1:5443/

</VirtualHost>
```

### Caddy
#### Basic
For a simple configuration that only proxies the SnikketX web portal, the following Caddyfile can be used.
```
http://chat.example.com,
http://groups.chat.example.com,
http://share.chat.example.com {
    reverse_proxy localhost:5080 {
        header_up Host {host}
    }
}

chat.example.com,
groups.chat.example.com,
share.chat.example.com {
    reverse_proxy https://localhost:5443 {
        header_up Host {host}
        transport http {
            tls_insecure_skip_verify
        }
    }
}
```

### Nginx Proxy Manager

Unfortunately setup with Nginx Proxy Manager (NPM) is rather difficult due to
a bug in NPM that causes it to block some requests that SnikketX needs to
function. Specifically, NPM does not currently forward the `/.well-known`
directory, which is needed for some features of SnikketX to work correctly,
including certificate management and web client discovery.

**Bug report:** https://github.com/NginxProxyManager/nginx-proxy-manager/issues/210

If SnikketX and NPM are running on the same machine, you could try [this
workaround](https://github.com/NginxProxyManager/nginx-proxy-manager/issues/210#issuecomment-1068955629).

Another option is to export the certificates that NPM obtains, and import them
into SnikketX (at `/snikket/letsencrypt/live/` inside the SnikketX containers).
You would need to do this regularly (i.e. automate it), as certificates need
to be updated every few weeks. You should disable SnikketX's cert manager
container if you do this.

Either of these workarounds will allow SnikketX to start, but you may still
have some problems, e.g. connecting from some web clients may not work, due to
NPM also blocking other things located in `/.well-known`. Unfortunately there
is not much we can do about this, as this is a limitation of NPM's current
design.

## Other setups

### Generic instructions

This page includes sample configuration for various popular reverse proxy
software already. However if yours is not listed, or you need to better understand
SnikketX's requirements, this section will help you understand how your proxy
needs to be configured.

A valid reverse proxy in front of SnikketX should do the following:

- Listen on port 80, and forward requests to the 3 domains to SnikketX's HTTP
  port (the one you configured using `SNIKKET_TWEAK_HTTP_PORT`) (SnikketX
  will handle redirecting HTTP to HTTPS when necessary)
- Listen on port 443, and forward requests to the 3 SnikketX domains to SnikketX's
  HTTPS port (the one you configured using `SNIKKET_TWEAK_HTTPS_PORT`).
- You may need to disable certificate verification of the 'upstream' server
  (SnikketX) in your reverse proxy, unless you can tell it to verify against the
  real hostname instead of e.g. 'localhost'.
- HTTP headers:
  - You must ensure that the original 'Host' header is preserved (e.g.
    'chat.example.com', not 'localhost')
  - Relay the original client's IP address in the `X-Forwarded-For` header
  - For HTTPS requests, include an `X-Forwarded-Proto: https` header
- If your proxy enforces any limits on the HTTP request body size, ensure it
  is at least 104857616 bytes (this is 100MB + 16 bytes).

### sslh

sslh is a little different to the other servers listed here, as it is not a web server. However it is able
to route encrypted traffic (such as HTTPS and even some kinds of XMPP traffic) to different places.

The snippet below lists the rules required to forward all of SnikketX's traffic to SnikketX. Don't forget that
SnikketX will also need port 80 forwarded to 5080 somehow (otherwise it won't be able to obtain certificates).

Unlike the other solutions here, this approach also allows you to run encrypted XMPP through the HTTPS port.
To take full advantage of this feature, you will need to add additional DNS records. See [advanced DNS](dns.md)
for more information.

This configuration requires sslh 1.18 or higher.

```
listen:
(
    { host: "0.0.0.0"; port: "443"; },
);

protocols:
(
     ## SnikketX rules
     # Send encrypted XMPP traffic directly to SnikketX (this must be above the HTTPS rules)
     { name: "tls";     host: "127.0.0.1"; port: "5223"; alpn_protocols: [ "xmpp-client" ]; },
     # Send HTTPS traffic to SnikketX's HTTPS port
     { name: "tls";     host: "127.0.0.1"; port: "5443"; sni_hostnames:  [ "chat.example.com", "groups.chat.example.com", "share.chat.example.com" ] },
     # Send unencrypted XMPP traffic to SnikketX (will use STARTTLS)
     { name: "xmpp";    host: "127.0.0.1"; port: "5222"; },

     ## Other rules
     # Add rules here to forward any other hosts/protocols to non-SnikketX destinations
);

```

### Advanced apache2 setup

{{< panel style="note" >}}
The following configuration is for reverse proxying from another machine (other from the one hosting SnikketX containers). If SnikketX is running on the same machine as the reverse proxy, use the [basic configuration](#apache2) instead.
{{< /panel >}}

A prerequisite is a mechanism to sync SnikketX-managed letsencrypt TLS key and cert to `/opt/chat/letsencrypt`. This is required because Apache 2.4 is not able to revproxying based on SNI, routing encrypted TLS directly to the SnikketX machine.
	
```
        <VirtualHost *:443>

                ServerName chat.example.com
                ServerAlias groups.chat.example.com
                ServerAlias share.chat.example.com

                ServerAdmin webmaster@localhost

                DocumentRoot /var/www/chat

                ErrorLog ${APACHE_LOG_DIR}/chat.example.com-ssl_error.log
                CustomLog ${APACHE_LOG_DIR}/chat.example.com-ssl_access.log combined

                SSLEngine on

                SSLCertificateFile /opt/chat/letsencrypt/chat.example.com/cert.pem
                SSLCertificateKeyFile /opt/chat/letsencrypt/chat.example.com/privkey.pem
                SSLCertificateChainFile /opt/chat/letsencrypt/chat.example.com/chain.pem

                SSLProxyEngine On
                ProxyPreserveHost On

                ProxyPass           / https://chat.example.com/
                ProxyPassReverse    / https://chat.example.com/

        </VirtualHost>

        <VirtualHost *:80>

                ServerName chat.example.com
                ServerAlias groups.chat.example.com
                ServerAlias share.chat.example.com

                ServerAdmin webmaster@localhost
                DocumentRoot /var/www/chat

                ProxyPreserveHost On

                ProxyPass           / http://chat.example.com/
                ProxyPassReverse    / http://chat.example.com/

                ErrorLog ${APACHE_LOG_DIR}/chat.example.com_error.log
                CustomLog ${APACHE_LOG_DIR}/chat.example.com_access.log combined

        </VirtualHost>

```

### Advanced Caddy setup

This advanced configuration allows for Caddy to be used as a "multiplexer", that is,
serving HTTPS and encrypted XMPP traffic through the same port. This can be used to get
around some very restrictive firewalls, similar to [`sslh`](#sslh). The configuration
also forwards port 80 to 5080 (which `sslh` cannot do). However, since Caddy, by design,
is a layer 7 (HTTP) proxy, an additional layer 4 plugin is needed.

{{< panel style="note" >}}
If you only need a simple Caddy setup so SnikketX can share HTTP/HTTPS ports with
other services, see the [basic Caddy configuration](#caddy) instead.
{{< /panel >}}

Download [xcaddy](https://github.com/caddyserver/xcaddy) and build Caddy with the [layer4](https://github.com/mholt/caddy-l4) plugin. Also include the [YAML plugin](https://github.com/abiosoft/caddy-yaml), since the layer4 plugin does not support Caddyfile ([yet](https://github.com/mholt/caddy-l4/issues/16)).
```bash
xcaddy build \
  --with github.com/mholt/caddy-l4 \
  --with github.com/abiosoft/caddy-yaml
```
Run Caddy with

```bash
caddy run --config config.yaml --adapter yaml
```

Alternatively, if you use Caddy with Docker, use the following Dockerfile. Make sure that the folder containing `config.yaml` is mounted as `/etc/caddy` inside the container.

```dockerfile
FROM caddy:builder AS builder

RUN xcaddy build \
    --with github.com/mholt/caddy-l4 \
    --with github.com/abiosoft/caddy-yaml

FROM caddy:latest

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

CMD ["caddy", "run", "--config", "/etc/caddy/config.yaml", "--adapter", "yaml"]
```

The `config.yaml` needs to
1. Forward HTTP traffic (on port 80) with SnikketX hostnames to port 5080, and redirect other HTTP traffic to HTTPS. This is done by `srv1` in the example.
1. Forward HTTPS traffic (on port 443) with SnikketX hostnames to port 5443 *without* terminating TLS, since SnikketX obtains certificates by itself.
1. Forward encrypted XMPP traffic on port 443 to port 5223.
1. Forward unencrypted XMPP traffic on port 443 to port 5222 (which uses STARTTLS).
1. Forward the remaining traffic on port 443 to Caddy's standard HTTPS proxy.
1. Lastly, continue to run as a standard HTTP/S proxy.

```yaml
---
apps:
  layer4:             # layer4 plugin
    servers:
      srv0:
        listen:
        - ":443"      # the l4 plugin listens only on port 443
                      # for port 80 we use the http app
        routes:
        - match:
          - tls:      # match encrypted XMPP traffic
              alpn:
              - xmpp-client
          handle:
          - handler: proxy
            upstreams:
            - dial:   # and send to SnikketX's encrypted XMPP port
              - localhost:5223
        - match:
          - tls:
              sni:    # match HTTPS traffic containing SnikketX hostnames
              - chat.example.com
              - groups.chat.example.com
              - share.chat.example.com
          handle:
          - handler: proxy
            upstreams:
            - dial:   # and send to SnikketX's HTTPS port
              - localhost:5443
        - match:
          - xmpp: {}  # match unencrypted XMPP traffic
          handle:
          - handler: proxy
            upstreams:
            - dial:   # and send to SnikketX (will use STARTLS)
              - localhost:5222
        - handle:     # no `match` here, so it matches all leftover traffic
          - handler: proxy
            upstreams:
            - dial:   # send it to Caddy's HTTPS proxy, defined below
              - 127.0.0.1:1337
  http:
    https_port: 1337  # needed for HTTPS to work
    servers:
      srv1:
        listen:
        - ":80"       # handles SnikketX HTTP traffic, and redirects
                      # other HTTP traffic to HTTPS
        routes:
          - match:
            - host:   # send SnikketX's HTTP traffic to SnikketX's HTTP port
                      # this is needed to let SnikketX obtain certificates
              - chat.example.com
              - groups.chat.example.com
              - share.chat.example.com
            handle:
            - handler: subroute
              routes:
              - handle:
                - handler: reverse_proxy
                  upstreams:
                  - dial: localhost:5080
            terminal: true  # stop processing
          - handle:   # redirect leftover traffic to HTTPS
            - handler: static_response
              headers:
                Location:
                - https://{http.request.host}{http.request.uri}
      srv2:
        listen:
        - "127.0.0.1:1337"  # bind to localhost only
        routes:       # replace the below with your regular Caddy config (two standard examples are provided below)
        - match:      # this host will reverse proxy to port 1025
          - host:
            - reverse-proxy-1025.example.com
          handle:
          - handler: subroute
            routes:
            - handle:
              - handler: reverse_proxy
                upstreams:
                - dial: localhost:1025
          terminal: true
        - match:      # this host will proxy to port 1026
          - host:
            - reverse-proxy-1026.example.com
          handle:
          - handler: subroute
            routes:
            - handle:
              - handler: reverse_proxy
                upstreams:
                - dial: localhost:1026
          terminal: true
```

In case you are using Docker, don't forget to [add the `host.docker.internal` extra host](https://stackoverflow.com/questions/48546124/what-is-linux-equivalent-of-host-docker-internal/61001152) and replace `localhost` with `host.docker.internal` in `config.yaml`.
