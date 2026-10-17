from typing import List

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingStrategy, ChunkingType

# the strategy of a request that names none
DEFAULT_CHUNKING_STRATEGY = ChunkingType.FIXED


class ChunkingRouter:
    """Wrapper class to various chunking strategies"""

    def __init__(self, chunking_strategies: dict[str, ChunkingStrategy]):
        self.chunking_strategies = chunking_strategies

    def chunk(self, document: ExtractedDocument, *, chunking_strategy: str,
              chunk_size: int = 512, chunk_overlap: int = 50) -> List[TextChunk]:
        strategy = self.chunking_strategies.get(chunking_strategy or DEFAULT_CHUNKING_STRATEGY)
        if strategy is None:
            raise ValueError(f"unknown chunking strategy {chunking_strategy!r}: use one of "
                             f"{', '.join(self.chunking_strategies)}")
        return strategy.chunk(document=document, chunk_overlap=chunk_overlap, chunk_size=chunk_size)
