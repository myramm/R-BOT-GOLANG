// Package identity menyediakan pengenalan pengirim (kandidat ID & cek owner)
// pada layer bawah, supaya paket energy/premium bisa memakainya tanpa
// membentuk import cycle dengan paket command. Port dari isOwner + idsOf.
package identity

import (
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
)

// Candidates mengembalikan daftar nomor-bare unik milik pengirim pesan.
// whatsmeow sudah meresolusi identitas pengirim ke Sender/SenderAlt (LID & PN),
// jadi keduanya cukup untuk mencocokkan owner/premium — setara idsOf di Node.
func Candidates(evt *events.Message) []string {
	if evt == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(j types.JID) {
		if u := config.BareNumber(j.User); u != "" && !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	add(evt.Info.Sender)
	add(evt.Info.SenderAlt)
	return out
}

// SenderPhone mengembalikan nomor telepon (PN/msisdn) pengirim.
// Jika Sender berdomain @lid (contoh: 238182377492614@lid), mengambil dari SenderAlt (@s.whatsapp.net).
func SenderPhone(evt *events.Message) string {
	if evt == nil {
		return ""
	}
	if evt.Info.Sender.Server == "s.whatsapp.net" {
		return evt.Info.Sender.User
	}
	if evt.Info.SenderAlt.Server == "s.whatsapp.net" && !evt.Info.SenderAlt.IsEmpty() {
		return evt.Info.SenderAlt.User
	}
	return evt.Info.Sender.User
}

// IsOwner true bila salah satu kandidat ID pengirim ada di config.Owners atau pesan dikirim dari akun bot sendiri (IsFromMe).
func IsOwner(evt *events.Message) bool {
	if evt != nil && evt.Info.IsFromMe {
		return true
	}
	cands := Candidates(evt)
	if len(cands) == 0 {
		return false
	}
	set := make(map[string]bool, len(cands))
	for _, c := range cands {
		set[c] = true
	}
	for _, o := range config.C.Owners {
		if set[config.BareNumber(o)] {
			return true
		}
	}
	return false
}
