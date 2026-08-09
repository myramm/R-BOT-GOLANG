package cmd

import (
	"context"
	"regexp"
	"strings"

	"rbot/brain/command"
	"rbot/brain/config"
)

// join.go: bot masuk grup lewat link undangan (owner). Port join.js.
// whatsmeow JoinGroupWithLink menerima kode undangan (bukan URL penuh), jadi
// kode di-ekstrak dulu dari argumen. GetGroupInfoFromLink dipakai best-effort
// untuk ambil nama grup buat pesan sukses.

var (
	reInviteLink = regexp.MustCompile(`(?i)chat\.whatsapp\.com/(?:invite/)?([A-Za-z0-9]+)`)
	reInviteTail = regexp.MustCompile(`(?:^|/)([A-Za-z0-9]{15,30})$`)
	reInviteBare = regexp.MustCompile(`^[A-Za-z0-9]{15,30}$`)
)

func init() {
	command.Register(&command.Command{
		Name:        "join",
		Category:    "Owner",
		Alias:       []string{"gabung", "joingc"},
		Description: "Bot masuk grup lewat link undangan (owner). Contoh: .join https://chat.whatsapp.com/xxxx",
		OwnerOnly:   true,
		Handler:     joinHandler,
	})
}

// parseInviteCode mengekstrak kode undangan dari URL/segmen/kode mentah.
func parseInviteCode(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}
	if m := reInviteLink.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if m := reInviteTail.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if reInviteBare.MatchString(s) {
		return s
	}
	return ""
}

func joinHandler(ctx context.Context, c *command.Ctx) error {
	arg := ""
	if len(c.Args) > 0 {
		arg = c.Args[0]
	}
	code := parseInviteCode(arg)
	if code == "" {
		_, err := c.Reply(ctx, "Kasih link undangannya. Contoh:\n"+config.MainPrefix()+"join https://chat.whatsapp.com/XXXXXXXXXXXX")
		return err
	}

	c.React(ctx, "⏳")

	// Nama grup best-effort dari info undangan (sebelum benar-benar join).
	subject := ""
	if info, err := c.Client.GetGroupInfoFromLink(ctx, code); err == nil && info != nil {
		subject = info.Name
	}

	groupJID, err := c.Client.JoinGroupWithLink(ctx, code)
	if err != nil {
		c.React(ctx, "❌")
		_, e := c.Reply(ctx, "Gagal masuk grup: "+err.Error()+"."+joinHint(err.Error()))
		return e
	}

	if subject == "" {
		if info, e := c.Client.GetGroupInfo(ctx, groupJID); e == nil && info != nil {
			subject = info.Name
		}
	}
	if subject == "" {
		subject = groupJID.String()
	}

	c.React(ctx, "✅")
	_, err = c.Reply(ctx, "✅ Berhasil masuk grup *"+subject+"*.")
	return err
}

// joinHint menerjemahkan pesan error jadi saran singkat (port cabang hint join.js).
func joinHint(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "reachout") || strings.Contains(m, "restricted"):
		return " Ini pembatasan sementara dari WhatsApp (bukan admin grup), biasanya karena akun bot masih baru atau kena flag. " +
			"Tunggu beberapa hari sampai dibuka, kurangi aktivitas join/kirim beruntun, atau pakai nomor yang sudah lebih matang."
	case strings.Contains(m, "conflict") || strings.Contains(m, "already"):
		return " Bot mungkin sudah jadi anggota grup ini."
	case strings.Contains(m, "gone") || strings.Contains(m, "not-authorized") || strings.Contains(m, "404"):
		return " Link mungkin sudah tidak berlaku/dicabut."
	case strings.Contains(m, "forbidden") || strings.Contains(m, "403"):
		return " Grup mungkin butuh persetujuan admin untuk bergabung."
	default:
		return ""
	}
}
