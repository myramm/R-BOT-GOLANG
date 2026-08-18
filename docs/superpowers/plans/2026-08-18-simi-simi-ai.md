# Implementation Plan: Simi-Simi AI & Sticker Auto-Responder

## Phase 1: Configuration & LMDB Storage Layer (`brain/config`, `brain/simi`)
1. Update `brain/config/config.go` and `config.example.json` to include `Simi` configuration (`apiKey`, `model`, `systemPrompt`, `enabledByDefault`).
2. Create `brain/simi/simi.go`:
   - Gemini Interactions API caller (`AskSimi`) using `https://generativelanguage.googleapis.com/v1/interactions`.
   - Default persona system prompt (Indonesian netizen slang, sarcastic, roasting, non-robotic).
   - LMDB sticker persistence (`SaveGroupSticker`, `GetRandomSticker`) using store key `simi:stickers`.
   - LMDB chat status toggle (`IsEnabled`, `SetEnabled`).
   - In-memory thread-safe rate limiter (`CheckCooldown`).
3. Add unit tests in `brain/simi/simi_test.go`.

## Phase 2: Sticker Capture Hook & Dispatcher Integration
1. In `cmd/sticker.go`: when a sticker is successfully created in a group chat (`c.IsGroup()`), call `simi.SaveGroupSticker(webp)`.
2. In `brain/command/command.go`:
   - Detect non-command incoming messages quoting the bot.
   - If quote target is the bot and Simi is enabled for the chat & not on cooldown:
     - If text: reply with `simi.AskSimi` response.
     - If sticker: reply with `simi.GetRandomSticker()`.

## Phase 3: Commands
1. Implement `cmd/simi.go` (`.simi on`, `.simi off`, `.simi status`).

## Phase 4: Verification & Git Push
1. Run all unit tests with `go test ./...`.
2. Test building the binary with `go build -o rbot .`.
3. Commit and push changes to remote GitHub repository.
