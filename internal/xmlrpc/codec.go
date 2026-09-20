// Package xmlrpc is BlogCFC's remote editor API: the MetaWeblog, Blogger
// and Movable Type calls Windows Live Writer, MarsEdit and friends make
// against `POST /xmlrpc` (PLAN §7, §8, §9 X01-X10). The as-is is
// `client/xmlrpc/xmlrpc.cfm` over Roger Benningfield's `xmlrpc.cfc`
// codec; this package is both halves in Go, with the packets' shapes and
// the struct keys kept and the ISO-8859-1 prolog on UTF-8 output fixed
// (PLAN §7 "Bugs fixed rather than ported").
//
// This file is the codec (X01): XML-RPC values to Go values and back,
// with no dependency beyond encoding/xml.
package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Prolog heads every response. The as-is wrote `encoding="ISO-8859-1"`
// above UTF-8 bytes, which is the bug the plan asks us to fix rather
// than port.
const Prolog = `<?xml version="1.0" encoding="UTF-8"?>`

// dateLayout is XML-RPC's dateTime.iso8601 as the as-is wrote it:
// `#DateFormat(d,"yyyymmdd")#T#TimeFormat(d,"HH:mm:ss")#`, with no zone.
const dateLayout = "20060102T15:04:05"

// Member is one field of a struct value. Members are a slice rather than
// a map so a response's keys come out in the order the method built
// them, which makes the packets testable.
type Member struct {
	Name  string
	Value any
}

// Struct is an XML-RPC <struct>.
type Struct []Member

// Get returns the first member with this name, matched case-insensitively
// because the clients disagree about `categoryId` and `categoryid`.
func (s Struct) Get(name string) (any, bool) {
	for _, m := range s {
		if strings.EqualFold(m.Name, name) {
			return m.Value, true
		}
	}
	return nil, false
}

// Has reports whether the struct carries the key at all, which is how
// newPost and editPost tell "no categories sent" from "an empty list".
func (s Struct) Has(name string) bool {
	_, ok := s.Get(name)
	return ok
}

// Str returns a member as text: a string value as itself, anything else
// through the same rendering the encoder would use.
func (s Struct) Str(name string) string {
	v, ok := s.Get(name)
	if !ok {
		return ""
	}
	return asString(v)
}

// Array is an XML-RPC <array>.
type Array []any

// DateTime is a dateTime.iso8601 value. The type carries whether the
// packet said anything about a zone: a bare `20060102T15:04:05` is a
// wall clock the blog reads in its own zone (PLAN §11 "Timezone"),
// while a value with `Z` or an offset is an instant.
type DateTime struct {
	Time    time.Time
	HasZone bool
}

// NewDateTime wraps an instant for encoding.
func NewDateTime(t time.Time) DateTime { return DateTime{Time: t, HasZone: false} }

// InZone resolves the value to UTC: an absolute value as it stands, a
// zoneless one read in loc.
func (d DateTime) InZone(loc *time.Location) time.Time {
	if d.HasZone {
		return d.Time.UTC()
	}
	if loc == nil {
		loc = time.UTC
	}
	t := d.Time
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc).UTC()
}

// Call is a decoded <methodCall>.
type Call struct {
	Method string
	Params []any
}

// Response is a decoded <methodResponse>: either params or a fault,
// never both.
type Response struct {
	Params []any
	Fault  *Fault
}

// Fault is an XML-RPC <fault>, and also the error type every method in
// this package returns when it wants one on the wire.
type Fault struct {
	Code   int
	String string
}

func (f Fault) Error() string { return fmt.Sprintf("xmlrpc fault %d: %s", f.Code, f.String) }

// Encoding.

// EncodeResponse renders a <methodResponse> with one param, prolog
// included. XML-RPC allows exactly one, which is what every method here
// returns.
func EncodeResponse(value any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(Prolog)
	b.WriteString("<methodResponse><params><param>")
	if err := encodeValue(&b, value); err != nil {
		return nil, err
	}
	b.WriteString("</param></params></methodResponse>")
	return b.Bytes(), nil
}

// EncodeFault renders a <fault>, prolog included. It cannot fail: the
// fault's two members are an int and a string.
func EncodeFault(f Fault) []byte {
	var b bytes.Buffer
	b.WriteString(Prolog)
	b.WriteString("<methodResponse><fault><value><struct>")
	b.WriteString("<member><name>faultCode</name><value><int>")
	b.WriteString(strconv.Itoa(f.Code))
	b.WriteString("</int></value></member>")
	b.WriteString("<member><name>faultString</name><value><string>")
	b.WriteString(escapeXML(f.String))
	b.WriteString("</string></value></member>")
	b.WriteString("</struct></value></fault></methodResponse>")
	return b.Bytes()
}

// EncodeCall renders a <methodCall>. Nothing in the server needs it; the
// codec's tests and any later client do.
func EncodeCall(method string, params ...any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(Prolog)
	b.WriteString("<methodCall><methodName>")
	b.WriteString(escapeXML(method))
	b.WriteString("</methodName><params>")
	for _, p := range params {
		b.WriteString("<param>")
		if err := encodeValue(&b, p); err != nil {
			return nil, err
		}
		b.WriteString("</param>")
	}
	b.WriteString("</params></methodCall>")
	return b.Bytes(), nil
}

func encodeValue(b *bytes.Buffer, v any) error {
	b.WriteString("<value>")
	if err := encodeInner(b, v); err != nil {
		return err
	}
	b.WriteString("</value>")
	return nil
}

func encodeInner(b *bytes.Buffer, v any) error {
	switch v := v.(type) {
	case nil:
		b.WriteString("<string></string>")
	case string:
		b.WriteString("<string>")
		b.WriteString(escapeXML(v))
		b.WriteString("</string>")
	case bool:
		if v {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case int:
		fmt.Fprintf(b, "<int>%d</int>", v)
	case int64:
		fmt.Fprintf(b, "<int>%d</int>", v)
	case float64:
		b.WriteString("<double>")
		b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		b.WriteString("</double>")
	case []byte:
		b.WriteString("<base64>")
		b.WriteString(base64.StdEncoding.EncodeToString(v))
		b.WriteString("</base64>")
	case time.Time:
		b.WriteString("<dateTime.iso8601>")
		b.WriteString(v.Format(dateLayout))
		b.WriteString("</dateTime.iso8601>")
	case DateTime:
		b.WriteString("<dateTime.iso8601>")
		b.WriteString(v.Time.Format(dateLayout))
		b.WriteString("</dateTime.iso8601>")
	case Struct:
		b.WriteString("<struct>")
		for _, m := range v {
			b.WriteString("<member><name>")
			b.WriteString(escapeXML(m.Name))
			b.WriteString("</name>")
			if err := encodeValue(b, m.Value); err != nil {
				return err
			}
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	case Array:
		b.WriteString("<array><data>")
		for _, item := range v {
			if err := encodeValue(b, item); err != nil {
				return err
			}
		}
		b.WriteString("</data></array>")
	case []any:
		return encodeInner(b, Array(v))
	case []string:
		items := make(Array, 0, len(v))
		for _, s := range v {
			items = append(items, s)
		}
		return encodeInner(b, items)
	default:
		return fmt.Errorf("xmlrpc: cannot encode %T", v)
	}
	return nil
}

// escapeXML is CFML's XmlFormat: the five predefined entities, with
// whitespace left as typed so an entry body survives a round trip
// readable. Characters XML 1.0 forbids outright are dropped rather than
// written, since a client that sends one would otherwise get a packet no
// parser accepts.
func escapeXML(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			if isXMLChar(r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func isXMLChar(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\r':
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	}
	return false
}

// asString renders a decoded value as the text a caller asked for.
func asString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "1"
		}
		return "0"
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []byte:
		return string(v)
	case DateTime:
		return v.Time.Format(dateLayout)
	default:
		return fmt.Sprint(v)
	}
}

// Decoding.

// DecodeCall parses a <methodCall> packet.
func DecodeCall(data []byte) (*Call, error) {
	d := newDecoder(data)
	if err := expectRoot(d, "methodCall"); err != nil {
		return nil, err
	}
	c := &Call{}
	for {
		se, ok, err := nextStart(d)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		switch se.Name.Local {
		case "methodName":
			s, err := elemText(d)
			if err != nil {
				return nil, err
			}
			c.Method = strings.TrimSpace(s)
		case "params":
			p, err := parseParams(d)
			if err != nil {
				return nil, err
			}
			c.Params = p
		default:
			if err := d.Skip(); err != nil {
				return nil, err
			}
		}
	}
	if c.Method == "" {
		return nil, fmt.Errorf("xmlrpc: methodCall without a methodName")
	}
	return c, nil
}

// DecodeResponse parses a <methodResponse> packet, params or fault.
func DecodeResponse(data []byte) (*Response, error) {
	d := newDecoder(data)
	if err := expectRoot(d, "methodResponse"); err != nil {
		return nil, err
	}
	r := &Response{}
	for {
		se, ok, err := nextStart(d)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		switch se.Name.Local {
		case "params":
			p, err := parseParams(d)
			if err != nil {
				return nil, err
			}
			r.Params = p
		case "fault":
			f, err := parseFault(d)
			if err != nil {
				return nil, err
			}
			r.Fault = f
		default:
			if err := d.Skip(); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

func parseFault(d *xml.Decoder) (*Fault, error) {
	for {
		se, ok, err := nextStart(d)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("xmlrpc: fault without a value")
		}
		if se.Name.Local != "value" {
			if err := d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		v, err := parseValue(d)
		if err != nil {
			return nil, err
		}
		st, ok := v.(Struct)
		if !ok {
			return nil, fmt.Errorf("xmlrpc: fault value is %T, want a struct", v)
		}
		f := &Fault{String: st.Str("faultString")}
		if code, ok := st.Get("faultCode"); ok {
			f.Code, _ = toInt(code)
		}
		// Consume the rest of <fault>.
		for {
			more, ok, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if !ok {
				return f, nil
			}
			_ = more
			if err := d.Skip(); err != nil {
				return nil, err
			}
		}
	}
}

func parseParams(d *xml.Decoder) ([]any, error) {
	var out []any
	for {
		se, ok, err := nextStart(d)
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		if se.Name.Local != "param" {
			if err := d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		var got bool
		for {
			inner, ok, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			if inner.Name.Local != "value" {
				if err := d.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			v, err := parseValue(d)
			if err != nil {
				return nil, err
			}
			if !got {
				out, got = append(out, v), true
			}
		}
		if !got {
			out = append(out, "")
		}
	}
}

// parseValue is called with <value> already consumed and returns having
// consumed its </value>. A value with no typed child is a string, as the
// spec says and as the clients rely on.
func parseValue(d *xml.Decoder) (any, error) {
	var text strings.Builder
	var val any
	var typed bool
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, wrapEOF(err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			if !typed {
				text.Write(t)
			}
		case xml.StartElement:
			if typed {
				if err := d.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			v, err := parseTyped(d, t)
			if err != nil {
				return nil, err
			}
			val, typed = v, true
		case xml.EndElement:
			if typed {
				return val, nil
			}
			return text.String(), nil
		}
	}
}

func parseTyped(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "string":
		return elemText(d)
	case "int", "i4":
		s, err := elemText(d)
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: %s value %q: %w", se.Name.Local, s, err)
		}
		return n, nil
	case "boolean":
		s, err := elemText(d)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "1", "true", "yes":
			return true, nil
		case "0", "false", "no", "":
			return false, nil
		}
		return nil, fmt.Errorf("xmlrpc: boolean value %q", s)
	case "double":
		s, err := elemText(d)
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: double value %q: %w", s, err)
		}
		return f, nil
	case "dateTime.iso8601":
		s, err := elemText(d)
		if err != nil {
			return nil, err
		}
		return parseDateTime(s)
	case "base64":
		s, err := elemText(d)
		if err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(stripSpace(s))
		if err != nil {
			return nil, fmt.Errorf("xmlrpc: base64 value: %w", err)
		}
		return raw, nil
	case "nil":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return nil, nil
	case "struct":
		return parseStruct(d)
	case "array":
		return parseArray(d)
	}
	return nil, fmt.Errorf("xmlrpc: unknown value type <%s>", se.Name.Local)
}

func parseStruct(d *xml.Decoder) (Struct, error) {
	out := Struct{}
	for {
		se, ok, err := nextStart(d)
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		if se.Name.Local != "member" {
			if err := d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		var m Member
		for {
			inner, ok, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			switch inner.Name.Local {
			case "name":
				s, err := elemText(d)
				if err != nil {
					return nil, err
				}
				m.Name = strings.TrimSpace(s)
			case "value":
				v, err := parseValue(d)
				if err != nil {
					return nil, err
				}
				m.Value = v
			default:
				if err := d.Skip(); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, m)
	}
}

func parseArray(d *xml.Decoder) (Array, error) {
	out := Array{}
	for {
		se, ok, err := nextStart(d)
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		if se.Name.Local != "data" {
			if err := d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		for {
			inner, ok, err := nextStart(d)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			if inner.Name.Local != "value" {
				if err := d.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			v, err := parseValue(d)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
}

// dateZone matches the zone suffix the spec says is not there and half
// the clients send anyway.
var dateZone = regexp.MustCompile(`(?i)(Z|[+-]\d{2}:?\d{2})$`)

// dateLayouts are the spellings seen in the wild: the compact form the
// spec asks for, the dashed form Windows Live Writer sends (the as-is
// patched it up by hand, "fix by ddblock"), with or without the colons
// in the time.
var dateLayouts = []string{
	"20060102T15:04:05",
	"20060102T150405",
	"2006-01-02T15:04:05",
	"2006-01-02T150405",
	"2006-01-02 15:04:05",
	"20060102T15:04",
	"20060102",
	"2006-01-02",
}

func parseDateTime(raw string) (DateTime, error) {
	s := strings.TrimSpace(raw)
	loc := time.UTC
	hasZone := false
	if suffix := dateZone.FindString(s); suffix != "" {
		s = strings.TrimSuffix(s, suffix)
		hasZone = true
		if !strings.EqualFold(suffix, "Z") {
			digits := strings.ReplaceAll(suffix[1:], ":", "")
			if len(digits) != 4 {
				return DateTime{}, fmt.Errorf("xmlrpc: dateTime offset %q", suffix)
			}
			h, _ := strconv.Atoi(digits[:2])
			m, _ := strconv.Atoi(digits[2:])
			secs := h*3600 + m*60
			if suffix[0] == '-' {
				secs = -secs
			}
			loc = time.FixedZone("", secs)
		}
	}
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return DateTime{Time: t, HasZone: hasZone}, nil
		}
	}
	return DateTime{}, fmt.Errorf("xmlrpc: dateTime value %q is not ISO 8601", raw)
}

// toInt reads a decoded value as a whole number: the clients send
// `numberOfPosts` as an int, an i4, a double or a plain string.
func toInt(v any) (int, bool) {
	switch v := v.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}

// toBool reads a decoded value as CFML's isBoolean would: the booleans,
// the numbers and the yes/no strings the clients mix.
func toBool(v any) (bool, bool) {
	switch v := v.(type) {
	case bool:
		return v, true
	case int:
		return v != 0, true
	case float64:
		return v != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes":
			return true, true
		case "0", "false", "no":
			return false, true
		}
	}
	return false, false
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
}

// elemText reads the character data of the element whose start tag was
// just consumed, discarding any nested markup, and consumes its end tag.
func elemText(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return "", wrapEOF(err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			_ = t
			if err := d.Skip(); err != nil {
				return "", err
			}
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

// nextStart returns the next start tag inside the element being read. It
// reports ok = false once that element's end tag arrives, which it
// consumes.
func nextStart(d *xml.Decoder) (xml.StartElement, bool, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				return xml.StartElement{}, false, nil
			}
			return xml.StartElement{}, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return t, true, nil
		case xml.EndElement:
			return xml.StartElement{}, false, nil
		}
	}
}

func expectRoot(d *xml.Decoder, name string) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return wrapEOF(err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local != name {
				return fmt.Errorf("xmlrpc: root element is <%s>, want <%s>", se.Name.Local, name)
			}
			return nil
		}
	}
}

func wrapEOF(err error) error {
	if err == io.EOF {
		return fmt.Errorf("xmlrpc: packet ends early")
	}
	return err
}

// newDecoder parses the packet leniently about its declared encoding:
// UTF-8 and US-ASCII are the body's own bytes, and ISO-8859-1 (which the
// as-is itself declared) is widened a byte at a time. Anything else is
// refused rather than mis-read.
func newDecoder(data []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(charset) {
		case "utf-8", "utf8", "us-ascii", "ascii", "":
			return input, nil
		case "iso-8859-1", "latin1", "iso8859-1", "windows-1252", "cp1252":
			raw, err := io.ReadAll(input)
			if err != nil {
				return nil, err
			}
			var b bytes.Buffer
			b.Grow(len(raw))
			var buf [utf8.UTFMax]byte
			for _, c := range raw {
				n := utf8.EncodeRune(buf[:], rune(c))
				b.Write(buf[:n])
			}
			return bytes.NewReader(b.Bytes()), nil
		}
		return nil, fmt.Errorf("xmlrpc: unsupported charset %q", charset)
	}
	return d
}
