#!/usr/bin/env bash
# A5 真实数据库故障注入矩阵。容器只绑定 127.0.0.1 随机端口，退出时始终回收。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
POSTGRES_IMAGE="${VASTPLAN_A5_POSTGRES_IMAGE:-postgres:17.10}"
MYSQL_IMAGE="${VASTPLAN_A5_MYSQL_IMAGE:-mysql:8.0.42}"
RUN_ID="${PPID}-$$"
POSTGRES_CONTAINER="vastplan-a5-postgresql-${RUN_ID}"
MYSQL_CONTAINER="vastplan-a5-mysql-${RUN_ID}"
PASSWORD="vastplan-a5-ephemeral"

reserve_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

POSTGRES_PORT=""
MYSQL_PORT=""

cleanup() {
  docker rm -f "$POSTGRES_CONTAINER" "$MYSQL_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

if ! command -v docker >/dev/null 2>&1; then
  echo "A5 需要 Docker CLI" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "A5 需要 Python 3 分配本机临时端口" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon 未运行" >&2
  exit 1
fi

for _ in $(seq 1 5); do
  POSTGRES_PORT="$(reserve_port)"
  if docker run -d --name "$POSTGRES_CONTAINER" \
    --label cn.vastplan.test=a5-database-fault-matrix \
    -e POSTGRES_USER=vastplan -e POSTGRES_PASSWORD="$PASSWORD" -e POSTGRES_DB=vastplan \
    -p "127.0.0.1:${POSTGRES_PORT}:5432" "$POSTGRES_IMAGE" >/dev/null 2>&1; then
    break
  fi
  docker rm -f "$POSTGRES_CONTAINER" >/dev/null 2>&1 || true
  POSTGRES_PORT=""
done
if [[ -z "$POSTGRES_PORT" ]]; then
  echo "无法为临时 PostgreSQL 分配本机端口" >&2
  exit 1
fi

for _ in $(seq 1 5); do
  MYSQL_PORT="$(reserve_port)"
  if [[ "$MYSQL_PORT" == "$POSTGRES_PORT" ]]; then
    MYSQL_PORT=""
    continue
  fi
  if docker run -d --name "$MYSQL_CONTAINER" \
    --label cn.vastplan.test=a5-database-fault-matrix \
    -e MYSQL_USER=vastplan -e MYSQL_PASSWORD="$PASSWORD" -e MYSQL_DATABASE=vastplan \
    -e MYSQL_ROOT_PASSWORD="$PASSWORD" \
    -p "127.0.0.1:${MYSQL_PORT}:3306" "$MYSQL_IMAGE" >/dev/null 2>&1; then
    break
  fi
  docker rm -f "$MYSQL_CONTAINER" >/dev/null 2>&1 || true
  MYSQL_PORT=""
done
if [[ -z "$MYSQL_PORT" ]]; then
  echo "无法为临时 MySQL 分配本机端口" >&2
  exit 1
fi

for _ in $(seq 1 90); do
  if docker exec "$POSTGRES_CONTAINER" pg_isready -U vastplan -d vastplan >/dev/null 2>&1 && \
     docker exec -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysqladmin ping -uvastplan --silent >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$POSTGRES_CONTAINER" pg_isready -U vastplan -d vastplan >/dev/null
docker exec -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysqladmin ping -uvastplan --silent >/dev/null

cd "$ROOT"
VASTPLAN_TEST_POSTGRESQL_ENDPOINT="127.0.0.1:${POSTGRES_PORT}" \
VASTPLAN_TEST_POSTGRESQL_USER=vastplan \
VASTPLAN_TEST_POSTGRESQL_PASSWORD="$PASSWORD" \
VASTPLAN_TEST_POSTGRESQL_DATABASE=vastplan \
VASTPLAN_TEST_POSTGRESQL_TLS_MODE=disable \
VASTPLAN_TEST_POSTGRESQL_FAULT_CONTAINER="$POSTGRES_CONTAINER" \
VASTPLAN_TEST_MYSQL_ENDPOINT="127.0.0.1:${MYSQL_PORT}" \
VASTPLAN_TEST_MYSQL_USER=vastplan \
VASTPLAN_TEST_MYSQL_PASSWORD="$PASSWORD" \
VASTPLAN_TEST_MYSQL_DATABASE=vastplan \
VASTPLAN_TEST_MYSQL_TLS_MODE=disable \
VASTPLAN_TEST_MYSQL_FAULT_CONTAINER="$MYSQL_CONTAINER" \
go test -count=1 -timeout=8m \
  -run 'Test(PostgreSQL|MySQL)Provider(Integration|FaultMatrix)$' \
  ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime
