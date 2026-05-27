package kite

import (
	"net/url"
	"testing"
)

func TestSafeFormValuesRedactsSensitiveFields(t *testing.T) {
	form := url.Values{}
	form.Set("api_key", "api-key")
	form.Set("request_token", "request-token")
	form.Set("checksum", "checksum")
	form.Set("tradingsymbol", "SILVERM26JUNFUT")
	form.Set("quantity", "1")

	fields := safeFormValues(form)

	if fields["api_key"] != "[REDACTED]" {
		t.Fatalf("expected api_key to be redacted, got %q", fields["api_key"])
	}
	if fields["request_token"] != "[REDACTED]" {
		t.Fatalf("expected request_token to be redacted, got %q", fields["request_token"])
	}
	if fields["checksum"] != "[REDACTED]" {
		t.Fatalf("expected checksum to be redacted, got %q", fields["checksum"])
	}
	if fields["tradingsymbol"] != "SILVERM26JUNFUT" {
		t.Fatalf("expected tradingsymbol to remain visible, got %q", fields["tradingsymbol"])
	}
	if fields["quantity"] != "1" {
		t.Fatalf("expected quantity to remain visible, got %q", fields["quantity"])
	}
}
