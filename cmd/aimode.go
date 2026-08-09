package cmd

import (
	"context"
	"strings"
	"time"

	"rbot/brain/ai"
	"rbot/brain/command"
	"rbot/brain/config"
)

// aimode.go: sub-command `.ai mode ...` (obrolan sementara, context reply,
// thinking, role). Dipisah dari ai.go agar tiap file fokus. Port bagian "mode" di
// ai.js. Semua balasan mode di-quote (forceQuote true di Node) → pakai c.Reply.

func aiModeHandler(ctx context.Context, c *command.Ctx, senderID string, now time.Time, modeArg string) error {
	mp := config.MainPrefix()

	// Tanpa argumen → tampilkan status + daftar perintah mode.
	if modeArg == "" {
		var s ai.Session
		ai.Update(senderID, now, func(sess *ai.Session) { s = *sess })
		onoff := func(b bool, on, off string) string {
			if b {
				return on
			}
			return off
		}
		role := s.CustomRoleName
		if role == "" {
			role = "R-BOT Default"
		}
		text := "⚙️ *PENGATURAN & STATUS MODE AI R-BOT*\n\n" +
			"*Status Mode Saat Ini:*\n" +
			"• 🧠 *Model AI:* " + ai.ModelName(s.SelectedModel) + "\n" +
			"• 🕶️ *Obrolan Sementara (Incognito):* " + onoff(s.TemporaryChat, "ON (Tanpa Simpan Riwayat)", "OFF (Riwayat Disimpan)") + "\n" +
			"• 💬 *Context Reply (Quote Balasan):* " + onoff(s.ContextReply, "ON (Membalas Pesan)", "OFF (Pesan Tanpa Quote)") + "\n" +
			"• 💡 *Proses Berpikir (Thinking):* " + onoff(s.ShowReasoning, "ON", "OFF") + "\n" +
			"• 🎭 *Peran (Role):* " + role + "\n\n" +
			"*Daftar Perintah Pengaturan Mode:*\n" +
			"1. 🕶️ *Obrolan Sementara (Incognito):*\n   `" + mp + "ai mode temp on` (atau `temp off`)\n" +
			"2. 💬 *Context Reply Bot:*\n   `" + mp + "ai mode reply on` (atau `reply off`)\n" +
			"3. 💡 *Proses Berpikir (Thinking):*\n   `" + mp + "ai mode think on` (atau `think off`)\n" +
			"4. 🎭 *Peran Karakter (Role):*\n   `" + mp + "ai mode role programmer` / `translator` / `anime` / `formal` / `santai` / `reset`\n" +
			"5. 🤖 *Pilih Model AI:*\n   `" + mp + "ai model` (atau `" + mp + "ai model 2`)"
		_, err := c.Reply(ctx, text)
		return err
	}

	// Obrolan Sementara / Incognito.
	if hasPrefixAny(modeArg, "temp", "temporary", "obrolan sementara", "incognito") {
		if strings.Contains(modeArg, "on") {
			ai.Update(senderID, now, func(s *ai.Session) { s.TemporaryChat = true })
			_, err := c.Reply(ctx, "🕶️ *Mode Obrolan Sementara Diaktifkan.*\nAI tidak akan menyimpan riwayat percakapan. Setiap pertanyaan dianggap obrolan baru.")
			return err
		}
		if strings.Contains(modeArg, "off") {
			ai.Update(senderID, now, func(s *ai.Session) { s.TemporaryChat = false })
			_, err := c.Reply(ctx, "💬 *Mode Obrolan Sementara Dimatikan.*\nAI akan mengingat konteks riwayat percakapan sebelumnya.")
			return err
		}
		var st bool
		ai.Update(senderID, now, func(s *ai.Session) { st = s.TemporaryChat })
		_, err := c.Reply(ctx, "Status Obrolan Sementara: *"+boolOnOff(st)+"*.\n\nKetik:\n"+mp+"ai mode temp on (aktifkan)\n"+mp+"ai mode temp off (matikan)")
		return err
	}

	// Context Reply.
	if hasPrefixAny(modeArg, "reply", "context reply", "quote") {
		if strings.Contains(modeArg, "on") {
			ai.Update(senderID, now, func(s *ai.Session) { s.ContextReply = true })
			_, err := c.Reply(ctx, "💬 *Mode Context Reply Diaktifkan.*\nBot akan mengutip (quote reply) pesan kamu saat mengirimkan jawaban AI.")
			return err
		}
		if strings.Contains(modeArg, "off") {
			ai.Update(senderID, now, func(s *ai.Session) { s.ContextReply = false })
			_, err := c.Reply(ctx, "💬 *Mode Context Reply Dimatikan.*\nBot akan mengirimkan jawaban AI tanpa mengutip pesan kamu.")
			return err
		}
		var st bool
		ai.Update(senderID, now, func(s *ai.Session) { st = s.ContextReply })
		_, err := c.Reply(ctx, "Status Context Reply: *"+boolOnOff(st)+"*.\n\nKetik:\n"+mp+"ai mode reply on (aktifkan)\n"+mp+"ai mode reply off (matikan)")
		return err
	}

	// Proses Berpikir / Thinking.
	if hasPrefixAny(modeArg, "think", "thinking", "berpikir") {
		if strings.Contains(modeArg, "on") {
			ai.Update(senderID, now, func(s *ai.Session) { s.ShowReasoning = true })
			_, err := c.Reply(ctx, "💡 *Mode Proses Berpikir Diaktifkan.*\nAlur pemikiran AI akan ditampilkan di bawah balasan.")
			return err
		}
		if strings.Contains(modeArg, "off") {
			ai.Update(senderID, now, func(s *ai.Session) { s.ShowReasoning = false })
			_, err := c.Reply(ctx, "💡 *Mode Proses Berpikir Dimatikan.*\nProses berpikir AI disembunyikan.")
			return err
		}
		var st bool
		ai.Update(senderID, now, func(s *ai.Session) { st = s.ShowReasoning })
		_, err := c.Reply(ctx, "Status Proses Berpikir: *"+boolOnOff(st)+"*.\n\nKetik:\n"+mp+"ai mode think on (aktifkan)\n"+mp+"ai mode think off (matikan)")
		return err
	}

	// Peran / Role.
	if hasPrefixAny(modeArg, "role", "peran", "karakter") {
		roleKey := modeArg
		for _, p := range []string{"role", "peran", "karakter"} {
			if strings.HasPrefix(roleKey, p) {
				roleKey = strings.TrimSpace(roleKey[len(p):])
				break
			}
		}
		roleKey = strings.ToLower(strings.TrimSpace(roleKey))

		if roleKey == "" || roleKey == "list" {
			var b strings.Builder
			b.WriteString("🎭 *DAFTAR PERAN KARAKTER AI (ROLE)*\n\n")
			for _, r := range ai.RoleOrder {
				b.WriteString("• *" + r + "* ➔ " + ai.PresetRoles[r] + "\n\n")
			}
			b.WriteString("*Cara Pakai:* `" + mp + "ai mode role <nama_role>`\n")
			b.WriteString("_Contoh: *" + mp + "ai mode role programmer* atau *" + mp + "ai mode role reset*_")
			_, err := c.Reply(ctx, b.String())
			return err
		}

		if roleKey == "reset" || roleKey == "normal" || roleKey == "default" {
			ai.Update(senderID, now, func(s *ai.Session) { s.CustomRole = ""; s.CustomRoleName = "" })
			_, err := c.Reply(ctx, "🎭 *Peran AI dikembalikan ke R-BOT Default.*")
			return err
		}

		if preset, ok := ai.PresetRoles[roleKey]; ok {
			name := strings.ToUpper(roleKey)
			ai.Update(senderID, now, func(s *ai.Session) { s.CustomRole = preset; s.CustomRoleName = name })
			_, err := c.Reply(ctx, "🎭 *Peran AI berhasil diubah menjadi: "+name+"*\n\n_\""+preset+"\"_")
			return err
		}

		// Prompt kustom.
		ai.Update(senderID, now, func(s *ai.Session) { s.CustomRole = roleKey; s.CustomRoleName = "Custom Role" })
		_, err := c.Reply(ctx, "🎭 *Peran AI kustom berhasil diatur:* \n_\""+roleKey+"\"_")
		return err
	}

	_, err := c.Reply(ctx, "⚠️ Mode \"*"+modeArg+"*\" tidak dikenali.\n\nKetik *"+mp+"ai mode* untuk melihat daftar opsi mode AI.")
	return err
}

// hasPrefixAny true bila s diawali salah satu prefix.
func hasPrefixAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// boolOnOff memformat bool jadi "ON"/"OFF".
func boolOnOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}
