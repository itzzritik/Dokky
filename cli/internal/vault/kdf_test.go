package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewKDFDefaults(t *testing.T) {
	useRealCost(t)
	k := NewKDF()
	require.Equal(t, "argon2id", k.Alg)
	require.Len(t, k.Salt, 32)
	require.Equal(t, uint32(3), k.T)
	require.Equal(t, uint32(65536), k.M)
	require.Equal(t, uint8(4), k.P)
	require.NoError(t, k.Validate())
	require.NotEqual(t, k.Salt, NewKDF().Salt)
}

func TestKDFCapsRejectHostileParams(t *testing.T) {
	useRealCost(t)
	good := NewKDF()
	cases := map[string]func(k *KDF){
		"other alg":       func(k *KDF) { k.Alg = "scrypt" },
		"empty alg":       func(k *KDF) { k.Alg = "" },
		"short salt":      func(k *KDF) { k.Salt = k.Salt[:31] },
		"long salt":       func(k *KDF) { k.Salt = append(k.Salt, 0) },
		"no salt":         func(k *KDF) { k.Salt = nil },
		"zero time":       func(k *KDF) { k.T = 0 },
		"huge time":       func(k *KDF) { k.T = 11 },
		"max time":        func(k *KDF) { k.T = 1 << 31 },
		"tiny memory":     func(k *KDF) { k.M = 19455 },
		"huge memory":     func(k *KDF) { k.M = 262145 },
		"4 GiB memory":    func(k *KDF) { k.M = 4 << 20 },
		"zero memory":     func(k *KDF) { k.M = 0 },
		"zero threads":    func(k *KDF) { k.P = 0 },
		"too many thread": func(k *KDF) { k.P = 17 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			k := good
			k.Salt = bytes.Clone(good.Salt)
			mutate(&k)
			require.Error(t, k.Validate())
			start := time.Now()
			_, err := PasswordKEK([]byte("correct horse"), k)
			require.Error(t, err)
			require.Less(t, time.Since(start), time.Second, "argon2 must not run on hostile params")
		})
	}
	edges := good
	edges.T, edges.M, edges.P = 1, 19456, 1
	require.NoError(t, edges.Validate())
	edges.T, edges.M, edges.P = 10, 262144, 16
	require.NoError(t, edges.Validate())
}

func TestKDFJSON(t *testing.T) {
	k := NewKDF()
	b, err := json.Marshal(k)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	require.Equal(t, base64.StdEncoding.EncodeToString(k.Salt), m["salt"])
	require.ElementsMatch(t, []string{"alg", "salt", "t", "m", "p"}, keys(m))

	var back KDF
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, k, back)

	require.Error(t, json.Unmarshal([]byte(`{"alg":"argon2id","p":300}`), &back), "p is a byte")
	require.Error(t, json.Unmarshal([]byte(`{"alg":"argon2id","m":-1}`), &back))
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPasswordKEK(t *testing.T) {
	k := NewKDF()
	a, err := PasswordKEK([]byte("correct horse"), k)
	require.NoError(t, err)
	require.Len(t, a, 32)
	again, err := PasswordKEK([]byte("correct horse"), k)
	require.NoError(t, err)
	require.Equal(t, a, again)

	other, err := PasswordKEK([]byte("correct horsf"), k)
	require.NoError(t, err)
	require.NotEqual(t, a, other)

	salted := k
	salted.Salt = NewKDF().Salt
	other, err = PasswordKEK([]byte("correct horse"), salted)
	require.NoError(t, err)
	require.NotEqual(t, a, other)

	_, err = PasswordKEK(nil, k)
	require.Error(t, err)
}

func TestPasswordKEKNormalisesUnicode(t *testing.T) {
	k := NewKDF()
	composed := []byte("café au lait")
	decomposed := []byte("café au lait")
	a, err := PasswordKEK(composed, k)
	require.NoError(t, err)
	b, err := PasswordKEK(decomposed, k)
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Equal(t, []byte("café au lait"), decomposed, "the caller's bytes are left for the caller to zero")
}

func TestOtherKEKs(t *testing.T) {
	salt := NewKDF().Salt
	r, err := RecoveryKEK(bytes.Repeat([]byte{7}, RecoverySize), salt)
	require.NoError(t, err)
	require.Len(t, r, 32)
	r2, err := RecoveryKEK(bytes.Repeat([]byte{7}, RecoverySize), NewKDF().Salt)
	require.NoError(t, err)
	require.NotEqual(t, r, r2, "the salt goes into the recovery KEK")

	_, err = RecoveryKEK(make([]byte, 19), salt)
	require.Error(t, err)
	_, err = RecoveryKEK(make([]byte, RecoverySize), salt[:8])
	require.Error(t, err)

	d, err := DeviceKEK(key(7))
	require.NoError(t, err)
	require.Len(t, d, 32)
	p, err := derive(key(7), nil, infoPasswordKEK)
	require.NoError(t, err)
	require.NotEqual(t, d, p, "each KEK has its own label")
	_, err = DeviceKEK(make([]byte, 16))
	require.Error(t, err)
}
