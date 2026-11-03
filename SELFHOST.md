# Self-hosted deployment branch

`selfhost` preserves the customized backend source used by the local installation.
Its upstream base is v0.7.0, `dbdbb299164347586e2650bb67684a490726a798`.
The baseline customization commit is `f665175`.

## Baseline provenance

All 34 modified and untracked source files were copied byte-for-byte from
`/Volumes/ext/_service/multica/radio-reference/coordinator-prompt-fix-20261024/source`.
Existing specifications, detailed cases and automated tests are included unchanged.
This captures existing behavior; it does not introduce a new behavior change.

The running backend image is
`holas-multica-backend:0.7.0-openviking-handoff-coordinator-20261024`.
Its recorded server binary SHA256 is
`487f4b77ba8a6116e71cbe11deb267d24d7c15141821bc8e29f4fc24e2cab385`.
Historical build and test evidence is in the installation's
`radio-reference/coordinator-prompt-fix-20261024/` directory.
No fresh build or test run has been performed for this branch capture.

The frontend remains a separate deployed baseline: commit
`fbbe1d573aad872de3720098e747033f19614afe`, branch
`rad-45-plugin-credentials`, in
`/Volumes/ext/_service/multica/radio-reference/rad-45-frontend/source`.
Its image is `holas-multica-web@sha256:105309f1593f7ecafbf436d8d7b2e004bd2a5dfd6a1a6b876569f023e5d2b0f8`.
These frontend changes have not been integrated into `selfhost`.

## Remotes and upgrades

`origin` is the official `https://github.com/multica-ai/multica.git` repository.
`yan` is `https://github.com/y-lobau/multica.git`; selfhost pushes target `yan`.
Fetch upstream main, then merge it into a candidate worktree branched from selfhost.
Integrate the frontend baseline as well before replacing the web image.
For each community PR, record its URL, reviewed exact commit and merge commit.
Resolve overlapping behavior using upstream specs and existing local tests.
Run required checks, including backend `make test`, before promoting a candidate.
Build immutable images identified by the candidate commit and record binary hashes.
Rehearse migrations on an isolated private database copy before any schema upgrade.
Back up database, uploads and private configuration before changing the service.
Image-only rollback is insufficient after incompatible schema migrations.

## Transition state

The local branch exists; production still uses the original pinned images.
No production configuration, database, workspace settings or containers were changed.
Upstream main fetched during preparation was `908456c` (114 commits beyond the base).
Publishing selfhost failed because Git HTTPS credentials were unavailable;
GitHub CLI authentication also returned HTTP 401.
The source worktree is `/Volumes/ext/git/multica-selfhost`.
