from pydantic import BaseModel, Field
from typing import Optional, Dict

class TextChunk(BaseModel):
    """Model for representing a chunk of a piece of text
    """

    text: str
    start_offset: int
    end_offset: int
    heading: Optional[str] = None
    metadata: Dict[str, str] = Field(default_factory=dict)