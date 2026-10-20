Feature: Embedding models
  As an application that keeps its users' documents in gogi
  I want to say, each time a document is made searchable, which models embed it
  So that search finds what the documents say, and gogi runs on a small server

  An index is a user's storage area: storing a document in it keeps the document, and nothing
  more. A document becomes searchable only when the application asks gogi to chunk it and names
  the embedding models that embed its chunks: one or more, with no default. Each model's chunks
  and vectors are kept apart, in a collection of the index's for that model, as vectors of
  different models cannot be compared; so a search names one model and finds the documents that
  model embedded. A document is chunked for each model, into chunks the model reads whole.

  gogi runs two small text embedding models itself, "all-MiniLM-L6-v2" and "bge-small-en-v1.5".
  It also embeds with OpenAI's "text-embedding-3-small", using the application's own OpenAI key,
  stored as the credential "openai". Any other model, e.g. a large one, the application hosts
  itself behind an OpenAI-compatible embeddings endpoint and registers with gogi by its URL; gogi
  does not run heavy models.

  Background:
    Given gogi has the application "qubit-notes"
    And "qubit-notes" has the index "maria--aeronautics"

  Rule: Storing a document does not chunk it

    Scenario: A stored document is kept but not searchable
      When "qubit-notes" stores the document "fem-notes.md" in "maria--aeronautics"
      Then "fem-notes.md" is listed in "maria--aeronautics"
      And it has no chunks
      And searching "maria--aeronautics" with "bge-small-en-v1.5" does not find it

  Rule: The application names the models that embed a document each time it is chunked

    Scenario: A document is chunked and embedded by the models the application names
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" with the models:
        | model             |
        | bge-small-en-v1.5 |
        | all-MiniLM-L6-v2  |
      Then "fem-notes.md" is found by searching "maria--aeronautics" with "bge-small-en-v1.5"
      And it is found by searching "maria--aeronautics" with "all-MiniLM-L6-v2"

    Scenario: A document is chunked for each model it is embedded by
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" with the models:
        | model             | reads at most |
        | bge-small-en-v1.5 | 512 tokens    |
        | all-MiniLM-L6-v2  | 256 tokens    |
      Then each model's chunks of "fem-notes.md" are no longer than that model reads
      And "fem-notes.md" has more chunks for "all-MiniLM-L6-v2" than for "bge-small-en-v1.5"

    Scenario: A document is ingested only with the models named
      Given "qubit-notes" has ingested "fem-notes.md" into "maria--aeronautics" with "bge-small-en-v1.5"
      When "qubit-notes" searches "maria--aeronautics" with "all-MiniLM-L6-v2"
      Then "fem-notes.md" is not found

    Scenario: A stored document is chunked later
      Given "qubit-notes" has stored "fem-notes.md" in "maria--aeronautics"
      When "qubit-notes" ingests "fem-notes.md" with "bge-small-en-v1.5"
      Then "fem-notes.md" is found by searching "maria--aeronautics" with "bge-small-en-v1.5"

    Scenario: An ingested document is embedded by another model later
      Given "qubit-notes" has ingested "fem-notes.md" into "maria--aeronautics" with "bge-small-en-v1.5"
      When "qubit-notes" ingests "fem-notes.md" with "all-MiniLM-L6-v2"
      Then "fem-notes.md" is found by searching "maria--aeronautics" with either model

    Scenario: Ingesting without naming a model is refused
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" without naming a model
      Then it is told to name at least one embedding model

    Scenario: A model gogi neither runs nor has registered is refused
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" with "clip"
      Then it is told there is no embedding model "clip"
      And "fem-notes.md" is not stored

    Scenario: Ingesting again with a model replaces what that model embedded
      Given "qubit-notes" has ingested "fem-notes.md" into "maria--aeronautics" with "bge-small-en-v1.5"
      When "qubit-notes" ingests a new version of "fem-notes.md" with "bge-small-en-v1.5"
      Then searching "maria--aeronautics" with "bge-small-en-v1.5" finds only the new version's chunks

    Scenario: A document is found by what it says beyond its opening words
      Given "qubit-notes" has ingested into "maria--aeronautics", with "bge-small-en-v1.5", a document whose chunks each end with a sentence about "wing flutter"
      And another document about the same without "wing flutter"
      When "qubit-notes" searches "maria--aeronautics" for "wing flutter" with "bge-small-en-v1.5"
      Then the document is found

    Scenario: Deleting a document removes what every model embedded
      Given "qubit-notes" has ingested "fem-notes.md" into "maria--aeronautics" with "bge-small-en-v1.5" and "all-MiniLM-L6-v2"
      When "qubit-notes" deletes "fem-notes.md"
      Then searching "maria--aeronautics" with either model does not find it

  Rule: A search names one model

    Scenario: Searching without naming a model is refused
      When "qubit-notes" searches "maria--aeronautics" for "shape functions" without naming a model
      Then it is told to name the embedding model to search with

  Rule: OpenAI's model is used with the application's own OpenAI key

    # runs against a fake OpenAI: see bdd/applications_test.go
    @fake-openai
    Scenario: An application with an OpenAI key embeds with OpenAI
      Given "qubit-notes" has stored the credential "openai"
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" with "text-embedding-3-small"
      Then its chunks are embedded with the "openai" key of "qubit-notes"

    Scenario: An application without an OpenAI key cannot use OpenAI's model
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" with "text-embedding-3-small"
      Then it is told to store its key as the credential "openai"

  Rule: An application can bring a model it hosts

    Scenario: A model hosted by the application is registered by its URL
      Given "qubit-notes" hosts the embedding model "nomic-embed-text" at "https://models.example.org/v1"
      When "qubit-notes" registers the embedding model "nomic" at that URL
      Then "qubit-notes" can list the embedding model "nomic" with its dimensions
      And it can ingest documents with "nomic"

    Scenario: A hosted model that needs a key is called with the application's credential
      Given "qubit-notes" has stored the credential "models-key"
      And "qubit-notes" has registered its hosted model as "nomic", which needs "models-key"
      When "qubit-notes" ingests "fem-notes.md" into "maria--aeronautics" with "nomic"
      Then the model is called with the "models-key" of "qubit-notes"

    Scenario: A model that does not answer is not registered
      When "qubit-notes" registers the embedding model "nomic" at a URL where nothing answers
      Then it is told the model at that URL could not be reached

    Scenario: A model on gogi's own network is not registered
      When "qubit-notes" registers the embedding model "nomic" at "http://169.254.169.254/v1"
      Then it is told the URL is not allowed

    Scenario: An application cannot use another's model
      Given gogi has the application "xact-marine"
      And "qubit-notes" has registered the embedding model "nomic"
      When "xact-marine" ingests a document with "nomic"
      Then it is told there is no embedding model "nomic"
