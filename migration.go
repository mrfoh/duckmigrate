package duckmigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
)

type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

type Migration struct {
	Version    uint64
	Title      string
	UpSQL      string
	DownSQL    string
	HasDown    bool
	UpChecksum string
}

var filenamePattern = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

func parseFilename(name string) (version uint64, title string, dir Direction, ok bool) {
	m := filenamePattern.FindStringSubmatch(name)
	if m == nil {
		return 0, "", "", false
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		return 0, "", "", false
	}
	return v, m[2], Direction(m[3]), true
}

func checksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}
