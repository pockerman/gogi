package chunks

import (
	"strings"

	"gogi/gogi/documents"
)

// Fixed-size chunking implementation
type FixedSizeChunking struct {
	ChunkSize    int
	ChunkOverlap int
}

// Constructor
func NewFixedSizeChunking(
	chunkSize int,
	chunkOverlap int,
) *FixedSizeChunking {

	if chunkSize <= 0 {
		chunkSize = 512
	}

	if chunkOverlap < 0 {
		chunkOverlap = 50
	}

	return &FixedSizeChunking{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Chunk implements ChunkingStrategy
func (f *FixedSizeChunking) Chunk(
	document documents.ExtractedDocument,
) []Chunk {

	text := document.Text()

	if strings.TrimSpace(text) == "" {
		return []Chunk{}
	}

	words := strings.Fields(text)

	var chunks []Chunk

	step := f.ChunkSize - f.ChunkOverlap

	if step < 1 {
		step = 1
	}

	i := 0

	for i < len(words) {

		end := i + f.ChunkSize

		if end > len(words) {
			end = len(words)
		}

		chunkWords := words[i:end]
		chunkText := strings.Join(chunkWords, " ")

		// approximate search offset
		searchStart := 0

		for _, w := range words[:i] {
			searchStart += len(w) + 1
		}

		startOffset := searchStart

		if len(chunkWords) > 0 {
			found := strings.Index(
				text[searchStart:],
				chunkWords[0],
			)

			if found >= 0 {
				startOffset = searchStart + found
			}
		}

		endOffset := startOffset + len(chunkText)

		chunks = append(chunks, Chunk{
			Text:        chunkText,
			StartOffset: startOffset,
			EndOffset:   endOffset,
		})

		i += step
	}

	return chunks
}
