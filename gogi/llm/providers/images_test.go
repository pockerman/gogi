package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gogiv1 "gogi/gogi/gogi/v1"
	"gogi/gogi/llm"
)

// the signature of a PNG, which is enough to tell its type
var png = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, '\r', 'I', 'H', 'D', 'R'}

func userMessage(parts ...*gogiv1.ContentPart) *gogiv1.LLMMessage {
	content := "What is this?"
	return &gogiv1.LLMMessage{Role: llm.RoleUser, Content: &content, Parts: parts}
}

func imagePart(image *gogiv1.ImageContent) *gogiv1.ContentPart {
	return &gogiv1.ContentPart{Part: &gogiv1.ContentPart_Image{Image: image}}
}

func textPart(text string) *gogiv1.ContentPart {
	return &gogiv1.ContentPart{Part: &gogiv1.ContentPart_Text{Text: text}}
}

func TestConvertMessagesWithImages(t *testing.T) {
	messages, err := convertLLMMessages([]*gogiv1.LLMMessage{userMessage(
		imagePart(&gogiv1.ImageContent{Source: &gogiv1.ImageContent_Data{Data: png}}),
		textPart("and this one?"),
		imagePart(&gogiv1.ImageContent{Source: &gogiv1.ImageContent_Document{Document: &gogiv1.DocumentReference{
			IndexName: "elena--surveys", DocumentId: "hull-port-bow"}}}),
	)})
	if err != nil {
		t.Fatal(err)
	}

	parts := messages[0].Parts
	if len(parts) != 3 {
		t.Fatalf("parts = %+v", parts)
	}
	// the media type is worked out from the image
	if parts[0].Image == nil || parts[0].Image.MediaType != "image/png" {
		t.Errorf("first part = %+v, want a PNG", parts[0])
	}
	if parts[1].Text != "and this one?" {
		t.Errorf("second part = %+v", parts[1])
	}
	if reference := parts[2].Image.Document; reference == nil || reference.DocumentID != "hull-port-bow" {
		t.Errorf("third part = %+v, want a document", parts[2])
	}
}

func TestInvalidImages(t *testing.T) {
	content := "hi"
	tests := []struct {
		name    string
		message *gogiv1.LLMMessage
		want    string
	}{
		{"an unsupported type", userMessage(imagePart(&gogiv1.ImageContent{Source: &gogiv1.ImageContent_Data{Data: []byte("%PDF-1.4")}})), "unsupported image type"},
		{"no data or document", userMessage(imagePart(&gogiv1.ImageContent{})), "needs its data or a document"},
		{"a document without its id", userMessage(imagePart(&gogiv1.ImageContent{Source: &gogiv1.ImageContent_Document{
			Document: &gogiv1.DocumentReference{IndexName: "elena--surveys"}}})), "index_name and document_id"},
		{"an empty part", userMessage(&gogiv1.ContentPart{}), "text or an image"},
		{"parts on an assistant message", &gogiv1.LLMMessage{Role: llm.RoleAssistant, Content: &content,
			Parts: []*gogiv1.ContentPart{textPart("x")}}, "only user messages have parts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := convertLLMMessages([]*gogiv1.LLMMessage{test.message})
			if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), test.want) {
				t.Errorf("err = %v, want an invalid request saying %q", err, test.want)
			}
		})
	}
}

func documentMessage() []llm.LLMMessage {
	return []llm.LLMMessage{{Role: llm.RoleUser, Content: "What is this?", Parts: []llm.ContentPart{
		{Image: &llm.Image{Document: &llm.DocumentReference{IndexName: "elena--surveys", DocumentID: "hull-port-bow"}}},
	}}}
}

func TestImagesInDocumentsAreRead(t *testing.T) {
	router := NewLLMProviderRouter(nil)
	var asked string
	router.SetImageFetcher(func(_ context.Context, indexName, documentID string) ([]byte, string, error) {
		asked = indexName + "/" + documentID
		return png, "image/png", nil
	})
	messages := documentMessage()

	resolved, err := router.resolveImages(context.Background(), messages)

	if err != nil {
		t.Fatal(err)
	}
	image := resolved[0].Parts[0].Image
	if asked != "elena--surveys/hull-port-bow" || string(image.Data) != string(png) || image.MediaType != "image/png" || image.Document != nil {
		t.Errorf("asked for %s, resolved %+v", asked, image)
	}
	if messages[0].Parts[0].Image.Document == nil {
		t.Error("the request's messages were changed")
	}
}

func TestImagesInDocumentsNeedAFetcher(t *testing.T) {
	_, err := NewLLMProviderRouter(nil).resolveImages(context.Background(), documentMessage())

	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want an invalid request", err)
	}
}

func TestADocumentThatIsNotAnImage(t *testing.T) {
	router := NewLLMProviderRouter(nil)
	router.SetImageFetcher(func(context.Context, string, string) ([]byte, string, error) {
		return []byte("%PDF-1.4"), "application/pdf", nil
	})

	_, err := router.resolveImages(context.Background(), documentMessage())

	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "unsupported image type") {
		t.Errorf("err = %v, want an unsupported image type", err)
	}
}

func imageMessages() []llm.LLMMessage {
	return []llm.LLMMessage{{Role: llm.RoleUser, Content: "What is this?", Parts: []llm.ContentPart{
		{Image: &llm.Image{Data: png, MediaType: "image/png"}},
		{Text: "Be brief."},
	}}}
}

func TestAnthropicSendsImages(t *testing.T) {
	params, err := (&AnthropicLLMModelProvider{}).preparePayload(imageMessages(), llm.LLMModelConfig{ModelName: "claude-opus-5-5"})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(params)
	var result struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	content := result.Messages[0].Content
	if len(content) != 3 || content[0].Text != "What is this?" || content[2].Text != "Be brief." {
		t.Fatalf("content = %+v", content)
	}
	image := content[1]
	if image.Type != "image" || image.Source.Type != "base64" || image.Source.MediaType != "image/png" ||
		image.Source.Data != base64.StdEncoding.EncodeToString(png) {
		t.Errorf("image block = %+v", image)
	}
}

func TestOpenAISendsImagesAsDataURLs(t *testing.T) {
	body, err := (&OpenAILLMModelProvider{}).preparePayload(imageMessages(), llm.LLMModelConfig{ModelName: "gpt-4o"}, false)
	if err != nil {
		t.Fatal(err)
	}

	var result struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	content := result.Messages[0].Content
	if len(content) != 3 || content[0].Text != "What is this?" || content[1].Type != "image_url" ||
		content[1].ImageURL.URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png) {
		t.Errorf("content = %+v", content)
	}

	// a message without images still has plain text content
	body, _ = (&OpenAILLMModelProvider{}).preparePayload([]llm.LLMMessage{{Role: "user", Content: "hi"}}, llm.LLMModelConfig{}, false)
	if !strings.Contains(string(body), `"content":"hi"`) {
		t.Errorf("a text message = %s", body)
	}
}
