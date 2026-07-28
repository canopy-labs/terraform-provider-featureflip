#!/usr/bin/env bash
# Seed a Featureflip instance with a fresh org + Admin service token for
# provider acceptance tests. Writes .env.acceptance in the repo root.
set -euo pipefail

API="${1:-http://localhost:5000}"
SUFFIX="$(head -c4 /dev/urandom | od -An -tx1 | tr -d ' \n')"
EMAIL="tf-acc-${SUFFIX}@example.com"
SLUG="tf-acc-${SUFFIX}"
PASSWORD="TfAcc-P4ssw0rd-${SUFFIX}"

# Sign up WITHOUT a plan: upstream rejects paid-plan signups that carry no
# billing interval (fix for canopy-labs/featureflip#1783), so the org starts
# on Solo and the DB bump below grants Business for test headroom.
signup=$(curl -fsS -X POST "$API/api/management/v1/auth/signup-with-organization" \
  -H 'Content-Type: application/json' \
  -d "{\"userName\":\"TF Acceptance\",\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"organizationName\":\"TF Acc $SUFFIX\",\"organizationSlug\":\"$SLUG\"}")
JWT=$(echo "$signup" | jq -re '.token')
ORG_ID=$(echo "$signup" | jq -re '.organizationId')

token=$(curl -fsS -X POST "$API/api/management/v1/organizations/$ORG_ID/service-tokens" \
  -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
  -d '{"name":"terraform-acceptance","role":"Admin"}')
FF_TOKEN=$(echo "$token" | jq -re '.token')

# Solo allows only 1 project / 2 envs — bump to Business in the test DB so
# acceptance tests aren't plan-limited. Override SEED_PSQL in CI (psql
# client vs docker exec).
PSQL="${SEED_PSQL:-docker exec featureflip-postgres psql -U postgres}"
$PSQL -d featureflip_management -v ON_ERROR_STOP=1 \
  -c "UPDATE organizations SET plan = 'Business' WHERE slug = '$SLUG';"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cat > "$ROOT/.env.acceptance" <<EOF
FEATUREFLIP_BASE_URL=$API
FEATUREFLIP_ORGANIZATION=$SLUG
FEATUREFLIP_TOKEN=$FF_TOKEN
EOF
echo "Seeded org '$SLUG' — wrote .env.acceptance"
