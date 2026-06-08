from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.documents.document_section import DocumentSection
from workers.worker_utils.documents.parsers.document_parser_base import DocumentParserBase

class TextParser(DocumentParserBase):
    """Parses plain text files into paragraph-based sections."""

    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        text = file_bytes.decode("utf-8", errors="replace")
        if not text.strip():
            return ExtractedDocument(sections=[])
        paragraphs = [p.strip() for p in text.split("\n\n") if p.strip()]
        sections = [DocumentSection(content=p) for p in paragraphs]
        return ExtractedDocument(sections=sections)