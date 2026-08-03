#!/bin/bash
set -euo pipefail

rendered=$(docker compose --env-file .env.example config --format json)
docker compose --env-file .env.example config --quiet

RENDERED_COMPOSE="$rendered" python3 - <<'PY'
import json
import os
import sys

config = json.loads(os.environ["RENDERED_COMPOSE"])
errors = []
postgres = config.get("services", {}).get("postgres")
if postgres is None:
    errors.append("missing services.postgres")
else:
    if not postgres.get("healthcheck", {}).get("test"):
        errors.append("missing services.postgres.healthcheck.test")
    image = postgres.get("image", "")
    expected = "postgres:18.4@sha256:3a82e1f56c8f0f5616a11103ac3d47e632c3938698946a7ad26da0df1334744a"
    if image != expected:
        errors.append(f"unexpected PostgreSQL image: {image!r}; expected {expected!r}")
    if image.endswith(":latest"):
        errors.append("PostgreSQL image must not use :latest")
    if "@sha256:" not in image:
        errors.append("PostgreSQL image must use an immutable sha256 digest")
    ports = postgres.get("ports", [])
    if not any(str(port.get("published", "")) == "5432" and str(port.get("host_ip", "")) in {"127.0.0.1", "::1"} for port in ports if isinstance(port, dict)):
        errors.append("PostgreSQL port must bind only to localhost")

if "frameops-postgres-data" not in config.get("volumes", {}):
    errors.append("missing volumes.frameops-postgres-data")

if errors:
    print("Compose contract check failed:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    sys.exit(1)
PY
