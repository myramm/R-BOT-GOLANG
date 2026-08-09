package cmd

import (
	"go.mau.fi/whatsmeow"

	cmdfunc "rbot/cmd/func"
)

// memberaction.go: daftarkan kick/promote/demote. Logika factory ada di
// cmd/func (cmdfunc.RegisterMemberAction).

func init() {
	cmdfunc.RegisterMemberAction("kick", []string{"tendang"}, "mengeluarkan", whatsmeow.ParticipantChangeRemove,
		"Keluarkan anggota dari grup (admin). Tag/reply/nomor. Contoh: .kick @user")
	cmdfunc.RegisterMemberAction("promote", []string{"up"}, "mengangkat jadi admin", whatsmeow.ParticipantChangePromote,
		"Jadikan anggota admin grup (admin). Tag/reply/nomor. Contoh: .promote @user")
	cmdfunc.RegisterMemberAction("demote", []string{"down"}, "menurunkan dari admin", whatsmeow.ParticipantChangeDemote,
		"Turunkan admin jadi anggota biasa (admin). Tag/reply/nomor. Contoh: .demote @user")
}
