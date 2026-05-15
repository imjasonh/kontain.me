# git.kontain.me incremental clones via layered images

## Status: BLOCKED on go-git thin-pack support

The design below is sound, but it depends on `git fetch` correctly ingesting a
delta pack from upstream after we send `have <prev>` lines. **go-git v5.19.0
cannot ingest thin packs**, and v6.0.0-alpha.3 still has `// TODO: Support
thin-pack` comments in the transport. Until that lands (or we work around it),
the cached-packfile arm of the design produces silently-incomplete images.

### Spike findings (2026-05-15)

Reproduced against `https://github.com/google/go-containerregistry`: depth=1
clone of tag `v0.20.0`, then fetch `main` into the same storage. Tested three
configurations of v5.19.0:

| Storage                                | Resulting state    | Missing objects |
| -------------------------------------- | ------------------ | --------------- |
| `filesystem.Storage` (PackfileWriter)  | pack on disk       | 9               |
| Wrapper hiding PackfileWriter          | loose objects      | 9 (same hashes) |
| `memory.Storage`                       | in-memory          | 9 (same hashes) |

Real `git` CLI doing the same operation resolves all of them — confirms upstream
*is* sending those objects (as REF_DELTAs against bases in the v0.20.0 pack);
go-git silently drops them. Matches the ❌ for `thin-pack` in go-git's own
COMPATIBILITY.md (both v5 and v6 alpha 3).

Reproducer lives at `/tmp/gitspike/` during the spike (file:// version,
HTTPS version, wrapped-storage version, memory-storage version).

### Ways forward

1. **Wait on go-git v6.** v6.0.0-alpha.3 still doesn't support thin-pack on
   the transport side. Worth re-checking on each release; cheap to test (port
   the spike).
2. **Drop the server-side win, keep client-side layering.** Always do a fresh
   `--depth=1` clone (today's cost — no thin-pack involved, self-contained
   pack), cache only a tiny `(path → blob-hash)` worktree manifest in GCS, and
   build the image as base + delta layers. Loses the upstream-bandwidth win;
   keeps the bigger client-side win.
3. **Wrap the transport to strip `ThinPack` from request capabilities.**
   Implement a custom `transport.UploadPackSession` that deletes
   `capability.ThinPack` before sending. Upstream sends a fat (self-contained)
   pack go-git ingests fine. ~150 LOC. Restores the server-side win but every
   fetch transfers more than real `git` would.
4. **Bundle the `git` binary, shell out for fetches.** Real `git` does
   `index-pack --fix-thin` natively. Cleanest behavior; adds a non-trivial dep
   to a service that today is pure-Go (ko-built).

## Goal

Make repeated branch pulls faster on both sides:

- **Server**: keep a per-branch packfile cache in GCS so `git fetch` negotiates
  `have <prev>` with upstream and only the delta comes back. ← **blocked, see
  above**
- **Client**: emit multi-layer images so OCI layer dedup means a `docker pull`
  of a new branch tip only downloads the changed-files layer.

The client-side win is the bigger one for typical kontain.me usage (CI/dev
pulling images) and survives even without the server-side win.

## Scope

Applies to **branch pulls only**. Tag pulls and full-SHA pulls keep today's
single-layer shallow-clone path unchanged. The `:latest` default that resolves
to the remote's default branch counts as a branch pull.

Replaces the current single-layer path for branches (no flag).

## Cache key, GCS layout

```
gs://<bucket>/git-cache/<sha256(cloneURL)>/heads/<branch>/HEAD       -> JSON pointer
gs://<bucket>/git-cache/<sha256(cloneURL)>/heads/<branch>/<commit>   -> state bundle (tar)
```

`HEAD` (the per-branch head pointer):
```json
{
  "commit":     "<sha>",
  "depth":      3,
  "layers":     [{"digest":"sha256:...","size":N,"mediaType":"..."}, ...],
  "annotations":{"me.kontain.git.commit": "...", ...}
}
```

`depth` is the chain length (base = 0, first delta = 1, ...). `layers` is the
full chain (base first, top delta last), each entry enough for the next build
to construct a `v1.Layer` reference without re-reading the blob.

State bundle (per-commit, immutable): tar of
- `objects/pack/*.pack`, `*.idx` — every pack referenced by this commit's
  chain. Cumulative so the next fetch can resolve thin deltas.
- `refs/heads/<branch>` — text file with the commit SHA.
- `HEAD` — `ref: refs/heads/<branch>`.

This is enough to hydrate a go-git `filesystem.Storage` and call `Fetch`
against the upstream — **once go-git can ingest the response**.

Under the "drop server-side win" path (#2 above), we don't need a state
bundle at all — just store a (path → blob-hash, mode) JSON manifest under
the same `<commit>` key. Each new build does a fresh shallow clone, downloads
the prior manifest, diffs path lists, and emits the delta layer.

## Per-request flow (branch pull)

1. Resolve ref → `(hash, refName)`. **Unchanged.**
2. Manifest cache check by `(cloneURL, ref, hash)`. **Unchanged** — if the
   exact commit was built before, redirect.
3. Read `heads/<branch>/HEAD`. Branches: try incremental. Missing or
   `depth >= 8`: fall through to fresh.
4. **Incremental path:**
   1. Read state bundle (or manifest, depending on chosen approach) for
      `HEAD.commit`. If missing, treat as no prior state.
   2. Hydrate / fetch — see the "ways forward" section above.
   3. Resolve `commit` (new tip) and its `tree`.
   4. Diff against the prior tree (`object.DiffTree(oldTree, newTree)`): set
      of `(action, oldPath, newPath, oldEntry, newEntry)` changes.
   5. Build the **delta layer** (see below).
   6. Assemble image: `base = remote.Image(baseRef)`; append the existing
      chain layers (constructed as lazy `v1.Layer`s reading from GCS) +
      new delta + the gitconfig layer.
   7. Serve manifest (uploads delta blob, manifest blob; chain blobs already
      exist in GCS so the conditional write is a no-op).
   8. Write new state bundle / manifest under `<new-commit>`. Conditional-
      update `HEAD` with `IfGenerationMatch` to avoid stomping a concurrent
      winner; on conflict, drop our update (image still served).
5. **Fresh path** (no prior state, depth ≥ 8, or incremental failed e.g.
   upstream rejected `have` after a force-push):
   1. Do today's shallow clone + single-layer build.
   2. Write a fresh state bundle / manifest and reset `HEAD` to
      `{commit, depth:0, layers:[<base-digest>]}` (overwrite, no condition).

## Delta-layer construction

Tar contents (all owned by image uid:gid, `epoch` modtime):

- **Worktree changes** under `git/<repo>/`:
  - `Insert`, `Modify`: regular file/symlink/executable entry with new content.
  - `Delete`: a whiteout file `git/<repo>/<dir>/.wh.<basename>` (AUFS-style,
    supported by the OCI image spec).
- **`.git` updates** under `git/<repo>/.git/`:
  - `objects/pack/pack-<new>.pack` + `.idx` — the new pack from the fetch.
    Older packs from prior layers stay visible via OCI overlay; git scans all
    packs.
  - `HEAD` (`ref: refs/heads/<branch>`), `refs/heads/<branch>` (new SHA),
    `index` (rewritten via `writeIndex` for the new tree), `config` (matches
    today's output).

No whiteouts needed inside `.git/objects/pack/` — only adding files.
Index/HEAD/refs are plain overwrites; no whiteouts needed.

Build via the existing `tarball.LayerFromOpener` pattern. Stream straight from
the in-memory storage; never unpack to disk.

## Prior layers as `v1.Layer`s

Each entry in `HEAD.layers` is `{digest, size, mediaType}`. Build a `v1.Layer`
implementation that opens the GCS blob lazily on `Compressed()`. Cheapest:
`tarball.LayerFromOpener` over a GCS object reader, with `tarball.WithMediaType`
and a precomputed `Digest`. Reuse this for all prior layers.

Manifest assembly: `mutate.Append(base, addendums...)` in order (oldest delta
first, new delta last, gitconfig layer last as today).

## Flatten policy

`K = 8`. When `HEAD.depth >= 8` on read, ignore the chain, take the fresh
path, reset `HEAD` to depth 0. Keeps OCI layer counts bounded (base + ≤8
deltas + gitconfig = 10 layers max).

## Code changes

- `cmd/git/main.go`
  - `serveGitManifest`: after manifest-cache miss, branch on `refName.IsBranch()`
    to pick incremental vs. existing fresh path.
  - New `buildIncremental(ctx, cloneURL, repoName, refName, hash, state)`
    returning `(v1.Image, plumbing.Hash, headPointer, err)`.
  - Keep `build` for tags and SHA pulls.
- New file `cmd/git/cache.go` (or add to main.go if it stays small):
  - `type headPointer struct { Commit string; Depth int; Layers []layerRef;
    Annotations map[string]string }`
  - `readHead(ctx, cloneURL, branch) (*headPointer, generation, error)`
  - `writeHead(ctx, cloneURL, branch, hp, ifGen)` + `writeFreshHead(...)`
  - `readStateBundle(ctx, cloneURL, branch, commit) (billy.Filesystem, error)`
  - `writeStateBundle(ctx, cloneURL, branch, commit, dotgit) error`
  - `gcsLayer(ref layerRef) v1.Layer` — thin GCS-backed `v1.Layer`.
- New file `cmd/git/delta.go`:
  - `deltaLayer(ctx, oldTree, newTree, dotgit, prefix, uid, gid, branch,
    newCommit) (v1.Layer, error)` — builds the tar described above.
  - `diffEntries(oldTree, newTree *object.Tree) ([]change, error)` via
    `object.DiffTree`.
- `pkg/serve/serve.go`: add helpers for raw object read/write with generation
  conditions if not already convenient.

## Tests

- Unit-level (`go test ./cmd/git -short` skips network):
  - `deltaLayer` on hand-built trees: insertions, modifications, deletes,
    symlinks, executable bits, nested-dir delete (whiteouts in the right
    place).
  - `headPointer` JSON round-trip; flatten at depth 8.
- Integration (clones a real repo, like the existing `TestGitLayer`):
  - Two-step: build for an older commit, then build for a newer commit,
    assert (a) the resulting image has the expected layer count, (b)
    extracting all layers in order yields the same worktree as a fresh
    shallow clone of the newer commit, (c) `.git` has both packs and
    `git log -1` resolves the new commit.
  - Force-push simulation: prior state is unreachable; assert fall-through
    to fresh path.
  - Depth-8 flatten: synthesize a chain at depth=8; assert next build
    flattens.

Prefer testscript for the end-to-end flow if it fits — it's listed as a
project preference.

## Risks / open questions

- **Thin-pack support in go-git** — the blocker, see top of doc.
- **`object.DiffTree` cost**: O(changed entries) using subtree-skipping;
  cheap. Already used elsewhere in go-git.
- **Whiteout interop**: AUFS whiteouts (`.wh.X`) are the OCI standard.
  containerd, Docker, ko all honor them. Verify our tar format isn't
  producing PAX entries that some runtimes mishandle (today's code uses
  plain ustar — keep that).
- **GCS races on `HEAD`**: use `IfGenerationMatch`. On conflict, we drop
  our update — the manifest still served is correct, only the cache loses
  an entry.
- **State-bundle size growth**: packs accumulate up to K=8 deltas. Per
  branch, GCS holds ≤ K state bundles' worth of packs. Cleanup: TTL the
  per-commit bundles older than the current HEAD's depth window. Not P0.
- **Tags / `:latest` mapping**: `:latest` resolves to the default branch —
  treat that as a branch pull keyed by the branch name (e.g. `main`), not
  by `latest`. Tags and SHA pulls bypass entirely.
