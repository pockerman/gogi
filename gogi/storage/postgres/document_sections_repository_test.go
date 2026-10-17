package postgres

import (
	"context"
	"testing"
)

func TestDocumentSectionsRepository(t *testing.T) {
	indexes := newTestIndexRepository(t)
	ctx := context.Background()
	index := createTestIndex(t, indexes, "maria--aeronautics", "maria")
	documents := NewDocumentsRepository(indexes.pool)
	if err := documents.Create(ctx, Document{ID: "fem-notes", IndexName: "maria--aeronautics", Filename: "fem-notes.md"}); err != nil {
		t.Fatal(err)
	}
	// the worker stores the sections
	for _, section := range []DocumentSection{
		{1, "1 Introduction", 1, 0, 0, "intro"},
		{2, "2 Shape functions", 1, 0, 0, "shape"},
		{3, "2.1 Linear elements", 2, 0, 0, "linear"},
		{4, "2.1.1 Hat functions", 3, 0, 0, "hat"},
		{5, "2.2 Quadratic elements", 2, 0, 0, "quadratic"},
		{6, "3 Assembly", 1, 0, 0, "assembly"},
	} {
		_, err := indexes.pool.Exec(ctx, `INSERT INTO gogi_document_sections (index_id, document_id, position, title, level, content)
			VALUES ($1, 'fem-notes', $2, $3, $4, $5)`, index.Id, section.Position, section.Title, section.Level, section.Content)
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := NewDocumentSectionsRepository(indexes.pool)

	listed, err := repo.List(ctx, "maria--aeronautics", "fem-notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 6 || listed[2].Title != "2.1 Linear elements" || listed[2].Level != 2 || listed[2].Content != "" {
		t.Errorf("listed = %+v", listed)
	}

	contents := func(position int32, includeSubsections bool) []string {
		t.Helper()
		read, err := repo.Read(ctx, "maria--aeronautics", "fem-notes", position, includeSubsections)
		if err != nil {
			t.Fatal(err)
		}
		texts := make([]string, len(read))
		for i, section := range read {
			texts[i] = section.Content
		}
		return texts
	}
	tests := []struct {
		position           int32
		includeSubsections bool
		want               []string
	}{
		{2, true, []string{"shape", "linear", "hat", "quadratic"}},
		{2, false, []string{"shape"}},
		{3, true, []string{"linear", "hat"}},
		// the last section runs to the end of the document
		{6, true, []string{"assembly"}},
		{5, true, []string{"quadratic"}},
		{7, true, []string{}},
	}
	for _, test := range tests {
		if got := contents(test.position, test.includeSubsections); !equal(got, test.want) {
			t.Errorf("read %d (subsections %v) = %v, want %v", test.position, test.includeSubsections, got, test.want)
		}
	}

	// deleting the document deletes its sections
	if err := documents.Delete(ctx, "maria--aeronautics", "fem-notes"); err != nil {
		t.Fatal(err)
	}
	if listed, _ := repo.List(ctx, "maria--aeronautics", "fem-notes"); len(listed) != 0 {
		t.Errorf("sections of a deleted document = %v", listed)
	}
}
