import re

from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.documents.document_section import DocumentSection
from workers.worker_utils.documents.parsers.document_parser_base import DocumentParserBase

# an ATX heading, e.g. "## 2.1 Linear elements"; up to three spaces may precede it
HEADING = re.compile(r"^ {0,3}(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$")
FENCE = re.compile(r"^ {0,3}(```|~~~)")


class MarkdownParser(DocumentParserBase):
    """Parses a Markdown file into a section per heading, the heading's level being its section's.
    Text before the first heading is a section without a heading. Lines in code blocks are never headings."""

    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        text = file_bytes.decode("utf-8", errors="replace")

        sections = []
        heading, level, lines = None, 1, []

        def close():
            content = "\n".join(lines).strip()
            if heading is not None or content:
                sections.append(DocumentSection(content=content, heading=heading, level=level))

        fence = None
        for line in text.splitlines():
            fence_match = FENCE.match(line)
            if fence_match:
                marker = fence_match.group(1)
                fence = None if fence == marker else (fence or marker)
            match = HEADING.match(line) if fence is None and not fence_match else None
            if match:
                close()
                heading, level, lines = match.group(2).strip(), len(match.group(1)), []
            else:
                lines.append(line)
        close()
        return ExtractedDocument(sections=sections)
