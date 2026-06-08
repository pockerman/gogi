from typing import Optional, override, List, Dict, Any
from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.storage.vector_storage_base import VectorStorageBase
from workers.worker_utils.storage.vector_store_search_result import VectorStoreSearchResult
from workers.worker_utils.storage.chromadb_wrapper import ChromaDBHttpWrapper

class VectorStorageRouter(VectorStorageBase):

    @classmethod
    def build(cls, name: str,  host: str, port: int) -> VectorStorageBase:

        if name == "chromadb":
            return ChromaDBHttpWrapper(host=host, port=port)
        
        raise ValueError(f"VectorDB '{name}' is invalid")


    def __init__(self, name: str,  host: str, port: int):
        super().__init__(name=name, port=port, host=host)
        self._impl: VectorStorageBase  = VectorStorageRouter.build(name=name, port=port, host=host)

    @override
    def delete_index(self, index_name: str) -> None:
        self._impl.delete_index(index_name)

    @override
    def create_index(self, index_name: str) -> Optional[Any]:
        return self._impl.create_index(index_name)
    
    def list_indexes(self) -> List[str]:
        """Return every index in the store."""
        return self._impl.list_indexes()

    @override
    def get_index(self, index_name: str) -> Any:
        return self._impl.get_index(index_name)
    
    @override
    def insert(self,
        index_name: str,
        document_id: str,
        chunks: List[TextChunk],
        embeddings: List[List[float]],
        metadata: Dict[str, str],
    ) -> int:
        
        return self._impl.insert(index_name=index_name, document_id=document_id,
                                 chunks=chunks, embeddings=embeddings, metadata=metadata)

    @override
    def search(self, index_name: str,
        query_embedding: List[float],
        top_k: int = 5,
        metadata_filters: Optional[Dict[str, str]] = None,
        score_threshold: Optional[float] = None) -> List[VectorStoreSearchResult]:
        return self._impl.search(index_name=index_name, query_embedding=query_embedding, top_k=top_k,
                                 metadata_filters=metadata_filters, score_threshold=score_threshold)
    
    @override
    def delete_by_document(self, index_name: str, document_id: str) -> int:
        """Delete chunks and document metadata for a document. Returns chunks deleted."""
        return self._impl.delete_by_document(index_name=index_name, document_id=document_id)
