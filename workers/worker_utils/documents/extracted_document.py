from pydantic import BaseModel, Field
from typing import Dict, List

from workers.worker_utils.documents.document_section import DocumentSection

class ExtractedDocument(BaseModel):
    """Parsed document with structural information"""

    sections: List[DocumentSection]
    metadata: Dict[str, str] = Field(default_factory=dict)
    # the number of pages of a PDF; 0 for other documents
    page_count: int = 0

    @property
    def text(self) -> str:
        return "\n\n".join(s.content for s in self.sections)