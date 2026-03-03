#!/bin/bash
# setup-nginx-ssl.sh
# Configures system nginx as reverse proxy for Grafana and obtains a Let's Encrypt cert.
# Run once after first deployment.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load .env
if [ -f "${SCRIPT_DIR}/.env" ]; then
  set -a
  source "${SCRIPT_DIR}/.env"
  set +a
fi

if [ -z "${GRAFANA_DOMAIN:-}" ]; then
  echo "ERROR: GRAFANA_DOMAIN is not set in .env"
  exit 1
fi

if [ -z "${ACME_EMAIL:-}" ]; then
  echo "ERROR: ACME_EMAIL is not set in .env"
  exit 1
fi

echo "==> Installing certbot..."
apt-get update -qq
apt-get install -y -qq certbot python3-certbot-nginx

echo "==> Writing nginx site config for ${GRAFANA_DOMAIN}..."
sed "s/GRAFANA_DOMAIN_PLACEHOLDER/${GRAFANA_DOMAIN}/g" \
  "${SCRIPT_DIR}/nginx/grafana.conf" \
  > /etc/nginx/sites-available/smarthub-grafana

ln -sf /etc/nginx/sites-available/smarthub-grafana \
       /etc/nginx/sites-enabled/smarthub-grafana

# Remove default site if still enabled
rm -f /etc/nginx/sites-enabled/default

nginx -t
systemctl reload nginx

echo "==> Obtaining Let's Encrypt certificate for ${GRAFANA_DOMAIN}..."
certbot --nginx \
  -d "${GRAFANA_DOMAIN}" \
  --email "${ACME_EMAIL}" \
  --agree-tos \
  --non-interactive \
  --redirect

echo "==> Setting up auto-renewal..."
systemctl enable certbot.timer 2>/dev/null || \
  (crontab -l 2>/dev/null; echo "0 3 * * * certbot renew --quiet") | crontab -

echo ""
echo "Done! Grafana is now live at https://${GRAFANA_DOMAIN}"
echo "Test renewal with: certbot renew --dry-run"
