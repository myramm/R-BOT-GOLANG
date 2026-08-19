// Package command adalah kerangka command bot: registry, dispatcher, dan
// konteks eksekusi. Port dari lib/commands.js + helper lib/utils.js.
package command

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	stdDraw "image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	xDraw "golang.org/x/image/draw"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/config"
	"rbot/brain/energy"
	"rbot/brain/errortracker"
	"rbot/brain/identity"
	"rbot/brain/premium"
	"rbot/brain/settings"
	"rbot/brain/sponsor"
	"rbot/lib/httpx"
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

// SenderPhone mengembalikan nomor telepon pengirim (resilien terhadap LID).
func (c *Ctx) SenderPhone() string { return identity.SenderPhone(c.Evt) }

// IsGroup true bila pesan dari grup.
func (c *Ctx) IsGroup() bool { return c.Evt.Info.IsGroup }

// ArgStr menggabungkan seluruh argumen jadi satu string.
func (c *Ctx) ArgStr() string { return strings.Join(c.Args, " ") }

var (
	thumbMu          sync.RWMutex
	cachedThumbURL   string
	cachedThumbBytes []byte
)

// isValidNewsletterJID memeriksa apakah JID newsletter valid dan bukan placeholder/dummy.
func isValidNewsletterJID(jid string) bool {
	jid = strings.TrimSpace(jid)
	if !strings.HasSuffix(jid, "@newsletter") {
		return false
	}
	if strings.Contains(jid, "00000000") || len(jid) < 18 {
		return false
	}
	return true
}

// compressThumbnailBytes mengubah ukuran dan mengompresi byte gambar menjadi JPEG mini (max ~30KB)
// yang aman untuk protobuf WhatsApp ContextInfo.
func compressThumbnailBytes(data []byte, maxDim int, quality int) []byte {
	if len(data) == 0 {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}

	newW, newH := w, h
	if w > maxDim || h > maxDim {
		if w > h {
			newW = maxDim
			newH = (h * maxDim) / w
		} else {
			newH = maxDim
			newW = (w * maxDim) / h
		}
	}
	if newW <= 0 {
		newW = 1
	}
	if newH <= 0 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xDraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, xDraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil
	}
	res := buf.Bytes()
	if len(res) > 35*1024 && quality > 40 {
		buf.Reset()
		_ = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 40})
		res = buf.Bytes()
	}
	return res
}

// GetThumbnailBytes mengambil byte thumbnail dari URL (dengan caching in-memory dan kompresi JPEG).
func GetThumbnailBytes(url string) []byte {
	if url == "" {
		return nil
	}
	thumbMu.RLock()
	if cachedThumbURL == url && len(cachedThumbBytes) > 0 {
		b := cachedThumbBytes
		thumbMu.RUnlock()
		return b
	}
	thumbMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := httpx.GetBytes(ctx, url, 3*time.Second, 2*1024*1024)
	if err == nil && len(raw) > 0 {
		compressed := compressThumbnailBytes(raw, 300, 75)
		if len(compressed) > 0 {
			thumbMu.Lock()
			cachedThumbURL = url
			cachedThumbBytes = compressed
			thumbMu.Unlock()
			return compressed
		}
	}
	return nil
}

var (
	defaultThumbOnce  sync.Once
	defaultThumbBytes []byte
)

func getDefaultThumbnail() []byte {
	defaultThumbOnce.Do(func() {
		width, height := 300, 300
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r := uint8(20 + (x * 40 / width))
				g := uint8(30 + (y * 50 / height))
				b := uint8(90 + ((x + y) * 80 / (width + height)))
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
		inner := image.Rect(60, 60, 240, 240)
		stdDraw.Draw(img, inner, &image.Uniform{color.RGBA{99, 102, 241, 255}}, image.Point{}, stdDraw.Src)

		var buf bytes.Buffer
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
		defaultThumbBytes = buf.Bytes()
	})
	return defaultThumbBytes
}

// ApplyCustomContextInfo menyematkan konfigurasi ExternalAdReply dan Newsletter
// ke ContextInfo pesan keluar bila fitur custom context info diaktifkan.
func ApplyCustomContextInfo(ci *waE2E.ContextInfo) {
	if ci == nil {
		return
	}
	cfg := settings.GetContextInfo()
	if !cfg.Enabled {
		return
	}

	if cfg.IsForwarded {
		ci.IsForwarded = proto.Bool(true)
	}
	if cfg.ForwardingScore > 0 {
		ci.ForwardingScore = proto.Uint32(cfg.ForwardingScore)
	}

	// Saluran WhatsApp / Newsletter: HANYA sematkan jika JID valid dan bukan dummy
	if isValidNewsletterJID(cfg.NewsletterJID) && strings.TrimSpace(cfg.NewsletterName) != "" {
		jid := strings.TrimSpace(cfg.NewsletterJID)
		name := strings.TrimSpace(cfg.NewsletterName)
		msgID := cfg.ServerMessageID
		if msgID <= 0 {
			msgID = 1
		}
		ci.ForwardedNewsletterMessageInfo = &waE2E.ContextInfo_ForwardedNewsletterMessageInfo{
			NewsletterJID:   proto.String(jid),
			NewsletterName:  proto.String(name),
			ServerMessageID: proto.Int32(msgID),
		}
		ci.IsForwarded = proto.Bool(true)
		if ci.ForwardingScore == nil || *ci.ForwardingScore == 0 {
			ci.ForwardingScore = proto.Uint32(1)
		}
	}

	// ExternalAdReply Banner Card
	mediaType := waE2E.ContextInfo_ExternalAdReplyInfo_IMAGE
	if cfg.MediaType == 2 {
		mediaType = waE2E.ContextInfo_ExternalAdReplyInfo_VIDEO
	}

	title := strings.TrimSpace(cfg.Title)
	if title == "" {
		title = config.C.BotName + " Assistant"
	}
	body := strings.TrimSpace(cfg.Body)
	if body == "" {
		body = "Multi-Device WhatsApp Bot"
	}
	sourceURL := strings.TrimSpace(cfg.SourceURL)
	if sourceURL == "" {
		sourceURL = "https://github.com/myramm/R-BOT-GOLANG"
	}
	thumbURL := strings.TrimSpace(cfg.ThumbnailURL)

	var thumbBytes []byte
	if thumbURL != "" {
		thumbBytes = GetThumbnailBytes(thumbURL)
	}
	if len(thumbBytes) == 0 {
		thumbBytes = getDefaultThumbnail()
	}

	adReply := &waE2E.ContextInfo_ExternalAdReplyInfo{
		Title:                 proto.String(title),
		Body:                  proto.String(body),
		MediaType:             &mediaType,
		RenderLargerThumbnail: proto.Bool(cfg.RenderLargerThumbnail),
		ShowAdAttribution:     proto.Bool(cfg.ShowAdAttribution),
		SourceURL:             proto.String(sourceURL),
		MediaURL:              proto.String(sourceURL),
		Thumbnail:             thumbBytes,
	}
	if thumbURL != "" {
		adReply.ThumbnailURL = proto.String(thumbURL)
	}
	ci.ExternalAdReply = adReply
}

// BuildContextInfo membuat ContextInfo dengan quote (bila diminta) dan menyematkan
// ExternalAdReply / Newsletter info sesuai konfigurasi yang aktif.
func (c *Ctx) BuildContextInfo(quoted bool) *waE2E.ContextInfo {
	ci := &waE2E.ContextInfo{}
	if quoted && c.Evt != nil {
		ci.StanzaID = proto.String(c.Evt.Info.ID)
		if c.Evt.Info.IsGroup {
			ci.Participant = proto.String(c.Evt.Info.Sender.ToNonAD().String())
		}
		ci.QuotedMessage = c.Evt.Message
	}
	ApplyCustomContextInfo(ci)
	return ci
}

// Reply mengirim balasan teks yang mengutip (quote) pesan pemicu dengan ContextInfo custom.
// Jika terjadi error saat pengiriman pesan ber-ContextInfo, bot otomatis melakukan fallback
// pengiriman pesan quote standar / plain text sehingga bot tetap merespon tanpa silent failure.
func (c *Ctx) Reply(ctx context.Context, text string) (whatsmeow.SendResponse, error) {
	ci := c.BuildContextInfo(true)
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: ci,
		},
	}
	resp, err := c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	if err != nil {
		log.Printf("[rbot] [Reply] Gagal mengirim balasan dengan ContextInfo (%v). Mencoba fallback pengiriman quote standar...", err)
		simpleCI := &waE2E.ContextInfo{
			StanzaID:      proto.String(c.Evt.Info.ID),
			QuotedMessage: c.Evt.Message,
		}
		if c.Evt.Info.IsGroup {
			simpleCI.Participant = proto.String(c.Evt.Info.Sender.ToNonAD().String())
		}
		fallbackMsg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(text),
				ContextInfo: simpleCI,
			},
		}
		resp, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, fallbackMsg)
		if err != nil {
			return c.Client.SendMessage(ctx, c.Evt.Info.Chat, &waE2E.Message{Conversation: proto.String(text)})
		}
	}
	return resp, nil
}

// SendText mengirim teks biasa tanpa quote.
func (c *Ctx) SendText(ctx context.Context, text string) (whatsmeow.SendResponse, error) {
	return c.Client.SendMessage(ctx, c.Evt.Info.Chat, &waE2E.Message{Conversation: proto.String(text)})
}

// ReplyMentions seperti Reply tapi menandai (mention) daftar JID.
func (c *Ctx) ReplyMentions(ctx context.Context, text string, mentions []types.JID) (whatsmeow.SendResponse, error) {
	jids := make([]string, len(mentions))
	for i, m := range mentions {
		jids[i] = m.ToNonAD().String()
	}
	ci := c.BuildContextInfo(true)
	ci.MentionedJID = jids
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: ci,
		},
	}
	resp, err := c.Client.SendMessage(ctx, c.Evt.Info.Chat, msg)
	if err != nil {
		log.Printf("[rbot] [ReplyMentions] Gagal mengirim pesan mentions dengan ContextInfo (%v). Mencoba fallback...", err)
		simpleCI := &waE2E.ContextInfo{
			StanzaID:      proto.String(c.Evt.Info.ID),
			QuotedMessage: c.Evt.Message,
			MentionedJID:  jids,
		}
		if c.Evt.Info.IsGroup {
			simpleCI.Participant = proto.String(c.Evt.Info.Sender.ToNonAD().String())
		}
		fallbackMsg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(text),
				ContextInfo: simpleCI,
			},
		}
		resp, err = c.Client.SendMessage(ctx, c.Evt.Info.Chat, fallbackMsg)
		if err != nil {
			return c.Client.SendMessage(ctx, c.Evt.Info.Chat, &waE2E.Message{Conversation: proto.String(text)})
		}
	}
	return resp, nil
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
	// IsGroupAdminHook: true bila pengirim pesan admin di grup.
	IsGroupAdminHook func(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool
	// StatsHook: catat statistik pemakaian command.
	StatsHook func(cmdName string, evt *events.Message)
	// SubBotStatsHook: catat statistik pemakaian command khusus sub-bot / jadibot.
	SubBotStatsHook func(client *whatsmeow.Client, cmdName string, evt *events.Message, isError bool)
	// ErrorHook menerima error command/panic agar runtime dapat meneruskannya
	// ke owner tanpa membuat command dispatcher bergantung pada transport tujuan.
	ErrorHook func(ctx context.Context, c *Ctx, err error)
	// SimiHook: balas otomatis percakapan/sticker jika user me-reply pesan bot.
	SimiHook func(ctx context.Context, client *whatsmeow.Client, evt *events.Message) bool
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
	fmt.Fprintf(&b, "💤 Ketik *%sistirahat* untuk memulihkan energi kamu.", mp)

	if !premium.IsPremium(evt) {
		fmt.Fprintf(&b, "\n\n💎 Upgrade ke premium untuk energi lebih: *%spremium*", mp)
	}
	if url := config.C.GrupOfficial.Invite; url != "" {
		fmt.Fprintf(&b, "\n\n🎁 Join grup official buat diskon energi:\n%s", url)
	}
	return b.String()
}

// Dispatch mengurai pesan masuk, mencocokkan command, dan menjalankannya.
// Port dari handleCommand: prefix → resolve → ownerOnly → charge energi →
// handler → consume → reward join → footer sponsor.
func Dispatch(ctx context.Context, client *whatsmeow.Client, evt *events.Message, subBot bool) {
	text := ExtractText(evt.Message)

	var key string
	var args []string
	var cmd *Command

	prefix := config.MatchPrefix(text)
	if prefix == "" {
		// Pesan tanpa prefix: Cek pemicu command owner langsung (#, $, >, =>)
		trimmed := strings.TrimSpace(text)
		if !subBot && IsOwner(evt) {
			switch {
			case strings.HasPrefix(trimmed, "=>"):
				cmd = Resolve(">")
				key = ">"
				args = strings.Fields(strings.TrimSpace(trimmed[2:]))
			case strings.HasPrefix(trimmed, ">"):
				cmd = Resolve(">")
				key = ">"
				args = strings.Fields(strings.TrimSpace(trimmed[1:]))
			case strings.HasPrefix(trimmed, "#"):
				cmd = Resolve("#")
				key = "#"
				args = strings.Fields(strings.TrimSpace(trimmed[1:]))
			case strings.HasPrefix(trimmed, "$"):
				cmd = Resolve("$")
				key = "$"
				args = strings.Fields(strings.TrimSpace(trimmed[1:]))
			}
		}

		if cmd == nil {
			cands := identity.Candidates(evt)
			if settings.IsUserBlacklisted(cands) && !IsOwner(evt) {
				return
			}
			if evt.Info.IsGroup {
				groupID := evt.Info.Chat.String()
				if settings.IsGroupMuted(groupID) {
					return
				}
				if settings.IsUserBannedInGroup(groupID, cands) && !IsOwner(evt) {
					return
				}
			}

			// SimiHook: auto-reply jika me-reply pesan bot
			if SimiHook != nil && SimiHook(ctx, client, evt) {
				return
			}

			// Tanpa prefix: coba lanjutkan sesi interaktif (ada teks).
			if text != "" && ResumeHook != nil {
				ResumeHook(ctx, client, evt, text)
			}
			return
		}
	} else {
		rest := strings.TrimSpace(text[len(prefix):])
		if rest == "" {
			return
		}
		fields := strings.Fields(rest)
		key = strings.ToLower(fields[0])
		args = fields[1:]
		cmd = Resolve(key)
	}

	cands := identity.Candidates(evt)

	// 0. Cek Mode Self (Hanya Owner yang boleh memakai bot saat Self Mode aktif)
	if settings.IsSelfMode() && !IsOwner(evt) {
		return
	}

	// 1. Cek Global Blacklist (100% diblokir dari bot di mana pun, kecuali Owner)
	if settings.IsUserBlacklisted(cands) && !IsOwner(evt) {
		return
	}

	if cmd == nil {
		if evt.Info.IsGroup {
			groupID := evt.Info.Chat.String()
			if settings.IsGroupMuted(groupID) {
				return
			}
			if settings.IsUserBannedInGroup(groupID, cands) && !IsOwner(evt) {
				return
			}
		}

		// SimiHook: auto-reply jika me-reply pesan bot
		if SimiHook != nil && SimiHook(ctx, client, evt) {
			return
		}
		return
	}
	if cmd.OwnerOnly {
		if subBot || !IsOwner(evt) {
			return
		}
	}

	c := &Ctx{Client: client, Evt: evt, Args: args, Text: text, InvokedAs: key, SubBot: subBot}

	// 2. Cek Pengaturan Grup (Group Mute & Group Ban User di grup tertentu)
	if c.IsGroup() {
		groupID := c.Chat().String()
		isOwner := IsOwner(evt)

		// Cek Mute GC (Bot di-mute total di grup ini)
		// Saat grup di-mute, SEMUA command di grup diabaikan total,
		// KECUALI command "mute" (.unmute / .unmutegc / .unbangc) yang hanya bisa dieksekusi Admin/Owner.
		if settings.IsGroupMuted(groupID) {
			if cmd.Name != "mute" {
				return
			}
		}

		// Cek Ban User khusus di grup ini (User yang di-ban tidak bisa memakai bot di grup ini)
		if settings.IsUserBannedInGroup(groupID, cands) && !isOwner {
			isAdmin := false
			if IsGroupAdminHook != nil {
				isAdmin = IsGroupAdminHook(ctx, client, evt)
			}
			if !isAdmin {
				return
			}
		}
	}

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
	if subBot && SubBotStatsHook != nil {
		SubBotStatsHook(client, cmd.Name, evt, false)
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic: %v", r)
				log.Printf("[rbot] panic di command %q: %v", key, err)
				errContext := fmt.Sprintf("Command: .%s | Sender: %s | Chat: %s | Args: %s", key, c.Sender().String(), c.Chat().String(), c.ArgStr())
				errortracker.RecordError("COMMAND", fmt.Sprintf("panic di command .%s: %v", key, r), errContext)
				notifyErrorHook(ctx, c, err)
				if subBot && SubBotStatsHook != nil {
					SubBotStatsHook(client, cmd.Name, evt, true)
				}
			}
		}()
		if err := cmd.Handler(ctx, c); err != nil {
			log.Printf("[rbot] error command %q: %v", key, err)
			errContext := fmt.Sprintf("Command: .%s | Sender: %s | Chat: %s | Args: %s", key, c.Sender().String(), c.Chat().String(), c.ArgStr())
			errortracker.RecordError("COMMAND", fmt.Sprintf("error command .%s: %v", key, err), errContext)
			notifyErrorHook(ctx, c, err)
			if subBot && SubBotStatsHook != nil {
				SubBotStatsHook(client, cmd.Name, evt, true)
			}
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
