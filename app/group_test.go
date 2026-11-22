package main

import "testing"

func TestGroupToken(t *testing.T) {
	for in, want := range map[string]string{
		"f1party://group/CZTcMWqVatN5uw6f":       "CZTcMWqVatN5uw6f",
		"f1party://group/CZTcMWqVatN5uw6f/":      "CZTcMWqVatN5uw6f",
		"f1party://group/short":                  "",
		"f1party://group/bad-token-chars!":       "",
		"f1party://join/ABCDE":                   "",
		"https://example.com/group/AAAAAAAAAAAA": "",
	} {
		if got := groupToken(in); got != want {
			t.Errorf("groupToken(%q) = %q, want %q", in, got, want)
		}
	}
	if !isGroupKey("f1p_ExampleExampleExample1") || isGroupKey("f1p_short") || isGroupKey("https://discord.com/api/webhooks/1/x") {
		t.Error("isGroupKey")
	}
}

func TestTeamLinkToken(t *testing.T) {
	for in, want := range map[string]string{
		"https://jeremyklootwijk.nl/f1party/group/CZTcMWqVatN5uw6f": "CZTcMWqVatN5uw6f",
		"f1party://group/CZTcMWqVatN5uw6f":                          "CZTcMWqVatN5uw6f",
		"http://jeremyklootwijk.nl/f1party/group/CZTcMWqVatN5uw6f":  "",
		"https://jeremyklootwijk.nl/f1party/join/ABCDE":             "",
		"f1p_ExampleExampleExample1":                                "",
	} {
		if got := teamLinkToken(in); got != want {
			t.Errorf("teamLinkToken(%q) = %q, want %q", in, got, want)
		}
	}
}
