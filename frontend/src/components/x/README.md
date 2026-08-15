# Marvo X components

These Vue 3 components adapt the component boundaries and interaction patterns of Ant Design X
for Marvo. They do not bundle React or `@ant-design/x`.

The reference baseline is `ant-design/x` 2.9.0 at commit
`b529d8e96d5b35fe81ec68922fedb1ea124c7235`; its package metadata declares the MIT license.

Official source: https://github.com/ant-design/x

The implementation intentionally covers only the behavior used by Marvo and is maintained as
local Vue code.

`XConversations` is a controlled view that follows the upstream component boundary. Message history
by conversation, initial history loading, and request state live in `frontend/src/stores/agent.ts`.
That Pinia layer is Marvo's Vue counterpart to the upstream `useXChat` and chat message store; React
hooks are not ported into the UI component.
