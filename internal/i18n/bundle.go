// Package i18n serves BlogCFC's resource bundles: Java `.properties`
// files, read with their escapes, looked up by key and formatted with
// `{1}`-style placeholders (PLAN §9 R05, §12 "Strings").
package i18n

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// DefaultLocale is the only bundle shipped today. M2 adds de_DE; until
// then New answers every locale with this one.
const DefaultLocale = "en_US"

// files holds the bundles. main_en_US.properties is BlogCFC's own file,
// copied verbatim (see README.md in this directory).
//
//go:embed main_en_US.properties
var files embed.FS

// Bundle is one locale's strings. It is read-only after New and safe for
// concurrent use.
type Bundle struct {
	locale string
	values map[string]string
}

var (
	loadOnce sync.Once
	loaded   *Bundle
	loadErr  error
)

// New returns the bundle for a locale. Any locale but en_US falls back to
// en_US, which is the only bundle in M1. The parse happens once.
func New(locale string) *Bundle {
	loadOnce.Do(func() {
		b, err := parseFile("main_en_US.properties")
		if err != nil {
			loadErr = err
			loaded = &Bundle{locale: DefaultLocale, values: map[string]string{}}
			return
		}
		loaded = b
	})
	_ = locale // every locale is en_US until M2 adds de_DE.
	return loaded
}

// LoadError reports a failure to parse the embedded bundle. It can only
// fire if the embedded file is corrupt, so callers may check it at start.
func LoadError() error { _ = New(DefaultLocale); return loadErr }

// Locale is the bundle's locale name.
func (b *Bundle) Locale() string { return b.locale }

// Has reports whether the key exists.
func (b *Bundle) Has(key string) bool {
	if b == nil {
		return false
	}
	_, ok := b.values[key]
	return ok
}

// T returns the string for a key with its placeholders filled in. An
// unknown key returns the key itself, which is visible in a page and
// greppable, rather than an empty string that hides the mistake.
//
// Placeholders are BlogCFC's `{1}`-style, one-based: T("hi", "Ray") turns
// "Hello {1}" into "Hello Ray". A placeholder with no argument is left
// alone, so a half-supplied string still shows what it wanted.
func (b *Bundle) T(key string, args ...any) string {
	if b == nil {
		return key
	}
	v, ok := b.values[key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return v
	}
	return substitute(v, args)
}

// substitute replaces {1}, {2}, ... with the arguments.
func substitute(s string, args []any) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '{' {
			out.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			out.WriteString(s[i:])
			break
		}
		end += i
		n, err := strconv.Atoi(s[i+1 : end])
		if err != nil || n < 1 || n > len(args) {
			out.WriteString(s[i : end+1])
			i = end + 1
			continue
		}
		fmt.Fprintf(&out, "%v", args[n-1])
		i = end + 1
	}
	return out.String()
}

// parseFile reads one embedded bundle.
func parseFile(name string) (*Bundle, error) {
	raw, err := files.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("i18n: read %s: %w", name, err)
	}
	locale := strings.TrimSuffix(strings.TrimPrefix(name, "main_"), ".properties")
	return &Bundle{locale: locale, values: Parse(string(raw))}, nil
}

// Parse reads the Java `.properties` format: `key=value` or `key:value` or
// `key value`, `#` and `!` comment lines, a trailing `\` continuing the
// value on the next line, and the `\t \n \r \f \\ \uXXXX` escapes. A
// repeated key keeps the last value, as java.util.Properties does.
func Parse(src string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimLeft(lines[i], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		// A value continues while the line ends in an odd number of
		// backslashes.
		for oddTrailingBackslashes(line) && i+1 < len(lines) {
			i++
			line = strings.TrimSuffix(line, `\`) + strings.TrimLeft(lines[i], " \t\f")
		}
		key, value := splitEntry(line)
		if key == "" {
			continue
		}
		out[unescape(key)] = unescape(value)
	}
	return out
}

// oddTrailingBackslashes reports whether the line ends in a continuation
// backslash rather than an escaped one.
func oddTrailingBackslashes(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// splitEntry finds the first unescaped `=`, `:` or run of whitespace and
// returns the raw key and value around it, both trimmed of the separator's
// surrounding spaces.
func splitEntry(line string) (string, string) {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		switch line[i] {
		case '=', ':':
			return strings.TrimRight(line[:i], " \t\f"), strings.TrimLeft(line[i+1:], " \t\f")
		case ' ', '\t', '\f':
			key := line[:i]
			rest := strings.TrimLeft(line[i:], " \t\f")
			// `key = value` and `key : value` keep their separator.
			if rest != "" && (rest[0] == '=' || rest[0] == ':') {
				return key, strings.TrimLeft(rest[1:], " \t\f")
			}
			return key, rest
		}
	}
	return strings.TrimRight(line, " \t\f"), ""
}

// unescape resolves the `.properties` escapes. An unknown escape yields
// the character itself, which is what java.util.Properties does: `\:`
// is a colon, `\d` is a `d`.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					b.WriteRune(rune(n))
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
