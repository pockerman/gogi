package impl

import (
	"context"
	"slices"

	"gogi/gogi/applications"
	"gogi/gogi/documents"
	"gogi/gogi/documents/ingestion"
	"gogi/gogi/embeddings"
	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// EmbeddingModelCatalog lists the embedding models the call's application can use, and embeds
// with them
type EmbeddingModelCatalog interface {
	ListEmbeddingModels(ctx context.Context) ([]*gogiv1.EmbeddingModel, error)
	// Embed is the vector of each text, by the application's model
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// ModelServiceCatalog asks the model service, not through the gateway, which needs an
// application's key
func ModelServiceCatalog(llmsAddress string) (EmbeddingModelCatalog, error) {
	conn, err := grpc.NewClient(llmsAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &modelServiceCatalog{client: gogiv1.NewLLMModelServerClient(conn)}, nil
}

type modelServiceCatalog struct {
	client gogiv1.LLMModelServerClient
}

func (c *modelServiceCatalog) ListEmbeddingModels(ctx context.Context) ([]*gogiv1.EmbeddingModel, error) {
	ctx = applications.OutgoingContext(ctx, applications.FromContext(ctx))
	response, err := c.client.ListEmbeddingModels(ctx, &gogiv1.ListEmbeddingModelsRequest{})
	if err != nil {
		return nil, err
	}
	return response.GetModels(), nil
}

func (c *modelServiceCatalog) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	ctx = applications.OutgoingContext(ctx, applications.FromContext(ctx))
	response, err := c.client.Embed(ctx, &gogiv1.EmbedRequest{Model: model, Inputs: texts})
	if err != nil {
		return nil, err
	}
	vectors := make([][]float32, 0, len(response.GetEmbeddings()))
	for _, embedding := range response.GetEmbeddings() {
		vectors = append(vectors, embedding.GetValues())
	}
	return vectors, nil
}

// embeddingModels are how the document is chunked for each of the models named, and the index's
// collection for each. A model the application can't use is refused before anything is stored
func (s *DocumentsServer) embeddingModels(ctx context.Context, indexName string, names []string) ([]ingestion.EmbeddingModel, error) {
	catalog, err := s.models.ListEmbeddingModels(ctx)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "failed to find the embedding models: %v", status.Convert(err).Message())
	}

	configs := make([]ingestion.EmbeddingModel, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true

		index := slices.IndexFunc(catalog, func(model *gogiv1.EmbeddingModel) bool { return model.GetName() == name })
		if index < 0 {
			return nil, status.Errorf(codes.NotFound, "there is no embedding model %q", name)
		}
		model := catalog[index]
		if !model.GetAvailable() {
			return nil, status.Errorf(codes.FailedPrecondition, "the embedding model %q needs a key: store one as the credential %q",
				name, model.GetCredentialRef())
		}

		size, overlap := embeddings.ChunkWords(int(model.GetMaxInputTokens()))
		configs = append(configs, ingestion.EmbeddingModel{
			Model:        name,
			Collection:   documents.CollectionName(indexName, name),
			ChunkSize:    size,
			ChunkOverlap: overlap,
		})
	}
	return configs, nil
}

// collections are the index's collections for the models
func collections(indexName string, models []string) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		names = append(names, documents.CollectionName(indexName, model))
	}
	return names
}
