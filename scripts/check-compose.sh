#!/bin/bash
set -euo pipefail

fifo_dir=$(mktemp -d)
trap 'rm -rf "$fifo_dir"' EXIT
export FRAMEOPS_MINIO_ROOT_USER_FIFO="$fifo_dir/minio-root-user"
export FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO="$fifo_dir/minio-root-password"
mkfifo "$FRAMEOPS_MINIO_ROOT_USER_FIFO" "$FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO"

rendered=$(docker compose --env-file .env.example config --format json)
docker compose --env-file .env.example config --quiet

RENDERED_COMPOSE="$rendered" python3 - <<'PY'
import json
import os
import re
import sys
from decimal import Decimal, InvalidOperation

config = json.loads(os.environ["RENDERED_COMPOSE"])
errors = []
compose_source = open("compose.yaml").read()
expected_fifo_sources = {
    "/run/secrets/minio-root-user": "FRAMEOPS_MINIO_ROOT_USER_FIFO",
    "/run/secrets/minio-root-password": "FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO",
}


def validate_resources(service_name, service):
    resources = service.get("deploy", {}).get("resources", {})
    expected = {
        "limits": {"cpus": Decimal("1.00"), "memory": 1024**3},
        "reservations": {"cpus": Decimal("0.25"), "memory": 512 * 1024**2},
    }
    actual = {}
    for group, fields in expected.items():
        actual[group] = {}
        values = resources.get(group, {})
        for field, wanted in fields.items():
            path = f"services.{service_name}.deploy.resources.{group}.{field}"
            value = values.get(field)
            try:
                normalized = Decimal(str(value)) if field == "cpus" else int(value)
            except (InvalidOperation, TypeError, ValueError):
                normalized = None
            actual[group][field] = normalized
            if normalized != wanted:
                display = "1.00" if wanted == 1 else "0.25" if field == "cpus" else "1G" if wanted == 1024**3 else "512M"
                errors.append(f"{path} must equal {display!r}")
    for field in ("cpus", "memory"):
        reservation = actual["reservations"][field]
        limit = actual["limits"][field]
        if reservation is not None and limit is not None and reservation > limit:
            errors.append(f"services.{service_name}.deploy.resources.reservations.{field} must not exceed its limit")


for target, variable in expected_fifo_sources.items():
    pattern = rf"(?m)^        source: \$\{{{variable}:[^}}\n]*\}}\n        target: {re.escape(target)}\n        read_only: true\n        bind:\n          create_host_path: false$"
    if not re.search(pattern, compose_source):
        errors.append(f"compose.yaml must bind {variable} read-only at {target} with create_host_path disabled")
postgres = config.get("services", {}).get("postgres")
if postgres is None:
    errors.append("missing services.postgres")
else:
    validate_resources("postgres", postgres)
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

minio = config.get("services", {}).get("minio")
if minio is None:
    errors.append("missing services.minio")
else:
    validate_resources("minio", minio)
    expected = "minio/minio:RELEASE.2025-09-07T16-13-09Z@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e"
    image = minio.get("image", "")
    if image != expected:
        errors.append(f"unexpected MinIO image: {image!r}; expected {expected!r}")
    if image.endswith(":latest") or "@sha256:" not in image:
        errors.append("MinIO must use an immutable image reference and must not use :latest")
    environment = minio.get("environment", {})
    expected_environment = {
        "MINIO_ROOT_USER_FILE": "/run/secrets/minio-root-user",
        "MINIO_ROOT_PASSWORD_FILE": "/run/secrets/minio-root-password",
    }
    if environment != expected_environment:
        errors.append("MinIO must use only MINIO_ROOT_*_FILE paths, never direct root environment variables")
    healthcheck = minio.get("healthcheck", {})
    healthcheck_test = healthcheck.get("test")
    if not isinstance(healthcheck_test, list) or len(healthcheck_test) != 2 or healthcheck_test[0] != "CMD-SHELL" or "curl -f http://localhost:9000/minio/health/live" not in healthcheck_test[1]:
        errors.append("MinIO healthcheck must use its local live-health endpoint")
    ports = minio.get("ports", [])
    if not isinstance(ports, list) or len(ports) != 1:
        errors.append("MinIO must expose exactly one localhost S3 API port mapping")
    elif not isinstance(ports[0], dict) or not (
        str(ports[0].get("published", "")) == "9000"
        and str(ports[0].get("target", "")) == "9000"
        and str(ports[0].get("host_ip", "")) == "127.0.0.1"
        and ports[0].get("protocol", "tcp") == "tcp"
    ):
        errors.append("MinIO must map 127.0.0.1:9000 to container TCP port 9000")
    mounts = minio.get("volumes", [])
    expected_fifo_mounts = {
        "/run/secrets/minio-root-user": os.environ["FRAMEOPS_MINIO_ROOT_USER_FIFO"],
        "/run/secrets/minio-root-password": os.environ["FRAMEOPS_MINIO_ROOT_PASSWORD_FIFO"],
    }
    if not isinstance(mounts, list) or len(mounts) != 3:
        errors.append("MinIO must mount its data volume and exactly two read-only root-secret FIFOs")
    else:
        data_mounts = [mount for mount in mounts if isinstance(mount, dict) and mount.get("target") == "/data"]
        if len(data_mounts) != 1 or not (
            data_mounts[0].get("type") == "volume"
            and data_mounts[0].get("source") == "frameops-minio-data"
        ):
            errors.append("MinIO must mount frameops-minio-data at /data")
        fifo_mounts = {mount.get("target"): mount for mount in mounts if isinstance(mount, dict) and mount.get("target") in expected_fifo_mounts}
        if set(fifo_mounts) != set(expected_fifo_mounts) or any(
            mount.get("type") != "bind"
            or mount.get("source") != expected_fifo_mounts[target]
            or mount.get("read_only") is not True
            for target, mount in fifo_mounts.items()
        ):
            errors.append("MinIO root-secret FIFOs must be exclusive read-only bind mounts")

if "frameops-minio-data" not in config.get("volumes", {}):
    errors.append("missing volumes.frameops-minio-data")

api = config.get("services", {}).get("api", {})
if api.get("depends_on", {}).get("migrate", {}).get("condition") != "service_completed_successfully":
    errors.append("services.api.depends_on.migrate.condition must equal service_completed_successfully")
api_ports = api.get("ports", [])
if not isinstance(api_ports, list) or len(api_ports) != 1 or not isinstance(api_ports[0], dict) or not (
    str(api_ports[0].get("target", "")) == "8080"
    and api_ports[0].get("host_ip", "") == "127.0.0.1"
    and api_ports[0].get("protocol", "tcp") == "tcp"
):
    errors.append("services.api must expose exactly one loopback TCP port mapping to target 8080")

web_build_args = config.get("services", {}).get("web", {}).get("build", {}).get("args")
if web_build_args != {
    "FRAMEOPS_API_URL": "http://api:8080",
    "FRAMEOPS_COMPOSE_INTERNAL_API": "1",
}:
    errors.append("services.web.build.args must pin the Compose internal API origin and marker")
web_environment = config.get("services", {}).get("web", {}).get("environment")
if web_environment != {
    "FRAMEOPS_API_URL": "http://api:8080",
    "FRAMEOPS_COMPOSE_INTERNAL_API": "1",
}:
    errors.append("services.web.environment must pin the Compose internal API origin and marker")

if errors:
    print("Compose contract check failed:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    sys.exit(1)
PY
