#!/usr/bin/env bash
# Platform Control 与 Database Runtime 真实数据库矩阵。
# 容器只绑定 127.0.0.1 随机端口，退出时始终回收。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
POSTGRES_IMAGE="${VASTPLAN_A5_POSTGRES_IMAGE:-postgres:17.10}"
MYSQL_IMAGE="${VASTPLAN_A5_MYSQL_IMAGE:-mysql:8.0.42}"
RUN_ID="${PPID}-$$"
POSTGRES_CONTAINER="vastplan-a5-postgresql-${RUN_ID}"
MYSQL_CONTAINER="vastplan-a5-mysql-${RUN_ID}"
PASSWORD=""
BACKUP_DIRECTORY=""

reserve_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

POSTGRES_PORT=""
MYSQL_PORT=""
POSTGRES_START_ERROR=""
MYSQL_START_ERROR=""

cleanup() {
  docker rm -f "$POSTGRES_CONTAINER" "$MYSQL_CONTAINER" >/dev/null 2>&1 || true
  if [[ -n "$BACKUP_DIRECTORY" ]]; then
    rm -f "$BACKUP_DIRECTORY/postgresql.dump" "$BACKUP_DIRECTORY/mysql.sql"
    rmdir "$BACKUP_DIRECTORY" >/dev/null 2>&1 || true
  fi
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
PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
if ! python3 -c 'import subprocess,sys
try:
    result=subprocess.run(["docker","info"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
except subprocess.TimeoutExpired:
    sys.exit(124)
sys.exit(result.returncode)'; then
  echo "Docker daemon 未在 5 秒内响应；请先恢复 Docker，再重试真实数据库矩阵" >&2
  exit 1
fi

echo "准备临时 PostgreSQL 与 MySQL 容器"
for _ in $(seq 1 5); do
  POSTGRES_PORT="$(reserve_port)"
  if POSTGRES_START_ERROR="$(docker run -d --name "$POSTGRES_CONTAINER" \
    --label cn.vastplan.test=a5-database-fault-matrix \
    -e POSTGRES_USER=vastplan -e POSTGRES_PASSWORD="$PASSWORD" -e POSTGRES_DB=vastplan \
    -p "127.0.0.1:${POSTGRES_PORT}:5432" "$POSTGRES_IMAGE" 2>&1)"; then
    break
  fi
  docker rm -f "$POSTGRES_CONTAINER" >/dev/null 2>&1 || true
  POSTGRES_PORT=""
done
if [[ -z "$POSTGRES_PORT" ]]; then
  echo "无法启动临时 PostgreSQL: ${POSTGRES_START_ERROR}" >&2
  exit 1
fi

for _ in $(seq 1 5); do
  MYSQL_PORT="$(reserve_port)"
  if [[ "$MYSQL_PORT" == "$POSTGRES_PORT" ]]; then
    MYSQL_PORT=""
    continue
  fi
  if MYSQL_START_ERROR="$(docker run -d --name "$MYSQL_CONTAINER" \
    --label cn.vastplan.test=a5-database-fault-matrix \
    -e MYSQL_USER=vastplan -e MYSQL_PASSWORD="$PASSWORD" -e MYSQL_DATABASE=vastplan \
    -e MYSQL_ROOT_PASSWORD="$PASSWORD" \
    -p "127.0.0.1:${MYSQL_PORT}:3306" "$MYSQL_IMAGE" 2>&1)"; then
    break
  fi
  docker rm -f "$MYSQL_CONTAINER" >/dev/null 2>&1 || true
  MYSQL_PORT=""
done
if [[ -z "$MYSQL_PORT" ]]; then
  echo "无法启动临时 MySQL: ${MYSQL_START_ERROR}" >&2
  exit 1
fi

for _ in $(seq 1 90); do
  if docker exec "$POSTGRES_CONTAINER" pg_isready -U vastplan -d vastplan >/dev/null 2>&1 && \
     docker exec -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysqladmin ping -uvastplan --silent >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! docker exec "$POSTGRES_CONTAINER" pg_isready -U vastplan -d vastplan >/dev/null 2>&1; then
  echo "临时 PostgreSQL 未在 90 秒内就绪" >&2
  docker logs --tail 80 "$POSTGRES_CONTAINER" >&2
  exit 1
fi
if ! docker exec -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysqladmin ping -uvastplan --silent >/dev/null 2>&1; then
  echo "临时 MySQL 未在 90 秒内就绪" >&2
  docker logs --tail 80 "$MYSQL_CONTAINER" >&2
  exit 1
fi

cd "$ROOT"
export GOCACHE="${GOCACHE:-/tmp/vastplan-go-cache}"
export VASTPLAN_TEST_POSTGRESQL_ENDPOINT="127.0.0.1:${POSTGRES_PORT}"
export VASTPLAN_TEST_POSTGRESQL_USER=vastplan
export VASTPLAN_TEST_POSTGRESQL_PASSWORD="$PASSWORD"
export VASTPLAN_TEST_POSTGRESQL_DATABASE=vastplan
export VASTPLAN_TEST_POSTGRESQL_TLS_MODE=disable
export VASTPLAN_TEST_POSTGRESQL_FAULT_CONTAINER="$POSTGRES_CONTAINER"
export VASTPLAN_TEST_MYSQL_ENDPOINT="127.0.0.1:${MYSQL_PORT}"
export VASTPLAN_TEST_MYSQL_USER=vastplan
export VASTPLAN_TEST_MYSQL_PASSWORD="$PASSWORD"
export VASTPLAN_TEST_MYSQL_DATABASE=vastplan
export VASTPLAN_TEST_MYSQL_TLS_MODE=disable
export VASTPLAN_TEST_MYSQL_FAULT_CONTAINER="$MYSQL_CONTAINER"

echo "[1/4] 验证 Provider、Record Store、连接池与故障恢复矩阵"
go test -count=1 -timeout=3m \
  -run 'Test(PostgreSQL|MySQL)ProviderIntegration$' \
  ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime
go test -count=1 -timeout=8m \
  -run 'Test(PostgreSQL|MySQL)ProviderFaultMatrix$' \
  ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime

echo "[2/4] 验证 Platform Control 并发初始化与完整重启恢复"
go test -count=1 -timeout=3m \
  -run 'Test(PostgreSQL|MySQL)PlatformControlBootstrapIntegration$' \
  ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/platformcontrolbootstrap

echo "[3/4] 创建 Platform Control 事务一致备份并执行隔离恢复"
VASTPLAN_TEST_PLATFORM_CONTROL_BACKUP_PHASE=seed \
go test -count=1 -timeout=3m \
  -run 'Test(PostgreSQL|MySQL)PlatformControlBackupFixture$' \
  ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/platformcontrolbootstrap

BACKUP_DIRECTORY="$(mktemp -d "${TMPDIR:-/tmp}/vastplan-control-backup.XXXXXX")"
chmod 700 "$BACKUP_DIRECTORY"
docker exec "$POSTGRES_CONTAINER" pg_dump -U vastplan -d vastplan \
  --format=custom --schema=vastplan_control_backup >"$BACKUP_DIRECTORY/postgresql.dump"
docker exec -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysqldump -uvastplan \
  --single-transaction --no-tablespaces --set-gtid-purged=OFF --databases vastplan \
  >"$BACKUP_DIRECTORY/mysql.sql"
chmod 600 "$BACKUP_DIRECTORY/postgresql.dump" "$BACKUP_DIRECTORY/mysql.sql"
test -s "$BACKUP_DIRECTORY/postgresql.dump"
test -s "$BACKUP_DIRECTORY/mysql.sql"

docker exec "$POSTGRES_CONTAINER" psql -U vastplan -d vastplan -v ON_ERROR_STOP=1 \
  -c 'DROP SCHEMA vastplan_control_backup CASCADE' >/dev/null
docker exec -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysql -uroot \
  -e 'DROP DATABASE vastplan' >/dev/null
docker exec -i "$POSTGRES_CONTAINER" pg_restore -U vastplan -d vastplan \
  --exit-on-error <"$BACKUP_DIRECTORY/postgresql.dump"
docker exec -i -e MYSQL_PWD="$PASSWORD" "$MYSQL_CONTAINER" mysql -uroot \
  <"$BACKUP_DIRECTORY/mysql.sql"

echo "[4/4] 重新打开恢复后的 Store 并验证 CAS 数据完整性"
VASTPLAN_TEST_PLATFORM_CONTROL_BACKUP_PHASE=verify \
go test -count=1 -timeout=3m \
  -run 'Test(PostgreSQL|MySQL)PlatformControlBackupFixture$' \
  ./extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/platformcontrolbootstrap
