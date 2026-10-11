# Session service

In this section we will go over the session service. The session service is responsible
for providing conversation memory to the AI agents. In other words, using the session service an agent can 
remember what has already been said so that follow-up questions make sense. It also allows an assistant to reference earlier parts of the conversation. The session service transforms a static AI system into a dynamic one in terms of interaction.

Gogi assumes that sessions are structured and relational. That being said, within gogi, a user has conversations, conversations have messages, messages have roles and content. SQL -based databases are very good at handling this sort of structure.

## Scope

Let's try to provide a bit more scope about the session service. 
Let's start by specifying what a session is all about. A session represents a conversation between a user and an AI application. At minimum, it needs to track who the conversation belongs to, when it started, and what's been said.

---
**Remark**

Note that a message in gogi assumes the OpenAI established format i.e.:

```
{"role": "user|system|assistant|tool", "content": "message content"}
```
---

The session service therefore is not just a simple storage solution.
Instead, it handles several concerns:

- Support for concurrent access: A user might have multiple browser tabs open or switch between devices mid-conversation. 
- Integrate with the platform's gRPC infrastructure, exposing operations that the SDK can call through the API gateway
- It manages context windows intelligently: A long conversation might accumulate hundreds of messages, but models have token limits. When preparing the next request, we can't simply pass the entire history if it exceeds what the model can process.

In addition, sessions have to be fast for both read/write operations because every conversation turn involves fetching history and appending new messages. The user asks a question, we retrieve their session, pass the history to the model, get a response, and append both the question and answer before the patient even notices a delay. Hence, sessions deliberately avoid a  complex nested structure that most likely would slow down these hot-path operations. The flat list of messages with lightweight metadata keeps things efficient while capturing everything the AI needs to maintain coherent, context-aware conversations.