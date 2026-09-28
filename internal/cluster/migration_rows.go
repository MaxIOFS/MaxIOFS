package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// bucketTable is a table of a node's own database that holds rows of a
// bucket, found by the column naming it. Bucket names are unique in the
// cluster, so the name alone finds them.
type bucketTable struct {
	name   string
	bucket string
	// generatedID: the id column is assigned by the database and is not
	// carried to another node, where it would collide with that node's rows.
	generatedID bool
}

// bucketTables are copied with a migrated bucket, parents before the rows
// that reference them.
var bucketTables = []bucketTable{
	{name: "bucket_inventory_configs", bucket: "bucket_name"},
	{name: "bucket_inventory_reports", bucket: "bucket_name"},
	{name: "replication_rules", bucket: "source_bucket"},
	{name: "replication_queue", bucket: "bucket", generatedID: true},
	{name: "replication_status", bucket: "source_bucket", generatedID: true},
	{name: "shares", bucket: "bucket_name"},
}

func lookupBucketTable(name string) (bucketTable, bool) {
	for _, t := range bucketTables {
		if t.name == name {
			return t, true
		}
	}
	return bucketTable{}, false
}

// RowValue is one column of a copied row, typed so the other node stores what
// this one holds.
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

// BucketRows are a bucket's rows in a node's database, by table.
type BucketRows map[string][]map[string]RowValue

// readBucketRows returns the rows of bucket in every bucket table.
func readBucketRows(ctx context.Context, db *sql.DB, bucket string) (BucketRows, error) {
	out := BucketRows{}
	for _, t := range bucketTables {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %s WHERE %s = ?`, t.name, t.bucket), bucket)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", t.name, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read %s: %w", t.name, err)
			}
			row := make(map[string]RowValue, len(columns))
			for i, c := range columns {
				if t.generatedID && c == "id" {
					continue
				}
				v, err := rowValueOf(values[i])
				if err != nil {
					rows.Close()
					return nil, fmt.Errorf("read %s.%s: %w", t.name, c, err)
				}
				row[c] = v
			}
			out[t.name] = append(out[t.name], row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", t.name, err)
		}
	}
	return out, nil
}

// applyBucketRows makes rows copied from another node the rows of bucket
// here, in one transaction: what the node held for it before is replaced, so
// applying the same rows twice stores them once. Each row must name bucket and
// only columns the table has here.
func applyBucketRows(ctx context.Context, db *sql.DB, bucket string, in BucketRows) error {
	for name := range in {
		if _, ok := lookupBucketTable(name); !ok {
			return fmt.Errorf("rows for unknown table %q", name)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := deleteBucketRowsTx(ctx, tx, bucket); err != nil {
		return err
	}
	for _, t := range bucketTables {
		if len(in[t.name]) == 0 {
			continue
		}
		known, err := tableColumns(ctx, tx, t.name)
		if err != nil {
			return err
		}
		for _, row := range in[t.name] {
			if v := row[t.bucket]; v.Type != "text" || v.Text != bucket {
				return fmt.Errorf("a %s row names another bucket", t.name)
			}
			columns := make([]string, 0, len(row))
			args := make([]any, 0, len(row))
			for c, v := range row {
				if !known[c] || (t.generatedID && c == "id") {
					return fmt.Errorf("column %q of %s does not exist on this node", c, t.name)
				}
				value, err := v.value()
				if err != nil {
					return fmt.Errorf("%s.%s: %w", t.name, c, err)
				}
				columns = append(columns, `"`+c+`"`)
				args = append(args, value)
			}
			query := fmt.Sprintf(`INSERT OR REPLACE INTO %s (%s) VALUES (%s)`, t.name,
				strings.Join(columns, ", "), strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", "))
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("store %s row: %w", t.name, err)
			}
		}
	}
	return tx.Commit()
}

// deleteBucketRows removes the rows of bucket from every bucket table.
func deleteBucketRows(ctx context.Context, db *sql.DB, bucket string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := deleteBucketRowsTx(ctx, tx, bucket); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteBucketRowsTx removes the rows of bucket, referencing rows first.
func deleteBucketRowsTx(ctx context.Context, tx *sql.Tx, bucket string) error {
	for i := len(bucketTables) - 1; i >= 0; i-- {
		t := bucketTables[i]
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, t.name, t.bucket), bucket); err != nil {
			return fmt.Errorf("delete %s rows: %w", t.name, err)
		}
	}
	return nil
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
