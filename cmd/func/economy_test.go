package cmdfunc

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"rbot/brain/command"
)

// ctxWith membuat command.Ctx sintetis dengan pesan + args (tanpa client).
func ctxWith(m *waE2E.Message, args ...string) *command.Ctx {
	e := &events.Message{}
	e.Message = m
	return &command.Ctx{Evt: e, Args: args}
}

// extText membungkus ContextInfo dalam ExtendedTextMessage (jalur mention/reply lazim).
func extText(ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: ci}}
}

func TestContextInfoJalurTipe(t *testing.T) {
	ci := &waE2E.ContextInfo{Participant: proto.String("628@s.whatsapp.net")}

	// nil message → nil.
	if got := ctxWith(nil).ContextInfo(); got != nil {
		t.Error("ContextInfo pesan nil harus nil")
	}
	// extendedText.
	if got := ctxWith(extText(ci)).ContextInfo(); got != ci {
		t.Error("ContextInfo extendedText tidak terbaca")
	}
	// imageMessage.
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ContextInfo: ci}}
	if got := ctxWith(img).ContextInfo(); got != ci {
		t.Error("ContextInfo imageMessage tidak terbaca")
	}
	// documentMessage.
	doc := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{ContextInfo: ci}}
	if got := ctxWith(doc).ContextInfo(); got != ci {
		t.Error("ContextInfo documentMessage tidak terbaca")
	}
	// Pesan teks biasa tanpa contextInfo → nil.
	if got := ctxWith(&waE2E.Message{Conversation: proto.String("halo")}).ContextInfo(); got != nil {
		t.Error("pesan tanpa contextInfo harus nil")
	}
}

func TestResolveTargetID(t *testing.T) {
	// Mention menang atas participant & args; JID di-bare-kan.
	ci := &waE2E.ContextInfo{
		MentionedJID: []string{"6283891155427@s.whatsapp.net"},
		Participant:  proto.String("999@s.whatsapp.net"),
	}
	if got := ResolveTargetID(ctxWith(extText(ci), "628000111")); got != "6283891155427" {
		t.Errorf("mention harus menang → %q, mau 6283891155427", got)
	}

	// Tanpa mention: pakai participant (reply), strip device/lid.
	ci2 := &waE2E.ContextInfo{Participant: proto.String("50324836462733:12@lid")}
	if got := ResolveTargetID(ctxWith(extText(ci2))); got != "50324836462733" {
		t.Errorf("participant → %q, mau 50324836462733", got)
	}

	// Tanpa ctx info: argumen angka ≥6 digit (setelah strip non-digit).
	if got := ResolveTargetID(ctxWith(&waE2E.Message{Conversation: proto.String("x")}, "hai", "+62 838-9115")); got != "628389115" {
		t.Errorf("arg angka → %q, mau 628389115", got)
	}

	// Arg terlalu pendek → kosong.
	if got := ResolveTargetID(ctxWith(nil, "123", "ok")); got != "" {
		t.Errorf("arg pendek harus kosong → %q", got)
	}

	// Sama sekali tak ada target → kosong.
	if got := ResolveTargetID(ctxWith(nil)); got != "" {
		t.Errorf("tanpa target harus kosong → %q", got)
	}
}

func TestFirstArgMatch(t *testing.T) {
	args := []string{"halo", "-5", "30", "--all"}
	if got := FirstArgMatch(args, ReDigits); got != "30" {
		t.Errorf("ReDigits ambil pure-digit pertama → %q, mau 30 (-5 tak cocok)", got)
	}
	if got := FirstArgMatch(args, ReSignedDigits); got != "-5" {
		t.Errorf("ReSignedDigits ambil bertanda pertama → %q, mau -5", got)
	}
	if got := FirstArgMatch(args, ReAll); got != "--all" {
		t.Errorf("ReAll → %q, mau --all", got)
	}
	if got := FirstArgMatch([]string{"-all"}, ReAll); got != "-all" {
		t.Errorf("ReAll juga terima -all → %q", got)
	}
	if got := FirstArgMatch([]string{"abc"}, ReDigits); got != "" {
		t.Errorf("tak ada cocok → %q, mau kosong", got)
	}
}

func TestFmtDate(t *testing.T) {
	// Pakai zona lokal (mirror new Date() Node) supaya stabil lintas TZ.
	d := time.Date(2026, time.February, 5, 15, 30, 0, 0, time.Local)
	if got := FmtDate(d.UnixMilli()); got != "05/02/2026" {
		t.Errorf("FmtDate = %q, mau 05/02/2026", got)
	}
}
