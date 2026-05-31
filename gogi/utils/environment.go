package utils

import "os"

func GetEnv(key string, fallback string) string {
	value := os.Getenv(key)

	if value == "" {
		return fallback
	}

	return value
}

func GetDatabaseURL() string {
	return GetEnv(
		"DATABASE_URL",
		"postgres://gogi:gogi@postgres:5432/gogi",
	)
}
