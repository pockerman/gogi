package documents

// Bucket is the object store bucket of the documents' uploaded files
const Bucket = "gogi-documents"

// ObjectKey is where a document's uploaded file is kept: under its index's prefix, so the
// files of an index are deleted with it
func ObjectKey(indexName string, documentID string) string {
	return IndexPrefix(indexName) + documentID
}

func IndexPrefix(indexName string) string {
	return indexName + "/"
}
