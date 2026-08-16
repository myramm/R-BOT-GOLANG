# Design Spec: Jadibot (WhatsApp Multi-Session Sub-Bot) Feature

## Overview
The Jadibot feature allows users to clone the main WhatsApp bot by pairing their own WhatsApp account using a WhatsApp pairing code. Once paired, the sub-bot will automatically process messages using the bot's command framework (`command.Dispatch`) with `subBot = true`.

## Architecture & Data Flow

### 1. Package `brain/jadibot`
- **Location**: `brain/jadibot/jadibot.go`
- **Responsibilities**:
  - Managing sub-bot client instances in memory (`map[string]*SubBot`).
  - Managing SQLite databases for each sub-bot session in `session/jadibot/<phone>.db`.
  - Handling pairing code requests (`client.PairPhone`).
  - Auto-reconnecting active sub-bots on startup (`Init`).
  - Gracefully stopping and cleaning up logged-out sub-bots.

### 2. SubBot Struct & Session Storage
- Each sub-bot uses a separate SQLite file `session/jadibot/<phone>.db` via whatsmeow's `sqlstore.New`.
- `SubBot` fields:
  - `Client`: `*whatsmeow.Client`
  - `Container`: `*sqlstore.Container`
  - `Phone`: `string`
  - `OwnerJID`: `types.JID`
  - `StartTime`: `time.Time`

### 3. Lifecycle & Event Handling
- Sub-bot event handler forwards `*events.Message` to `command.Dispatch(ctx, subClient, evt, true)`.
- On `*events.LoggedOut` or manual stop, sub-bot client disconnects, store closes, and `session/jadibot/<phone>.db` file is removed.
- On main bot restart, `jadibot.Init(ctx)` scans `session/jadibot/` and reconnects all existing sub-bots.

### 4. Commands
- `.jadibot [nomor]` (`cmd/jadibot.go`): Initiates pairing for sender or specified phone number, returns formatted 8-character pairing code.
- `.stopjadibot [nomor]` (`cmd/stopjadibot.go`): Stops sub-bot session (user can stop own, main owner can stop any).
- `.listjadibot` (`cmd/listjadibot.go`): Lists all active sub-bots with status and uptime.

## Configuration & Limits
- `config.C.MaxJadibot`: Maximum allowed concurrent sub-bots (default: 5 or as set in `config.json`).
