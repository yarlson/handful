# Releasing Handful

> Never change the Developer ID certificate (team ID) used to sign the app without first shipping a bridge release signed with both. Squirrel.Mac rejects an update whose signature doesn't satisfy the running app's designated requirement, and every user would stop getting updates.

## How a release runs

1. Push a tag such as `v1.4.0`, or `v1.4.0-rc.1` for a pre-release. Publishing a release on GitHub pushes its tag too.
2. `.github/workflows/release.yml` runs on a macOS runner, in the job `build`, which gets no write token:
   - `make app-universal-deps` builds the UI and the universal server and installs the desktop packages.
   - `make app-universal-package VERSION=1.4.0` is the only step with the signing secrets. electron-builder signs the app with the Developer ID and the hardened runtime, has Apple notarize it, staples the ticket, and then zips the app. It also writes `handful-mac.yml`, the feed of the channel `handful`, and the zip's blockmap.
   - **Check the app** unzips the zip and checks the signature, Gatekeeper's notarization, the stapled ticket and both architectures.
   - **Prepare the update feed** runs `desktop/scripts/release-feed.js prepare`. It renames the zip to `Handful-universal-mac.zip` and the blockmap to `Handful-universal-mac.zip.blockmap`, and rewrites `handful-mac.yml` so that `path` and each `files[].url` name the renamed zip, with its sha512 and size.
   - **Check the update feed** runs `release-feed.js verify desktop/dist handful=<version>`: it fails when the sha512 or size of the zip to upload differs from `handful-mac.yml`. The app in that exact zip must pass `codesign --verify --deep --strict`, and its `app-update.yml` must name the channel `handful`.
3. The job `release` downloads the three bridge files from the bridge release (see [Bridge files](#bridge-files)), checks both feeds, and uploads all six files to the tag's release in one command. A tag with a hyphen makes a pre-release.

## The six files

| File | For |
|---|---|
| `Handful-universal-mac.zip` | The app. The site links `releases/latest/download/Handful-universal-mac.zip`, so its name never changes. |
| `Handful-universal-mac.zip.blockmap` | Lets the updater download only the changed parts of the zip, once it has an earlier zip. |
| `handful-mac.yml` | The feed. The app reads `https://github.com/yarlson/handful/releases/latest/download/handful-mac.yml` once a day: its `version`, and the zip's name, sha512 and size. |
| `YarGPT-universal-mac.zip`, its blockmap and `latest-mac.yml` | The bridge files: copies of the bridge release's three files, for the apps from before the rename. |

`releases/latest` is the newest release that is not a pre-release, so a pre-release reaches nobody's updater. `build.publish` in `desktop/package.json` sets the channel `handful`, so electron-builder names the feed `handful-mac.yml`, for every version, and the app reads `handful-mac.yml`: without a channel, the feed of a version such as `1.4.0-rc.1` would be `rc-mac.yml`, and that app would look for `rc-mac.yml`. With it, a pre-release app reads `handful-mac.yml` and updates to the next release. Test a change to the release or the updater with a pre-release tag: only a tag tests the Developer ID signature and notarization.

The updater only works in an app signed with a Developer ID. A copy built with `make app` is signed ad-hoc, and Squirrel.Mac refuses an update for it; such a copy only reports that a release is out.

## Staged rollout

A tag push releases to everyone. To give an update to a share of users, run the workflow by hand: **Actions › release › Run workflow**, with

- `tag`: the release, such as `v1.4.0`;
- `staging_percentage`: a whole number from 0 to 100.

The job `stage` downloads the release's `handful-mac.yml`, sets `stagingPercentage`, and uploads it again; the zips, the blockmaps and the bridge's `latest-mac.yml` stay. `release-feed.js stage` refuses any other file. electron-updater gives each install a random id and offers the update only to the ids within the percentage. Raise the percentage in later runs; an empty `staging_percentage` removes it, which releases to everyone.

Only the latest release's `handful-mac.yml` matters, because the app reads the feed through `releases/latest/download/`.

## Bridge files

The app was called YarGPT before, with the bundle id `dev.yarlson.yargpt`. Those apps, v0.9.0 and later, read `releases/latest/download/latest-mac.yml`, through GitHub's redirect from the old repo name, and Squirrel.Mac installs only an update with their bundle id. The bridge release, `v0.13.0`, is the last one under the old name: it offers the move to Handful.

So every Handful release also carries the bridge's three files, `YarGPT-universal-mac.zip`, `YarGPT-universal-mac.zip.blockmap` and `latest-mac.yml`, unchanged. While a Handful release is the latest, the old apps still find the bridge there. Handful's updater reads only `handful-mac.yml`, so it never sees them.

- The job `release` downloads them with `gh release download "$BRIDGE_TAG"`. `BRIDGE_TAG` is set once, in the job's `env`, and stays `v0.13.0` for 12 months after Handful's first release, v0.14.0. Then remove the step, the bridge set from the gate and the files from the upload, in one release.
- The job fails when the bridge release lacks any of the three files.
- **Never upload a Handful zip as `latest-mac.yml`, and never upload Handful's files under the bridge's names.** The old apps would download a zip with another bundle id, and Squirrel.Mac would refuse it with "Couldn't verify the update's signature."; they would never reach the bridge.
- The gate checks both feeds before the upload: `release-feed.js verify <dir> handful=<version> bridge=0.13.0` fails unless `handful-mac.yml` names `Handful-universal-mac.zip` and `latest-mac.yml` names `YarGPT-universal-mac.zip`, each with the sha512 and size of that zip, both blockmaps are there, and each feed has its version.
- One `gh release create` or `gh release upload` carries all six files, so neither feed names a zip that is not there yet.

## Stop a bad release

1. Run the workflow by hand with the release's `tag` and `staging_percentage` `0`. No app downloads the update after that, although an app that downloaded it already can still install it.
2. Fix the problem and release a newer version. An app never installs an older version, so a fix must have a higher version than the bad release.

## The signing certificate

The app's designated requirement names its bundle id and the team ID of its Developer ID certificate. Squirrel.Mac installs an update only when the new app satisfies the running app's requirement. A release signed with another team's certificate fails that check in every installed app, with the error "Couldn't verify the update's signature.", and those users stop getting updates.

Renewing the certificate of the same team keeps the team ID, so it is safe. To change the team, first ship a bridge release signed so that it satisfies both the old and the new requirement, wait until users run it, and only then sign with the new certificate alone.
