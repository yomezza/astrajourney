// Package naming defines the shared, locale-independent display-name policy.
package naming

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Key is also used for historical names, whose display text is never rewritten.
func Key(s string) string {
	return norm.NFKC.String(cases.Fold().String(strings.Join(strings.Fields(norm.NFKC.String(s)), " ")))
}

// Normalize implements naming policy v1; accepted scripts include Cyrillic.
func Normalize(s string, max int) (string, string, error) {
	if !utf8.ValidString(s) || len(s) > 1024 {
		return "", "", errors.New("Недопустимое название")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", "", errors.New("Недопустимые символы в названии")
		}
	}
	s = strings.Join(strings.Fields(norm.NFKC.String(s)), " ")
	count := uniseg.GraphemeClusterCount(s)
	if count < 3 || count > max {
		return "", "", errors.New("Недопустимая длина названия")
	}
	letter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			letter = true
		}
		if !(unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || strings.ContainsRune(" -'.’", r)) {
			return "", "", errors.New("Недопустимые символы в названии")
		}
	}
	key := Key(s)
	if !letter || strings.Contains(key, "www.") || strings.Contains(key, "http") {
		return "", "", errors.New("Недопустимое название")
	}
	// Local, versioned moderation policy, independent of external services.
	for _, v := range []string{"administrator", "администратор", "undefined", "null", "system", "fuck", "пизд", "хуй"} {
		if key == v || ((v == "fuck" || v == "пизд" || v == "хуй") && strings.Contains(key, v)) {
			return "", "", errors.New("Название запрещено правилами именования")
		}
	}
	return s, key, nil
}
