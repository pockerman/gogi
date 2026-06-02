from pydantic import BaseModel, Field
from typing import Optional, Dict

class DocumentIngestionRequest(BaseModel):
    index_name: str 
    filename: str 
    document_id: str 
    batch_size: int
    embeddings_model: str 
    embeddings_client: str 
    chunk_strategy: str
    metadata: Optional[Dict[str, str]]
    content: bytes