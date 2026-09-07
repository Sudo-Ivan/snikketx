#!/bin/sh
# Alpine install path that replaces the Debian ansible playbook.
set -eu

PROSODY_MODULES_REV="${PROSODY_MODULES_REV:-7851ba324048}"

install -d -m 755 /snikket
install -d -m 755 /etc/prosody/modules /etc/prosody/firewall /var/www
install -d -m 755 /usr/local/lib/prosody-modules /usr/local/share/snikket
install -d -m 755 /var/run/prosody /var/spool/anacron /etc/cron.daily /etc/sv

ln -sf /usr/bin/lua5.4 /usr/bin/lua

# Prosody package creates /etc/prosody/certs as root-owned sometimes.
install -d -m 750 -o prosody -g prosody /etc/prosody/certs
install -d -m 750 -o prosody -g prosody /snikket/prosody
rm -f /etc/prosody/certs/localhost.crt /etc/prosody/certs/localhost.key

cp /opt/snikket-build/prosody.cfg.lua /etc/prosody/prosody.cfg.lua
cp /opt/snikket-build/migrator.cfg.lua /etc/prosody/migrator.cfg.lua
cp /opt/snikket-build/restricted_users.pfw /etc/prosody/firewall/
cp /opt/snikket-build/turnserver.conf /etc/turnserver.conf
cp /opt/snikket-build/msmtp.conf /etc/msmtprc
cp /opt/snikket-build/snikket-logo.png /usr/local/share/snikket/logo.png
cp /opt/snikket-build/bin/* /usr/local/bin/
chmod 755 /usr/local/bin/*
cp /opt/snikket-build/refresh-certs.cron /etc/cron.daily/refresh-certs
chmod 555 /etc/cron.daily/refresh-certs

# Prefer msmtp over busybox sendmail.
ln -sf /usr/bin/msmtp /usr/sbin/sendmail

# Community modules snapshot.
tmpdir=$(mktemp -d)
wget -q -O "$tmpdir/modules.tar.gz" \
  "https://hg.prosody.im/prosody-modules/archive/${PROSODY_MODULES_REV}.tar.gz"
tar -xzf "$tmpdir/modules.tar.gz" -C /usr/local/lib/prosody-modules --strip-components=1
rm -rf "$tmpdir"

link_mod() {
  src="/usr/local/lib/prosody-modules/$1"
  dest="/etc/prosody/modules/$1"
  if [ -e "$src" ]; then
    ln -sfn "$src" "$dest"
  else
    echo "WW: missing community module $1" >&2
  fi
}

for m in \
  mod_cloud_notify_extensions \
  mod_cloud_notify_encrypted \
  mod_cloud_notify_priority_tag \
  mod_cloud_notify_filters \
  mod_block_registrations \
  mod_conversejs \
  mod_migrate_http_upload \
  mod_lastlog2 \
  mod_limit_auth \
  mod_password_policy \
  mod_email \
  mod_firewall \
  mod_admin_notify \
  mod_http_oauth2 \
  mod_http_admin_api \
  mod_rest \
  mod_groups_migration \
  mod_invites_api \
  mod_invites_groups \
  mod_invites_register_api \
  mod_invites_tracking \
  mod_groups_internal \
  mod_groups_muc_bookmarks \
  mod_muc_defaults \
  mod_muc_local_only \
  mod_muc_offline_delivery \
  mod_http_host_status_check \
  mod_measure_process \
  mod_spam_reporting \
  mod_watch_spam_reports \
  mod_muc_auto_reserve_nicks \
  mod_measure_active_users \
  mod_measure_lua \
  mod_http_xep227 \
  mod_sasl2 \
  mod_sasl2_bind2 \
  mod_sasl2_sm \
  mod_sasl2_fast \
  mod_client_management \
  mod_audit \
  mod_audit_auth \
  mod_audit_status \
  mod_audit_user_accounts \
  mod_s2s_status \
  mod_sasl_ssdp \
  mod_privilege \
  mod_admin_blocklist \
  mod_muc_moderation \
  mod_push2 \
  mod_migrate_lastlog2 \
  mod_http_connect \
  mod_restrict_federation \
  mod_protect_last_admin \
  mod_c2s_limit_sessions
do
  link_mod "$m"
done

for m in \
  mod_update_check \
  mod_update_notify \
  mod_invites_default_group \
  mod_invites_bootstrap \
  mod_snikket_client_id \
  mod_snikket_ios_preserve_push \
  mod_snikket_restricted_users \
  mod_snikket_deprecate_general_muc \
  mod_migrate_snikket_roles \
  mod_health_report \
  mod_snikket_server_vcard \
  mod_snikket_billing \
  mod_snikket_version
do
  ln -sfn "/usr/local/lib/snikket-modules/$m" "/etc/prosody/modules/$m"
done

# s6 services (anacron-compatible hourly runner for cron.daily).
cp -a /opt/snikket-build/services/. /etc/sv/
chmod -R 750 /etc/sv

cat > /etc/sv/anacron/run <<'EOF'
#!/bin/sh -e
while true; do
  for f in /etc/cron.daily/*; do
    [ -x "$f" ] || continue
    "$f" || true
  done
  sleep 3600
done
EOF
chmod 750 /etc/sv/anacron/run

# Prosody on Alpine is already a lua5.4 script.
cat > /etc/sv/prosody/run <<'EOF'
#!/bin/sh -e
./wait-for-certs
exec s6-setuidgid prosody /usr/bin/prosody -F
EOF
chmod 750 /etc/sv/prosody/run /etc/sv/prosody/wait-for-certs
chmod 750 /etc/sv/coturn/run /etc/sv/coturn/finish

echo "Snikket ${BUILD_SERIES:-dev} ${BUILD_ID:-0}" > /usr/lib/prosody/snikket.version
