from abc import ABC, abstractmethod
from typing import List
from enum import StrEnum 

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument

class ChunkingType(StrEnum):

    FIXED = "fixed"
    

class ChunkingStrategy(ABC):
    """Abstract chunking interface"""

    def __init__(self, name: ChunkingType):
        self.name = name

    @abstractmethod
    def chunk(self, document: ExtractedDocument, *, chunk_size: int = 512, chunk_overlap: int = 50) -> List[TextChunk]:
        pass