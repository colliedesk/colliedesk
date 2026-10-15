# Tags

Tags group conversations, for example `billing` or `bug`.

- **Create** a tag from the conversation details pane: type a **New tag name** and select
  **Create tag**. The tag is added to the conversation you are viewing.
- **Add or remove** a tag on a conversation from the same pane. Adding or removing one tag never
  changes the other tags on the conversation.
- **Filter** the inbox by tag with the **Tag** filter.

Renaming and deleting tags is available through the API (`PATCH` and `DELETE /api/v1/tags/{id}`).
Deleting a tag removes it from every conversation in the workspace.
