# macOS app

The Mac app keeps the Handful server running in the background, from login on, with an icon in the menu bar. It runs on Apple silicon and Intel Macs.

1. Download `Handful-universal-mac.zip` from the [latest release](https://github.com/yarlson/handful/releases/latest), unzip it, and move `Handful.app` to `/Applications`.
2. Open the app. If macOS asks, turn on Handful in System Settings › General › Login Items & Extensions.

To build the app yourself, run `make app` and move `desktop/dist/mac-arm64/Handful.app` (`desktop/dist/mac/Handful.app` on an Intel Mac) to `/Applications`. A copy that you build has an ad-hoc signature; macOS opens it because it was not downloaded.

- The server keeps running when you quit the app, and starts at every login. It has no login of its own, so use the app only on a Mac that one person uses: see [Security](../README.md#security).
- The icon in the menu bar has two items. **Check for updates…** opens the app and checks for a new version. **Exit server** stops the server, which then stays stopped until you open the app again or log in again.
- **Handful › Settings…** (⌘,) opens the Settings page. A change that needs a restart says so. **Restart server** restarts the server at once, or, if you choose, waits for the running bots first.
- **Handful › Open Settings File** opens `~/.handful/config.json` in your default editor.
- When the server can't start, the error screen shows its last error and offers **Open config.json**.
- Links in chats open in your default browser.
- The server writes its log to `~/Library/Logs/Handful/server.log`.
- To remove the app, turn off Handful in System Settings › General › Login Items & Extensions, then delete `Handful.app`. Your bots and chats stay in `~/.handful`. The app also leaves `~/Library/Logs/Handful` and `~/Library/Application Support/Handful`; delete them too to remove everything.

## Updates

The app keeps itself up to date.

- Once a day it checks the latest release on GitHub, and downloads a new version in the background. A pill in the window then shows it. **Later** hides the pill until the next launch or for a day; **Skip this version** hides it until a newer version comes out.
- It installs an update only when you ask: **Restart to update** in the pill or in the **Handful** menu, or ⌘Q. Then the app and its server restart, in about half a minute.
- A restart of the server stops the turns that run. So when bots work, the app asks first. **Update when they finish** waits for their running turns, then restarts. After 30 minutes it asks you again, to restart now or to wait longer; it never restarts on its own while bots work.
- **Handful › Check for updates…** and **Check for updates…** in the menu-bar icon check at once.
- To turn off the daily check, put `"updates": {"autoCheck": false}` in `~/.handful/config.json`. **Check for updates…** still works. `drainMinutes`, from 5 to 240, sets how long the app waits for running turns, for example `"updates": {"drainMinutes": 60}`. The app reads these settings at each check, so they need no restart.
- A copy that you build yourself has an ad-hoc signature, so it can't update itself: macOS installs an update only when its signature matches the running app's. It only shows that a new release is out.

### Go back to an older version

1. Put `"updates": {"autoCheck": false}` in `~/.handful/config.json`, so that the app doesn't update itself again. A version without updates doesn't know this setting and doesn't start with it; to go back to such a version, remove `updates` instead.
2. Download `Handful-universal-mac.zip` of the version you want from [Releases](https://github.com/yarlson/handful/releases), and unzip it.
3. Quit Handful with ⌘Q, and replace `/Applications/Handful.app` with the older app.
4. Open it. When bots work, it waits for their running turns before it starts its own server.
