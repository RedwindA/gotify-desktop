// Package mdtext decodes the backslash escapes and character references of Markdown text.
package mdtext

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/util"
)

var reference = regexp.MustCompile(`^&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

// Decode resolves "\*" to "*" and "&amp;" to "&" in ordinary text, in one pass so an escaped
// ampersand stays literal. Text of code spans and blocks must not go through it.
func Decode(b []byte) string {
	if !strings.ContainsAny(string(b), `\&`) {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\\' && i+1 < len(b) && util.IsPunct(b[i+1]):
			sb.WriteByte(b[i+1])
			i++
		case b[i] == '&':
			if m := reference.Find(b[i:]); m != nil {
				if m[1] == '#' {
					sb.WriteRune(numericReference(m[2 : len(m)-1]))
				} else {
					sb.Write(util.ResolveEntityNames(m))
				}
				i += len(m) - 1
			} else {
				sb.WriteByte('&')
			}
		default:
			sb.WriteByte(b[i])
		}
	}
	return sb.String()
}

// numericReference decodes the body of "&#...;", as CommonMark says: decimal or
// x-prefixed hexadecimal, and U+FFFD for zero, surrogates and out-of-range values.
func numericReference(body []byte) rune {
	digits, base := string(body), 10
	if body[0] == 'x' || body[0] == 'X' {
		digits, base = string(body[1:]), 16
	}
	v, err := strconv.ParseUint(digits, base, 32)
	if err != nil || v == 0 || v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF) {
		return utf8.RuneError
	}
	return rune(v)
}
