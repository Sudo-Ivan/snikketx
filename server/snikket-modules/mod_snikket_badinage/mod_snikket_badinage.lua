-- Serves the Badinage web client as a static single-page app.
--
-- Badinage is a fully static build (Vite). Clients pick their XMPP server
-- at login and discover WebSocket/BOSH endpoints via XEP-0156 host-meta,
-- which mod_http_altconnect already publishes, so no runtime config is
-- injected here.
--
-- Files under assets/ carry content hashes in their names and are cached
-- immutably. Everything else, including index.html and the service worker,
-- is served with no-cache so updates deploy cleanly. Requests without a
-- file extension that do not match a file on disk fall back to index.html.

local lfs = require "lfs";
local http_files = require "prosody.net.http.files";
local urldecode = require "prosody.util.http".urldecode;

local open = io.open;
local stat = lfs.attributes;
local t_concat = table.concat;

module:depends "http";

local base_path = module:get_option_path("badinage_path", "/usr/local/lib/snikket-badinage");

local mime_map = setmetatable({
	css = "text/css";
	html = "text/html";
	ico = "image/x-icon";
	js = "application/javascript";
	json = "application/json";
	map = "application/json";
	png = "image/png";
	svg = "image/svg+xml";
	txt = "text/plain";
	webmanifest = "application/manifest+json";
	woff = "font/woff";
	woff2 = "font/woff2";
	xml = "application/xml";
}, { __index = function () return "application/octet-stream"; end });

-- Matches the headers shipped in Badinage's own nginx config.
local content_security_policy = t_concat({
	"default-src 'self'";
	"script-src 'self' 'wasm-unsafe-eval'";
	"style-src 'self' 'unsafe-inline'";
	"img-src 'self' data: blob: https:";
	"media-src blob: https:";
	"connect-src 'self' wss: https:";
	"font-src 'self'";
	"object-src 'none'";
	"base-uri 'self'";
	"form-action 'self'";
	"frame-ancestors 'none'";
}, "; ");

local security_headers = {
	x_content_type_options = "nosniff";
	x_frame_options = "DENY";
	referrer_policy = "no-referrer";
	permissions_policy = "camera=(), microphone=(), geolocation=()";
	content_security_policy = content_security_policy;
};

local serve_files = http_files.serve({ path = base_path; mime_map = mime_map });

local index_html;
do
	local f, err = open(base_path.."/index.html", "rb");
	if f then
		index_html = f:read("*a");
		f:close();
	else
		module:log("error", "Could not read %s/index.html: %s", base_path, err);
	end
end

local function sanitize_path(path)
	local out = {};
	local c = 0;
	for component in path:gmatch("[^/]+") do
		component = urldecode(component);
		if component:find("[/%z]") then
			return nil;
		elseif component == ".." then
			if c <= 0 then
				return nil;
			end
			out[c] = nil;
			c = c - 1;
		elseif component ~= "." then
			c = c + 1;
			out[c] = component;
		end
	end
	return "/"..t_concat(out, "/");
end

local function apply_headers(response, cache_control)
	local headers = response.headers;
	for name, value in pairs(security_headers) do
		headers[name] = value;
	end
	headers.cache_control = cache_control;
end

local function serve_app(event)
	if not index_html then
		return 503;
	end
	apply_headers(event.response, "no-cache");
	event.response.headers.content_type = "text/html";
	return index_html;
end

local function serve(event, path)
	local sanitized = sanitize_path(path or "");
	if not sanitized then
		return 400;
	end
	local attr = stat(base_path..sanitized);
	if attr and attr.mode == "file" then
		if sanitized:sub(1, 8) == "/assets/" then
			apply_headers(event.response, "public, max-age=31536000, immutable");
		else
			apply_headers(event.response, "no-cache");
		end
		return serve_files(event, sanitized);
	end
	if sanitized:match("%.[^/]*$") then
		return 404;
	end
	return serve_app(event);
end

module:provides("http", {
	name = "badinage";
	title = "Badinage";
	default_path = "/chat";
	route = {
		["GET /*"] = serve;
	};
});
