# Moving from YarGPT

YarGPT is now Handful. Your bots, chats, teams, memory and settings come with you.

- **Mac app, YarGPT 0.9.0 or later:** YarGPT offers the move itself. Click **Move to Handful**.
- **Mac app, YarGPT 0.8.0 or earlier:** download `Handful-universal-mac.zip` from the [latest release](https://github.com/yarlson/handful/releases/latest), open it, and follow its steps. The old app can't update itself to the new name.
- **From source:** pull and run `make build`. The binary is now `bin/handful`; delete the old `bin/yargpt`. `YARGPT_NO_UPDATE_CHECK` is now `HANDFUL_NO_UPDATE_CHECK`.
- **Your data** moves from `~/.yargpt` to `~/.handful` at the first start. `~/.yargpt` stays as a link to it, so saved paths keep working. Leave the link in place: `rm -rf ~/.yargpt/` deletes your data. A copy of the database from before the move is in `~/.handful/backup`.

## Go back to YarGPT

Going back restores your data as it was at the move. What you did in Handful since then stays in `~/.yargpt/handful-after-move.db`.

1. Quit Handful and turn it off in System Settings › General › Login Items & Extensions.
2. Run `/Applications/Handful.app/Contents/Resources/handful -move-back`, read what you'd lose, then run it again with `-yes`.
3. Download `YarGPT-universal-mac.zip` from the [v0.13.0 release](https://github.com/yarlson/handful/releases/tag/v0.13.0) and open it.
4. Click **Later** in its "YarGPT is now Handful" window, and turn off **Check for updates automatically** in its Settings.

From source, run `bin/handful -move-back -yes`, then build the `v0.13.0` tag. Never delete `~/.yargpt` by hand to go back or to tidy up: while it's a link, `rm -rf ~/.yargpt/` deletes the real data in `~/.handful`.
