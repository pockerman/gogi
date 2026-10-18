# Applications

One ``gogi`` deployment can serve many applications, e.g. qubit-notes and xact-marine. Each
application is registered with ``gogi`` by its operator and gets an API key; what it makes is its
own, and it calls the model providers with its own keys. The specification is
``features/applications.feature``.

## Keys

Every call to the gateway carries a key of its application:

```
authorization: Bearer gogi_...
```

A call without a key, with a key ``gogi`` did not give, with a revoked key or with a key of a
disabled application is refused with ``UNAUTHENTICATED``. ``gogi`` keeps only the SHA-256 hash of a
key and its first characters, to tell keys apart, so a key is seen once, when it is made.

The gateway is configured with ``GOGI_APPLICATION_KEYS``:

| Value        | Meaning                                                                        |
|--------------|--------------------------------------------------------------------------------|
| ``off``      | The default. No keys: every call is for the ``default`` application, as in development |
| ``required`` | Every call carries a key; the gateway's health is answered without one         |

Where keys are required, set ``GOGI_REQUIRE_APPLICATION=true`` on the services too, so that a call
that reaches a service without saying which application it is for is refused rather than taken to
be for the ``default`` application.

``docker-compose.yml`` runs both: ``gateway`` on port 50051 without keys, and
``gateway-applications`` on port 50050 with keys required, in front of the same services.

## Managing applications

The operator manages applications with ``gogi-admin``, which works on ``gogi``'s database
(``DATABASE_URL``) and ships in the gateway's image:

```
docker compose exec gateway /root/gogi-admin register qubit-notes
docker compose exec gateway /root/gogi-admin list
docker compose exec gateway /root/gogi-admin new-key qubit-notes
docker compose exec gateway /root/gogi-admin revoke-key qubit-notes gogi_AbCdEfG
docker compose exec gateway /root/gogi-admin disable qubit-notes
docker compose exec gateway /root/gogi-admin enable qubit-notes
docker compose exec gateway /root/gogi-admin delete qubit-notes
```

An application's name is 3 to 40 lowercase letters, digits and dashes. To rotate a key, give the
application a new key, move it to the new key, then revoke the old one: both work in between.

## What an application makes is its own

The gateway passes the application of each call to the services in the ``x-gogi-application``
header; a header the caller sends is not passed on. The services keep the names an application
gives qualified by it, ``qubit-notes.maria--aeronautics``, and give them back as the application
gave them, so two applications use the same names without meeting:

| Service   | Kept apart                                                                  |
|-----------|-----------------------------------------------------------------------------|
| indexes   | indexes, their owners and members; an index found by its id is checked too  |
| documents | the documents of each index, their files and chunks                         |
| sessions  | sessions and the users they and memories are of                             |
| prompts   | prompts, which are found by id, and their content                           |
| llms      | registered models, credentials, and the keys of the built-in providers      |

Tools, workflows, guardrail policies and observability are not kept apart yet.

The ``default`` application's names are kept as they are given, so a deployment that ran without
keys keeps its data; it is ``gogi`` for one application.

## Each application's own keys

An application stores the credentials it needs with ``StoreCredential``; storing one it already has
replaces its value. ``ListCredentials`` lists them, never their values. The keys of the built-in
providers are credentials named after the provider, ``anthropic`` and ``openai``: an application's
calls to Anthropic or OpenAI are made with its key, and refused with ``FAILED_PRECONDITION`` if it
has stored none. The platform's own ``ANTHROPIC_API_KEY`` and ``OPENAI_API_KEY`` are used only for
the ``default`` application.

A model an application registers needs one of its credentials, if any; another application cannot
use it. See [Credentials](credentials.md).

## Running the specification

The features run against a running ``gogi``, with migrations applied:

```
docker compose up -d
migrate -path migrations -database "postgres://gogi:gogi@localhost:5432/gogi?sslmode=disable" up
go test -tags bdd ./bdd/ -v
```

``GOGI_BDD_GATEWAY`` (``localhost:50050``), ``GOGI_BDD_POSTGRES_DSN`` and
``GOGI_BDD_HOST_FROM_GOGI`` (``host.docker.internal``, how the services reach the fake model server
the steps start) configure the run.
