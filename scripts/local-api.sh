#!/usr/bin/env bash
# Bring up the FeatureFlip management API from a feature-flagger monorepo checkout
# (postgres + migrations + API) and seed it for local acceptance tests.
#
# This is a standalone repo, so it can't see the monorepo — point FEATUREFLIP_REPO
# at your feature-flagger checkout, which supplies the docker-compose.yml stack.
set -euo pipefail

REPO="${FEATUREFLIP_REPO:-}"
if [ -z "$REPO" ] || [ ! -f "$REPO/docker-compose.yml" ]; then
  echo "Set FEATUREFLIP_REPO to a feature-flagger checkout (one with docker-compose.yml)." >&2
  echo "e.g. FEATUREFLIP_REPO=~/src/feature-flagger ./scripts/local-api.sh" >&2
  exit 1
fi

# local-api.override.yml flips on Features:PublicManagementApi (default false
# upstream), which gates the entire /api/v1 surface this provider talks to —
# see that file for why. It's layered in via -f and never modifies $REPO.
#
# --build guards against a stale cached image: `docker compose up -d` alone
# reuses an existing image for a service even if the checkout has moved on
# since it was built, which silently serves old routes/behavior against a
# newer client. Rebuilding is cheap when the image is already current
# (layer-cached) and is the only way to guarantee the API matches $REPO's
# checked-out commit.
docker compose -f "$REPO/docker-compose.yml" -f "$(dirname "$0")/local-api.override.yml" \
  up -d --build postgres db-migrate management-api

echo "Waiting for management-api at http://localhost:5000/health ..."
for i in $(seq 1 60); do
  if curl -fsS http://localhost:5000/health >/dev/null 2>&1; then
    exec "$(dirname "$0")/seed.sh" http://localhost:5000
  fi
  sleep 2
done
echo "management-api did not become healthy after 120s" >&2
exit 1
