package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/owainlewis/machinist/internal/tickets"
)

func TestGitHubIssueLifecycle(t *testing.T) {
	intake, _ := newIntake(t)
	intake.Presets = func(name string) bool { return name == "default" }
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	intake.Store.SetClock(func() time.Time { return now })
	var changes []string
	var comments []githubComment
	updated := now.Add(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake" {
			t.Error("missing repository token")
		}
		reply := func(value any) { _ = json.NewEncoder(w).Encode(value) }
		path := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues")
		if r.Method != http.MethodGet {
			switch {
			case strings.Contains(path, "/labels"):
				var data map[string][]string
				if r.Method == http.MethodPost {
					_ = json.NewDecoder(r.Body).Decode(&data)
				}
				changes = append(changes, r.Method+" "+path+" "+strings.Join(data["labels"], ","))
			case strings.HasSuffix(path, "/comments"):
				var data map[string]string
				_ = json.NewDecoder(r.Body).Decode(&data)
				if !strings.HasSuffix(data["body"], githubMark) {
					t.Error("unmarked bot comment")
				}
			case r.Method == http.MethodPatch:
				var data map[string]string
				_ = json.NewDecoder(r.Body).Decode(&data)
				changes = append(changes, data["state"])
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		switch path {
		case "":
			if r.URL.Query().Get("labels") != "fabrika" {
				t.Error("missing issue label filter")
			}
			reply([]githubIssue{
				{Number: 1, Title: "Add tests", HTMLURL: "https://github.com/owner/repo/issues/1", UpdatedAt: updated},
				{Number: 2, Title: "Rejected", UpdatedAt: updated},
				{Number: 3, PullRequest: json.RawMessage("{}"), UpdatedAt: updated},
			})
		case "/1/events", "/2/events":
			actor := "Maintainer"
			if path == "/2/events" {
				actor = "Other"
			}
			reply([]githubEvent{
				{Event: "labeled", Actor: githubUser{Login: "Maintainer"}, Label: githubLabel{Name: "fabrika"}, CreatedAt: updated.Add(-time.Hour)},
				{Event: "labeled", Actor: githubUser{Login: actor}, Label: githubLabel{Name: "fabrika"}, CreatedAt: updated},
			})
		case "/comments":
			reply(comments)
		case "/1":
			reply(githubIssue{State: "open", Labels: []githubLabel{{Name: "fabrika"}}})
		default:
			t.Errorf("unexpected GitHub path %s", path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := &Poller{
		Tracker: &GitHubIssues{Repositories: []string{"owner/repo"}, Label: "fabrika", Endpoint: srv.URL,
			Client: srv.Client(), Token: func(context.Context, string) (string, error) { return "fake", nil }},
		Rules:          TrackerRules{AllowedUsers: []string{"maintainer"}, Preset: "default", Teams: map[string]string{"owner/repo": "owner/repo"}},
		Intake:         intake,
		Store:          intake.Store,
		MaxJobsPerHour: 10,
		ReportLeftOpen: true,
		now:            func() time.Time { return now },
	}
	poll := func() {
		t.Helper()
		if err := p.Poll(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	jobs, err := intake.Store.ListTickets(t.Context(), tickets.TicketFilter{})
	if err != nil || len(jobs) != 1 || jobs[0].Source != "github" {
		t.Fatalf("jobs = %+v, err = %v", jobs, err)
	}
	job := jobs[0]
	apply := func(transition tickets.Transition) {
		t.Helper()
		if err := intake.Store.Apply(t.Context(), job.ID, tickets.StateWorking, transition); err != nil {
			t.Fatal(err)
		}
	}
	apply(tickets.Transition{State: tickets.StatePRReady, PRNumber: 7})
	poll()
	poll()
	now = now.Add(time.Minute)
	comments = []githubComment{
		{ID: 1, Body: "Machinist says hello. " + githubMark, User: githubUser{Login: "Maintainer"}, IssueURL: "/issues/1", CreatedAt: now},
		{ID: 2, Body: "Please revise.", User: githubUser{Login: "Maintainer"}, IssueURL: "/issues/1", CreatedAt: now},
	}
	poll()
	if _, err := intake.Store.TicketBySourceKey(t.Context(), "github-comment:2"); err != nil {
		t.Fatal(err)
	}
	apply(tickets.Transition{State: tickets.StateDone, PRNumber: 7, MergedAt: &now})
	poll()
	poll()
	got := strings.Join(changes, "\n")
	for _, change := range []string{"POST /1/labels fabrika:working", "POST /1/labels fabrika:needs-human", "POST /1/labels fabrika:merged", "closed"} {
		want := 1
		if strings.HasSuffix(change, ":working") {
			want = 2
		}
		if strings.Count(got, change) != want {
			t.Errorf("wanted %d %q, got:\n%s", want, change, got)
		}
	}
}

func TestGitHubRepliesStopAtEndOfWindow(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			t.Errorf("paged past the requested window")
		}
		page := make([]githubComment, githubPage)
		for i := range page {
			page[i] = githubComment{ID: int64(i + 1), Body: fmt.Sprint(i), IssueURL: "/issues/1", CreatedAt: now.Add(time.Duration(i) * time.Second)}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	source := &GitHubIssues{Repositories: []string{"owner/repo"}, Endpoint: server.URL}
	replies, err := source.Replies(t.Context(), now.Add(-time.Second), now.Add(time.Second))
	if err != nil || len(replies) != 2 {
		t.Fatalf("replies = %v, error = %v", replies, err)
	}
}

func TestGitHubAssignmentsDoNotAdvancePastUnreadRepository(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value any
		switch r.URL.Path {
		case "/repos/owner/busy/issues":
			page := make([]githubIssue, githubPage)
			for i := range page {
				page[i].UpdatedAt = now.Add(-time.Minute)
			}
			if r.URL.Query().Get("page") == fmt.Sprint(maxGitHubPages) {
				page[githubPage-2] = githubIssue{Number: 1, UpdatedAt: now.Add(time.Second)}
				page[githubPage-1] = githubIssue{Number: 2, UpdatedAt: now.Add(2 * time.Second)}
			}
			value = page
		case "/repos/owner/quiet/issues":
			value = []githubIssue{{Number: 1, UpdatedAt: now.Add(3 * time.Second)}}
		case "/repos/owner/busy/issues/1/events":
			value = []githubEvent{}
		default:
			t.Errorf("unexpected call past safe cursor frontier: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	defer server.Close()
	source := &GitHubIssues{Repositories: []string{"owner/busy", "owner/quiet"}, Endpoint: server.URL}
	got, err := source.Assignments(t.Context(), now)
	if err != nil || len(got) != 1 || got[0].Issue != "owner/busy#1" {
		t.Fatalf("assignments = %v, error = %v", got, err)
	}
}

func TestTrackerDecideUsesRepositoryPreset(t *testing.T) {
	rules := TrackerRules{
		AllowedUsers: []string{"maintainer"}, Preset: "fabrika",
		Teams:             map[string]string{"owner/toy": "owner/toy", "owner/nymo": "Owner/Nymo"},
		RepositoryPresets: map[string]string{"owner/nymo": "nymo"},
	}
	for team, expected := range map[string]string{"owner/toy": "fabrika", "owner/nymo": "nymo"} {
		job, reason := rules.Decide(Assignment{Team: team, Actor: "maintainer"})
		if reason != "" || job.preset != expected {
			t.Fatalf("%s: preset = %s, reason = %s", team, job.preset, reason)
		}
	}
}
