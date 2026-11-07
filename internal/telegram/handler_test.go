package telegram

import "testing"

func TestIsHelpCommand(t *testing.T) {
	cases := map[string]bool{
		"/start":                               true,
		"/help":                                true,
		"/start@flanchan_bot":                  true,
		"/help@other_bot":                      true,
		"/gallery":                             false,
		"/":                                    false,
		"start":                                false,
		"https://e-hentai.org/g/1/abcdef1234/": false,
	}
	for in, want := range cases {
		if got := isHelpCommand(in); got != want {
			t.Fatalf("isHelpCommand(%q)=%v，期望 %v", in, got, want)
		}
	}
}
