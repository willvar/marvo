# Marvo X components

These Vue 3 components reimplement the component boundaries and interaction patterns used by
Ant Design X for Marvo. They do not bundle React or `@ant-design/x`.

Reference baseline: `ant-design/x` 2.9.0, commit
`b529d8e96d5b35fe81ec68922fedb1ea124c7235` (MIT package metadata).

Official source: https://github.com/ant-design/x

The implementation is intentionally limited to the behavior Marvo uses and is maintained as
local Vue code.

`XConversations` is a controlled view, matching the upstream component boundary. Conversation-keyed
message history, initial-history loading, and request state live in `frontend/src/stores/agent.ts`; that
Pinia layer is Marvo's Vue equivalent of the upstream `useXChat`/chat message store rather than a
React-hook port inside the UI component.
