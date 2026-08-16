# Jadibot Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a WhatsApp multi-session clone bot feature (Jadibot) that allows users to request a pairing code, link their WhatsApp account, run commands via sub-bot sessions, manage sessions, and auto-reconnect sub-bots on main bot restart.

**Architecture:** Create a `brain/jadibot` package managing sub-bot `whatsmeow.Client` instances with separate SQLite databases (`session/jadibot/<phone>.db`). Register `.jadibot`, `.stopjadibot`, and `.listjadibot` commands, and hook sub-bot startup into `main.go`.

**Tech Stack:** Go 1.26+, `go.mau.fi/whatsmeow`, `go.mau.fi/whatsmeow/store/sqlstore`, SQLite (`github.com/mattn/go-sqlite3`).

**Spec:** `docs/superpowers/specs/2026-08-16-jadibot-feature-design.md`

## Global Constraints
- Must enforce `config.C.MaxJadibot` limit.
- Sub-bots process events via `command.Dispatch(ctx, subClient, evt, true)`.
- Store sub-bot databases in `session/jadibot/<phone>.db`.
- Format pairing codes as 8 characters with hyphen (e.g. `ABCD-1234`).

---

### Task 1: Package `brain/jadibot` (Session Manager & Client Handling)

**Files:**
- Create: `brain/jadibot/jadibot.go`
- Create: `brain/jadibot/jadibot_test.go`

**Interfaces:**
- Consumes: `go.mau.fi/whatsmeow`, `rbot/brain/config`, `rbot/brain/command`
- Produces: `jadibot.Init(ctx)`, `jadibot.StartPairing(ctx, phone, senderJID)`, `jadibot.Stop(ctx, phoneOrJID, requestedByJID, isOwner)`, `jadibot.List()`, `jadibot.Count()`

- [ ] **Step 1: Write failing unit test for `jadibot` manager data structures**

```go
package jadibot_test

import (
	"testing"
	"rbot/brain/jadibot"
)

func TestJadibotManagerEmptyList(t *testing.T) {
	if count := jadibot.Count(); count != 0 {
		t.Fatalf("expected count 0, got %d", count)
	}
	list := jadibot.List()
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./brain/jadibot`
Expected: FAIL (package or functions not defined)

- [ ] **Step 3: Implement `brain/jadibot/jadibot.go`**

Implement `SubBotInfo`, `SubBot`, `Manager`, `Init`, `StartPairing`, `Stop`, `List`, `Count`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./brain/jadibot`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add brain/jadibot/
git commit -m "feat(jadibot): implement sub-bot session manager package"
```

---

### Task 2: Jadibot Commands (`.jadibot`, `.stopjadibot`, `.listjadibot`)

**Files:**
- Create: `cmd/jadibot.go`
- Create: `cmd/stopjadibot.go`
- Create: `cmd/listjadibot.go`
- Create: `cmd/jadibot_test.go`

**Interfaces:**
- Consumes: `rbot/brain/command`, `rbot/brain/jadibot`, `rbot/brain/config`
- Produces: Registered commands `jadibot`, `stopjadibot`, `listjadibot`

- [ ] **Step 1: Write unit test for command registration**

```go
package cmd_test

import (
	"testing"
	"rbot/brain/command"
	_ "rbot/cmd"
)

func TestJadibotCommandsRegistered(t *testing.T) {
	for _, name := range []string{"jadibot", "stopjadibot", "listjadibot"} {
		if cmd := command.Resolve(name); cmd == nil {
			t.Fatalf("command %q not registered", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./cmd -run TestJadibotCommandsRegistered`
Expected: FAIL (commands not found)

- [ ] **Step 3: Implement `cmd/jadibot.go`, `cmd/stopjadibot.go`, `cmd/listjadibot.go`**

Implement handlers with full error checking, user permissions, and clear Indonesian UI text formatting.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd -run TestJadibotCommandsRegistered`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/jadibot.go cmd/stopjadibot.go cmd/listjadibot.go cmd/jadibot_test.go
git commit -m "feat(cmd): add .jadibot, .stopjadibot, and .listjadibot commands"
```

---

### Task 3: Integration in `main.go` and Verification Build

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `rbot/brain/jadibot`

- [ ] **Step 1: Integrate `jadibot.Init(ctx)` in `main.go`**

In `main.go`, add `jadibot.Init(ctx)` call after main client setup so saved sessions reconnect on startup.

- [ ] **Step 2: Run all tests and build check**

Run: `go test ./...` and `go build -o rbot main.go`
Expected: PASS and binary compiled successfully.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat(jadibot): integrate sub-bot auto-reconnect on main bot startup"
```
