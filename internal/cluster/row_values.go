package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RowValue is one column of a row sent to another node, typed so the other
// node stores what this one holds.
type RowValue struct {
	Type string    `json:"t"` // "null", "int", "real", "text", "blob" or "time"
	Int  int64     `json:"i,omitempty"`
	Real float64   `json:"r,omitempty"`
	Text string    `json:"s,omitempty"`
	Blob []byte    `json:"b,omitempty"`
	Time time.Time `json:"d,omitempty"`
}

func rowValueOf(v any) (RowValue, error) {
	switch x := v.(type) {
	case nil:
		return RowValue{Type: "null"}, nil
	case int64:
		return RowValue{Type: "int", Int: x}, nil
	case bool:
		if x {
			return RowValue{Type: "int", Int: 1}, nil
		}
		return RowValue{Type: "int"}, nil
	case float64:
		return RowValue{Type: "real", Real: x}, nil
	case string:
		return RowValue{Type: "text", Text: x}, nil
	case []byte:
		return RowValue{Type: "blob", Blob: append([]byte(nil), x...)}, nil
	case time.Time:
		return RowValue{Type: "time", Time: x}, nil
	}
	return RowValue{}, fmt.Errorf("unsupported column value %T", v)
}

func (v RowValue) value() (any, error) {
	switch v.Type {
	case "null":
		return nil, nil
	case "int":
		return v.Int, nil
	case "real":
		return v.Real, nil
	case "text":
		return v.Text, nil
	case "blob":
		return v.Blob, nil
	case "time":
		return v.Time, nil
	}
	return nil, fmt.Errorf("unknown column type %q", v.Type)
}

// scanRows reads every row of rows by column, leaving out the column skip
// names, and closes them.
func scanRows(rows *sql.Rows, table, skip string) ([]map[string]RowValue, error) {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]RowValue
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("read %s: %w", table, err)
		}
		row := make(map[string]RowValue, len(columns))
		for i, c := range columns {
			if c == skip {
				continue
			}
			v, err := rowValueOf(values[i])
			if err != nil {
				return nil, fmt.Errorf("read %s.%s: %w", table, c, err)
			}
			row[c] = v
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	return out, nil
}

func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		known[name] = true
	}
	if len(known) == 0 {
		return nil, fmt.Errorf("table %s does not exist on this node", table)
	}
	return known, rows.Err()
}
