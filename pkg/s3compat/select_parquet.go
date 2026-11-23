package s3compat

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

// maxSelectRowGroup is the largest row group S3 Select reads, uncompressed:
// 512 MB, as in AWS.
var maxSelectRowGroup int64 = 512 << 20

var errSelectRowGroupTooLarge = errors.New("a row group is larger than the 512 MB S3 Select reads uncompressed")

// errSelectParquetCodec: a column is compressed in a way S3 Select does not
// read: LZO, or the LZ4 Parquet deprecated for LZ4_RAW.
var errSelectParquetCodec = errors.New("a column is compressed with a codec S3 Select does not read: it reads uncompressed, Snappy, GZIP, ZSTD, Brotli and LZ4_RAW columns")

// parquetCodecRead reports whether S3 Select reads columns compressed with
// codec.
func parquetCodecRead(codec format.CompressionCodec) bool {
	switch codec {
	case format.Uncompressed, format.Snappy, format.Gzip, format.Zstd, format.Brotli, format.Lz4Raw:
		return true
	}
	return false
}

// parquetColumn is one top-level field of a Parquet object as the query sees
// it: a column of a type, and how a value of the field is stored in it.
type parquetColumn struct {
	name    string
	sqlType string
	store   func(any) any
	// leaf, when not negative, is the column store takes the value of as it
	// was read, a parquet.Value: a decimal is exact only from there.
	leaf int
}

// parquetColumnOf decides how a field is stored. Numbers stay numbers, so they
// compare and sort as numbers; dates and times become ISO 8601 text; a group, a
// list or a map becomes its JSON, as a nested JSON value does.
func parquetColumnOf(schema *parquet.Schema, field parquet.Field) parquetColumn {
	c := parquetColumn{name: field.Name(), sqlType: "TEXT", store: parquetText, leaf: -1}
	if !field.Leaf() || field.Repeated() {
		c.store = parquetJSON
		return c
	}
	if lt := field.Type().LogicalType(); lt != nil {
		switch t := lt.Value.(type) {
		case *format.TimestampType:
			c.store = parquetTimestamp(t.Unit, t.IsAdjustedToUTC)
			return c
		case *format.DateType:
			c.store = parquetDate
			return c
		case *format.TimeType:
			c.store = parquetTimeOfDay(t.Unit)
			return c
		case *format.DecimalType:
			if leaf, ok := schema.Lookup(field.Name()); ok {
				c.sqlType, c.store, c.leaf = "NUMERIC", parquetDecimal(t.Scale), leaf.ColumnIndex
			}
			return c
		case *format.UUIDType:
			c.store = parquetUUID
			return c
		case *format.IntType:
			c.sqlType, c.store = "INTEGER", parquetNumber
			return c
		case *format.StringType, *format.EnumType, *format.JsonType:
			return c
		}
	}
	switch field.Type().Kind() {
	case parquet.Int32, parquet.Int64:
		c.sqlType, c.store = "INTEGER", parquetNumber
	case parquet.Float, parquet.Double:
		c.sqlType, c.store = "REAL", parquetNumber
	}
	return c
}

// parquetNumber stores a number as itself; one no SQLite integer holds, as
// text.
func parquetNumber(v any) any {
	switch n := v.(type) {
	case uint64:
		if n > 1<<63-1 {
			return strconv.FormatUint(n, 10)
		}
		return int64(n)
	case int32, int64, uint32, float32, float64, int, uint, int8, int16, uint8, uint16:
		return n
	}
	return parquetText(v)
}

// parquetText stores a value as the text S3 Select returns for it.
func parquetText(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return t
	case []byte:
		return string(t)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	case deprecated.Int96:
		return parquetInt96(t)
	}
	return fmt.Sprint(v)
}

func parquetJSON(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// parquetDecimal stores a decimal as its exact text, which the NUMERIC
// column takes as a number: the unscaled integer, in an INT32, an INT64 or
// big-endian two's complement bytes, over 10 to the scale.
func parquetDecimal(scale int32) func(any) any {
	return func(v any) any {
		value, ok := v.(parquet.Value)
		if !ok || value.IsNull() {
			return nil
		}
		unscaled := new(big.Int)
		switch value.Kind() {
		case parquet.Int32:
			unscaled.SetInt64(int64(value.Int32()))
		case parquet.Int64:
			unscaled.SetInt64(value.Int64())
		case parquet.ByteArray, parquet.FixedLenByteArray:
			b := value.ByteArray()
			unscaled.SetBytes(b)
			if len(b) > 0 && b[0]&0x80 != 0 {
				unscaled.Sub(unscaled, new(big.Int).Lsh(big.NewInt(1), uint(len(b))*8))
			}
		default:
			return parquetText(value.String())
		}
		digits := new(big.Int).Abs(unscaled).String()
		if scale > 0 {
			if pad := int(scale) + 1 - len(digits); pad > 0 {
				digits = strings.Repeat("0", pad) + digits
			}
			digits = digits[:len(digits)-int(scale)] + "." + digits[len(digits)-int(scale):]
		}
		if unscaled.Sign() < 0 {
			return "-" + digits
		}
		return digits
	}
}

// parquetUUID stores a UUID in its usual form, as AWS returns it.
func parquetUUID(v any) any {
	var b []byte
	switch t := v.(type) {
	case [16]byte:
		b = t[:]
	case []byte:
		b = t
	}
	if len(b) != 16 {
		return parquetText(v)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// instant is n counts of unit after the Unix epoch, without the overflow of n
// in nanoseconds an instant after 2262, as 9999-12-31, would have.
func instant(unit format.TimeUnit, n int64) time.Time {
	switch unit.Value.(type) {
	case *format.MilliSeconds:
		return time.UnixMilli(n).UTC()
	case *format.MicroSeconds:
		return time.UnixMicro(n).UTC()
	}
	return time.Unix(0, n).UTC()
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int32:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// parquetTimestamp renders an instant; one not adjusted to UTC is a local
// time and is written without a zone.
func parquetTimestamp(unit format.TimeUnit, utc bool) func(any) any {
	return func(v any) any {
		n, ok := asInt64(v)
		if !ok {
			return parquetText(v)
		}
		t := instant(unit, n)
		if !utc {
			return t.Format("2006-01-02T15:04:05.999999999")
		}
		return t.Format(time.RFC3339Nano)
	}
}

func parquetDate(v any) any {
	n, ok := asInt64(v)
	if !ok {
		return parquetText(v)
	}
	return time.Unix(n*86400, 0).UTC().Format("2006-01-02")
}

func parquetTimeOfDay(unit format.TimeUnit) func(any) any {
	return func(v any) any {
		n, ok := asInt64(v)
		if !ok {
			return parquetText(v)
		}
		return instant(unit, n).Format("15:04:05.999999999")
	}
}

// parquetInt96 renders the INT96 timestamp of Hive, Impala and Spark: the
// nanoseconds of the day, then the Julian day.
func parquetInt96(v any) any {
	i, ok := v.(deprecated.Int96)
	if !ok {
		return parquetText(v)
	}
	nanos := int64(uint64(i[1])<<32 | uint64(i[0]))
	days := int64(i[2]) - 2440588 // the Julian day of 1970-01-01
	return time.Unix(days*86400, nanos).UTC().Format(time.RFC3339Nano)
}

// storedLength is about how much of a record a value takes.
func storedLength(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case string:
		return int64(len(t))
	}
	return 8
}

// loadParquet reads a Parquet object into s3object, one row at a time. Its
// row groups are held to maxSelectRowGroup and all of them to
// maxSelectDecompressed, as their metadata states, before any is read; a
// record to maxSelectRecord. processed is what the row groups hold
// uncompressed. A file the reader cannot make sense of is refused, whatever
// way it fails.
func loadParquet(db *sql.DB, r io.ReaderAt, size int64) (cols []string, processed int64, err error) {
	defer func() {
		if p := recover(); p != nil {
			cols, err = nil, fmt.Errorf("parsing Parquet: %v", p)
		}
	}()

	file, err := parquet.OpenFile(r, size)
	if err != nil {
		return nil, 0, fmt.Errorf("parsing Parquet: %w", err)
	}
	for _, group := range file.Metadata().RowGroups {
		if group.TotalByteSize < 0 || group.TotalByteSize > maxSelectRowGroup {
			return nil, 0, errSelectRowGroupTooLarge
		}
		for _, column := range group.Columns {
			if !parquetCodecRead(column.MetaData.Codec) {
				return nil, 0, errSelectParquetCodec
			}
		}
		processed += group.TotalByteSize
		if processed > maxSelectDecompressed {
			return nil, processed, errSelectTooLarge
		}
	}

	schema := file.Schema()
	fields := schema.Fields()
	columns := make([]parquetColumn, len(fields))
	names, types := make([]string, len(fields)), make([]string, len(fields))
	for i, field := range fields {
		columns[i] = parquetColumnOf(schema, field)
		names[i], types[i] = columns[i].name, columns[i].sqlType
	}

	writer := newRowWriter(db)
	defer writer.rollback()
	if err := writer.typedColumns(names, types); err != nil {
		return nil, processed, err
	}

	reader := parquet.NewReader(file)
	defer reader.Close() //nolint:errcheck
	values := make([]any, len(columns))
	read := make([]parquet.Row, 1)
	for {
		n, readErr := reader.ReadRows(read)
		if readErr != nil && readErr != io.EOF {
			return nil, processed, fmt.Errorf("parsing Parquet: %w", readErr)
		}
		if n == 0 {
			break
		}
		row := map[string]any{}
		if err := schema.Reconstruct(&row, read[0]); err != nil {
			return nil, processed, fmt.Errorf("parsing Parquet: %w", err)
		}
		var length int64
		for i, c := range columns {
			if c.leaf >= 0 {
				values[i] = c.store(leafValue(read[0], c.leaf))
			} else {
				values[i] = c.store(row[c.name])
			}
			length += storedLength(values[i])
		}
		if length > maxSelectRecord {
			return nil, processed, errSelectRecordTooLarge
		}
		if err := writer.insert(values); err != nil {
			return nil, processed, err
		}
	}

	if err := writer.commit(); err != nil {
		return nil, processed, err
	}
	return names, processed, nil
}

// leafValue is the value of column leaf in row, which holds one for a field
// that is neither a group nor repeated.
func leafValue(row parquet.Row, leaf int) any {
	for _, v := range row {
		if v.Column() == leaf {
			return v
		}
	}
	return nil
}

// loadParquetObject loads a Parquet object, which is read from its end: it is
// copied first to a temporary file of the storage root, named like the
// query's database.
func (h *Handler) loadParquetObject(db *sql.DB, object io.Reader) (int64, error) {
	spool, err := os.CreateTemp(h.dataDir, "maxiofs-upload-select-*.parquet")
	if err != nil {
		return 0, err
	}
	defer os.Remove(spool.Name()) //nolint:errcheck
	defer spool.Close()           //nolint:errcheck
	size, err := io.Copy(spool, object)
	if err != nil {
		return 0, err
	}
	_, processed, err := loadParquet(db, spool, size)
	return processed, err
}
