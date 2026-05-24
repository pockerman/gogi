package impl

type IndexService interface {
	CreateIndex(config IndexConfig) (Index, error)
	ListIndexes() ([]Index, error)
	GetIndex(indexName string) (Index, error)
	DeleteIndex(indexName string) error
}
