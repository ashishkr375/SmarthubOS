#!/bin/bash
# Fix Caddyfile with proper line breaks
# ACME_EMAIL and GRAFANA_DOMAIN must be set in the .env file.

if [ -z "${ACME_EMAIL}" ]; then
  echo "ERROR: ACME_EMAIL is not set. Add it to your .env file."
  echo "Example:  ACME_EMAIL=you@example.com"
  exit 1
fi

if [ -z "${GRAFANA_DOMAIN}" ]; then
  echo "ERROR: GRAFANA_DOMAIN is not set. Add it to your .env file."
  echo "Example:  GRAFANA_DOMAIN=dashboard.yourdomain.com"
  exit 1
fi

cat > Caddyfile << ENDMARKER
{
    email ${ACME_EMAIL}
}

${GRAFANA_DOMAIN} {
    reverse_proxy grafana:3000
}
ENDMARKER

echo "Caddyfile created successfully!"
cat Caddyfile
