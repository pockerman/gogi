package ingestion

import "fmt"

type IngestionServer struct{}

func (s *IngestionServer) Index() (string, error) {
	fmt.Sprintf("This is the message")
	// return "None", nil
}
