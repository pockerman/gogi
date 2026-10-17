"""Builds small PDFs with text and an outline, for tests."""
from io import BytesIO

from pypdf import PdfWriter
from pypdf.generic import DecodedStreamObject, DictionaryObject, NameObject


def make_pdf(pages: list[str], outline: list[tuple[str, int, int]] = ()) -> bytes:
    """a PDF of the pages' texts, with outline entries (title, level, 1-based page) in order"""
    writer = PdfWriter()
    font = writer._add_object(DictionaryObject({
        NameObject("/Type"): NameObject("/Font"),
        NameObject("/Subtype"): NameObject("/Type1"),
        NameObject("/BaseFont"): NameObject("/Helvetica"),
    }))
    for text in pages:
        page = writer.add_blank_page(width=612, height=792)
        stream = DecodedStreamObject()
        escaped = text.replace("\\", "\\\\").replace("(", "\\(").replace(")", "\\)")
        stream.set_data(f"BT /F1 12 Tf 72 720 Td ({escaped}) Tj ET".encode())
        page[NameObject("/Contents")] = writer._add_object(stream)
        page[NameObject("/Resources")] = DictionaryObject({
            NameObject("/Font"): DictionaryObject({NameObject("/F1"): font}),
        })

    parents = {}
    for title, level, page in outline:
        parents[level] = writer.add_outline_item(title, page - 1, parent=parents.get(level - 1))

    output = BytesIO()
    writer.write(output)
    return output.getvalue()
