package utils

import "os"

const DEFAULT_DB_URL string = "postgres://gogi:gogi@postgres:5432/gogi"
const DEFAULT_INGESTION_DOCUMENT_QUEUE_NAME = "ingestion-document-queue"

// the queue of the quick document activities, searching and deleting, apart from the long ingestions
const DEFAULT_DOCUMENTS_QUEUE_NAME = "documents-queue"

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

func GetDocumentsQueueName() string {
	return GetEnv("GOGI_DOCUMENTS_QUEUE_NAME", DEFAULT_DOCUMENTS_QUEUE_NAME)
}

func GetIngestionDocumentQueueName() string {
	return GetEnv(
		"GOGI_INGESTION_DOCUMENT_QUEUE_NAME",
		DEFAULT_INGESTION_DOCUMENT_QUEUE_NAME,
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
