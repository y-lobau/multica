# Self-hosted deployment branch

`selfhost` is the deployment branch for the local Multica installation.
`origin` points to the official `multica-ai/multica` repository; `yan` points to
`y-lobau/multica`. Push deployment commits to `yan`, not to `origin`.

## Source and local changes

The integrated application source is commit `1612acbb`, based on upstream
`bcdf970c76c2e9f41960381a76a3a6d399fe700f` (version 0.8.0).

| Component | Preserved source |
| --- | --- |
| Backend customizations | Baseline commit `f665175`: 34 files copied byte-for-byte from the saved 0.7.0 production source, including specifications and tests |
| Plugin credentials UI | Original commits `7446056` and `fbbe1d5`, transferred as `f7dacf6` and `67225d3` |
| Upstream integration | Merges `a069015` and `1612acbb`; retains upstream handler-less workflow spans and daemon compatibility |
| Locale and icon integration | `42825a6` and `396aaea`: Traditional Chinese credentials and the upstream icon package |
| Test isolation | `1e259a8`: task-claim cases use fresh per-test runtimes; production heartbeat admission remains unchanged |

Upstream PR #9506 (`6017861`) already includes the description/workflow-assignee
fix. The integrated branch uses upstream's implementation and equivalent tests.
The local observer coordination boundary and OpenViking/RAD5 plugin APIs remain.
No additional community PR has been selected for this deployment.

## Build and deployment

The application images are `holas-multica-backend:selfhost-1612acbb` and
`holas-multica-web:selfhost-1612acbb`. Exact image IDs, source and binary hashes,
check results, migration rehearsal and deployment status are recorded locally in:

`/Volumes/ext/_service/multica/radio-reference/selfhost-main-20261103/`

The web image uses a host-compiled standalone output and Linux musl sharp from
a frozen-lockfile Linux dependency install. The recorded build-only Next config
limits workers and enables webpack memory optimizations. The original tracked
Next config is restored after compilation. The Linux runtime is smoke-tested
without network access, including sharp processing and native binary format.
The standard Docker web compilation exceeds the current Colima VM memory limit.

The service manager is `/Volumes/ext/_service/multica/bin/service`.
The deployment uses a final `compose.branch.yml` override loaded by `bin/compose`.
Do not bypass this wrapper with an assumed default Docker context.
The installation environment, database, uploads and backups remain outside Git.
Consult `deployment-state.json` for whether the switch completed and the exact
pre-upgrade backup directory. Never infer deployment from a branch or image tag.

## Local CLI and daemon

The CLI/daemon runs `0.8.0-selfhost`, application source `1612acbb`.
The active macOS executable is `~/.local/lib/multica/multica`.
Both `~/.local/bin/multica` and the historical
`/Volumes/ext/_service/multica-cli/bin/multica` point to that executable.
The profile LaunchAgent directly executes the internal path. Loading the freshly
replaced external-drive executable stalled in macOS's loader; the internal path
starts successfully. Server data and profiles retain their existing locations.

For a CLI upgrade, build the native executable from the tested selfhost source,
retain the previous binary, wait for zero active tasks and pending terminal
reports, stop the profile daemon, then replace the internal executable atomically.
Verify macOS signing and reload the profile LaunchAgent. Check version, daemon ID,
workspace/runtime IDs and server-side online status after restart. The retained
installation and rollback record is under
`/Volumes/ext/_service/multica-cli/upgrades/selfhost-1612acbb/`.

The native `/Applications/Multica.app` remains version 0.7.0. Server, CLI/daemon
and native desktop application upgrades are separate operations.

## Regular upgrades

1. Fetch `origin +refs/heads/main:refs/remotes/origin/main` explicitly (a release-tag-only refspec can leave `origin/main` stale), then create an isolated candidate branch/worktree from
   `selfhost` and merge `origin/main` there. Preserve merge history.
2. For each selected community PR, record its URL, exact reviewed commit and merge
   commit here. Reconcile local patches when upstream adopts their behavior.
3. Resolve overlaps using upstream specifications and the local regression tests.
   Keep specifications, detailed cases and tests synchronized with behavior.
4. Run type checking, lint, frontend tests and the full backend `make test` using
   the generated isolated worktree database. Keep Go temporary files external;
   use limited concurrency and avoid memory-heavy builds alongside DB tests.
5. Build both images with unique commit tags; record immutable image IDs and
   binary hashes. Smoke-test the frontend runtime before changing production.
6. Restore a private production dump in an isolated rehearsal environment and run
   the exact candidate migrator. Never rehearse against the production database.
7. Fast-forward `selfhost` to the tested candidate and push to `yan`. Check that
   no agent runs are active, then stop backend/frontend gracefully and save a
   consistent database dump, uploads archive and private configuration backup.
8. Update the branch image override and recreate only backend/frontend. Verify
   readiness, source commit, image IDs, local/public login, and preserved GitHub,
   plugin, MCP, workflow and runtime settings. Keep PostgreSQL and host daemon
   lifecycle separate from this application deployment.

## Recovery

Before migrations start, restore the saved Compose wrapper/configuration and
restart the previous images. Once migrations may have run, inspect the recorded
state first; do not assume image-only rollback is sufficient. Preserve new writes
and uploads, restore the matching pre-upgrade dump to a fresh database when needed,
and switch the database and compatible images together. Never remove the
production volume or stop the shared Colima VM to roll back an application build.
