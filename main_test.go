package main

import (
	"testing"

	"github.com/egoist/mygo"

	"gotify-desktop/internal/api"
)

func TestConfirmDialog(t *testing.T) {
	o := confirmDialog(nil, api.Confirmation{
		Title: "Remove?", Message: "Gone.", ConfirmLabel: "Remove", CancelLabel: "Keep", Destructive: true,
	})
	if o.Type != mygo.MessageWarning || o.Title != appName || o.Message != "Remove?" || o.Detail != "Gone." {
		t.Fatalf("destructive: %+v", o)
	}
	if o.DefaultButton != 1 || o.CancelButton != 1 || len(o.Buttons) != 2 || o.Buttons[0] != "Remove" || o.Buttons[1] != "Keep" {
		t.Fatalf("buttons: %+v", o)
	}
	o = confirmDialog(nil, api.Confirmation{Message: "Sure?", ConfirmLabel: "OK", CancelLabel: "Cancel"})
	if o.Type != mygo.MessageQuestion || o.Message != "Sure?" || o.Detail != "" {
		t.Fatalf("question: %+v", o)
	}
	o = confirmDialog(nil, api.Confirmation{Title: "Only title"})
	if o.Message != "Only title" || o.Detail != "" {
		t.Fatalf("title only: %+v", o)
	}
}
