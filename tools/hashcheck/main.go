// Command hashcheck verifies that every password hash in a users.json is
// a bcrypt hash Go can check (it never prints hashes or names).
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	var users map[string]struct{ Hash string }
	b, _ := os.ReadFile(os.Args[1])
	if err := json.Unmarshal(b, &users); err != nil {
		panic(err)
	}
	ok, bad := 0, 0
	for _, u := range users {
		err := bcrypt.CompareHashAndPassword([]byte(u.Hash), []byte("definitely not the password"))
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			ok++
		} else {
			bad++
			fmt.Println("unusable hash:", err)
		}
	}
	fmt.Printf("%d usable, %d unusable\n", ok, bad)
}
