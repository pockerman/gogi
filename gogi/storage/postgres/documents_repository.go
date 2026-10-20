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
	SizeBytes   int64
	// the key of the uploaded file in the object store; empty if it has none
	ObjectKey string
	JobID     string
	// the status of the document's ingestion job
	Status     string
	ChunkCount int32
	PageCount  int32
	WordCount  int32
	CreatedAt  time.Time
	// the models the document is embedded by, as the application named them
	EmbeddingModels []string
}

type DocumentsRepository struct {
	pool *pgxpool.Pool
}

func NewDocumentsRepository(pool *pgxpool.Pool) *DocumentsRepository {
	return &DocumentsRepository{pool: pool}
}

const documentColumns = `
	d.id, i.name, d.filename, d.content_type, d.metadata, COALESCE(d.job_id::text, ''),
	COALESCE(j.status, ''), d.chunk_count, d.page_count, d.word_count, d.created_at,
	d.size_bytes, d.object_key,
	COALESCE((SELECT array_agg(e.model ORDER BY e.model) FROM gogi_document_embeddings e
		WHERE e.index_id = d.index_id AND e.document_id = d.id), '{}')`

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
		INSERT INTO gogi_documents (index_id, id, filename, content_type, metadata, job_id, size_bytes, object_key)
		SELECT id, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8 FROM gogi_indexes WHERE name = $1
	`, document.IndexName, document.ID, document.Filename, document.ContentType, metadata, document.JobID,
		document.SizeBytes, document.ObjectKey)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return utils.ErrIndexNotFound
	}
	return nil
}

// IndexExists says whether there's an index of the name
func (r *DocumentsRepository) IndexExists(ctx context.Context, indexName string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM gogi_indexes WHERE name = $1)`, indexName).Scan(&exists)
	return exists, err
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

// Replace gives the document new content: its file, job and metadata; its stats start again from zero,
// and no model embeds it until it's ingested again. It fails with ErrDocumentNotFound if there's no
// such document
func (r *DocumentsRepository) Replace(ctx context.Context, document Document) error {

	metadata := document.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE gogi_documents
		SET filename = $3, content_type = $4, metadata = $5, job_id = NULLIF($6, '')::uuid, size_bytes = $7,
			object_key = $8, chunk_count = 0, page_count = 0, word_count = 0
		WHERE id = $2 AND index_id = (SELECT id FROM gogi_indexes WHERE name = $1)
	`, document.IndexName, document.ID, document.Filename, document.ContentType, metadata, document.JobID,
		document.SizeBytes, document.ObjectKey)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return utils.ErrDocumentNotFound
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM gogi_document_embeddings
		WHERE document_id = $2 AND index_id = (SELECT id FROM gogi_indexes WHERE name = $1)
	`, document.IndexName, document.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetJob makes the job the document's ingestion job, e.g. when a stored document is ingested with
// more models. It fails with ErrDocumentNotFound if there's no such document
func (r *DocumentsRepository) SetJob(ctx context.Context, indexName string, documentID string, jobID string) error {

	tag, err := r.pool.Exec(ctx, `
		UPDATE gogi_documents SET job_id = $3::uuid
		WHERE id = $2 AND index_id = (SELECT id FROM gogi_indexes WHERE name = $1)
	`, indexName, documentID, jobID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return utils.ErrDocumentNotFound
	}
	return nil
}

// IndexEmbeddingModels are the models any document of the index is embedded by: the index has a
// collection for each
func (r *DocumentsRepository) IndexEmbeddingModels(ctx context.Context, indexName string) ([]string, error) {

	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT e.model FROM gogi_document_embeddings e
		JOIN gogi_indexes i ON i.id = e.index_id
		WHERE i.name = $1 ORDER BY e.model
	`, indexName)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
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
		&document.SizeBytes,
		&document.ObjectKey,
		&document.EmbeddingModels,
	)
	return document, err
}
