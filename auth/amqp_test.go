package auth

import (
	"net/url"
	"testing"
)

func TestValidateAMQPScheme(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{"amqp is valid", "amqp://user:pass@host/vhost", false},
		{"amqps is valid", "amqps://user:pass@host/vhost", false},
		{"http is rejected", "http://host/api/auth", true},
		{"https is rejected", "https://host/api/auth", true},
		{"transposed typo is rejected", "ampq://host/vhost", true},
		{"suffixed typo is rejected", "amqp2://host/vhost", true},
		{"missing scheme is rejected", "host/vhost", true},
		{"empty url is rejected", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURL)
			if err != nil {
				t.Fatalf("failed to parse test url %q: %v", tt.rawURL, err)
			}
			err = ValidateAMQPScheme(u)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAMQPScheme(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAMQPSchemeNil(t *testing.T) {
	if err := ValidateAMQPScheme(nil); err == nil {
		t.Error("ValidateAMQPScheme(nil) expected error, got nil")
	}
}
