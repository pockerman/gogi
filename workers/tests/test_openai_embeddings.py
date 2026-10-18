"""
Run from the repository root: PYTHONPATH=. pytest workers/tests
"""
from workers.worker_utils.embeddings.openai_embeddings import OpenAIEmbeddings


def test_the_worker_starts_without_an_openai_key(monkeypatch):
    # where no material is embedded with OpenAI's models, a deployment has no key for them
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)

    embeddings = OpenAIEmbeddings()

    assert embeddings.model_name == "text-embedding-3-small"


def test_the_key_is_read_when_openai_is_first_called(monkeypatch):
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    embeddings = OpenAIEmbeddings()
    monkeypatch.setenv("OPENAI_API_KEY", "sk-test")

    assert embeddings.client.api_key == "sk-test"
