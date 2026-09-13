#!/usr/bin/env python3
"""Create and verify the Cloudflare DNS records SnikketX needs.

Scope is locked to the XMPP domain (for example chat.example.com) and its
share./groups. subdomains plus the _xmpp-*._tcp SRV names. Nothing outside
that scope is ever created, changed, or deleted. A full backup of the zone
is saved before any write so the previous state can be restored.

Usage:
  ./scripts/dns-setup.py [--domain chat.example.com] [--token CF_TOKEN]
                         [--ipv4 A.B.C.D] [--ipv6 X:X::X | --no-ipv6]
                         [--dry-run] [--yes]

Token needs Zone:Read and DNS:Edit on the zone. Create a scoped token at
https://dash.cloudflare.com/profile/api-tokens (Create Custom Token ->
Zone.DNS:Edit + Zone.Zone:Read, Zone Resources: the zone only).
"""

import argparse
import datetime
import json
import os
import re
import socket
import sys
import urllib.error
import urllib.request

# CF_API_BASE exists only so tests can point the client at a mock API.
API = os.environ.get("CF_API_BASE",
                     "https://api.cloudflare.com/client/v4")
AUTO_TTL = 1  # 1 means automatic TTL at Cloudflare


def die(msg, code=1):
    print(f"ERROR  {msg}", file=sys.stderr)
    sys.exit(code)


def load_conf_value(key, path):
    try:
        with open(path, encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if line.startswith(key + "="):
                    return line.split("=", 1)[1].strip().strip('"').strip("'")
    except OSError:
        pass
    return ""


def api(token, method, path, payload=None, raw=False):
    req = urllib.request.Request(
        API + path,
        method=method,
        headers={
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        },
    )
    data = None
    if payload is not None:
        data = json.dumps(payload).encode()
    try:
        with urllib.request.urlopen(req, data=data, timeout=30) as resp:
            body = resp.read()
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", "replace")[:500]
        if exc.code == 403:
            die("Cloudflare rejected the token (403). It needs "
                "Zone:Read and DNS:Edit on the zone. "
                f"Response: {body}")
        die(f"Cloudflare API {method} {path} -> {exc.code}: {body}")
    if raw:
        return body.decode("utf-8", "replace")
    out = json.loads(body)
    if not out.get("success"):
        die(f"Cloudflare API {method} {path} failed: {out.get('errors')}")
    return out


def find_zone(token, domain):
    """Walk labels off the domain until a zone matches."""
    labels = domain.split(".")
    for i in range(len(labels) - 1):
        cand = ".".join(labels[i:])
        res = api(token, "GET", f"/zones?name={cand}")
        if res.get("result"):
            return res["result"][0]
    die(f"No Cloudflare zone found for {domain} "
        "(is the domain's zone in this account?)")


def all_records(token, zone_id):
    records, page = [], 1
    while True:
        res = api(token, "GET",
                  f"/zones/{zone_id}/dns_records?per_page=500&page={page}")
        records.extend(res["result"])
        info = res.get("result_info") or {}
        if page >= info.get("total_pages", 1):
            return records
        page += 1


def in_scope(name, domain):
    """True when name is the domain itself or a subdomain of it."""
    return name == domain or name.endswith("." + domain)


def desired_records(domain, ipv4, ipv6, want_caa):
    """The record set SnikketX needs. Names are built only from domain,
    so nothing outside its subtree can ever be targeted."""
    want = []
    for name in (domain, f"share.{domain}", f"groups.{domain}"):
        if ipv4:
            want.append({"type": "A", "name": name, "content": ipv4,
                         "ttl": AUTO_TTL, "proxied": False})
        if ipv6:
            want.append({"type": "AAAA", "name": name, "content": ipv6,
                         "ttl": AUTO_TTL, "proxied": False})
    want.append({
        "type": "SRV", "name": f"_xmpp-client._tcp.{domain}",
        "data": {"service": "_xmpp-client", "proto": "_tcp",
                 "name": domain, "priority": 0, "weight": 5,
                 "port": 5222, "target": domain},
        "ttl": AUTO_TTL,
    })
    want.append({
        "type": "SRV", "name": f"_xmpp-server._tcp.{domain}",
        "data": {"service": "_xmpp-server", "proto": "_tcp",
                 "name": domain, "priority": 0, "weight": 5,
                 "port": 5269, "target": domain},
        "ttl": AUTO_TTL,
    })
    if want_caa:
        for tag in ("issue", "issuewild"):
            want.append({
                "type": "CAA", "name": domain,
                "data": {"flags": 0, "tag": tag,
                         "value": "letsencrypt.org"},
                "ttl": AUTO_TTL,
            })
    return want


def rec_content(rec):
    if rec["type"] in ("SRV", "CAA", "TLSA"):
        d = rec.get("data") or {}
        return json.dumps(d, sort_keys=True)
    return str(rec.get("content", ""))


def matches(existing, want):
    if existing["type"] != want["type"] or existing["name"] != want["name"]:
        return False
    if want["type"] in ("A", "AAAA"):
        return (str(existing.get("content")) == str(want.get("content"))
                and bool(existing.get("proxied")) is False)
    if want["type"] in ("SRV", "CAA"):
        ed, wd = existing.get("data") or {}, want.get("data") or {}
        keys = set(ed) | set(wd)
        # Only compare the fields we set. Cloudflare fills in extras.
        keys -= {"name", "service", "proto"}
        return all(str(ed.get(k)) == str(wd.get(k)) for k in keys)
    return True


def describe(want):
    if want["type"] in ("A", "AAAA"):
        return f"{want['type']:5} {want['name']} -> {want['content']}"
    if want["type"] == "SRV":
        d = want["data"]
        return (f"SRV   {want['name']} -> "
                f"{d['priority']} {d['weight']} {d['port']} {d['target']}")
    if want["type"] == "CAA":
        d = want["data"]
        return f"CAA   {want['name']} -> {d['flags']} {d['tag']} \"{d['value']}\""
    return f"{want['type']} {want['name']}"


def detect_ipv4():
    for url in ("https://api.ipify.org", "https://ipv4.icanhazip.com"):
        try:
            with urllib.request.urlopen(url, timeout=10) as r:
                ip = r.read().decode().strip()
            socket.inet_pton(socket.AF_INET, ip)
            return ip
        except Exception:
            continue
    return ""


def detect_ipv6():
    try:
        with urllib.request.urlopen("https://api64.ipify.org", timeout=10) as r:
            ip = r.read().decode().strip()
        socket.inet_pton(socket.AF_INET6, ip)
        return ip
    except Exception:
        return ""


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--domain", default="")
    ap.add_argument("--token", default="")
    ap.add_argument("--ipv4", default="")
    ap.add_argument("--ipv6", default="")
    ap.add_argument("--no-ipv6", action="store_true")
    ap.add_argument("--no-caa", action="store_true",
                    help="skip CAA letsencrypt.org records")
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--yes", action="store_true", help="skip the apply prompt")
    args = ap.parse_args()

    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    domain = (args.domain
              or load_conf_value("SNIKKET_DOMAIN", os.path.join(root, "snikket.conf"))
              or load_conf_value("SNIKKET_DOMAIN", os.path.join(root, ".env")))
    if not domain:
        die("domain unknown. Pass --domain or set SNIKKET_DOMAIN")
    domain = domain.lower().strip().rstrip(".")

    token = (args.token or os.environ.get("CF_DNS_API_TOKEN", "")
             or load_conf_value("CF_DNS_API_TOKEN", os.path.join(root, ".env")))
    if not token:
        die("Cloudflare token missing. Pass --token, export "
            "CF_DNS_API_TOKEN, or add it to .env")

    print(f"== DNS setup for {domain} ==")

    print("step 1/6  locating Cloudflare zone")
    zone = find_zone(token, domain)
    zone_name = zone["name"]
    zone_id = zone["id"]
    print(f"        zone {zone_name} ({zone_id})")
    if not domain.endswith(zone_name):
        die(f"refusing: {domain} is not inside zone {zone_name}")

    print("step 2/6  backing up the full zone before any change")
    existing = all_records(token, zone_id)
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d-%H%M%S")
    backup_json = os.path.join(root, f"dns-backup-{zone_name}-{stamp}.json")
    with open(backup_json, "w", encoding="utf-8") as fh:
        json.dump(existing, fh, indent=2)
    zone_text = api(token, "GET", f"/zones/{zone_id}/dns_records/export",
                    raw=True)
    backup_zone = os.path.join(root, f"dns-backup-{zone_name}-{stamp}.zone")
    with open(backup_zone, "w", encoding="utf-8") as fh:
        fh.write(zone_text)
    print(f"        {len(existing)} records saved to")
    print(f"        {backup_json}")
    print(f"        {backup_zone}")

    print("step 3/6  detecting this server's public addresses")
    ipv4 = args.ipv4 or detect_ipv4()
    ipv6 = args.ipv6 or ("" if args.no_ipv6 else detect_ipv6())
    print(f"        IPv4: {ipv4 or 'not detected (A records skipped)'}")
    print(f"        IPv6: {ipv6 or 'none (AAAA records skipped)'}")
    if not ipv4 and not ipv6:
        die("no public IP detected. Pass --ipv4 (and --ipv6) explicitly")

    print("step 4/6  building the plan (scope: *.%s only)" % domain)
    want = desired_records(domain, ipv4, ipv6, not args.no_caa)
    for w in want:
        assert in_scope(w["name"], domain), f"out-of-scope record {w['name']}"

    creates, updates, keeps = [], [], []
    for w in want:
        hit = [e for e in existing
               if e["type"] == w["type"] and e["name"] == w["name"]]
        if any(matches(e, w) for e in hit):
            keeps.append(w)
        elif hit:
            updates.append((w, hit[0]))
        else:
            creates.append(w)

    for w in keeps:
        print(f"        keep    {describe(w)}")
    for w in creates:
        print(f"        create  {describe(w)}")
    for w, old in updates:
        print(f"        update  {describe(w)}  (was {rec_content(old)})")

    # Warn about in-scope records that conflict with what we need.
    # Records already queued for update are not conflicts.
    wanted_names = {(w["type"], w["name"]) for w in want}
    update_ids = {old["id"] for _w, old in updates}
    host_names = (domain, f"share.{domain}", f"groups.{domain}")
    conflicts = []
    for e in existing:
        if not in_scope(e["name"], domain) or e["id"] in update_ids:
            continue
        if e["type"] == "CNAME" and e["name"] in host_names:
            conflicts.append(e)
        if e["type"] == "A" and ipv4 and e["name"] in host_names \
                and str(e.get("content")) != ipv4:
            conflicts.append(e)
        if e["type"] == "AAAA" and ipv6 and e["name"] in host_names \
                and str(e.get("content")) != ipv6:
            conflicts.append(e)
        if e["type"] == "CAA" and e["name"] == domain:
            d = e.get("data") or {}
            if d.get("tag") in ("issue", "issuewild") \
                    and "letsencrypt.org" not in str(d.get("value", "")) \
                    and str(d.get("value", "")) != ";":
                conflicts.append(e)
    for e in conflicts:
        print(f"        NOTE    {e['type']} {e['name']} conflicts: "
              f"{rec_content(e)} (left untouched, review manually)")

    # Records inside scope we did not ask for are reported, never removed.
    extras = [e for e in existing
              if in_scope(e["name"], domain)
              and (e["type"], e["name"]) not in wanted_names
              and e not in conflicts]
    if extras:
        print("        other in-scope records exist and are left alone:")
        for e in extras[:15]:
            print(f"          {e['type']:5} {e['name']} -> {rec_content(e)}")

    if args.dry_run:
        print("step 5/6  dry run, no changes written")
        print("step 6/6  skipped (dry run)")
        print("")
        print("Re-run without --dry-run to apply the plan above.")
        return
    if not creates and not updates:
        print("step 5/6  nothing to change, all records already correct")
    else:
        if not args.yes:
            ans = input(f"step 5/6  apply {len(creates)} creates and "
                        f"{len(updates)} updates? [y/N] ")
            if ans.strip().lower() not in ("y", "yes"):
                die("aborted by user", 0)
        else:
            print("step 5/6  applying changes")
        for w in creates:
            api(token, "POST", f"/zones/{zone_id}/dns_records", w)
            print(f"        created {describe(w)}")
        for w, old in updates:
            api(token, "PUT",
                f"/zones/{zone_id}/dns_records/{old['id']}", w)
            print(f"        updated {describe(w)}")

    print("step 6/6  verifying through the Cloudflare API")
    after = all_records(token, zone_id)
    missing = 0
    for w in want:
        hit = [e for e in after
               if e["type"] == w["type"] and e["name"] == w["name"]
               and matches(e, w)]
        if hit:
            print(f"        PASS  {describe(w)}")
        else:
            print(f"        FAIL  {describe(w)} (not present after apply)")
            missing += 1

    if missing:
        die(f"{missing} record(s) missing after apply")
    print("")
    print("Done. Public resolvers can take a moment to pick up changes.")
    print(f"Run ./scripts/verify-domain.py --domain {domain} to check the")
    print("live service end to end.")


if __name__ == "__main__":
    main()
