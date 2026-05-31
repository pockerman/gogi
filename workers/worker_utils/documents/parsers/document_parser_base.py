from abc import ABC, abstractmethod
from typing import Final, Dict

from workers.worker_utils.documents.extracted_document import ExtractedDocument




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
        return DocumentParserBase.EXTENSION_MAP.get(ext, "text")

    @abstractmethod
    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        pass