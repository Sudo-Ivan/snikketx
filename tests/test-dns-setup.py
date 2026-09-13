#!/usr/bin/env python3
"""Test dns-setup.py against a mock Cloudflare API.

Run from the repo root: python3 tests/test-dns-setup.py
"""

import json
import os
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCRIPT = os.path.join(ROOT, "scripts", "dns-setup.py")
DOMAIN = "chat.example.com"
ZONE = "example.com"
ZONE_ID = "zone123"

# Records the mock zone starts with. Includes records that must never be
# touched and one record dns-setup should update.
SEED = [
    {"id": "r1", "type": "A", "name": DOMAIN, "content": "192.0.2.1",
     "ttl": 1, "proxied": False},
    {"id": "r2", "type": "A", "name": f"www.{ZONE}", "content": "192.0.2.9",
     "ttl": 1, "proxied": False},
    {"id": "r3", "type": "MX", "name": ZONE, "content": f"mail.{ZONE}",
     "ttl": 3600, "priority": 10},
    {"id": "r4", "type": "TXT", "name": DOMAIN,
     "content": "v=spf1 -all", "ttl": 300},
    {"id": "r5", "type": "A", "name": f"mail.{ZONE}",
     "content": "192.0.2.10", "ttl": 1, "proxied": False},
]


class MockCF(BaseHTTPRequestHandler):
    records = []
    writes = []

    def _send(self, obj, code=200, raw=False):
        body = obj.encode() if isinstance(obj, str) else json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urlparse(self.path)
        qs = parse_qs(parsed.query)
        if parsed.path == "/zones":
            name = qs.get("name", [""])[0]
            result = ([{"id": ZONE_ID, "name": ZONE}]
                      if name == ZONE else [])
            self._send({"success": True, "result": result})
        elif parsed.path == f"/zones/{ZONE_ID}/dns_records":
            self._send({"success": True, "result": MockCF.records,
                        "result_info": {"total_pages": 1}})
        elif parsed.path == f"/zones/{ZONE_ID}/dns_records/export":
            text = "".join(
                f"{r['name']} {r.get('ttl', 1)} IN {r['type']} "
                f"{r.get('content', '')}\n" for r in MockCF.records)
            self._send(text)
        else:
            self._send({"success": False, "errors": ["not found"]}, 404)

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path == f"/zones/{ZONE_ID}/dns_records":
            body = json.loads(self.rfile.read(
                int(self.headers["Content-Length"])))
            body["id"] = f"new{len(MockCF.records)}"
            MockCF.records.append(body)
            MockCF.writes.append(("POST", body))
            self._send({"success": True, "result": body})
        else:
            self._send({"success": False, "errors": ["not found"]}, 404)

    def do_PUT(self):
        parsed = urlparse(self.path)
        prefix = f"/zones/{ZONE_ID}/dns_records/"
        if parsed.path.startswith(prefix):
            rid = parsed.path[len(prefix):]
            body = json.loads(self.rfile.read(
                int(self.headers["Content-Length"])))
            for i, rec in enumerate(MockCF.records):
                if rec["id"] == rid:
                    MockCF.records[i] = {**body, "id": rid}
                    break
            MockCF.writes.append(("PUT", body))
            self._send({"success": True, "result": body})
        else:
            self._send({"success": False, "errors": ["not found"]}, 404)

    def do_DELETE(self):
        MockCF.writes.append(("DELETE", self.path))
        self._send({"success": False, "errors": ["deletes are not allowed"]},
                   403)

    def log_message(self, *args):
        pass


class DNSSetupTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        MockCF.records = [dict(r) for r in SEED]
        MockCF.writes = []
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), MockCF)
        cls.port = cls.server.server_address[1]
        cls.thread = threading.Thread(target=cls.server.serve_forever,
                                      daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()

    def setUp(self):
        MockCF.records = [dict(r) for r in SEED]
        MockCF.writes = []

    def run_script(self, *extra):
        env = dict(os.environ)
        env["CF_API_BASE"] = f"http://127.0.0.1:{self.port}"
        return subprocess.run(
            [sys.executable, SCRIPT, "--domain", DOMAIN, "--token", "tok",
             "--ipv4", "198.51.100.7", "--no-ipv6", "--yes", *extra],
            cwd=ROOT, env=env, capture_output=True, text=True, timeout=60)

    def tearDown(self):
        # dns-setup writes backups into the repo root. Clean them up.
        for name in os.listdir(ROOT):
            if name.startswith(f"dns-backup-{ZONE}-"):
                os.unlink(os.path.join(ROOT, name))

    def test_apply_only_touches_scope(self):
        proc = self.run_script()
        self.assertEqual(proc.returncode, 0, proc.stderr + proc.stdout)

        writes = MockCF.writes
        self.assertTrue(writes, "expected creates or updates")
        for method, body in writes:
            self.assertIn(method, ("POST", "PUT"),
                          "dns-setup must never delete records")
            self.assertTrue(
                body["name"] == DOMAIN or body["name"].endswith("." + DOMAIN),
                f"out-of-scope write to {body['name']}")

        names = {(r["type"], r["name"]) for r in MockCF.records}
        for want in [
            ("A", DOMAIN), ("A", f"share.{DOMAIN}"), ("A", f"groups.{DOMAIN}"),
            ("SRV", f"_xmpp-client._tcp.{DOMAIN}"),
            ("SRV", f"_xmpp-server._tcp.{DOMAIN}"),
            ("CAA", DOMAIN),
        ]:
            self.assertIn(want, names, f"missing {want}")

        # The stale A record was updated in place, not duplicated.
        a_recs = [r for r in MockCF.records
                  if r["type"] == "A" and r["name"] == DOMAIN]
        self.assertEqual(len(a_recs), 1)
        self.assertEqual(a_recs[0]["content"], "198.51.100.7")

        # Untouched records are still present.
        self.assertIn(("MX", ZONE), names)
        self.assertIn(("TXT", DOMAIN), names)
        self.assertIn(("A", f"mail.{ZONE}"), names)
        self.assertIn(("A", f"www.{ZONE}"), names)

    def test_dry_run_writes_nothing(self):
        MockCF.writes = []
        proc = self.run_script("--dry-run")
        self.assertEqual(proc.returncode, 0, proc.stderr + proc.stdout)
        posts = [w for w in MockCF.writes if w[0] in ("POST", "PUT")]
        self.assertEqual(posts, [], "dry run must not write")
        self.assertIn("dry run", proc.stdout)

    def test_second_run_is_idempotent(self):
        proc1 = self.run_script()
        self.assertEqual(proc1.returncode, 0, proc1.stderr + proc1.stdout)
        MockCF.writes = []
        proc2 = self.run_script()
        self.assertEqual(proc2.returncode, 0, proc2.stderr + proc2.stdout)
        self.assertEqual(MockCF.writes, [],
                         "second run should keep all records")

    def test_backups_written(self):
        # Run in a copy of the tree so the backup lands somewhere we can
        # inspect, then confirm the naming pattern holds.
        proc = self.run_script()
        self.assertEqual(proc.returncode, 0, proc.stderr + proc.stdout)
        backups = [n for n in os.listdir(ROOT)
                   if n.startswith(f"dns-backup-{ZONE}-")]
        self.assertTrue(any(n.endswith(".json") for n in backups))
        self.assertTrue(any(n.endswith(".zone") for n in backups))

    def test_rejects_domain_outside_zone(self):
        env = dict(os.environ)
        env["CF_API_BASE"] = f"http://127.0.0.1:{self.port}"
        proc = subprocess.run(
            [sys.executable, SCRIPT, "--domain", "chat.other.org",
             "--token", "tok", "--yes"],
            cwd=ROOT, env=env, capture_output=True, text=True, timeout=60)
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("zone", proc.stderr.lower())


if __name__ == "__main__":
    unittest.main(verbosity=2)
