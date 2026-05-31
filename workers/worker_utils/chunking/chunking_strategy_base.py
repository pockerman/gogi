from abc import ABC, abstractmethod
from typing import List 

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument

class ChunkingStrategy(ABC):
    """Abstract chunking interface (Listing 5.9)."""

    @abstractmethod
    def chunk(self, document: ExtractedDocument) -> List[TextChunk]:
        pass