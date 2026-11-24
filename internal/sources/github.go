package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// GitHubAPI is GitHub's REST API.
const GitHubAPI = "https://api.github.com"

const (
	githubPage = 100
	// maxGitHubPages bounds a read. Partial results cannot advance the cursor.
	maxGitHubPages  = 10
	maxGitHubAnswer = 8 << 20
)

// githubMark ends every comment Machinist writes, so Replies can tell them
// from a person's even when Machinist and the person share a GitHub login.
const githubMark = "<!-- machinist -->"

// GitHubIssues takes work from GitHub issues that carry a label. Adding the
// label hands an issue to Machinist; the person who added it is the
// assignment's actor. Machinist shows how the job goes with labels of its own,
// named after the trigger label: <label>:working, <label>:needs-human,
// <label>:failed and <label>:merged. It is a Tracker.
//
// An issue's ID is owner/name#number.
type GitHubIssues struct {
	// Repositories are the repositories whose issues are read.
	Repositories []string
	// Label hands an issue to Machinist.
	Label string
	// Token returns the token to call GitHub with for a repository.
	Token    func(ctx context.Context, repository string) (string, error)
	Endpoint string
	Client   *http.Client
}

var _ Tracker = (*GitHubIssues)(nil)

func (g *GitHubIssues) Name() string { return "github" }

// The labels Machinist sets, after the trigger label.
func (g *GitHubIssues) working() string    { return g.Label + ":working" }
func (g *GitHubIssues) needsHuman() string { return g.Label + ":needs-human" }
func (g *GitHubIssues) failed() string     { return g.Label + ":failed" }
func (g *GitHubIssues) merged() string     { return g.Label + ":merged" }

type githubLabel struct {
	Name string `json:"name"`
}

type githubUser struct {
	Login string `json:"login"`
}

type githubIssue struct {
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	HTMLURL     string          `json:"html_url"`
	State       string          `json:"state"`
	Labels      []githubLabel   `json:"labels"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}

func (i githubIssue) has(label string) bool {
	return slices.ContainsFunc(i.Labels, func(l githubLabel) bool { return strings.EqualFold(l.Name, label) })
}

type githubEvent struct {
	Event     string      `json:"event"`
	Actor     githubUser  `json:"actor"`
	Label     githubLabel `json:"label"`
	CreatedAt time.Time   `json:"created_at"`
}

type githubComment struct {
	ID        int64      `json:"id"`
	Body      string     `json:"body"`
	User      githubUser `json:"user"`
	IssueURL  string     `json:"issue_url"`
	CreatedAt time.Time  `json:"created_at"`
}

// splitIssue reads owner/name#number.
func splitIssue(issue string) (string, int, error) {
	repository, number, ok := strings.Cut(issue, "#")
	n, err := strconv.Atoi(number)
	if !ok || err != nil || n <= 0 || strings.Count(repository, "/") != 1 {
		return "", 0, fmt.Errorf("github: %q is not owner/name#number", issue)
	}
	return repository, n, nil
}

// Assignments returns open issues with the label, updated after since,
// oldest first. The actor is whoever added the label last.
func (g *GitHubIssues) Assignments(ctx context.Context, since time.Time) ([]Assignment, error) {
	type updatedIssue struct {
		repository string
		issue      githubIssue
	}
	var pending []updatedIssue
	var cutoff time.Time
	since = since.Truncate(time.Second)
	for _, repository := range g.Repositories {
		query := url.Values{"labels": {g.Label}, "state": {"open"}, "sort": {"updated"}, "direction": {"asc"},
			"since": {since.Add(-time.Second).UTC().Format(time.RFC3339)}, "per_page": {strconv.Itoa(githubPage)}}
		for page := 1; page <= maxGitHubPages; page++ {
			query.Set("page", strconv.Itoa(page))
			var issues []githubIssue
			if err := g.call(ctx, repository, http.MethodGet, "repos/"+repository+"/issues?"+query.Encode(), nil, &issues); err != nil {
				return nil, err
			}
			for _, issue := range issues {
				// GitHub lists pull requests as issues too.
				if !issue.UpdatedAt.Before(since) && (len(issue.PullRequest) == 0 || string(issue.PullRequest) == "null") {
					pending = append(pending, updatedIssue{repository, issue})
				}
			}
			if len(issues) < githubPage {
				break
			}
			if page == maxGitHubPages {
				// A different repository must not advance the shared cursor
				// beyond any unread issue, including one in the same second.
				updated := issues[len(issues)-1].UpdatedAt
				if cutoff.IsZero() || updated.Before(cutoff) {
					cutoff = updated
				}
			}
		}
	}
	var found []Assignment
	for _, item := range pending {
		if !cutoff.IsZero() && !item.issue.UpdatedAt.Before(cutoff) {
			continue
		}
		assignment, err := g.assignment(ctx, item.repository, item.issue)
		if err != nil {
			return nil, err
		}
		found = append(found, assignment)
	}
	if len(found) == 0 && !cutoff.IsZero() {
		return nil, fmt.Errorf("github: too many updated issues at %s", cutoff)
	}
	slices.SortStableFunc(found, func(a, b Assignment) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	return found, nil
}

func (g *GitHubIssues) assignment(ctx context.Context, repository string, issue githubIssue) (Assignment, error) {
	id := fmt.Sprintf("%s#%d", repository, issue.Number)
	assignment := Assignment{
		Key: "github:" + id, Issue: id, Identifier: fmt.Sprintf("#%d", issue.Number),
		Title: issue.Title, Body: issue.Body, URL: issue.HTMLURL, Team: repository, UpdatedAt: issue.UpdatedAt,
	}
	// The newest labeled event for the label says who handed the issue over.
	for page := 1; page <= maxGitHubPages; page++ {
		var events []githubEvent
		path := fmt.Sprintf("repos/%s/issues/%d/events?per_page=%d&page=%d", repository, issue.Number, githubPage, page)
		if err := g.call(ctx, repository, http.MethodGet, path, nil, &events); err != nil {
			return Assignment{}, err
		}
		for _, event := range events {
			if event.Event == "labeled" && strings.EqualFold(event.Label.Name, g.Label) && !event.CreatedAt.Before(assignment.GivenAt) {
				assignment.Actor, assignment.GivenAt = strings.ToLower(event.Actor.Login), event.CreatedAt
			}
		}
		if len(events) < githubPage {
			break
		}
		if page == maxGitHubPages {
			assignment.Actor, assignment.GivenAt = "", time.Time{}
		}
	}
	return assignment, nil
}

// Comment posts body on an issue, marked as Machinist's.
func (g *GitHubIssues) Comment(ctx context.Context, issue, body string) error {
	repository, number, err := splitIssue(issue)
	if err != nil {
		return err
	}
	return g.call(ctx, repository, http.MethodPost, fmt.Sprintf("repos/%s/issues/%d/comments", repository, number),
		map[string]string{"body": body + "\n\n" + githubMark}, nil)
}

// relabel removes the labels in drop, adds add when it is set, and comments
// when comment is set.
func (g *GitHubIssues) relabel(ctx context.Context, issue, add, comment string, drop ...string) error {
	repository, number, err := splitIssue(issue)
	if err != nil {
		return err
	}
	for _, label := range drop {
		path := fmt.Sprintf("repos/%s/issues/%d/labels/%s", repository, number, url.PathEscape(label))
		// A label the issue does not have answers 404, which is fine.
		if err := g.call(ctx, repository, http.MethodDelete, path, nil, nil); err != nil && !isGitHubStatus(err, http.StatusNotFound) {
			return err
		}
	}
	if add != "" {
		// GitHub creates a label it does not know yet.
		if err := g.call(ctx, repository, http.MethodPost, fmt.Sprintf("repos/%s/issues/%d/labels", repository, number),
			map[string][]string{"labels": {add}}, nil); err != nil {
			return err
		}
	}
	if comment == "" {
		return nil
	}
	return g.Comment(ctx, issue, comment)
}

// Start labels the issue as being worked on and says Machinist is on it.
func (g *GitHubIssues) Start(ctx context.Context, issue string) error {
	return g.relabel(ctx, issue, g.working(), acknowledgement, g.needsHuman(), g.failed())
}

// Return labels the issue as waiting for a person and comments.
func (g *GitHubIssues) Return(ctx context.Context, issue, comment string) error {
	return g.relabel(ctx, issue, g.needsHuman(), comment, g.working())
}

// Fail labels the issue as failed and says why.
func (g *GitHubIssues) Fail(ctx context.Context, issue, reason string) error {
	return g.relabel(ctx, issue, g.failed(), "Machinist's job failed: "+reason+"\n\nReply here to tell it what to change, and it will run again.", g.working())
}

// Finish labels the issue as merged and closes it.
func (g *GitHubIssues) Finish(ctx context.Context, issue string) error {
	if err := g.relabel(ctx, issue, g.merged(), "", g.working(), g.needsHuman(), g.failed()); err != nil {
		return err
	}
	repository, number, err := splitIssue(issue)
	if err != nil {
		return err
	}
	return g.call(ctx, repository, http.MethodPatch, fmt.Sprintf("repos/%s/issues/%d", repository, number),
		map[string]string{"state": "closed", "state_reason": "completed"}, nil)
}

// Replies returns comments people left on the repositories' issues, created
// after since and no later than until, oldest first. Comments Machinist wrote
// carry githubMark and are left out. Only those on an issue with a job are
// used.
func (g *GitHubIssues) Replies(ctx context.Context, since, until time.Time) ([]Reply, error) {
	var replies []Reply
	for _, repository := range g.Repositories {
		// since filters on when a comment was last updated, so every comment
		// created later is in the answer.
		query := url.Values{"sort": {"created"}, "direction": {"asc"}, "since": {since.UTC().Format(time.RFC3339)}, "per_page": {strconv.Itoa(githubPage)}}
		for page := 1; ; page++ {
			if page > maxGitHubPages {
				return nil, ErrTooManyReplies
			}
			query.Set("page", strconv.Itoa(page))
			var comments []githubComment
			if err := g.call(ctx, repository, http.MethodGet, "repos/"+repository+"/issues/comments?"+query.Encode(), nil, &comments); err != nil {
				return nil, err
			}
			for _, comment := range comments {
				if !until.IsZero() && comment.CreatedAt.After(until) {
					break
				}
				if !comment.CreatedAt.After(since) || strings.HasSuffix(strings.TrimSpace(comment.Body), githubMark) {
					continue
				}
				number := comment.IssueURL[strings.LastIndex(comment.IssueURL, "/")+1:]
				id := repository + "#" + number
				replies = append(replies, Reply{Key: "github:" + id, ID: strconv.FormatInt(comment.ID, 10), Issue: id,
					Author: strings.ToLower(comment.User.Login), Body: comment.Body, CreatedAt: comment.CreatedAt})
			}
			if len(comments) < githubPage || (!until.IsZero() && comments[len(comments)-1].CreatedAt.After(until)) {
				break
			}
		}
	}
	slices.SortStableFunc(replies, func(a, b Reply) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return replies, nil
}

// States reads issues. An issue is assigned while it has the label, and
// active while it is open.
func (g *GitHubIssues) States(ctx context.Context, issues []string) (map[string]IssueState, error) {
	states := map[string]IssueState{}
	for _, id := range issues {
		repository, number, err := splitIssue(id)
		if err != nil {
			return nil, err
		}
		var issue githubIssue
		err = g.call(ctx, repository, http.MethodGet, fmt.Sprintf("repos/%s/issues/%d", repository, number), nil, &issue)
		if isGitHubStatus(err, http.StatusNotFound) || isGitHubStatus(err, http.StatusGone) {
			continue
		}
		if err != nil {
			return nil, err
		}
		states[id] = IssueState{Assigned: issue.has(g.Label), Active: issue.State == "open"}
	}
	return states, nil
}

type githubStatusError struct {
	status int
	body   string
}

func (e *githubStatusError) Error() string {
	return fmt.Sprintf("github: HTTP %d: %s", e.status, e.body)
}

func isGitHubStatus(err error, status int) bool {
	var failure *githubStatusError
	return errors.As(err, &failure) && failure.status == status
}

// call sends one request to GitHub's REST API and decodes the answer into out,
// when out is set.
func (g *GitHubIssues) call(ctx context.Context, repository, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := g.Endpoint
	if endpoint == "" {
		endpoint = GitHubAPI
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(endpoint, "/")+"/"+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if g.Token != nil {
		token, err := g.Token(ctx, repository)
		if err != nil {
			return fmt.Errorf("github: token for %s: %w", repository, err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("github: %w", err)
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, maxGitHubAnswer))
	if err != nil {
		return fmt.Errorf("github: %w", err)
	}
	if response.StatusCode == http.StatusTooManyRequests || (response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0") {
		return fmt.Errorf("github: %w", ErrRateLimited)
	}
	if response.StatusCode >= 300 {
		return &githubStatusError{status: response.StatusCode, body: clip(string(answer), "")}
	}
	if out == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("github: read %s: %w", path, err)
	}
	return nil
}
