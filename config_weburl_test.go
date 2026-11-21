package stonyperms

import (
	"testing"

	"github.com/andreisugu/gate-stonyperms/manager"
)

func TestResolveWebBaseURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil", cfg: nil, want: manager.DefaultPublicWebURL},
		{name: "api disabled", cfg: &Config{APIEnabled: false}, want: manager.DefaultPublicWebURL},
		{
			name: "explicit public url wins",
			cfg: &Config{
				APIEnabled:         true,
				APIListen:          "0.0.0.0:8737",
				APIEditorPublicURL: "http://restedfreevm2nd:8737/",
			},
			want: "http://restedfreevm2nd:8737",
		},
		{
			name: "wildcard listen falls back to loopback",
			cfg:  &Config{APIEnabled: true, APIListen: "0.0.0.0:8737"},
			want: "http://127.0.0.1:8737",
		},
		{
			name: "tls listen uses https",
			cfg: &Config{
				APIEnabled: true,
				APIListen:  "127.0.0.1:9000",
				APITLSCert: "cert.pem",
				APITLSKey:  "key.pem",
			},
			want: "https://127.0.0.1:9000",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ResolveWebBaseURL(test.cfg); got != test.want {
				t.Fatalf("ResolveWebBaseURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeEditorPublicURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "trims trailing slash", input: "http://restedfreevm2nd:8737/", want: "http://restedfreevm2nd:8737"},
		{name: "keeps path", input: "https://example.com/perms/", want: "https://example.com/perms"},
		{name: "rejects scheme", input: "ftp://example.com", wantErr: true},
		{name: "rejects missing host", input: "https:///editor", wantErr: true},
		{name: "rejects query", input: "https://example.com?session=1", wantErr: true},
		{name: "rejects fragment", input: "https://example.com#editor", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEditorPublicURL(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeEditorPublicURL(%q) = %q, want error", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEditorPublicURL(%q) error: %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeEditorPublicURL(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
