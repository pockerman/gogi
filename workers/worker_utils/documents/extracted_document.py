from pydantic import BaseModel, Field
from typing import Dict, List

from workers.worker_utils.documents.document_section import DocumentSection

class ExtractedDocument(BaseModel):
    """Parsed document with structural information"""

    sections: List[DocumentSection]
    metadata: Dict[str, str] = Field(default_factory=dict)

    @property
    def text(self) -> str:
        return "\n\n".join(s.content for s in self.sections)