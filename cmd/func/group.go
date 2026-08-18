// group.go: helper bersama untuk command grup (port lib/group.js): gerbang
// admin, resolusi target anggota, dan pengenalan admin/bot. whatsmeow sudah
// meresolusi identitas ke JID/LID/PhoneNumber per-participant, jadi pencocokan
// cukup lewat nomor-bare.
package cmdfunc

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
	"rbot/brain/config"
)

func init() {
	command.IsGroupAdminHook = func(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool {
		if evt == nil || !evt.Info.IsGroup {
			return false
		}
		c := &command.Ctx{Client: client, Evt: evt}
		info, err := client.GetGroupInfo(ctx, evt.Info.Chat)
		if err != nil {
			return false
		}
		return SenderIsAdmin(c, info)
	}
}

// AdminGate adalah hasil pengecekan konteks admin grup.
type AdminGate struct {
	Info *types.GroupInfo
	Err  string // pesan ramah bila gagal; kosong bila lolos
}

// BareID mengambil bagian angka/identitas sebelum '@' atau ':' dari JID.
func BareID(j types.JID) string {
	if j.IsEmpty() {
		return ""
	}
	return config.BareNumber(j.User)
}

// BotJIDs mengembalikan set nomor-bare milik bot (PN & LID) untuk cek "ini bot".
func BotJIDs(c *command.Ctx) map[string]bool {
	set := map[string]bool{}
	if id := BareID(c.Client.Store.GetJID()); id != "" {
		set[id] = true
	}
	if lid := BareID(c.Client.Store.GetLID()); lid != "" {
		set[lid] = true
	}
	return set
}

// FindParticipant mencari participant yang salah satu id-nya (JID/PhoneNumber/LID)
// cocok dengan nomor-bare mana pun di want.
func FindParticipant(info *types.GroupInfo, want map[string]bool) *types.GroupParticipant {
	if len(want) == 0 {
		return nil
	}
	for i := range info.Participants {
		p := &info.Participants[i]
		for _, id := range []string{BareID(p.JID), BareID(p.PhoneNumber), BareID(p.LID)} {
			if id != "" && want[id] {
				return p
			}
		}
	}
	return nil
}

// SenderIsAdmin true bila pengirim pesan adalah admin/superadmin di grup.
func SenderIsAdmin(c *command.Ctx, info *types.GroupInfo) bool {
	want := map[string]bool{}
	if id := BareID(c.Evt.Info.Sender); id != "" {
		want[id] = true
	}
	if id := BareID(c.Evt.Info.SenderAlt); id != "" {
		want[id] = true
	}
	p := FindParticipant(info, want)
	return p != nil && (p.IsAdmin || p.IsSuperAdmin)
}

// BotIsAdmin true bila bot sendiri admin/superadmin di grup.
func BotIsAdmin(c *command.Ctx, info *types.GroupInfo) bool {
	p := FindParticipant(info, BotJIDs(c))
	return p != nil && (p.IsAdmin || p.IsSuperAdmin)
}

// EnsureAdminContext meniru ensureAdminContext(sock,msg,ctx): pastikan di grup,
// bukan sub-bot, ambil metadata, pengirim admin, dan bot admin.
func EnsureAdminContext(ctx context.Context, c *command.Ctx) AdminGate {
	if !c.IsGroup() {
		return AdminGate{Err: "Perintah ini cuma bisa dipakai di dalam grup."}
	}
	if c.SubBot {
		return AdminGate{Err: "Command admin grup tidak tersedia lewat sub-bot."}
	}
	info, err := c.Client.GetGroupInfo(ctx, c.Chat())
	if err != nil {
		return AdminGate{Err: "Gagal mengambil data grup. Coba lagi sebentar lagi."}
	}
	if !SenderIsAdmin(c, info) {
		return AdminGate{Err: "Khusus admin grup ya. Kamu bukan admin di sini."}
	}
	if !BotIsAdmin(c, info) {
		return AdminGate{Err: "Jadikan bot admin dulu biar bisa mengurus grup."}
	}
	return AdminGate{Info: info}
}

// ResolveMemberTargets mengumpulkan JID target dari mention, reply, dan nomor di
// args, lalu memetakannya ke JID kanonik participant grup (dedup). Port
// resolveMemberTargets group.js.
func ResolveMemberTargets(c *command.Ctx, info *types.GroupInfo) []types.JID {
	var raw []types.JID
	if ci := c.ContextInfo(); ci != nil {
		for _, m := range ci.GetMentionedJID() {
			if j, err := types.ParseJID(m); err == nil {
				raw = append(raw, j)
			}
		}
		if p := ci.GetParticipant(); p != "" {
			if j, err := types.ParseJID(p); err == nil {
				raw = append(raw, j)
			}
		}
	}
	for _, a := range c.Args {
		d := config.Digits(a)
		if len(d) >= 6 {
			raw = append(raw, types.NewJID(d, types.DefaultUserServer))
		}
	}

	seen := map[string]bool{}
	var out []types.JID
	for _, j := range raw {
		want := map[string]bool{}
		if id := BareID(j); id != "" {
			want[id] = true
		}
		target := j
		if p := FindParticipant(info, want); p != nil {
			target = p.JID
		}
		key := target.String()
		if !seen[key] {
			seen[key] = true
			out = append(out, target)
		}
	}
	return out
}

// ResolvePhoneTargets mengumpulkan JID nomor telepon mentah dari mention, reply,
// dan args (dedup by digit), tanpa memetakan ke participant grup. Dipakai command
// yang menarget orang di LUAR grup (mis. add). Port resolvePhoneTargets group.js.
func ResolvePhoneTargets(c *command.Ctx) []types.JID {
	seen := map[string]bool{}
	var out []types.JID
	add := func(raw string) {
		d := config.Digits(config.BareNumber(raw))
		if len(d) < 6 || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, types.NewJID(d, types.DefaultUserServer))
	}
	if ci := c.ContextInfo(); ci != nil {
		for _, m := range ci.GetMentionedJID() {
			add(m)
		}
		if p := ci.GetParticipant(); p != "" {
			add(p)
		}
	}
	for _, a := range c.Args {
		add(a)
	}
	return out
}

// MentionTag membuat teks "@nomor" untuk sebuah JID.
func MentionTag(j types.JID) string { return "@" + BareID(j) }

// MentionList menggabungkan tag @nomor beberapa JID dipisah spasi.
func MentionList(jids []types.JID) string {
	parts := make([]string, len(jids))
	for i, j := range jids {
		parts[i] = MentionTag(j)
	}
	return strings.Join(parts, " ")
}
