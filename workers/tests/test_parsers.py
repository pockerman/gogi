from workers.tests.pdfs import make_pdf
from workers.worker_utils.chunking.fixed_size_chunking import FixedSizeChunking
from workers.worker_utils.documents.extracted_document import ExtractedDocument
from workers.worker_utils.documents.document_section import DocumentSection
from workers.worker_utils.documents.parsers.markdown_parser import MarkdownParser
from workers.worker_utils.documents.parsers.pdf_parser import PdfParser
from workers.worker_utils.documents.parsers.text_parser import TextParser


def outline(document):
    return [(s.heading, s.level, s.content) for s in document.sections]


def test_a_markdown_file_has_a_section_per_heading():
    document = MarkdownParser().parse(b"""Notes for week 1.

# 1 Introduction
The method divides a domain.

## 1.1 History
Courant, 1943.

```python
# not a heading
```

# 2 Shape functions ##
Interpolate inside elements.
""", "fem.md")

    assert outline(document) == [
        (None, 1, "Notes for week 1."),
        ("1 Introduction", 1, "The method divides a domain."),
        ("1.1 History", 2, "Courant, 1943.\n\n```python\n# not a heading\n```"),
        ("2 Shape functions", 1, "Interpolate inside elements."),
    ]


def test_a_heading_without_text_is_still_a_section():
    document = MarkdownParser().parse(b"# 1 Chapter\n## 1.1 Section\nText.", "fem.md")

    assert outline(document) == [("1 Chapter", 1, ""), ("1.1 Section", 2, "Text.")]


def test_a_text_file_is_a_single_section():
    document = TextParser().parse(b"First paragraph.\n\nSecond paragraph.\n", "notes.txt")

    assert outline(document) == [(None, 1, "First paragraph.\n\nSecond paragraph.")]


def test_a_pdf_without_an_outline_has_a_section_per_page():
    document = PdfParser().parse(make_pdf(["one", "two", "three"]), "lecture.pdf")

    assert outline(document) == [("Page 1", 1, "one"), ("Page 2", 1, "two"), ("Page 3", 1, "three")]
    assert [(s.page_number, s.page_end) for s in document.sections] == [(1, 1), (2, 2), (3, 3)]
    assert document.page_count == 3


def test_a_pdf_with_an_outline_has_a_section_per_entry_over_its_pages():
    pdf = make_pdf(["intro", "shape", "linear", "assembly"],
                   [("1 Introduction", 1, 1), ("2 Shape functions", 1, 2), ("2.1 Linear", 2, 3), ("3 Assembly", 1, 4)])

    document = PdfParser().parse(pdf, "lecture.pdf")

    assert outline(document) == [
        ("1 Introduction", 1, "intro"),
        ("2 Shape functions", 1, "shape"),
        ("2.1 Linear", 2, "linear"),
        ("3 Assembly", 1, "assembly"),
    ]
    assert [(s.page_number, s.page_end) for s in document.sections] == [(1, 1), (2, 2), (3, 3), (4, 4)]


def test_chunks_stay_within_their_section_and_say_which_it_is():
    document = ExtractedDocument(sections=[
        DocumentSection(content="one two three four five", heading="1 Intro"),
        DocumentSection(content="six seven", heading="2 Next", level=1),
    ])

    chunks = FixedSizeChunking().chunk(document, chunk_size=3, chunk_overlap=1)

    assert [(c.text, c.metadata["section"], c.metadata["section_title"]) for c in chunks] == [
        ("one two three", "1", "1 Intro"),
        ("three four five", "1", "1 Intro"),
        ("six seven", "2", "2 Next"),
    ]
    assert document.text[chunks[2].start_offset:chunks[2].end_offset] == "six seven"
