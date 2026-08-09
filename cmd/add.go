package cmd

import (
	"context"
	"strings"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"rbot/brain/command"
	cmdfunc "rbot/cmd/func"
)

// add.go: tambah anggota ke grup (admin). Kalau diblokir privasi, kirim undangan
// ke DM target. Port add.js. whatsmeow UpdateGroupParticipants mengembalikan
// status per-target (Error==0 sukses); AddRequest terisi bila target memblokir
// penambahan langsung → fallback ke pesan GroupInviteMessage.

func init() {
	command.Register(&command.Command{
		Name:        "add",
		Category:    "Grup",
		Alias:       []string{"tambah", "invite"},
		Description: "Tambah anggota ke grup (admin). Kalau diblokir privasi, undangan dikirim ke DM-nya. Contoh: .add 62812xxxx",
		Handler:     addHandler,
	})
}

func addHandler(ctx context.Context, c *command.Ctx) error {
	gate := cmdfunc.EnsureAdminContext(ctx, c)
	if gate.Err != "" {
		_, err := c.Reply(ctx, gate.Err)
		return err
	}

	targets := cmdfunc.ResolvePhoneTargets(c)
	if len(targets) == 0 {
		_, err := c.Reply(ctx, "Tulis nomornya. Contoh: .add 62812xxxxxxx (boleh beberapa, pisah spasi).")
		return err
	}

	// Ambil kode undangan sekali; dipakai sebagai fallback bila add langsung ditolak.
	inviteCode := ""
	if url, err := c.Client.GetGroupInviteLink(ctx, c.Chat(), false); err == nil {
		inviteCode = strings.TrimPrefix(url, whatsmeow.InviteLinkPrefix)
	}

	var added, invited, failed []types.JID
	undang := func(t types.JID) bool {
		return inviteCode != "" && kirimUndangan(ctx, c.Client, t, c.Chat(), inviteCode, gate.Info.Name) == nil
	}

	res, err := c.Client.UpdateGroupParticipants(ctx, c.Chat(), targets, whatsmeow.ParticipantChangeAdd)
	if err != nil {
		// Seluruh panggilan gagal → coba undang semua lewat DM bila ada kode.
		for _, t := range targets {
			if undang(t) {
				invited = append(invited, t)
			} else {
				failed = append(failed, t)
			}
		}
	} else {
		for i := range res {
			p := &res[i]
			target := p.JID
			if p.Error == 0 {
				added = append(added, target)
				continue
			}
			if undang(target) {
				invited = append(invited, target)
				continue
			}
			failed = append(failed, target)
		}
	}

	var lines []string
	var mentions []types.JID
	if len(added) > 0 {
		lines = append(lines, "✅ Ditambahkan: "+cmdfunc.MentionList(added))
		mentions = append(mentions, added...)
	}
	if len(invited) > 0 {
		lines = append(lines, "✉️ Diblokir privasi, undangan dikirim ke DM: "+cmdfunc.MentionList(invited))
		mentions = append(mentions, invited...)
	}
	if len(failed) > 0 {
		lines = append(lines, "❌ Gagal: "+cmdfunc.MentionList(failed))
		mentions = append(mentions, failed...)
	}
	if len(lines) == 0 {
		lines = append(lines, "Tidak ada yang bisa diproses.")
	}
	_, err = c.ReplyMentions(ctx, strings.Join(lines, "\n"), mentions)
	return err
}

// kirimUndangan mengirim pesan GroupInviteMessage ke DM target sebagai fallback
// saat penambahan langsung ditolak privasi. inviteExpiration 0 = ikut default grup.
func kirimUndangan(ctx context.Context, client *whatsmeow.Client, target, grup types.JID, code, subject string) error {
	msg := &waE2E.Message{
		GroupInviteMessage: &waE2E.GroupInviteMessage{
			GroupJID:         proto.String(grup.String()),
			InviteCode:       proto.String(code),
			InviteExpiration: proto.Int64(0),
			GroupName:        proto.String(subject),
			Caption:          proto.String("Kamu diundang gabung grup *" + subject + "*"),
		},
	}
	_, err := client.SendMessage(ctx, target, msg)
	return err
}
