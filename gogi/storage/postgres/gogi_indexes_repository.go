package postgres

import (
	"context"
	"errors"
	"fmt"
	"gogi/gogi/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const TABLE_NAME string = "gogi_indexes"

type GogiIndexRepository struct {
	pool *pgxpool.Pool
}

func NewGogiIndexesRepository(
	pool *pgxpool.Pool,
) *GogiIndexRepository {

	return &GogiIndexRepository{
		pool: pool,
	}
}

func (r *GogiIndexRepository) Create(
	ctx context.Context,
	index GogiIndex,
) error {

	query := fmt.Sprintf(`
    		INSERT INTO %s (id, name, owner)
    		VALUES ($1, $2, $3)
			`, TABLE_NAME)

	_, err := r.pool.Exec(ctx, query, index.Id, index.Name, index.Owner)

	return err
}

func (r *GogiIndexRepository) GetIndexById(id string) (GogiIndex, error) {

	var index GogiIndex

	query := fmt.Sprintf(`
    		SELECT id, name, owner, created_at, last_updated_at FROM %s 
    		WHERE id = $1
			`, TABLE_NAME)

	err := r.pool.QueryRow(context.Background(), query, id).Scan(
		&index.Id,
		&index.Name,
		&index.Owner,
		&index.CreatedAt,
		&index.LastUpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return GogiIndex{}, utils.ErrJobNotFound
	}

	if err != nil {
		return GogiIndex{}, err
	}

	return index, nil
}

func (r *GogiIndexRepository) GetIndexByName(name string) (GogiIndex, error) {

	var index GogiIndex

	query := fmt.Sprintf(`
    		SELECT id, name, owner, created_at, last_updated_at FROM %s 
    		WHERE name = $1
			`, TABLE_NAME)

	err := r.pool.QueryRow(context.Background(), query, name).Scan(
		&index.Id,
		&index.Name,
		&index.Owner,
		&index.CreatedAt,
		&index.LastUpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return GogiIndex{}, utils.ErrJobNotFound
	}

	if err != nil {
		return GogiIndex{}, err
	}

	return index, nil
}
