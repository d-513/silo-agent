package db

import (
	"reflect"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// CleanText makes s storable in a Postgres text column: invalid UTF-8 becomes
// U+FFFD and NUL bytes are dropped (Postgres rejects both; terminal output and
// tool results carry them).
func CleanText(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return s
}

// registerScrub cleans every string a create/update writes, so no call site
// has to remember to.
func registerScrub(gdb *gorm.DB) error {
	if err := gdb.Callback().Create().Before("gorm:create").Register("silo:scrub", scrub); err != nil {
		return err
	}
	return gdb.Callback().Update().Before("gorm:update").Register("silo:scrub", scrub)
}

func scrub(tx *gorm.DB) {
	st := tx.Statement
	if m, ok := st.Dest.(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				m[k] = CleanText(s)
			}
		}
	}
	if st.Schema == nil {
		return
	}
	rv := reflect.Indirect(st.ReflectValue)
	switch rv.Kind() {
	case reflect.Struct:
		scrubStruct(rv)
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if el := reflect.Indirect(rv.Index(i)); el.Kind() == reflect.Struct {
				scrubStruct(el)
			}
		}
	}
}

func scrubStruct(v reflect.Value) {
	if !v.CanSet() {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.String && f.CanSet() {
			if s := f.String(); s != CleanText(s) {
				f.SetString(CleanText(s))
			}
		}
	}
}
