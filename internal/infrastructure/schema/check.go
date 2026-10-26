package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"regexp"
	"strings"
	"time"
)

type Metadata struct {
	Engine  string
	Columns []MetadataColumn
	Indexes []MetadataIndex
}
type MetadataColumn struct {
	Name     string `gorm:"column:column_name"`
	Type     string `gorm:"column:column_type"`
	Nullable string `gorm:"column:is_nullable"`
}
type MetadataIndex struct {
	Name      string        `gorm:"column:index_name"`
	NonUnique int           `gorm:"column:non_unique"`
	Sequence  int           `gorm:"column:seq_in_index"`
	Column    string        `gorm:"column:column_name"`
	Prefix    sql.NullInt64 `gorm:"column:sub_part"`
}
type Reader interface {
	Read(context.Context, string) (Metadata, error)
}
type SQLReader struct{ DB *gorm.DB }

var bigintColumn = regexp.MustCompile(`^bigint(?:\([0-9]+\))?( unsigned)?$`)

func (r SQLReader) Read(ctx context.Context, table string) (Metadata, error) {
	if r.DB == nil || TableOwner(table) == "" {
		return Metadata{}, errors.New("schema reader target invalid")
	}
	db := r.DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	m := Metadata{}
	if err := db.Raw("SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? LIMIT 1", table).Scan(&m.Engine).Error; err != nil {
		return m, errors.New("schema table metadata unavailable")
	}
	if m.Engine == "" {
		return m, nil
	}
	if err := db.Raw("SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION LIMIT 512", table).Scan(&m.Columns).Error; err != nil {
		return m, errors.New("schema columns unavailable")
	}
	if err := db.Raw("SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, SUB_PART FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY INDEX_NAME, SEQ_IN_INDEX LIMIT 512", table).Scan(&m.Indexes).Error; err != nil {
		return m, errors.New("schema indexes unavailable")
	}
	if len(m.Columns) >= 512 || len(m.Indexes) >= 512 {
		return m, errors.New("schema metadata bound exceeded")
	}
	return m, nil
}
func Validate(table Table, metadata Metadata) error {
	if metadata.Engine != "InnoDB" {
		return fmt.Errorf("schema %s requires InnoDB", table.Name)
	}
	columns := map[string]MetadataColumn{}
	for _, column := range metadata.Columns {
		if _, ok := columns[column.Name]; ok {
			return errors.New("duplicate schema column metadata")
		}
		columns[column.Name] = column
	}
	for _, required := range table.Columns {
		actual, ok := columns[required.Name]
		kind := strings.ToLower(actual.Type)
		if !ok || ((required.Unsigned || required.Signed64) && !bigintColumn.MatchString(kind)) || (required.Unsigned && !strings.HasSuffix(kind, " unsigned")) || (required.Signed64 && strings.HasSuffix(kind, " unsigned")) || (required.NotNull && actual.Nullable != "NO") {
			return fmt.Errorf("schema %s column %s missing or incompatible", table.Name, required.Name)
		}
	}
	indexes := map[string][]MetadataIndex{}
	for _, index := range metadata.Indexes {
		indexes[index.Name] = append(indexes[index.Name], index)
	}
	for _, required := range table.Indexes {
		actual := indexes[required.Name]
		if len(actual) != len(required.Columns) {
			return fmt.Errorf("schema %s index %s shape mismatch", table.Name, required.Name)
		}
		for i, column := range required.Columns {
			if actual[i].Sequence != i+1 || actual[i].Column != column || actual[i].Prefix.Valid || (required.Unique && actual[i].NonUnique != 0) {
				return fmt.Errorf("schema %s index %s incompatible", table.Name, required.Name)
			}
		}
	}
	return nil
}
func Check(ctx context.Context, reader Reader, tables []Table) error {
	if reader == nil || len(tables) == 0 || len(tables) > 32 {
		return errors.New("invalid schema check scope")
	}
	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return err
		}
		m, err := reader.Read(ctx, table.Name)
		if err != nil {
			return fmt.Errorf("schema %s metadata unavailable", table.Name)
		}
		if err := Validate(table, m); err != nil {
			return err
		}
	}
	return nil
}
func CheckRuntime(ctx context.Context, db *gorm.DB, role string, extra ...interface{}) error {
	models, err := RuntimeModels(role)
	if err != nil {
		return err
	}
	models = append(models, extra...)
	tables, err := Describe(models)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if !allowedRuntimeTable(role, table.Name) {
			return errors.New("runtime schema scope denied")
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return Check(bounded, SQLReader{DB: db}, tables)
}
