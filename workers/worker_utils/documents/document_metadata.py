from pydantic import BaseModel, Field
from typing import Optional, Dict
from datetime import datetime


class DocumentMetadata:
    """Metadata tracked for each ingested document (Listing 5.7)."""

    document_id: str
    index_name: str
    filename: str
    ingested_at: datetime = Field(default_factory=datetime.utcnow)
    chunk_count: int = 0
    page_count: Optional[int] = None
    word_count: Optional[int] = None
    custom_metadata: Optional[Dict[str, str]] = None