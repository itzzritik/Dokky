package vault

import (
	"os"
	"testing"
)

var realCost = cost

// argon2 at the real cost takes a good part of a second, so tests run it cheap unless they ask
func TestMain(m *testing.M) {
	cost = kdfCost{t: 1, m: 64, p: 1, minM: 8}
	os.Exit(m.Run())
}

func useRealCost(t *testing.T) {
	cheap := cost
	cost = realCost
	t.Cleanup(func() { cost = cheap })
}
