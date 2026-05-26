package documents

import (
	"testing"
)

func TestExtractedDocument_Text(t *testing.T) {
	doc := ExtractedDocument{
		Sections: []DocumentSection{
			{Content: "Hello world."},
			{Content: "This is a test."},
		},
	}

	expected := "Hello world.\n\nThis is a test."
	actual := doc.Text()

	if actual != expected {
		t.Errorf(
			"expected %q but got %q",
			expected,
			actual,
		)
	}
}

func TestExtractedDocument_Text_Empty(t *testing.T) {
	doc := ExtractedDocument{
		Sections: []DocumentSection{
			{Content: "   "},
			{Content: "\n"},
		},
	}

	expected := "   \n\n\n"
	actual := doc.Text()

	if actual != expected {
		t.Errorf(
			"expected %q but got %q",
			expected,
			actual,
		)
	}
}

func TestExtractedDocument_Text_SingleSection(
	t *testing.T,
) {

	doc := ExtractedDocument{
		Sections: []DocumentSection{
			{
				Content: "Only one section",
			},
		},
	}

	expected := "Only one section"

	actual := doc.Text()

	if actual != expected {
		t.Errorf(
			"expected %q but got %q",
			expected,
			actual,
		)
	}
}

func TestExtractedDocument_Text_EmptySections(
	t *testing.T,
) {

	doc := ExtractedDocument{
		Sections: []DocumentSection{},
	}

	actual := doc.Text()

	if actual != "" {
		t.Errorf(
			"expected empty string but got %q",
			actual,
		)
	}
}
