package auth

import "testing"

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if h == "hunter2" {
		t.Fatal("hash equals plaintext")
	}
	if !VerifyPassword(h, "hunter2") {
		t.Fatal("verify should pass for correct password")
	}
	if VerifyPassword(h, "wrong") {
		t.Fatal("verify should fail for wrong password")
	}
}
