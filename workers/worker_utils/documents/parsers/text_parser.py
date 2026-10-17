from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.documents.document_section import DocumentSection
from workers.worker_utils.documents.parsers.document_parser_base import DocumentParserBase

class TextParser(DocumentParserBase):
    """Parses a plain text file into a single section: plain text has no structure to go by."""

    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        text = file_bytes.decode("utf-8", errors="replace").strip()
        if not text:
            return ExtractedDocument(sections=[])
        return ExtractedDocument(sections=[DocumentSection(content=text)])
