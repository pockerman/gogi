"""Wrapper for ChromaDB
"""
import chromadb
from pydantic import BaseModel
from typing import Optional, Any, override, List, Dict
import uuid


from workers.worker_utils.storage.vector_storage_base import VectorStorageBase
from workers.worker_utils.storage.vector_store_search_result import VectorStoreSearchResult
from workers.worker_utils.chunking.text_chunk_model import TextChunk


   
class ChromaDBHttpWrapper(VectorStorageBase):
    def __init__(self, host: str = '0.0.0.0', port: int = 8003):
        super().__init__(name="chromadb", host=host, port=port)
        self._chroma_client = chromadb.HttpClient(host=host, port=port)

    @override
    def delete_index(self, index_name: str) -> None:
        self._chroma_client.delete_collection(index_name)

    @override
    def create_index(self, index_name: str) -> Optional[Any]:
        return self._chroma_client.create_collection(index_name)
    
    def list_indexes(self) -> List[str]:
        """Return every index in the store."""
        return self._chroma_client.list_collections()

    @override
    def get_index(self, index_name: str) -> Any:
        return self._chroma_client.get_collection(index_name)
    
    @override
    def insert(self,
        index_name: str,
        document_id: str,
        chunks: List[TextChunk],
        embeddings: List[List[float]],
        metadata: Dict[str, str],
    ) -> int:
        
        collection = self._chroma_client.get_collection(index_name)

        # for each chunk we will create one id
        documents = []
        ids = []
        metadatas = []
        for i, chunk in enumerate(chunks):
            ids.append(uuid.uuid4().hex)
            documents.append(chunk.text)
            chunk.metadata['document_id'] = document_id
            metadatas.append[chunk.model_dump()]


        # You must provide either documents, embeddings, or both. 
        # metadatas are always optional. When only providing documents, 
        # Chroma will generate embeddings for you using the collection’s embedding function.
        # Metadata values can be strings, integers, floats, or booleans. 
        # Additionally, you can store arrays of these types.
        collection.add(ids=ids,
                       embeddings=embeddings, 
                       metadatas=metadatas, 
                       documents=documents)

    def search( self,
        index_name: str,
        query_embedding: List[float],
        top_k: int = 5,
        metadata_filters: Optional[Dict[str, str]] = None,
        score_threshold: Optional[float] = None) -> List[VectorStoreSearchResult]:
        collection = self._chroma_client.get_collection(index_name)

        if metadata_filters:
            retrieved_result = collection.query(query_embeddings=query_embedding,
                                                n_results=top_k,
                                                where=metadata_filters)
        else:
            retrieved_result = collection.query(query_embeddings=query_embedding,
                                                n_results=top_k)
            
        # TODO implement the score threshold   
        # 
        results = [] 

        ids = retrieved_result['ids'][0] if 'ids' in retrieved_result else None
        distances = retrieved_result['distances'][0] if 'distances' in retrieved_result else None
        documents = retrieved_result['documents'][0] if 'documents' in retrieved_result else None
        metadatas = retrieved_result['metadatas'] if 'metadatas' in retrieved_result else None
        uris = retrieved_result['uris'][0] if 'uris' in retrieved_result and retrieved_result['uris'] else None
        data = retrieved_result['data'][0] if 'data' in retrieved_result and retrieved_result['data'] else None

        for id, dist, doc, metadata in zip(ids, distances, documents, metadatas):
            results.append(VectorStoreSearchResult(chunk_id=id, text=doc, score=dist, metadata=metadata))

       
        return results  