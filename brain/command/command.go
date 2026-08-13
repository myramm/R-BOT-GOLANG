// Package command adalah kerangka command bot: registry, dispatcher, dan
// konteks eksekusi. Port dari lib/commands.js + helper lib/utils.js.
package command

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/energy"
	"rbot/brain/identity"
	"rbot/brain/premium"
	"rbot/brain/sponsor"
)

// Handler adalah fungsi eksekusi command. Mengembalikan error untuk logging.
type Handler func(ctx context.Context, c *Ctx) error

// Command mendeskripsikan satu perintah bot (setara module.exports di *.js).
type Command struct {
	Name        string
	Category    string
	Alias       []string
	Description string
	OwnerOnly   bool
	Handler     Handler
}

var (
	mu       sync.RWMutex
	registry = map[string]*Command{}
	aliases  = map[string]string{}
)

// Register mendaftarkan command. Dipanggil dari init() tiap file command.
func Register(cmd *Command) {
	if cmd == nil || cmd.Name == "" || cmd.Handler == nil {
		panic("command tidak valid: butuh Name & Handler")
	}
	mu.Lock()
	defer mu.Unlock()
	registry[cmd.Name] = cmd
	for _, a := range cmd.Alias {
		key := strings.ToLower(a)
		if key == cmd.Name {
			continue
		}
		if _, bentrok := registry[key]; bentrok {
			log.Printf("[rbot] alias %q (dari %s) bentrok dengan nama command, dilewati", key, cmd.Name)
			continue
		}
		if prev, ada := aliases[key]; ada && prev != cmd.Name {
			log.Printf("[rbot] alias %q (dari %s) bentrok dengan alias %q, dilewati", key, cmd.Name, prev)
			continue
		}
		aliases[key] = cmd.Name
	}
}

// Resolve mengembalikan command untuk sebuah key (nama atau alias), atau nil.
func Resolve(key string) *Command {
	key = strings.ToLower(key)
	mu.RLock()
	defer mu.RUnlock()
	if c, ok := registry[key]; ok {
		return c
	}
	if name, ok := aliases[key]; ok {
		return registry[name]
	}
	return nil
}

// All mengembalikan seluruh command terurut berdasarkan nama.
func All() []*Command {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Command, 0, len(registry))
	for _, c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Count mengembalikan jumlah command terdaftar.
func Count() int {
	mu.RLock()
	defer mu.RUnlock()
	return len(registry)
}

// Ctx adalah konteks yang diteruskan ke handler (setara (sock, msg, args, ctx)).
type Ctx struct {
	Client    *whatsmeow.Client
	Evt       *events.Message
	Args      []string
	Text      string
	InvokedAs string
	SubBot    bool
}

// Chat mengembalikan JID chat (remoteJid).
func (c *Ctx) Chat() types.JID { return c.Evt.Info.Chat }

// Sender mengembalikan JID pengirim (participant).
func (c *Ctx) Sender() types.JID { return c.Evt.Info.Sender }

// IsGroup true bila pesan dari grup.
func (c *Ctx) IsGroup() bool { return c.Evt.Info.IsGroup }

// ArgStr menggabungkan seluruh argumen jadi satu string.
func (c *Ctx) ArgStr() string { return strings.Join(c.Args, " ") }

// Reply mengirim balasan teks yang mengutip (quote) pesan pemicu.
func (c *Ctx) Reply(ctx context.Context, text string) (whatsmeow.SendResponse, error) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(c.Evt.Info.ID),
				Participant:   proto.String(c.Evt.Info.Sender.String()),
				QuotedMessage: c.Evt.Message,
			},
		},
	}
	return c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
}

// SendText mengirim teks biasa tanpa quote.
func (c *Ctx) SendText(ctx context.Context, text string) (whatsmeow.SendResponse, error) {
	return c.Client.SendMessage(ctx, c.Evt.Info.Chat, &waE2E.Message{Conversation: proto.String(text)})
}

// ReplyMentions seperti Reply tapi menandai (mention) daftar JID, sehingga tag
// @nomor pada teks bisa diklik & memberi notifikasi (port opsi { mentions } Baileys).
func (c *Ctx) ReplyMentions(ctx context.Context, text string, mentions []types.JID) (whatsmeow.SendResponse, error) {
	jids := make([]string, len(mentions))
	for i, m := range mentions {
		jids[i] = m.String()
	}
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(c.Evt.Info.ID),
				Participant:   proto.String(c.Evt.Info.Sender.String()),
				QuotedMessage: c.Evt.Message,
				MentionedJID:  jids,
			},
		},
	}
	return c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
}

// React memasang reaksi emoji ke pesan pemicu (best-effort, error diabaikan).
func (c *Ctx) React(ctx context.Context, emoji string) {
	r := c.Client.BuildReaction(c.Evt.Info.Chat, c.Evt.Info.Sender, c.Evt.Info.ID, emoji)
	_, _ = c.Client.SendMessage(ctx, c.Evt.Info.Chat, r)
}

// ContextInfo mengambil ContextInfo pesan pemicu (port getContextInfo group.js).
// whatsmeow sudah membuka bungkus ephemeral/viewOnce ke Evt.Message, jadi cukup
// baca tipe pesan yang lazim membawa contextInfo (mention & quoted-reply). Getter
// protobuf aman-nil, jadi tak perlu cek nil per tipe.
func (c *Ctx) ContextInfo() *waE2E.ContextInfo {
	m := c.Evt.Message
	if m == nil {
		return nil
	}
	if ci := m.GetExtendedTextMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetImageMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetVideoMessage().GetContextInfo(); ci != nil {
		return ci
	}
	if ci := m.GetDocumentMessage().GetContextInfo(); ci != nil {
		return ci
	}
	return nil
}

// ExtractText mengambil teks/caption/pilihan-button dari pesan (port getText).
func ExtractText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	if v := m.GetInteractiveResponseMessage(); v != nil {
		if nf := v.GetNativeFlowResponseMessage(); nf != nil {
			if id := parseNativeFlowID(nf.GetParamsJSON()); id != "" {
				return id
			}
		}
		if b := v.GetBody(); b != nil && b.GetText() != "" {
			return strings.TrimSpace(b.GetText())
		}
	}
	if v := m.GetButtonsResponseMessage(); v != nil {
		if id := v.GetSelectedButtonID(); id != "" {
			return strings.TrimSpace(id)
		}
	}
	if v := m.GetListResponseMessage(); v != nil {
		if ssr := v.GetSingleSelectReply(); ssr != nil && ssr.GetSelectedRowID() != "" {
			return strings.TrimSpace(ssr.GetSelectedRowID())
		}
		if v.GetTitle() != "" {
			return strings.TrimSpace(v.GetTitle())
		}
	}
	if v := m.GetTemplateButtonReplyMessage(); v != nil {
		if v.GetSelectedID() != "" {
			return strings.TrimSpace(v.GetSelectedID())
		}
	}
	if c := m.GetConversation(); c != "" {
		return strings.TrimSpace(c)
	}
	if e := m.GetExtendedTextMessage(); e != nil {
		return strings.TrimSpace(e.GetText())
	}
	if i := m.GetImageMessage(); i != nil {
		return strings.TrimSpace(i.GetCaption())
	}
	if vid := m.GetVideoMessage(); vid != nil {
		return strings.TrimSpace(vid.GetCaption())
	}
	return ""
}

// parseNativeFlowID mengambil id/selectedId/row_id/text dari paramsJson button.
func parseNativeFlowID(paramsJSON string) string {
	if paramsJSON == "" {
		return ""
	}
	// Parsing ringan tanpa struct: cari kunci umum.
	for _, k := range []string{`"id"`, `"selectedId"`, `"row_id"`, `"text"`} {
		if v := jsonStringField(paramsJSON, k); v != "" {
			return v
		}
	}
	return ""
}

// jsonStringField mengambil nilai string untuk key dari JSON datar (best-effort).
func jsonStringField(s, quotedKey string) string {
	i := strings.Index(s, quotedKey)
	if i < 0 {
		return ""
	}
	rest := s[i+len(quotedKey):]
	c := strings.IndexByte(rest, ':')
	if c < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[c+1:])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// IsOwner mengecek apakah pengirim termasuk owner. Diteruskan ke paket
// identity (layer bawah) supaya energy/premium bisa memakainya tanpa cycle.
func IsOwner(evt *events.Message) bool { return identity.IsOwner(evt) }

// heavyCommands adalah command yang menarik energi (port HEAVY_COMMANDS).
// Dicek berdasarkan nama kanonik command (bukan alias).
var heavyCommands = map[string]bool{
	"hd": true, "smooth": true, "sticker": true,
	"ai": true, "play": true, "download": true,
}

// skipSponsor: command yang tidak memicu footer sponsor (port SKIP_SPONSOR).
var skipSponsor = map[string]bool{
	"menu": true, "sponsor": true, "setsponsor": true, "report": true,
}

// Hooks diisi oleh paket lain lewat init() supaya command.go tidak perlu
// mengimpor mereka (mencegah import cycle) dan tetap stabil saat fitur menyusul
// (grup official, session/jadibot, stats, rest, consume — semua Task #4).
var (
	// MemberGrupHook: true bila pengirim member grup official (diskon energi).
	MemberGrupHook func(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool
	// KlaimRewardHook: klaim reward join grup official (sekali per user).
	KlaimRewardHook func(ctx context.Context, client *whatsmeow.Client, evt *events.Message)
	// ResumeHook: lanjutkan sesi interaktif untuk pesan tanpa prefix.
	ResumeHook func(ctx context.Context, client *whatsmeow.Client, evt *events.Message, text string)
	// StatsHook: catat statistik pemakaian command.
	StatsHook func(cmdName string, evt *events.Message)
	// ErrorHook menerima error command/panic agar runtime dapat meneruskannya
	// ke owner tanpa membuat command dispatcher bergantung pada transport tujuan.
	ErrorHook func(ctx context.Context, c *Ctx, err error)
	// RestSectionHook & MakananSectionHook: bagian tambahan pesan "energi habis".
	RestSectionHook    func(evt *events.Message) string
	MakananSectionHook func(evt *events.Message) string
)

// ReportError meneruskan error teknis ke hook runtime tanpa mengubah balasan
// command ke user. Dipakai handler yang sudah menampilkan error lalu return nil.
// Pelaporan dilakukan sinkron agar laporan owner dibuat sebelum balasan user.
func (c *Ctx) ReportError(ctx context.Context, err error) {
	notifyErrorHookSync(ctx, c, err)
}

// ReportErrorMessage adalah bentuk praktis ReportError untuk helper yang sudah
// mengubah error menjadi pesan user-facing.
func (c *Ctx) ReportErrorMessage(ctx context.Context, message string) {
	if message != "" {
		c.ReportError(ctx, fmt.Errorf("%s", message))
	}
}

func notifyErrorHookSync(ctx context.Context, c *Ctx, err error) {
	hook := ErrorHook
	if hook == nil || err == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[rbot] panic saat meneruskan error command: %v", r)
		}
	}()
	hook(ctx, c, err)
}

func notifyErrorHook(ctx context.Context, c *Ctx, err error) {
	hook := ErrorHook
	if hook == nil || err == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[rbot] panic saat meneruskan error command: %v", r)
			}
		}()
		hook(ctx, c, err)
	}()
}

func memberGrup(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool {
	if MemberGrupHook == nil {
		return false
	}
	return MemberGrupHook(ctx, client, evt)
}

// energiHabisMessage menyusun pesan saat energi user habis (port pesanEnergiHabis).
// Bagian rest & makanan diisi lewat hook; sisanya (saldo, premium, invite) lokal.
func energiHabisMessage(evt *events.Message) string {
	e := energy.Get(evt)
	maxE := e.Max
	if maxE == 0 {
		maxE = e.Limit
	}
	mp := config.MainPrefix()

	var b strings.Builder
	fmt.Fprintf(&b, "⚡ *Energi habis!* (sisa %d/%d ⚡)\n\n", e.Bank, maxE)
	b.WriteString("*Silahkan istirahat atau makan hasil tani/ternak:*")

	if RestSectionHook != nil {
		if s := RestSectionHook(evt); s != "" {
			b.WriteString("\n\n" + s)
		}
	}
	if MakananSectionHook != nil {
		if s := MakananSectionHook(evt); s != "" {
			b.WriteString("\n\n" + s)
		}
	} else {
		fmt.Fprintf(&b, "\n\n_Kulkas kosong — panen dulu: %stani / %sternak_", mp, mp)
	}

	if !premium.IsPremium(evt) {
		fmt.Fprintf(&b, "\n\n💎 Atau upgrade ke premium: *%spremium*", mp)
	}
	if url := config.C.GrupOfficial.Invite; url != "" {
		fmt.Fprintf(&b, "\n\n🎁 Join grup official buat diskon 3%%:\n%s", url)
	}
	return b.String()
}

// Dispatch mengurai pesan masuk, mencocokkan command, dan menjalankannya.
// Port dari handleCommand: prefix → resolve → ownerOnly → charge energi →
// handler → consume → reward join → footer sponsor.
func Dispatch(ctx context.Context, client *whatsmeow.Client, evt *events.Message, subBot bool) {
	text := ExtractText(evt.Message)

	prefix := config.MatchPrefix(text)
	if prefix == "" {
		// Tanpa prefix: coba lanjutkan sesi interaktif (bukan sub-bot, ada teks).
		if !subBot && text != "" && ResumeHook != nil {
			ResumeHook(ctx, client, evt, text)
		}
		return
	}
	rest := strings.TrimSpace(text[len(prefix):])
	if rest == "" {
		return
	}
	fields := strings.Fields(rest)
	key := strings.ToLower(fields[0])
	args := fields[1:]

	cmd := Resolve(key)
	if cmd == nil {
		return
	}
	if cmd.OwnerOnly {
		if subBot || !IsOwner(evt) {
			return
		}
	}

	c := &Ctx{Client: client, Evt: evt, Args: args, Text: text, InvokedAs: key, SubBot: subBot}

	// Member grup official bayar energi lebih murah. Dicek sekali per command.
	member := !subBot && !config.C.Energy.IsUnlimited() && heavyCommands[cmd.Name] &&
		memberGrup(ctx, client, evt)

	chargesEnergy := !subBot && heavyCommands[cmd.Name] && !IsOwner(evt)
	cost := 0
	if chargesEnergy {
		cost = energy.BiayaEfektif(cmd.Name, member)
		if !energy.HasEnergy(evt, cost) {
			_, _ = c.Reply(ctx, energiHabisMessage(evt))
			return
		}
	}

	if StatsHook != nil {
		StatsHook(cmd.Name, evt)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Printf("[rbot] panic di command %q: %v", key, err)
				notifyErrorHook(ctx, c, err)
			}
		}()
		if err := cmd.Handler(ctx, c); err != nil {
			log.Printf("[rbot] error command %q: %v", key, err)
			notifyErrorHook(ctx, c, err)
		}
	}()

	if chargesEnergy {
		energy.Consume(evt, cost)
	}

	// Reward join grup official — sekali per user, tidak menahan balasan.
	if !subBot && member && KlaimRewardHook != nil {
		KlaimRewardHook(ctx, client, evt)
	}

	// Footer sponsor (throttled, non-premium, di luar command tertentu).
	if !subBot && !skipSponsor[cmd.Name] && !premium.IsPremium(evt) {
		if footer := sponsor.FooterThrottled(evt.Info.Chat.String(), time.Now()); footer != "" {
			_, _ = c.SendText(ctx, strings.TrimLeft(footer, "\n"))
		}
	}
}
