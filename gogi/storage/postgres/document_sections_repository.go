package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DocumentSection is a section of a document, as its ingestion parsed it
type DocumentSection struct {
	Position  int32
	Title     string
	Level     int32
	PageStart int32
	PageEnd   int32
	// the section's own text, without its subsections'; only Read returns it
	Content string
}

// DocumentSectionsRepository reads the sections the ingestion worker stores
type DocumentSectionsRepository struct {
	pool *pgxpool.Pool
}

func NewDocumentSectionsRepository(pool *pgxpool.Pool) *DocumentSectionsRepository {
	return &DocumentSectionsRepository{pool: pool}
}

const sectionsOfDocument = `
	FROM gogi_document_sections s
	JOIN gogi_indexes i ON i.id = s.index_id
	WHERE i.name = $1 AND s.document_id = $2`

// List returns the document's sections in order, without their content
func (r *DocumentSectionsRepository) List(ctx context.Context, indexName string, documentID string) ([]DocumentSection, error) {
	rows, err := r.pool.Query(ctx, `SELECT s.position, s.title, s.level, s.page_start, s.page_end, ''`+
		sectionsOfDocument+` ORDER BY s.position`, indexName, documentID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanSection)
}

// Read returns the section at the position, with its content, followed by its subsections if
// includeSubsections: the sections after it up to the next one of its level or higher. It returns
// no sections if there's none at the position.
func (r *DocumentSectionsRepository) Read(ctx context.Context, indexName string, documentID string, position int32,
	includeSubsections bool) ([]DocumentSection, error) {

	rows, err := r.pool.Query(ctx, `
		WITH target AS (
			SELECT s.position, s.level`+sectionsOfDocument+` AND s.position = $3
		), next AS (
			-- the position of the next section of the target's level or higher
			SELECT MIN(s.position) AS position`+sectionsOfDocument+`
			AND s.position > (SELECT position FROM target) AND s.level <= (SELECT level FROM target)
		)
		SELECT s.position, s.title, s.level, s.page_start, s.page_end, s.content`+sectionsOfDocument+`
		AND s.position >= (SELECT position FROM target)
		AND (CASE WHEN $4 THEN s.position < COALESCE((SELECT position FROM next), 2147483647)
			ELSE s.position = (SELECT position FROM target) END)
		ORDER BY s.position
	`, indexName, documentID, position, includeSubsections)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanSection)
}

func scanSection(row pgx.CollectableRow) (DocumentSection, error) {
	var section DocumentSection
	err := row.Scan(&section.Position, &section.Title, &section.Level, &section.PageStart, &section.PageEnd, &section.Content)
	return section, err
}
