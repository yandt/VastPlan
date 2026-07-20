package databaseruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

type sqlPool struct {
	db      *sql.DB
	minIdle int
	warmMu  sync.Mutex
	warmed  bool
	healthy atomic.Bool
	closed  atomic.Bool
}

func newSQLPool(connector *materialConnector, policy databasev1.PoolPolicy) (*sqlPool, error) {
	if connector == nil {
		return nil, errors.New("database/sql Connector 不能为空")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(policy.MaxOpen)
	db.SetMaxIdleConns(policy.MaxIdle)
	db.SetConnMaxLifetime(time.Duration(policy.MaxLifetimeMS) * time.Millisecond)
	db.SetConnMaxIdleTime(time.Duration(policy.MaxIdleTimeMS) * time.Millisecond)
	return &sqlPool{db: db, minIdle: policy.MinIdle}, nil
}

func (p *sqlPool) Probe(ctx context.Context) error {
	if err := p.available(); err != nil {
		return err
	}
	if ctx == nil {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("probe context 不能为空"))
	}
	err := p.db.PingContext(ctx)
	if err == nil {
		err = p.warm(ctx)
	}
	p.healthy.Store(err == nil)
	return classifySQLError(err, false)
}

func (p *sqlPool) Query(ctx context.Context, statement databasev1.Statement, maxRows int) (databasev1.QueryResult, error) {
	if err := p.available(); err != nil {
		return databasev1.QueryResult{}, err
	}
	result, err := querySQL(ctx, p.db, statement, maxRows, false)
	p.observe(err)
	return result, err
}

func (p *sqlPool) Execute(ctx context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	if err := p.available(); err != nil {
		return databasev1.ExecuteResult{}, err
	}
	result, err := executeSQL(ctx, p.db, statement, false)
	p.observe(err)
	return result, err
}

func (p *sqlPool) Begin(ctx context.Context, options databasev1.TransactionOptions) (Transaction, error) {
	if err := p.available(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("begin context 不能为空"))
	}
	isolation, err := sqlIsolation(options.Isolation)
	if err != nil || options.TimeoutMS < 100 || options.TimeoutMS > 3_600_000 {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务选项无效"))
	}
	transactionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Duration(options.TimeoutMS)*time.Millisecond)
	transaction, err := p.db.BeginTx(transactionContext, &sql.TxOptions{Isolation: isolation, ReadOnly: options.ReadOnly})
	if err != nil {
		cancel()
		return nil, classifySQLError(err, true)
	}
	return &sqlTransaction{transaction: transaction, cancel: cancel}, nil
}

func (p *sqlPool) Stats() PoolStats {
	if p == nil || p.db == nil {
		return PoolStats{}
	}
	stats := p.db.Stats()
	return PoolStats{
		Open: int64(stats.OpenConnections), Idle: int64(stats.Idle), InUse: int64(stats.InUse),
		WaitCount: stats.WaitCount, WaitDurationMS: stats.WaitDuration.Milliseconds(),
		MaxOpen: int64(stats.MaxOpenConnections), Healthy: p.healthy.Load() && !p.closed.Load(),
	}
}

func (p *sqlPool) Close() error {
	if p == nil || p.db == nil || p.closed.Swap(true) {
		return nil
	}
	p.healthy.Store(false)
	return p.db.Close()
}

func (p *sqlPool) warm(ctx context.Context) error {
	p.warmMu.Lock()
	defer p.warmMu.Unlock()
	if p.warmed || p.minIdle == 0 {
		p.warmed = true
		return nil
	}
	connections := make([]*sql.Conn, 0, p.minIdle)
	defer func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	}()
	for len(connections) < p.minIdle {
		connection, err := p.db.Conn(ctx)
		if err != nil {
			return err
		}
		connections = append(connections, connection)
	}
	p.warmed = true
	return nil
}

func (p *sqlPool) available() error {
	if p == nil || p.db == nil || p.closed.Load() {
		return NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, errors.New("数据库连接池已关闭"))
	}
	return nil
}

func (p *sqlPool) observe(err error) {
	if err == nil {
		p.healthy.Store(true)
		return
	}
	if code, _ := ErrorDetails(err); code == databasev1.ErrorConnectionUnavailable {
		p.healthy.Store(false)
	}
}

type sqlQueryExecutor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func querySQL(ctx context.Context, executor sqlQueryExecutor, statement databasev1.Statement,
	maxRows int, transaction bool) (databasev1.QueryResult, error) {
	if ctx == nil || executor == nil || maxRows < 1 || maxRows > 10_000 {
		return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("query 参数无效"))
	}
	arguments, err := statementArguments(statement)
	if err != nil {
		return databasev1.QueryResult{}, err
	}
	rows, err := executor.QueryContext(ctx, statement.SQL, arguments...)
	if err != nil {
		return databasev1.QueryResult{}, classifySQLError(err, transaction)
	}
	defer rows.Close()
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return databasev1.QueryResult{}, classifySQLError(err, transaction)
	}
	if len(columnTypes) > 1024 {
		return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorQueryFailed, false,
			fmt.Errorf("结果列数超过上限: %d", len(columnTypes)))
	}
	result := databasev1.QueryResult{
		Columns: make([]databasev1.Column, len(columnTypes)), Rows: make([][]databasev1.Value, 0, min(maxRows, 64)),
	}
	for index, columnType := range columnTypes {
		nullable, _ := columnType.Nullable()
		result.Columns[index] = databasev1.Column{
			Name: columnType.Name(), DatabaseType: columnType.DatabaseTypeName(), Nullable: nullable,
		}
	}
	for len(result.Rows) < maxRows && rows.Next() {
		raw := make([]any, len(columnTypes))
		destinations := make([]any, len(columnTypes))
		for index := range raw {
			destinations[index] = &raw[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return databasev1.QueryResult{}, classifySQLError(err, transaction)
		}
		converted := make([]databasev1.Value, len(raw))
		for index, value := range raw {
			converted[index], err = scannedValue(value, columnTypes[index].DatabaseTypeName())
			if err != nil {
				return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorQueryFailed, false,
					fmt.Errorf("转换结果列 %d: %w", index, err))
			}
		}
		result.Rows = append(result.Rows, converted)
	}
	if err := rows.Err(); err != nil {
		return databasev1.QueryResult{}, classifySQLError(err, transaction)
	}
	if len(result.Rows) == maxRows {
		result.Truncated = rows.Next()
		if err := rows.Err(); err != nil {
			return databasev1.QueryResult{}, classifySQLError(err, transaction)
		}
	}
	if err := databasev1.ValidateQueryResult(result); err != nil {
		return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorQueryFailed, false, err)
	}
	return result, nil
}

func executeSQL(ctx context.Context, executor sqlQueryExecutor, statement databasev1.Statement,
	transaction bool) (databasev1.ExecuteResult, error) {
	if ctx == nil || executor == nil {
		return databasev1.ExecuteResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("execute 参数无效"))
	}
	arguments, err := statementArguments(statement)
	if err != nil {
		return databasev1.ExecuteResult{}, err
	}
	result, err := executor.ExecContext(ctx, statement.SQL, arguments...)
	if err != nil {
		return databasev1.ExecuteResult{}, classifySQLError(err, transaction)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return databasev1.ExecuteResult{}, classifySQLError(err, transaction)
	}
	if rowsAffected < 0 {
		return databasev1.ExecuteResult{}, NewRuntimeError(databasev1.ErrorQueryFailed, false,
			errors.New("数据库返回了负 rowsAffected"))
	}
	return databasev1.ExecuteResult{RowsAffected: rowsAffected}, nil
}

type sqlTransaction struct {
	transaction *sql.Tx
	cancel      context.CancelFunc
	mu          sync.Mutex
	ended       bool
}

func (t *sqlTransaction) Query(ctx context.Context, statement databasev1.Statement, maxRows int) (databasev1.QueryResult, error) {
	if err := t.active(); err != nil {
		return databasev1.QueryResult{}, err
	}
	return querySQL(ctx, t.transaction, statement, maxRows, true)
}

func (t *sqlTransaction) Execute(ctx context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	if err := t.active(); err != nil {
		return databasev1.ExecuteResult{}, err
	}
	return executeSQL(ctx, t.transaction, statement, true)
}

func (t *sqlTransaction) Commit(ctx context.Context) error {
	return t.end(ctx, true)
}

func (t *sqlTransaction) Rollback(ctx context.Context) error {
	return t.end(ctx, false)
}

func (t *sqlTransaction) active() error {
	if t == nil || t.transaction == nil {
		return NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务不存在"))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务已结束"))
	}
	return nil
}

func (t *sqlTransaction) end(ctx context.Context, commit bool) error {
	if ctx == nil {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务 context 不能为空"))
	}
	if t == nil || t.transaction == nil {
		return NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务不存在"))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务已结束"))
	}
	t.ended = true
	defer t.cancel()
	if err := ctx.Err(); err != nil {
		_ = t.transaction.Rollback()
		return classifySQLError(err, true)
	}
	var err error
	if commit {
		err = t.transaction.Commit()
	} else {
		err = t.transaction.Rollback()
	}
	return classifySQLError(err, true)
}

func sqlIsolation(value string) (sql.IsolationLevel, error) {
	switch value {
	case "default":
		return sql.LevelDefault, nil
	case "read-uncommitted":
		return sql.LevelReadUncommitted, nil
	case "read-committed":
		return sql.LevelReadCommitted, nil
	case "repeatable-read":
		return sql.LevelRepeatableRead, nil
	case "serializable":
		return sql.LevelSerializable, nil
	default:
		return sql.LevelDefault, errUnsupportedIsolation
	}
}
