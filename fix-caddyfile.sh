#!/bin/bash
# Fix Caddyfile with proper line breaks

cat > Caddyfile << 'ENDMARKER'
{
    email {$ACME_EMAIL}
}

{$GRAFANA_DOMAIN} {
    reverse_proxy grafana:3000
}
ENDMARKER

echo "Caddyfile created successfully!"
cat Caddyfile
