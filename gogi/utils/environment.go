package utils

import "os"

const DEFAULT_DB_URL string = "postgres://gogi:gogi@postgres:5432/gogi"

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
		DEFAULT_DB_URL,
	)
}
