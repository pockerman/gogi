from pydantic import BaseModel
from typing import Optional


class DocumentSection(BaseModel):
    """A section of an extracted document """

    content: str
    heading: Optional[str] = None
    level: int = 0
    page_number: Optional[int] = None