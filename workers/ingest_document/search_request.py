from pydantic import BaseModel, Field
from typing import Optional, Dict, List


class DocumentSearchRequest(BaseModel):
    index_name: str
    query: str
    top_k: int = 5
    embeddings_model: str
    embeddings_client: str
    document_ids: Optional[List[str]] = Field(default_factory=list)
    metadata_filter: Optional[Dict[str, str]] = Field(default_factory=dict)
