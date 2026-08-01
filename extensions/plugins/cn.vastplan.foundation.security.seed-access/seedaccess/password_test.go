package seedaccess

import "testing"

func TestPasswordVerifierNormalizesBoundaryWhitespace(t *testing.T) {
	verifier, err := NewPasswordVerifier([]byte(" \t\r\n correct horse battery staple \r\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range [][]byte{
		[]byte("correct horse battery staple"),
		[]byte("\n\tcorrect horse battery staple \r\n"),
	} {
		if !verifier.Verify(candidate) {
			t.Fatalf("normalized password was rejected: %q", candidate)
		}
	}
	if verifier.Verify([]byte("correcthorsebatterystaple")) {
		t.Fatal("password normalization must preserve internal whitespace")
	}
}

func TestPasswordVerifierChecksLengthAfterNormalization(t *testing.T) {
	if _, err := NewPasswordVerifier([]byte(" \t too-short \r\n")); err == nil {
		t.Fatal("password shorter than 12 bytes after normalization was accepted")
	}
}
