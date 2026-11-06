package components

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"sync"
)

const versionsFile = "versions.env"

var (
	keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

	versionsOnce sync.Once
	versionsMap  map[string]string
	versionsErr  error
)

// ParseEnv parses KEY="value" lines. Blank lines and lines starting with # are ignored.
// Values may be double-quoted, single-quoted or bare. Anything else is an error with
// the line number. Duplicate keys are an error, and so is a value of "latest".
func ParseEnv(data []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok || !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("line %d: want KEY=\"value\" with an UPPER_SNAKE_CASE key, got %q", n, line)
		}
		value, err := parseValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %s: %w", n, key, err)
		}
		if strings.EqualFold(value, "latest") {
			return nil, fmt.Errorf("line %d: %s: \"latest\" is not a pinned version", n, key)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %s", n, key)
		}
		out[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseValue accepts only forms bash reads literally: no expansion, escapes or trailing text.
func parseValue(raw string) (string, error) {
	switch {
	case raw == "":
		return "", nil
	case raw[0] == '"':
		if len(raw) < 2 || !strings.HasSuffix(raw, `"`) || strings.ContainsAny(raw[1:len(raw)-1], "\"$`\\") {
			return "", fmt.Errorf("bad double-quoted value %s (no inner quotes, $, ` or \\)", raw)
		}
		return raw[1 : len(raw)-1], nil
	case raw[0] == '\'':
		if len(raw) < 2 || !strings.HasSuffix(raw, "'") || strings.Contains(raw[1:len(raw)-1], "'") {
			return "", fmt.Errorf("bad single-quoted value %s", raw)
		}
		return raw[1 : len(raw)-1], nil
	default:
		if strings.ContainsAny(raw, " \t\"'$`\\#;&|<>(){}") {
			return "", fmt.Errorf("bare value %q has characters that need quoting", raw)
		}
		return raw, nil
	}
}

// Versions returns the parsed embedded versions.env.
func Versions() (map[string]string, error) {
	versionsOnce.Do(func() {
		data, err := FS.ReadFile(versionsFile)
		if err != nil {
			versionsErr = err
			return
		}
		versionsMap, err = ParseEnv(data)
		if err != nil {
			versionsErr = fmt.Errorf("components/%s: %w", versionsFile, err)
		}
	})
	if versionsErr != nil {
		return nil, versionsErr
	}
	return maps.Clone(versionsMap), nil
}

// Version returns one pinned value, or an error naming the missing key and the file.
// A key that is present but empty is not pinned yet and is also an error.
func Version(key string) (string, error) {
	v, err := Versions()
	if err != nil {
		return "", err
	}
	value, ok := v[key]
	switch {
	case !ok:
		return "", fmt.Errorf("%s is missing from components/%s", key, versionsFile)
	case value == "":
		return "", fmt.Errorf("%s is not pinned yet in components/%s", key, versionsFile)
	}
	return value, nil
}
