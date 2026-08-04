package recordstore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type Compiler struct {
	dialect Dialect
	model   compiledModel
}

func NewCompiler(dialect Dialect, model datamodelv1.Model) (*Compiler, error) {
	if dialect == nil {
		return nil, errors.New("Record Store SQL Dialect 不能为空")
	}
	if err := datamodelv1.Validate(model); err != nil {
		return nil, err
	}
	return &Compiler{dialect: dialect, model: compileModel(model)}, nil
}

func (c *Compiler) Create(record recordstorev1.Record, scope TrustedScope, now time.Time) (databasev1.Statement, recordstorev1.Record, error) {
	prepared, err := c.model.prepareCreate(record, scope, now)
	if err != nil {
		return databasev1.Statement{}, nil, err
	}
	fields := c.presentFields(prepared)
	columns, placeholders, parameters := make([]string, 0, len(fields)), make([]string, 0, len(fields)), make([]databasev1.Value, 0, len(fields))
	for _, field := range fields {
		value, valueErr := wireValue(field, prepared[field.ID])
		if valueErr != nil {
			return databasev1.Statement{}, nil, valueErr
		}
		columns = append(columns, c.dialect.Quote(field.Column))
		parameters = append(parameters, value)
		placeholders = append(placeholders, c.dialect.Placeholder(len(parameters)))
	}
	statement := databasev1.Statement{SQL: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", c.table(), strings.Join(columns, ", "), strings.Join(placeholders, ", ")), Parameters: parameters}
	return statement, prepared, databasev1.ValidateStatement(statement)
}

func (c *Compiler) Get(key recordstorev1.Key, scope TrustedScope) (databasev1.Statement, error) {
	prepared, err := c.model.prepareKey(key, scope)
	if err != nil {
		return databasev1.Statement{}, err
	}
	where, parameters, err := c.keyWhere(prepared, 0)
	if err != nil {
		return databasev1.Statement{}, err
	}
	where = append(where, c.notDeleted())
	statement := databasev1.Statement{SQL: fmt.Sprintf("SELECT %s FROM %s WHERE %s", c.selectColumns(), c.table(), strings.Join(nonEmpty(where), " AND ")), Parameters: parameters}
	return statement, databasev1.ValidateStatement(statement)
}

func (c *Compiler) List(request recordstorev1.ListRequest, scope TrustedScope) (databasev1.Statement, int, error) {
	if request.Limit < 1 || request.Limit > 200 {
		return databasev1.Statement{}, 0, errors.New("List limit 必须在 1..200")
	}
	offset, err := decodeCursor(request.Cursor, request.Model)
	if err != nil {
		return databasev1.Statement{}, 0, err
	}
	where, parameters, err := c.scopeWhere(scope)
	if err != nil {
		return databasev1.Statement{}, 0, err
	}
	where = append(where, c.notDeleted())
	for _, filter := range request.Filters {
		clause, values, filterErr := c.filter(filter, len(parameters))
		if filterErr != nil {
			return databasev1.Statement{}, 0, filterErr
		}
		where, parameters = append(where, clause), append(parameters, values...)
	}
	order, err := c.orderBy(request.Sort)
	if err != nil {
		return databasev1.Statement{}, 0, err
	}
	parameters = append(parameters, databasev1.Value{Type: "int64", Value: mustJSON(fmt.Sprintf("%d", request.Limit+1))})
	limitPlaceholder := c.dialect.Placeholder(len(parameters))
	parameters = append(parameters, databasev1.Value{Type: "int64", Value: mustJSON(fmt.Sprintf("%d", offset))})
	offsetPlaceholder := c.dialect.Placeholder(len(parameters))
	statement := databasev1.Statement{SQL: fmt.Sprintf("SELECT %s FROM %s%s ORDER BY %s LIMIT %s OFFSET %s", c.selectColumns(), c.table(), optionalWhere(where), order, limitPlaceholder, offsetPlaceholder), Parameters: parameters}
	return statement, offset, databasev1.ValidateStatement(statement)
}

func (c *Compiler) Update(request recordstorev1.UpdateRequest, scope TrustedScope, now time.Time) (databasev1.Statement, error) {
	values, err := c.model.prepareUpdate(request.Values, scope, now)
	if err != nil {
		return databasev1.Statement{}, err
	}
	key, err := c.model.prepareKey(request.Key, scope)
	if err != nil {
		return databasev1.Statement{}, err
	}
	sets, parameters := []string{}, []databasev1.Value{}
	for _, field := range c.presentFields(values) {
		value, valueErr := wireValue(field, values[field.ID])
		if valueErr != nil {
			return databasev1.Statement{}, valueErr
		}
		parameters = append(parameters, value)
		sets = append(sets, fmt.Sprintf("%s = %s", c.dialect.Quote(field.Column), c.dialect.Placeholder(len(parameters))))
	}
	if lock := c.model.model.OptimisticLock; lock != nil {
		column := c.dialect.Quote(c.model.fields[lock.Field].Column)
		sets = append(sets, fmt.Sprintf("%s = %s + 1", column, column))
	}
	where, keyValues, err := c.keyWhere(key, len(parameters))
	if err != nil {
		return databasev1.Statement{}, err
	}
	parameters = append(parameters, keyValues...)
	if lock := c.model.model.OptimisticLock; lock != nil {
		parameters = append(parameters, databasev1.Value{Type: "int64", Value: mustJSON(fmt.Sprintf("%d", request.ExpectedRevision))})
		where = append(where, fmt.Sprintf("%s = %s", c.dialect.Quote(c.model.fields[lock.Field].Column), c.dialect.Placeholder(len(parameters))))
	}
	where = append(where, c.notDeleted())
	statement := databasev1.Statement{SQL: fmt.Sprintf("UPDATE %s SET %s WHERE %s", c.table(), strings.Join(sets, ", "), strings.Join(nonEmpty(where), " AND ")), Parameters: parameters}
	return statement, databasev1.ValidateStatement(statement)
}

func (c *Compiler) Delete(request recordstorev1.DeleteRequest, scope TrustedScope, now time.Time) (databasev1.Statement, error) {
	key, err := c.model.prepareKey(request.Key, scope)
	if err != nil {
		return databasev1.Statement{}, err
	}
	where, parameters, err := c.keyWhere(key, 0)
	if err != nil {
		return databasev1.Statement{}, err
	}
	if lock := c.model.model.OptimisticLock; lock != nil {
		parameters = append(parameters, databasev1.Value{Type: "int64", Value: mustJSON(fmt.Sprintf("%d", request.ExpectedRevision))})
		where = append(where, fmt.Sprintf("%s = %s", c.dialect.Quote(c.model.fields[lock.Field].Column), c.dialect.Placeholder(len(parameters))))
	}
	where = append(where, c.notDeleted())
	if c.model.model.Deletion.Mode == "hard" {
		statement := databasev1.Statement{SQL: fmt.Sprintf("DELETE FROM %s WHERE %s", c.table(), strings.Join(nonEmpty(where), " AND ")), Parameters: parameters}
		return statement, databasev1.ValidateStatement(statement)
	}
	field := c.model.fields[c.model.model.Deletion.Field]
	var deleted json.RawMessage
	if field.Type == "bool" {
		deleted = json.RawMessage("true")
	} else {
		deleted, _ = json.Marshal(now.UTC().Format(time.RFC3339Nano))
	}
	value, _ := wireValue(field, deleted)
	parameters = append([]databasev1.Value{value}, parameters...)
	// Prepending one parameter changes every existing placeholder; rebuild the WHERE clause.
	where, keyValues, err := c.keyWhere(key, 1)
	if err != nil {
		return databasev1.Statement{}, err
	}
	parameters = append(parameters[:1], keyValues...)
	if lock := c.model.model.OptimisticLock; lock != nil {
		parameters = append(parameters, databasev1.Value{Type: "int64", Value: mustJSON(fmt.Sprintf("%d", request.ExpectedRevision))})
		where = append(where, fmt.Sprintf("%s = %s", c.dialect.Quote(c.model.fields[lock.Field].Column), c.dialect.Placeholder(len(parameters))))
	}
	where = append(where, c.notDeleted())
	sets := []string{fmt.Sprintf("%s = %s", c.dialect.Quote(field.Column), c.dialect.Placeholder(1))}
	if lock := c.model.model.OptimisticLock; lock != nil {
		column := c.dialect.Quote(c.model.fields[lock.Field].Column)
		sets = append(sets, fmt.Sprintf("%s = %s + 1", column, column))
	}
	statement := databasev1.Statement{SQL: fmt.Sprintf("UPDATE %s SET %s WHERE %s", c.table(), strings.Join(sets, ", "), strings.Join(nonEmpty(where), " AND ")), Parameters: parameters}
	return statement, databasev1.ValidateStatement(statement)
}

func (c *Compiler) DecodeRow(row []databasev1.Value) (recordstorev1.Record, error) {
	if len(row) != len(c.model.fieldOrder) {
		return nil, errors.New("Record Store 返回列数与 DataModel 不一致")
	}
	record := make(recordstorev1.Record, len(row))
	for index, fieldID := range c.model.fieldOrder {
		raw, err := rawValue(c.model.fields[fieldID], row[index])
		if err != nil {
			return nil, fmt.Errorf("解码字段 %s: %w", fieldID, err)
		}
		record[fieldID] = raw
	}
	return record, nil
}

func EncodeCursor(ref recordstorev1.ModelRef, offset int) string {
	raw, _ := json.Marshal(struct {
		Model  string `json:"model"`
		SHA256 string `json:"sha256"`
		Offset int    `json:"offset"`
	}{ref.ID, ref.SHA256, offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(cursor string, ref recordstorev1.ModelRef) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, errors.New("List cursor 无效")
	}
	var value struct {
		Model  string `json:"model"`
		SHA256 string `json:"sha256"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(raw, &value); err != nil || value.Model != ref.ID || value.SHA256 != ref.SHA256 || value.Offset < 0 {
		return 0, errors.New("List cursor 与 DataModel 不匹配")
	}
	return value.Offset, nil
}

func (c *Compiler) table() string { return c.dialect.Quote(c.model.model.Storage.Table) }

func (c *Compiler) selectColumns() string {
	columns := make([]string, 0, len(c.model.fieldOrder))
	for _, fieldID := range c.model.fieldOrder {
		columns = append(columns, c.dialect.Quote(c.model.fields[fieldID].Column))
	}
	return strings.Join(columns, ", ")
}

func (c *Compiler) presentFields(record recordstorev1.Record) []datamodelv1.Field {
	fields := make([]datamodelv1.Field, 0, len(record))
	for _, fieldID := range c.model.fieldOrder {
		if _, exists := record[fieldID]; exists {
			fields = append(fields, c.model.fields[fieldID])
		}
	}
	return fields
}

func (c *Compiler) keyWhere(key recordstorev1.Key, parameterOffset int) ([]string, []databasev1.Value, error) {
	fields := make([]string, 0, len(key))
	for _, fieldID := range c.model.fieldOrder {
		if _, exists := key[fieldID]; exists {
			fields = append(fields, fieldID)
		}
	}
	where, values := make([]string, 0, len(fields)), make([]databasev1.Value, 0, len(fields))
	for _, fieldID := range fields {
		field := c.model.fields[fieldID]
		value, err := wireValue(field, key[fieldID])
		if err != nil {
			return nil, nil, err
		}
		values = append(values, value)
		where = append(where, fmt.Sprintf("%s = %s", c.dialect.Quote(field.Column), c.dialect.Placeholder(parameterOffset+len(values))))
	}
	return where, values, nil
}

func (c *Compiler) scopeWhere(scope TrustedScope) ([]string, []databasev1.Value, error) {
	key := recordstorev1.Key{}
	if c.model.model.Scope.Tenant == "required" {
		key["tenantId"], _ = json.Marshal(scope.TenantID)
	}
	if c.model.model.Scope.Service == "required" {
		key["serviceId"], _ = json.Marshal(scope.ServiceID)
	}
	return c.keyWhere(key, 0)
}

func (c *Compiler) notDeleted() string {
	if c.model.model.Deletion.Mode != "soft" {
		return ""
	}
	field := c.model.fields[c.model.model.Deletion.Field]
	if field.Type == "bool" {
		return fmt.Sprintf("%s = %s", c.dialect.Quote(field.Column), c.dialect.Boolean(false))
	}
	return fmt.Sprintf("%s IS NULL", c.dialect.Quote(field.Column))
}

func nonEmpty(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func optionalWhere(values []string) string {
	values = nonEmpty(values)
	if len(values) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(values, " AND ")
}

func (c *Compiler) orderBy(requested []recordstorev1.Sort) (string, error) {
	seen, values := map[string]struct{}{}, make([]recordstorev1.Sort, 0, len(requested)+len(c.model.model.PrimaryKey))
	for _, item := range requested {
		if _, exists := c.model.fields[item.Field]; !exists || (item.Direction != "asc" && item.Direction != "desc") {
			return "", fmt.Errorf("Sort 字段或方向无效: %s", item.Field)
		}
		if _, duplicate := seen[item.Field]; duplicate {
			return "", fmt.Errorf("Sort 字段重复: %s", item.Field)
		}
		seen[item.Field], values = struct{}{}, append(values, item)
	}
	for _, fieldID := range c.model.model.PrimaryKey {
		if _, exists := seen[fieldID]; !exists {
			values = append(values, recordstorev1.Sort{Field: fieldID, Direction: "asc"})
		}
	}
	parts := make([]string, 0, len(values))
	for _, item := range values {
		parts = append(parts, c.dialect.Quote(c.model.fields[item.Field].Column)+" "+strings.ToUpper(item.Direction))
	}
	return strings.Join(parts, ", "), nil
}

func (c *Compiler) filter(filter recordstorev1.Filter, parameterOffset int) (string, []databasev1.Value, error) {
	field, exists := c.model.fields[filter.Field]
	if !exists {
		return "", nil, fmt.Errorf("Filter 引用未知字段 %s", filter.Field)
	}
	column := c.dialect.Quote(field.Column)
	switch filter.Operator {
	case "is-null", "not-null":
		if !field.Nullable {
			return "", nil, fmt.Errorf("Filter 字段 %s 不可为 null", field.ID)
		}
		if len(filter.Value) != 0 || len(filter.Values) != 0 {
			return "", nil, errors.New("null Filter 不得携带值")
		}
		if filter.Operator == "is-null" {
			return column + " IS NULL", nil, nil
		}
		return column + " IS NOT NULL", nil, nil
	case "in":
		if len(filter.Values) == 0 || len(filter.Value) != 0 {
			return "", nil, errors.New("in Filter 只接受 values")
		}
		values, placeholders := make([]databasev1.Value, 0, len(filter.Values)), make([]string, 0, len(filter.Values))
		for _, raw := range filter.Values {
			value, err := wireValue(field, raw)
			if err != nil {
				return "", nil, err
			}
			values = append(values, value)
			placeholders = append(placeholders, c.dialect.Placeholder(parameterOffset+len(values)))
		}
		return fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ", ")), values, nil
	case "prefix":
		if field.Type != "string" || len(filter.Value) == 0 || len(filter.Values) != 0 {
			return "", nil, errors.New("prefix Filter 只接受单个 string value")
		}
		var text string
		if err := json.Unmarshal(filter.Value, &text); err != nil {
			return "", nil, errors.New("prefix Filter value 无效")
		}
		text = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
		value, _ := wireValue(field, mustJSON(text))
		return fmt.Sprintf("%s LIKE %s ESCAPE '\\\\'", column, c.dialect.Placeholder(parameterOffset+1)), []databasev1.Value{value}, nil
	case "eq", "ne", "lt", "lte", "gt", "gte":
		if len(filter.Value) == 0 || len(filter.Values) != 0 {
			return "", nil, errors.New("比较 Filter 只接受单个 value")
		}
		value, err := wireValue(field, filter.Value)
		if err != nil {
			return "", nil, err
		}
		operator := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}[filter.Operator]
		return fmt.Sprintf("%s %s %s", column, operator, c.dialect.Placeholder(parameterOffset+1)), []databasev1.Value{value}, nil
	default:
		return "", nil, fmt.Errorf("Filter operator 无效: %s", filter.Operator)
	}
}
