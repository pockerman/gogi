package vector

// Factory function to create a new VectorStore based on the specified type and configuration.
func NewVectorStoreFactory(store_type string, url string, config map[string]interface{}) (VectorStore, error) {
	switch store_type {
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
