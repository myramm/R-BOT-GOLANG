package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"rbot/brain/ai"
	"rbot/brain/command"
	"rbot/brain/config"
)

// ai.go: ngobrol dengan AI + pengaturan mode (obrolan sementara, context reply,
// role, model). Port lib/commands/ai.js (bagian handler). Sesi & engine ada di
// brain/ai. Energi di-charge terpusat oleh dispatcher (ai ∈ heavyCommands).

func init() {
	command.Register(&command.Command{
		Name:        "ai",
		Category:    "AI",
		Alias:       []string{"claude", "tanya", "chatai", "gpt", "aimode"},
		Description: "Ngobrol dengan AI & Pengaturan Mode (Obrolan Sementara, Context Reply, Role, Model)",
		Handler:     aiHandler,
	})
}

// senderIDAI mengambil id kanonik pengirim (nomor tanpa @server), sumber kunci
// sesi AI. Port senderIdOf: participant (grup) atau remoteJid (DM).
func senderIDAI(c *command.Ctx) string {
	return config.BareNumber(c.Sender().String())
}

func aiHandler(ctx context.Context, c *command.Ctx) error {
	mp := config.MainPrefix()
	senderID := senderIDAI(c)
	invokedAs := strings.ToLower(c.InvokedAs)
	input := strings.TrimSpace(c.ArgStr())
	lower := strings.ToLower(input)
	now := time.Now()

	// Snapshot sesi setelah alias-forcing (.claude → model claude). Semua mutasi
	// sesi lewat ai.Update (terkunci); I/O jaringan dilakukan di luar lock.
	var s ai.Session
	ai.Update(senderID, now, func(sess *ai.Session) {
		if invokedAs == "claude" && input != "" &&
			!strings.HasPrefix(lower, "mode") && !strings.HasPrefix(lower, "model") {
			sess.SelectedModel = "claude"
		}
		s = *sess
	})

	// --- PENGATURAN MODE (.ai mode ...) atau alias .aimode ---
	if strings.HasPrefix(lower, "mode") || invokedAs == "aimode" {
		modeArg := strings.TrimSpace(strings.TrimPrefix(lower, "mode"))
		return aiModeHandler(ctx, c, senderID, now, modeArg)
	}

	// --- PILIH MODEL (.ai model ...) ---
	if strings.HasPrefix(lower, "model") {
		modelArg := strings.TrimSpace(input[len("model"):])
		return aiModelHandler(ctx, c, senderID, now, modelArg)
	}

	// --- Bantuan bila tanpa argumen ---
	if input == "" {
		temp, reply := "OFF", "OFF"
		if s.TemporaryChat {
			temp = "ON"
		}
		if s.ContextReply {
			reply = "ON"
		}
		_, err := c.Reply(ctx, "Mau tanya apa ke AI?\n\n"+
			"*Model Aktif:* "+ai.ModelName(s.SelectedModel)+"\n"+
			"*Obrolan Sementara:* "+temp+"\n"+
			"*Context Reply:* "+reply+"\n\n"+
			"*Contoh:* `"+mp+"ai jelaskan teori relativitas`\n"+
			"*Pengaturan Mode:* `"+mp+"ai mode`\n"+
			"*Ganti Model:* `"+mp+"ai model`\n"+
			"*Reset Percakapan:* `"+mp+"ai reset`")
		return err
	}

	// --- Reset percakapan ---
	if lower == "reset" || lower == "clear" || lower == "lupakan" {
		had := ai.Clear(senderID)
		msg := "Belum ada percakapan AI yang perlu dibersihkan."
		if had {
			msg = "Oke, ingatan percakapan AI kamu sudah dibersihkan."
		}
		_, err := c.Reply(ctx, msg)
		return err
	}

	// --- Obrolan AI ---
	return aiChat(ctx, c, senderID, now, input, s)
}

// aiChat menjalankan satu giliran obrolan: susun pesan (system+riwayat+user),
// panggil AI di luar lock, simpan riwayat (kecuali mode sementara), balas +
// reaksi. contextReply menentukan balasan mengutip pesan atau tidak.
func aiChat(ctx context.Context, c *command.Ctx, senderID string, now time.Time, input string, s ai.Session) error {
	c.React(ctx, "⏳")

	systemPrompt := s.CustomRole
	if systemPrompt == "" {
		systemPrompt = ai.DefaultSystemPrompt()
	}
	var history []ai.Message
	if !s.TemporaryChat {
		history = s.Messages
	}
	messages := make([]ai.Message, 0, len(history)+2)
	messages = append(messages, ai.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)
	messages = append(messages, ai.Message{Role: "user", Content: input})

	answer, err := ai.AskAI(ctx, s.SelectedModel, messages)
	if err != nil {
		c.React(ctx, "❌")
		_, _ = c.Reply(ctx, "Gagal menghubungi AI: "+err.Error()+". Coba ulangi beberapa saat lagi.")
		return fmt.Errorf("ai error (%s): %w", s.SelectedModel, err)
	}
	if answer.Content == "" {
		c.React(ctx, "❌")
		_, _ = c.Reply(ctx, "AI tidak memberikan respon. Coba ulangi pertanyaannya.")
		return fmt.Errorf("ai empty response (%s)", s.SelectedModel)
	}

	// Simpan riwayat bila bukan obrolan sementara (potong ke MAX_TURNS terakhir).
	if !s.TemporaryChat {
		ai.Update(senderID, now, func(sess *ai.Session) {
			sess.Messages = append(sess.Messages,
				ai.Message{Role: "user", Content: input},
				ai.Message{Role: "assistant", Content: answer.Content})
			if len(sess.Messages) > 10 {
				sess.Messages = sess.Messages[len(sess.Messages)-10:]
			}
		})
	}

	out := answer.Content
	if s.ShowReasoning && answer.Reasoning != "" {
		think := answer.Reasoning
		if len(think) > ai.MaxReasoningChars {
			think = think[:ai.MaxReasoningChars] + "…"
		}
		var b strings.Builder
		for _, l := range strings.Split(think, "\n") {
			b.WriteString("> " + l + "\n")
		}
		out += "\n\n─────\n_proses berpikir:_\n" + strings.TrimRight(b.String(), "\n")
	}

	text := "🤖 *" + ai.ModelName(s.SelectedModel) + "*\n\n" + out
	var err2 error
	if s.ContextReply {
		_, err2 = c.Reply(ctx, text)
	} else {
		_, err2 = c.SendText(ctx, text)
	}
	c.React(ctx, "✅")
	return err2
}

// aiModelHandler menangani `.ai model [nomor|id]`: tampilkan daftar atau ganti
// model aktif. Port sub-command model.
func aiModelHandler(ctx context.Context, c *command.Ctx, senderID string, now time.Time, modelArg string) error {
	mp := config.MainPrefix()

	if modelArg == "" {
		var cur string
		ai.Update(senderID, now, func(s *ai.Session) { cur = s.SelectedModel })
		var b strings.Builder
		b.WriteString("🤖 *PILIHAN MODEL AI R-BOT*\n\n")
		b.WriteString("*Model Aktif Kamu:* " + ai.ModelName(cur) + "\n\n")
		b.WriteString("*Daftar Model AI Yang Tersedia:*\n")
		for i, m := range ai.Models {
			mark := ""
			if m.ID == cur {
				mark = " ✅"
			}
			b.WriteString(strconv.Itoa(i+1) + ". *" + m.Name + "*" + mark + "\n   _id: " + m.ID + "_\n")
		}
		b.WriteString("\n*Cara Ganti Model:* `" + mp + "ai model <nomor_atau_id>`\n")
		b.WriteString("_Contoh: ketik *" + mp + "ai model 2* atau *" + mp + "ai model claude*_")
		_, err := c.Reply(ctx, b.String())
		return err
	}

	var found *ai.Model
	if num, err := strconv.Atoi(modelArg); err == nil && num >= 1 && num <= len(ai.Models) {
		found = &ai.Models[num-1]
	} else {
		q := strings.ToLower(modelArg)
		for i := range ai.Models {
			if strings.Contains(strings.ToLower(ai.Models[i].ID), q) ||
				strings.Contains(strings.ToLower(ai.Models[i].Name), q) {
				found = &ai.Models[i]
				break
			}
		}
	}
	if found == nil {
		_, err := c.Reply(ctx, "❌ Model \"*"+modelArg+"*\" tidak ditemukan.\n\nKetik *"+mp+"ai model* untuk melihat daftar model AI.")
		return err
	}
	ai.Update(senderID, now, func(s *ai.Session) { s.SelectedModel = found.ID })
	_, err := c.Reply(ctx, "✅ *Model AI Berhasil Diubah*\n\nSekarang percakapan kamu menggunakan: *"+found.Name+"*.")
	return err
}
