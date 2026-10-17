package providers

import (
	"context"
	"fmt"

	LLM "gogi/gogi/llm"
)

// ImageFetcher reads the image a document of the platform's holds: its bytes and its content type
type ImageFetcher func(ctx context.Context, indexName, documentID string) ([]byte, string, error)

// SetImageFetcher sets how the images that messages refer to by document are read; without one, such
// messages are invalid requests
func (p *LLMProviderRouter) SetImageFetcher(fetch ImageFetcher) {
	p.images = fetch
}

// resolveImages replaces the images the messages refer to by document with the documents' bytes, so the
// providers only see images with data. The messages are copied, not changed
func (p *LLMProviderRouter) resolveImages(ctx context.Context, messages []LLM.LLMMessage) ([]LLM.LLMMessage, error) {

	resolved := make([]LLM.LLMMessage, len(messages))
	for i, message := range messages {
		resolved[i] = message
		if len(message.Parts) == 0 {
			continue
		}
		parts := make([]LLM.ContentPart, len(message.Parts))
		for j, part := range message.Parts {
			parts[j] = part
			if part.Image == nil || part.Image.Document == nil {
				continue
			}
			if p.images == nil {
				return nil, invalidRequest("message %d, part %d: images in documents can't be read here", i, j)
			}
			reference := part.Image.Document
			data, contentType, err := p.images(ctx, reference.IndexName, reference.DocumentID)
			if err != nil {
				return nil, fmt.Errorf("message %d, part %d: failed to read document %s of %s: %w",
					i, j, reference.DocumentID, reference.IndexName, err)
			}
			image := &LLM.Image{Data: data, MediaType: part.Image.MediaType}
			if image.MediaType == "" && LLM.ImageMediaTypes[contentType] {
				image.MediaType = contentType
			}
			if err := checkImage(image); err != nil {
				return nil, invalidRequest("message %d, part %d: document %s: %v", i, j, reference.DocumentID, err)
			}
			parts[j] = LLM.ContentPart{Image: image}
		}
		resolved[i].Parts = parts
	}
	return resolved, nil
}
