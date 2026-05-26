package chunks

type Chunk struct {
	DocumentID  string
	Text        string
	StartOffset int
	EndOffset   int
	Heading     string
	Metadata    map[string]interface{}
}
