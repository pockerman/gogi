from io import BytesIO

from pypdf import PdfReader

from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.documents.document_section import DocumentSection
from workers.worker_utils.documents.parsers.document_parser_base import DocumentParserBase


class PdfParser(DocumentParserBase):
    """Parses a PDF into a section per entry of its outline (bookmarks), or per page if it has none.

    A section's content is the text of its pages, from its own first page up to the page before the
    next section's: pages are the finest unit a PDF's outline points to. A section that starts on the
    same page as the next one has no content of its own."""

    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        reader = PdfReader(BytesIO(file_bytes))
        pages = [(page.extract_text() or "").strip() for page in reader.pages]

        entries = self._outline(reader)
        if not entries:
            sections = [DocumentSection(content=text, heading=f"Page {number}", level=1, page_number=number,
                                        page_end=number)
                        for number, text in enumerate(pages, start=1)]
            return ExtractedDocument(sections=sections, page_count=len(pages))

        sections = []
        for i, (title, level, start) in enumerate(entries):
            next_start = entries[i + 1][2] if i + 1 < len(entries) else len(pages) + 1
            end = max(start, next_start - 1)
            own_pages = range(start, next_start) if next_start > start else range(0)
            content = "\n\n".join(pages[number - 1] for number in own_pages if pages[number - 1])
            sections.append(DocumentSection(content=content, heading=title, level=level, page_number=start,
                                            page_end=end))
        return ExtractedDocument(sections=sections, page_count=len(pages))

    @staticmethod
    def _outline(reader: PdfReader) -> list[tuple[str, int, int]]:
        """the outline's entries in order, as (title, level, 1-based page)"""
        entries = []

        def walk(items, level):
            for item in items:
                if isinstance(item, list):
                    walk(item, level + 1)
                    continue
                try:
                    page = reader.get_destination_page_number(item) + 1
                except Exception:
                    continue
                entries.append((str(item.title).strip(), level, page))

        try:
            walk(reader.outline, 1)
        except Exception:
            return []
        # an outline out of page order can't give each entry a range of pages
        if any(entries[i][2] > entries[i + 1][2] for i in range(len(entries) - 1)):
            return []
        return entries
