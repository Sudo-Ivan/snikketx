#!/bin/bash

set -eo pipefail

# shellcheck source=lib/compose.sh
source "$(cd "$(dirname "$0")" && pwd)/lib/compose.sh"

snikketx_cd_root
snikketx_parse_mode --dev "$@"

OUT="$(snikketx_root)/docs/images/portal-dashboard-dark.png"
mkdir -p "$(dirname "$OUT")"

domain="$(snikketx_domain)"
domain="${domain:-chat.localhost}"
base="http://${domain}:8080"
user="${SCREENSHOT_USER:-admin@${domain}}"
pass="${SCREENSHOT_PASSWORD:-admin}"

CHROMIUM="${CHROMIUM:-/usr/bin/chromium}"
if [[ ! -x "$CHROMIUM" ]]; then
	CHROMIUM="$(command -v chromium || command -v chromium-browser || true)"
fi
if [[ -z "$CHROMIUM" ]]; then
	echo "chromium not found" >&2
	exit 1
fi

WORKDIR="${TMPDIR:-/tmp}/snikketx-shot"
mkdir -p "$WORKDIR"
if [[ ! -d "$WORKDIR/node_modules/puppeteer-core" ]]; then
	(
		cd "$WORKDIR"
		npm init -y >/dev/null 2>&1 || true
		npm install puppeteer-core@24 >/dev/null
	)
fi

cat >"$WORKDIR/shot.mjs" <<EOF
import puppeteer from "puppeteer-core";

const out = process.env.SHOT_OUT;
const base = process.env.SHOT_BASE;
const user = process.env.SHOT_USER;
const pass = process.env.SHOT_PASS;
const chrome = process.env.SHOT_CHROME;
const domain = process.env.SHOT_DOMAIN;

const browser = await puppeteer.launch({
  executablePath: chrome,
  headless: true,
  args: [
    "--no-sandbox",
    "--disable-gpu",
    "--disable-dev-shm-usage",
    \`--host-resolver-rules=MAP \${domain} 127.0.0.1\`,
  ],
  defaultViewport: { width: 1280, height: 900, deviceScaleFactor: 1 },
});

const page = await browser.newPage();
await page.emulateMediaFeatures([{ name: "prefers-color-scheme", value: "dark" }]);
await page.goto(\`\${base}/login\`, { waitUntil: "networkidle0", timeout: 60000 });
await page.evaluate(() => {
  localStorage.setItem("snikketx-theme", "dark");
  document.documentElement.setAttribute("data-theme", "dark");
});
await page.reload({ waitUntil: "networkidle0", timeout: 60000 });
await page.type('input[name="address"]', user, { delay: 5 });
await page.type('input[name="password"]', pass, { delay: 5 });
await Promise.all([
  page.waitForNavigation({ waitUntil: "networkidle0", timeout: 60000 }),
  page.click("button[type=submit], form button"),
]);
await page.goto(\`\${base}/admin/\`, { waitUntil: "networkidle0", timeout: 60000 });
await page.waitForSelector(".footer", { timeout: 30000 });
await page.addStyleTag({
  content: \`
    html, body { height: auto !important; min-height: 0 !important; }
    .admin { height: auto !important; min-height: 0 !important; }
    .sidebar { position: relative !important; height: auto !important; overflow: visible !important; }
    .admin-main { min-height: 0 !important; }
    .admin-content { flex: 0 0 auto !important; }
    .footer { margin-top: 1.25rem !important; padding-bottom: 1rem !important; }
  \`,
});
await new Promise((r) => setTimeout(r, 400));
const final = await page.evaluate(() => {
  const footer = document.querySelector(".footer");
  const admin = document.querySelector(".admin");
  const bottom = Math.max(footer?.getBoundingClientRect().bottom || 0, admin?.getBoundingClientRect().bottom || 0);
  return {
    width: Math.min(1280, document.documentElement.clientWidth),
    height: Math.ceil(bottom + 20),
  };
});
await page.setViewport({ width: final.width, height: Math.min(final.height, 1800), deviceScaleFactor: 1 });
await new Promise((r) => setTimeout(r, 250));
const again = await page.evaluate(() => {
  const footer = document.querySelector(".footer");
  const admin = document.querySelector(".admin");
  const bottom = Math.max(footer?.getBoundingClientRect().bottom || 0, admin?.getBoundingClientRect().bottom || 0);
  return {
    width: Math.min(1280, document.documentElement.clientWidth),
    height: Math.ceil(bottom + 20),
  };
});
await page.screenshot({ path: out, clip: { x: 0, y: 0, width: again.width, height: again.height } });
await browser.close();
console.log("wrote", out, again);
EOF

export SHOT_OUT="$OUT" SHOT_BASE="$base" SHOT_USER="$user" SHOT_PASS="$pass" SHOT_CHROME="$CHROMIUM" SHOT_DOMAIN="$domain"
(cd "$WORKDIR" && node shot.mjs)

if command -v magick >/dev/null; then
	magick "$OUT" -resize 1000x "$OUT"
elif command -v convert >/dev/null; then
	convert "$OUT" -resize 1000x "$OUT"
fi

echo "Screenshot ready: $OUT"
