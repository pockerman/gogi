package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Document is a document of an index, as registered when its ingestion starts
type Document struct {
	ID          string
	IndexName   string
	Filename    string
	ContentType string
	Metadata    map[string]string
	JobID       string
	// the status of the document's ingestion job
	Status     string
	ChunkCount int32
	PageCount  int32
	WordCount  int32
	CreatedAt  time.Time
}

type DocumentsRepository struct {
	pool *pgxpool.Pool
}

func NewDocumentsRepository(pool *pgxpool.Pool) *DocumentsRepository {
	return &DocumentsRepository{pool: pool}
}

const documentColumns = `
	d.id, i.name, d.filename, d.content_type, d.metadata, COALESCE(d.job_id::text, ''),
	COALESCE(j.status, ''), d.chunk_count, d.page_count, d.word_count, d.created_at`

const documentFrom = `
	FROM gogi_documents d
	JOIN gogi_indexes i ON i.id = d.index_id
	LEFT JOIN jobs j ON j.id = d.job_id`

// Create registers a document of the index. It fails with ErrIndexNotFound if there's no such index.
func (r *DocumentsRepository) Create(ctx context.Context, document Document) error {

	metadata := document.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}

	tag, err := r.pool.Exec(ctx, `
		INSERT INTO gogi_documents (index_id, id, filename, content_type, metadata, job_id)
		SELECT id, $2, $3, $4, $5, NULLIF($6, '')::uuid FROM gogi_indexes WHERE name = $1
	`, document.IndexName, document.ID, document.Filename, document.ContentType, metadata, document.JobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return utils.ErrIndexNotFound
	}
	return nil
}

// List returns the documents of the index whose metadata has every key and value of metadataFilter
func (r *DocumentsRepository) List(ctx context.Context, indexName string, metadataFilter map[string]string) ([]Document, error) {

	if metadataFilter == nil {
		metadataFilter = map[string]string{}
	}

	rows, err := r.pool.Query(ctx, `SELECT `+documentColumns+documentFrom+`
		WHERE i.name = $1 AND d.metadata @> $2
		ORDER BY d.created_at, d.id
	`, indexName, metadataFilter)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanDocument)
}

// Get returns the document, or ErrDocumentNotFound
func (r *DocumentsRepository) Get(ctx context.Context, indexName string, documentID string) (Document, error) {

	rows, err := r.pool.Query(ctx, `SELECT `+documentColumns+documentFrom+`
		WHERE i.name = $1 AND d.id = $2
	`, indexName, documentID)
	if err != nil {
		return Document{}, err
	}

	document, err := pgx.CollectExactlyOneRow(rows, scanDocument)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, utils.ErrDocumentNotFound
	}
	return document, err
}

// Delete removes the document; deleting one that doesn't exist does nothing
func (r *DocumentsRepository) Delete(ctx context.Context, indexName string, documentID string) error {

	_, err := r.pool.Exec(ctx, `
		DELETE FROM gogi_documents
		WHERE id = $2 AND index_id = (SELECT id FROM gogi_indexes WHERE name = $1)
	`, indexName, documentID)
	return err
}

func scanDocument(row pgx.CollectableRow) (Document, error) {
	var document Document
	err := row.Scan(
		&document.ID,
		&document.IndexName,
		&document.Filename,
		&document.ContentType,
		&document.Metadata,
		&document.JobID,
		&document.Status,
		&document.ChunkCount,
		&document.PageCount,
		&document.WordCount,
		&document.CreatedAt,
	)
	return document, err
}
