package utils

import (
	"os"
	"strconv"
)

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

type VectorDBConnectionDetails struct {
	GOGI_VECTOR_DB_TYPE string
	GOGI_VECTOR_DB_HOST string
	GOGI_VECTOR_DB_PORT string
}

func GetVectorDBStorageConnectionDetails() (VectorDBConnectionDetails, error) {

	dbType := GetEnv("GOGI_VECTOR_DB_TYPE", "None")
	dbHost := GetEnv("GOGI_VECTOR_DB_HOST", "None")
	dbPort := GetEnv("GOGI_VECTOR_DB_PORT", "None")

	return VectorDBConnectionDetails{GOGI_VECTOR_DB_TYPE: dbType,
		GOGI_VECTOR_DB_HOST: dbHost, GOGI_VECTOR_DB_PORT: dbPort}, nil

}

// MaxMessageBytes is the largest gRPC message a service takes (GOGI_MAX_MESSAGE_MB, 32 by default): requests
// to models can carry images, which don't fit gRPC's default of 4 MB
func MaxMessageBytes() int {
	megabytes, err := strconv.Atoi(GetEnv("GOGI_MAX_MESSAGE_MB", "32"))
	if err != nil || megabytes <= 0 {
		megabytes = 32
	}
	return megabytes * 1024 * 1024
}
