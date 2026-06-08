from typing import override 
from workers.worker_utils.documents.parsers.document_parser_base import DocumentParserBase
from workers.worker_utils.documents.parsers.text_parser import TextParser
from workers.worker_utils.documents.extracted_document import ExtractedDocument

class ParseRouter(DocumentParserBase):

    def __init__(self, parsers: dict[str, DocumentParserBase]):
        self.parsers = parsers

    @override
    def parse(self, file_bytes: bytes, filename: str) -> ExtractedDocument:
        format = self.detect_format(filename=filename, file_bytes=file_bytes)
        return self.parsers[format].parse(file_bytes=file_bytes, filename=filename)