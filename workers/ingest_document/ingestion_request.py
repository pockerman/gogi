from pydantic import BaseModel, Field
from typing import Optional, Dict

class DocumentIngestionRequest(BaseModel):
    job_id: str
    index_name: str 
    filename: str 
    document_id: str 
    batch_size: int
    chunk_size: int = 512
    chunk_overlap: int = 50
    embeddings_model: str 
    embeddings_client: str 
    chunk_strategy: str
    metadata: Optional[Dict[str, str]]
    content: bytes
    