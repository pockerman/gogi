package vector

import (
	"container/list"
	"sort"
	"strings"
	"sync"
	"time"

	"gogi/gogi/embeddings"

	"gogi/gogi/maths/similarity"
	"gogi/gogi/storage/vector/models"

	"github.com/google/uuid"
)

type storedChunk struct {
	ChunkID    string
	DocumentID string
	IndexName  string
	Text       string
	Embedding  []float64
	Metadata   map[string]string
}

// InMemoryVectorStore is a simple in-memory implementation of the VectorStore interface.
type InMemoryVectorStore struct {
	chunks        []storedChunk
	indexes       map[string]models.Index
	documents     map[string]map[string]models.DocumentMetadata
	jobs          map[string]models.IngestJob
	jobPayloads   map[string]models.JobPayload
	jobIndexNames map[string]string
	jobAttempts   map[string]int
	jobClaimedAt  map[string]time.Time
	queue         *list.List
	lock          sync.Mutex
}

func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		chunks:        []storedChunk{},
		indexes:       make(map[string]models.Index),
		documents:     make(map[string]map[string]models.DocumentMetadata),
		jobs:          make(map[string]models.IngestJob),
		jobPayloads:   make(map[string]models.JobPayload),
		jobIndexNames: make(map[string]string),
		jobAttempts:   make(map[string]int),
		jobClaimedAt:  make(map[string]time.Time),
		queue:         list.New(),
	}
}

// ------------------------------------------------------------------ chunks

func (s *InMemoryVectorStore) Insert(
	index_name string,
	document_id string,
	chunks []embeddings.Chunk,
	embeddings [][]float64,
	metadata map[string]string,
) (int, error) {

	for i, chunk := range chunks {

		embedding := embeddings[i]

		s.chunks = append(s.chunks, storedChunk{
			ChunkID:    uuid.New().String(),
			IndexName:  index_name,
			DocumentID: document_id,
			Text:       chunk.Text,
			Embedding:  embedding,
			Metadata:   copyMetadata(metadata),
		})
	}

	return len(chunks), nil
}

func (s *InMemoryVectorStore) DeleteByDocumentID(
	index_name string,
	document_id string,
) (int, error) {

	before := len(s.chunks)
	filtered := make([]storedChunk, 0)
	for _, c := range s.chunks {
		if !(c.IndexName == index_name && c.DocumentID == document_id) {
			filtered = append(filtered, c)
		}
	}

	s.chunks = filtered
	if docs, ok := s.documents[index_name]; ok {
		delete(docs, document_id)
	}

	return before - len(s.chunks), nil
}

func (s *InMemoryVectorStore) DeleteIndex(index_name string) (int, error) {

	before := len(s.chunks)
	filtered := make([]storedChunk, 0)
	for _, c := range s.chunks {
		if c.IndexName != index_name {
			filtered = append(filtered, c)
		}
	}

	s.chunks = filtered

	delete(s.documents, index_name)
	delete(s.indexes, index_name)

	return before - len(s.chunks), nil
}

func (s *InMemoryVectorStore) Search(
	query []float64, top_k int, index_name string,
	metadata_filters map[string]string, score_threshold float64,
) ([]models.VectorSearchResult, error) {

	candidates := make([]storedChunk, 0)
	for _, c := range s.chunks {
		if c.IndexName == index_name {
			candidates = append(candidates, c)
		}
	}

	if metadata_filters != nil {

		filtered := make([]storedChunk, 0)
		for _, c := range candidates {

			match := true
			for k, v := range metadata_filters {
				if c.Metadata[k] != v {
					match = false
					break
				}
			}

			if match {
				filtered = append(filtered, c)
			}
		}

		candidates = filtered
	}

	type scoredChunk struct {
		Score float64
		Chunk storedChunk
	}

	scored := make([]scoredChunk, 0)
	for _, c := range candidates {

		score := similarity.CosineSimilarity(query, c.Embedding)
		if score_threshold != 0 && score < score_threshold {
			continue
		}

		scored = append(scored, scoredChunk{
			Score: score,
			Chunk: c,
		})
	}

	// TODO: sort descending by score
	results := make([]models.VectorSearchResult, 0)
	for i, item := range scored {

		if i >= top_k {
			break
		}

		results = append(results, models.VectorSearchResult{
			ChunkID:    item.Chunk.ChunkID,
			DocumentID: item.Chunk.DocumentID,
			Text:       item.Chunk.Text,
			Score:      item.Score,
			Metadata:   item.Chunk.Metadata,
		})
	}

	return results, nil
}

func (s *InMemoryVectorStore) KeywordSearch(
	indexName string,
	query string,
	topK int,
	metadataFilters map[string]string,
) ([]models.VectorSearchResult, error) {

	query = strings.ToLower(strings.TrimSpace(query))

	if query == "" {
		return []models.VectorSearchResult{}, nil
	}

	type scoredChunk struct {
		Chunk storedChunk
		Score float64
	}

	scored := make([]scoredChunk, 0)
	for _, chunk := range s.chunks {

		// filter by index
		if chunk.IndexName != indexName {
			continue
		}

		// metadata filtering
		match := true
		for k, v := range metadataFilters {
			if chunk.Metadata[k] != v {
				match = false
				break
			}
		}

		if !match {
			continue
		}

		text := strings.ToLower(chunk.Text)

		// naive keyword scoring
		count := strings.Count(text, query)

		if count == 0 {
			continue
		}

		score := float64(count)

		scored = append(scored, scoredChunk{
			Chunk: chunk,
			Score: score,
		})
	}

	// sort descending by score
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	results := make([]models.VectorSearchResult, 0)

	for i, item := range scored {

		if i >= topK {
			break
		}

		results = append(results, models.VectorSearchResult{
			ChunkID:    item.Chunk.ChunkID,
			DocumentID: item.Chunk.DocumentID,
			Text:       item.Chunk.Text,
			Score:      item.Score,
			Metadata:   item.Chunk.Metadata,
		})
	}

	return results, nil
}

// ------------------------------------------------------------------ indexes

// func (s *InMemoryVectorStore) CreateIndex(index Index) error {

// 	s.lock.Lock()
// 	defer s.lock.Unlock()

// 	if _, exists := s.indexes[index.Name]; exists {
// 		return ErrIndexAlreadyExists
// 	}

// 	s.indexes[index.Name] = index

// 	if _, exists := s.documents[index.Name]; !exists {
// 		s.documents[index.Name] = make(map[string]DocumentMetadata)
// 	}

// 	return nil
// }

// func (s *InMemoryVectorStore) GetIndex(name string) (*Index, error) {

// 	s.lock.Lock()
// 	defer s.lock.Unlock()

// 	idx, ok := s.indexes[name]

// 	if !ok {
// 		return nil, ErrIndexNotFound
// 	}

// 	return &idx, nil
// }

// func (s *InMemoryVectorStore) ListIndexes() []Index {

// 	s.lock.Lock()
// 	defer s.lock.Unlock()

// 	result := make([]Index, 0)

// 	for _, idx := range s.indexes {
// 		result = append(result, idx)
// 	}

// 	return result
// }

// ------------------------------------------------------------------ utilities

func copyMetadata(src map[string]string) map[string]string {

	dst := make(map[string]string)

	for k, v := range src {
		dst[k] = v
	}

	return dst
}
