// passgen generates random passwords or word passphrases.
//
//	passgen                     three 20-char passwords
//	passgen -len 32 -n 1        one long one
//	passgen -words 4            correct-horse style passphrase
package main

import (
	"crypto/rand"
	_ "embed"
	"flag"
	"fmt"
	"math/big"
	"strings"
)

// The wordlist ships inside the binary: go:embed turns a file into a
// variable at build time — no runtime file lookups to get wrong.
//
//go:embed words.txt
var wordsFile string

const (
	lower   = "abcdefghijklmnopqrstuvwxyz"
	upper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits  = "0123456789"
	symbols = "!@#$%^&*-_=+"
)

func main() {
	var (
		n         = flag.Int("n", 3, "how many to generate")
		length    = flag.Int("len", 20, "password length")
		noDigits  = flag.Bool("no-digits", false, "letters only")
		noSymbols = flag.Bool("no-symbols", false, "skip !@#$ characters")
		words     = flag.Int("words", 0, "passphrase mode: number of words (0 = password mode)")
		sep       = flag.String("sep", "-", "passphrase separator")
	)
	flag.Parse()

	alphabet := lower + upper
	if !*noDigits {
		alphabet += digits
	}
	if !*noSymbols {
		alphabet += symbols
	}
	wordlist := strings.Fields(wordsFile)

	for range *n {
		if *words > 0 {
			fmt.Println(passphrase(wordlist, *words, *sep))
		} else {
			fmt.Println(password(alphabet, *length))
		}
	}
}

// randInt returns a uniform random int in [0, max) from crypto/rand.
// math/rand/v2 is fine for games and simulations; for secrets, predictable
// output is the entire failure mode, so the crypto source is non-negotiable.
func randInt(max int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(err) // the OS entropy source failing is not a recoverable state
	}
	return int(v.Int64())
}

func password(alphabet string, length int) string {
	var b strings.Builder
	b.Grow(length) // one allocation instead of log(n) regrows
	for range length {
		b.WriteByte(alphabet[randInt(len(alphabet))])
	}
	return b.String()
}

func passphrase(words []string, n int, sep string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = words[randInt(len(words))]
	}
	return strings.Join(parts, sep)
}
