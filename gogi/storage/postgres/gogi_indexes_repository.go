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
const MEMBERS_TABLE_NAME string = "gogi_index_members"

const indexColumns = "id, name, owner, created_at, last_updated_at"

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

// Create stores the index, with its owner as its first member
func (r *GogiIndexRepository) Create(
	ctx context.Context,
	index GogiIndex,
) error {

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		query := fmt.Sprintf(`INSERT INTO %s (id, name, owner) VALUES ($1, $2, $3)`, TABLE_NAME)
		if _, err := tx.Exec(ctx, query, index.Id, index.Name, index.Owner); err != nil {
			return err
		}

		query = fmt.Sprintf(`INSERT INTO %s (index_id, member, role) VALUES ($1, $2, $3)`, MEMBERS_TABLE_NAME)
		_, err := tx.Exec(ctx, query, index.Id, index.Owner, IndexRoleOwner)
		return err
	})
}

func (r *GogiIndexRepository) GetIndexById(id string) (GogiIndex, error) {
	return r.getIndex(context.Background(), "id", id)
}

func (r *GogiIndexRepository) GetIndexByName(name string) (GogiIndex, error) {
	return r.getIndex(context.Background(), "name", name)
}

func (r *GogiIndexRepository) getIndex(ctx context.Context, column string, value string) (GogiIndex, error) {

	query := fmt.Sprintf(`SELECT %s FROM %s WHERE %s = $1`, indexColumns, TABLE_NAME, column)

	var index GogiIndex
	err := r.pool.QueryRow(ctx, query, value).Scan(
		&index.Id,
		&index.Name,
		&index.Owner,
		&index.CreatedAt,
		&index.LastUpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return GogiIndex{}, utils.ErrIndexNotFound
	}

	if err != nil {
		return GogiIndex{}, err
	}

	indexes, err := r.withMembers(ctx, []GogiIndex{index})
	if err != nil {
		return GogiIndex{}, err
	}
	return indexes[0], nil
}

// Return the indexes created by the given owner
func (r *GogiIndexRepository) GetIndexesForOwner(owner string) ([]GogiIndex, error) {

	query := fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE owner = $1
		ORDER BY created_at DESC
	`, indexColumns, TABLE_NAME)

	return r.listIndexes(context.Background(), query, owner)
}

// Return the indexes the given user has any role on
func (r *GogiIndexRepository) GetIndexesForMember(member string) ([]GogiIndex, error) {

	query := fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE id IN (SELECT index_id FROM %s WHERE member = $1)
		ORDER BY created_at DESC
	`, indexColumns, TABLE_NAME, MEMBERS_TABLE_NAME)

	return r.listIndexes(context.Background(), query, member)
}

func (r *GogiIndexRepository) listIndexes(ctx context.Context, query string, args ...any) ([]GogiIndex, error) {

	rows, err := r.pool.Query(ctx, query, args...)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make([]GogiIndex, 0)

	for rows.Next() {
		var index GogiIndex

		err := rows.Scan(
			&index.Id,
			&index.Name,
			&index.Owner,
			&index.CreatedAt,
			&index.LastUpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		indexes = append(indexes, index)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return r.withMembers(ctx, indexes)
}

// withMembers fills in the members of the indexes
func (r *GogiIndexRepository) withMembers(ctx context.Context, indexes []GogiIndex) ([]GogiIndex, error) {

	if len(indexes) == 0 {
		return indexes, nil
	}

	ids := make([]string, len(indexes))
	byId := make(map[string]*GogiIndex, len(indexes))
	for i := range indexes {
		indexes[i].Members = map[string]IndexRole{}
		ids[i] = indexes[i].Id
		byId[indexes[i].Id] = &indexes[i]
	}

	query := fmt.Sprintf(`SELECT index_id, member, role FROM %s WHERE index_id = ANY($1::uuid[])`, MEMBERS_TABLE_NAME)
	rows, err := r.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var indexId, member string
		var role IndexRole
		if err := rows.Scan(&indexId, &member, &role); err != nil {
			return nil, err
		}
		byId[indexId].Members[member] = role
	}

	return indexes, rows.Err()
}

// SetMember gives the user the role on the index, or changes the role they have. Demoting the
// last owner to a reader fails with ErrLastIndexOwner.
func (r *GogiIndexRepository) SetMember(ctx context.Context, indexId string, member string, role IndexRole) error {

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if role != IndexRoleOwner {
			if err := ensureAnotherOwner(ctx, tx, indexId, member); err != nil {
				return err
			}
		}

		query := fmt.Sprintf(`
			INSERT INTO %s (index_id, member, role) VALUES ($1, $2, $3)
			ON CONFLICT (index_id, member) DO UPDATE SET role = EXCLUDED.role
		`, MEMBERS_TABLE_NAME)
		if _, err := tx.Exec(ctx, query, indexId, member, role); err != nil {
			return err
		}
		return touchIndex(ctx, tx, indexId)
	})
}

// RemoveMember takes the user off the index. Removing its last owner fails with ErrLastIndexOwner;
// removing a user who isn't a member does nothing.
func (r *GogiIndexRepository) RemoveMember(ctx context.Context, indexId string, member string) error {

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := ensureAnotherOwner(ctx, tx, indexId, member); err != nil {
			return err
		}

		query := fmt.Sprintf(`DELETE FROM %s WHERE index_id = $1 AND member = $2`, MEMBERS_TABLE_NAME)
		if _, err := tx.Exec(ctx, query, indexId, member); err != nil {
			return err
		}
		return touchIndex(ctx, tx, indexId)
	})
}

// ensureAnotherOwner fails with ErrLastIndexOwner if member is the index's only owner. It locks
// the index's members, so two concurrent changes can't remove both of its last two owners.
func ensureAnotherOwner(ctx context.Context, tx pgx.Tx, indexId string, member string) error {

	query := fmt.Sprintf(`SELECT member FROM %s WHERE index_id = $1 AND role = $2 FOR UPDATE`, MEMBERS_TABLE_NAME)
	rows, err := tx.Query(ctx, query, indexId, IndexRoleOwner)
	if err != nil {
		return err
	}
	owners, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}

	if len(owners) == 1 && owners[0] == member {
		return utils.ErrLastIndexOwner
	}
	return nil
}

func touchIndex(ctx context.Context, tx pgx.Tx, indexId string) error {
	query := fmt.Sprintf(`UPDATE %s SET last_updated_at = NOW() WHERE id = $1`, TABLE_NAME)
	_, err := tx.Exec(ctx, query, indexId)
	return err
}

// Delete the index with the given name
func (r *GogiIndexRepository) DeleteIndexByName(index_name string) (bool, error) {

	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE name = $1
	`, TABLE_NAME)

	tag, err := r.pool.Exec(context.Background(), query, index_name)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil

}

// Delete the index with the given id
func (r *GogiIndexRepository) DeleteIndexById(index_id string) (bool, error) {

	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE id = $1
	`, TABLE_NAME)

	tag, err := r.pool.Exec(context.Background(), query, index_id)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil

}

// Delete the indexes for the owner with the given name
func (r *GogiIndexRepository) DeleteOwnerIndexes(owner string) (bool, error) {

	query := fmt.Sprintf(`
		DELETE FROM %s
		WHERE owner = $1
	`, TABLE_NAME)

	tag, err := r.pool.Exec(context.Background(), query, owner)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() > 0, nil

}
