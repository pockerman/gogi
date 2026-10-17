package utils

import "os"

const DEFAULT_DB_URL string = "postgres://gogi:gogi@postgres:5432/gogi"
const DEFAULT_INGESTION_DOCUMENT_QUEUE_NAME = "ingestion-document-queue"

// searches and deletes each have a queue and a worker of their own, apart from the long ingestions
const DEFAULT_SEARCH_DOCUMENTS_QUEUE_NAME = "search-documents-queue"
const DEFAULT_DELETE_DOCUMENTS_QUEUE_NAME = "delete-documents-queue"

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

func GetSearchDocumentsQueueName() string {
	return GetEnv("GOGI_SEARCH_DOCUMENTS_QUEUE_NAME", DEFAULT_SEARCH_DOCUMENTS_QUEUE_NAME)
}

func GetDeleteDocumentsQueueName() string {
	return GetEnv("GOGI_DELETE_DOCUMENTS_QUEUE_NAME", DEFAULT_DELETE_DOCUMENTS_QUEUE_NAME)
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
