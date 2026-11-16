# The Mac app

After this page, you know what the Mac app does in the background, how it updates itself, and how to go back to an older version.

The Mac app keeps the Handful server running from login on, with an icon in the menu bar. It runs on Apple silicon and Intel Macs. To install it, see [First steps](/docs/first-steps/).

## What runs where

- The server keeps running when you quit the app, and starts at every login. Links in chats open in your default browser.
- If macOS asks, turn on Handful in System Settings › General › Login Items & Extensions. Until then, the server does not start.
- The icon in the menu bar has two items. **Check for updates…** opens the app and checks for a new version. **Exit server** stops the server. It stays stopped until you open the app again or log in again.
- The server has no login of its own. Use the app only on a Mac that one person uses. See [Security and data](/security/).
- When a phone asks to pair and the window is not in front, the Dock icon shows a badge and bounces once.

## The Handful menu

- **Settings…** (⌘,) opens the Settings page.
- **Open Settings File** opens `~/.handful/config.json` in your default editor.
- The update item: **Check for updates…**, or the state of an update, for example "Restart to Update 0.22.1…".

## Restart the server

Some settings apply only after a restart. Then the Settings page shows a banner, for example "1 change applies after a restart: Turns at once", with **Restart server…**.

When no bot works, the server restarts at once. When bots work, the app asks first: **Restart now** stops their turns, and **Restart when they finish** waits for them. New turns wait meanwhile. After the time in **Wait for running bots up to**, 30 minutes by default, it asks you again.

## When the server does not start

The app shows "The Handful server did not start" with the server's last error, for example an invalid `config.json`. The buttons **Open config.json** and **Open Log** open the file to fix.

The server writes its log to `~/Library/Logs/Handful/server.log`. In **Settings**, the section **Runtime** has **Reveal in Finder** for it.

## Updates

The app keeps itself up to date.

- Once a day it checks the latest release on GitHub and downloads a new version in the background. A pill then shows the version, with the link "What's new in <version>". In a chat, the pill sits at the right, above the message box. In other views, it sits at the bottom right of the window.
- It installs an update only when you ask: **Restart now** in the pill, the update item in the **Handful** menu, or ⌘Q. Then the app and its server restart.
- A restart stops the turns that run. So when bots work, the pill lists them and offers **Update when they finish**. While it waits, new turns wait, and teams pause their tasks for the update. After the same wait time, it asks you again: wait longer, or **Restart now**. It never restarts on its own while bots work.
- **Later** hides the pill until the next launch or for a day. **Skip this version** hides it until a newer version comes out.
- **Check for updates…**, in the **Handful** menu or the menu-bar icon, checks at once. So does **Check for updates** in the **Updates** section of **Settings**. A check that you start ends **Later**, and shows a skipped version again.

In the **Updates** section of **Settings**, you can also set:

| Setting | In `config.json` | Default |
|---|---|---|
| **Check for updates automatically** | `updates.autoCheck` | on |
| **Wait for running bots up to** | `updates.drainMinutes`, 5 to 240 | 30 minutes |

These apply without a restart.

A copy that you build yourself with `make app` has an ad-hoc signature, so it cannot update itself: the app's updater, Squirrel.Mac, installs an update only when its signature matches the running app's. When a new release is out, the pill says so and offers **Open release page ↗**.

## Go back to an older version

1. Turn off **Check for updates automatically**, so that the app does not update itself again. An older version does not start with a `config.json` key that it does not know, so remove the keys that the older version lacks.
2. Download `Handful-universal-mac.zip` of the version you want from [Releases](https://github.com/yarlson/handful/releases), and unzip it.
3. Quit Handful with ⌘Q, and replace `/Applications/Handful.app` with the older app.
4. Open it. When bots work, it waits for their running turns before it starts its own server. A dialog lists the running turns, with **Start now**, which stops them.

## Remove the app

1. Turn off Handful in System Settings › General › Login Items & Extensions.
2. Delete `Handful.app`.

Your bots and chats stay in `~/.handful`. The app also leaves `~/Library/Logs/Handful` and `~/Library/Application Support/Handful`. Delete them too to remove everything.
