#!/usr/bin/env python3
"""Verify DNS, TLS, DANE and service reachability for a SnikketX domain.

Pure stdlib: works on hosts without dig, dnspython or openssl CLI.
Exit code 0 when nothing fails, 1 otherwise (warnings do not fail).

Usage: ./scripts/verify-domain.py [--domain chat.example.com] [--timeout 5]
       [--nameserver 1.1.1.1] [--skip-external] [--quiet]
"""

import argparse
import hashlib
import os
import socket
import ssl
import struct
import sys
import urllib.error
import urllib.request
from datetime import datetime, timezone

QTYPE = {"SRV": 33, "TLSA": 52, "CAA": 257}
TLSA_USAGE = {0: "PKIX-CA", 1: "PKIX-EE", 2: "DANE-TA", 3: "DANE-EE"}
TLSA_SELECTOR = {0: "cert", 1: "spki"}
TLSA_MTYPE = {0: "full", 1: "sha256", 2: "sha512"}

PASS, WARN, FAIL, INFO = "PASS", "WARN", "FAIL", "INFO"
counts = {PASS: 0, WARN: 0, FAIL: 0, INFO: 0}
quiet = False


def report(level, msg):
    counts[level] += 1
    if quiet and level == INFO:
        return
    print(f"{level:5} {msg}")


# --- minimal DNS resolver -------------------------------------------------

def system_nameservers():
    servers = []
    try:
        with open("/etc/resolv.conf") as fh:
            for line in fh:
                parts = line.split()
                if len(parts) >= 2 and parts[0] == "nameserver":
                    servers.append(parts[1])
    except OSError:
        pass
    return [s for s in servers if ":" not in s] or ["1.1.1.1", "8.8.8.8"]


def decode_name(buf, off):
    """Decode a possibly-compressed DNS name. Returns (name, offset_after)."""
    labels = []
    end = None
    while True:
        ln = buf[off]
        if ln & 0xC0 == 0xC0:
            ptr = ((ln & 0x3F) << 8) | buf[off + 1]
            if end is None:
                end = off + 2
            off = ptr
            continue
        if ln == 0:
            if end is None:
                end = off + 1
            return ".".join(labels), end
        off += 1
        labels.append(buf[off:off + ln].decode("ascii", "replace"))
        off += ln


def dns_query(name, qtype, nameserver, timeout):
    """Return answer RRs as (type, rdata_offset, rdata_bytes, message)."""
    tid = os.urandom(2)
    qname = b"".join(bytes([len(p)]) + p.encode()
                     for p in name.split(".")) + b"\x00"
    query = (tid + b"\x01\x00" + struct.pack(">HHHH", 1, 0, 0, 0) +
             qname + struct.pack(">HH", qtype, 1))
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.settimeout(timeout)
        sock.sendto(query, (nameserver, 53))
        buf, _ = sock.recvfrom(4096)
    if buf[2] & 0x02:  # truncated: retry over TCP
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.settimeout(timeout)
            sock.connect((nameserver, 53))
            sock.sendall(struct.pack(">H", len(query)) + query)
            raw = sock.recv(2)
            buf = b""
            want = struct.unpack(">H", raw)[0]
            while len(buf) < want:
                chunk = sock.recv(want - len(buf))
                if not chunk:
                    break
                buf += chunk
    if len(buf) < 12 or buf[:2] != tid:
        raise OSError("bad DNS response")
    rcode = buf[3] & 0x0F
    if rcode == 3:
        return []
    if rcode != 0:
        raise OSError(f"DNS rcode {rcode}")
    qd, an = struct.unpack(">HH", buf[4:8])
    off = 12
    for _ in range(qd):
        _, off = decode_name(buf, off)
        off += 4
    answers = []
    for _ in range(an):
        _, off = decode_name(buf, off)
        rtype, _class, _ttl, rdlen = struct.unpack(">HHIH", buf[off:off + 10])
        off += 10
        if rtype == qtype:
            answers.append((rtype, off, buf[off:off + rdlen], buf))
        off += rdlen
    return answers


def lookup(name, rtype, nameserver, timeout):
    try:
        return dns_query(name, QTYPE[rtype], nameserver, timeout)
    except OSError as exc:
        report(WARN, f"DNS {rtype} {name}: query failed ({exc})")
        return []


# --- cert / DER helpers ----------------------------------------------------

def der_read(buf, off):
    """Return (tag, content_bytes, next_offset) for one DER element."""
    tag = buf[off]
    ln = buf[off + 1]
    hdr = 2
    if ln & 0x80:
        n = ln & 0x7F
        ln = int.from_bytes(buf[off + 2:off + 2 + n], "big")
        hdr += n
    return tag, buf[off + hdr:off + hdr + ln], off + hdr + ln


def cert_spki_der(cert_der):
    """Extract subjectPublicKeyInfo DER from an X.509 certificate."""
    _, cert_body, _ = der_read(cert_der, 0)      # Certificate SEQ content
    _, tbs, _ = der_read(cert_body, 0)           # tbsCertificate SEQ content
    off = 0
    tag, _, next_off = der_read(tbs, 0)
    if tag == 0xA0:                              # explicit version [0]
        off = next_off
    for _ in range(5):                           # serial, sig, issuer,
        _, _, off = der_read(tbs, off)           # validity, subject
    spki_end = der_read(tbs, off)[2]
    return tbs[off:spki_end]


def tlsa_match(rdata, cert_der):
    if len(rdata) < 4:
        return "malformed"
    usage, selector, mtype = rdata[0], rdata[1], rdata[2]
    desc = (f"{TLSA_USAGE.get(usage, usage)}/"
            f"{TLSA_SELECTOR.get(selector, selector)}/"
            f"{TLSA_MTYPE.get(mtype, mtype)}")
    try:
        target = cert_der if selector == 0 else cert_spki_der(cert_der)
    except Exception:
        return f"unparsed-cert ({desc})"
    if mtype == 1:
        got = hashlib.sha256(target).digest()
    elif mtype == 2:
        got = hashlib.sha512(target).digest()
    elif mtype == 0:
        got = target
    else:
        return f"unknown-mtype ({desc})"
    return "match" if got == rdata[3:] else f"MISMATCH ({desc})"


def cert_summary(info):
    exp = datetime.strptime(info["notAfter"], "%b %d %H:%M:%S %Y %Z")
    days = (exp.replace(tzinfo=timezone.utc)
            - datetime.now(timezone.utc)).days
    issuer = {}
    for rdn in info.get("issuer", ()):
        for key, val in rdn:
            issuer[key] = val
    sans = [v for k, v in info.get("subjectAltName", ()) if k == "DNS"]
    return days, issuer.get("organizationName", "?"), sans


def fetch_tls_cert(host, port, timeout):
    ctx = ssl.create_default_context()
    with socket.create_connection((host, port), timeout=timeout) as raw:
        with ctx.wrap_socket(raw, server_hostname=host) as tls:
            return tls.getpeercert(), tls.getpeercert(binary_form=True)


def fetch_xmpp_cert(domain, port, namespace, timeout):
    """Do the STARTTLS dance and return (peer_cert_info, peer_cert_der)."""
    ns = b"jabber:client" if namespace == "client" else b"jabber:server"
    with socket.create_connection((domain, port), timeout=timeout) as raw:
        raw.settimeout(timeout)
        raw.sendall(
            b"<?xml version='1.0'?><stream:stream to='" + domain.encode() +
            b"' xmlns='" + ns +
            b"' xmlns:stream='http://etherx.jabber.org/streams'"
            b" version='1.0'>")
        banner = raw.recv(65536)
        if b"<starttls" not in banner:
            hint = banner[:200].decode("utf-8", "replace").strip()
            raise OSError(f"server did not offer STARTTLS ({hint or 'no banner'})")
        raw.sendall(
            b"<starttls xmlns='urn:ietf:params:xml:ns:xmpp-tls'/>")
        reply = raw.recv(4096)
        if b"<proceed" not in reply:
            hint = reply[:200].decode("utf-8", "replace").strip()
            raise OSError(f"server refused STARTTLS ({hint or 'empty reply'})")
        ctx = ssl.create_default_context()
        with ctx.wrap_socket(raw, server_hostname=domain) as tls:
            return tls.getpeercert(), tls.getpeercert(binary_form=True)


# --- checks ----------------------------------------------------------------

def check_dns(domain):
    hosts = [domain, f"share.{domain}", f"groups.{domain}"]
    for host in hosts:
        try:
            infos = socket.getaddrinfo(host, None, proto=socket.IPPROTO_TCP)
            addrs = sorted({i[4][0] for i in infos})
            report(PASS, f"DNS resolves {host} -> {', '.join(addrs)}")
        except socket.gaierror:
            report(FAIL, f"DNS does not resolve {host}")
    return hosts


def check_srv(domain, nameserver, timeout):
    found = False
    for srv in (f"_xmpp-client._tcp.{domain}", f"_xmpp-server._tcp.{domain}"):
        for _t, roff, _rdata, msg in lookup(srv, "SRV", nameserver, timeout):
            prio, weight, port = struct.unpack(">HHH", msg[roff:roff + 6])
            target, _ = decode_name(msg, roff + 6)
            if target and port:
                report(PASS, f"SRV {srv} -> {target}:{port}")
                found = True
    if not found:
        report(WARN, f"No XMPP SRV records for {domain} "
                     "(clients fall back to A/AAAA on 5222/5269)")


def check_caa(domain, nameserver, timeout):
    records = lookup(domain, "CAA", nameserver, timeout)
    if not records:
        report(INFO, f"No CAA record on {domain} (any CA may issue)")
        return
    ok = False
    for _t, _o, rdata, _m in records:
        if len(rdata) < 2:
            continue
        tag_len = rdata[1]
        tag = rdata[2:2 + tag_len].decode("ascii", "replace")
        val = rdata[2 + tag_len:].decode("ascii", "replace").strip('"')
        report(INFO, f"CAA {domain}: {tag} {val}")
        if tag == "issue" and ("letsencrypt.org" in val or val == ";"):
            ok = True
    if not ok:
        report(FAIL, f"CAA on {domain} does not permit letsencrypt.org")


def check_tls(host, timeout, label="TLS"):
    try:
        info, der = fetch_tls_cert(host, 443, timeout)
    except ssl.SSLCertVerificationError as exc:
        report(FAIL, f"{label} {host}:443 certificate invalid "
                     f"({exc.verify_message})")
        return None
    except Exception as exc:
        report(FAIL, f"{label} {host}:443 handshake failed ({exc})")
        return None
    days, issuer, sans = cert_summary(info)
    level = PASS if days > 14 else WARN if days > 0 else FAIL
    report(level, f"{label} {host}:443 valid, {days}d left, "
                  f"issuer {issuer}, SANs {','.join(sans) or '-'}")
    return der


def check_xmpp(domain, port, namespace, label, timeout):
    try:
        info, der = fetch_xmpp_cert(domain, port, namespace, timeout)
    except ssl.SSLCertVerificationError as exc:
        report(FAIL, f"XMPP {label} {domain}:{port} cert invalid "
                     f"({exc.verify_message})")
        return None
    except Exception as exc:
        report(FAIL, f"XMPP {label} {domain}:{port} failed ({exc})")
        return None
    days, issuer, sans = cert_summary(info)
    level = PASS if days > 14 else WARN if days > 0 else FAIL
    report(level, f"XMPP {label} {domain}:{port} STARTTLS ok, {days}d left, "
                  f"issuer {issuer}, SANs {','.join(sans) or '-'}")
    return der


def check_tlsa(port, domain, nameserver, timeout, cert_der):
    name = f"_{port}._tcp.{domain}"
    records = lookup(name, "TLSA", nameserver, timeout)
    if not records:
        report(INFO, f"No TLSA {name} (DANE not published)")
        return
    if cert_der is None:
        report(WARN, f"TLSA {name} present but no peer cert to compare")
        return
    for _t, _o, rdata, _m in records:
        res = tlsa_match(rdata, cert_der)
        if res == "match":
            report(PASS, f"TLSA {name} matches live certificate")
        elif res.startswith("MISMATCH"):
            report(FAIL, f"TLSA {name} {res}")
        else:
            report(WARN, f"TLSA {name} {res}")


def check_https(host, timeout):
    url = f"https://{host}/login"
    try:
        req = urllib.request.Request(
            url, headers={"User-Agent": "snikketx-verify"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            code = resp.status
    except urllib.error.HTTPError as exc:
        code = exc.code
    except Exception as exc:
        report(FAIL, f"HTTPS {url} unreachable ({exc})")
        return
    if code in (200, 301, 302, 303):
        report(PASS, f"HTTPS {url} -> {code}")
    else:
        report(FAIL, f"HTTPS {url} -> {code}")


def check_addr_reach(domain, port, timeout):
    """Connect to every resolved address directly. An advertised address
    that does not answer is a misconfiguration, because clients prefer
    IPv6 and would hit a dead end."""
    try:
        infos = socket.getaddrinfo(domain, port, proto=socket.IPPROTO_TCP)
    except socket.gaierror:
        return
    seen = set()
    for fam, _t, _p, _c, sa in infos:
        label = "IPv6" if fam == socket.AF_INET6 else "IPv4"
        if label in seen:
            continue
        seen.add(label)
        try:
            # IPv6 sockaddr is a 4-tuple, create_connection wants 2.
            with socket.create_connection(sa[:2], timeout=timeout):
                report(PASS, f"{label} {domain}:{port} accepts "
                             f"connections ({sa[0]})")
        except Exception as exc:
            rtype = "AAAA" if fam == socket.AF_INET6 else "A"
            report(FAIL, f"{label} {domain}:{port} does not answer "
                         f"({sa[0]}: {exc}). Either publish {label} for "
                         f"this service or remove the {rtype} record")


def check_tcp(domain, port, label, timeout):
    try:
        with socket.create_connection((domain, port), timeout=timeout):
            report(PASS, f"TCP {domain}:{port} ({label}) reachable")
    except Exception as exc:
        report(WARN, f"TCP {domain}:{port} ({label}) unreachable ({exc})")


def check_tls_port(domain, port, label, timeout):
    """Direct TLS listener check (no STARTTLS), for TURN-TLS and c2s 5223."""
    try:
        info, _der = fetch_tls_cert(domain, port, timeout)
    except ssl.SSLCertVerificationError as exc:
        report(FAIL, f"{label} {domain}:{port} cert invalid "
                     f"({exc.verify_message})")
        return
    except Exception as exc:
        report(FAIL, f"{label} {domain}:{port} failed ({exc})")
        return
    days, issuer, sans = cert_summary(info)
    level = PASS if days > 14 else WARN if days > 0 else FAIL
    report(level, f"{label} {domain}:{port} valid, {days}d left, "
                  f"issuer {issuer}, SANs {','.join(sans) or '-'}")


def check_stun(domain, timeout):
    """Send a real RFC 5389 binding request to the STUN port."""
    tid = os.urandom(12)
    req = struct.pack(">HHI12s", 0x0001, 0, 0x2112A442, tid)
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.settimeout(timeout)
            sock.sendto(req, (domain, 3478))
            data, _ = sock.recvfrom(2048)
    except Exception as exc:
        report(FAIL, f"STUN {domain}:3478/udp no answer ({exc})")
        return
    if len(data) < 20:
        report(FAIL, f"STUN {domain}:3478/udp short reply")
        return
    rtype, _rlen, magic = struct.unpack(">HHI", data[:8])
    if rtype != 0x0101 or magic != 0x2112A442:
        report(FAIL, f"STUN {domain}:3478/udp bad response "
                     f"(type 0x{rtype:04x})")
        return
    mapped = ""
    off = 20
    while off + 4 <= len(data):
        atype, alen = struct.unpack(">HH", data[off:off + 4])
        aval = data[off + 4:off + 4 + alen]
        if atype == 0x0020 and len(aval) >= 8 and aval[1] == 0x01:
            port = struct.unpack(">H", aval[2:4])[0] ^ (0x2112A442 >> 16)
            addr = bytes(b ^ c for b, c in
                         zip(aval[4:8], struct.pack(">I", 0x2112A442)))
            mapped = f" mapped {socket.inet_ntoa(addr)}:{port}"
            break
        if atype == 0x0001 and len(aval) >= 8 and aval[1] == 0x01:
            port = struct.unpack(">H", aval[2:4])[0]
            mapped = f" mapped {socket.inet_ntoa(aval[4:8])}:{port}"
            break
        off += 4 + alen + (-alen % 4)
    report(PASS, f"STUN {domain}:3478/udp binding response ok{mapped}")


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--domain", default=os.environ.get("SNIKKET_DOMAIN", ""))
    ap.add_argument("--nameserver", default=None)
    ap.add_argument("--timeout", type=float, default=5.0)
    ap.add_argument("--quiet", action="store_true", help="hide INFO lines")
    ap.add_argument("--skip-external", action="store_true",
                    help="only check DNS, skip TLS/port probes")
    args = ap.parse_args()

    global quiet
    quiet = args.quiet

    domain = args.domain.strip().lower()
    if not domain and os.path.exists("snikket.conf"):
        with open("snikket.conf") as fh:
            for line in fh:
                if line.startswith("SNIKKET_DOMAIN="):
                    domain = line.split("=", 1)[1].strip().strip("\"'")
    if not domain:
        ap.error("no domain: pass --domain or set SNIKKET_DOMAIN")

    nameserver = args.nameserver or system_nameservers()[0]
    print(f"== verify-domain {domain} (resolver {nameserver}) ==")

    hosts = check_dns(domain)
    check_srv(domain, nameserver, args.timeout)
    check_caa(domain, nameserver, args.timeout)

    if not args.skip_external:
        leaf_443 = check_tls(domain, args.timeout)
        for host in hosts[1:]:
            check_tls(host, args.timeout)
        check_https(domain, args.timeout)
        der_5222 = check_xmpp(domain, 5222, "client", "c2s", args.timeout)
        der_5269 = check_xmpp(domain, 5269, "server", "s2s", args.timeout)
        check_tlsa(443, domain, nameserver, args.timeout, leaf_443)
        check_tlsa(5222, domain, nameserver, args.timeout, der_5222)
        check_tlsa(5269, domain, nameserver, args.timeout, der_5269)
        check_tls_port(domain, 5223, "XMPP direct TLS", args.timeout)
        check_addr_reach(domain, 443, args.timeout)
        check_addr_reach(domain, 5222, args.timeout)
        check_stun(domain, args.timeout)
        check_tcp(domain, 3478, "STUN/TURN", args.timeout)
        check_tls_port(domain, 5349, "TURN TLS", args.timeout)

    print(f"\nSummary: {counts[PASS]} pass, {counts[WARN]} warn, "
          f"{counts[FAIL]} fail")
    return 1 if counts[FAIL] else 0


if __name__ == "__main__":
    sys.exit(main())
