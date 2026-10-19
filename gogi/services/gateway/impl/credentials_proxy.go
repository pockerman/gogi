package impl

import (
	"context"
	gogiv1 "gogi/gogi/gogi/v1"
)

// CredentialsProxy routes the applications' credentials to the llms service, which holds them
type CredentialsProxy struct {
	gogiv1.UnimplementedCredentialServerServer
	proxy *GenericGRPCProxy
}

func (p *CredentialsProxy) StoreCredential(ctx context.Context, req *gogiv1.StoreCredentialRequest) (*gogiv1.StoreCredentialResponse, error) {
	conn, err := p.proxy.buildConnection("llms")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return gogiv1.NewCredentialServerClient(conn).StoreCredential(ctx, req)
}

func (p *CredentialsProxy) ListCredentials(ctx context.Context, req *gogiv1.ListCredentialsRequest) (*gogiv1.ListCredentialsResponse, error) {
	conn, err := p.proxy.buildConnection("llms")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return gogiv1.NewCredentialServerClient(conn).ListCredentials(ctx, req)
}
