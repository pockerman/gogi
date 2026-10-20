//go:build bdd

package bdd

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"gogi/gogi/embeddings"
	gogiv1 "gogi/gogi/gogi/v1"

	"github.com/cucumber/godog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	bge    = "bge-small-en-v1.5"
	miniLM = "all-MiniLM-L6-v2"
	// how long an ingestion may take on a small CPU
	ingestionTimeout = 5 * time.Minute
)

func (w *world) embeddingModelSteps(sc *godog.ScenarioContext) {
	sc.Step(`^gogi has the application "([^"]*)"$`, w.register)
	sc.Step(`^"([^"]*)" has the index "([^"]*)"$`, w.hasIndex)

	sc.Step(`^"([^"]*)" stores the document "([^"]*)" in "([^"]*)"$`, w.storesDocument)
	sc.Step(`^"([^"]*)" has stored "([^"]*)" in "([^"]*)"$`, w.storesDocument)
	sc.Step(`^"([^"]*)" is listed in "([^"]*)"$`, w.isListed)
	sc.Step(`^it has no chunks$`, w.hasNoChunks)

	sc.Step(`^"([^"]*)" ingests "([^"]*)" into "([^"]*)" with the models:$`, w.ingestsWithTable)
	sc.Step(`^"([^"]*)" ingests "([^"]*)" into "([^"]*)" with "([^"]*)"$`, w.ingestsWith)
	sc.Step(`^"([^"]*)" has ingested "([^"]*)" into "([^"]*)" with "([^"]*)"$`, w.hasIngested)
	sc.Step(`^"([^"]*)" has ingested "([^"]*)" into "([^"]*)" with "([^"]*)" and "([^"]*)"$`, w.hasIngestedWithBoth)
	sc.Step(`^"([^"]*)" ingests "([^"]*)" with "([^"]*)"$`, w.ingestsStored)
	sc.Step(`^"([^"]*)" ingests a new version of "([^"]*)" with "([^"]*)"$`, w.ingestsNewVersion)
	sc.Step(`^"([^"]*)" ingests "([^"]*)" into "([^"]*)" without naming a model$`, w.ingestsWithoutAModel)
	sc.Step(`^it is told to name at least one embedding model$`, w.isToldInvalid("name at least one embedding model"))
	sc.Step(`^it is told there is no embedding model "([^"]*)"$`, func(model string) error { return w.isToldNotFound(model) })
	sc.Step(`^"([^"]*)" is not stored$`, w.isNotStored)

	sc.Step(`^"([^"]*)" is found by searching "([^"]*)" with "([^"]*)"$`, w.isFoundWith)
	sc.Step(`^it is found by searching "([^"]*)" with "([^"]*)"$`, w.itIsFoundWith)
	sc.Step(`^"([^"]*)" is found by searching "([^"]*)" with either model$`, w.isFoundWithEither)
	sc.Step(`^"([^"]*)" searches "([^"]*)" with "([^"]*)"$`, w.searchesWith)
	sc.Step(`^"([^"]*)" is not found$`, w.isNotFound)
	sc.Step(`^each model's chunks of "([^"]*)" are no longer than that model reads$`, w.chunksFitTheirModels)
	sc.Step(`^"([^"]*)" has more chunks for "([^"]*)" than for "([^"]*)"$`, w.hasMoreChunksFor)
	sc.Step(`^searching "([^"]*)" with "([^"]*)" finds only the new version's chunks$`, w.findsOnlyTheNewVersion)
	sc.Step(`^"([^"]*)" deletes "([^"]*)"$`, w.deletesDocument)
	sc.Step(`^searching "([^"]*)" with either model does not find it$`, w.eitherModelDoesNotFind)
	sc.Step(`^searching "([^"]*)" with "([^"]*)" does not find it$`, w.modelDoesNotFind)

	sc.Step(`^"([^"]*)" has ingested into "([^"]*)", with "([^"]*)", a document whose chunks each end with a sentence about "([^"]*)"$`, w.hasIngestedEndingWith)
	sc.Step(`^another document about the same without "([^"]*)"$`, w.anotherDocumentWithout)
	sc.Step(`^"([^"]*)" searches "([^"]*)" for "([^"]*)" with "([^"]*)"$`, w.searchesForWith)
	sc.Step(`^the document is found$`, w.theDocumentIsFound)
	sc.Step(`^"([^"]*)" searches "([^"]*)" for "([^"]*)" without naming a model$`, w.searchesWithoutAModel)
	sc.Step(`^it is told to name the embedding model to search with$`, w.isToldInvalid("name the embedding model to search with"))

	sc.Step(`^its chunks are embedded with the "([^"]*)" key of "([^"]*)"$`, w.modelIsCalledWith)

	sc.Step(`^"([^"]*)" hosts the embedding model "([^"]*)" at "([^"]*)"$`, w.hostsModel)
	sc.Step(`^"([^"]*)" registers the embedding model "([^"]*)" at that URL$`, w.registersHostedModel)
	sc.Step(`^"([^"]*)" can list the embedding model "([^"]*)" with its dimensions$`, w.listsEmbeddingModel)
	sc.Step(`^it can ingest documents with "([^"]*)"$`, w.canIngestWith)
	sc.Step(`^"([^"]*)" has registered its hosted model as "([^"]*)", which needs "([^"]*)"$`, w.registersHostedModelNeeding)
	sc.Step(`^"([^"]*)" has registered the embedding model "([^"]*)"$`, w.hasRegisteredEmbeddingModel)
	sc.Step(`^"([^"]*)" registers the embedding model "([^"]*)" at a URL where nothing answers$`, w.registersWhereNothingAnswers)
	sc.Step(`^it is told the model at that URL could not be reached$`, w.isToldUnreachable)
	sc.Step(`^"([^"]*)" registers the embedding model "([^"]*)" at "([^"]*)"$`, w.registersEmbeddingModelAt)
	sc.Step(`^it is told the URL is not allowed$`, w.isToldInvalid("not allowed"))
	sc.Step(`^"([^"]*)" ingests a document with "([^"]*)"$`, w.ingestsADocumentWith)
}

func (w *world) documentsClient() gogiv1.DocumentServerClient {
	return gogiv1.NewDocumentServerClient(w.conn)
}

func (w *world) hasIndex(name, index string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	if _, err := w.indexesClient().CreateIndex(ctx, &gogiv1.CreateIndexRequest{IndexName: index, Owner: "maria"}); err != nil {
		return err
	}
	w.indexes[name] = append(w.indexes[name], index)
	return nil
}

// notes are a document's contents: paragraphs on finite elements, so a search for one of their
// subjects finds them
func notes(version string) string {
	paragraphs := []string{
		"Shape functions interpolate the unknown field within an element from its values at the nodes.",
		"The stiffness matrix of the whole mesh is assembled from the matrices of its elements.",
		"Gauss quadrature integrates the element matrices at a few points chosen for accuracy.",
		"Boundary conditions are imposed on the assembled system before it is solved.",
	}
	var text strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&text, "%s This is the %s version of the notes.\n\n", paragraphs[i%len(paragraphs)], version)
	}
	return text.String()
}

// upload stores the document, as the application calls it, and ingests it with the models
func (w *world) upload(name, index, filename, content string, replace bool, models ...string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	upload, err := w.documentsClient().UploadDocument(ctx)
	if err != nil {
		return err
	}
	parts := []*gogiv1.UploadDocumentRequest{
		{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{
			IndexName: index, DocumentId: filename, Filename: filename, ContentType: "text/markdown",
			Replace: replace, EmbeddingModels: models}}},
		{Part: &gogiv1.UploadDocumentRequest_Chunk{Chunk: []byte(content)}},
	}
	for _, part := range parts {
		if err := upload.Send(part); err != nil {
			break
		}
	}
	response, err := upload.CloseAndRecv()
	w.response, w.err = response, err
	if err == nil {
		w.documents[filename] = content
		w.job = response.GetJobId()
	}
	return nil
}

// ingested waits for the last ingestion to finish, and fails if it didn't
func (w *world) ingested(name string) error {
	if w.err != nil {
		return w.err
	}
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(ingestionTimeout)
	for time.Now().Before(deadline) {
		job, err := w.documentsClient().GetDocumentIngestJob(ctx, &gogiv1.GetIngestDocumentJobRequest{JobId: w.job})
		if err != nil {
			return err
		}
		switch job.GetStatus() {
		case "COMPLETED":
			return nil
		case "FAILED", "CANCELLED":
			return fmt.Errorf("the ingestion %s: %s", strings.ToLower(job.GetStatus()), job.GetErrorMessage())
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the ingestion did not finish in %s", ingestionTimeout)
}

func (w *world) storesDocument(name, filename, index string) error {
	if err := w.upload(name, index, filename, notes("first"), false); err != nil {
		return err
	}
	return w.err
}

func (w *world) document(name, index, filename string) (*gogiv1.DocumentMetadata, error) {
	ctx, err := w.for_(name)
	if err != nil {
		return nil, err
	}
	listed, err := w.documentsClient().ListDocuments(ctx, &gogiv1.ListDocumentsRequest{IndexName: index})
	if err != nil {
		return nil, err
	}
	for _, document := range listed.GetDocuments() {
		if document.GetFilename() == filename {
			return document, nil
		}
	}
	return nil, nil
}

func (w *world) isListed(filename, index string) error {
	document, err := w.document("qubit-notes", index, filename)
	if err != nil {
		return err
	}
	if document == nil {
		return fmt.Errorf("%s is not listed", filename)
	}
	w.response = document
	return nil
}

func (w *world) hasNoChunks() error {
	document := w.response.(*gogiv1.DocumentMetadata)
	if document.GetChunkCount() != 0 || len(document.GetEmbeddingModels()) != 0 || document.GetStatus() != "COMPLETED" {
		return fmt.Errorf("the document is %+v", document)
	}
	return nil
}

func (w *world) ingestsWithTable(name, filename, index string, table *godog.Table) error {
	models := make([]string, 0, len(table.Rows)-1)
	for _, row := range table.Rows[1:] {
		models = append(models, row.Cells[0].Value)
	}
	if err := w.upload(name, index, filename, notes("first"), false, models...); err != nil {
		return err
	}
	return w.ingested(name)
}

// ingestsWith ingests, and waits for the ingestion only if it was started
func (w *world) ingestsWith(name, filename, index, model string) error {
	if err := w.upload(name, index, filename, notes("first"), false, model); err != nil {
		return err
	}
	if w.err != nil {
		return nil
	}
	return w.ingested(name)
}

func (w *world) hasIngested(name, filename, index, model string) error {
	if err := w.upload(name, index, filename, notes("first"), false, model); err != nil {
		return err
	}
	return w.ingested(name)
}

func (w *world) hasIngestedWithBoth(name, filename, index, first, second string) error {
	if err := w.upload(name, index, filename, notes("first"), false, first, second); err != nil {
		return err
	}
	return w.ingested(name)
}

func (w *world) ingestsStored(name, filename, model string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	response, err := w.documentsClient().IngestStoredDocument(ctx, &gogiv1.IngestStoredDocumentRequest{
		IndexName: w.indexes[name][0], DocumentId: filename, EmbeddingModels: []string{model}})
	if err != nil {
		return err
	}
	w.job = response.GetJobId()
	return w.ingested(name)
}

func (w *world) ingestsNewVersion(name, filename, model string) error {
	if err := w.upload(name, w.indexes[name][0], filename, notes("second"), true, model); err != nil {
		return err
	}
	return w.ingested(name)
}

func (w *world) ingestsWithoutAModel(name, filename, index string) error {
	if err := w.storesDocument(name, filename, index); err != nil {
		return err
	}
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = w.documentsClient().IngestStoredDocument(ctx, &gogiv1.IngestStoredDocumentRequest{
		IndexName: index, DocumentId: filename})
	return nil
}

func (w *world) isToldInvalid(want string) func() error {
	return func() error {
		if status.Code(w.err) != codes.InvalidArgument || !strings.Contains(status.Convert(w.err).Message(), want) {
			return fmt.Errorf("expected InvalidArgument saying %q, got %v", want, w.err)
		}
		return nil
	}
}

func (w *world) isNotStored(filename string) error {
	document, err := w.document("qubit-notes", w.indexes["qubit-notes"][0], filename)
	if err != nil {
		return err
	}
	if document != nil {
		return fmt.Errorf("%s is stored", filename)
	}
	return nil
}

// search searches the application's index with the model
func (w *world) search(name, index, query, model string, topK int32) ([]*gogiv1.DocumentChunk, error) {
	ctx, err := w.for_(name)
	if err != nil {
		return nil, err
	}
	response, err := w.documentsClient().SearchDocuments(ctx, &gogiv1.SearchDocumentsRequest{
		IndexName: index, Query: query, TopK: topK, EmbeddingModel: model})
	w.response, w.err = response, err
	return response.GetChunks(), err
}

func found(chunks []*gogiv1.DocumentChunk, filename string) bool {
	return slices.ContainsFunc(chunks, func(chunk *gogiv1.DocumentChunk) bool { return chunk.GetDocumentId() == filename })
}

func (w *world) isFoundWith(filename, index, model string) error {
	chunks, err := w.search("qubit-notes", index, "shape functions of an element", model, 5)
	if err != nil {
		return err
	}
	if !found(chunks, filename) {
		return fmt.Errorf("searching with %s did not find %s: %v", model, filename, chunks)
	}
	return nil
}

func (w *world) itIsFoundWith(index, model string) error {
	return w.isFoundWith("fem-notes.md", index, model)
}

func (w *world) isFoundWithEither(filename, index string) error {
	for _, model := range []string{bge, miniLM} {
		if err := w.isFoundWith(filename, index, model); err != nil {
			return err
		}
	}
	return nil
}

func (w *world) searchesWith(name, index, model string) error {
	w.search(name, index, "shape functions of an element", model, 5)
	return nil
}

func (w *world) isNotFound(filename string) error {
	if w.err != nil {
		return w.err
	}
	if chunks := w.response.(*gogiv1.SearchDocumentsResponse).GetChunks(); found(chunks, filename) {
		return fmt.Errorf("%s is found", filename)
	}
	return nil
}

// chunksOf are the document's chunks for the model
func (w *world) chunksOf(index, filename, model string) ([]*gogiv1.DocumentChunk, error) {
	ctx, err := w.for_("qubit-notes")
	if err != nil {
		return nil, err
	}
	response, err := w.documentsClient().SearchDocuments(ctx, &gogiv1.SearchDocumentsRequest{
		IndexName: index, Query: "notes", TopK: 200, EmbeddingModel: model, DocumentIds: []string{filename}})
	return response.GetChunks(), err
}

func (w *world) chunksFitTheirModels(filename string) error {
	index := w.indexes["qubit-notes"][0]
	for _, model := range []struct {
		name   string
		tokens int
	}{{bge, 512}, {miniLM, 256}} {
		chunks, err := w.chunksOf(index, filename, model.name)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			return fmt.Errorf("%s has no chunks for %s", filename, model.name)
		}
		size, _ := embeddings.ChunkWords(model.tokens)
		for _, chunk := range chunks {
			// a word is a token or two: a chunk of the words a model is given is read whole
			if words := len(strings.Fields(chunk.GetContent())); words > size {
				return fmt.Errorf("a chunk for %s has %d words, more than the %d it reads whole", model.name, words, size)
			}
		}
	}
	return nil
}

func (w *world) hasMoreChunksFor(filename, more, fewer string) error {
	index := w.indexes["qubit-notes"][0]
	many, err := w.chunksOf(index, filename, more)
	if err != nil {
		return err
	}
	few, err := w.chunksOf(index, filename, fewer)
	if err != nil {
		return err
	}
	if len(many) <= len(few) {
		return fmt.Errorf("%d chunks for %s and %d for %s", len(many), more, len(few), fewer)
	}
	return nil
}

func (w *world) findsOnlyTheNewVersion(index, model string) error {
	chunks, err := w.chunksOf(index, "fem-notes.md", model)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("the new version has no chunks")
	}
	for _, chunk := range chunks {
		if strings.Contains(chunk.GetContent(), "first version") {
			return fmt.Errorf("a chunk of the old version is found: %q", chunk.GetContent())
		}
	}
	return nil
}

func (w *world) deletesDocument(name, filename string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	_, err = w.documentsClient().DeleteDocument(ctx, &gogiv1.DeleteDocumentRequest{IndexName: w.indexes[name][0], DocumentId: filename})
	return err
}

func (w *world) modelDoesNotFind(index, model string) error {
	chunks, err := w.search("qubit-notes", index, "shape functions of an element", model, 5)
	if err != nil {
		return err
	}
	if found(chunks, "fem-notes.md") {
		return fmt.Errorf("searching with %s finds it", model)
	}
	return nil
}

func (w *world) eitherModelDoesNotFind(index string) error {
	for _, model := range []string{bge, miniLM} {
		if err := w.modelDoesNotFind(index, model); err != nil {
			return err
		}
	}
	return nil
}

// meshing is a passage on another subject than the one searched for, as long as the chunks the
// model is given
func meshing(words int) string {
	sentence := "A structured mesh of quadrilateral elements is refined near the corners of the domain where gradients are steep. "
	var text strings.Builder
	for text.Len() == 0 || len(strings.Fields(text.String())) < words {
		text.WriteString(sentence)
	}
	return text.String()
}

func (w *world) hasIngestedEndingWith(name, index, model, subject string) error {
	size, _ := embeddings.ChunkWords(512)
	var text strings.Builder
	for i := 0; i < 3; i++ {
		// each chunk opens on meshing, and its last sentence is about the subject
		fmt.Fprintf(&text, "%sThe wing showed %s at high speed in the tunnel.\n\n", meshing(size-40), subject)
	}
	if err := w.upload(name, index, "flutter-tests.md", text.String(), false, model); err != nil {
		return err
	}
	return w.ingested(name)
}

func (w *world) anotherDocumentWithout(string) error {
	size, _ := embeddings.ChunkWords(512)
	var text strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&text, "%s\n\n", meshing(size-30))
	}
	if err := w.upload("qubit-notes", w.indexes["qubit-notes"][0], "meshing.md", text.String(), false, bge); err != nil {
		return err
	}
	return w.ingested("qubit-notes")
}

func (w *world) searchesForWith(name, index, query, model string) error {
	w.search(name, index, query, model, 1)
	return nil
}

func (w *world) theDocumentIsFound() error {
	if w.err != nil {
		return w.err
	}
	chunks := w.response.(*gogiv1.SearchDocumentsResponse).GetChunks()
	if len(chunks) == 0 || chunks[0].GetDocumentId() != "flutter-tests.md" {
		return fmt.Errorf("the closest chunk is not the document's: %v", chunks)
	}
	return nil
}

func (w *world) searchesWithoutAModel(name, index, query string) error {
	w.search(name, index, query, "", 3)
	return nil
}

// models applications host

func (w *world) hostsModel(name, servedName, _ string) error {
	// the fake model server stands in for it
	return nil
}

func (w *world) registerEmbeddingModel(name, model, url, credential string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = w.modelsClient().RegisterEmbeddingModel(ctx, &gogiv1.RegisterEmbeddingModelRequest{
		Name: model, Url: url, ServedName: "nomic-embed-text", CredentialRef: credential})
	return nil
}

func (w *world) registersHostedModel(name, model string) error {
	return w.registerEmbeddingModel(name, model, w.model.url(), "")
}

func (w *world) registersHostedModelNeeding(name, model, credential string) error {
	if err := w.registerEmbeddingModel(name, model, w.model.url(), credential); err != nil {
		return err
	}
	return w.err
}

func (w *world) hasRegisteredEmbeddingModel(name, model string) error {
	if err := w.registerEmbeddingModel(name, model, w.model.url(), ""); err != nil {
		return err
	}
	return w.err
}

func (w *world) registersWhereNothingAnswers(name, model string) error {
	return w.registerEmbeddingModel(name, model, fmt.Sprintf("http://%s:1/v1", env("GOGI_BDD_HOST_FROM_GOGI", "host.docker.internal")), "")
}

func (w *world) registersEmbeddingModelAt(name, model, url string) error {
	return w.registerEmbeddingModel(name, model, url, "")
}

func (w *world) isToldUnreachable() error {
	if status.Code(w.err) != codes.Unavailable || !strings.Contains(status.Convert(w.err).Message(), "could not be reached") {
		return fmt.Errorf("expected Unavailable, got %v", w.err)
	}
	return nil
}

func (w *world) listsEmbeddingModel(name, model string) error {
	if w.err != nil {
		return w.err
	}
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	listed, err := w.modelsClient().ListEmbeddingModels(ctx, &gogiv1.ListEmbeddingModelsRequest{})
	if err != nil {
		return err
	}
	for _, listed := range listed.GetModels() {
		if listed.GetName() == model {
			if listed.GetDimensions() != fakeEmbeddingDimensions || !listed.GetAvailable() {
				return fmt.Errorf("the model is listed as %+v", listed)
			}
			return nil
		}
	}
	return fmt.Errorf("%s is not listed: %v", model, listed.GetModels())
}

func (w *world) canIngestWith(model string) error {
	if err := w.upload("qubit-notes", w.indexes["qubit-notes"][0], "fem-notes.md", notes("first"), false, model); err != nil {
		return err
	}
	if err := w.ingested("qubit-notes"); err != nil {
		return err
	}
	return w.isFoundWith("fem-notes.md", w.indexes["qubit-notes"][0], model)
}

func (w *world) ingestsADocumentWith(name, model string) error {
	if err := w.hasIndex(name, "maria--aeronautics"); err != nil {
		return err
	}
	return w.upload(name, "maria--aeronautics", "hull-survey.md", notes("first"), false, model)
}
