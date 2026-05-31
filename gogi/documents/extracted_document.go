package documents

import "strings"

// This file defines the ExtractedDocument struct, which represents a document that has been
// processed and extracted into sections, along with its metadata.
type ExtractedDocument struct {
	Sections []DocumentSection
	Metadata map[string]string
}

func (d *ExtractedDocument) Text() string {

	contents := make([]string, 0, len(d.Sections))

	for _, section := range d.Sections {
		contents = append(contents, section.Content)
	}

	return strings.Join(contents, "\n\n")
}
