package impl

type DocumentParser interface {
	Parse(fileBytes []byte) Document
}
