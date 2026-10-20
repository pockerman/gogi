from pydantic import BaseModel, Field
from typing import List


class DocumentDeleteRequest(BaseModel):
    index_name: str
    document_id: str
    # the index's collections the document has chunks in, one for each model that embedded it
    collections: List[str] = Field(default_factory=list)
