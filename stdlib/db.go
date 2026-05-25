package stdlib

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"

	"github.com/adriangitvitz/yoru/interpreter"
)

// DBDriver abstracts database operations (mirrors database/sql semantics).
type DBDriver interface {
	Query(query string, args []any) ([]map[string]any, error)
	QueryRow(query string, args []any) (map[string]any, error)
	Exec(query string, args []any) (int64, error)
	Begin() (Transaction, error)
	Close() error
}

// Transaction abstracts a database transaction.
type Transaction interface {
	Query(query string, args []any) ([]map[string]any, error)
	Exec(query string, args []any) (int64, error)
	Commit() error
	Rollback() error
}

// MemoryDriver is an in-memory DBDriver for tests. SQL is regex-parsed:
// only the narrow shapes used by tests work; anything else → "unsupported query".
type MemoryDriver struct {
	mu     sync.RWMutex
	tables map[string][]map[string]any
}

func NewMemoryDriver() *MemoryDriver {
	return &MemoryDriver{
		tables: make(map[string][]map[string]any),
	}
}

var (
	reInsert = regexp.MustCompile(`(?i)^\s*INSERT\s+INTO\s+(\w+)\s*\(([^)]+)\)\s*VALUES\s*\(([^)]+)\)\s*$`)
	reSelect = regexp.MustCompile(`(?i)^\s*SELECT\s+(.+?)\s+FROM\s+(\w+)(?:\s+WHERE\s+(\w+)\s*=\s*(?:\?|\$\d+))?\s*$`)
	reUpdate = regexp.MustCompile(`(?i)^\s*UPDATE\s+(\w+)\s+SET\s+(\w+)\s*=\s*(?:\?|\$\d+)\s+WHERE\s+(\w+)\s*=\s*(?:\?|\$\d+)\s*$`)
	reDelete = regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+(\w+)(?:\s+WHERE\s+(\w+)\s*=\s*(?:\?|\$\d+))?\s*$`)
)

// normalizePlaceholders replaces $1, $2, ... with ? for uniform handling.
func normalizePlaceholders(query string) string {
	re := regexp.MustCompile(`\$\d+`)
	return re.ReplaceAllString(query, "?")
}

func (d *MemoryDriver) Query(query string, args []any) ([]map[string]any, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.queryInternal(query, args)
}

func (d *MemoryDriver) queryInternal(query string, args []any) ([]map[string]any, error) {
	q := normalizePlaceholders(query)
	m := reSelect.FindStringSubmatch(q)
	if m == nil {
		return nil, fmt.Errorf("MemoryDriver: unsupported query: %s", query)
	}

	table := m[2]
	whereCol := m[3]

	rows := d.tables[table]
	if rows == nil {
		return []map[string]any{}, nil
	}

	if whereCol == "" {
		// Defensive copies: callers must not mutate stored rows.
		result := make([]map[string]any, len(rows))
		for i, row := range rows {
			result[i] = copyRow(row)
		}
		return result, nil
	}

	if len(args) < 1 {
		return nil, fmt.Errorf("MemoryDriver: WHERE clause requires argument")
	}
	whereVal := args[0]
	var result []map[string]any
	for _, row := range rows {
		if fmt.Sprintf("%v", row[whereCol]) == fmt.Sprintf("%v", whereVal) {
			result = append(result, copyRow(row))
		}
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

func (d *MemoryDriver) QueryRow(query string, args []any) (map[string]any, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.queryRowInternal(query, args)
}

func (d *MemoryDriver) queryRowInternal(query string, args []any) (map[string]any, error) {
	rows, err := d.queryInternal(query, args)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 1 {
		return nil, fmt.Errorf("MemoryDriver: multiple rows returned, expected 0 or 1")
	}
	return rows[0], nil
}

func (d *MemoryDriver) Exec(query string, args []any) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.execInternal(query, args)
}

func (d *MemoryDriver) execInternal(query string, args []any) (int64, error) {
	q := normalizePlaceholders(query)

	if m := reInsert.FindStringSubmatch(q); m != nil {
		table := m[1]
		cols := splitTrim(m[2])
		if len(cols) != len(args) {
			return 0, fmt.Errorf("MemoryDriver: column count (%d) != arg count (%d)", len(cols), len(args))
		}
		row := make(map[string]any)
		for i, col := range cols {
			row[col] = args[i]
		}
		d.tables[table] = append(d.tables[table], row)
		return 1, nil
	}

	if m := reUpdate.FindStringSubmatch(q); m != nil {
		table := m[1]
		setCol := m[2]
		whereCol := m[3]
		if len(args) < 2 {
			return 0, fmt.Errorf("MemoryDriver: UPDATE requires 2 arguments")
		}
		setVal := args[0]
		whereVal := args[1]
		var affected int64
		for _, row := range d.tables[table] {
			if fmt.Sprintf("%v", row[whereCol]) == fmt.Sprintf("%v", whereVal) {
				row[setCol] = setVal
				affected++
			}
		}
		return affected, nil
	}

	if m := reDelete.FindStringSubmatch(q); m != nil {
		table := m[1]
		whereCol := m[2]
		rows := d.tables[table]
		if rows == nil {
			return 0, nil
		}
		if whereCol == "" {
			count := int64(len(rows))
			d.tables[table] = nil
			return count, nil
		}
		if len(args) < 1 {
			return 0, fmt.Errorf("MemoryDriver: DELETE WHERE requires argument")
		}
		whereVal := args[0]
		var kept []map[string]any
		var affected int64
		for _, row := range rows {
			if fmt.Sprintf("%v", row[whereCol]) == fmt.Sprintf("%v", whereVal) {
				affected++
			} else {
				kept = append(kept, row)
			}
		}
		d.tables[table] = kept
		return affected, nil
	}

	return 0, fmt.Errorf("MemoryDriver: unsupported query: %s", query)
}

func (d *MemoryDriver) Begin() (Transaction, error) {
	d.mu.RLock()
	snapshot := d.deepCopyTables()
	d.mu.RUnlock()

	return &memoryTx{
		driver:   d,
		snapshot: snapshot,
	}, nil
}

func (d *MemoryDriver) Close() error { return nil }

// memoryTx is snapshot-isolated: writes land on tx.snapshot; Commit publishes,
// Rollback discards.
type memoryTx struct {
	driver   *MemoryDriver
	snapshot map[string][]map[string]any
	done     bool
}

func (tx *memoryTx) Query(query string, args []any) ([]map[string]any, error) {
	if tx.done {
		return nil, fmt.Errorf("transaction already completed")
	}
	tmp := &MemoryDriver{tables: tx.snapshot}
	return tmp.queryInternal(query, args)
}

func (tx *memoryTx) Exec(query string, args []any) (int64, error) {
	if tx.done {
		return 0, fmt.Errorf("transaction already completed")
	}
	tmp := &MemoryDriver{tables: tx.snapshot}
	return tmp.execInternal(query, args)
}

func (tx *memoryTx) Commit() error {
	if tx.done {
		return fmt.Errorf("transaction already completed")
	}
	tx.done = true
	tx.driver.mu.Lock()
	tx.driver.tables = tx.snapshot
	tx.driver.mu.Unlock()
	return nil
}

func (tx *memoryTx) Rollback() error {
	if tx.done {
		return fmt.Errorf("transaction already completed")
	}
	tx.done = true
	return nil
}

func copyRow(row map[string]any) map[string]any {
	c := make(map[string]any, len(row))
	maps.Copy(c, row)
	return c
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, len(parts))
	for i, p := range parts {
		result[i] = strings.TrimSpace(p)
	}
	return result
}

func (d *MemoryDriver) deepCopyTables() map[string][]map[string]any {
	cp := make(map[string][]map[string]any)
	for table, rows := range d.tables {
		cpRows := make([]map[string]any, len(rows))
		for i, row := range rows {
			cpRows[i] = copyRow(row)
		}
		cp[table] = cpRows
	}
	return cp
}

// DBProvider implements the DB effect. Interp (optional, wired by
// stdlib.InstallAll) is required when DB.transaction is passed a Yoru closure;
// without it only Go BuiltinVal callbacks work.
type DBProvider struct {
	driver DBDriver
	Interp *interpreter.Interpreter
}

func NewDBProvider(driver DBDriver) *DBProvider {
	return &DBProvider{driver: driver}
}

// WithInterp attaches an interpreter so DB.transaction accepts Yoru closures.
func (p *DBProvider) WithInterp(interp *interpreter.Interpreter) *DBProvider {
	p.Interp = interp
	return p
}

func (p *DBProvider) EffectName() string { return "DB" }

func (p *DBProvider) Methods() map[string]interpreter.Value {
	return map[string]interpreter.Value{
		"query": &interpreter.BuiltinVal{Name: "DB.query", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("DB.query() takes 2 arguments (sql, args)")
			}
			sql, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("DB.query() first argument must be String")
			}
			goArgs := listValToGoArgs(args[1])
			rows, err := p.driver.Query(sql.V, goArgs)
			if err != nil {
				return nil, fmt.Errorf("DB.query() error: %s", err)
			}
			return rowsToListVal(rows), nil
		}},
		"query_one": &interpreter.BuiltinVal{Name: "DB.query_one", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("DB.query_one() takes 2 arguments (sql, args)")
			}
			sql, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("DB.query_one() first argument must be String")
			}
			goArgs := listValToGoArgs(args[1])
			row, err := p.driver.QueryRow(sql.V, goArgs)
			if err != nil {
				return nil, fmt.Errorf("DB.query_one() error: %s", err)
			}
			if row == nil {
				return &interpreter.NilVal{}, nil
			}
			return rowToObjectVal(row), nil
		}},
		"exec": &interpreter.BuiltinVal{Name: "DB.exec", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 2 {
				return nil, fmt.Errorf("DB.exec() takes 2 arguments (sql, args)")
			}
			sql, ok := args[0].(*interpreter.StringVal)
			if !ok {
				return nil, fmt.Errorf("DB.exec() first argument must be String")
			}
			goArgs := listValToGoArgs(args[1])
			affected, err := p.driver.Exec(sql.V, goArgs)
			if err != nil {
				return nil, fmt.Errorf("DB.exec() error: %s", err)
			}
			return &interpreter.IntVal{V: affected}, nil
		}},
		"transaction": &interpreter.BuiltinVal{Name: "DB.transaction", Fn: func(args []interpreter.Value) (interpreter.Value, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("DB.transaction() takes 1 argument (fn)")
			}
			// Accept FunctionVal (Yoru) or BuiltinVal (Go).
			switch args[0].(type) {
			case *interpreter.BuiltinVal, *interpreter.FunctionVal:
			default:
				return nil, fmt.Errorf("DB.transaction() argument must be a function")
			}
			if _, isFn := args[0].(*interpreter.FunctionVal); isFn && p.Interp == nil {
				return nil, fmt.Errorf("DB.transaction(): Yoru closures require the DB provider to hold an interpreter handle (use WithInterp)")
			}

			tx, err := p.driver.Begin()
			if err != nil {
				return nil, fmt.Errorf("DB.transaction() begin error: %s", err)
			}

			var result interpreter.Value
			var panicReason any
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicReason = r
					}
				}()
				switch fn := args[0].(type) {
				case *interpreter.BuiltinVal:
					v, fnErr := fn.Fn(nil)
					if fnErr != nil {
						panicReason = fnErr.Error()
						return
					}
					result = v
				case *interpreter.FunctionVal:
					result = p.Interp.ApplyCallback(fn, nil)
				}
			}()

			// Panic → rollback + Result.Err so callers pattern-match.
			if panicReason != nil {
				_ = tx.Rollback()
				return interpreter.MakeErrResult("db_transaction_panic",
					fmt.Sprintf("transaction body panicked: %v", panicReason)), nil
			}

			// Result.Err from closure: rollback, propagate verbatim (no re-wrap).
			if ev, ok := result.(*interpreter.EnumVal); ok && ev.TypeName == "Result" && ev.Variant == "Err" {
				_ = tx.Rollback()
				return ev, nil
			}

			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("DB.transaction() commit error: %s", err)
			}

			// Wrap in Result.Ok so callers always get a Result back.
			if ev, ok := result.(*interpreter.EnumVal); ok && ev.TypeName == "Result" {
				return ev, nil
			}
			if result == nil {
				result = &interpreter.NilVal{}
			}
			return interpreter.MakeOkResult(result), nil
		}},
	}
}

func listValToGoArgs(v interpreter.Value) []any {
	list, ok := v.(*interpreter.ListVal)
	if !ok || list == nil || list.Elements == nil {
		return nil
	}
	result := make([]any, len(list.Elements))
	for i, elem := range list.Elements {
		result[i] = valueToGo(elem)
	}
	return result
}

func rowToObjectVal(row map[string]any) *interpreter.ObjectVal {
	fields := make(map[string]interpreter.Value, len(row))
	for k, v := range row {
		fields[k] = goToValue(v)
	}
	return &interpreter.ObjectVal{TypeName: "Object", Fields: fields}
}

func rowsToListVal(rows []map[string]any) *interpreter.ListVal {
	elems := make([]interpreter.Value, len(rows))
	for i, row := range rows {
		elems[i] = rowToObjectVal(row)
	}
	return &interpreter.ListVal{Elements: elems}
}
