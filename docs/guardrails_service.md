# Guardrails service

The guardrails service evaluates content and actions against named policies (chapter 6.6-6.8 of the book). It
covers the five inspection points of a request:

| Inspection point                 | RPC             | Policy types                         |
|----------------------------------|-----------------|--------------------------------------|
| 1. Input, before the model       | `ValidateInput` | `input`                              |
| 2. Before an action, e.g. a tool call | `CheckPolicy` | `behavioral`, `human_gate`         |
| 3. The action's arguments        | `CheckPolicy`   | `tool_validation`                    |
| 4. Responses of external systems | `FilterOutput`  | `output`                             |
| 5. Output, before the user       | `FilterOutput`  | `output`                             |

Applications call the service explicitly, as in the book's listing 6.20. The model and tools services do not call
it yet.

## Policies

A policy has a name, a type, the application it applies to, a mode and a list of rules:

```json
{
  "name": "travel-desk-input",
  "type": "input",
  "application_id": "travel-desk",
  "mode": "enforce",
  "version": "1.0.0",
  "rules_json": "[{\"type\": \"prompt_injection\", \"action\": \"block\", \"response\": \"I can help with trips and bookings only.\"}]"
}
```

- **name**: lowercase letters, digits, `.`, `_` and `-`.
- **application_id**: the application the policy applies to. Empty means every application.
- **mode**: `enforce` (default) or `shadow`. A policy in shadow mode is evaluated and recorded, with its triggered
  rules marked `shadow`, but never blocks, redacts or denies anything. Use it to see what a new policy would do
  before enforcing it.
- **version**: a label for the author's own use, e.g. `1.2.0`. The platform keeps its own `revision`, which goes up
  every time the policy changes.

`AddPolicy` adds a policy, or replaces the one with the same name. It checks every rule first and reports every
problem at once. Rules that are valid but not enforced yet, e.g. those that need a classifier, are accepted and
listed in the response's `warnings`. Fields a rule does not use are listed there too, so typos show up.

### Applying policies

- `ValidateInput` and `FilterOutput` apply the policies named in `policy_names`, plus, if `application_id` is
  given, the application's policies of the right type and those of every application. They also run the built-in
  `checks` or `filters` the request names.
- `CheckPolicy` applies the policy named in `policy_name`, or the application's `tool_validation`, `behavioral`
  and `human_gate` policies, plus those of every application.

A policy name that doesn't exist fails with `NOT_FOUND`, and a policy of the wrong type with `INVALID_ARGUMENT`.
Neither is silently skipped.

### Policies as configuration

The service loads the YAML files (`.yaml`, `.yml`) of `GOGI_GUARDRAILS_POLICY_DIR` when it starts (listing 6.26). A
file can hold several policies, separated by `---`:

```yaml
name: travel-desk-input
version: "1.0.0"
application: travel-desk
type: input
rules:
  - type: prompt_injection
    action: block
    response: "I can help with trips and bookings only."
```

A file's policy replaces the policy with the same name, including one added through the API. Loading the same
files again changes nothing, so every replica can load them. An invalid file, an unknown field or a name defined
twice stops the service from starting, so a broken policy never goes unnoticed.

With Docker Compose, mount a directory of policy files into the `guardrails` service and set
`GOGI_GUARDRAILS_POLICY_DIR` to it. On Kubernetes, the deployment mounts the `gogi-guardrail-policies` ConfigMap,
if it exists, at `/etc/gogi/guardrails`:

```bash
kubectl -n gogi create configmap gogi-guardrail-policies --from-file=<directory of policy files>
```

## Input and output rules

Each rule has a `type` (or a list of `types`), an `action` and optional `severity` and message fields.

| Type               | Finds                                                                                     | Fields                       |
|--------------------|-------------------------------------------------------------------------------------------|------------------------------|
| `prompt_injection` | Common injection phrasings: "ignore previous instructions", "you are now in developer mode", "reveal your system prompt", `system:` lines, `<system>` tags | `patterns`: extra regular expressions |
| `pii_detection`    | Credit card numbers (Luhn-checked), SSNs, email addresses, phone numbers, IP addresses   | `categories`: `credit_card`, `ssn`, `email`, `phone`, `ip_address`; all if empty |
| `blocked_terms`    | The terms, as whole words, whatever their case                                            | `terms`                      |
| `regex`            | Matches of the regular expressions                                                        | `patterns`                   |
| `max_length`       | Content longer than `max_chars` characters                                                | `max_chars`                  |

`pii`, `pii_exposure` and `pii_redaction` are other names for `pii_detection`.

The pattern detectors are a first layer only. Paraphrased injections need a classifier. `topic_classifier`,
`medical_advice`, `factual_error`, `unverified_claim` and `hallucination` rules are accepted with a warning but
not enforced yet. The same goes for the `transform` action (e.g. `add_hedging_language`), which only logs.

**Input actions**

| Action     | Effect                                                              |
|------------|---------------------------------------------------------------------|
| `block` (default) | Denies the input; `user_message` is the rule's `response` or `message` |
| `redirect` | Same as `block`; the message points the user elsewhere              |
| `warn`     | Allows the input; the triggered rule is reported and recorded       |
| `log_only` | Same as `warn`                                                      |

**Output actions**

| Action     | Effect                                                              |
|------------|---------------------------------------------------------------------|
| `redact` (default for `pii_detection`, `blocked_terms`, `regex`) | Replaces what was found with `replacement`, `[REDACTED]` by default |
| `block` (default for the others) | Replaces the whole response with the rule's `fallback`, `response` or `message` |
| `warn`, `log_only` | Leaves the response as it is                                |

Severity tiers (section 6.7.2) are written as rules with a `severity` and a list of `types`:

```yaml
rules:
  - severity: low
    types: [max_length]
    max_chars: 4000
    action: log_only
  - severity: medium
    types: [pii_detection]
    categories: [email, phone]
    action: redact
  - severity: high
    types: [pii_detection]
    categories: [credit_card]
    action: block
    fallback: "That reply contained payment details, so I've held it back. Please ask me again."
```

**Built-in checks and filters.** Without a policy, `ValidateInput` can run the `checks` `prompt_injection` and
`pii_detection`, which block, and `FilterOutput` can apply the `filters` `pii_redaction`, which redacts. Unknown
names fail with `INVALID_ARGUMENT`. Their triggered rules name the policy `builtin`.

## Tool validation rules

A `tool_validation` rule checks the arguments of the tools it names (listing 6.22). `tool` (or `tools`) takes
glob patterns, e.g. `travel.flights.*`:

```yaml
rules:
  - tool: travel.flights.book
    constraints:
      - field: departure_date
        checks:
          - type: required
          - type: future_date
            min_days_ahead: 1
          - type: max_days_ahead
            days: 330
      - field: passengers
        checks:
          - type: range
            min: 1
            max: 9
      - field: traveller_id
        checks:
          - type: matches_session_user
```

| Check                  | Passes if the argument                                       | Fields              |
|------------------------|--------------------------------------------------------------|---------------------|
| `required`             | is present and not empty                                     |                     |
| `in_list`, `not_in_list` | is, or is not, one of the values                           | `values`            |
| `range`                | is a number within the bounds                                | `min`, `max`        |
| `min_length`, `max_length` | is a string of at least, or at most, `value` characters  | `value`             |
| `pattern`              | is a string that matches the regular expression              | `pattern`           |
| `future_date`          | is a date at least `min_days_ahead` days ahead (default 0: not in the past) | `min_days_ahead` |
| `max_days_ahead`       | is a date at most `days` days ahead                          | `days`              |
| `matches_session_user` | equals the context's `user_id`                               |                     |
| `matches_context`      | equals the context's value for `key`                         | `key`               |

Fields can be nested, e.g. `traveller.id`. Checks other than `required` are skipped when the argument is missing.
Dates are `YYYY-MM-DD` or RFC 3339, and days are counted in UTC. Any check can have a `message` that replaces the
default one. `exists_in_system`, `custom`, `business_hours` and `in_list` with a `source` instead of `values` need
data the service doesn't have; they are accepted with a warning and not enforced.

## Behavioral rules

| Type                  | Denies the action if                                               | Fields                      |
|-----------------------|--------------------------------------------------------------------|-----------------------------|
| `blocked_action`      | it matches one of `actions`                                        | `actions`                   |
| `allowed_actions`     | it matches none of `actions`                                       | `actions`                   |
| `required_context`    | one of `fields` is missing from the context                        | `fields`, `actions`         |
| `requires_permission` | the context's `permissions` (comma-separated) lack one of `permissions` | `permissions`, `actions` |
| `max_per_session`     | the session already ran `limit` of the actions                     | `actions`, `limit`          |
| `conflicting_actions` | another of `actions` was allowed earlier in the same turn, or session | `actions`, `scope`: `turn` (default) or `session` |

Except for `allowed_actions`, a rule with `actions` applies only to those actions, and a rule without them applies
to every action. Rules can set a `message`, a `severity` and a `suggested_action` for when they deny.

`max_per_session` and `conflicting_actions` need state that spans requests. They count the actions this service
allowed earlier in the session, from its own evaluation records, so the context must carry `session_id` (and
`turn_id` for the `turn` scope). Without them the action is denied, since the rule can't be checked. Two checks
of the same session that run at the same moment can both pass a limit.

## Human approval gates

A `human_gate` rule makes the actions it names wait for a person's approval (listing 6.25):

```yaml
rules:
  - tool: travel.payments.refund
    condition: "total > 1500"
    approval_role: finance_lead
```

When it applies, `CheckPolicy` denies the action with `suggested_action` `require_approval` and the rule's
`approval_role`. The application, or later the workflow service, queues the action and asks for approval. Queuing,
notifying, `timeout_hours` and `on_timeout` aren't part of the guardrails service yet.

A `condition` compares an argument with a number, a quoted string or a boolean, using `==`, `!=`, `>`, `>=`, `<`
or `<=`. Several comparisons can be joined with `and`, e.g. `cabin == 'first' and corporate == false`.
If the condition needs an argument the call doesn't have, the gate applies: approval is the safe default.

If an action also breaks another rule, the other rule's `suggested_action` comes first, since approval can't fix it.

## Context

The `context` of a request carries what policies evaluate against. These keys have a meaning:

| Key           | Used by                                                             |
|---------------|---------------------------------------------------------------------|
| `user_id`     | `matches_session_user`; recorded with every evaluation              |
| `session_id`  | `max_per_session`, `conflicting_actions`; recorded                  |
| `turn_id`     | `conflicting_actions` with the `turn` scope; recorded               |
| `permissions` | `requires_permission`, comma-separated                              |

## Audit

Every evaluation is recorded in `gogi_guardrail_evaluations`, whatever its outcome (`allowed`, `blocked`,
`modified` or `approval_required`). The record holds the inspection point, the policies applied, the triggered
rules, the context and the SHA-256 hash of the content. The content itself is never stored. The hash lets
evaluations of the same content be matched across inspection points. The response's `evaluation_id` identifies
the record.

Applications report violations with `ReportViolation`, giving a `severity` of `low` (default), `medium`, `high`,
`critical` or `blocked`, and optionally the `evaluation_id`. `ListViolations` returns them newest first, filtered
by policy, severity and time, 100 by default and at most 1000.

If the database can't record an evaluation, the error is logged and the response is still returned, without an
`evaluation_id`, so the guardrails don't go down with the audit store.

## Configuration

| Variable                     | Default  | Meaning                                         |
|------------------------------|----------|-------------------------------------------------|
| `GOGI_GUARDRAILS_PORT`       | `:50061` | Port of the gRPC server                         |
| `GOGI_GUARDRAILS_POLICY_DIR` | (none)   | Directory of the YAML policy files to load on start |
| `DATABASE_URL`               | `postgres://gogi:gogi@postgres:5432/gogi` | The database of policies and audit records |
