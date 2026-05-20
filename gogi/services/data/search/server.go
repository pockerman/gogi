package search

import "fmt"

type SearchServer struct{}

func (s *SearchServer) Index() (string, error) {
	fmt.Sprintf("This is the message")
	// return "None", nil
}
