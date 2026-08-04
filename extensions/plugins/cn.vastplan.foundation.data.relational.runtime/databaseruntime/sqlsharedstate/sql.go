package sqlsharedstate

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

func stringValue(value string) databasev1.Value {
	return databasev1.Value{Type: "string", Value: marshal(value)}
}
func bytesValue(value []byte) databasev1.Value {
	return databasev1.Value{Type: "bytes", Value: marshal(base64.StdEncoding.EncodeToString(value))}
}
func intValue(value uint64) databasev1.Value {
	return databasev1.Value{Type: "int64", Value: marshal(strconv.FormatUint(value, 10))}
}
func timestampValue(value time.Time) databasev1.Value {
	return databasev1.Value{Type: "timestamp", Value: marshal(value.UTC().Format(time.RFC3339Nano))}
}
func marshal(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }

func quoteAll(dialect recordstore.Dialect, columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = dialect.Quote(column)
	}
	return strings.Join(quoted, ", ")
}

func placeholders(dialect recordstore.Dialect, count int) string {
	values := make([]string, count)
	for index := range values {
		values[index] = dialect.Placeholder(index + 1)
	}
	return strings.Join(values, ", ")
}

func equalWhere(dialect recordstore.Dialect, columns []string) string {
	return equalWhereOffset(dialect, columns, 0)
}
func equalWhereOffset(dialect recordstore.Dialect, columns []string, offset int) string {
	values := make([]string, len(columns))
	for index, column := range columns {
		values[index] = fmt.Sprintf("%s = %s", dialect.Quote(column), dialect.Placeholder(offset+index+1))
	}
	return strings.Join(values, " AND ")
}

type runtimeCoded interface{ RuntimeCode() string }

func mapConstraint(err error) error {
	var coded runtimeCoded
	if errors.As(err, &coded) && coded.RuntimeCode() == databasev1.ErrorConstraintViolation {
		return sharedstate.ErrConflict
	}
	return err
}
