package mcp

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// orderedObj is a JSON object that remembers key insertion order. The MCP
// renderers must match Python's json.dumps output byte-for-byte, so plain Go
// maps (unordered) cannot be used.
type orderedObj struct {
	keys []string
	vals map[string]any
}

func newObj() *orderedObj {
	return &orderedObj{vals: map[string]any{}}
}

func (o *orderedObj) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *orderedObj) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

func (o *orderedObj) len() int { return len(o.keys) }

// pyJSONString mirrors Python's json.dumps string escaping. With ensureASCII
// (Python's ensure_ascii=True), every non-ASCII rune is emitted as \uXXXX.
func pyJSONString(s string, ensureASCII bool) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\b':
			b.WriteString("\\b")
		case '\f':
			b.WriteString("\\f")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			switch {
			case r < 0x20:
				fmt.Fprintf(&b, "\\u%04x", r)
			case ensureASCII && r > 0x7f:
				writeUnicodeEscape(&b, r)
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func writeUnicodeEscape(b *strings.Builder, r rune) {
	if r > 0xffff {
		r -= 0x10000
		hi := 0xd800 + (r >> 10)
		lo := 0xdc00 + (r & 0x3ff)
		fmt.Fprintf(b, "\\u%04x\\u%04x", hi, lo)
		return
	}
	fmt.Fprintf(b, "\\u%04x", r)
}

// marshalPy serializes v the way Python's json.dumps(v, indent=2) does, with
// the given base indentation (used when embedding a value at column > 0).
func marshalPy(v any, indent int, ensureASCII bool) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return pyJSONString(t, ensureASCII)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case *orderedObj:
		if t.len() == 0 {
			return "{}"
		}
		var b strings.Builder
		b.WriteString("{\n")
		for i, k := range t.keys {
			b.WriteString(strings.Repeat(" ", indent+2))
			b.WriteString(pyJSONString(k, ensureASCII))
			b.WriteString(": ")
			b.WriteString(marshalPy(t.vals[k], indent+2, ensureASCII))
			if i != len(t.keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat(" ", indent))
		b.WriteByte('}')
		return b.String()
	case []any:
		if len(t) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		for i, item := range t {
			b.WriteString(strings.Repeat(" ", indent+2))
			b.WriteString(marshalPy(item, indent+2, ensureASCII))
			if i != len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat(" ", indent))
		b.WriteByte(']')
		return b.String()
	default:
		panic(fmt.Sprintf("marshalPy: unsupported type %T", v))
	}
}

// jsonValidUTF8 reports whether s is valid UTF-8 (used to keep surrogate
// handling honest in tests).
func jsonValidUTF8(s string) bool { return utf8.ValidString(s) }
