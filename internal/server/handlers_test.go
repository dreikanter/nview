package server

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestServer creates a server rooted at a temp dir containing:
//   - 2026/03/20260331_9201_todo.todo.md  (slug=todo, tags=[todo,daily])
//   - 2026/01/20260101_1_readme.md   (slug=readme)
//   - README.md at root              (plain file for tree-listing tests only)
//
// ID 9201 is the primary test note; ID 1 provides the readme redirect target.
func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "2026", "03"), 0o755)
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	os.WriteFile(
		filepath.Join(dir, "2026", "03", "20260331_9201_todo.todo.md"),
		[]byte("---\ntitle: Todo\ntype: todo\ndescription: Daily task list\ntags: [todo, daily]\n---\n# Todo\n- [x] Done\n- [ ] Pending\n"),
		0o644,
	)
	os.WriteFile(
		filepath.Join(dir, "2026", "01", "20260101_1_readme.md"),
		[]byte("# Welcome\nHello"),
		0o644,
	)
	// Plain README.md at root (not indexed; used only by tree API tests).
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Welcome\nHello"), 0o644)
	srv, err := NewServer(dir, "", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, dir
}

func TestServerStartsWithInvalidFrontmatter(t *testing.T) {
	dir := t.TempDir()
	monthDir := filepath.Join(dir, "2026", "07")
	if err := os.MkdirAll(monthDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		filepath.Join(monthDir, "20260717_10433_unicredit.md"),
		filepath.Join(monthDir, "20260718_10434_broken.md"),
	}
	contents := []string{
		"---\ntitle: UniCredit\ntags: unicredit\n---\n# UniCredit\n",
		"---\ntitle: [unterminated\n---\n# Broken\n",
	}
	for i, path := range paths {
		if err := os.WriteFile(path, []byte(contents[i]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	validPath := filepath.Join(monthDir, "20260719_10435_healthy.md")
	if err := os.WriteFile(validPath, []byte("---\ntitle: Healthy\ntags: [healthy]\n---\n# Healthy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	srv, err := NewServer(dir, "", logger)
	if err != nil {
		t.Fatalf("NewServer with invalid frontmatter: %v", err)
	}

	for _, id := range []string{"10433", "10434"} {
		req := httptest.NewRequest("GET", "/n/"+id, nil)
		w := httptest.NewRecorder()
		srv.Routes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET /n/%s status = %d, want 200; body: %s", id, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest("GET", "/n/healthy", nil)
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), ">healthy<") {
		t.Errorf("valid note metadata was not preserved: status=%d body=%s", w.Code, w.Body.String())
	}

	output := logs.String()
	if got := strings.Count(output, "invalid frontmatter; using filename metadata"); got != len(paths) {
		t.Fatalf("frontmatter warning count = %d, want %d; logs:\n%s", got, len(paths), output)
	}
	for _, path := range paths {
		if got := strings.Count(output, path); got != 1 {
			t.Errorf("warning path count for %q = %d, want 1; logs:\n%s", path, got, output)
		}
	}
}

func TestNoteHandlerBySlug(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/todo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(body, "<h1") || !strings.Contains(body, ">Todo<") {
		t.Errorf("expected frontmatter title <h1> in body, got: %s", body)
	}
	if !strings.Contains(body, ">todo<") || !strings.Contains(body, ">daily<") {
		t.Errorf("expected frontmatter tags in body")
	}
	if !strings.Contains(body, `id="sidebar"`) {
		t.Errorf("expected #sidebar element in layout, got: %s", body)
	}
	if !strings.Contains(body, `id="note-pane"`) {
		t.Errorf("expected #note-pane element in layout, got: %s", body)
	}
}

func TestNoteHandlerByID(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/9201", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ">Todo<") {
		t.Errorf("expected title in body")
	}
}

func TestRefreshEndpoint(t *testing.T) {
	srv, dir := setupTestServer(t)
	handler := srv.Routes()

	// Drop in a new note out-of-band — without the watcher running,
	// the index won't see it until Reconcile is called.
	os.MkdirAll(filepath.Join(dir, "2026", "05"), 0o755)
	os.WriteFile(
		filepath.Join(dir, "2026", "05", "20260501_4242_drift.md"),
		[]byte("---\ntitle: Drift\n---\n# Drift\n"),
		0o644,
	)

	req := httptest.NewRequest("POST", "/api/index/refresh", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"added":1`) {
		t.Errorf("expected added:1 in response, got: %s", body)
	}

	// After refresh the new note should be resolvable.
	if _, ok := srv.index.NoteByID(4242); !ok {
		t.Error("expected note 4242 to be indexed after refresh")
	}
}

func TestNoteHandler404(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/doesnotexist", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestNoteHandlerNotePanePartial verifies that an HX-Request with
// HX-Target: note-pane returns just the note-pane fragment, not a
// full page.
func TestNoteHandlerNotePanePartial(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/todo", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `id="sidebar"`) {
		t.Errorf("note-pane partial should not contain #sidebar, got: %s", body)
	}
	if strings.Contains(body, `id="topbar"`) {
		t.Errorf("note-pane partial should not contain #topbar, got: %s", body)
	}
	if !strings.Contains(body, `id="note-card"`) {
		t.Errorf("note-pane partial should contain the note card, got: %s", body)
	}
}

// TestNoteHandler404Partial verifies that a missing note with
// HX-Target: note-pane yields HTTP 200 and an empty-state body.
func TestNoteHandler404Partial(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/doesnotexist", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for partial 404, got: %d", w.Code, w.Code)
	}
	if !strings.Contains(w.Body.String(), "not found") {
		t.Errorf("expected 'not found' message in body, got: %s", w.Body.String())
	}
}

// TestRootRedirectToReadme pins the / redirect behavior when a note with
// slug "readme" exists.
func TestRootRedirectToReadme(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/n/readme" {
		t.Errorf("redirect location = %q, want /n/readme", loc)
	}
}

// TestRootEmptyState pins the no-readme case: / renders the two-pane
// layout with an empty note-pane.
func TestRootEmptyState(t *testing.T) {
	dir := t.TempDir()
	// No note with slug "readme" in this dir; OSStore requires YYYY/MM layout
	// so plain files at root are not indexed.
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	os.WriteFile(filepath.Join(dir, "2026", "01", "20260101_5.md"), []byte("# Hi"), 0o644)
	srv, err := NewServer(dir, "", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no readme means two-pane empty state)", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="sidebar"`) {
		t.Errorf("empty state should still render #sidebar, got: %s", body)
	}
	if !strings.Contains(body, "No note selected") {
		t.Errorf("empty state should show 'No note selected', got: %s", body)
	}
}

func TestRawHandler(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	// Note with ID 9201 is the todo note.
	req := httptest.NewRequest("GET", "/api/raw/9201", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Todo") {
		t.Errorf("raw = %q, expected todo content", w.Body.String())
	}
}

func TestRawHandlerInvalidID(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/api/raw/notanumber", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestRawHandlerUnknownID(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/api/raw/99999", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestPathTraversal(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	// The rejectDirtyPaths middleware blocks any URL containing "..".
	req := httptest.NewRequest("GET", "/n/../../etc/passwd", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestEditHandlerNoEditor(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("POST", "/api/edit/9201", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no editor)", w.Code)
	}
}

// TestEditHandlerWhitespaceEditor guards against a nil-slice panic when
// the editor env var contains only whitespace.
func TestEditHandlerWhitespaceEditor(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "2026", "01", "20260101_7_note.md"), []byte("# Hi"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	srv, err := NewServer(dir, "   \t  ", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("POST", "/api/edit/7", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
}

func TestEditHandlerUnknownID(t *testing.T) {
	dir := t.TempDir()
	srv, err := NewServer(dir, "true", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("POST", "/api/edit/99999", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (unknown ID)", w.Code)
	}
}

func TestEditHandlerInvalidID(t *testing.T) {
	dir := t.TempDir()
	srv, err := NewServer(dir, "true", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("POST", "/api/edit/notanumber", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestEditHandlerSimpleEditor pins the success path for a plain editor
// binary. Uses `true` so the test does not depend on any real editor.
func TestEditHandlerSimpleEditor(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "2026", "01", "20260101_7_note.md"), []byte("# Hi"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	srv, err := NewServer(dir, "true", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("POST", "/api/edit/7", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %q, want status:ok", w.Body.String())
	}
}

// TestEditHandlerEditorWithArgs is the regression guard for #7: an $EDITOR
// value with embedded arguments must split into binary + args.
func TestEditHandlerEditorWithArgs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "2026", "01", "20260101_7_note.md"), []byte("# Hi"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	srv, err := NewServer(dir, "true --wait -n", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("POST", "/api/edit/7", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"simple":                `'simple'`,
		"with space":            `'with space'`,
		"with'quote":            `'with'\''quote'`,
		"/path/to/note's me.md": `'/path/to/note'\''s me.md'`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoteHandlerStripsRedundantH1(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/todo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	// The markdown has `# Todo` which matches frontmatter title "Todo".
	// The rendered markdown body should not contain a duplicate <h1>Todo</h1>.
	idx := strings.Index(body, `class="markdown-body`)
	if idx == -1 {
		t.Fatalf("expected markdown-body wrapper in body, got: %s", body)
	}
	md := body[idx:]
	if strings.Contains(md, "<h1>Todo</h1>") {
		t.Errorf("expected duplicate <h1>Todo</h1> to be stripped, got: %s", md)
	}
}

func TestTagsHandler(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/tags", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "sidebar")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// The test fixture has tags: [todo, daily]
	if !strings.Contains(body, "todo") || !strings.Contains(body, "daily") {
		t.Errorf("expected tags in sidebar, got: %s", body)
	}
}

func TestTagNotesHandler(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/tags/todo", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "sidebar")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Todo") {
		t.Errorf("expected todo note in filtered list, got: %s", body)
	}
}

func TestTagsHandler_NotePanePartial(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/tags/todo", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Todo") {
		t.Errorf("expected note in tag listing, got: %s", body)
	}
	if !strings.Contains(body, `id="dir-listing"`) {
		t.Errorf("expected dir-listing container, got: %s", body)
	}
}

func TestTypesHandler_NotePanePartial(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/types/todo", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Daily task list") || !strings.Contains(body, "#9201") {
		t.Errorf("expected typed note metadata in listing, got: %s", body)
	}
}

func TestDatesHandlersDrillDown(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	for _, path := range []string{"/dates", "/dates/2026", "/dates/2026/03", "/dates/2026/03/31"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "note-pane")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200, body: %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `id="dir-listing"`) {
			t.Fatalf("%s expected dir listing, got: %s", path, w.Body.String())
		}
	}
}

func TestDatesHandlerRejectsInvalidPath(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	for _, path := range []string{"/dates/abc", "/dates/2026/13", "/dates/2026/03/99"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, w.Code)
		}
	}
}

func TestSidebarPartialRendersRecentAndFilters(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/sidebar?selected=2026/03/20260331_9201_todo.todo.md", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`id="recent-section"`, "Todo", "daily", "todo"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected sidebar partial to contain %q, got: %s", want, body)
		}
	}
}

func TestNoteHeaderEmailStyleMetadata(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/9201", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"2026-03-31", "todo", "#9201", "/todo", "Daily task list"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected note header to contain %q, got: %s", want, body)
		}
	}
}

func TestTagNotesHandlerUnknownTag(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/tags/nonexistent", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "No notes") {
		t.Errorf("expected empty state message, got: %s", body)
	}
}

func TestTypesHandler(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	os.WriteFile(
		filepath.Join(dir, "2026", "01", "20260101_10.journal.md"),
		[]byte("---\ntitle: Day\ntype: journal\n---\n# Day\n"),
		0o644,
	)
	srv, err := NewServer(dir, "", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/types", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "journal") {
		t.Errorf("expected type 'journal' in response, got: %s", w.Body.String())
	}
}

func TestTypeNotesHandler(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	os.WriteFile(
		filepath.Join(dir, "2026", "01", "20260101_10.journal.md"),
		[]byte("---\ntitle: Day\ntype: journal\n---\n# Day\n"),
		0o644,
	)
	srv, err := NewServer(dir, "", nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/types/journal", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Day") {
		t.Errorf("expected journal note title in listing, got: %s", w.Body.String())
	}
}

func TestDatesHandler(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/dates", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	// Fixture has notes from 2026/01/01 and 2026/03/31.
	body := w.Body.String()
	if !strings.Contains(body, "2026") {
		t.Errorf("expected year 2026 in response, got: %s", body)
	}
}

func TestDateNotesHandler(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/dates/2026/03", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "31") {
		t.Errorf("expected March day for 2026-03, got: %s", w.Body.String())
	}
}

func TestDateNotesHandlerNoMatch(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/dates/2025", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "No dated notes") {
		t.Errorf("expected empty state for 2025, got: %s", w.Body.String())
	}
}

func TestNoteHandlerEmbedsInitialSelectedPath(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/n/todo", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `id="tv-initial"`) {
		t.Errorf("expected tv-initial script tag, got: %s", body)
	}
	// selectedPath is the rel-path, not the slug.
	if !strings.Contains(body, `"selectedPath":"2026/03/20260331_9201_todo.todo.md"`) {
		t.Errorf("expected selectedPath=relPath in initial JSON, got: %s", body)
	}
}

func TestTagsEmbedsNullSelectedPath(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/tags", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `"selectedPath":null`) {
		t.Errorf("expected selectedPath=null on tags page, got: %s", body)
	}
}

func TestRootEmbedsNullSelectedPath(t *testing.T) {
	dir := t.TempDir()
	// Only a note with no slug "readme", so root shows empty state.
	os.MkdirAll(filepath.Join(dir, "2026", "01"), 0o755)
	os.WriteFile(filepath.Join(dir, "2026", "01", "20260101_5.md"), []byte("x"), 0o644)
	srv, _ := NewServer(dir, "", nil)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `"selectedPath":null`) {
		t.Errorf("expected selectedPath=null at empty root, got: %s", body)
	}
}

// TestTagNotesHandlerHrefUsesNID verifies that tag-filtered note listings
// produce /n/{id} hrefs.
func TestTagNotesHandlerHrefUsesNID(t *testing.T) {
	srv, _ := setupTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest("GET", "/tags/todo", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "note-pane")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, fmt.Sprintf(`href="/n/%d"`, 9201)) {
		t.Errorf("expected /n/9201 href in tag listing, got: %s", body)
	}
}
