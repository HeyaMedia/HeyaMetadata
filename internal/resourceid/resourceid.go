// Package resourceid derives stable Heya-owned identifiers for addressable
// resources which are not canonical entities (for example, a movie
// collection or a television season).
package resourceid

import "github.com/google/uuid"

var namespace = uuid.MustParse("dcf9e1ec-2222-4af5-a82f-bc927c75135b")

// For returns a deterministic opaque UUID. Provider coordinates may be used as
// private input, but callers expose only the resulting Heya resource ID.
func For(kind, privateIdentity string) string {
	return uuid.NewSHA1(namespace, []byte(kind+"\x00"+privateIdentity)).String()
}
