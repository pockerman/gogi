from pydantic import BaseModel, Field
from typing import Optional, Dict, List


class DocumentSearchRequest(BaseModel):
    index_name: str
    query: str
    top_k: int = 5
    # the application whose index it is, and the model, of its, that embeds the query
    application: str
    embedding_model: str
    # the index's collection for the model
    collection: str
    document_ids: Optional[List[str]] = Field(default_factory=list)
    metadata_filter: Optional[Dict[str, str]] = Field(default_factory=dict)
