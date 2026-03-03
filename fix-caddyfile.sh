#!/bin/bash
# Fix Caddyfile with proper line breaks
# Auto-sources .env from the same directory if variables are not already exported.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load .env with set -a so every assignment is automatically exported to child processes
if [ -f "${SCRIPT_DIR}/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  source "${SCRIPT_DIR}/.env"
  set +a
fi

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
