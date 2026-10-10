package postgres

import (
	"context"
	"errors"
	"gogi/gogi/utils"
	"os"
	"testing"
)

// The test runs against a migrated database, e.g.
// GOGI_TEST_POSTGRES_DSN=postgres://postgres:test@localhost:5432/gogi?sslmode=disable
func newTestRegisteredLLMsRepository(t *testing.T) *GogiRegisteredLLMsRepository {
	dsn := os.Getenv("GOGI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOGI_TEST_POSTGRES_DSN is not set")
	}

	pool, err := NewPool(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// the table is emptied before and after the test, so use a dedicated test database

	clear := func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM "+GOGI_REGISTERED_LLMS_TABLE_NAME); err != nil {
			t.Fatal(err)
		}
	}
	clear()
	t.Cleanup(clear)
	return NewGogiRegisteredLLMsRepository(pool)
}

func TestRegisteredLLMsRepository(t *testing.T) {
	repo := newTestRegisteredLLMsRepository(t)
	ctx := context.Background()

	if _, err := repo.GetRegisteredLLMByName(ctx, "llama3.2"); !errors.Is(err, utils.ErrRegisteredLLMNotFound) {
		t.Fatalf("expected ErrRegisteredLLMNotFound, got %v", err)
	}

	created, err := repo.UpsertRegisteredLLM(ctx, &GogiRegisteredLLM{
		Name: "llama3.2", Provider: "ollama", ContextWindow: 128_000, SupportsTools: true,
		Endpoint: "http://localhost:11434/v1", HealthCheck: "/api/version", AdapterType: "openai",
		CredentialRef: "ml-inference-prod", Status: "provisioning",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("id and created_at not set %+v", created)
	}
	firstID, firstCreatedAt := created.ID, created.CreatedAt

	// a health check records the status
	if err := repo.UpdateRegisteredLLMStatus(ctx, "llama3.2", "healthy", firstCreatedAt); err != nil {
		t.Fatal(err)
	}
	checked, _ := repo.GetRegisteredLLMByName(ctx, "llama3.2")
	if checked.CredentialRef != "ml-inference-prod" || checked.Status != "healthy" || checked.LastCheckedAt == nil || !checked.LastCheckedAt.Equal(firstCreatedAt) {
		t.Errorf("status not recorded %+v", checked)
	}
	if err := repo.UpdateRegisteredLLMStatus(ctx, "unknown", "healthy", firstCreatedAt); !errors.Is(err, utils.ErrRegisteredLLMNotFound) {
		t.Errorf("expected ErrRegisteredLLMNotFound, got %v", err)
	}

	// registering the same name again updates the registration and keeps its id and creation time
	updated, err := repo.UpsertRegisteredLLM(ctx, &GogiRegisteredLLM{
		Name: "llama3.2", Provider: "ollama", ContextWindow: 64_000,
		Endpoint: "http://ollama:11434/v1", AdapterType: "openai", Status: "provisioning",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != firstID || !updated.CreatedAt.Equal(firstCreatedAt) || !updated.UpdatedAt.After(firstCreatedAt) {
		t.Errorf("wrong upsert result %+v", updated)
	}

	fetched, err := repo.GetRegisteredLLMByName(ctx, "llama3.2")
	if err != nil {
		t.Fatal(err)
	}
	// the new registration resets the status until the next health check
	if fetched.Endpoint != "http://ollama:11434/v1" || fetched.ContextWindow != 64_000 ||
		fetched.SupportsTools || fetched.HealthCheck != "" || fetched.CredentialRef != "" || fetched.Status != "provisioning" || fetched.LastCheckedAt != nil {
		t.Errorf("registration not updated %+v", fetched)
	}

	if _, err := repo.UpsertRegisteredLLM(ctx, &GogiRegisteredLLM{
		Name: "mistral", Provider: "ollama", Endpoint: "http://localhost:11434/v1", AdapterType: "openai",
		Status: "provisioning",
	}); err != nil {
		t.Fatal(err)
	}

	models, err := repo.ListRegisteredLLMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Name != "llama3.2" || models[1].Name != "mistral" {
		t.Errorf("wrong models %+v", models)
	}
}
