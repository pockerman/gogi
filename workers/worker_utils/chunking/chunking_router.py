from typing import List

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingStrategy

class ChunkingRouter(ChunkingStrategy):
    """Word-count-based fixed-size chunking"""

    def __init__(self, chunking_strategies: dict[str, ChunkingStrategy]):
        self.chunking_strategies = chunking_strategies

    def chunk(self, document: ExtractedDocument) -> List[TextChunk]:
        pass