package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/patrickmn/go-cache"
)

var c *cache.Cache

type Dashboard struct {
	Owner          string
	Repo           string
	DateGenerated  string
	NextGeneration string
	Data           map[string][]Status
}

func main() {
	c = cache.New(15*time.Minute, 30*time.Minute)

	http.HandleFunc("/", handleRequest)

	log.Print("Listening on :3000...")
	err := http.ListenAndServe(":3000", nil) //nolint: gosec
	if err != nil {
		log.Fatal(err)
	}
}

type Status struct {
	SHA         string
	Status      string
	Event       string
	Conclusion  string
	TableStatus string
	JobHTML     string
	WorkflowID  int64
	CreatedAt   time.Time
	PRUrl       string
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "404 not found.", http.StatusNotFound)
		return
	}

	switch r.Method {
	case "GET":
		input := fmt.Sprintf("%s/templates/input.html", http.Dir(os.Getenv("KO_DATA_PATH")))
		http.ServeFile(w, r, input)
	case "POST":
		if err := r.ParseForm(); err != nil {
			fmt.Fprintf(w, "ParseForm() err: %v", err)
			return
		}

		owner := r.FormValue("owner")
		repo := r.FormValue("repo")
		serveTemplate(w, r, owner, repo)
	default:
		fmt.Fprintf(w, "Sorry, only GET and POST methods are supported.")
	}
}

func serveTemplate(w http.ResponseWriter, _ *http.Request, owner, repo string) {
	dataReport := getJobs(c, owner, repo)
	lp := filepath.Join(os.Getenv("KO_DATA_PATH"), "templates/dashboard.html")

	tmpl, _ := template.ParseFiles(lp)
	w.Header().Set("Content-Type", "text/html")

	err := tmpl.ExecuteTemplate(w, "dashboard", dataReport)
	if err != nil {
		// Log the detailed error
		log.Print(err.Error())
		http.Error(w, http.StatusText(500), 500)
	}
}
