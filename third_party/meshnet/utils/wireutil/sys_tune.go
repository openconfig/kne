package wireutil

import (
	"os"
	"strconv"
)

// GetEnvInt reads an integer environment variable with a default fallback value if unset or invalid.
func GetEnvInt(key string, defaultVal int) int {
	if valStr := os.Getenv(key); valStr != "" {
		if val, err := strconv.Atoi(valStr); err == nil && val > 0 {
			return val
		}
	}
	return defaultVal
}
