package server

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dreikanter/nview/internal/index"
	"github.com/dreikanter/nview/internal/logging"
	"github.com/dreikanter/nview/internal/renderer"
	"github.com/dreikanter/nview/web"
)

type Server struct {
	root      string
	editor    string
	logger    *slog.Logger
	renderer  *renderer.Renderer
	index     *index.NoteIndex
	events    *EventHub
	templates *templateSet
}

// NewServer builds a Server rooted at the given notes directory. The logger
// is optional: a nil logger is replaced with a discard logger so handlers
// can always call s.logger.* without a nil check. Callers that want output
// should pass a logger built via internal/logging.
func NewServer(root, editor string, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = logging.Discard()
	}
	store := newRecoveringOSStore(root, logger)
	idx := index.New(store, logger)
	if err := idx.Build(); err != nil {
		return nil, fmt.Errorf("initial index build: %w", err)
	}
	tpls, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	return &Server{
		root:      root,
		editor:    editor,
		logger:    logger,
		renderer:  renderer.NewRenderer(idx),
		index:     idx,
		events:    NewEventHub(store, logger, idx),
		templates: tpls,
	}, nil
}

// Logger returns the server's structured logger. Exposed so cmd wiring can
// share one logger for both request logging and startup messages.
func (s *Server) Logger() *slog.Logger {
	return s.logger
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /n/{x}", s.handleNote)
	mux.HandleFunc("GET /tags", s.handleTags)
	mux.HandleFunc("GET /tags/{tag}", s.handleTagNotes)
	mux.HandleFunc("GET /types", s.handleTypes)
	mux.HandleFunc("GET /types/{type}", s.handleTypeNotes)
	mux.HandleFunc("GET /dates", s.handleDates)
	mux.HandleFunc("GET /dates/{year}", s.handleDateYear)
	mux.HandleFunc("GET /dates/{year}/{month}", s.handleDateMonth)
	mux.HandleFunc("GET /dates/{year}/{month}/{day}", s.handleDateDay)
	mux.HandleFunc("POST /api/edit/{id}", s.handleEdit)
	mux.HandleFunc("GET /api/raw/{id}", s.handleRaw)
	mux.HandleFunc("POST /api/index/refresh", s.handleRefresh)
	mux.HandleFunc("GET /sidebar", s.handleSidebar)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /api/tree/list", s.handleTreeList)
	mux.HandleFunc("GET /", s.handleRoot)

	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		s.logger.Error("failed to open embedded static FS", "err", err)
	} else {
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}

	return logRequests(s.logger)(rejectDirtyPaths(setHXCacheHeaders(mux)))
}

// rejectDirtyPaths returns 400 for any request whose raw URL path is not
// already clean (i.e. contains ".." segments), rather than letting the mux
// silently redirect them.
func rejectDirtyPaths(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawPath := r.URL.Path
		if strings.Contains(rawPath, "..") {
			http.Error(w, "bad request: unclean path", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) StartWatcher() error {
	return s.events.Start()
}

// Shutdown stops the event hub, closing the watcher and draining
// connected clients.
func (s *Server) Shutdown() {
	s.events.Stop()
}
