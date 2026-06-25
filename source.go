package duckmigrate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
)

type Source interface {
	Migrations() ([]*Migration, error)
}

type fsSource struct {
	fsys fs.FS
	root string
}

func NewFSSource(fsys fs.FS, root string) Source {
	if root == "" {
		root = "."
	}
	return &fsSource{fsys: fsys, root: root}
}

func NewFileSource(dir string) Source {
	return &fsSource{fsys: os.DirFS(dir), root: "."}
}

func (s *fsSource) Migrations() ([]*Migration, error) {
	entries, err := fs.ReadDir(s.fsys, s.root)
	if err != nil {
		return nil, fmt.Errorf("duckmigrate: reading migration source: %w", err)
	}

	byVersion := map[uint64]*Migration{}
	seenUp := map[uint64]bool{}
	seenDown := map[uint64]bool{}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		version, title, dir, ok := parseFilename(e.Name())
		if !ok {
			continue
		}
		content, err := fs.ReadFile(s.fsys, path.Join(s.root, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("duckmigrate: reading %s: %w", e.Name(), err)
		}

		m := byVersion[version]
		if m == nil {
			m = &Migration{Version: version, Title: title}
			byVersion[version] = m
		}

		switch dir {
		case DirectionUp:
			if seenUp[version] {
				return nil, ErrDuplicateVersion{Version: version, Direction: DirectionUp}
			}
			seenUp[version] = true
			m.Title = title
			m.UpSQL = string(content)
		case DirectionDown:
			if seenDown[version] {
				return nil, ErrDuplicateVersion{Version: version, Direction: DirectionDown}
			}
			seenDown[version] = true
			m.DownSQL = string(content)
			m.HasDown = true
		}
	}

	out := make([]*Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if !seenUp[m.Version] {
			return nil, fmt.Errorf("duckmigrate: migration %d has no .up.sql file", m.Version)
		}
		m.UpChecksum = checksum(m.UpSQL)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}
