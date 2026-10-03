package server

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dreikanter/notes/note"
)

// recoveringOSStore keeps filesystem parse failures local to the storage
// adapter. Valid notes retain the notes package's metadata semantics;
// malformed notes fall back to metadata recoverable from their filenames.
type recoveringOSStore struct {
	*note.OSStore
	logger *slog.Logger
}

func newRecoveringOSStore(root string, logger *slog.Logger) *recoveringOSStore {
	return &recoveringOSStore{OSStore: note.NewOSStore(root), logger: logger}
}

// All first takes the normal, concurrent OSStore path. If invalid frontmatter
// aborts that operation, it performs one direct pass over the same files so it
// can preserve valid entries and recover every malformed entry independently.
func (s *recoveringOSStore) All(opts ...note.QueryOpt) ([]note.Entry, error) {
	entries, allErr := s.OSStore.All(opts...)
	if allErr == nil || len(opts) != 0 {
		return entries, allErr
	}

	paths, err := notePathsByID(s.Root())
	if err != nil {
		return nil, err
	}
	ids, err := s.OSStore.IDs()
	if err != nil {
		return nil, err
	}

	entries = make([]note.Entry, 0, len(ids))
	for _, id := range ids {
		path := paths[id]
		if path == "" {
			return nil, allErr
		}
		entry, err := s.readEntry(path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *recoveringOSStore) readEntry(path string) (note.Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return note.Entry{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return note.Entry{}, err
	}
	ref, err := note.ParseFilename(strings.TrimSuffix(filepath.Base(path), ".md"))
	if err != nil {
		return note.Entry{}, err
	}
	id, err := strconv.Atoi(ref.ID)
	if err != nil {
		return note.Entry{}, err
	}
	filenameDate, err := time.Parse(note.DateFormat, ref.Date)
	if err != nil {
		return note.Entry{}, err
	}

	fm, body, parseErr := note.ParseNote(data)
	if parseErr != nil {
		s.logger.Warn(
			"invalid frontmatter; using filename metadata",
			"path", path,
			"err", parseErr,
		)
		return note.Entry{
			ID: id,
			Meta: note.Meta{
				CreatedAt: filenameDate,
				UpdatedAt: info.ModTime(),
				Slug:      ref.Slug,
				Type:      ref.Type,
			},
			Body: string(body),
		}, nil
	}

	createdAt := fm.Date
	dateExplicit := !createdAt.IsZero()
	if !dateExplicit {
		createdAt = filenameDate
	}
	slug := fm.Slug
	if slug == "" {
		slug = ref.Slug
	}
	noteType := fm.Type
	if noteType == "" {
		noteType = ref.Type
	}
	return note.Entry{
		ID: id,
		Meta: note.Meta{
			Title:        fm.Title,
			Slug:         slug,
			Type:         noteType,
			CreatedAt:    createdAt,
			DateExplicit: dateExplicit,
			UpdatedAt:    info.ModTime(),
			Tags:         mergedTags(fm.Tags, note.ExtractHashtags(body)),
			Aliases:      append([]string(nil), fm.Aliases...),
			Description:  fm.Description,
			Public:       fm.Public,
		},
		Body: string(body),
	}, nil
}

func mergedTags(frontmatter, hashtags []string) []string {
	set := make(map[string]struct{}, len(frontmatter)+len(hashtags))
	for _, tag := range append(frontmatter, hashtags...) {
		if tag != "" {
			set[strings.ToLower(tag)] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	tags := make([]string, 0, len(set))
	for tag := range set {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// notePathsByID mirrors only the filename-discovery part of OSStore. Normal
// reads and writes remain owned by the notes package.
func notePathsByID(root string) (map[int]string, error) {
	paths := make(map[int]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 || !note.IsDigits(parts[0]) || len(parts[1]) != 2 || !note.IsDigits(parts[1]) {
			return nil
		}
		ref, err := note.ParseFilename(strings.TrimSuffix(filepath.Base(path), ".md"))
		if err != nil {
			return nil
		}
		id, err := strconv.Atoi(ref.ID)
		if err != nil {
			return nil
		}
		if _, exists := paths[id]; !exists {
			paths[id] = path
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return paths, nil
}
