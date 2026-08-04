package platformcontrol

import (
	"context"
	"testing"
)

type testSecret []byte

func (s testSecret) WithSecret(ctx context.Context, use func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return use(s)
}

func TestSecretSourceLendsMaterial(t *testing.T) {
	want := "secret"
	err := testSecret(want).WithSecret(context.Background(), func(got []byte) error {
		if string(got) != want {
			t.Fatalf("secret material = %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
