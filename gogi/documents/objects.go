package documents

import "gogi/gogi/storage/objects"

// Bucket is the object store bucket of the documents' uploaded files: GOGI_DOCUMENTS_BUCKET, gogi-documents by default
var Bucket = objects.DocumentsBucket()

// ObjectKey is where a document's uploaded file is kept: under its index's prefix, so the
// files of an index are deleted with it
func ObjectKey(indexName string, documentID string) string {
	return IndexPrefix(indexName) + documentID
}

// VersionKey is where new content of a document is kept when it replaces the old: a key of its own, so the
// old content stays until the document refers to the new
func VersionKey(indexName string, documentID string, version string) string {
	return ObjectKey(indexName, documentID) + "/" + version
}

func IndexPrefix(indexName string) string {
	return indexName + "/"
}

// CollectionName is the vector store collection of an index's chunks for an embedding model, as
// the application named the model: each model's vectors are kept apart, as vectors of different
// models cannot be compared. A registered model's name has no dots, so the name is never read as
// another index's
func CollectionName(indexName string, model string) string {
	return indexName + "." + model
}
