package mail

import (
	"github.com/mingzaily/mailwake/internal/fault"
	"testing"
)

func TestFolderCheckValidationAndBudget(t *testing.T) {
	for _, check := range []string{"", "1m", "REALTIME", "content"} {
		if _, err := NormalizeSubscriptions([]Folder{{Name: "INBOX", Check: check}}); fault.From(err, "").Code != "folder_check_invalid" {
			t.Fatal("invalid check accepted", check, err)
		}
	}
	if _, err := NormalizeSubscriptions([]Folder{{"INBOX", "5m"}, {"inbox", "15m"}}); err == nil {
		t.Fatal("duplicate normalized name accepted")
	}
	state, err := NormalizeSubscriptions([]Folder{{"inbox", Realtime}, {"Archive", "5m"}, {"Later", "15m"}})
	if err != nil || state[0].Name != "INBOX" {
		t.Fatal(state, err)
	}
	if err := CheckConnectionBudget(state, 3); err != nil {
		t.Fatal("scheduled folders did not share slot", err)
	}
	if err := CheckConnectionBudget(state, 2); fault.From(err, "").Code != "connection_budget_exceeded" {
		t.Fatal("reserved slot consumed", err)
	}
	if err := CheckConnectionBudget(state[1:], 2); err != nil {
		t.Fatal("all scheduled need only one slot", err)
	}
}
