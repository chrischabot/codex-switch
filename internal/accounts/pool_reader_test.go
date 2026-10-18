package accounts

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func makePoolFixture(t testing.TB, count int) string {
	t.Helper()
	root := t.TempDir()
	for i := 1; i <= count; i++ {
		dir := filepath.Join(root, fmt.Sprintf("account-%d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"email":"%d@example.test"}`, i)))
		data := []byte(fmt.Sprintf(`{"tokens":{"account_id":"account-%d","id_token":"x.%s.x"}}`, i, claims))
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPoolReaderReusesUnchangedIdentitiesAndObservesExternalWrites(t *testing.T) {
	root := makePoolFixture(t, 6)
	loads := 0
	reader := poolReader{load: func(path string) ([]PoolAccount, error) { loads++; return ReadPool(path) }}
	first, err := reader.read(root)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Email = "caller mutation"
	for i := 0; i < 10; i++ {
		pool, err := reader.read(root)
		if err != nil || pool[0].Email == "caller mutation" {
			t.Fatal("cache leaked mutable result", err)
		}
	}
	if loads != 1 {
		t.Fatalf("unchanged six-account pool loaded %d times, want 1", loads)
	}
	path := filepath.Join(root, "account-1", "auth.json")
	info, _ := os.Stat(path)
	data, _ := os.ReadFile(path)
	// An external atomic replacement with the same length and modification time
	// must invalidate the cache, even when it contains a broken credential.
	data[0] = '!'
	next := path + ".next"
	os.WriteFile(next, data, 0600)
	os.Chtimes(next, info.ModTime(), info.ModTime())
	os.Rename(next, path)
	pool, err := reader.read(root)
	if err != nil || pool[0].ReadError == nil || loads != 2 {
		t.Fatalf("replacement invisible: %v, loads %d", err, loads)
	}
	if _, err := SetAccountReserve(root, "account-2", AccountReserve{SoftPercent: 40, HardPercent: 20}); err != nil {
		t.Fatal(err)
	}
	pool, err = reader.read(root)
	if err != nil || pool[1].Reserve.SoftPercent != 40 {
		t.Fatal("external reserve change invisible", err)
	}
	os.RemoveAll(filepath.Join(root, "account-1"))
	pool, err = reader.read(root)
	if err != nil || len(pool) != 5 {
		t.Fatal("removal invisible", err)
	}
}

func BenchmarkPoolReaderSixAccounts(b *testing.B) {
	root := makePoolFixture(b, 6)
	reader := poolReader{}
	reader.read(root)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reader.read(root); err != nil {
			b.Fatal(err)
		}
	}
}
