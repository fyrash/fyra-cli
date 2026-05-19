package main

import (
	"testing"
)

func TestValidatePasswordMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		password string
		confirm  string
		wantErr  bool
	}{
		{name: "match", password: "secret", confirm: "secret", wantErr: false},
		{name: "mismatch", password: "secret", confirm: "different", wantErr: true},
		{name: "both empty", password: "", confirm: "", wantErr: false},
		{name: "one empty", password: "secret", confirm: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePasswordMatch(tt.password, tt.confirm)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePasswordMatch(%q, %q) err = %v, wantErr = %v",
					tt.password, tt.confirm, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePasswordMatch_errorMessage(t *testing.T) {
	err := validatePasswordMatch("a", "b")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}
