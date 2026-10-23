# Releasing

For maintainers. How a tag becomes a release is described in
[CONTRIBUTING.md](../CONTRIBUTING.md#releasing-maintainers); this page is the
order of steps. The order matters because the README and `scripts/install.sh`
name release versions and archive URLs: a version that is not published yet is
a 404 for anyone who copies the example.

## Order: merge, tag, bump

1. **Prepare in a PR, and merge it.** Add the new section to
   [CHANGELOG.md](../CHANGELOG.md) with its date. Do not change the README
   install examples yet: they must keep pointing at a release that exists.
   Wait for CI (including the `lint` job) and for a review from someone other
   than the author. Nobody merges their own PR.
2. **Tag the merge commit.** From an up-to-date `master`:

   ```
   git tag vX.Y.Z <merge-commit>
   git push origin vX.Y.Z
   ```

   Pushing the tag starts the `release` workflow: tests, then GoReleaser, which
   publishes the archives and `checksums.txt`. A tag with a suffix such as
   `vX.Y.Z-rc.1` is published as a pre-release. Tags are never moved or
   reused, so check the merge commit and the changelog date before you push.
3. **Check the release.** Wait for the workflow to finish, then run
   `SITEWARM_VERSION=X.Y.Z sh scripts/install.sh` and confirm that
   `sitewarm --version` prints `X.Y.Z`.
4. **Bump the README in a second PR.** Change `VERSION=` in the install section
   and `SITEWARM_VERSION:` in the CI example to the new version, and merge it.

Until step 3 passes, `scripts/install.sh` without `SITEWARM_VERSION` installs
the previous stable release, or stops with "no stable release yet" when there
is none.

## Rules

- The release notes come from the commits (GoReleaser skips `Bump ` commits);
  the changelog is written by hand and is the place for user-facing wording.
- Release candidates (`-rc.N`) follow the same steps; they never update the
  "latest stable" lookup in `scripts/install.sh`.
- This repository is public. Keep plans, prices, internal decisions and
  customer data out of changelogs, release notes and tag messages;
  `scripts/check_boundary.sh` checks the tracked files in CI.
