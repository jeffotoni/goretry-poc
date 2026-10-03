package env

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(v), v != ""
}

func GetInt(name string, def int) int {
	if s, ok := lookup(name); ok && s != "" {
		// ParseInt avoids 32-bit overflow issues when casting
		if n, err := strconv.ParseInt(s, 10, 0); err == nil {
			return int(n)
		}
	}
	return def
}

func GetInt64(name string, def int64) int64 {
	if s, ok := lookup(name); ok && s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// GetDuration now keeps backward compatibility with integer values in milliseconds.
// If the value contains a unit letter, it accepts Go's native duration format, like "250ms", "2s", "1m".
// Examples:
//
//	"1500"   -> 1500ms
//	"2s"     -> 2 seconds
//	"750ms"  -> 750 milliseconds
func GetDuration(name string, def time.Duration) time.Duration {
	if s, ok := lookup(name); ok && s != "" {
		// If contains a unit, try time.ParseDuration first
		if strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
			if d, err := time.ParseDuration(s); err == nil {
				return d
			}
		}
		// Fallback to integer milliseconds
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Duration(n) * time.Millisecond
		}
	}
	return def
}

func GetString(name string, def string) string {
	if s, ok := lookup(name); ok && s != "" {
		return s
	}
	return def
}

func GetBool(name string, def bool) bool {
	if s, ok := lookup(name); ok && s != "" {
		if v, err := strconv.ParseBool(s); err == nil {
			return v
		}
	}
	return def
}

func GetFloat(name string, def float64) float64 {
	if s, ok := lookup(name); ok && s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return def
}
