package indexes

import "fmt"

type IndexServer struct{}

func (s *IndexServer) Index() (string, error) {
	fmt.Sprintf("This is the message")
	// return "None", nil
}
