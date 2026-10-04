from io import BytesIO

from pypdf import PdfReader

from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.documents.document_section import DocumentSection
from workers.worker_utils.documents.parsers.document_parser_base import DocumentParserBase


class PdfParser(DocumentParserBase):
    """Parses PDF files into page-based sections."""

    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        reader = PdfReader(BytesIO(file_bytes))
        sections = []
        for page_number, page in enumerate(reader.pages, start=1):
            text = (page.extract_text() or "").strip()
            if not text:
                continue
            sections.append(DocumentSection(content=text, page_number=page_number))
        return ExtractedDocument(sections=sections)
