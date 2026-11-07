// Lead Finder finds local businesses on Google Maps that have no website
// and exports them to a spreadsheet (CSV).
//
// Scraping is powered by github.com/gosom/google-maps-scraper (MIT).
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

// version is set at build time with -ldflags "-X main.version=1.0.0".
var version = "dev"

//go:embed web
var webFiles embed.FS

func main() {
	port := flag.Int("port", 8765, "port for the local page")
	noBrowser := flag.Bool("no-browser", false, "do not open the browser automatically")
	showVersion := flag.Bool("version", false, "print the version and exit")
	worker := flag.Bool("worker", false, "internal: run one search (used by the app itself)")
	query := flag.String("query", "", "internal: search text for --worker")
	town := flag.String("town", "", "internal: town for --worker")
	count := flag.Int("count", 20, "internal: places to check for --worker")
	flag.Parse()

	if *showVersion {
		fmt.Println("Lead Finder", version)
		return
	}

	if *worker {
		runWorker(*query, *town, *count)
		return
	}

	ln, err := listen(*port)
	if err != nil {
		fatal("Could not start Lead Finder: %v", err)
	}

	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	srv := &http.Server{Handler: localOnly(newMux(newSearcher())), ReadHeaderTimeout: 10 * time.Second}

	fmt.Println("========================================")
	fmt.Println("  Lead Finder", version)
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("  Lead Finder is running. Your browser should open by itself.")
	fmt.Println("  If it does not, open this address:", url)
	fmt.Println()
	fmt.Println("  Keep this window open while you use the app.")
	fmt.Println("  Close it (or click \"Quit\" on the page) when you are done.")
	fmt.Println()

	if !*noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("Server error: %v", err)
	}
}

// listen binds to localhost only, falling back to a free port if the
// preferred one is busy.
func listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		return ln, nil
	}

	return net.Listen("tcp", "127.0.0.1:0")
}

func newMux(s *searcher) *http.ServeMux {
	mux := http.NewServeMux()

	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("GET /", http.FileServer(http.FS(static)))

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.Status())
	})

	mux.HandleFunc("POST /api/search", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var req struct {
			Business string `json:"business"`
			City     string `json:"city"`
			HowMany  int    `json:"howMany"`
		}

		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}

		if err := s.Start(req.Business, req.City, req.HowMany); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, s.Status())
	})

	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		s.Stop()
		writeJSON(w, http.StatusOK, s.Status())
	})

	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		s.Stop()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

		go func() {
			time.Sleep(500 * time.Millisecond)
			os.Exit(0)
		}()
	})

	mux.HandleFunc("GET /download", func(w http.ResponseWriter, _ *http.Request) {
		data, name, err := s.CSV()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write(data)
	})

	return mux
}

// localOnly rejects requests whose Host is not localhost, which protects
// against DNS-rebinding attacks from other websites.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}

		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// sameOrigin blocks other websites from driving the local app (CSRF).
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	return origin == "http://"+r.Host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("could not open the browser automatically: %v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	fmt.Fprintln(os.Stderr, "Press Enter to close this window.")
	_, _ = fmt.Scanln()
	os.Exit(1)
}
