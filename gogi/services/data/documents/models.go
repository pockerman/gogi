package documents

type DocumentSection struct {
	Content    string
	Heading    string
	Level      int64
	PageNumber int64
}

type Document struct {
	Sections []DocumentSection
	Metadata map[string]string
}

type DocumentMetadata struct{}
