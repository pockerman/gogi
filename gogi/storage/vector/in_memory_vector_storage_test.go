package vector

import (
	"testing"

	"gogi/gogi/chunks"
)

func TestInsert(t *testing.T) {

	s := NewInMemoryVectorStore()

	chunks := []chunks.Chunk{
		{Text: "hello world"},
		{Text: "golang grpc"},
	}

	vectors := [][]float64{
		{1.0, 0.0},
		{0.0, 1.0},
	}

	metadata := map[string]string{
		"source": "pdf",
	}

	count, err := s.Insert(
		"test-index",
		"doc-1",
		chunks,
		vectors,
		metadata,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 inserted chunks, got %d", count)
	}

	if len(s.chunks) != 2 {
		t.Fatalf("expected internal chunk count 2, got %d", len(s.chunks))
	}
}

func TestDeleteByDocumentID(t *testing.T) {

	s := NewInMemoryVectorStore()

	chunks := []chunks.Chunk{
		{Text: "chunk one"},
		{Text: "chunk two"},
	}

	vectors := [][]float64{
		{1, 0},
		{0, 1},
	}

	_, _ = s.Insert(
		"index-1",
		"doc-1",
		chunks,
		vectors,
		nil,
	)

	deleted, err := s.DeleteByDocumentID("index-1", "doc-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleted != 2 {
		t.Fatalf("expected 2 deleted chunks, got %d", deleted)
	}

	if len(s.chunks) != 0 {
		t.Fatalf("expected 0 chunks remaining, got %d", len(s.chunks))
	}
}

func TestDeleteIndex(t *testing.T) {

	s := NewInMemoryVectorStore()

	_, _ = s.Insert(
		"index-1",
		"doc-1",
		[]chunks.Chunk{
			{Text: "hello"},
		},
		[][]float64{
			{1, 0},
		},
		nil,
	)

	_, _ = s.Insert(
		"index-2",
		"doc-2",
		[]chunks.Chunk{
			{Text: "world"},
		},
		[][]float64{
			{0, 1},
		},
		nil,
	)

	deleted, err := s.DeleteIndex("index-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleted != 1 {
		t.Fatalf("expected 1 deleted chunk, got %d", deleted)
	}

	if len(s.chunks) != 1 {
		t.Fatalf("expected 1 remaining chunk, got %d", len(s.chunks))
	}
}

func TestSearch(t *testing.T) {

	s := NewInMemoryVectorStore()

	_, _ = s.Insert(
		"test-index",
		"doc-1",
		[]chunks.Chunk{
			{Text: "hello world"},
		},
		[][]float64{
			{1.0, 0.0},
		},
		map[string]string{
			"type": "text",
		},
	)

	results, err := s.Search(
		[]float64{1.0, 0.0},
		5,
		"test-index",
		nil,
		0,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].DocumentID != "doc-1" {
		t.Fatalf("unexpected document id: %s", results[0].DocumentID)
	}
}

func TestSearchWithMetadataFilter(t *testing.T) {

	s := NewInMemoryVectorStore()

	_, _ = s.Insert(
		"test-index",
		"doc-1",
		[]chunks.Chunk{
			{Text: "python chunk"},
		},
		[][]float64{
			{1, 0},
		},
		map[string]string{
			"lang": "python",
		},
	)

	_, _ = s.Insert(
		"test-index",
		"doc-2",
		[]chunks.Chunk{
			{Text: "golang chunk"},
		},
		[][]float64{
			{1, 0},
		},
		map[string]string{
			"lang": "go",
		},
	)

	results, err := s.Search(
		[]float64{1, 0},
		10,
		"test-index",
		map[string]string{
			"lang": "go",
		},
		0,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].DocumentID != "doc-2" {
		t.Fatalf("expected doc-2, got %s", results[0].DocumentID)
	}
}

func TestKeywordSearch(t *testing.T) {

	s := NewInMemoryVectorStore()

	_, _ = s.Insert(
		"docs",
		"doc-1",
		[]chunks.Chunk{
			{Text: "golang grpc protobuf grpc"},
		},
		[][]float64{
			{1, 0},
		},
		nil,
	)

	results, err := s.KeywordSearch(
		"docs",
		"grpc",
		5,
		nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Score != 2 {
		t.Fatalf("expected keyword score 2, got %f", results[0].Score)
	}
}

func TestKeywordSearchWithMetadataFilter(t *testing.T) {

	s := NewInMemoryVectorStore()

	_, _ = s.Insert(
		"docs",
		"doc-1",
		[]chunks.Chunk{
			{Text: "grpc tutorial"},
		},
		[][]float64{
			{1, 0},
		},
		map[string]string{
			"type": "tutorial",
		},
	)

	_, _ = s.Insert(
		"docs",
		"doc-2",
		[]chunks.Chunk{
			{Text: "grpc reference"},
		},
		[][]float64{
			{1, 0},
		},
		map[string]string{
			"type": "reference",
		},
	)

	results, err := s.KeywordSearch(
		"docs",
		"grpc",
		10,
		map[string]string{
			"type": "reference",
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].DocumentID != "doc-2" {
		t.Fatalf("expected doc-2, got %s", results[0].DocumentID)
	}
}
