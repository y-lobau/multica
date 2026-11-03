# Description-bearing workflow handoff cases

Behavioral reference: [Issue review handoffs](../../../apps/docs/content/docs/issues.mdx#review-handoffs-with-saved-results).

| Case | Expected saved state and dispatch | Automated coverage |
| --- | --- | --- |
| Save result and enter review, existing engineer assignee | Result saved, configured reviewer assigned, one reviewer run, no engineer run | TestDescriptionUpdatePreservesWorkflowHandoff |
| Batch description/status handoff | Same outcome for batch write | TestBatchDescriptionUpdatePreservesWorkflowHandoff |
| Explicit member reviewer with description/status | Member retained, no configured agent run | TestDescriptionHandoffExplicitMemberOverride |
| Retry unchanged status with result | No second reviewer run | TestDescriptionUpdatePreservesWorkflowHandoff |
| Concurrent untouched assignee refresh | Existing nullable-field merge behavior remains | TestRefreshUntouchedNullableIssueParamsKeepsValidatedAssigneePair |

The reviewer sees the saved result without a second client action. Server-crash/outbox recovery is outside this change.

## Non-assignee coordination

Behavioral reference: [Coordination by a non-assignee](../../../apps/docs/content/docs/issues.mdx#coordination-by-a-non-assignee).

| Case | Expected brief or behavior | Coverage |
| --- | --- | --- |
| A coordinator is not the current assignee | Existing task/workspace coordination authority remains valid for status and assignee reconciliation | TestObserverBriefPreservesExistingCoordinationAuthority; live reconciliation scenarios |
| An ordinary agent is mentioned for advice | Mention alone grants no takeover authority; explicit approval and acceptance requirements remain | TestObserverBriefPreservesExistingCoordinationAuthority; live consultation and ambiguous-approval controls |
| Observer views assigned or unassigned work | Brief names the current owner when present and does not supply assignee-only step instructions or completion commands | TestBriefForObserverLeavesTheStepToItsAssignee |
| Human provides only part of the requested input | Coordinator records the supplied answer and retains only the outstanding human request | Live partial-answer scenario |
| Sole human input is supplied | Unfinished work returns to its authorized team owner rather than remaining in the human action queue | Live complete-answer scenario |
| Reviewed artifact changes before final acceptance | Missing technical validation returns to the team; old QA is not acceptance of the new artifact | Live changed-artifact scenario |
