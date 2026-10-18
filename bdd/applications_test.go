//go:build bdd

// Package bdd runs gogi's Gherkin specifications, features/*.feature, against a running gogi:
//
//	docker compose up -d && migrate -path migrations -database "$DSN" up
//	go test -tags bdd ./bdd/
//
// GOGI_BDD_GATEWAY is the gateway that requires keys (localhost:50050, gateway-applications in
// docker-compose), GOGI_BDD_POSTGRES_DSN its database, where the steps register applications as
// the operator does with gogi-admin, and GOGI_BDD_HOST_FROM_GOGI the name gogi's containers reach
// this machine by, for the fake model server.
package bdd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"gogi/gogi/applications"
	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/storage/postgres"

	"github.com/cucumber/godog"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func TestFeatures(t *testing.T) {
	pool, err := postgres.NewPool(env("GOGI_BDD_POSTGRES_DSN", "postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := grpc.NewClient(env("GOGI_BDD_GATEWAY", "localhost:50050"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	model, err := startFakeModel()
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()

	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			w := &world{pool: pool, repo: applications.NewRepository(pool), conn: conn, model: model}
			w.steps(sc)
		},
		Options: &godog.Options{Format: "pretty", Paths: []string{"../features"}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("the features failed")
	}
}

// application is an application of a scenario: registered under a name of its own, so scenarios
// and runs do not meet
type application struct {
	name string
	keys []string
}

type world struct {
	pool  *pgxpool.Pool
	repo  *applications.Repository
	conn  *grpc.ClientConn
	model *fakeModel

	suffix       string
	applications map[string]*application
	// the outcome of the last call
	err      error
	response proto.Message
	// what the applications made, to look up and clean up
	indexes  map[string][]string
	sessions map[string]string
	prompts  map[string]string
}

func (w *world) steps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		suffix := make([]byte, 3)
		rand.Read(suffix)
		w.suffix = hex.EncodeToString(suffix)
		w.applications = map[string]*application{}
		w.indexes, w.sessions, w.prompts = map[string][]string{}, map[string]string{}, map[string]string{}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		return ctx, w.cleanUp()
	})

	sc.Step(`^gogi has the applications "([^"]*)" and "([^"]*)"$`, w.hasApplications)

	sc.Step(`^the operator registers the application "([^"]*)"$`, w.register)
	sc.Step(`^"([^"]*)" is given an API key$`, w.isGivenAKey)
	sc.Step(`^the key is not shown again when the operator lists the applications$`, w.keyIsNotShownAgain)
	sc.Step(`^an application calls gogi (without a key|with a key gogi did not give|with a key that has been revoked|with a key of a disabled application)$`, w.callsWith)
	sc.Step(`^it is told it is not authenticated$`, w.isNotAuthenticated)
	sc.Step(`^the operator gives "([^"]*)" a new key$`, w.givesANewKey)
	sc.Step(`^"([^"]*)" can call gogi with its new key$`, w.canCallWith(-1, true))
	sc.Step(`^"([^"]*)" can still call gogi with its old key$`, w.canCallWith(0, true))
	sc.Step(`^"([^"]*)" cannot call gogi with its old key$`, w.canCallWith(0, false))
	sc.Step(`^the operator revokes the old key of "([^"]*)"$`, w.revokesTheOldKey)

	sc.Step(`^"([^"]*)" has the index "([^"]*)" with the document "([^"]*)"$`, w.hasIndexWithDocument)
	sc.Step(`^"([^"]*)" lists the documents of "([^"]*)"$`, w.listsDocuments)
	sc.Step(`^it gets only "([^"]*)"$`, w.getsOnly)
	sc.Step(`^"([^"]*)" searches "([^"]*)" for "([^"]*)"$`, w.searches)
	sc.Step(`^it is told there is no index "([^"]*)"$`, func(index string) error { return w.isToldNotFound(index) })
	sc.Step(`^"([^"]*)" has a session with the message "([^"]*)"$`, w.hasSession)
	sc.Step(`^"([^"]*)" asks for the messages of that session$`, w.asksForTheMessages)
	sc.Step(`^it is told there is no such session$`, func() error { return w.isToldNotFound("") })
	sc.Step(`^"([^"]*)" has the prompt "([^"]*)" saying "([^"]*)"$`, w.hasPrompt)
	sc.Step(`^"([^"]*)" gets its prompt "([^"]*)"$`, w.getsItsPrompt)
	sc.Step(`^it says "([^"]*)"$`, w.says)
	sc.Step(`^"([^"]*)" gets the prompt of "([^"]*)" by its id$`, w.getsPromptOf)
	sc.Step(`^it is told there is no such prompt$`, func() error { return w.isToldNotFound("") })

	sc.Step(`^"([^"]*)" has stored the credential "([^"]*)"$`, w.storesCredential)
	sc.Step(`^"([^"]*)" and "([^"]*)" have each registered the model "([^"]*)", which needs "([^"]*)"$`, w.eachRegisters)
	sc.Step(`^"([^"]*)" runs the model "([^"]*)"$`, w.runs)
	sc.Step(`^the model is called with the "([^"]*)" of "([^"]*)"$`, w.modelIsCalledWith)
	sc.Step(`^"([^"]*)" registers the model "([^"]*)", which needs "([^"]*)"$`, w.registersModel)
	sc.Step(`^it is told there is no credential "([^"]*)"$`, w.isToldNoCredential)
	sc.Step(`^"([^"]*)" runs an Anthropic model without having stored an Anthropic key$`, w.runsAnthropic)
	sc.Step(`^it is told to store its key as the credential "([^"]*)"$`, w.isToldToStore)
	sc.Step(`^"([^"]*)" lists its credentials$`, w.listsCredentials)
	sc.Step(`^it sees "([^"]*)" without its value$`, w.seesCredential)
	sc.Step(`^"([^"]*)" sees no credentials$`, w.seesNoCredentials)
}

// applications and their keys

func (w *world) app(name string) (*application, error) {
	if app, ok := w.applications[name]; ok {
		return app, nil
	}
	return nil, fmt.Errorf("the scenario has no application %q", name)
}

func (w *world) register(name string) error {
	registered := name + "-" + w.suffix
	key, err := w.repo.Register(context.Background(), registered)
	if err != nil {
		return err
	}
	w.applications[name] = &application{name: registered, keys: []string{key}}
	return nil
}

func (w *world) hasApplications(first, second string) error {
	if err := w.register(first); err != nil {
		return err
	}
	return w.register(second)
}

// as is the context of a call with the key
func as(key string) context.Context {
	if key == "" {
		return context.Background()
	}
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+key)
}

// for is the context of a call of the application, with its newest key
func (w *world) for_(name string) (context.Context, error) {
	app, err := w.app(name)
	if err != nil {
		return nil, err
	}
	return as(app.keys[len(app.keys)-1]), nil
}

func (w *world) indexesClient() gogiv1.IndexServiceClient {
	return gogiv1.NewIndexServiceClient(w.conn)
}

// call is any call to gogi, with the key
func (w *world) call(key string) error {
	_, err := w.indexesClient().ListIndexes(as(key), &gogiv1.ListIndexesRequest{OwnerName: "maria"})
	return err
}

func (w *world) isGivenAKey(name string) error {
	app, err := w.app(name)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(app.keys[0], "gogi_") {
		return fmt.Errorf("%q is not a gogi key", app.keys[0])
	}
	return w.call(app.keys[0])
}

func (w *world) keyIsNotShownAgain() error {
	listed, err := w.repo.List(context.Background())
	if err != nil {
		return err
	}
	for _, app := range w.applications {
		for _, registered := range listed {
			if registered.Name != app.name {
				continue
			}
			for _, key := range registered.Keys {
				if key.Shown == app.keys[0] || len(key.Shown) >= len(app.keys[0])/2 {
					return fmt.Errorf("the list shows %q of the key", key.Shown)
				}
			}
		}
	}
	// nor is any of the key's hash kept that would give it back: the table has no key column
	var columns int
	err = w.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.columns WHERE table_name = 'gogi_application_keys' AND column_name = 'key'`).Scan(&columns)
	if err != nil || columns != 0 {
		return fmt.Errorf("the keys are kept: %v", err)
	}
	return nil
}

func (w *world) callsWith(with string) error {
	key := ""
	switch with {
	case "with a key gogi did not give":
		key = applications.NewKey()
	case "with a key that has been revoked":
		app, _ := w.app("qubit-notes")
		revoked, err := w.repo.NewKey(context.Background(), app.name)
		if err != nil {
			return err
		}
		if err := w.repo.RevokeKey(context.Background(), app.name, revoked); err != nil {
			return err
		}
		key = revoked
	case "with a key of a disabled application":
		app, _ := w.app("xact-marine")
		if err := w.repo.SetDisabled(context.Background(), app.name, true); err != nil {
			return err
		}
		key = app.keys[0]
	}
	w.err = w.call(key)
	return nil
}

func (w *world) isNotAuthenticated() error {
	if status.Code(w.err) != codes.Unauthenticated {
		return fmt.Errorf("expected Unauthenticated, got %v", w.err)
	}
	return nil
}

func (w *world) givesANewKey(name string) error {
	app, err := w.app(name)
	if err != nil {
		return err
	}
	key, err := w.repo.NewKey(context.Background(), app.name)
	if err != nil {
		return err
	}
	app.keys = append(app.keys, key)
	return nil
}

func (w *world) canCallWith(which int, can bool) func(string) error {
	return func(name string) error {
		app, err := w.app(name)
		if err != nil {
			return err
		}
		if which < 0 {
			which = len(app.keys) - 1
		}
		err = w.call(app.keys[which])
		if can && err != nil {
			return fmt.Errorf("the call failed: %v", err)
		}
		if !can && status.Code(err) != codes.Unauthenticated {
			return fmt.Errorf("expected Unauthenticated, got %v", err)
		}
		return nil
	}
}

func (w *world) revokesTheOldKey(name string) error {
	app, err := w.app(name)
	if err != nil {
		return err
	}
	return w.repo.RevokeKey(context.Background(), app.name, app.keys[0])
}

// what an application makes

func (w *world) hasIndexWithDocument(name, index, filename string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	if _, err := w.indexesClient().CreateIndex(ctx, &gogiv1.CreateIndexRequest{IndexName: index, Owner: "maria"}); err != nil {
		return err
	}
	w.indexes[name] = append(w.indexes[name], index)

	upload, err := gogiv1.NewDocumentServerClient(w.conn).UploadDocument(ctx)
	if err != nil {
		return err
	}
	parts := []*gogiv1.UploadDocumentRequest{
		{Part: &gogiv1.UploadDocumentRequest_Info{Info: &gogiv1.IngestDocumentRequest{
			IndexName: index, Filename: filename, ContentType: "text/plain", StoreOnly: true}}},
		{Part: &gogiv1.UploadDocumentRequest_Chunk{Chunk: []byte("Shape functions interpolate within an element.")}},
	}
	for _, part := range parts {
		if err := upload.Send(part); err != nil {
			return err
		}
	}
	_, err = upload.CloseAndRecv()
	return err
}

func (w *world) listsDocuments(name, index string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = gogiv1.NewDocumentServerClient(w.conn).ListDocuments(ctx, &gogiv1.ListDocumentsRequest{IndexName: index})
	return nil
}

func (w *world) getsOnly(filename string) error {
	if w.err != nil {
		return w.err
	}
	documents := w.response.(*gogiv1.ListDocumentsResponse).GetDocuments()
	if len(documents) != 1 || documents[0].GetFilename() != filename {
		return fmt.Errorf("got %v", documents)
	}
	if strings.Contains(documents[0].GetIndexName(), ".") {
		return fmt.Errorf("the index is named %q", documents[0].GetIndexName())
	}
	return nil
}

func (w *world) searches(name, index, query string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = gogiv1.NewDocumentServerClient(w.conn).SearchDocuments(ctx,
		&gogiv1.SearchDocumentsRequest{IndexName: index, Query: query, TopK: 3})
	return nil
}

// isToldNotFound checks the last call found nothing; what it names, it names as the application does
func (w *world) isToldNotFound(name string) error {
	if status.Code(w.err) != codes.NotFound {
		return fmt.Errorf("expected NotFound, got %v (%v)", w.err, w.response)
	}
	message := status.Convert(w.err).Message()
	for _, app := range w.applications {
		if strings.Contains(message, app.name) {
			return fmt.Errorf("the error names the application: %q", message)
		}
	}
	if name != "" && !strings.Contains(message, name) {
		return fmt.Errorf("the error does not name %q: %q", name, message)
	}
	return nil
}

func (w *world) sessionsClient() gogiv1.LLMSessionServerClient {
	return gogiv1.NewLLMSessionServerClient(w.conn)
}

func (w *world) hasSession(name, message string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	session, err := w.sessionsClient().GetOrCreateSession(ctx, &gogiv1.GetOrCreateSessionRequest{UserId: "maria"})
	if err != nil {
		return err
	}
	id := session.GetSession().GetSessionId()
	if strings.Contains(id, ".") {
		return fmt.Errorf("the session's id is %q", id)
	}
	w.sessions[name] = id
	_, err = w.sessionsClient().AddMessages(ctx, &gogiv1.AddMessagesRequest{SessionId: id,
		Messages: []*gogiv1.LLMMessage{{Role: "user", Content: proto.String(message)}}})
	if err != nil {
		return err
	}
	// the application reads its own session
	messages, err := w.sessionsClient().GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: id})
	if err != nil || len(messages.GetMessages()) != 1 {
		return fmt.Errorf("the application cannot read its session: %v %v", messages, err)
	}
	return nil
}

func (w *world) asksForTheMessages(name string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	// the session of the other application of the scenario
	session := ""
	for other, id := range w.sessions {
		if other != name {
			session = id
		}
	}
	w.response, w.err = w.sessionsClient().GetMessages(ctx, &gogiv1.GetMessagesRequest{SessionId: session})
	return nil
}

func (w *world) promptsClient() gogiv1.PromptServerClient {
	return gogiv1.NewPromptServerClient(w.conn)
}

func (w *world) hasPrompt(name, prompt, content string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	registered, err := w.promptsClient().RegisterPrompt(ctx, &gogiv1.PromptRegistrationRequest{
		PromptName: prompt, PromptVersion: "v1", Content: []byte(content)})
	if err != nil {
		return err
	}
	w.prompts[name+"|"+prompt] = registered.GetPromptId()
	return nil
}

func (w *world) getsItsPrompt(name, prompt string) error {
	return w.getPrompt(name, name, prompt)
}

func (w *world) getsPromptOf(name, owner string) error {
	return w.getPrompt(name, owner, "")
}

func (w *world) getPrompt(name, owner, prompt string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	id := ""
	for key, registered := range w.prompts {
		if strings.HasPrefix(key, owner+"|") && (prompt == "" || key == owner+"|"+prompt) {
			id = registered
		}
	}
	w.response, w.err = w.promptsClient().GetPrompt(ctx, &gogiv1.PromptGetRequest{PromptId: id})
	return nil
}

func (w *world) says(content string) error {
	if w.err != nil {
		return w.err
	}
	prompt := w.response.(*gogiv1.PromptGetResponse)
	if string(prompt.GetContent()) != content || strings.Contains(prompt.GetPromptName(), ".") {
		return fmt.Errorf("got %q named %q", prompt.GetContent(), prompt.GetPromptName())
	}
	return nil
}

// models and credentials

func secretOf(name string) string {
	return "secret-of-" + name
}

func (w *world) credentialsClient() gogiv1.CredentialServerClient {
	return gogiv1.NewCredentialServerClient(w.conn)
}

func (w *world) modelsClient() gogiv1.LLMModelServerClient {
	return gogiv1.NewLLMModelServerClient(w.conn)
}

func (w *world) storesCredential(name, credential string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	_, err = w.credentialsClient().StoreCredential(ctx, &gogiv1.StoreCredentialRequest{Name: credential, Value: secretOf(name)})
	return err
}

func (w *world) registersModel(name, model, credential string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = w.modelsClient().RegisterLLM(ctx, &gogiv1.RegisterLLMRequest{
		Info:          &gogiv1.ModelInfo{Name: model, Provider: "echo-server"},
		Endpoint:      w.model.url(),
		AdapterType:   "openai",
		CredentialRef: credential,
	})
	return nil
}

func (w *world) eachRegisters(first, second, model, credential string) error {
	for _, name := range []string{first, second} {
		if err := w.registersModel(name, model, credential); err != nil {
			return err
		}
		if w.err != nil {
			return w.err
		}
	}
	return nil
}

func (w *world) runs(name, model string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = w.modelsClient().Run(ctx, &gogiv1.LLMRunRequest{
		Config:   &gogiv1.LLMRunRequestConfig{Provider: "echo-server", Model: model, Fallback: &gogiv1.FallbackConfig{Enabled: proto.Bool(false)}},
		Messages: []*gogiv1.LLMMessage{{Role: "user", Content: proto.String("Hello")}},
	})
	return nil
}

func (w *world) modelIsCalledWith(credential, name string) error {
	if w.err != nil {
		return w.err
	}
	call := w.model.last()
	if call.authorization != "Bearer "+secretOf(name) {
		return fmt.Errorf("the model was called with %q", call.authorization)
	}
	// by the name it was registered with, and the application gets that name back
	if call.model != "echo" || w.response.(*gogiv1.LLMRunResponse).GetModel() != "echo" {
		return fmt.Errorf("the model was called as %q and answered as %q", call.model, w.response.(*gogiv1.LLMRunResponse).GetModel())
	}
	return nil
}

func (w *world) isToldNoCredential(credential string) error {
	if status.Code(w.err) != codes.InvalidArgument {
		return fmt.Errorf("expected InvalidArgument, got %v", w.err)
	}
	if message := status.Convert(w.err).Message(); !strings.Contains(message, `"`+credential+`"`) {
		return fmt.Errorf("the error does not name %q as the application does: %q", credential, message)
	}
	return nil
}

func (w *world) runsAnthropic(name string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = w.modelsClient().Run(ctx, &gogiv1.LLMRunRequest{
		Config:   &gogiv1.LLMRunRequestConfig{Provider: "anthropic", Model: "claude-haiku-4-5", Fallback: &gogiv1.FallbackConfig{Enabled: proto.Bool(false)}},
		Messages: []*gogiv1.LLMMessage{{Role: "user", Content: proto.String("Hello")}},
	})
	return nil
}

func (w *world) isToldToStore(credential string) error {
	if status.Code(w.err) != codes.FailedPrecondition {
		return fmt.Errorf("expected FailedPrecondition, got %v", w.err)
	}
	if message := status.Convert(w.err).Message(); !strings.Contains(message, `credential "`+credential+`"`) {
		return fmt.Errorf("got %q", message)
	}
	return nil
}

func (w *world) listsCredentials(name string) error {
	ctx, err := w.for_(name)
	if err != nil {
		return err
	}
	w.response, w.err = w.credentialsClient().ListCredentials(ctx, &gogiv1.ListCredentialsRequest{})
	return nil
}

func (w *world) seesCredential(credential string) error {
	if w.err != nil {
		return w.err
	}
	listed := w.response.(*gogiv1.ListCredentialsResponse)
	if len(listed.GetCredentials()) != 1 || listed.GetCredentials()[0].GetName() != credential {
		return fmt.Errorf("got %v", listed)
	}
	if text := prototext.Format(listed); strings.Contains(text, "secret-of-") {
		return fmt.Errorf("the value is given back: %s", text)
	}
	return nil
}

func (w *world) seesNoCredentials(name string) error {
	if err := w.listsCredentials(name); err != nil {
		return err
	}
	if w.err != nil {
		return w.err
	}
	if listed := w.response.(*gogiv1.ListCredentialsResponse); len(listed.GetCredentials()) != 0 {
		return fmt.Errorf("got %v", listed)
	}
	return nil
}

// cleanUp removes what the scenario's applications made, then the applications
func (w *world) cleanUp() error {
	ctx := context.Background()
	for name, app := range w.applications {
		// an application disabled by the scenario is let make its last calls
		w.repo.SetDisabled(ctx, app.name, false)
		as, _ := w.for_(name)
		for _, index := range w.indexes[name] {
			w.indexesClient().DeleteIndexByName(as, &gogiv1.DeleteIndexByNameRequest{IndexName: index})
		}
		if session, ok := w.sessions[name]; ok {
			w.sessionsClient().DeleteSession(as, &gogiv1.DeleteSessionRequest{SessionId: session, DeleteMemories: true})
		}
		for key, id := range w.prompts {
			if strings.HasPrefix(key, name+"|") {
				w.promptsClient().DeletePrompt(as, &gogiv1.PromptDeleteRequest{PromptId: id})
			}
		}
		for _, statement := range []string{
			`DELETE FROM gogi_registered_llms WHERE name LIKE $1 || '.%'`,
			`DELETE FROM gogi_credentials WHERE application = $1`,
		} {
			if _, err := w.pool.Exec(ctx, statement, app.name); err != nil {
				return err
			}
		}
		if err := w.repo.Delete(ctx, app.name); err != nil {
			return err
		}
	}
	return nil
}

// fakeModel is a model server with an OpenAI-compatible API, that records how it is called
type fakeModel struct {
	server   *http.Server
	listener net.Listener
	mu       sync.Mutex
	calls    []modelCall
}

type modelCall struct {
	authorization string
	model         string
}

func startFakeModel() (*fakeModel, error) {
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	fake := &fakeModel{listener: listener}
	fake.server = &http.Server{Handler: http.HandlerFunc(fake.serve)}
	go fake.server.Serve(listener)
	return fake, nil
}

func (f *fakeModel) url() string {
	return fmt.Sprintf("http://%s:%d/v1", env("GOGI_BDD_HOST_FROM_GOGI", "host.docker.internal"),
		f.listener.Addr().(*net.TCPAddr).Port)
}

func (f *fakeModel) serve(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Model string `json:"model"`
	}
	var body bytes.Buffer
	body.ReadFrom(r.Body)
	json.Unmarshal(body.Bytes(), &request)
	f.mu.Lock()
	f.calls = append(f.calls, modelCall{authorization: r.Header.Get("Authorization"), model: request.Model})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"1","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"echo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, request.Model)
}

func (f *fakeModel) last() modelCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return modelCall{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeModel) Close() error {
	return f.server.Close()
}
