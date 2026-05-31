from pydantic import BaseModel, Field
from typing import Dict


class VectorStoreSearchResult(BaseModel):
    """A single result from vector or hybrid search."""

    chunk_id: str
    document_id: str
    text: str
    score: float
    metadata: Dict[str, str] = Field(default_factory=dict)