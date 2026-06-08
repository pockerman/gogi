from typing import List

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingStrategy

class ChunkingRouter:
    """Wrapper class to various chunking strategies"""

    def __init__(self, chunking_strategies: dict[str, ChunkingStrategy]):
        self.chunking_strategies = chunking_strategies

    def chunk(self, document: ExtractedDocument, *, chunking_strategy: str, 
              chunk_size: int = 512, chunk_overlap: int = 50) -> List[TextChunk]:
        return self.chunking_strategies[chunking_strategy].chunk(document=document,
                                                                 chunk_overlap=chunk_overlap,
                                                                 chunk_size=chunk_size)