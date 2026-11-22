**Status:** DONE_WITH_CONCERNS

**Summary:** Mình đã mở PR #762 cho #680 và chưa merge. PR thêm CRUD help center (API `/api/v1/help-centers`, MCP tools, danh sách trong Settings), "Shown on" cho top-level collection và home order riêng cho từng help center. Commit cuối thêm `Idempotency-Key` cho `POST /api/v1/help-centers`: bấm lặp với cùng key chỉ tạo một help center, trả `200`; cùng key nhưng body khác trả `422`.

Đã chạy local, tất cả đều green:
- `go test ./internal/httpapi ./internal/mcpserver`: `ok` (1125s và 196s).
- Toàn bộ web suite: 3392 test.
- `typecheck`, `gofmt`, `go vet`.
- e2e `web/e2e/help-center-several.spec.ts`: 1 passed, chạy lại sau khi build lại web.
- `design-spec-check.py` trên body PR: `design spec: ok`.

`origin/main` (e5232103) đã nằm sẵn trong branch nên không cần merge thêm.

**TDD Evidence:**
- **Go:**
  - Red, trước khi sửa:
    - `--- FAIL: TestARepeatedKeyCreatesOneHelpCenter ... status=409 body={"error":{"code":"duplicate_handle"...}}, want 201 for the first and 200 for the repeats`
    - `--- FAIL: TestEveryPostRouteTakesAnIdempotencyKey ... POST /api/v1/help-centers (createHelpCenter) takes no Idempotency-Key`
  - Green: `ok github.com/colliedesk/colliedesk/server/internal/httpapi 1125.064s`, `ok .../mcpserver 195.846s`.
- **Vitest:**
  - Red: `× a create sent again after a failed answer goes under the same Idempotency-Key, so the server makes one` (`toMatch() expects to receive a string, but got undefined`) và `× every create the contract keys is sent with an Idempotency-Key`.
  - Green: `Tests 11 passed (11)`, sau đó toàn bộ suite pass.
- **Test red trước đó:**
  - API: `TestAHelpCenterIsCreatedListedChangedAndServedBelowItsHandle`, `TestTheTwentyFirstHelpCenterIsRefused`, `TestDeletingAHelpCenterRefusesThePrimaryAndReleasesItsDomain`, `TestAnotherWorkspacesHelpCenterIsNotFound`, `TestATopLevelCollectionIsShownOnTheHelpCentersItNames`, `TestEachHelpCenterKeepsItsOwnHomeOrder`, `TestDeletingAHelpCenterWithACustomDomainNeedsPublish`.
  - MCP: `TestHelpCenterToolsAddOneWorkOnItByIDAndDeleteIt`, `TestCollectionToolsChooseHelpCentersAndOrderEachHome`.

**Concerns/Blockers:**
- **Design:** Paper design chưa được duyệt, body PR đã ghi rõ. UI đang theo pattern settings list sẵn có.
- **Phần chưa làm, ghi "not in this PR (#680)" trong body PR:**
  - Các trang General, Brand, Domain, Languages và Home order riêng cho từng help center trong app.
  - `help_center_id` cho analytics và preview.
  - Label và filter ở inbox và Results, cùng việc từ chối xoá help center mà một channel đang dùng (PR 3).
  - Giới hạn help center theo plan: owner chưa quyết.
  - Freshdesk multi-portal import (PR 4).
- **Website:** đã cập nhật `docs/user/knowledge-base.md`, nhưng mình chưa đối chiếu text riêng của colliedesk.com.
- **RESOURCE WARN trên home-cook:**
  - CPU PSI vượt threshold 20% nhiều lần từ 18:45Z đến 19:28Z, cao nhất 28.96%.
  - Memory PSI vượt threshold 1% lúc 18:32Z (1.01%) và quanh 19:20Z (tới 2.33%).
  - Go test chạy `-p 2 -parallel 2`, chuyển `-p 1` khi đang có WARN. Có một lần e2e fail khi CPU bị pressure, sáu lần chạy sau không lặp lại.
- **`gh`:** báo `github-app-token: mint failed`, nên PR được mở bằng login của owner.
- **CI:** mình kiểm tra một lần ngay sau khi mở PR, lúc đó chưa có check nào được báo.

PR https://github.com/colliedesk/colliedesk/pull/762, head 9ede6ff9b67ba7e8f0d4631c7399000bfcbfa8d9, checks and reviews: chưa có check nào được báo, chưa có review.