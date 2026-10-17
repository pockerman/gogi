from typing import List, override
from loguru import logger

from workers.worker_utils.chunking.text_chunk_model import TextChunk
from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.chunking.chunking_strategy_base import ChunkingStrategy, ChunkingType

class FixedSizeChunking(ChunkingStrategy):
    """Word-count-based fixed-size chunking, within each section of the document: a chunk never
    spans two sections, and carries its section's position and title in its metadata."""

    def __init__(self):
        super().__init__(name=ChunkingType.FIXED)

    @override
    def chunk(self, document: ExtractedDocument, *, chunk_size: int = 512, chunk_overlap: int = 50) -> List[TextChunk]:

        logger.debug(f"Chunking document with strategy: {self.name} with chunk_size={chunk_size}, chunk_overlap={chunk_overlap}")

        chunks: List[TextChunk] = []
        step = max(1, chunk_size - chunk_overlap)
        # offsets are into document.text, which joins the sections with a blank line
        section_offset = 0
        for position, section in enumerate(document.sections, start=1):
            text = section.content
            metadata = {"section": str(position), "section_title": section.heading or ""}

            # the offset of every word, found once, so chunking is linear in the words
            words, starts = [], []
            cursor = 0
            for word in text.split():
                cursor = text.find(word, cursor)
                words.append(word)
                starts.append(cursor)
                cursor += len(word)

            for i in range(0, len(words), step):
                chunk_words = words[i : i + chunk_size]
                start = starts[i]
                end = starts[i + len(chunk_words) - 1] + len(chunk_words[-1])
                chunks.append(TextChunk(text=" ".join(chunk_words), start_offset=section_offset + start,
                                        end_offset=section_offset + end, heading=section.heading,
                                        metadata=metadata))
                if i + chunk_size >= len(words):
                    break

            section_offset += len(text) + 2

        return chunks
