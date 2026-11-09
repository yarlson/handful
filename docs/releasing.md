# Releasing YarGPT

> Never change the Developer ID certificate (team ID) used to sign the app without first shipping a bridge release signed with both. Squirrel.Mac rejects an update whose signature doesn't satisfy the running app's designated requirement, and every user would stop getting updates.

## How a release runs

1. Push a tag such as `v1.4.0`, or `v1.4.0-rc.1` for a pre-release. Publishing a release on GitHub pushes its tag too.
2. `.github/workflows/release.yml` runs on a macOS runner, in the job `build`, which gets no write token:
   - `make app-universal-deps` builds the UI and the universal server and installs the desktop packages.
   - `make app-universal-package VERSION=1.4.0` is the only step with the signing secrets. electron-builder signs the app with the Developer ID and the hardened runtime, has Apple notarize it, staples the ticket, and then zips the app. It also writes `latest-mac.yml` and the zip's blockmap.
   - **Check the app** unzips the zip and checks the signature, Gatekeeper's notarization, the stapled ticket and both architectures.
   - **Prepare the update feed** runs `desktop/scripts/release-feed.js prepare`. It renames the zip to `YarGPT-universal-mac.zip` and the blockmap to `YarGPT-universal-mac.zip.blockmap`, and rewrites `latest-mac.yml` so that `path` and each `files[].url` name the renamed zip, with its sha512 and size.
   - **Check the update feed** is the gate: `release-feed.js verify` fails when the sha512 or size of the zip to upload differs from `latest-mac.yml`, and the app in that exact zip must pass `codesign --verify --deep --strict`.
3. The job `release` uploads the three files to the tag's release in one command. A tag with a hyphen makes a pre-release.

## The three files

| File | For |
|---|---|
| `YarGPT-universal-mac.zip` | The app. yargpt.com links `releases/latest/download/YarGPT-universal-mac.zip`, so its name never changes. |
| `YarGPT-universal-mac.zip.blockmap` | Lets the updater download only the changed parts of the zip, once it has an earlier zip. |
| `latest-mac.yml` | The feed. The app reads `https://github.com/yarlson/yargpt/releases/latest/download/latest-mac.yml` once a day: its `version`, and the zip's name, sha512 and size. |

`releases/latest` is the newest release that is not a pre-release, so a pre-release reaches nobody's updater. `build.publish` in `desktop/package.json` sets the channel `latest`: without it, electron-builder would name the feed of a version such as `1.4.0-rc.1` `rc-mac.yml`, and that app would look for `rc-mac.yml`. With it, a pre-release app reads `latest-mac.yml` and updates to the next release. Test a change to the release or the updater with a pre-release tag: only a tag tests the Developer ID signature and notarization.

The updater only works in an app signed with a Developer ID. A copy built with `make app` is signed ad-hoc, and Squirrel.Mac refuses an update for it; such a copy only reports that a release is out.

## Staged rollout

A tag push releases to everyone. To give an update to a share of users, run the workflow by hand: **Actions › release › Run workflow**, with

- `tag`: the release, such as `v1.4.0`;
- `staging_percentage`: a whole number from 0 to 100.

The job `stage` downloads the release's `latest-mac.yml`, sets `stagingPercentage`, and uploads it again; the zip and the blockmap stay. electron-updater gives each install a random id and offers the update only to the ids within the percentage. Raise the percentage in later runs; an empty `staging_percentage` removes it, which releases to everyone.

Only the latest release's `latest-mac.yml` matters, because the app reads the feed through `releases/latest/download/`.

## Stop a bad release

1. Run the workflow by hand with the release's `tag` and `staging_percentage` `0`. No app downloads the update after that, although an app that downloaded it already can still install it.
2. Fix the problem and release a newer version. An app never installs an older version, so a fix must have a higher version than the bad release.

## The signing certificate

The app's designated requirement names its bundle id and the team ID of its Developer ID certificate. Squirrel.Mac installs an update only when the new app satisfies the running app's requirement. A release signed with another team's certificate fails that check in every installed app, with the error "Couldn't verify the update's signature.", and those users stop getting updates.

Renewing the certificate of the same team keeps the team ID, so it is safe. To change the team, first ship a bridge release signed so that it satisfies both the old and the new requirement, wait until users run it, and only then sign with the new certificate alone.
