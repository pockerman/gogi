package applications

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	gogiv1 "gogi/gogi/gogi/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestQualify(t *testing.T) {
	if got := Qualify("qubit-notes", "maria--aeronautics"); got != "qubit-notes.maria--aeronautics" {
		t.Fatalf("got %q", got)
	}
	if got := Unqualify("qubit-notes", "qubit-notes.maria--aeronautics"); got != "maria--aeronautics" {
		t.Fatalf("got %q", got)
	}
	// the Default application's names are kept as they are, and nothing is not a name
	if got := Qualify(Default, "maria--aeronautics"); got != "maria--aeronautics" {
		t.Fatalf("got %q", got)
	}
	if got := Qualify("qubit-notes", ""); got != "" {
		t.Fatalf("got %q", got)
	}
	// a name with a "." of its own is split at the application's
	if got := Unqualify("qubit-notes", Qualify("qubit-notes", "llama3.2")); got != "llama3.2" {
		t.Fatalf("got %q", got)
	}

	if !Owns("qubit-notes", "qubit-notes.maria--aeronautics") || Owns("xact-marine", "qubit-notes.maria--aeronautics") {
		t.Fatal("an application owns only its names")
	}
	if !Owns(Default, "qubit-notes.maria--aeronautics") || !Owns(Default, "maria--aeronautics") {
		t.Fatal("everything is the Default application's")
	}
}

func TestCheckName(t *testing.T) {
	for _, name := range []string{"qubit-notes", "xact-marine", "app"} {
		if err := CheckName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"default", "Qubit", "qubit.notes", "q", "-app", "app-", "1app", strings.Repeat("a", 41)} {
		if err := CheckName(name); err == nil {
			t.Errorf("%q should not be a name", name)
		}
	}
}

func TestKeys(t *testing.T) {
	key, other := NewKey(), NewKey()
	if key == other || !strings.HasPrefix(key, "gogi_") || !looksLikeKey(key) {
		t.Fatalf("unexpected keys %q %q", key, other)
	}
	if Hash(key) == Hash(other) || Hash(key) != Hash(key) || strings.Contains(Hash(key), key) {
		t.Fatal("a key's hash is its own and does not show it")
	}
	if len(Shown(key)) != 12 || !strings.HasPrefix(key, Shown(key)) {
		t.Fatalf("shown %q", Shown(key))
	}
}

func TestScopeRewritesNamesAtAnyDepth(t *testing.T) {
	scope := NewScope(false, "index_name", "owner", "user")

	request := &gogiv1.SetIndexMemberRequest{IndexName: "maria--aeronautics", User: "tom"}
	scope.Qualify("qubit-notes", request)
	if request.IndexName != "qubit-notes.maria--aeronautics" || request.User != "qubit-notes.tom" {
		t.Fatalf("not qualified: %v", request)
	}

	response := &gogiv1.ListIndexesResponse{Indexes: []*gogiv1.IndexResponse{{
		Id: "an-id", IndexName: "qubit-notes.maria--aeronautics", Owner: "qubit-notes.maria",
		Members: []*gogiv1.IndexMember{{User: "qubit-notes.maria"}, {User: "qubit-notes.tom"}},
	}}}
	scope.Unqualify("qubit-notes", response)
	index := response.Indexes[0]
	if index.IndexName != "maria--aeronautics" || index.Owner != "maria" || index.Id != "an-id" ||
		index.Members[0].User != "maria" || index.Members[1].User != "tom" {
		t.Fatalf("not unqualified: %v", response)
	}
}

// keys is a KeyChecker of the keys it holds
type keys map[string]string

func (k keys) ApplicationOf(_ context.Context, key string) (string, error) {
	if application, ok := k[key]; ok {
		return application, nil
	}
	return "", ErrNotAuthenticated
}

// indexes is an index service that answers with the application the call came for, and the
// index name as it was given to it
type indexes struct {
	gogiv1.UnimplementedIndexServiceServer
}

func (indexes) GetIndexByName(ctx context.Context, req *gogiv1.GetIndexByNameRequest) (*gogiv1.IndexResponse, error) {
	if req.IndexName == "missing" || strings.HasSuffix(req.IndexName, ".missing") {
		return nil, status.Errorf(codes.NotFound, "index %s not found", req.IndexName)
	}
	md, _ := metadata.FromIncomingContext(ctx)
	return &gogiv1.IndexResponse{Id: FromContext(ctx) + "|" + strings.Join(md.Get(Header), ","), IndexName: req.IndexName}, nil
}

// serve is a client of the index service, behind the interceptors
func serve(t *testing.T, options ...grpc.ServerOption) gogiv1.IndexServiceClient {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(options...)
	gogiv1.RegisterIndexServiceServer(server, indexes{})
	go server.Serve(listener)
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return gogiv1.NewIndexServiceClient(conn)
}

func TestGatewayAuthenticatesCalls(t *testing.T) {
	gateway, err := NewGateway(KeysRequired, keys{"gogi_qubit-notes-key": "qubit-notes"})
	if err != nil {
		t.Fatal(err)
	}
	// the gateway's interceptor in front of a service that reads what the gateway sends on
	client := serve(t, append(gateway.ServerOptions(), grpc.ChainUnaryInterceptor(
		func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			// what the gateway sends on is what the service receives
			md, _ := metadata.FromOutgoingContext(ctx)
			return handler(metadata.NewIncomingContext(context.Background(), md), req)
		}))...)

	call := func(authorization ...string) (*gogiv1.IndexResponse, error) {
		ctx := metadata.AppendToOutgoingContext(context.Background(), Header, "xact-marine")
		if len(authorization) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", authorization[0])
		}
		return client.GetIndexByName(ctx, &gogiv1.GetIndexByNameRequest{IndexName: "maria--aeronautics"})
	}

	// the key says which application the call is for, whatever header the caller sent
	response, err := call("Bearer gogi_qubit-notes-key")
	if err != nil {
		t.Fatal(err)
	}
	if response.Id != "qubit-notes|qubit-notes" {
		t.Fatalf("the service was told %q", response.Id)
	}

	for _, authorization := range [][]string{{}, {"Bearer gogi_unknown-key-1234"}, {"gogi_qubit-notes-key"}, {"Basic gogi_qubit-notes-key"}} {
		if _, err := call(authorization...); status.Code(err) != codes.Unauthenticated {
			t.Errorf("%v: expected Unauthenticated, got %v", authorization, err)
		}
	}
}

func TestGatewayWithoutKeys(t *testing.T) {
	gateway, err := NewGateway(KeysOff, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := serve(t, append(gateway.ServerOptions(), grpc.ChainUnaryInterceptor(
		func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			md, _ := metadata.FromOutgoingContext(ctx)
			return handler(metadata.NewIncomingContext(context.Background(), md), req)
		}))...)

	response, err := client.GetIndexByName(context.Background(), &gogiv1.GetIndexByNameRequest{IndexName: "maria--aeronautics"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Id != Default+"|"+Default {
		t.Fatalf("the service was told %q", response.Id)
	}

	if _, err := NewGateway(KeysRequired, nil); err == nil {
		t.Fatal("keys cannot be required with nothing to check them")
	}
	if _, err := NewGateway("sometimes", nil); err == nil {
		t.Fatal("an unknown mode is refused")
	}
}

func TestScopeInterceptor(t *testing.T) {
	client := serve(t, NewScope(true, "index_name").ServerOptions()...)
	forQubitNotes := metadata.AppendToOutgoingContext(context.Background(), Header, "qubit-notes")

	// the service is given the qualified name, and the application gets back the name it gave
	response, err := client.GetIndexByName(forQubitNotes, &gogiv1.GetIndexByNameRequest{IndexName: "maria--aeronautics"})
	if err != nil {
		t.Fatal(err)
	}
	if response.IndexName != "maria--aeronautics" || response.Id != "qubit-notes|qubit-notes" {
		t.Fatalf("got %v", response)
	}

	// errors name things as the application does
	_, err = client.GetIndexByName(forQubitNotes, &gogiv1.GetIndexByNameRequest{IndexName: "missing"})
	if status.Code(err) != codes.NotFound || status.Convert(err).Message() != "index missing not found" {
		t.Fatalf("got %v", err)
	}

	// a call that does not say which application it is for is refused, where that is required
	if _, err := client.GetIndexByName(context.Background(), &gogiv1.GetIndexByNameRequest{IndexName: "x"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
	// and is for the Default application where not
	optional := serve(t, NewScope(false, "index_name").ServerOptions()...)
	response, err = optional.GetIndexByName(context.Background(), &gogiv1.GetIndexByNameRequest{IndexName: "maria--aeronautics"})
	if err != nil || response.IndexName != "maria--aeronautics" || !strings.HasPrefix(response.Id, Default+"|") {
		t.Fatalf("got %v %v", response, err)
	}
}

func TestErrorsOfOtherKinds(t *testing.T) {
	if err := unqualifyError("qubit-notes", errors.New("qubit-notes.x")); err.Error() != "qubit-notes.x" {
		t.Fatal("errors without a status are left as they are")
	}
}
