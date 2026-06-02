from abc import ABC, abstractmethod
from typing import Final, Dict
from enum import StrEnum

from workers.worker_utils.documents.extracted_document import ExtractedDocument


class ParserType(StrEnum):

    TEXT = "text"
    MARKDOWN = "markdown"
    PDF = "pdf"
    DOCX = "docx"
    HTML = "html"

class DocumentParserBase(ABC):
    """Abstract parser interface"""


    EXTENSION_MAP: Final[Dict[str, str]] = {
        ".txt": "text",
        ".text": "text",
        ".md": "markdown",
        ".markdown": "markdown",
        ".pdf": "pdf",
        ".docx": "docx",
        ".html": "html",
        ".htm": "html",
    }

    @staticmethod
    def detect_format(filename: str, file_bytes: bytes) -> str:
        """Detect document format from magic bytes, then extension fallback."""
        if file_bytes[:5] == b"%PDF-":
            return "pdf"
        if file_bytes[:4] == b"PK\x03\x04":
            return "docx"

        ext = ""
        if "." in filename:
            ext = "." + filename.rsplit(".", 1)[-1].lower()
        return DocumentParserBase.EXTENSION_MAP.get(ext, ParserType.TEXT)

    @abstractmethod
    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        pass