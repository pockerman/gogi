package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// The test runs against a migrated database, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func newTestIndexRepository(t *testing.T) *GogiIndexRepository {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}

	pool, err := NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// the tables are emptied before and after the test, so use a dedicated test database

	clear := func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM gogi_indexes"); err != nil {
			t.Fatal(err)
		}
	}
	clear()
	t.Cleanup(clear)
	return NewGogiIndexesRepository(pool)
}

func createTestIndex(t *testing.T, repo *GogiIndexRepository, name string, owner string) GogiIndex {
	t.Helper()
	if err := repo.Create(context.Background(), GogiIndex{Id: uuid.New().String(), Name: name, Owner: owner}); err != nil {
		t.Fatal(err)
	}
	index, err := repo.GetIndexByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func TestIndexRepositoryCreatorIsOwner(t *testing.T) {
	repo := newTestIndexRepository(t)

	index := createTestIndex(t, repo, "maria--aeronautics", "maria")

	if len(index.Members) != 1 || index.Members["maria"] != IndexRoleOwner {
		t.Errorf("members = %v, want maria as the owner", index.Members)
	}
	if _, err := repo.GetIndexByName("missing"); !errors.Is(err, utils.ErrIndexNotFound) {
		t.Errorf("expected ErrIndexNotFound, got %v", err)
	}
}

func TestIndexRepositoryMembers(t *testing.T) {
	repo := newTestIndexRepository(t)
	ctx := context.Background()
	aeronautics := createTestIndex(t, repo, "maria--aeronautics", "maria")
	createTestIndex(t, repo, "tom--history", "tom")

	if err := repo.SetMember(ctx, aeronautics.Id, "tom", IndexRoleReader); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMember(ctx, aeronautics.Id, "lena", IndexRoleOwner); err != nil {
		t.Fatal(err)
	}

	index, _ := repo.GetIndexByName("maria--aeronautics")
	want := map[string]IndexRole{"maria": IndexRoleOwner, "tom": IndexRoleReader, "lena": IndexRoleOwner}
	if len(index.Members) != len(want) {
		t.Fatalf("members = %v, want %v", index.Members, want)
	}
	for member, role := range want {
		if index.Members[member] != role {
			t.Errorf("role of %s = %s, want %s", member, index.Members[member], role)
		}
	}

	// tom sees his own index and the one shared with him; lena only the shared one
	tomIndexes, _ := repo.GetIndexesForMember("tom")
	if len(tomIndexes) != 2 {
		t.Errorf("tom's indexes = %v, want 2", tomIndexes)
	}
	lenaIndexes, _ := repo.GetIndexesForMember("lena")
	if len(lenaIndexes) != 1 || lenaIndexes[0].Name != "maria--aeronautics" {
		t.Errorf("lena's indexes = %v, want maria--aeronautics", lenaIndexes)
	}
	// only the indexes they created
	if owned, _ := repo.GetIndexesForOwner("tom"); len(owned) != 1 {
		t.Errorf("tom's own indexes = %v, want 1", owned)
	}

	// changing a role
	if err := repo.SetMember(ctx, aeronautics.Id, "tom", IndexRoleOwner); err != nil {
		t.Fatal(err)
	}
	index, _ = repo.GetIndexByName("maria--aeronautics")
	if index.Members["tom"] != IndexRoleOwner {
		t.Errorf("role of tom = %s, want owner", index.Members["tom"])
	}

	if err := repo.RemoveMember(ctx, aeronautics.Id, "tom"); err != nil {
		t.Fatal(err)
	}
	if indexes, _ := repo.GetIndexesForMember("tom"); len(indexes) != 1 {
		t.Errorf("tom's indexes after removal = %v, want 1", indexes)
	}
}

func TestIndexRepositoryKeepsAnOwner(t *testing.T) {
	repo := newTestIndexRepository(t)
	ctx := context.Background()
	index := createTestIndex(t, repo, "maria--aeronautics", "maria")
	if err := repo.SetMember(ctx, index.Id, "tom", IndexRoleReader); err != nil {
		t.Fatal(err)
	}

	if err := repo.RemoveMember(ctx, index.Id, "maria"); !errors.Is(err, utils.ErrLastIndexOwner) {
		t.Errorf("removing the last owner: expected ErrLastIndexOwner, got %v", err)
	}
	if err := repo.SetMember(ctx, index.Id, "maria", IndexRoleReader); !errors.Is(err, utils.ErrLastIndexOwner) {
		t.Errorf("demoting the last owner: expected ErrLastIndexOwner, got %v", err)
	}

	// with a second owner, either can go, but not both at once
	if err := repo.SetMember(ctx, index.Id, "lena", IndexRoleOwner); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, owner := range []string{"maria", "lena"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = repo.RemoveMember(ctx, index.Id, owner)
		}()
	}
	wg.Wait()

	failed := 0
	for _, err := range errs {
		if errors.Is(err, utils.ErrLastIndexOwner) {
			failed++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if failed != 1 {
		t.Errorf("removing both owners at once: %d removals failed, want 1", failed)
	}
}

func TestIndexRepositoryDeleteRemovesMembers(t *testing.T) {
	repo := newTestIndexRepository(t)
	index := createTestIndex(t, repo, "maria--aeronautics", "maria")
	if err := repo.SetMember(context.Background(), index.Id, "tom", IndexRoleReader); err != nil {
		t.Fatal(err)
	}

	if deleted, err := repo.DeleteIndexByName("maria--aeronautics"); err != nil || !deleted {
		t.Fatalf("delete: %v, %v", deleted, err)
	}
	if indexes, _ := repo.GetIndexesForMember("tom"); len(indexes) != 0 {
		t.Errorf("tom's indexes = %v, want none", indexes)
	}
}
