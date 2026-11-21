package mail

import "testing"

func TestDisplayNameRemovesSurroundingQuotes(t *testing.T) {
	for in, want := range map[string]string{
		`"Cindy"`:           "Cindy",
		` "Cindy" `:         "Cindy",
		`'Cindy'`:           "Cindy",
		`"Say \"hi\" team"`: `Say "hi" team`,
		`"通知中心"`:            "通知中心",
		`Cindy`:             "Cindy",
		`"Cindy`:            `"Cindy`,
		`"`:                 `"`,
		`""`:                "",
		`Cindy "Ops" Team`:  `Cindy "Ops" Team`,
		`O'Neil`:            "O'Neil",
	} {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}
