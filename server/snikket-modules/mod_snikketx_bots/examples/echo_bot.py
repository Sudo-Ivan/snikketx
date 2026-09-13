#!/usr/bin/env python3
"""Webhook echo bot for mod_snikketx_bots. Stdlib only.

Receives bot events as JSON POSTs and replies through the REST API.
Configure the bot webhook to point at http://<host>:9000/hook and set
BOT_TOKEN to the sxb_ token minted at creation time.

Environment:
  BOT_TOKEN   sxb_... bearer token (required)
  BOT_NAME    bot name, used for the API path (required)
  API_URL     base API URL including host header target
              (default http://127.0.0.1:5280/bots)
  API_HOST    value of the Host header, the component host
              (default bots.localhost)
  HOOK_SECRET webhook secret to verify X-Bots-Signature (optional)
  LISTEN      webhook bind address (default 127.0.0.1:9000)
"""

import hashlib
import hmac
import http.server
import json
import os
import urllib.request

BOT_TOKEN = os.environ["BOT_TOKEN"]
BOT_NAME = os.environ["BOT_NAME"]
API_URL = os.environ.get("API_URL", "http://127.0.0.1:5280/bots")
API_HOST = os.environ.get("API_HOST", "bots.localhost")
HOOK_SECRET = os.environ.get("HOOK_SECRET")
LISTEN = os.environ.get("LISTEN", "127.0.0.1:9000")


def api(path, payload=None, method=None):
    req = urllib.request.Request(
        API_URL + "/" + BOT_NAME + path,
        data=json.dumps(payload).encode() if payload is not None else None,
        method=method or ("POST" if payload is not None else "GET"),
        headers={
            "Host": API_HOST,
            "Authorization": "Bearer " + BOT_TOKEN,
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req) as res:
        return json.loads(res.read())


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)

        if HOOK_SECRET:
            sig = self.headers.get("X-Bots-Signature", "")
            expect = "sha256=" + hmac.new(
                HOOK_SECRET.encode(), body, hashlib.sha256
            ).hexdigest()
            if not hmac.compare_digest(sig, expect):
                self.send_response(403)
                self.end_headers()
                return

        event = json.loads(body)
        self.send_response(200)
        self.end_headers()
        self.respond(event)

    def respond(self, event):
        if event.get("type") != "message":
            return
        body = event.get("body") or ""
        if not body:
            return
        kind = event["kind"]
        if kind == "groupchat":
            target = event["room"]
        elif kind == "pm":
            target = event["from"]  # room@host/nick
        else:
            target = event["from"]
        try:
            api("/send", {
                "to": target,
                "kind": "groupchat" if kind == "groupchat" else "chat",
                "body": "echo: " + body,
            })
        except Exception as err:
            print("send failed:", err)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    host, _, port = LISTEN.rpartition(":")
    print("echo bot listening on", LISTEN)
    http.server.HTTPServer((host, int(port)), Handler).serve_forever()
