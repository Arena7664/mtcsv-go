package mtcsv

import (
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Struct tags recognized by this package. See the package documentation.
const (
	tagKey     = "mtcsv"
	tagKeyType = "mtcsv.type"
	tagKeyDoc  = "mtcsv.doc"
	tagKeyMeta = "mtcsv.meta"
)

// tagOptions is the comma-separated list following a tag's name.
type tagOptions []string

func parseTag(tag string) (string, tagOptions) {
	name, opt, _ := strings.Cut(tag, ",")
	if opt == "" {
		return name, nil
	}
	return name, strings.Split(opt, ",")
}

// Contains reports whether the options include the given flag.
func (o tagOptions) Contains(flag string) bool {
	for _, s := range o {
		if s == flag {
			return true
		}
	}
	return false
}

// pairs returns the key=value options, which supply table metadata.
func (o tagOptions) pairs() Metadata {
	var meta Metadata
	for _, s := range o {
		if k, v, ok := strings.Cut(s, "="); ok && k != "" {
			meta = append(meta, MetaEntry{Key: k, Value: v})
		}
	}
	return meta
}

// A field is one column of a row struct.
type field struct {
	name      string // column name
	index     []int  // path to the field, for embedded structs
	typ       reflect.Type
	omitEmpty bool
	colType   string // mtcsv.type
	doc       string // mtcsv.doc
	tagged    bool
}

// structFields is the cached column layout of a row struct type.
type structFields struct {
	list       []field
	byName     map[string]int
	byFoldName map[string]int
}

// lookup finds the field for a column name: exact match first, then a
// case-insensitive match, mirroring encoding/json.
func (sf *structFields) lookup(name string) *field {
	if i, ok := sf.byName[name]; ok {
		return &sf.list[i]
	}
	if i, ok := sf.byFoldName[strings.ToLower(name)]; ok {
		return &sf.list[i]
	}
	return nil
}

var fieldCache sync.Map // reflect.Type -> *structFields

func cachedFields(t reflect.Type) *structFields {
	if f, ok := fieldCache.Load(t); ok {
		return f.(*structFields)
	}
	f, _ := fieldCache.LoadOrStore(t, typeFields(t))
	return f.(*structFields)
}

// typeFields returns the columns of a struct type: its exported fields in
// declaration order, with untagged embedded structs flattened in place.
func typeFields(t reflect.Type) *structFields {
	type queued struct {
		typ   reflect.Type
		index []int
		depth int
	}
	var (
		fields  []field
		current = []queued{{typ: t}}
		next    []queued
		visited = map[reflect.Type]bool{}
	)

	for depth := 0; len(current) > 0; depth++ {
		for _, q := range current {
			if visited[q.typ] {
				continue
			}
			visited[q.typ] = true

			for i := 0; i < q.typ.NumField(); i++ {
				sf := q.typ.Field(i)
				if sf.Anonymous {
					ft := sf.Type
					if ft.Kind() == reflect.Pointer {
						ft = ft.Elem()
					}
					// An untagged embedded struct is flattened; a tagged one
					// (or an embedded non-struct) is an ordinary column.
					if _, ok := sf.Tag.Lookup(tagKey); !ok && ft.Kind() == reflect.Struct && !isCellType(ft) {
						if sf.IsExported() || ft.Kind() == reflect.Struct {
							next = append(next, queued{ft, appendIndex(q.index, i), depth + 1})
						}
						continue
					}
				}
				if !sf.IsExported() {
					continue
				}
				tag, hasTag := sf.Tag.Lookup(tagKey)
				if tag == "-" {
					continue
				}
				name, opts := parseTag(tag)
				if name == "" {
					name = sf.Name
				}
				fields = append(fields, field{
					name:      name,
					index:     appendIndex(q.index, i),
					typ:       sf.Type,
					omitEmpty: opts.Contains("omitempty"),
					colType:   sf.Tag.Get(tagKeyType),
					doc:       sf.Tag.Get(tagKeyDoc),
					tagged:    hasTag && name != sf.Name,
				})
			}
		}
		current, next = next, nil
	}

	fields = resolveConflicts(fields)

	// Columns follow the struct's declaration order, with a flattened embedded
	// struct's fields sitting where the embedded field itself appears.
	sort.Slice(fields, func(i, j int) bool {
		return lessIndex(fields[i].index, fields[j].index)
	})

	sf := &structFields{
		list:       fields,
		byName:     make(map[string]int, len(fields)),
		byFoldName: make(map[string]int, len(fields)),
	}
	for i, f := range fields {
		if _, ok := sf.byName[f.name]; !ok {
			sf.byName[f.name] = i
		}
		if lower := strings.ToLower(f.name); !mapHas(sf.byFoldName, lower) {
			sf.byFoldName[lower] = i
		}
	}
	return sf
}

// resolveConflicts drops fields hidden by Go's embedding rules: for a given
// column name the shallowest field wins, a tagged field beats an untagged one
// at the same depth, and an unresolved tie removes the name entirely.
func resolveConflicts(fields []field) []field {
	byName := map[string][]int{}
	for i, f := range fields {
		byName[f.name] = append(byName[f.name], i)
	}
	drop := map[int]bool{}
	for _, idx := range byName {
		if len(idx) == 1 {
			continue
		}
		best := idx[0]
		tie := false
		for _, i := range idx[1:] {
			switch {
			case len(fields[i].index) < len(fields[best].index):
				best, tie = i, false
			case len(fields[i].index) > len(fields[best].index):
			case fields[i].tagged && !fields[best].tagged:
				best, tie = i, false
			case fields[i].tagged == fields[best].tagged:
				tie = true
			}
		}
		for _, i := range idx {
			if i != best || tie {
				drop[i] = true
			}
		}
	}
	if len(drop) == 0 {
		return fields
	}
	out := fields[:0]
	for i, f := range fields {
		if !drop[i] {
			out = append(out, f)
		}
	}
	return out
}

// lessIndex orders two field index paths as the fields appear in the source.
func lessIndex(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func mapHas(m map[string]int, key string) bool {
	_, ok := m[key]
	return ok
}

func appendIndex(index []int, i int) []int {
	out := make([]int, len(index)+1)
	copy(out, index)
	out[len(index)] = i
	return out
}

// fieldByIndex walks an index path, allocating nil pointers on the way when
// alloc is set. It returns the zero Value if a nil pointer is met and alloc is
// false.
func fieldByIndex(v reflect.Value, index []int, alloc bool) reflect.Value {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !alloc || !v.CanSet() {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}

// A tableTag is the table-level configuration of a container struct field, or
// of a top-level value.
type tableTag struct {
	name string
	// named reports that the name was chosen explicitly - by a struct tag, or
	// by the caller of EncodeTable or AddTable - rather than defaulting to the
	// Go field name. An explicit name outranks a TableDescriptor.
	named bool
	anon  bool
	doc   string
	meta  Metadata
}

// tableField is one table of a container struct.
type tableField struct {
	tableTag
	index []int
	typ   reflect.Type
}

var tableFieldCache sync.Map // reflect.Type -> []tableField

func cachedTableFields(t reflect.Type) []tableField {
	if f, ok := tableFieldCache.Load(t); ok {
		return f.([]tableField)
	}
	f, _ := tableFieldCache.LoadOrStore(t, tableFields(t))
	return f.([]tableField)
}

func tableFields(t reflect.Type) []tableField {
	var out []tableField
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get(tagKey)
		if tag == "-" {
			continue
		}
		name, opts := parseTag(tag)
		named := name != ""
		if name == "" {
			name = sf.Name
		}
		meta := opts.pairs()
		if s := sf.Tag.Get(tagKeyMeta); s != "" {
			meta = append(meta, ParseMetadata(s)...)
		}
		tt := tableTag{
			name:  name,
			named: named,
			anon:  opts.Contains("anon") || opts.Contains("anonymous"),
			doc:   sf.Tag.Get(tagKeyDoc),
			meta:  meta,
		}
		// A row type that describes its own table supplies whatever the field
		// did not. Both the encoder and the decoder use the result, so a
		// self-naming type round trips through a container struct.
		if info, ok := descriptorFor(sf.Type); ok {
			if !tt.named && info.Name != "" {
				tt.name = info.Name
			}
			if !tt.anon && !tt.named {
				tt.anon = info.Anonymous
			}
			if tt.doc == "" {
				tt.doc = info.Description
			}
			if len(tt.meta) == 0 {
				tt.meta = info.Meta
			}
		}
		out = append(out, tableField{tableTag: tt, index: []int{i}, typ: sf.Type})
	}
	return out
}

// sortedKeys returns a map's string keys in sorted order, so output is
// deterministic.
func sortedKeys(v reflect.Value) []reflect.Value {
	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	return keys
}

// descriptorFor returns the TableInfo of the row type behind a table-shaped
// value: the element type of a slice or array, or the type itself. It reports
// false when that type does not implement TableDescriptor.
func descriptorFor(t reflect.Type) (TableInfo, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		t = t.Elem()
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	case reflect.Map, reflect.Interface:
		return TableInfo{}, false
	}
	switch {
	case t.Implements(tableDescType):
		return reflect.New(t).Elem().Interface().(TableDescriptor).MTCSVTable(), true
	case reflect.PointerTo(t).Implements(tableDescType):
		return reflect.New(t).Interface().(TableDescriptor).MTCSVTable(), true
	}
	return TableInfo{}, false
}
