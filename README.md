# upstream-sync (control branch)

This branch only holds the daily upstream sync workflow for this fork.

- Mirror branch: the upstream default branch (`main`). It is kept as a clean,
  fast-forward-only mirror of the parent repository. Never commit to it.
- Personal work: feature branches (for example `fix/...`, `feat/...`), opened as
  pull requests against the upstream repository.
- This branch is the fork's GitHub default branch only because GitHub runs
  `schedule` workflows exclusively from the default branch.
- Schedule: daily at 04:23 UTC, plus manual `workflow_dispatch`.
- Extra mirror branches: set the repository variable `MIRROR_BRANCHES`
  (space-separated), e.g. `main dev`.

## When the sync fails with a workflow-permission error

`GITHUB_TOKEN` cannot push commits that create or modify files under
`.github/workflows/`. When upstream changes its workflows, `merge-upstream`
fails. Fix: create a personal access token with the `workflow` scope
(classic) or Contents + Workflows read/write on this repository
(fine-grained) and save it as the repository secret `UPSTREAM_SYNC_TOKEN`.
The workflow uses it automatically when present.

## Clones

`origin` = this fork, `upstream` = the parent repository. Check out `main`
explicitly after cloning (`git clone -b main ...`).
