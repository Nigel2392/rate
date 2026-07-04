package rate

import (
	"crypto/md5"
	"fmt"
	"strings"

	_ "unsafe"
)

//go:linkname isZero github.com/Nigel2392/go-django/src/internal/django_reflect.IsZero
func isZero(value interface{}) bool

func appendToMap[K comparable, V any](m map[K][]V, k K, v V) {
	result, ok := m[k]
	if ok {
		result = append(result, v)
	} else {
		result = make([]V, 0)
		result = append(result, v)
	}
	m[k] = result
}

func hashKey[DATA MatchType](prefix []string, keyData DATA) (string, error) {
	var key = SessionIDForType(keyData)
	if len(key) == 0 {
		return "", ErrSessionID.Wrapf(
			"Key length 0 when generating session ID for %T",
			keyData,
		)
	}

	var (
		sb       strings.Builder
		totalLen int
		hash     = md5.New()
	)

	for _, k := range key {
		hash.Write([]byte(k))
	}

	for _, p := range prefix {
		totalLen += len(p) + 1
	}

	sb.Grow(totalLen)

	for _, p := range prefix {
		sb.WriteString(p)
		sb.WriteRune(':')
	}

	fmt.Fprintf(&sb, "%x", hash.Sum(nil))
	return sb.String(), nil
}
