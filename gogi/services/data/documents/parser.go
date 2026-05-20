package documents

type DocumentParser interface {
	Parse(fileBytes []byte) Document
}
