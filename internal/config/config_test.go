package config

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"acme":                                   "https://acme-services.pm.beyondtrustcloud.com",
		"acme-services":                          "https://acme-services.pm.beyondtrustcloud.com",
		"https://acme.pm.beyondtrustcloud.com/x": "https://acme-services.pm.beyondtrustcloud.com",
		"ACME-services.pm.beyondtrustcloud.com/management-api/swagger": "https://acme-services.pm.beyondtrustcloud.com",
		"http://127.0.0.1:8080": "http://127.0.0.1:8080",
		"epm.corp.internal":     "https://epm.corp.internal",
	}
	for in, want := range cases {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeURL(" "); err == nil {
		t.Error("empty URL must fail")
	}
}

func TestProfileNames(t *testing.T) {
	for _, ok := range []string{"prod", "sand-box", "a.b_c1"} {
		if !ValidProfileName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-x", "a b", "x;rm", "ä"} {
		if ValidProfileName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
