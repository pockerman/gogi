package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"testing"

	"github.com/google/uuid"
)

func TestDocumentsRepository(t *testing.T) {
	indexes := newTestIndexRepository(t)
	ctx := context.Background()
	createTestIndex(t, indexes, "maria--aeronautics", "maria")
	createTestIndex(t, indexes, "tom--history", "tom")
	repo := NewDocumentsRepository(indexes.pool)
	jobs := NewJobsRepository(indexes.pool)

	jobID := uuid.New().String()
	if err := jobs.Create(ctx, Job{ID: jobID, DocumentID: "fe-basics", Status: utils.JobPending, JobType: string(utils.DocumentIngestionJob)}); err != nil {
		t.Fatal(err)
	}

	documents := []Document{
		{ID: "fe-basics", IndexName: "maria--aeronautics", Filename: "fe-basics.txt", ContentType: "text/plain",
			Metadata: map[string]string{"subject": "numerical-methods", "topic": "finite-elements"}, JobID: jobID,
			SizeBytes: 2048, ObjectKey: "maria--aeronautics/fe-basics"},
		{ID: "fd-schemes", IndexName: "maria--aeronautics", Filename: "fd-schemes.txt",
			Metadata: map[string]string{"subject": "numerical-methods", "topic": "finite-differences"}},
		{ID: "blasius", IndexName: "maria--aeronautics", Filename: "blasius.txt",
			Metadata: map[string]string{"subject": "aerodynamics", "topic": "boundary-layers"}},
		// the same id in another index is another document
		{ID: "fe-basics", IndexName: "tom--history", Filename: "rome.txt"},
	}
	for _, document := range documents {
		if err := repo.Create(ctx, document); err != nil {
			t.Fatalf("create %s: %v", document.ID, err)
		}
	}

	t.Run("an index exists once created", func(t *testing.T) {
		for name, want := range map[string]bool{"maria--aeronautics": true, "nobody--nothing": false} {
			if exists, err := repo.IndexExists(ctx, name); err != nil || exists != want {
				t.Errorf("IndexExists(%s) = %v, %v; want %v", name, exists, err, want)
			}
		}
	})

	if err := repo.Create(ctx, Document{ID: "x", IndexName: "missing", Filename: "x.txt"}); !errors.Is(err, utils.ErrIndexNotFound) {
		t.Errorf("create in a missing index: expected ErrIndexNotFound, got %v", err)
	}

	filenames := func(filter map[string]string) []string {
		t.Helper()
		listed, err := repo.List(ctx, "maria--aeronautics", filter)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, len(listed))
		for i, document := range listed {
			names[i] = document.Filename
		}
		return names
	}
	tests := []struct {
		filter map[string]string
		want   []string
	}{
		{nil, []string{"fe-basics.txt", "fd-schemes.txt", "blasius.txt"}},
		{map[string]string{"subject": "numerical-methods"}, []string{"fe-basics.txt", "fd-schemes.txt"}},
		{map[string]string{"subject": "numerical-methods", "topic": "finite-differences"}, []string{"fd-schemes.txt"}},
		{map[string]string{"subject": "chemistry"}, []string{}},
	}
	for _, test := range tests {
		if got := filenames(test.filter); !equal(got, test.want) {
			t.Errorf("list with %v = %v, want %v", test.filter, got, test.want)
		}
	}

	document, err := repo.Get(ctx, "maria--aeronautics", "fe-basics")
	if err != nil {
		t.Fatal(err)
	}
	if document.Status != string(utils.JobPending) || document.JobID != jobID || document.Metadata["topic"] != "finite-elements" ||
		document.SizeBytes != 2048 || document.ObjectKey != "maria--aeronautics/fe-basics" {
		t.Errorf("document = %+v", document)
	}
	// a document registered without a job has no status
	if document, _ := repo.Get(ctx, "maria--aeronautics", "blasius"); document.Status != "" {
		t.Errorf("status = %q, want none", document.Status)
	}

	replacingJob := uuid.New().String()
	if err := jobs.Create(ctx, Job{ID: replacingJob, DocumentID: "fd-schemes", Status: utils.JobPending, JobType: string(utils.DocumentIngestionJob)}); err != nil {
		t.Fatal(err)
	}
	err = repo.Replace(ctx, Document{ID: "fd-schemes", IndexName: "maria--aeronautics", Filename: "fd-schemes.md",
		ContentType: "text/markdown", Metadata: map[string]string{"subject": "numerical-methods"}, JobID: replacingJob,
		SizeBytes: 99, ObjectKey: "maria--aeronautics/fd-schemes/v2"})
	if err != nil {
		t.Fatal(err)
	}
	if replaced, _ := repo.Get(ctx, "maria--aeronautics", "fd-schemes"); replaced.Filename != "fd-schemes.md" ||
		replaced.JobID != replacingJob || replaced.SizeBytes != 99 || replaced.ObjectKey != "maria--aeronautics/fd-schemes/v2" {
		t.Errorf("replaced = %+v", replaced)
	}
	if err := repo.Replace(ctx, Document{ID: "missing", IndexName: "maria--aeronautics"}); !errors.Is(err, utils.ErrDocumentNotFound) {
		t.Errorf("replacing a missing document: expected ErrDocumentNotFound, got %v", err)
	}

	if err := repo.Delete(ctx, "maria--aeronautics", "fe-basics"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, "maria--aeronautics", "fe-basics"); !errors.Is(err, utils.ErrDocumentNotFound) {
		t.Errorf("get after delete: expected ErrDocumentNotFound, got %v", err)
	}
	if _, err := repo.Get(ctx, "tom--history", "fe-basics"); err != nil {
		t.Errorf("the document of the same id in another index was deleted too: %v", err)
	}

	// deleting the index deletes its documents
	if _, err := indexes.DeleteIndexByName("maria--aeronautics"); err != nil {
		t.Fatal(err)
	}
	if listed, _ := repo.List(ctx, "maria--aeronautics", nil); len(listed) != 0 {
		t.Errorf("documents of a deleted index = %v", listed)
	}
}

func equal(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
