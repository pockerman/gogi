package vector

// Factory function to create a new VectorStore based on the specified type and configuration.
func NewVectorStoreFactory(storeType string, config map[string]interface{}) (VectorStore, error) {
	switch storeType {
	case "in-memory":
		return NewInMemoryVectorStore(), nil
	// case "disk":
	// 	return NewDiskVectorStore(config)
	// case "cloud":
	// 	return NewCloudVectorStore(config)
	default:
		return nil, nil
	}
}
