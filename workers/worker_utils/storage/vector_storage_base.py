from abc import ABC, abstractmethod
from typing import Optional, List, Dict, Any

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.storage.vector_store_search_result import VectorStoreSearchResult

class VectorStorageBase(ABC):
    """Base class for vector storage implementations
    """
    def __init__(self, name: str,  host: str, port: int):
        self.name = name
        self.host = host 
        self.port = port

    @abstractmethod
    def create_index(self, index_name: str) -> Any:
        """Persist a new index. Raises ValueError if name already exists."""
        # TODO: Some dbs may allow as to use thier own embedding functions
        # So we may want to allo this here

    @abstractmethod
    def delete_index(self, index_name: str) -> None:
        """Cascade-delete chunks, documents, and index metadata."""

    @abstractmethod
    def get_index(self, index_name: str) -> Optional[Any]:
        """Return the index metadata or None if not found."""

    def get_or_create_index(self, index_name: str) -> Any:

        index = self.get_index(index_name=index_name)
        if not index:
            index = self.create_index(index_name=index_name)

        return index

    @abstractmethod
    def list_indexes(self) -> List[str]:
        """Return every index in the store."""
        return []

    @abstractmethod
    def insert(
        self,
        index_name: str,
        document_id: str,
        chunks: List[TextChunk],
        embeddings: List[List[float]],
        metadata: Dict[str, str],
    ) -> int:
        """Insert chunks with embeddings. Returns count inserted."""
        # TODO: Rethink about metadata. Do we need it?

    @abstractmethod
    def delete_by_document(self, index_name: str, document_id: str) -> int:
        """Delete chunks and document metadata for a document. Returns chunks deleted."""

   

    @abstractmethod
    def search(
        self,
        index_name: str,
        query_embedding: List[float],
        top_k: int = 5,
        metadata_filters: Optional[Dict[str, str]] = None,
        score_threshold: Optional[float] = None
    ) -> List[VectorStoreSearchResult]:
        """Find chunks most similar to the query embedding."""

    def keyword_search(
        self,
        index_name: str,
        query: str,
        top_k: int = 5,
        metadata_filters: Optional[Dict[str, str]] = None,
    ) -> List[VectorStoreSearchResult]:
        """Find chunks matching the query by keyword (Listing 5.21)."""
        raise NotImplementedError(f"{type(self).__name__} does not support keyword search")

    
