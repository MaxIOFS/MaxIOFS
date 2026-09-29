package encsecret

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-encsecret-*")
	require.NoError(t, err)
	db, err := sql.Open("sqlite", filepath.Join(dir, "maxiofs.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	require.NoError(t, migrations.NewMigrationManager(db, nil).Migrate())
	return db
}

func stored(t *testing.T, db *sql.DB) string {
	t.Helper()
	var secret string
	require.NoError(t, db.QueryRow(`SELECT secret FROM encryption_secret WHERE id = 1`).Scan(&secret))
	return secret
}

// The first start stores the configured secret, or the first fallback, or a
// random one; every later start uses the stored one, whatever is configured.
func TestBootstrapStoresTheSecretOnce(t *testing.T) {
	for _, c := range []struct {
		name       string
		configured string
		fallbacks  []string
		want       string
	}{
		{"configured", "configured-secret", []string{"jwt-secret"}, "configured-secret"},
		{"fallback", "", []string{"", "jwt-secret"}, "jwt-secret"},
	} {
		db := openDB(t)
		s, err := Bootstrap(db, c.configured, c.fallbacks...)
		require.NoError(t, err, c.name)
		assert.Equal(t, c.want, s.Current(), c.name)
		assert.Equal(t, c.want, stored(t, db), c.name)

		again, err := Bootstrap(db, "another-secret", "another-fallback")
		require.NoError(t, err)
		assert.Equal(t, c.want, again.Current(), "%s: the stored secret is kept", c.name)
	}

	db := openDB(t)
	s, err := Bootstrap(db, "", "")
	require.NoError(t, err)
	assert.Len(t, s.Current(), 64, "32 random bytes")
	other, err := Bootstrap(openDB(t), "")
	require.NoError(t, err)
	assert.NotEqual(t, s.Current(), other.Current())
	again, err := Bootstrap(db, "")
	require.NoError(t, err)
	assert.Equal(t, s.Current(), again.Current(), "a generated secret is the same every start")
}

// recorder is a Reencrypter over a table of its own: it rewrites the values
// encrypted with from, names the ones neither secret produced, and can fail.
type recorder struct {
	calls [][2]string
	fail  bool
}

func (r *recorder) reencrypt(ctx context.Context, tx *sql.Tx, from, to string) ([]string, error) {
	r.calls = append(r.calls, [2]string{from, to})
	if r.fail {
		return nil, errors.New("disk full")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, value FROM sealed`)
	if err != nil {
		return nil, err
	}
	type row struct{ id, value string }
	var all []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.value); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, x)
	}
	rows.Close()
	var unreadable []string
	for _, x := range all {
		switch {
		case strings.HasPrefix(x.value, from+":"):
			if from != to {
				if _, err := tx.ExecContext(ctx, `UPDATE sealed SET value = ? WHERE id = ?`, to+":"+strings.TrimPrefix(x.value, from+":"), x.id); err != nil {
					return nil, err
				}
			}
		case strings.HasPrefix(x.value, to+":"):
		default:
			unreadable = append(unreadable, x.id)
		}
	}
	return unreadable, nil
}

func sealed(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, value FROM sealed`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, value string
		require.NoError(t, rows.Scan(&id, &value))
		out[id] = value
	}
	return out
}

// Adopting a secret rewrites every stored credential and stores the secret in
// one transaction; a failure changes neither. What no secret decrypts is
// named. Check names it without writing.
func TestAdoptRewritesEveryCredentialAtOnce(t *testing.T) {
	db := openDB(t)
	_, err := db.Exec(`CREATE TABLE sealed (id TEXT PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO sealed VALUES ('mine', 'old:a'), ('theirs', 'new:b'), ('lost', 'other:c')`)
	require.NoError(t, err)
	s, err := Bootstrap(db, "old")
	require.NoError(t, err)
	first, second := &recorder{}, &recorder{}
	s.SetReencrypters(first.reencrypt, second.reencrypt)

	unreadable, err := s.Check(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"theirs", "lost", "theirs", "lost"}, unreadable)
	assert.Equal(t, [][2]string{{"old", "old"}}, first.calls)
	assert.Equal(t, map[string]string{"mine": "old:a", "theirs": "new:b", "lost": "other:c"}, sealed(t, db), "a check writes nothing")

	second.fail = true
	_, err = s.Adopt(context.Background(), "new")
	require.Error(t, err)
	assert.Equal(t, "old", s.Current())
	assert.Equal(t, "old", stored(t, db))
	assert.Equal(t, "old:a", sealed(t, db)["mine"], "nothing is kept of a failed adoption")

	second.fail = false
	unreadable, err = s.Adopt(context.Background(), "new")
	require.NoError(t, err)
	assert.Equal(t, []string{"lost", "lost"}, unreadable)
	assert.Equal(t, "new", s.Current())
	assert.Equal(t, "new", stored(t, db))
	assert.Equal(t, map[string]string{"mine": "new:a", "theirs": "new:b", "lost": "other:c"}, sealed(t, db))

	calls := len(first.calls)
	unreadable, err = s.Adopt(context.Background(), "new")
	require.NoError(t, err)
	assert.Empty(t, unreadable)
	assert.Len(t, first.calls, calls, "the secret it holds is not adopted again")
	_, err = s.Adopt(context.Background(), "")
	assert.Error(t, err)
}

// A fingerprint tells secrets apart without containing them.
func TestFingerprint(t *testing.T) {
	assert.Equal(t, Fingerprint("a"), Fingerprint("a"))
	assert.NotEqual(t, Fingerprint("a"), Fingerprint("b"))
	assert.NotContains(t, Fingerprint("secret-value"), "secret-value")
	assert.Len(t, Fingerprint("a"), 16)
}
