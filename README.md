# Stranglehold PC server emulator

Restores online multiplayer for the latest GOG release of Stranglehold.
Based on pjclas's [Ghostbusters PS3 Server](https://github.com/pjclas/ghostbusters-ps3-server).

## Run

Copy `.env.example` to `.env`, fill in the Stranglehold values, then:

```powershell
Copy-Item .env.example .env
go run ./cmd/stranglehold-server
```

You'll have to acquire the access key and rc4 key through disassembling the EXE.

The server automatically loads `.env` from its working directory. Existing
process environment variables take precedence.

Command-line flags override the documented defaults:

```text
-bind
-public-host
-auth-port
-secure-port
-nat-port
-db
-accounts-file
-access-key
-rc4-key
-keep-old-matches
```

The server listens on UDP ports `30670`, `30671`, and `30672` by default.
Accounts, career stats, and leaderboards are stored in SQLite.

## Preload accounts

The server automatically imports `accounts.json` from its working directory
when it starts:

```json
[
  { "username": "alice", "password": "alice-secret" },
  { "username": "bob", "password": "bob-secret" }
]
```

Accounts already present in SQLite are left unchanged. New preloaded accounts
use the specified plain-text password. The `password` field is optional; when
omitted, the server's default login password is used until the client supplies
its account key through the normal account lookup flow.

Use `-accounts-file` or `STRANGLEHOLD_ACCOUNTS_FILE` only when the file has a
different name or location.
