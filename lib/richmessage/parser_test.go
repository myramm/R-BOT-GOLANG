package richmessage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParse_BasicHeaderBody(t *testing.T) {
	input := `<rich>
		<header>🌀 SPEEDY DASH</header>
		<body>Ready?</body>
		<button id="boost">⚡ BOOST</button>
		<button id="jump">↗ JUMP</button>
	</rich>`

	rm, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse gagal: %v", err)
	}
	if rm.Header != "🌀 SPEEDY DASH" {
		t.Errorf("Header salah: %q", rm.Header)
	}
	if rm.Body != "Ready?" {
		t.Errorf("Body salah: %q", rm.Body)
	}
	if len(rm.Buttons) != 2 {
		t.Fatalf("Jumlah button salah: %d", len(rm.Buttons))
	}
	if rm.Buttons[0].ID != "boost" || rm.Buttons[0].Text != "⚡ BOOST" {
		t.Errorf("Button 0 salah: %+v", rm.Buttons[0])
	}
	if rm.Buttons[1].ID != "jump" || rm.Buttons[1].Text != "↗ JUMP" {
		t.Errorf("Button 1 salah: %+v", rm.Buttons[1])
	}
}

func TestParse_WithFooter(t *testing.T) {
	input := `<rich>
		<header>TITLE</header>
		<body>Body</body>
		<footer>Footer text</footer>
		<button id="a">A</button>
	</rich>`

	rm, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse gagal: %v", err)
	}
	if rm.Footer != "Footer text" {
		t.Errorf("Footer salah: %q", rm.Footer)
	}
}

func TestParse_CustomParams(t *testing.T) {
	// Pakai single-quote di attr untuk JSON, atau escape karakter.
	// Parser saat ini pakai double-quote untuk attrs. Test pakai format valid
	// (params berisi plain string atau escaped JSON).
	input := `<rich>
		<body>X</body>
		<button id="a" params="custom_action_xyz">A</button>
	</rich>`

	rm, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse gagal: %v", err)
	}
	if rm.Buttons[0].Params != "custom_action_xyz" {
		t.Errorf("Params custom tidak ke-parse: %q", rm.Buttons[0].Params)
	}
}

func TestParse_NoBody(t *testing.T) {
	input := `<rich>
		<header>X</header>
	</rich>`

	_, err := Parse(input)
	if err == nil {
		t.Fatal("Seharusnya error karena body kosong")
	}
}

func TestParse_MultilineBody(t *testing.T) {
	input := `<rich>
		<header>Game</header>
		<body>Row 1: 1 │ 2 │ 3
Row 2: 4 │ 5 │ 6
Row 3: 7 │ 8 │ 9</body>
		<button id="5">Pilih 5</button>
	</rich>`

	rm, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse gagal: %v", err)
	}
	if !strings.Contains(rm.Body, "Row 1:") {
		t.Errorf("Multiline body tidak ke-parse dengan benar: %q", rm.Body)
	}
}

func TestToInteractiveMessage_ButtonPrefix(t *testing.T) {
	rm := &RichMessage{
		Header: "Test",
		Body:   "Body",
		Footer: "Footer",
		Buttons: []RichButton{
			{ID: "boost", Text: "BOOST"},
			{ID: "jump", Text: "JUMP"},
		},
	}

	im := rm.ToInteractiveMessage(".ttt ")

	if im.Body == nil || im.Body.GetText() == "" {
		t.Fatal("Body kosong")
	}
	if im.Footer == nil || im.Footer.GetText() == "" {
		t.Fatal("Footer kosong")
	}
	if im.InteractiveMessage == nil {
		t.Fatal("InteractiveMessage (oneof) kosong")
	}
	flow := im.GetNativeFlowMessage()
	if flow == nil {
		t.Fatal("NativeFlowMessage kosong")
	}
	if len(flow.Buttons) != 2 {
		t.Fatalf("Jumlah button di flow salah: %d", len(flow.Buttons))
	}
	for i, btn := range flow.Buttons {
		if btn.GetName() != "cta" {
			t.Errorf("Button %d name salah: %q", i, btn.GetName())
		}
		var params map[string]string
		if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &params); err != nil {
			t.Errorf("Button %d paramsJSON tidak valid JSON: %v", i, err)
			continue
		}
		if !strings.HasPrefix(params["id"], ".ttt ") {
			t.Errorf("Button %d id tanpa prefix: %q", i, params["id"])
		}
	}
}

func TestToInteractiveMessage_NoPrefix(t *testing.T) {
	rm := &RichMessage{
		Body: "X",
		Buttons: []RichButton{
			{ID: "a", Text: "A"},
		},
	}
	im := rm.ToInteractiveMessage("")
	flow := im.GetNativeFlowMessage()
	btn := flow.Buttons[0]
	var params map[string]string
	_ = json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &params)
	if params["id"] != "a" {
		t.Errorf("id tanpa prefix: %q", params["id"])
	}
}

func TestToInteractiveMessage_NoButtons(t *testing.T) {
	rm := &RichMessage{Body: "Just text"}
	im := rm.ToInteractiveMessage(".x ")
	if im.GetNativeFlowMessage() != nil {
		t.Error("Seharusnya tidak ada native flow message")
	}
}
