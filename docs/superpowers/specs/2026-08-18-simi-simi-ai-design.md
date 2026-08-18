# Design Spec: Simi-Simi Conversational AI & Sticker Auto-Responder

## Overview
The **Simi-Simi AI** feature turns the WhatsApp bot into an engaging, humorous, and highly sarcastic conversational companion when users reply to (quote) any message sent by the bot. It uses Google's Gemini Interactions API with a specific Indonesian netizen / street-slang persona that roasts users, avoids formal robotic responses, and deflects serious/academic questions playfully. In addition, when a user replies to a bot message with a sticker, the bot automatically replies with a random sticker stored in LMDB.

---

## Key Features & Behaviors

### 1. Trigger Conditions
- **Quote-Based Auto-Reply**: Simi-Simi triggers only when an incoming message is a non-command and is quoting/replying to a message originally sent by the bot (`quoted.Participant == botJID` or sent by bot).
- **Scope**: Active in both Group Chats and Direct Messages (DM).
- **Toggle Control**: Can be enabled/disabled per chat via `.simi [on|off|status]`.

### 2. Personality & System Prompt (Gemini Interactions API)
- **Endpoint**: `https://generativelanguage.googleapis.com/v1/interactions`
- **Model**: `gemini-3.5-flash`
- **API Key**: Configurable in `config.json` (`simi.apiKey` with fallback).
- **Persona Rules**:
  - Full sarcasm, witty roasting, and street-slang tone ("lu", "gw", "wkwk", "bjir", "kocak lu", "anjir").
  - Strictly non-formal and non-robotic.
  - Refuses serious factual, academic, coding, or heavy informational questions by playfully roasting the user and telling them to search Google.
  - Short, punchy, conversational replies.

### 3. Sticker Auto-Responder & LMDB Storage
- **Auto-Collection**: Whenever the `.sticker` command is used in a group chat (`c.IsGroup()`), the resulting WebP sticker bytes (or base64) are saved into LMDB (key `simi_stickers`, capped up to 100 stickers with FIFO rotation).
- **Sticker Quoted Reply**: When a user quotes a bot message with a sticker, Simi selects a random sticker from LMDB and sends it as a reply.

### 4. Anti-Spam & Safety Mechanisms
- **Anti-Loop**: Bot never replies to its own messages (`evt.Info.IsFromMe` is ignored).
- **Per-User Cooldown**: 3-5 seconds cooldown per sender to prevent spamming.
- **Group Settings Enforcement**: Group mute/ban checks are respected.

---

## Architecture & File Layout

### 1. Package `brain/simi` (`brain/simi/simi.go`)
- `AskSimi(ctx context.Context, input string) (string, error)`: Calls Gemini Interactions API with persona system prompt.
- `SaveGroupSticker(data []byte) error`: Stores sticker bytes into LMDB list.
- `GetRandomSticker() ([]byte, bool)`: Retrieves random sticker bytes from LMDB list.
- `IsEnabled(chatID string) bool`: Checks whether Simi is active for chat in LMDB.
- `SetEnabled(chatID string, enabled bool) error`: Toggles Simi status in LMDB.
- `CheckCooldown(senderID string) bool`: Thread-safe rate limiter.

### 2. Message Dispatcher Hook (`brain/command/command.go` & `main.go`)
- In `command.Dispatch`, if text is not a command, check if message quotes bot's message.
- If quote matches bot's JID and Simi is enabled for the chat:
  - If sticker message: send random sticker.
  - If text message: call `simi.AskSimi` and reply with generated text.

### 3. Commands (`cmd/simi.go`)
- `cmd/simi.go`: Toggle `.simi on`, `.simi off`, `.simi status`.

---

## Configuration (`config.json`)
```json
"simi": {
  "apiKey": "YOUR_GEMINI_API_KEY",
  "model": "gemini-3.5-flash",
  "enabledByDefault": true
}
```
