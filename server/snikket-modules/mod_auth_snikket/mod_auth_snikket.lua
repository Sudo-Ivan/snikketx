-- SnikketX authentication provider. Wraps the configured password
-- backend (internal_hashed or ldap2) so every existing mechanism keeps
-- working, and additionally advertises SASL OAUTHBEARER (RFC 7628).
--
-- Bearer tokens are validated against the userinfo endpoint of an
-- external OAuth2/OIDC authorization server. On a failed token the
-- provider relays the IdP discovery URL in the RFC 7628 JSON error
-- document, which is what XEP-0493 clients such as Badinage read to
-- start a full OAuth flow.
--
-- Activate with:
--   authentication = "snikket";
--   oauth_backend = "internal_hashed"; -- or "ldap2"
--   oauth_discovery_url = "https://id.example.com/.well-known/openid-configuration";
--   oauth_validation_endpoint = "https://id.example.com/api/oidc/userinfo";
--   oauth_username_field = "preferred_username";
--luacheck: ignore 111 113/module 131 143/module

local modulemanager = require "prosody.core.modulemanager";
local http = require "prosody.net.http";
local async = require "prosody.util.async";
local json = require "prosody.util.json";
local sasl = require "prosody.util.sasl";
local jid = require "prosody.util.jid";
local nodeprep = require "prosody.util.encodings".stringprep.nodeprep;

local backend_name = module:get_option_string("oauth_backend", "internal_hashed");
local discovery_url = module:get_option_string("oauth_discovery_url");
local validation_endpoint = module:get_option_string("oauth_validation_endpoint");
local username_field = module:get_option_string("oauth_username_field", "preferred_username");
local scope = module:get_option_string("oauth_scope", "openid");
local allow_authzid = module:get_option_boolean("oauth_allow_authzid", false);

assert(validation_endpoint,
	"oauth_validation_endpoint is required when authentication = \"snikket\"");

-- The backend auth provider must be loaded so it can answer all the
-- user-management and password SASL calls we delegate to it.
module:depends("auth_" .. backend_name);

local backend;
for _, candidate in modulemanager.get_items("auth-provider", module.host) do
	if candidate.name == backend_name then
		backend = candidate;
	end
end
assert(backend, "oauth_backend '" .. backend_name .. "' did not load");

local http_client = http.default:new(
	module:get_option("oauth_http_settings") or { connection_pooling = true });

local function oauth_error(status)
	return nil, nil, {
		status = status;
		scope = scope;
		oidc_discovery_url = discovery_url;
	};
end

-- SASL profile hook called by util.sasl.oauthbearer.
local function oauthbearer(self, token, _realm, authzid)
	if not token or token == "" then
		-- Empty token is the XEP-0493 probe: answer with the discovery
		-- URL so the client knows where to get a real token.
		return oauth_error("invalid_token");
	end

	local ret, err = async.wait_for(http_client:request(validation_endpoint, {
		headers = {
			["Authorization"] = "Bearer " .. token;
			["Accept"] = "application/json";
		};
	}));
	if err then
		module:log("debug", "oauth validation request failed: %s", err);
		return oauth_error("server_error");
	end
	local response = ret.body and json.decode(ret.body);
	if not (ret.code >= 200 and ret.code < 300) then
		return oauth_error("invalid_token");
	end
	if type(response) ~= "table" or type(response[username_field]) ~= "string" then
		module:log("warn", "oauth validation response lacks username field %q", username_field);
		return oauth_error("server_error");
	end

	local username = nodeprep(response[username_field]);
	if not username or username == "" then
		module:log("warn", "oauth identity %q is not a usable XMPP localpart",
			tostring(response[username_field]));
		return oauth_error("invalid_token");
	end
	if not allow_authzid and authzid and authzid ~= "" then
		local wanted = jid.node(authzid) or authzid;
		if wanted ~= username then
			module:log("warn", "oauth authzid %q does not match identity %q",
				authzid, username);
			return oauth_error("invalid_token");
		end
	end

	self.token_info = response;
	return username, true, response;
end

local provider = { name = "snikket" };

function provider.get_sasl_handler(session)
	local handler = backend.get_sasl_handler(session);
	local profile = handler.profile;
	-- internal_hashed already wires oauthbearer to mod_tokenauth so that
	-- Prosody-issued OAuth2 tokens authenticate. Keep that path as a
	-- fallback: external IdP tokens are validated first, then locally
	-- issued tokens get their chance. Only the external challenge
	-- carries the IdP discovery URL for XEP-0493 clients.
	local backend_oauthbearer = profile.oauthbearer;
	profile.oauthbearer = function (self, token, realm, authzid)
		local username, state, info = oauthbearer(self, token, realm, authzid);
		if username and state then
			return username, state, info;
		end
		if token and token ~= "" and backend_oauthbearer then
			local b_username, b_state, b_info = backend_oauthbearer(self, token, realm, authzid);
			if b_username and b_state then
				return b_username, b_state, b_info;
			end
		end
		return username, state, info;
	end;
	-- Mechanisms are cached on the profile, so drop the cache to let
	-- the new backend method be picked up.
	profile.mechanisms = nil;
	return sasl.new(handler.realm, profile, handler.userdata);
end

module:provides("auth", setmetatable(provider, {
	__index = function (_, key)
		return backend[key];
	end;
}));

module:log("info", "snikket auth provider ready (backend: %s, oauth userinfo: %s)",
	backend_name, validation_endpoint);
