// Package richmessage adalah parser untuk format rich message XML-like
// yang di-convert jadi waE2E.InteractiveMessage (NativeFlowMessage).
// Format didukung:
//
//	<rich>
//	  <header>...</header>
//	  <body>...</body>
//	  <footer>...</footer>
//	  <button id="...">...</button>
//	</rich>
//
// Button callback dikirim sebagai command prefix + id (misal: ".ttt boost").
// Parser ini mengikuti prinsip SAZA-Bot-Go: gunakan NativeFlowMessage
// dengan button paramsJSON berisi action ID.
package richmessage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
)

// RichTag adalah tipe tag yang didukung parser.
type RichTag string

const (
	TagHeader RichTag = "header"
	TagBody   RichTag = "body"
	TagFooter RichTag = "footer"
	TagButton RichTag = "button"
	TagImage  RichTag = "image"
	TagVideo  RichTag = "video"
)

// RichButton adalah satu tombol yang di-parse.
type RichButton struct {
	ID     string
	Text   string
	Params string
}

// RichMessage adalah struktur intermediate sebelum di-convert jadi InteractiveMessage.
type RichMessage struct {
	Header  string
	Body    string
	Footer  string
	Buttons []RichButton
}

// Parse mengonversi string XML-like jadi RichMessage.
// Format:
//
//	<rich>
//	  <header>...</header>
//	  <body>...</body>
//	  <footer>...</footer>
//	  <button id="...">...</button>
//	</rich>
//
// Contoh:
//
//	<rich>
//	  <header>GAME</header>
//	  <body>Board...</body>
//	  <button id="boost">BOOST</button>
//	</rich>
func Parse(xml string) (*RichMessage, error) {
	xml = strings.TrimSpace(xml)
	if !strings.HasPrefix(xml, "<rich") {
		return nil, errors.New("root tag harus <rich>")
	}

	rm := &RichMessage{}
	body := xml
	body = strings.TrimPrefix(body, "<rich>")
	body = strings.TrimSuffix(body, "</rich>")
	body = strings.TrimSpace(body)

	for {
		idx := strings.IndexAny(body, "<")
		if idx < 0 {
			break
		}
		body = body[idx:]
		end := strings.IndexByte(body, '>')
		if end < 0 {
			return nil, fmt.Errorf("tag tanpa penutup: %q", body)
		}
		openTag := body[:end+1]
		attrs, tagName, isClose, isSelfClose := parseOpenTag(openTag)
		if isClose {
			body = body[end+1:]
			continue
		}
		if isSelfClose || isVoidTag(tagName) {
			if tagName == string(TagButton) {
				id, text, params := parseButtonAttrs(attrs, "")
				rm.Buttons = append(rm.Buttons, RichButton{ID: id, Text: text, Params: params})
			}
			body = body[end+1:]
			continue
		}
		closeTag := fmt.Sprintf("</%s>", tagName)
		closeIdx := strings.Index(body, closeTag)
		if closeIdx < 0 {
			return nil, fmt.Errorf("tag %s tidak punya penutup", tagName)
		}
		content := body[end+1 : closeIdx]
		body = body[closeIdx+len(closeTag):]

		switch RichTag(strings.ToLower(tagName)) {
		case TagHeader:
			rm.Header = strings.TrimSpace(content)
		case TagBody:
			rm.Body = strings.TrimSpace(content)
		case TagFooter:
			rm.Footer = strings.TrimSpace(content)
		case TagButton:
			id, text, params := parseButtonAttrs(attrs, content)
			rm.Buttons = append(rm.Buttons, RichButton{
				ID:     id,
				Text:   text,
				Params: params,
			})
		}
	}

	if rm.Body == "" {
		return nil, errors.New("body tidak boleh kosong")
	}
	return rm, nil
}

func parseOpenTag(tag string) (attrs string, tagName string, isClose, isSelfClose bool) {
	t := strings.TrimSpace(tag[1 : len(tag)-1])
	if strings.HasPrefix(t, "/") {
		return "", t[1:], true, false
	}
	if strings.HasSuffix(t, "/") {
		t = strings.TrimSpace(t[:len(t)-1])
		isSelfClose = true
	}
	parts := strings.SplitN(t, " ", 2)
	tagName = parts[0]
	if len(parts) > 1 {
		attrs = parts[1]
	}
	return attrs, tagName, false, isSelfClose
}

func isVoidTag(name string) bool {
	return false
}

func parseButtonAttrs(attrs, content string) (id, text, params string) {
	id = matchAttr(attrs, "id")
	params = matchAttr(attrs, "params")
	text = strings.TrimSpace(content)
	return
}

func matchAttr(attrs, name string) string {
	prefix := name + "=\""
	i := strings.Index(attrs, prefix)
	if i < 0 {
		return ""
	}
	rest := attrs[i+len(prefix):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// ToInteractiveMessage mengonversi RichMessage jadi waE2E.InteractiveMessage.
// Prefix digunakan untuk button callback (misal: ".ttt").
func (rm *RichMessage) ToInteractiveMessage(prefix string) *waE2E.InteractiveMessage {
	msg := &waE2E.InteractiveMessage{
		Body: &waE2E.InteractiveMessage_Body{
			Text: proto.String(rm.Body),
		},
		Footer: &waE2E.InteractiveMessage_Footer{
			Text: proto.String(rm.Footer),
		},
	}

	if rm.Header != "" {
		msg.Header = &waE2E.InteractiveMessage_Header{
			// Header text via Body text fallback
		}
		// WhatsApp tidak support header text di NativeFlowMessage,
		// jadi kita prepend header ke body
		msg.Body.Text = proto.String(rm.Header + "\n\n" + rm.Body)
	}

	if len(rm.Buttons) > 0 {
		flowButtons := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, 0, len(rm.Buttons))
		for _, btn := range rm.Buttons {
			paramsJSON := btn.Params
			if paramsJSON == "" {
				// Default: button ID jadi callback
				// Format JSON harus pakai key "id" karena command.ExtractText
				// parseNativeFlowID hanya membaca key "id" (lihat brain/command/command.go).
				actionID := btn.ID
				if prefix != "" {
					actionID = prefix + btn.ID
				}
				paramsMap := map[string]string{
					"id": actionID,
				}
				b, _ := json.Marshal(paramsMap)
				paramsJSON = string(b)
			}
			flowButtons = append(flowButtons, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
				Name:             proto.String("cta"),
				ButtonParamsJSON: proto.String(paramsJSON),
			})
		}
		msg.InteractiveMessage = &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons: flowButtons,
			},
		}
	}

	return msg
}

// ToProtoMessage mengonversi RichMessage jadi *waE2E.Message.
func (rm *RichMessage) ToProtoMessage(prefix string) *waE2E.Message {
	return &waE2E.Message{
		InteractiveMessage: rm.ToInteractiveMessage(prefix),
	}
}

// PrettyPrint debug RichMessage.
func (rm *RichMessage) PrettyPrint() string {
	var b bytes.Buffer
	b.WriteString("RichMessage{\n")
	b.WriteString(fmt.Sprintf("  Header: %q\n", rm.Header))
	b.WriteString(fmt.Sprintf("  Body: %q\n", rm.Body))
	b.WriteString(fmt.Sprintf("  Footer: %q\n", rm.Footer))
	b.WriteString("  Buttons: [\n")
	for _, btn := range rm.Buttons {
		b.WriteString(fmt.Sprintf("    {ID: %q, Text: %q, Params: %q}\n", btn.ID, btn.Text, btn.Params))
	}
	b.WriteString("  ]\n")
	b.WriteString("}")
	return b.String()
}