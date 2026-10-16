package notify

import (
	"encoding/xml"
	"strings"
	"testing"
)

type toastDoc struct {
	Launch   string `xml:"launch,attr"`
	Duration string `xml:"duration,attr"`
	Images   []struct {
		Placement string `xml:"placement,attr"`
		Src       string `xml:"src,attr"`
		Crop      string `xml:"hint-crop,attr"`
	} `xml:"visual>binding>image"`
	Texts []string `xml:"visual>binding>text"`
	Audio struct {
		Src    string `xml:"src,attr"`
		Silent string `xml:"silent,attr"`
	} `xml:"audio"`
}

func parseToast(t *testing.T, s string) toastDoc {
	t.Helper()
	var d toastDoc
	if err := xml.Unmarshal([]byte(s), &d); err != nil {
		t.Fatalf("not well-formed: %v\n%s", err, s)
	}
	return d
}

func TestToastXMLEscapesAndStripsInvalidChars(t *testing.T) {
	evil := "a<b>&\"'\x01\x08\x0b\x1f]]> \"; Start-Process calc; \"\ufffe\U0001F514 ok\ttab"
	n := Notification{ID: `s1-m"2<`, Title: evil, Body: evil, AppName: "App & Co", IconPath: `C:\a&b\"x.png`, ImagePath: `C:\h<.png`, Level: LevelNormal}
	d := parseToast(t, buildToastXML(n))
	want := "a<b>&\"'" + "]]> \"; Start-Process calc; \"" + "\U0001F514 ok\ttab"
	if d.Texts[0] != want {
		t.Fatalf("title %q, want %q", d.Texts[0], want)
	}
	if d.Texts[1] != "App & Co · "+want {
		t.Fatalf("body %q", d.Texts[1])
	}
	if d.Launch != n.ID || d.Images[0].Src != n.ImagePath || d.Images[1].Src != n.IconPath {
		t.Fatalf("%+v", d)
	}
	if d.Images[0].Placement != "hero" || d.Images[1].Placement != "appLogoOverride" || d.Images[1].Crop != "circle" {
		t.Fatalf("%+v", d.Images)
	}
	if strings.ContainsAny(buildToastXML(n), "\x01\x08\x0b\x1f\ufffe") {
		t.Fatal("invalid characters survived")
	}
}

func TestToastXMLLevels(t *testing.T) {
	for level, want := range map[Level][3]string{
		LevelSilent: {"short", "", "true"},
		LevelNormal: {"short", "ms-winsoundevent:Notification.Default", ""},
		LevelHigh:   {"long", "ms-winsoundevent:Notification.Reminder", ""},
	} {
		d := parseToast(t, buildToastXML(Notification{ID: "x", Title: "t", Level: level}))
		if d.Duration != want[0] || d.Audio.Src != want[1] || d.Audio.Silent != want[2] {
			t.Errorf("level %d: %+v", level, d)
		}
		if len(d.Images) != 0 || len(d.Texts) != 1 {
			t.Errorf("optional parts: %+v", d)
		}
	}
}

func TestActivatorCLSID(t *testing.T) {
	a, b := activatorCLSID("com.austin.gotifydesktop"), activatorCLSID("com.austin.gotifydesktop.notifytest")
	if a == b || a != activatorCLSID("com.austin.gotifydesktop") {
		t.Fatalf("%s %s", a, b)
	}
	if len(a) != 38 || a[0] != '{' || a[15] != '5' {
		t.Fatalf("not a v5 uuid: %s", a)
	}
}

func TestToastXMLActions(t *testing.T) {
	n := Notification{ID: "s1-m2", Title: "t", Actions: []Action{{ID: ActionRead, Label: "Mark & read"}, {ID: ActionOpen, Label: "Open link"}}}
	var d struct {
		Actions []struct {
			Content    string `xml:"content,attr"`
			Arguments  string `xml:"arguments,attr"`
			Activation string `xml:"activationType,attr"`
		} `xml:"actions>action"`
	}
	if err := xml.Unmarshal([]byte(buildToastXML(n)), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Actions) != 2 || d.Actions[0].Content != "Mark & read" || d.Actions[0].Arguments != "s1-m2#read" || d.Actions[0].Activation != "background" ||
		d.Actions[1].Arguments != "s1-m2#open" || d.Actions[1].Activation != "foreground" {
		t.Fatalf("%+v", d.Actions)
	}
	if strings.Contains(buildToastXML(Notification{ID: "x"}), "<actions>") {
		t.Fatal("actions without buttons")
	}
}
