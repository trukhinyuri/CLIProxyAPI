# vibeflow: Muse streaming tool arguments

This branch maintains a small compatibility patch for Muse Spark responses consumed
by Codex. The fork is [trukhinyuri/vibeflow](https://github.com/trukhinyuri/vibeflow);
the upstream project is [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI).

## Base and scope

The initial patch is based on upstream `main` at
`d198db54d4c4886c99b21488d54fc576933019a3`, checked on 2026-09-08. That commit
includes the latest release at that time, `v7.2.154`. The fork's `main` tracks
upstream; `fix/muse-streaming-tool-arguments` contains the maintained patch.

Muse can emit an empty `response.output_item.done` argument string after supplying
the arguments in earlier events. The patch retains arguments by item ID, restores
them when the final event omits them, and removes the saved item after completion.
An explicit nonempty final argument string takes precedence.

Completed Muse arguments also normalize exact integral JSON numbers, for example
`120000.0` to `120000`, so Codex integer fields can decode them. Strings, fractional
values, and large integer precision are preserved. Normalization is limited to
models whose upstream name starts with `muse-spark-`; other models pass through.
Aliases should map to a Muse upstream name. Both raw JSON events and SSE `data:`
events use the same conversion path.

Number normalization uses decimal strings instead of arbitrary-precision
arithmetic. Exponent magnitude and generated digit count are limited to 4,096;
larger numbers retain their original representation. A shared 64 KiB growth
budget applies to each completed argument string. If normalization would exceed
that budget, the entire original argument string is preserved. Limits are checked
before adding zeroes, preventing compact scientific notation from amplifying
memory use without bound.

The code change is confined to three files under
`internal/translator/codex/openai/responses/`: the response converter,
`muse_arguments.go`, and its tests. This branch does not contain runtime account
files or credentials. Provider configuration and authorization remain local.

## Build and verify

The canonical checkout on this computer is `~/Personal/Sources/vibeflow`.
To create the same checkout on another computer:

```sh
git clone --branch fix/muse-streaming-tool-arguments https://github.com/trukhinyuri/vibeflow.git ~/Personal/Sources/vibeflow
cd ~/Personal/Sources/vibeflow
git remote add upstream https://github.com/router-for-me/CLIProxyAPI.git
```

Use the Go version required by `go.mod` or a newer supported version, then run:

```sh
go test ./internal/translator/codex/openai/responses
go test -race ./internal/translator/codex/openai/responses
go build -o bin/cli-proxy-api ./cmd/server
go test ./...
```

`fork-muse-checks` runs the translator tests with the race detector and builds the
server on Linux and macOS for each push to the maintained branch. These checks
need no provider credentials. Tests cover accumulated deltas, missing final
arguments, interleaved calls, explicit final values, state cleanup, number
precision, invalid payloads, and non-Muse responses.
Large positive and negative exponents and cumulative number expansion have
regression tests as well.

## Known limits

This patch restores arguments only when the stream actually contains them. It
cannot recover a required field that Muse never generated. Empty arguments with
no saved value are normalized to `{}`; tools with required fields can still reject
that call. A prior live Codex delegation test failed on missing required fields,
so full Muse-to-subagent delegation in Codex is not certified by these unit tests.
The non-streaming response converter is outside the patch's scope.

Provider authorization, subscription eligibility, rate limits, and transient
provider errors are separate from this stream repair. No paid API fallback is
introduced by this patch.

## Updating and contributing upstream

Keep an `upstream` remote pointing to `router-for-me/CLIProxyAPI` and `origin`
pointing to this fork. With a clean worktree:

```sh
git fetch upstream main --tags
git switch fix/muse-streaming-tool-arguments
git merge upstream/main
```

Resolve any conflicts, rerun the verification commands, and push the maintained
branch without force. Update the fork's `main` only by a fast-forward after
checking for fork-specific commits. Keep credentials and generated binaries out
of commits.

The implementation and regression tests are kept in separate commits from this
fork guide and its CI workflow. After live compatibility is established, that
patch series can form a focused upstream contribution with the observed event sequence
and test evidence. No upstream pull request is opened by this setup.
