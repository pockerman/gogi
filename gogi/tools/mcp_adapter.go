package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPAdapter connects the platform, as an MCP client, to MCP servers over the
// Streamable HTTP transport: it lists the tools of a server, to import them into the
// registry, and calls them. The credential, if any, is sent as a bearer token
type MCPAdapter struct {
	client     *mcp.Client
	httpClient *http.Client
}

func NewMCPAdapter(httpClient *http.Client) *MCPAdapter {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &MCPAdapter{
		client:     mcp.NewClient(&mcp.Implementation{Name: "gogi-tools", Version: "1.0.0"}, nil),
		httpClient: httpClient,
	}
}

// bearerTransport adds a bearer token to every request
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(request)
}

func (a *MCPAdapter) connect(ctx context.Context, serverURL, credential string) (*mcp.ClientSession, error) {
	httpClient := a.httpClient
	if credential != "" {
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		authorized := *httpClient
		authorized.Transport = &bearerTransport{token: credential, base: base}
		httpClient = &authorized
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:             serverURL,
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	session, err := a.client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the MCP server: %w", err)
	}
	return session, nil
}

// ListTools returns the tools of the MCP server
func (a *MCPAdapter) ListTools(ctx context.Context, serverURL, credential string) ([]*mcp.Tool, error) {
	session, err := a.connect(ctx, serverURL, credential)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	tools := make([]*mcp.Tool, 0)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("failed to list the tools of the MCP server: %w", err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// Invoke calls the tool on its MCP server. The result is the tool's structured content
// if it returns any, or its text content otherwise: as is if the text is JSON, as a JSON
// string if not
func (a *MCPAdapter) Invoke(ctx context.Context, invocation Invocation) (json.RawMessage, error) {
	session, err := a.connect(ctx, invocation.Tool.MCPServerURL, invocation.Credential)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	arguments := json.RawMessage(invocation.ArgumentsJSON)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      invocation.Tool.MCPToolName,
		Arguments: arguments,
	})
	if err != nil {
		return nil, err
	}

	text := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			text = append(text, textContent.Text)
		}
	}

	if result.IsError {
		message := strings.Join(text, "\n")
		if message == "" {
			message = "the tool reported an error"
		}
		return nil, &ToolError{Message: message}
	}

	var output json.RawMessage
	if result.StructuredContent != nil {
		if output, err = json.Marshal(result.StructuredContent); err != nil {
			return nil, &ToolError{Message: fmt.Sprintf("invalid structured content: %v", err)}
		}
	} else {
		output = asJSON([]byte(strings.Join(text, "\n")))
	}

	if int64(len(output)) > invocation.MaxResponseBytes {
		return nil, &ToolError{Message: fmt.Sprintf("tool response exceeds the limit of %d KB",
			invocation.MaxResponseBytes/1024)}
	}
	return output, nil
}
