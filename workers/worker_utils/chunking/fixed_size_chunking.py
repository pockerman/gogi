from typing import List, override
from loguru import logger

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingStrategy, ChunkingType

class FixedSizeChunking(ChunkingStrategy):
    """Word-count-based fixed-size chunking"""

    def __init__(self):
        super().__init__(name=ChunkingType.FIXED)

    @override
    def chunk(self, document: ExtractedDocument, *, chunk_size: int = 512, chunk_overlap: int = 50) -> List[TextChunk]:


        logger.debug(f"Chunking document with strategy: {self.name} with chunk_size={chunk_size}, chunk_overlap={chunk_overlap}")

        text = document.text
        if not text.strip():
            return []

        words = text.split()
        chunks: List[TextChunk] = []
        step = max(1, chunk_size - chunk_overlap)
        i = 0

        while i < len(words):
            chunk_words = words[i : i + chunk_size]
            chunk_text = " ".join(chunk_words)
            search_start = sum(len(w) + 1 for w in words[:i])
            start_offset = text.find(chunk_words[0], search_start) if chunk_words else 0
            end_offset = start_offset + len(chunk_text)
            chunks.append(TextChunk(text=chunk_text, start_offset=start_offset, end_offset=end_offset))
            i += step

        return chunks