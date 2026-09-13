local function split(s)
	local v = {};
	for p in (s or ""):gmatch("[^%s,]+") do
		v[#v+1] = p;
	end
	return v;
end

local DOMAIN = Lua.assert(ENV_SNIKKET_DOMAIN, "Please set the SNIKKET_DOMAIN environment variable")

local RETENTION_DAYS = Lua.tonumber(ENV_SNIKKET_RETENTION_DAYS) or 7;
local UPLOAD_STORAGE_GB = Lua.tonumber(ENV_SNIKKET_UPLOAD_STORAGE_GB);
local DAILY_UPLOAD_LIMIT_PER_USER_GB = Lua.tonumber(ENV_SNIKKET_DAILY_UPLOAD_LIMIT_PER_USER_GB);

local LDAP_ENABLED = ENV_SNIKKET_TWEAK_LDAP == "1";

if Lua.prosody.process_type == "prosody" and not Lua.prosody.config_loaded then
	-- Wait at startup for certificates
	local lfs, socket = Lua.require "lfs", Lua.require "socket";
	local cert_path = "/etc/prosody/certs/"..DOMAIN..".crt";
	local counter = 0;
	while not lfs.attributes(cert_path, "mode") do
		counter = counter + 1;
		if counter == 1 or counter%6 == 0 then
			Lua.print("Waiting for certificates...");
		elseif counter > 60 then
			Lua.print("No certificates found... exiting");
			Lua.os.exit(1);
		end
		socket.sleep(5);
	end
	Lua._G.ltn12 = Lua.require "ltn12";
end

network_backend = "epoll"

plugin_paths = { "/etc/prosody/modules" }

data_path = "/snikket/prosody"

pidfile = "/var/run/prosody/prosody.pid"

admin_shell_prompt = ("prosody [%s]> "):format(DOMAIN)

modules_enabled = {

	-- Generally required
		"roster"; -- Allow users to have a roster. Recommended ;)
		"saslauth"; -- Authentication for clients and servers. Recommended if you want to log in.
		"tls"; -- Add support for secure TLS on c2s/s2s connections
		"disco"; -- Service discovery

	-- Not essential, but recommended
		"carbons"; -- Keep multiple clients in sync
		"pep"; -- Enables users to publish their avatar, mood, activity, playing music and more
		"blocklist"; -- Allow users to block communications with other users
		"vcard4"; -- User profiles (stored in PEP)
		"vcard_legacy"; -- Conversion between legacy vCard and PEP Avatar, vcard
		"password_policy";

	-- Nice to have
		"uptime"; -- Report how long server has been running
		"time"; -- Let others know the time here on this server
		"ping"; -- Replies to XMPP pings with pongs
		"register"; -- Allow users to register on this server using a client and change passwords
		"register_limits"; -- Rate limits on account creation (core module, no link needed)
		"mam"; -- Store messages in an archive and allow users to access it
		"csi_simple"; -- Simple Mobile optimizations

	-- SASL2/FAST
		"sasl2";
		"sasl2_bind2";
		"sasl2_sm";
		"sasl2_fast";
		"client_management";
		"sasl_ssdp"; -- XEP-0474: SASL mechanism/channel-binding downgrade protection

	-- Event auditing
		"audit";
		"audit_auth"; -- Audit authentication attempts and new clients
		"audit_status"; -- Audit status changes of the server (start, stop, crash)
		"audit_user_accounts"; -- Audit status changes of user accounts (created, deleted, etc.)

	-- Push notifications
		"cloud_notify";
		"cloud_notify_extensions";
		"push2";

	-- HTTP modules
		"bosh"; -- Enable BOSH clients, aka "Jabber over HTTP"
		"websocket"; -- XMPP over WebSockets
		"http_host_status_check"; -- Health checks over HTTP
		"http_xep227";

	-- Other specific functionality
		"limits"; -- Enable bandwidth limiting for XMPP connections
		"watchregistrations"; -- Alert admins of registrations
		"proxy65"; -- Enables a file transfer proxy service which clients behind NAT can use
		"smacks";
		"email";
		"http_altconnect";
		"bookmarks";
		"update_check";
		"update_notify";
		"admin_shell";
		"snikket_client_id";
		"snikket_ios_preserve_push";
		"snikket_restricted_users";
		"admin_blocklist";
		"snikket_server_vcard";
		"snikket_version"; -- Replies to server version requests
		"account_activity";
		"migrate_lastlog2"; -- Automatically migrate data from mod_lastlog2 if necessary
		"protect_last_admin";
		"c2s_limit_sessions";
		"snikket_tombstones"; -- XEP-0424: replace retracted MAM entries with tombstones
		"snikket_data_policy"; -- XEP-0504: publish data policy via disco

	-- Spam/abuse management
		"spam_reporting"; -- Allow users to report spam/abuse
		"watch_spam_reports"; -- Alert admins of spam/abuse reports by users
		"server_contact_info"; -- Publish contact information for this service

	-- TODO...
		--"groups"; -- Shared roster support
		--"announce"; -- Send announcement to all online users
		--"motd"; -- Send a message to users when they log in
		"http_files"; -- Serve static files from a directory over HTTP

	-- Invites
		"invites";
		"invites_adhoc";
		"invites_api";
		"invites_groups";
		"invites_register";
		"invites_register_api";
		"invites_tracking";
		"invites_default_group";
		"invites_bootstrap";
		"snikket_invites_quota"; -- Per-user daily quota on contact invite creation

		"firewall";

	-- Circles
		"groups_internal";
		"groups_migration";
		"groups_muc_bookmarks";

	-- For the web portal
		"http_oauth2";
		"http_admin_api";
		"rest";
		"snikket_audit_api";
		"snikket_muc_api";
		"snikket_ops_api";

	-- Monitoring & maintenance
		"measure_process";
		"measure_active_users";
		"measure_lua";
		-- measure_malloc needs LuaJIT meminfo. Disabled for Alpine/PUC-Rio Lua.
}

max_resources = Lua.tonumber(ENV_SNIKKET_MAX_USER_CLIENTS) or 10

registration_watchers = {} -- Disable by default
registration_notification = "New user registered: $username"

contact_info = {
	abuse = ENV_SNIKKET_ABUSE_EMAIL and {"mailto:"..ENV_SNIKKET_ABUSE_EMAIL} or nil;
	security = ENV_SNIKKET_SECURITY_EMAIL and {"mailto:"..ENV_SNIKKET_SECURITY_EMAIL} or nil;
}

http_ports  = { ENV_SNIKKET_TWEAK_INTERNAL_HTTP_PORT or 5280 }
http_interfaces = split(ENV_SNIKKET_TWEAK_INTERNAL_HTTP_INTERFACE or "127.0.0.1,::1")

-- The web portal reverse-proxies /xmpp-websocket, /http-bind and host-meta
-- from the TLS edge. Treat those transport hops as secure and honor the
-- X-Forwarded-* headers the proxy sets (docker bridge subnet).
consider_websocket_secure = true
consider_bosh_secure = true
trusted_proxies = { "172.16.0.0/12", "10.0.0.0/8" }
http_max_content_size = 1024 * 1024 -- non-streaming uploads limited to 1MB (improves RAM usage)

https_ports = {};

c2s_direct_tls_ports = { 5223 }

tls_profile = ENV_SNIKKET_TLS_PROFILE or "intermediate"
tls_profile_version = ENV_SNIKKET_TLS_PROFILE_VERSION or "5.7"

proxy65_ports = { ENV_SNIKKET_PROXY65_PORT or 5000 }

allow_registration = true
registration_invite_only = true

password_policy = {
	length = 10;
}

-- In the future we want to switch to SASL2 for better security,
-- as client ids are not supported in SASL1 (identification is via
-- the resource string, which is semi-public and not authenticated)
-- This tweak is for developers to test with the future configuration,
-- or people who want to opt into the new security sooner.
enforce_client_ids = ENV_SNIKKET_TWEAK_REQUIRE_SASL2 == "1"

-- In-app contact invites for regular users (XEP-0401). Disabled by default:
-- open invite creation can be abused to mass-invite contacts, so it is
-- limited by a per-user daily quota enforced by mod_snikket_invites_quota.
allow_contact_invites = (ENV_SNIKKET_TWEAK_CONTACT_INVITES == "1")

-- Whether regular users may create invites that can register new accounts.
-- Disabled by default: every such invite can create a real account.
allow_user_invites = (ENV_SNIKKET_TWEAK_USER_INVITES == "1")

-- Max contact invites a single user may create per UTC day (admins exempt)
invites_daily_limit = Lua.tonumber(ENV_SNIKKET_TWEAK_CONTACT_INVITES_PER_DAY) or 20

-- Disallow restricted users to create invitations to the server
deny_user_invites_by_roles = { "prosody:restricted" }

-- This role was renamed 'guest' in Prosody.
custom_roles = { { name = "prosody:restricted"; priority = 15 } }

invites_page = ENV_SNIKKET_INVITE_URL or ("https://"..DOMAIN.."/invite/{invite.token}/");
invites_page_supports = { "account", "contact", "account-and-contact", "password-reset" }

invites_bootstrap_index = Lua.tonumber(ENV_TWEAK_SNIKKET_BOOTSTRAP_INDEX)
invites_bootstrap_secret = ENV_TWEAK_SNIKKET_BOOTSTRAP_SECRET
invites_bootstrap_ttl = Lua.tonumber(ENV_TWEAK_SNIKKET_BOOTSTRAP_TTL or (28 * 86400)) -- default 28 days

-- Enable MUC integration for mod_groups_internal
groups_muc_host = "groups."..DOMAIN

-- The Resource Owner Credentials grant used internally between the web portal
-- and Prosody, so ensure this is enabled. Other unused flows can be disabled.
allowed_oauth2_grant_types = { "password" }
allowed_oauth2_response_types = {}

-- Longer access token lifetime than the default
-- TODO: Use the already longer-lived refresh tokens
oauth2_access_token_ttl = 86400

oauth2_registration_key = FileLine("/snikket/prosody/oauth2-registration-secret")

c2s_require_encryption = true
s2s_require_encryption = true
s2s_secure_auth = true

-- Grant federation privileges to regular users but not restricted users.
-- This is enforced by mod_isolate_host.
add_permissions = {
	["prosody:registered"] = {
		"xmpp:federate";
	};
}

archive_expires_after = ("%dd"):format(RETENTION_DAYS) -- Remove archived messages after N days

-- Push notifications carry only a wake-up ping (message-count) to the push
-- service; iOS clients that register an encryption key additionally get an
-- AES-128-GCM-encrypted summary they decrypt locally. Never include sender
-- or content in the clear.
push_notification_with_body = false
push_notification_with_sender = false

-- XEP-0504 data policy advertised in service discovery
data_policy = {
	auth_data = "hidden"; -- SCRAM: the server never sees plaintext passwords
	data_transmission = "encrypted";
	encryption_algorithm = "TLS";
	data_retention = Lua.tostring(RETENTION_DAYS * 24); -- hours
	data_deletion = true; -- retraction, MAM expiry, account deletion
	encryption_at_rest = false; -- volumes are not encrypted by default
	tos = "https://"..DOMAIN.."/";
	data_export = true; -- XEP-0227 export via http_xep227 and the portal
	access_policy = { "admins" };
	full_erasure = true;
}

-- This is required for Conversations 2.19 to receive offline messages, which
-- we currently utilize to attempt at-least-once delivery for messages beyond
-- the archive retention period.
-- Eventually it will be removed when mod_offline is removed in favour of
-- smarter retention strategies.
send_legacy_offline_messages_to_mam_clients = true

-- Delay full account deletion via IBR for RETENTION_DAYS, to allow restoration
-- in case of accidental or malicious deletion of an account
registration_delete_grace_period = ("%d days"):format(RETENTION_DAYS)

-- Allow disabling IPv6 because Docker does not have it enabled by default, but
-- we don't use Docker networking so it should not matter.
use_ipv6 = (ENV_SNIKKET_TWEAK_IPV6 ~= "0")

log = {
	[ENV_SNIKKET_LOGLEVEL or "info"] = "*stdout"
}

authorization = "internal"
allow_unencrypted_plain_auth = false

if LDAP_ENABLED then
	-- mod_auth_ldap2 only supports SASL PLAIN over an LDAP simple bind:
	-- passwords reach the server in plaintext and are protected only by
	-- TLS (c2s_require_encryption stays enabled). PLAIN must not be
	-- disabled, and the SCRAM storage options do not apply to LDAP.
	authentication = "ldap2"
	disable_sasl_mechanisms = { "OAUTHBEARER" }
else
	authentication = "internal_hashed"
	disable_sasl_mechanisms = { "PLAIN", "OAUTHBEARER" }

	-- SCRAM hash used for stored credentials. SHA-256 is recommended for new
	-- deployments, but it MUST be set before the first account is created:
	-- stored keys are hash-specific, so changing it on an existing host
	-- invalidates every password (users would need a reset).
	password_hash = ENV_SNIKKET_TWEAK_PASSWORD_HASH or "SHA-1"
	default_iteration_count = Lua.tonumber(ENV_SNIKKET_TWEAK_ITERATION_COUNT) or 10000
end

if ENV_SNIKKET_TWEAK_STORAGE == "sqlite" then
	storage = "sql"
	sql = {
		driver = "SQLite3";
		database = "/snikket/prosody/prosody.sqlite";
	}
else
	storage = "internal"
end

statistics = "internal"

if ENV_SNIKKET_TWEAK_PROMETHEUS == "1" then
	-- TODO rename to OPENMETRICS
	-- When using Prometheus, it is desirable to let the prometheus scraping
	-- drive the sampling of metrics
	statistics_interval = "manual"
else
	-- When not using Prometheus, we need an interval so that the metrics can
	-- be shown by the web portal. The HTTP admin API exposure does not force
	-- a collection as it is only interested in very few specific metrics.
	statistics_interval = 60
end

if ENV_SNIKKET_TWEAK_DNSSEC == "1" then
	use_dnssec = true

	local trustfile = "/usr/share/dns/root.ds"; -- Requires apt:dns-root-data
	-- Bail out if it doesn't work
	Lua.assert(Lua.require"lunbound".new{ resolvconf = true; trustfile = trustfile }:resolve ".".secure,
		"Upstream DNS resolver is not DNSSEC-capable. Fix this or disable SNIKKET_TWEAK_DNSSEC");
	unbound = { trustfile = trustfile }

	-- Since we have DNSSEC, we can also do DANE (unless disabled)
	use_dane = ENV_SNIKKET_TWEAK_DANE ~= "0"
else
	use_dnssec = false
	use_dane = false
end

certificates = "certs"

group_default_name = ENV_SNIKKET_SITE_NAME or DOMAIN

site_name = ENV_SNIKKET_SITE_NAME or DOMAIN
site_logo = "/usr/local/share/snikket/logo.png"

-- Update check configuration
software_name = "SnikketX"
update_notify_version_url = "https://snikket.org/updates/{branch}/{version}"
update_notify_support_url = "https://snikket.org/notices/{branch}/"
update_notify_message_url = "https://snikket.org/notices/{branch}/{message}"

if ENV_SNIKKET_UPDATE_CHECK ~= "0" then
	update_check_dns = "_{branch}.update.snikket.net"
	update_check_interval = 21613 -- ~6h
end

http_default_host = DOMAIN
http_host = DOMAIN
http_external_url = "https://"..DOMAIN.."/"

if ENV_SNIKKET_TWEAK_TURNSERVER ~= "0" or ENV_SNIKKET_TWEAK_TURNSERVER_DOMAIN then
	modules_enabled: append {
		"turn_external";
	}
	turn_external_host = ENV_SNIKKET_TWEAK_TURNSERVER_DOMAIN or DOMAIN
	turn_external_port = ENV_SNIKKET_TWEAK_TURNSERVER_PORT
	turn_external_secret = ENV_SNIKKET_TWEAK_TURNSERVER_SECRET or FileLine("/snikket/prosody/turn-auth-secret-v2")
	turn_external_tcp = true
end

-- Allow restricted users access to push notification servers
isolate_except_domains = { "push.snikket.net", "push-ios.snikket.net", "push.quad4.io" }

VirtualHost (DOMAIN)
	contact_uri = "https://" .. DOMAIN .. "/"

	if LDAP_ENABLED then
		authentication = "ldap2"
		-- Accounts come from the directory, so invite-based account
		-- registration is not meaningful when LDAP auth is enabled.
		-- hostname may be a plain host, host:port or an ldaps:// URI.
		local ldap_config = {
			hostname = Lua.assert(ENV_SNIKKET_LDAP_HOST, "SNIKKET_LDAP_HOST is required when SNIKKET_TWEAK_LDAP=1")
				.. (ENV_SNIKKET_LDAP_PORT and (":"..ENV_SNIKKET_LDAP_PORT) or "");
			bind_dn = ENV_SNIKKET_LDAP_BIND_DN;
			bind_password = ENV_SNIKKET_LDAP_BIND_PASSWORD
				or (ENV_SNIKKET_LDAP_BIND_PASSWORD_FILE and FileLine(ENV_SNIKKET_LDAP_BIND_PASSWORD_FILE));
			use_tls = (ENV_SNIKKET_LDAP_USE_TLS == "1");
			user = {
				basedn = ENV_SNIKKET_LDAP_USER_BASE_DN;
				usernamefield = ENV_SNIKKET_LDAP_USERNAME_FIELD or "uid";
				namefield = ENV_SNIKKET_LDAP_NAME_FIELD or "cn";
				filter = ENV_SNIKKET_LDAP_USER_FILTER;
			};
		}
		if ENV_SNIKKET_LDAP_GROUP_BASE_DN then
			ldap_config.groups = {
				basedn = ENV_SNIKKET_LDAP_GROUP_BASE_DN;
				memberfield = ENV_SNIKKET_LDAP_GROUP_MEMBER_FIELD or "member";
				namefield = "cn";
			}
		end
		ldap = ldap_config
		-- groups_migration enumerates local accounts, which do not exist
		-- under LDAP auth
		modules_disabled = { "groups_migration" }
	else
		authentication = "internal_hashed"
	end

	modules_enabled = {
		"snikket_badinage";
	}
	firewall_scripts = {}

	http_files_dir = "/var/www"
	http_paths = {
		files = "/";
		landing_page = "/";
		invites_page = "/invite";
		invites_register = "/register";
		badinage = "/chat";
	}

	if ENV_SNIKKET_TWEAK_PROMETHEUS == "1" then
		modules_enabled: append {
			"http_openmetrics";
		}
	end

	if ENV_SNIKKET_TWEAK_S2S_STATUS == "1" then
		modules_enabled: append {
			"s2s_status";
		}
	end

	if ENV_SNIKKET_TWEAK_SHARE_PROXY == "1" then
		modules_enabled: append {
			"http_connect";
		}
		-- Reuse the same secret we use for TURN
		http_proxy_secret = ENV_SNIKKET_TWEAK_TURNSERVER_SECRET or FileLine("/snikket/prosody/turn-auth-secret-v2")
	end

	if ENV_SNIKKET_TWEAK_RESTRICTED_USERS_V2 == "1" then
		firewall_scripts: append {
			"/etc/prosody/firewall/restricted_users.pfw";
		}
	else
		modules_enabled: append {
			"restrict_federation";
		}
	end


Component ("groups."..DOMAIN) "muc"
	modules_enabled = {
		"muc_mam";
		"muc_mam_hints"; -- Honor XEP-0334 no-store hints in group archives
		"muc_moderation";
		"muc_local_only";
		"muc_defaults";
		"muc_offline_delivery";
		"snikket_restricted_users";
		"snikket_deprecate_general_muc";
		"muc_auto_reserve_nicks";
	}

	if ENV_SNIKKET_TWEAK_S2S_STATUS == "1" then
		modules_enabled: append {
			"s2s_status";
		}
	end

	if ENV_SNIKKET_TWEAK_GC3 == "1" then
		muc_enable_experimental_gc3 = true;
	end

	restrict_room_creation = "local"
	muc_log_expires_after = ("%dd"):format(RETENTION_DAYS) -- Match personal archive retention

	-- Some older deployments may have the general@ MUC, so we still need
	-- to protect it:
	muc_local_only = { "general@groups."..DOMAIN }

	-- Default configuration for rooms (typically overwritten by the client)
	muc_room_default_allow_member_invites = true
	muc_room_default_persistent = true
	muc_room_default_public = false

	-- Enable push notifications for offline group members by default
	-- (this also requires mod_muc_auto_reserve_nicks in practice)
	muc_offline_delivery_default = true
	-- Include form in MUC registration query result (required for app
	-- to detect whether push notifications are enabled)
	muc_registration_include_form = true


Component ("share."..DOMAIN) "http_file_share"
	-- For backwards compat, allow HTTP upload on the base domain
	if ENV_SNIKKET_TWEAK_SHARE_DOMAIN ~= "1" then
		http_host = "share."..DOMAIN
		http_external_url = "https://share."..DOMAIN.."/"
	end

	-- 128 bits (i.e. 16 bytes) is the maximum length of a GCM auth tag, which
	-- is appended to encrypted uploads according to XEP-0454. This ensures we
	-- allow files up to the size limit even if they are encrypted.
	http_file_share_size_limit = (1024 * 1024 * 100) + 16 -- 100MB + 16 bytes
	http_file_share_expires_after = 60 * 60 * 24 * RETENTION_DAYS -- N days
	if DAILY_UPLOAD_LIMIT_PER_USER_GB then
		http_file_share_daily_quota = 1024 * 1024 * 1024 * DAILY_UPLOAD_LIMIT_PER_USER_GB
	end
	if UPLOAD_STORAGE_GB then
		http_file_share_global_quota = 1024 * 1024 * 1024 * UPLOAD_STORAGE_GB
	end
	http_paths = {
		file_share = "/upload"
	}

	modules_disabled = {
		"s2s";
	}

Include (ENV_SNIKKET_TWEAK_EXTRA_CONFIG or "/snikket/prosody/*.cfg.lua")
