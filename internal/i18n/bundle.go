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

// DefaultLocale is the bundle every other locale falls back to, key by
// key (PLAN §9 R05).
const DefaultLocale = "en_US"

// Locales are the bundles shipped, BlogCFC's own four. The German three
// are byte-identical in the as-is and are kept apart all the same: they
// differ in their dates (PLAN §9 R06), and a German string that wants to
// differ in Austria has somewhere to go.
var Locales = []string{"en_US", "de_DE", "de_AT", "de_CH"}

// files holds the bundles: BlogCFC's own `.properties` files, copied
// verbatim (see README.md in this directory).
//
//go:embed main_en_US.properties main_de_DE.properties main_de_AT.properties main_de_CH.properties
var files embed.FS

// Bundle is one locale's strings. It is read-only after New and safe for
// concurrent use.
type Bundle struct {
	locale   string
	values   map[string]string
	fallback *Bundle // en_US, for keys this bundle is missing; nil on en_US
}

var (
	loadOnce sync.Once
	bundles  map[string]*Bundle
	loadErr  error
)

// load parses every embedded bundle, once.
func load() {
	loadOnce.Do(func() {
		bundles = map[string]*Bundle{}
		base, err := parseFile("main_" + DefaultLocale + ".properties")
		if err != nil {
			loadErr = err
			base = &Bundle{locale: DefaultLocale, values: map[string]string{}}
		}
		bundles[DefaultLocale] = base
		for _, locale := range Locales {
			if locale == DefaultLocale {
				continue
			}
			b, err := parseFile("main_" + locale + ".properties")
			if err != nil {
				if loadErr == nil {
					loadErr = err
				}
				continue
			}
			b.fallback = base
			bundles[locale] = b
		}
	})
}

// New returns the bundle for a locale. An unknown locale - anything but
// the four in Locales - answers with en_US, and a key a known bundle is
// missing falls back to en_US's value (PLAN §9 R05).
func New(locale string) *Bundle {
	load()
	if b, ok := bundles[Normalize(locale)]; ok {
		return b
	}
	return bundles[DefaultLocale]
}

// Known reports whether a locale has a bundle of its own.
func Known(locale string) bool {
	load()
	_, ok := bundles[Normalize(locale)]
	return ok
}

// Normalize puts a locale in BlogCFC's `language_COUNTRY` shape, so
// "de-de", "DE_DE" and " de_DE " all reach the German bundle. A string
// that is not two parts is returned trimmed and otherwise untouched,
// which simply will not match.
func Normalize(locale string) string {
	locale = strings.TrimSpace(locale)
	locale = strings.ReplaceAll(locale, "-", "_")
	lang, country, ok := strings.Cut(locale, "_")
	if !ok {
		return locale
	}
	return strings.ToLower(lang) + "_" + strings.ToUpper(country)
}

// LoadError reports a failure to parse an embedded bundle. It can only
// fire if an embedded file is corrupt, so callers may check it at start.
func LoadError() error { load(); return loadErr }

// Locale is the bundle's locale name.
func (b *Bundle) Locale() string { return b.locale }

// Has reports whether the key exists.
func (b *Bundle) Has(key string) bool {
	if b == nil {
		return false
	}
	if _, ok := b.values[key]; ok {
		return true
	}
	return b.fallback.Has(key)
}

// lookup finds a key in this bundle or, failing that, in en_US.
func (b *Bundle) lookup(key string) (string, bool) {
	if b == nil {
		return "", false
	}
	if v, ok := b.values[key]; ok {
		return v, true
	}
	return b.fallback.lookup(key)
}

// T returns the string for a key with its placeholders filled in. A key
// this bundle is missing is taken from en_US (PLAN §9 R05); a key no
// bundle has returns the key itself, which is visible in a page and
// greppable, rather than an empty string that hides the mistake.
//
// Placeholders are BlogCFC's `{1}`-style, one-based: T("hi", "Ray") turns
// "Hello {1}" into "Hello Ray". A placeholder with no argument is left
// alone, so a half-supplied string still shows what it wanted.
func (b *Bundle) T(key string, args ...any) string {
	if b == nil {
		return key
	}
	v, ok := b.lookup(key)
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
