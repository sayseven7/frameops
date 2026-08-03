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
    healthcheck = postgres.get("healthcheck", {})
    healthcheck_test = healthcheck.get("test")
    if not isinstance(healthcheck_test, list) or len(healthcheck_test) != 2 or healthcheck_test[0] != "CMD-SHELL" or "pg_isready" not in healthcheck_test[1]:
        errors.append("healthcheck must run pg_isready through CMD-SHELL")
    image = postgres.get("image", "")
    expected = "postgres:18.4@sha256:3a82e1f56c8f0f5616a11103ac3d47e632c3938698946a7ad26da0df1334744a"
    if image != expected:
        errors.append(f"unexpected PostgreSQL image: {image!r}; expected {expected!r}")
    if image.endswith(":latest"):
        errors.append("PostgreSQL image must not use :latest")
    if "@sha256:" not in image:
        errors.append("PostgreSQL image must use an immutable sha256 digest")
    ports = postgres.get("ports", [])
    if not isinstance(ports, list) or len(ports) != 1:
        errors.append("PostgreSQL must expose exactly one localhost port mapping")
    elif not isinstance(ports[0], dict) or not (
        str(ports[0].get("published", "")) == "5432"
        and str(ports[0].get("target", "")) == "5432"
        and str(ports[0].get("host_ip", "")) == "127.0.0.1"
        and ports[0].get("protocol", "tcp") == "tcp"
    ):
        errors.append("PostgreSQL must map 127.0.0.1:5432 to container TCP port 5432")
    mounts = postgres.get("volumes", [])
    if not isinstance(mounts, list) or len(mounts) != 1 or not isinstance(mounts[0], dict) or not (
        mounts[0].get("type") == "volume"
        and mounts[0].get("source") == "frameops-postgres-data"
        and mounts[0].get("target") == "/var/lib/postgresql"
    ):
        errors.append("PostgreSQL must mount frameops-postgres-data at /var/lib/postgresql")

if "frameops-postgres-data" not in config.get("volumes", {}):
    errors.append("missing volumes.frameops-postgres-data")

if errors:
    print("Compose contract check failed:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    sys.exit(1)
PY
