# First run

You need a desktop the Omnoda build supports. You do not need Codex, Claude Code, Node.js, Docker, or Redis.

1. Install the `omnoda` binary you built or the signed release for your OS. A release that has not been installed on that OS is not a verified install.
2. Run `omnoda seed`. It listens on `127.0.0.1` only.
3. Open the printed URL. Create the administrator in the page. There is no default password.
4. Leave the model empty if you do not have one. Omnoda will say the model is not configured. It will still run the built-in system-info task.
5. If you have an OpenAI-compatible endpoint, enter the base URL, model name, and key in Settings. The key is sealed on disk and is not shown again.
6. On the home page, type a goal such as "report this computer". The same task id remains after you close the tab. Reopen the page to see the status.

## Advanced

Nodes, workflows, and harness selection stay on the Advanced panel and the Nodes page. Pairing still uses a one-time enrollment code. The node dials out. You do not open an inbound port on the device.

Voice input is not in this build. Text on the home page is the supported way to start a task.

A phone on the same LAN cannot open `127.0.0.1`. Remote use needs an HTTPS reverse proxy you configure, with the session cookie on that origin. Omnoda does not ship a public relay.

Advanced pages list nodes, workflows, and harnesses. Those choices are not required for the first built-in task.

`omnoda chat` is not the supported session entry. Use the web home page.

A phone alone cannot run desktop code in this version. Pair a computer you own, or wait for a hosted runner that this release does not include.
