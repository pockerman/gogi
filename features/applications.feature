Feature: Applications subscribe to gogi
  As the operator of a gogi deployment
  I want each application that uses gogi to have its own keys and its own data
  So that one gogi serves many applications, e.g. qubit-notes and xact-marine, without them
  seeing or spending each other's

  An application is registered with gogi by its operator and gets an API key, which goes with
  every call to the gateway (authorization: Bearer gogi_...). gogi keeps only a hash of the key
  and shows it once. What an application makes (indexes and their documents, sessions and
  memories, prompts, registered models and credentials) is its own: two applications can use the
  same names without meeting. Each application brings its own model providers' keys, stored as
  credentials named after the provider, e.g. "anthropic"; gogi uses an application's keys only for
  that application's calls, and never its own for an application.

  Tools, workflows, guardrail policies and observability are not kept apart yet.

  Background:
    Given gogi has the applications "qubit-notes" and "xact-marine"

  Rule: Every call says which application it is for, with a key gogi gave it

    Scenario: Registering an application gives it a key once
      When the operator registers the application "survey-tool"
      Then "survey-tool" is given an API key
      And the key is not shown again when the operator lists the applications

    Scenario Outline: Calls without a valid key are refused
      When an application calls gogi <with>
      Then it is told it is not authenticated

      Examples:
        | with                                  |
        | without a key                         |
        | with a key gogi did not give          |
        | with a key that has been revoked      |
        | with a key of a disabled application  |

    Scenario: A key is rotated without stopping the application
      When the operator gives "qubit-notes" a new key
      Then "qubit-notes" can call gogi with its new key
      And "qubit-notes" can still call gogi with its old key
      When the operator revokes the old key of "qubit-notes"
      Then "qubit-notes" cannot call gogi with its old key

  Rule: An application sees only what it made

    Scenario: Two applications have indexes of the same name
      Given "qubit-notes" has the index "maria--aeronautics" with the document "fem-notes.md"
      And "xact-marine" has the index "maria--aeronautics" with the document "hull-survey.pdf"
      When "qubit-notes" lists the documents of "maria--aeronautics"
      Then it gets only "fem-notes.md"

    Scenario: An application cannot reach another's index
      Given "qubit-notes" has the index "maria--aeronautics" with the document "fem-notes.md"
      When "xact-marine" searches "maria--aeronautics" for "shape functions"
      Then it is told there is no index "maria--aeronautics"

    Scenario: An application cannot read another's session
      Given "qubit-notes" has a session with the message "What is a shape function?"
      When "xact-marine" asks for the messages of that session
      Then it is told there is no such session

    Scenario: Two applications have prompts of the same name
      Given "qubit-notes" has the prompt "summarise" saying "Summarise the section"
      And "xact-marine" has the prompt "summarise" saying "Summarise the finding"
      When "qubit-notes" gets its prompt "summarise"
      Then it says "Summarise the section"

    Scenario: An application cannot read another's prompt
      Given "qubit-notes" has the prompt "summarise" saying "Summarise the section"
      When "xact-marine" gets the prompt of "qubit-notes" by its id
      Then it is told there is no such prompt

  Rule: Each application uses its own model providers' keys

    Scenario: A registered model is called with the key of the application that registered it
      Given "qubit-notes" has stored the credential "model-key"
      And "xact-marine" has stored the credential "model-key"
      And "qubit-notes" and "xact-marine" have each registered the model "echo", which needs "model-key"
      When "qubit-notes" runs the model "echo"
      Then the model is called with the "model-key" of "qubit-notes"

    Scenario: An application cannot use another's credential
      Given "qubit-notes" has stored the credential "model-key"
      When "xact-marine" registers the model "echo", which needs "model-key"
      Then it is told there is no credential "model-key"

    Scenario: An application calls the built-in providers with its own key, not gogi's
      When "xact-marine" runs an Anthropic model without having stored an Anthropic key
      Then it is told to store its key as the credential "anthropic"

    Scenario: A stored credential is never given back
      Given "qubit-notes" has stored the credential "model-key"
      When "qubit-notes" lists its credentials
      Then it sees "model-key" without its value
      And "xact-marine" sees no credentials
