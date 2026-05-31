package documents

// This file defines the DocumentSection struct,
// which represents a section of a document with its content, heading, level, and page number.
type DocumentSection struct {
	Content    string
	Heading    string
	Level      int64
	PageNumber int64
}
