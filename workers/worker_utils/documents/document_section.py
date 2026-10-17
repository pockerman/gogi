from pydantic import BaseModel
from typing import Optional


class DocumentSection(BaseModel):
    """A section of an extracted document: a chapter, a heading within one, or a page of a PDF
    without an outline. Its content is its own text, without its subsections'."""

    content: str
    # None for text before a document's first heading
    heading: Optional[str] = None
    # 1 for a chapter, 2 for a heading within it, and so on
    level: int = 1
    # the pages of a PDF the section is on
    page_number: Optional[int] = None
    page_end: Optional[int] = None
